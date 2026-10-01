package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"edge/internal/board"
	"edge/internal/oddspull"
	"edge/internal/wager"
)

// board props-report is the analysis layer the props tab never had. The tab
// (handleProps) reads a capture and prices it, but has no standalone command,
// no de-vig for two-sided props, and no way to write down what a capture said
// so a later report can cite it. This command adds all three, reusing the
// tab's own machinery -- readIngest to merge the captures, devigPairs to pair
// and de-vig two-sided markets, BoostedBreakeven for the one-sided rungs --
// rather than a second copy of any of it.
//
// It is read-only against the captures and, with -save, append-only against
// the snapshot folder: it never edits a capture and never rewrites a snapshot.

// propsBook is the book every prop row is attributed to. The ingest folder
// holds DraftKings captures and nothing else (see board_props_api.go), so the
// attribution is a fact about where the price was read, not a guess.
const propsBook = "draftkings"

// defaultPropsSeason scopes the snapshot path. The board defaults to 2026 for
// the same reason; a capture carries no season of its own, so the operator's
// current season is the only source for it.
const defaultPropsSeason = 2026

// defaultPropsDir is where -save writes, relative to the app dir: the
// git-tracked edge/props/ snapshot tree. See ADR-007 for why these are tracked
// despite being machine-generated.
const defaultPropsDir = "../props"

// marketRow is one priced outcome in the exact shape the MARKET block and
// `emit market` both consume. The JSON tags are a contract: `emit market`
// reads these rows back, and the DATA PROVENANCE block downstream counts them.
//
// FairDevig and Hold are pointers so a one-sided prop serialises them as an
// explicit null rather than a zero. A one-sided prop (an Anytime-TD price, a
// single alt-line rung) has no opposing price to measure the vig against, so a
// de-vigged fair value and a hold do not exist for it -- and a zero there would
// read as "the market thinks this never happens" / "the book takes no margin",
// both of which are false. Null says the honest thing: not computable from one
// side.
type marketRow struct {
	ID         string   `json:"id"`
	Game       string   `json:"game"`
	Market     string   `json:"market"`
	Selection  string   `json:"selection"`
	Book       string   `json:"book"`
	American   int      `json:"american"`
	ImpliedRaw float64  `json:"implied_raw"`
	FairDevig  *float64 `json:"fair_devig"`
	Breakeven  float64  `json:"breakeven"`
	Hold       *float64 `json:"hold"`

	// Unexported fields are for the human-readable report only; they are not
	// part of the serialised MARKET-block contract above.
	boostBE  *float64 // one-sided cash-lens boosted breakeven
	suspect  bool     // overround outside the plausible band (two-sided only)
	line     *float64 // the posted line, when the capture carried one
	category string   // oddspull category, for grouping the text report
}

// propsRows reads the ingest folder for one week and prices every outcome into
// a marketRow. It is the in-process entry point `emit market` calls directly;
// the CLI command below is a thin wrapper that adds flags, output and -save.
//
// gameFilter, when non-empty, keeps only outcomes whose event matches it (see
// gameMatches). The returned ingestResult carries the capture provenance --
// which files were read and the newest mtime among them -- so a caller can
// report what a price was actually observed against.
func propsRows(dir string, week int, gameFilter string) ([]marketRow, ingestResult, error) {
	ing := readIngest(dir, week)
	fair := devigPairs(ing.outcomes)

	var rows []marketRow
	for _, o := range ing.outcomes {
		if gameFilter != "" && !gameMatches(o.Event, gameFilter) {
			continue
		}
		raw, err := o.Price.ImpliedRaw()
		if err != nil {
			// A price edgectl cannot interpret is skipped, never zeroed -- the
			// same rule readIngest and oddspull already hold to.
			continue
		}
		row := marketRow{
			ID:         rowID(o, propsBook),
			Game:       o.Event,
			Market:     o.Market,
			Selection:  o.Selection,
			Book:       propsBook,
			American:   int(o.Price),
			ImpliedRaw: raw,
			Breakeven:  raw, // the hurdle rate is the raw implied prob, by definition
			category:   o.Category,
			line:       o.Line,
		}
		if d, ok := fair[outcomeKey(o)]; ok {
			f, h := d.Fair, d.Hold
			row.FairDevig = &f
			row.Hold = &h
			// The game-line board flags a two-way price whose overround falls
			// outside the plausible band as a probable transcription error; a
			// two-sided prop is the same kind of market, so it gets the same
			// flag with the same thresholds.
			if d.Overround < board.MinPlausibleOverround || d.Overround > board.MaxPlausibleOverround {
				row.suspect = true
			}
		} else if be, err := wager.BoostedBreakeven(o.Price, defaultBoostPct); err == nil {
			row.boostBE = &be
		}
		rows = append(rows, row)
	}
	return rows, ing, nil
}

// rowID is a short, stable, greppable id for one outcome.
//
// It is deterministic: a readable slug of the selection (so a human can grep
// "najee-harris" in a report and find the row) plus a six-hex digest of the
// outcome's full key and book (so two rungs of one player's ladder, or the
// same selection at a different line, never collide). The digest is derived
// from the data, so re-pricing the same capture reproduces the same id -- which
// is what lets a report cite a row by id and have the citation still resolve.
func rowID(o oddspull.Outcome, book string) string {
	src := o.Selection
	if src == "" {
		src = o.Market
	}
	return stableID(src, outcomeKey(o)+"|"+book)
}

// stableID builds a row id: a readable slug of slugSource (what a human greps
// for) plus a six-hex digest of uniqKey (what keeps distinct rows distinct and
// makes the id reproduce for the same input). Shared by props rows and the
// game-line rows `emit market` adds.
func stableID(slugSource, uniqKey string) string {
	base := slugify(slugSource)
	if base == "" {
		base = "row"
	}
	sum := sha1.Sum([]byte(uniqKey))
	return base + "-" + hex.EncodeToString(sum[:])[:6]
}

// slugify lowercases a string and collapses every run of non-alphanumerics to a
// single dash, trimming dashes off the ends. "A.J. Barner 70+" -> "a-j-barner-70".
func slugify(s string) string {
	var b strings.Builder
	dash := true // leading state: suppresses a leading dash
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// gameMatches reports whether an event matches a filter by tokens: every
// alphanumeric run in the filter must appear (case-insensitively) somewhere in
// the event. So "-game PIT CLE", "-game pit@cle" and "-game CLE" all match the
// event "PIT Steelers @ CLE Browns", without the caller having to reproduce the
// book's exact team spelling.
func gameMatches(event, filter string) bool {
	ev := strings.ToLower(event)
	toks := strings.FieldsFunc(strings.ToLower(filter), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	for _, t := range toks {
		if !strings.Contains(ev, t) {
			return false
		}
	}
	return len(toks) > 0
}

func boardPropsReport(args []string) error {
	fs := flag.NewFlagSet("board props-report", flag.ExitOnError)
	ingestDir := fs.String("ingest-dir", defaultIngestDir(),
		"folder of DraftKings captures (.har/.json) to price")
	week := fs.Int("week", 0, "week to report on (required); scopes to captures named for that week")
	game := fs.String("game", "",
		"keep only outcomes whose event matches these tokens, e.g. 'PIT CLE'")
	asJSON := fs.Bool("json", false,
		"emit one JSON row per outcome (the MARKET-block feed; fair_devig/hold are null for one-sided props)")
	save := fs.String("save", "",
		"write a git-tracked JSON snapshot under this dir (the repo's "+defaultPropsDir+"); off when empty")
	season := fs.Int("season", defaultPropsSeason, "season, for the snapshot path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}

	rows, ing, err := propsRows(*ingestDir, *week, *game)
	if err != nil {
		return err
	}

	// A snapshot is written before the report is printed, and its confirmation
	// goes to stderr so it never corrupts a -json stdout stream piped into
	// another tool.
	if *save != "" {
		path, err := saveSnapshot(*save, *season, *week, *game, rows, ing)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %d row(s) to %s\n", len(rows), path)
	}

	if *asJSON {
		if rows == nil {
			rows = []marketRow{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	printPropsReport(rows, ing, *ingestDir, *week, *game)
	return nil
}

// printPropsReport lays out the human-readable table. The arithmetic is all in
// propsRows; this only formats it, grouped by category the way the props tab
// groups it.
func printPropsReport(rows []marketRow, ing ingestResult, dir string, week int, game string) {
	fmt.Printf("PROPS REPORT  week %d — %s\n", week, propsBook)
	fmt.Printf("  %s\n", dir)
	if game != "" {
		fmt.Printf("  filtered to: %s\n", game)
	}
	fmt.Println()

	if len(rows) == 0 {
		if ing.nFiles == 0 {
			fmt.Printf("  no captures in the folder. Drop a .har/.json named for the week\n")
			fmt.Printf("  (e.g. dk_week%d_tnf.har) into %s and run this again.\n", week, dir)
			return
		}
		if len(ing.sources) == 0 {
			fmt.Printf("  found %d file(s) but none contained odds — re-check the capture.\n", ing.nFiles)
			return
		}
		fmt.Printf("  the captures carried no outcome matching the filter.\n")
		return
	}

	byCat := map[string][]marketRow{}
	for _, r := range rows {
		byCat[r.category] = append(byCat[r.category], r)
	}
	for _, c := range []string{"Passing", "Rushing", "Receiving", "Touchdown", "Game", "Other"} {
		rs := byCat[c]
		if len(rs) == 0 {
			continue
		}
		fmt.Printf("  %s\n", strings.ToUpper(c))
		fmt.Printf("  %-34s %6s %8s %8s %8s %6s\n",
			"selection", "price", "raw", "fair", "hold", "flag")
		fmt.Printf("  %s\n", strings.Repeat("-", 74))
		for _, r := range rs {
			fair, hold, flag := "—", "—", " "
			if r.FairDevig != nil {
				fair = fmt.Sprintf("%.1f%%", *r.FairDevig*100)
			}
			if r.Hold != nil {
				hold = fmt.Sprintf("%.2f%%", *r.Hold*100)
			}
			if r.suspect {
				flag = "!"
			}
			// A one-sided prop shows its boosted breakeven instead of a fair
			// value, because that is the number the cash lens actually turns on.
			sel := r.Selection
			if r.boostBE != nil {
				fair = fmt.Sprintf("be %.1f%%", *r.boostBE*100)
			}
			fmt.Printf("  %-34s %+6d %7.1f%% %8s %8s %6s\n",
				truncate(sel, 34), r.American, r.ImpliedRaw*100, fair, hold, flag)
		}
		fmt.Println()
	}
	fmt.Printf("  raw is the hurdle rate the price implies (vig included). fair is the\n")
	fmt.Printf("  de-vigged market estimate, shown only for two-sided markets; a one-sided\n")
	fmt.Printf("  prop shows its boosted breakeven (be) instead, because there is no\n")
	fmt.Printf("  opposing price to de-vig against. ! flags an implausible overround.\n")
	if len(ing.sources) > 0 {
		fmt.Println()
		fmt.Printf("  captures (newest wins):\n")
		for _, s := range ing.sources {
			fmt.Printf("    %s  %s\n", s["at"], s["name"])
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// saveSnapshot writes the priced rows as a git-tracked JSON snapshot.
//
// The file is evidence: it is what one capture said at one time, named so a
// report can cite it and a reader can find it. The timestamp in the name is the
// CAPTURE's (the newest file readIngest used), not the moment this command ran
// -- the provenance that matters is when the price was observed. See ADR-007.
func saveSnapshot(root string, season, week int, game string, rows []marketRow, ing ingestResult) (string, error) {
	ts := ing.newest
	if ts.IsZero() {
		ts = time.Now()
	}
	name := fmt.Sprintf("week%02d-%s-%s.json", week, snapshotSlug(game, rows), ts.Format("20060102-1504"))
	dir := filepath.Join(root, strconv.Itoa(season))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	if rows == nil {
		rows = []marketRow{}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	// Write through a temp file so an interrupted run cannot leave a truncated
	// snapshot that would later be cited as if whole.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// snapshotSlug names the game a snapshot is of. A -game filter is used verbatim
// (slugified); otherwise, if every row is one event, that event names it; a
// genuine multi-game capture with no filter is a "slate".
func snapshotSlug(game string, rows []marketRow) string {
	if s := slugify(game); s != "" {
		return s
	}
	events := map[string]bool{}
	for _, r := range rows {
		events[r.Game] = true
	}
	if len(events) == 1 {
		for e := range events {
			return slugify(e)
		}
	}
	return "slate"
}

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"edge/internal/board"
)

// Command `emit` serialises what edgectl already computes into the exact text
// contract a downstream consumer expects. Today it has one subcommand, `emit
// market`, which renders the MARKET block that every URPS wager report is meant
// to carry.
//
// The MARKET block has been built by hand for every scouting report so far --
// copying each price, implied probability, de-vigged fair value and hold out of
// edgectl's output into a Markdown table by hand. That is exactly the kind of
// transcription this repo moves into tested code wherever it can: the fields
// already exist (wager.EdgeReport, board.GameLine, the props rows), and a wrong
// number in a hand-copied table survives review because it looks sourced. This
// command emits the table from the same values the tool computed, so there is
// nothing left to mis-copy.
//
// WHAT THIS DELIBERATELY DOES NOT EMIT: the PROJECTIONS block.
//
// The URPS template pairs the MARKET block with a PROJECTIONS block whose
// p_true comes from "the mean of >= 3 independent free sources" of player
// projections, simulated into a win probability. Nothing in this repo sources
// external projections -- there is no consensus feed, no scraper, and
// deliberately so (the whole tool is built on the rule that it never fetches or
// invents a number it was not given). Emitting a PROJECTIONS block would mean
// either stubbing it with placeholder numbers or fabricating a consensus, and a
// fabricated projection formatted to look sourced is precisely the failure the
// ABSOLUTE CONSTRAINTS in urps-wager-engine.md exist to prevent. So the block is
// left out, not stubbed. Wiring a real multi-source projection feed is a
// separate, much larger piece of work; until it exists, the honest output is
// the market as the book posted it, and no projection.
func emitCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("emit needs a mode: 'market'")
	}
	switch args[0] {
	case "market":
		return emitMarket(args[1:])
	default:
		return fmt.Errorf("unknown emit mode %q (want 'market')", args[0])
	}
}

func emitMarket(args []string) error {
	fs := flag.NewFlagSet("emit market", flag.ExitOnError)
	week := fs.Int("week", 0, "week to emit (required)")
	game := fs.String("game", "",
		"keep only markets whose game matches these tokens, e.g. 'PIT CLE'")
	ingestDir := fs.String("ingest-dir", defaultIngestDir(),
		"folder of DraftKings captures (.har/.json) for the prop markets")
	boardDir := fs.String("board-dir", defaultBoardDir,
		"directory of week files for the game-line markets")
	bookFlag := fs.String("book", board.DefaultBook, "which book's game lines to read")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}

	// Game-line markets come from the hand-entered, git-tracked board (ADR-001);
	// prop markets come from the capture (ADR-007). Pricing both through the
	// same normalised row keeps one table able to hold moneylines and props at
	// once, and keeps the de-vig math in one place for each.
	gameRows, err := gameLineRows(*boardDir, *week, *bookFlag, *game)
	if err != nil {
		return err
	}

	propRowsAll, ing, err := propsRows(*ingestDir, *week, *game)
	if err != nil {
		return err
	}
	// The capture carries its own Game-category lines (moneyline/spread/total),
	// but those duplicate the board's hand-entered ones, which are the tracked
	// source of truth. Keep only the actual props from the capture.
	var propRows []marketRow
	for _, r := range propRowsAll {
		if r.category != "Game" {
			propRows = append(propRows, r)
		}
	}

	rows := append(gameRows, propRows...)

	asOf := "(no capture read)"
	if !ing.newest.IsZero() {
		asOf = ing.newest.Format("2006-01-02 15:04")
	}

	out := renderMarketBlock(rows, asOf)
	out += "\n" + renderProvenance(asOf, len(rows), len(propRows))
	fmt.Print(out)
	return nil
}

// gameLineRows reads one week's board for one book and emits each game's
// moneyline as two normalised rows, de-vigged through board.Devig -- the same
// path `board report` uses, not a second copy of the math.
//
// Moneyline only: Devig is the exported two-way de-vig, and it reads the
// moneyline. Spread and total sides live behind board.LinedLegs, which returns
// de-vigged legs but not the market hold the MARKET block's `hold` column
// needs, so emitting them cleanly is a later extension rather than a reach into
// the board package's internals here.
func gameLineRows(dir string, week int, book, gameFilter string) ([]marketRow, error) {
	path := filepath.Join(dir, fmt.Sprintf("week%02d.yaml", week))
	f, err := os.Open(path)
	if err != nil {
		// A missing board is not fatal to an emit: the props still carry a
		// table. Report it on stderr and continue with no game lines.
		fmt.Fprintf(os.Stderr, "emit market: no game lines (%v)\n", err)
		return nil, nil
	}
	defer f.Close()
	doc, err := board.Parse(f)
	if err != nil {
		return nil, err
	}

	var rows []marketRow
	for _, id := range doc.GameIDs() {
		g := doc.Games[id]
		matchup := g.Away + " @ " + g.Home
		if gameFilter != "" && !gameMatches(matchup, gameFilter) && !gameMatches(id, gameFilter) {
			continue
		}
		line, ok, err := board.Devig(id, g, book)
		if err != nil {
			// A malformed cell is skipped, not fatal -- one bad price must not
			// blank the whole table.
			fmt.Fprintf(os.Stderr, "emit market: %v\n", err)
			continue
		}
		if !ok {
			continue // this book has not priced this game
		}
		hold, err := line.Market.Hold()
		if err != nil {
			continue
		}
		for _, s := range []board.Side{line.Away, line.Home} {
			raw, err := s.Price.ImpliedRaw()
			if err != nil {
				continue
			}
			fair, h := s.Fair, hold
			rows = append(rows, marketRow{
				ID:         stableID(s.Team, id+"|ml|"+s.Team+"|"+book),
				Game:       matchup,
				Market:     "moneyline",
				Selection:  s.Team,
				Book:       book,
				American:   int(s.Price),
				ImpliedRaw: raw,
				FairDevig:  &fair,
				Breakeven:  raw,
				Hold:       &h,
				suspect:    line.Suspect,
				category:   "Game",
			})
		}
	}
	return rows, nil
}

// marketBlockHeader is the literal column contract from urps-wager-engine.md.
// A downstream consumer pattern-matches on this exact header line, so it is
// reproduced byte-for-byte rather than assembled from a column list -- the test
// asserts this constant against the document.
const marketBlockHeader = "| id | game | market | selection | book | american | implied_raw | fair_devig | breakeven | hold |"
const marketBlockRule = "|----|------|--------|-----------|------|----------|-------------|------------|-----------|------|"

// renderMarketBlock renders the MARKET block exactly as urps-wager-engine.md's
// INPUT CONTRACT specifies: the "## MARKET (as of ...)" heading, the fixed
// header and rule rows, then one row per market. fair_devig and hold are an em
// dash for a one-sided prop, because they do not exist without a second side to
// de-vig against -- the table says so rather than printing a misleading zero.
func renderMarketBlock(rows []marketRow, asOf string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## MARKET  (as of %s, operator-supplied)\n", asOf)
	b.WriteString(marketBlockHeader + "\n")
	b.WriteString(marketBlockRule + "\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join([]string{
			r.ID,
			r.Game,
			r.Market,
			r.Selection,
			r.Book,
			fmt.Sprintf("%+d", r.American),
			fmt.Sprintf("%.4f", r.ImpliedRaw),
			probCell(r.FairDevig),
			fmt.Sprintf("%.4f", r.Breakeven),
			probCell(r.Hold),
		}, " | ") + " |\n")
	}
	return b.String()
}

// probCell formats a probability-ish field, or an em dash when it is null.
func probCell(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%.4f", *p)
}

// renderProvenance renders the mandatory closing DATA PROVENANCE / CALIBRATION
// WARNING block. The field labels and the warning text are reproduced verbatim
// from urps-wager-engine.md; only the counts and the timestamp are filled.
//
// The timestamp is the capture's, not this command's runtime: the whole point
// of the provenance line is when the PRICE was observed, and dating it to the
// moment the table was rendered would quietly claim the numbers are fresher
// than they are. "Props with projections" is 0 of however many props are in the
// block, because no projection source is wired up (see the file's doc comment).
func renderProvenance(asOf string, markets, props int) string {
	var b strings.Builder
	b.WriteString("DATA PROVENANCE\n")
	fmt.Fprintf(&b, "  Market block timestamp : %s\n", asOf)
	fmt.Fprintf(&b, "  Markets supplied       : %d\n", markets)
	fmt.Fprintf(&b, "  Props with projections : 0 of %d  (no consensus-projection source in this repo; see `emit market` doc)\n", props)
	b.WriteString("  Wagers dropped         : 0  (emit renders the block; the four-filter audit that drops wagers is the urps skill's job)\n")
	b.WriteString("  Computation            : all EV/probability figures from edgectl; none derived in-model\n")
	b.WriteString("\n")
	b.WriteString("CALIBRATION WARNING\n")
	b.WriteString("  Every EV above is only as good as p_true. At +150 a 5-point error in p_true\n")
	b.WriteString("  swings EV by 12.5 points -- larger than most edges this process detects.\n")
	b.WriteString("  These are not forecasts.\n")
	return b.String()
}

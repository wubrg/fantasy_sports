package draft

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Lean is a personal read on a player, and it answers one of two different
// questions rather than blending them.
//
//	up / down  — conviction. "His range is better (or worse) than a median
//	             projection implies." A claim about value.
//	must       — acquisition. "I am not leaving the draft without him."
//	             Deliberately accepts paying above your own value estimate,
//	             which is the entire point of getting your guys.
//	dnd        — do not draft. Not a discount, a refusal.
//
// The distinction matters because they behave differently. A conviction
// nudge scales a number; a must-have overrides it. Measured against the
// board, Ciely's median has Ashton Jeanty at $24 while the market pays $43
// — no percentage of $24 ever produces a bid that wins him, so conviction
// alone cannot express "get him".
type Lean string

const (
	LeanMust Lean = "must"
	LeanUp   Lean = "up"
	LeanDown Lean = "down"
	LeanDND  Lean = "dnd"
	// LeanNone is an explicit absence of a read, and it exists so a lower
	// precedence set can be silenced.
	//
	// Clearing a lean the board inherited from an analyst set had nothing
	// to write: deleting a row from mine.csv that was never in mine.csv
	// changes nothing, and the read came straight back on restart. A none
	// row says "I looked at him and I have no opinion", which outranks the
	// set that does and survives a reload.
	LeanNone Lean = "none"
)

// convictionFactor is how much up/down moves your value estimate.
//
// These are intentionally modest. Conviction says the model's median
// understates a player's range, not that the model is wrong by half; when
// you disagree that strongly the honest expression is a capped must-have,
// where the number you will actually pay is stated outright.
var convictionFactor = map[Lean]float64{
	LeanUp:   1.15,
	LeanDown: 0.85,
}

// PlayerLean is one hand-entered read.
type PlayerLean struct {
	Player string
	Lean   Lean
	// Cap optionally overrides the computed recommendation for a
	// must-have. Leave it blank and the tool works out the most you can
	// pay before the rest of your roster is at risk, which moves with the
	// draft; a number you pick yourself cannot.
	Cap  int
	Note string
	// Favorite marks a player you will stretch toward a must-have price for
	// without committing budget to him. It is a tag layered on top of the
	// read above, not a read itself: a player can be up and a favorite at
	// once, and a favorite need carry no up/down/must read at all. See
	// WalkAway for what the tag does to a bid, and note MustHaves ignores it
	// on purpose — favorites are a willingness, not a plan.
	Favorite bool
	// Source names the lean set this read came from, so the board can say
	// whose opinion it is showing.
	Source string
	// Others holds what lower-precedence sets said about the same player.
	// A read nobody contradicts and a read you had to outvote are
	// different propositions, and only one of them is worth a second look
	// before you bid.
	Others []PlayerLean
}

// BidRule names which rule produced a walk-away price, so the board can say
// why a number is what it is.
type BidRule string

const (
	// RuleValue means the bid came from the model, unmodified.
	RuleValue BidRule = "value"
	// RuleConviction means an up/down lean moved it.
	RuleConviction BidRule = "conviction"
	// RuleMustHave means the cap set it — you are paying above your own
	// value estimate on purpose.
	RuleMustHave BidRule = "must-have"
	// RuleFavorite means a favorite tag stretched the bid above its base —
	// a soft lean toward a must-have's headroom, without the commitment.
	RuleFavorite BidRule = "favorite"
	// RuleRefused means do-not-draft.
	RuleRefused BidRule = "do-not-draft"
)

// Leans indexes personal reads by normalized player name.
type Leans map[string]PlayerLean

// mustHaveSwingShare is how much of a must-have's swing — the spread of his
// value across the valuation curves — becomes overpay headroom. Half: the
// swing is his plausible upside, and you meet it partway rather than paying
// the optimistic curve in full.
const mustHaveSwingShare = 0.5

// mustHaveBidBuffer is the flat dollars added on top, so a walk-away actually
// clears a competing bid rather than tying it.
const mustHaveBidBuffer = 3

// favoriteSwingShare is how much of a favorite's swing becomes stretch
// headroom — half of what a must-have gets. A favorite is a willingness to
// pay up, not a commitment to, so the lean is real but gentler.
const favoriteSwingShare = 0.25

// favoriteBidBuffer is the flat dollars a favorite adds on top of the swing
// stretch, smaller than a must-have's for the same reason.
const favoriteBidBuffer = 2

// WalkAway returns the most you will bid for a player, and which rule set
// it.
//
// value is the model's price for him. recommended is the most you can pay
// before the rest of your roster is at risk — see MaxRecommendedBid. swing is
// his BaselineSpread, the distance between the most and least generous
// valuation curve.
//
// A must-have with no explicit cap does not default to the risk ceiling: that
// priced every must-have at the same ~$49 whether he was a $45 stud or a $12
// sleeper. Instead the default flexes with his swing — value plus half his
// spread as overpay headroom, plus a buffer to win the bid — because wanting a
// specific player is an acquisition decision, and how far above his median he
// is worth chasing is exactly what the spread measures. The risk ceiling still
// caps it, so the studs land there while the mid-tier drops to a sane number.
func (l Leans) WalkAway(playerName string, value, recommended int, swing float64) (int, PlayerLean, BidRule) {
	pl, ok := l[normalizeName(playerName)]
	if !ok {
		return value, PlayerLean{}, RuleValue
	}

	switch pl.Lean {
	case LeanDND:
		// A refusal is a refusal; a favorite tag does not resurrect it.
		return 0, pl, RuleRefused
	case LeanMust:
		bid := value + int(swing*mustHaveSwingShare+0.5) + mustHaveBidBuffer
		if pl.Cap > 0 {
			// An explicit cap is your own number, replacing the swing default.
			bid = pl.Cap
		}
		if bid > recommended {
			// The risk ceiling is the hard upper bound either way.
			bid = recommended
		}
		// The ceiling binds upward only; if the model already values him
		// above it, there is no reason to bid less than he is worth.
		if value > bid {
			return value, pl, RuleValue
		}
		// A must-have already stretches past a favorite would, so the tag
		// adds nothing here.
		return bid, pl, RuleMustHave
	}

	// Base: the model's price, or a conviction-adjusted one.
	base, rule := value, RuleValue
	if factor, known := convictionFactor[pl.Lean]; known {
		base = int(float64(value)*factor + 0.5)
		if base < 1 {
			base = 1
		}
		rule = RuleConviction
	}

	// A favorite stretches the walk-away toward a must-have's headroom —
	// gentler, and without the budget commitment MustHaves tracks. Applied
	// on top of the base, capped at the risk ceiling, and only when it
	// actually raises the number.
	if pl.Favorite {
		fav := base + int(swing*favoriteSwingShare+0.5) + favoriteBidBuffer
		if fav > recommended {
			fav = recommended
		}
		if fav > base {
			return fav, pl, RuleFavorite
		}
	}
	return base, pl, rule
}

// MustHaveCost reports what the declared must-haves commit you to and what
// is left over, so "get your guys" is a budget decision rather than a
// feeling.
type MustHaveCost struct {
	Players []PlayerLean
	// Committed is the sum of the caps.
	Committed int
	// Remaining is the budget left after paying every cap.
	Remaining int
	// SlotsLeft is roster spots still open after the must-haves land.
	SlotsLeft int
}

// PerSlot is what each remaining roster spot can afford if every must-have
// is bought at its cap.
func (m MustHaveCost) PerSlot() float64 {
	if m.SlotsLeft <= 0 {
		return 0
	}
	return float64(m.Remaining) / float64(m.SlotsLeft)
}

// Overcommitted reports whether buying every must-have leaves too little to
// field a roster — under about $2 a slot there is no flexibility left.
func (m MustHaveCost) Overcommitted() bool {
	return m.SlotsLeft > 0 && m.PerSlot() < 2
}

// MustHaves totals what the declared must-haves commit you to. bids maps
// a normalized player name to the walk-away the board computed for him.
func (l Leans) MustHaves(budget, openSlots int, bids map[string]int) MustHaveCost {
	out := MustHaveCost{Remaining: budget, SlotsLeft: openSlots}
	for key, pl := range l {
		if pl.Lean != LeanMust {
			continue
		}
		cost, ok := bids[key]
		if !ok {
			cost = pl.Cap
		}
		pl.Cap = cost
		out.Players = append(out.Players, pl)
		out.Committed += cost
		out.SlotsLeft--
	}
	out.Remaining = budget - out.Committed
	sort.Slice(out.Players, func(i, j int) bool { return out.Players[i].Cap > out.Players[j].Cap })
	return out
}

// LoadLeans reads personal reads from a lean file, in whichever of the two
// formats its extension names. A missing file is not an error — no leans
// recorded just means the board runs on model prices alone.
func LoadLeans(path string) (Leans, error) {
	leans, _, err := LoadLeansFile(path)
	return leans, err
}

// LoadLeansFile is LoadLeans, also returning the players listed with no read
// yet.
//
// Anything that reads a file in order to write it back must use this rather
// than LoadLeans. The undecided names live only in the file, not in Leans,
// so a read-modify-write that drops them deletes part of the file on the
// next save without touching a read.
func LoadLeansFile(path string) (Leans, []string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Leans{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("draft: opening leans %s: %w", path, err)
	}
	defer f.Close()
	leans, undecided, err := parserFor(path)(f)
	if err != nil {
		return nil, nil, fmt.Errorf("draft: %s: %w", path, err)
	}
	return leans, undecided, nil
}

// parserFor picks a reader by extension, defaulting to the row-per-player
// CSV so an extensionless or legacy file still loads.
func parserFor(path string) func(io.Reader) (Leans, []string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, f := range leanFormats {
		if f.ext == ext {
			return f.parse
		}
	}
	return ParseLeans
}

// WriteLeans saves personal reads to path, in whichever of the two formats
// its extension names. Callers get the path from LeanSetPath rather than
// naming a file, so the format written is always the one the loader reads.
//
// Deliberately not WriteLeanSet. That one stamps the "# generated by" marker
// and refuses to overwrite a file without it — the guard that stops
// `leans -generate` eating hand-written reads. Writing the mine set through
// it would either fail or, worse, succeed and mark the file generated, at
// which point the next generation would be free to destroy it.
//
// Written to a temporary file in the same directory and renamed into place,
// so an interrupted save leaves the previous file whole rather than a
// truncated one. Same directory because rename is only atomic within a
// filesystem.
func WriteLeans(path string, leans Leans, undecided []string) error {
	rows := make([]PlayerLean, 0, len(leans))
	for _, pl := range leans {
		rows = append(rows, pl)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Player < rows[j].Player })

	// A name that has since been ruled on is no longer undecided; keeping it
	// in both places would make the file contradict itself and fail to load.
	kept := undecided[:0:0]
	for _, name := range undecided {
		if _, decided := leans[normalizeName(name)]; !decided {
			kept = append(kept, name)
		}
	}

	body, err := formatLeans(path, rows, kept)
	if err != nil {
		return err
	}
	return replaceFile(path, body)
}

// formatLeans renders the rows in the format path names.
func formatLeans(path string, rows []PlayerLean, undecided []string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		doc, err := FormatLeansYAML(rows, undecided)
		return string(doc), err
	}

	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write([]string{"player", "lean", "cap", "note"}); err != nil {
		return "", fmt.Errorf("draft: writing leans: %w", err)
	}
	for _, pl := range rows {
		cap := ""
		if pl.Cap > 0 {
			cap = strconv.Itoa(pl.Cap)
		}
		if err := w.Write([]string{pl.Player, string(pl.Lean), cap, pl.Note}); err != nil {
			return "", fmt.Errorf("draft: writing leans: %w", err)
		}
	}
	// A blank lean is how the row-per-player form says "not decided yet".
	for _, name := range undecided {
		if err := w.Write([]string{name, "", "", ""}); err != nil {
			return "", fmt.Errorf("draft: writing leans: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("draft: writing leans: %w", err)
	}
	return b.String(), nil
}

// replaceFile writes body to path via a temporary file in the same
// directory, so an interrupted save leaves the previous file whole rather
// than a truncated one.
//
// A symlink is resolved first and the target written instead. Renaming over
// the link would replace it with a real file, and a set symlinked into a
// notes vault would go on being edited there with nothing reaching the
// board — the same trap `leans -convert` refuses to set.
func replaceFile(path, body string) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	dir := filepath.Dir(path)
	// The lean directory may not exist yet on a config that predates it.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("draft: writing leans: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".leans-*")
	if err != nil {
		return fmt.Errorf("draft: writing leans: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("draft: writing leans: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("draft: writing leans: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

// ParseLeans reads personal reads from any CSV source, and also returns the
// players listed with no lean yet, in file order.
//
// Those two results are deliberately separate. A row naming a player with an
// empty lean is not a malformed row — it is you having written the name down
// before deciding, which is the normal intermediate state in a file edited a
// line at a time on a phone. Rejecting the file over it would throw away
// every finished read in the same file. Returning the undecided names rather
// than dropping them quietly keeps "not decided yet" distinguishable from
// "decided and applied", which is the distinction that makes the file
// trustworthy to edit away from the draft board.
//
// Undecided is not the same state as the "none" lean. A none row is a read —
// you looked at him and hold no opinion — and it outranks a lower-precedence
// set that does. An undecided row is the absence of one.
func ParseLeans(r io.Reader) (Leans, []string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	// Generated sets carry a provenance header saying what produced them
	// and when, which is the difference between a file you can trust to
	// regenerate and one you have to remember the origin of.
	cr.Comment = '#'

	records, err := cr.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("reading leans: %w", err)
	}
	out := Leans{}
	var undecided []string
	if len(records) == 0 {
		return out, nil, nil
	}

	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	nameCol, ok := cols["player"]
	if !ok {
		return nil, nil, fmt.Errorf("leans need a player column, have %v", records[0])
	}
	leanCol, ok := cols["lean"]
	if !ok {
		return nil, nil, fmt.Errorf("leans need a lean column, have %v", records[0])
	}
	at := func(row []string, name string) string {
		i, ok := cols[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	for n, row := range records[1:] {
		line := n + 2
		if nameCol >= len(row) {
			continue
		}
		name := strings.TrimSpace(row[nameCol])
		if name == "" {
			continue
		}
		lean := Lean(strings.ToLower(strings.TrimSpace(row[leanCol])))
		switch lean {
		case LeanMust, LeanUp, LeanDown, LeanDND, LeanNone:
		case "":
			// Named but not yet decided. Recorded, not applied — and not
			// grounds to reject the reads around it. Distinct from the none
			// lean, which is a read that silences a lower-precedence set.
			undecided = append(undecided, name)
			continue
		default:
			// A value that is present but unrecognized is still a typo, and
			// silently ignoring it would hide a read you believed you had.
			return nil, nil, fmt.Errorf("line %d: unknown lean %q for %s (use must, up, down, dnd, or none)",
				line, lean, name)
		}

		pl := PlayerLean{Player: name, Lean: lean, Note: at(row, "note")}
		// A cap is optional everywhere: without one, a must-have is priced
		// at whatever the risk math allows.
		if raw := at(row, "cap"); raw != "" {
			cap, err := strconv.Atoi(strings.TrimPrefix(raw, "$"))
			if err != nil || cap < 1 {
				return nil, nil, fmt.Errorf("line %d: bad cap %q for %s (whole dollars, at least 1)", line, raw, name)
			}
			pl.Cap = cap
		}
		out[normalizeName(name)] = pl
	}
	return out, undecided, nil
}

// Marker is a short glyph for the board, so a lean reads at a glance.
func (p PlayerLean) Marker() string {
	switch p.Lean {
	case LeanMust:
		return "MUST"
	case LeanUp:
		return "+"
	case LeanDown:
		return "-"
	case LeanDND:
		return "DND"
	}
	return ""
}

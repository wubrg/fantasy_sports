package draft

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
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
	// RuleRefused means do-not-draft.
	RuleRefused BidRule = "do-not-draft"
)

// Leans indexes personal reads by normalized player name.
type Leans map[string]PlayerLean

// WalkAway returns the most you will bid for a player, and which rule set
// it.
//
// value is the model's price for him. recommended is the most you can pay
// before the rest of your roster is at risk — see MaxRecommendedBid. A
// must-have uses that rather than the model, because wanting a specific
// player is an acquisition decision rather than a valuation one; the risk
// ceiling is what keeps it pragmatic without asking you to guess a number.
func (l Leans) WalkAway(playerName string, value, recommended int) (int, PlayerLean, BidRule) {
	pl, ok := l[normalizeName(playerName)]
	if !ok {
		return value, PlayerLean{}, RuleValue
	}

	switch pl.Lean {
	case LeanDND:
		return 0, pl, RuleRefused
	case LeanMust:
		bid := recommended
		if pl.Cap > 0 && pl.Cap < bid {
			// An explicit cap only ever tightens the recommendation.
			bid = pl.Cap
		}
		// The ceiling binds upward only; if the model already values him
		// above it, there is no reason to bid less than he is worth.
		if value > bid {
			return value, pl, RuleValue
		}
		return bid, pl, RuleMustHave
	}

	factor, known := convictionFactor[pl.Lean]
	if !known {
		return value, pl, RuleValue
	}
	adjusted := int(float64(value)*factor + 0.5)
	if adjusted < 1 {
		adjusted = 1
	}
	return adjusted, pl, RuleConviction
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

// LoadLeans reads personal reads from a CSV with the header
// player,lean,cap,note. A missing file is not an error — no leans recorded
// just means the board runs on model prices alone.
func LoadLeans(path string) (Leans, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Leans{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("draft: opening leans %s: %w", path, err)
	}
	defer f.Close()
	leans, err := ParseLeans(f)
	if err != nil {
		return nil, fmt.Errorf("draft: %s: %w", path, err)
	}
	return leans, nil
}

// ParseLeans reads personal reads from any CSV source.
func ParseLeans(r io.Reader) (Leans, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	// Generated sets carry a provenance header saying what produced them
	// and when, which is the difference between a file you can trust to
	// regenerate and one you have to remember the origin of.
	cr.Comment = '#'

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading leans: %w", err)
	}
	out := Leans{}
	if len(records) == 0 {
		return out, nil
	}

	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	nameCol, ok := cols["player"]
	if !ok {
		return nil, fmt.Errorf("leans need a player column, have %v", records[0])
	}
	leanCol, ok := cols["lean"]
	if !ok {
		return nil, fmt.Errorf("leans need a lean column, have %v", records[0])
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
		case LeanMust, LeanUp, LeanDown, LeanDND:
		default:
			return nil, fmt.Errorf("line %d: unknown lean %q for %s (use must, up, down, or dnd)",
				line, lean, name)
		}

		pl := PlayerLean{Player: name, Lean: lean, Note: at(row, "note")}
		// A cap is optional everywhere: without one, a must-have is priced
		// at whatever the risk math allows.
		if raw := at(row, "cap"); raw != "" {
			cap, err := strconv.Atoi(strings.TrimPrefix(raw, "$"))
			if err != nil || cap < 1 {
				return nil, fmt.Errorf("line %d: bad cap %q for %s (whole dollars, at least 1)", line, raw, name)
			}
			pl.Cap = cap
		}
		out[normalizeName(name)] = pl
	}
	return out, nil
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

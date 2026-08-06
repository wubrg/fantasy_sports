package draft

import (
	"fmt"
	"sort"
)

// Pivot is a named reason to change what you are doing right now.
//
// These exist because an auction gives you a few seconds to decide, and a
// board full of numbers does not tell you when the shape of the draft has
// changed. Each trigger is a single explainable rule — never a score, never
// a recommendation with hidden reasoning.
type Pivot struct {
	// Name is the short label, e.g. "SCARCITY BREAK".
	Name string
	// Reason is one line explaining what fired and why it matters.
	Reason string
	// Priority orders competing pivots; higher wins.
	Priority int
	// Position is the position the pivot concerns, empty if league-wide.
	Position string
}

// Pivot priorities. Only the highest-priority pivot is shown, because a
// wall of advice during live bidding is the same as no advice.
const (
	priorityBudgetPace     = 50
	priorityScarcityBreak  = 40
	priorityTierCliff      = 30
	priorityRB33           = 25
	priorityBarbell        = 20
	priorityDislocation    = 10
	scarcityBreakThreshold = 25.0
)

// MyState is what the draft room knows about your own team right now.
type MyState struct {
	// Budget is your remaining dollars.
	Budget int
	// OpenSlots is how many roster spots you still have to fill.
	OpenSlots int
	// StartersNeeded counts unfilled starting spots by position.
	StartersNeeded map[string]int
}

// MaxBid is the most you can spend on one player and still fill your
// roster, since every other open spot needs at least a dollar.
func (m MyState) MaxBid() int {
	if m.OpenSlots <= 0 {
		return 0
	}
	bid := m.Budget - (m.OpenSlots - 1)
	if bid < 0 {
		return 0
	}
	return bid
}

// DraftTempo describes how the room is pricing relative to our board.
type DraftTempo struct {
	// Spent and Expected are actual dollars paid versus what our cost
	// board predicted those same players would go for.
	//
	// Expected must come from the cost board, not the value board: value
	// answers what a player is worth, and comparing real prices against it
	// would report the market's permanent premium over medians as though
	// the room had suddenly turned hot.
	Spent, Expected int
	// Picks is how many sales the comparison covers.
	Picks int
}

// Ratio is how hot the room is running. Above 1 means the room is paying
// over our board; below 1 means value is going cheap.
func (t DraftTempo) Ratio() float64 {
	if t.Expected <= 0 {
		return 1
	}
	return float64(t.Spent) / float64(t.Expected)
}

// Pivots evaluates every trigger and returns them highest priority first.
//
// Callers show only the first. The rest are returned so a prep view can
// list what is close to firing.
func Pivots(players []PlayerSignals, scarcity map[string]PositionScarcity, me MyState, tempo DraftTempo) []Pivot {
	var out []Pivot

	out = append(out, budgetPacePivot(me)...)
	out = append(out, scarcityPivots(scarcity, me)...)
	out = append(out, tierCliffPivots(players, scarcity, me)...)
	out = append(out, rb33Pivot(scarcity, me)...)
	out = append(out, barbellPivots(players, me)...)
	out = append(out, dislocationPivot(tempo)...)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// budgetPacePivot fires when you can no longer afford the roster you still
// have to build. This outranks everything else because it is the only
// pivot describing a hard constraint rather than an opportunity.
func budgetPacePivot(me MyState) []Pivot {
	needed := 0
	for _, n := range me.StartersNeeded {
		needed += n
	}
	if needed == 0 || me.OpenSlots == 0 {
		return nil
	}
	// A starting spot filled at the $1 floor is a hole in the lineup, so
	// judge pace against starters rather than all open slots.
	perStarter := float64(me.Budget) / float64(needed)
	if perStarter >= 5 {
		return nil
	}
	return []Pivot{{
		Name:     "BUDGET PACE",
		Reason:   fmt.Sprintf("$%d left for %d starting spots (~$%.0f each) — stop bidding up and fill the lineup", me.Budget, needed, perStarter),
		Priority: priorityBudgetPace,
	}}
}

// scarcityPivots fire when a position you still must start is running out
// of value, using Subvertadown's cumulative scarcity.
//
// Their guide frames PS% as the tiebreak between two starting slots you
// both need: take the scarcer one. That is exactly this.
func scarcityPivots(scarcity map[string]PositionScarcity, me MyState) []Pivot {
	var out []Pivot
	for pos, need := range me.StartersNeeded {
		if need <= 0 {
			continue
		}
		s, ok := scarcity[pos]
		if !ok || s.Startable == 0 {
			continue
		}
		if s.TopScarcityPct > 0 && s.TopScarcityPct < scarcityBreakThreshold {
			out = append(out, Pivot{
				Name:     "SCARCITY BREAK",
				Position: pos,
				Reason:   fmt.Sprintf("only %.0f%% of %s value left and you still need %d — take one now or take the leftovers", s.TopScarcityPct, pos, need),
				Priority: priorityScarcityBreak,
			})
		}
	}
	return out
}

// tierCliffPivots fire when the best available player at a needed position
// is about to be followed by a steep drop — Ciely's "draft chasm".
func tierCliffPivots(players []PlayerSignals, scarcity map[string]PositionScarcity, me MyState) []Pivot {
	var out []Pivot
	for pos, need := range me.StartersNeeded {
		if need <= 0 {
			continue
		}
		s, ok := scarcity[pos]
		if !ok || s.Cliff <= 0 {
			continue
		}
		// A cliff only matters if it is large relative to what the
		// position's best remaining player is worth.
		best := bestPointsAt(players, pos)
		if best <= 0 || s.Cliff < best*0.15 {
			continue
		}
		out = append(out, Pivot{
			Name:     "TIER CLIFF",
			Position: pos,
			Reason:   fmt.Sprintf("%.0f-point drop coming at %s — the tier ends with the next one or two off the board", s.Cliff, pos),
			Priority: priorityTierCliff,
		})
	}
	return out
}

func bestPointsAt(players []PlayerSignals, pos string) float64 {
	var best float64
	for _, p := range players {
		if p.Position == pos && p.CielyPoints > best {
			best = p.CielyPoints
		}
	}
	return best
}

// cielyRBCutoff is the back whose departure closes the window: Ciely's rule
// is stated as the 33rd running back coming off the board.
const cielyRBCutoff = 33

// rb33WarningLead is how much of the window to keep in hand when warning,
// as a fraction of it.
//
// Without a lead the warning arrives exactly as the last back you can
// afford to wait for is leaving, which is too late to do anything about in
// an auction: you still have to win the bidding. A tenth of the window is
// roughly three backs of notice.
const rb33WarningLead = 0.10

// rb33Lead is the warning lead in whole backs.
func rb33Lead() int {
	lead := float64(cielyRBCutoff) * rb33WarningLead
	return int(lead + 0.5)
}

// rb33Pivot encodes Ciely's rule: lock up backs before the 33rd comes off
// the board, because after that every receiver you take while the room
// chases running backs builds an edge.
//
// The count is backs that have *gone*, which is what the rule says and is
// not the same as backs remaining. This compared 33 against the number still
// on the board, and 108 players are listed at running back while a 12-team
// draft makes 168 picks in total — so "fewer than 33 backs left" needed 75
// of them gone and could never happen. The rule never fired once.
//
// Two moments, because the rule has two halves. Before the window closes it
// is advice to act; after, it is advice to stop. The line between them is
// whether the backs left inside the window still cover what you need, which
// comes from the rule and your own roster rather than from a number picked
// here.
func rb33Pivot(scarcity map[string]PositionScarcity, me MyState) []Pivot {
	s, ok := scarcity["RB"]
	need := me.StartersNeeded["RB"]
	if !ok || need <= 0 {
		return nil
	}

	if left := cielyRBCutoff - s.Gone; left > 0 {
		// Still inside the window. Worth saying once the backs left before
		// it shuts no longer cover what you need, plus the lead time.
		if left > need+rb33Lead() {
			return nil
		}
		return []Pivot{{
			Name:     "RB33",
			Position: "RB",
			Reason: fmt.Sprintf("%s before Ciely's 33rd comes off and you still need %d — lock one up now",
				plural(left, "back"), need),
			Priority: priorityRB33,
		}}
	}

	return []Pivot{{
		Name:     "RB33",
		Position: "RB",
		Reason: fmt.Sprintf("%s gone, past Ciely's 33rd — every receiver you take while the room chases backs builds an edge",
			plural(s.Gone, "back")),
		Priority: priorityRB33,
	}}
}

// barbellPivots encode Ciely's "if I'm not first, I'm last" for QB and TE:
// either take a tier-one player or wait, because the middle is flat enough
// that paying for it buys nothing.
func barbellPivots(players []PlayerSignals, me MyState) []Pivot {
	var out []Pivot
	for _, pos := range []string{"QB", "TE"} {
		if me.StartersNeeded[pos] <= 0 {
			continue
		}
		elite := 0
		for _, p := range players {
			if p.Position == pos && p.Cost >= eliteThreshold(pos) {
				elite++
			}
		}
		if elite == 0 || elite > 2 {
			continue
		}
		out = append(out, Pivot{
			Name:     "BARBELL",
			Position: pos,
			Reason:   fmt.Sprintf("%d tier-one %s left — take one now or wait out the flat middle entirely", elite, pos),
			Priority: priorityBarbell,
		})
	}
	return out
}

// eliteThreshold is the price above which a QB or TE is a tier-one option
// rather than part of the flat middle.
func eliteThreshold(pos string) int {
	if pos == "QB" {
		return 20
	}
	return 15
}

// dislocationPivot reports when the room's actual prices have drifted from
// our board, which is the live calibration that three seasons of history
// cannot provide.
func dislocationPivot(tempo DraftTempo) []Pivot {
	// Early nominations are noisy; wait for a real sample.
	if tempo.Picks < 10 {
		return nil
	}
	r := tempo.Ratio()
	switch {
	case r >= 1.15:
		return []Pivot{{
			Name:     "ROOM IS HOT",
			Reason:   fmt.Sprintf("prices running %.0f%% over our board across %d picks — let others overpay and buy later", (r-1)*100, tempo.Picks),
			Priority: priorityDislocation,
		}}
	case r <= 0.85:
		return []Pivot{{
			Name:     "ROOM IS CHEAP",
			Reason:   fmt.Sprintf("prices running %.0f%% under our board across %d picks — spend now, the discount will not last", (1-r)*100, tempo.Picks),
			Priority: priorityDislocation,
		}}
	}
	return nil
}

// Top returns the single pivot to display, or false when nothing has fired.
func Top(pivots []Pivot) (Pivot, bool) {
	if len(pivots) == 0 {
		return Pivot{}, false
	}
	return pivots[0], true
}

// plural renders a count with its noun, so a pivot that fires on the last
// player does not read "1 backs" at the moment you are reading it fastest.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

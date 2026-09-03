package main

import (
	"net/http"
	"sort"
	"strings"

	"leaguehome/internal/draft"
)

// The arbitrage page answers a question the board cannot: how my own targets
// compete with each other.
//
// The board prices players one at a time, but my preferences make them
// mutually exclusive — one_per_offense takes the rest of an offense off my
// board the moment I own a piece of it, and no_handcuffs rules out a second
// player at the same position on the same team. So taking Gibbs costs me both
// St. Brown and LaPorta, and nothing on the board says so until they are
// already greyed out.
//
// Two views of the same fact. The groups say which targets rule each other
// out; the chain walks a best-fit lineup and reports what each pick cost.

// arbTarget is one of my targets, flattened for the page.
type arbTarget struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Team     string `json:"team"`
	Value    int    `json:"value"`
	MyMaxBid int    `json:"myMaxBid"`
	Lean     string `json:"lean,omitempty"`
	Favorite bool   `json:"favorite"`
}

// arbPair is how two targets on one offense relate.
//
// Relation is the board's own verdict, never this page's opinion: see
// pairRelation, which asks draft.BlockedForMe rather than reimplementing the
// preference rules.
type arbPair struct {
	A        string `json:"a"`
	B        string `json:"b"`
	Relation string `json:"relation"` // handcuff | one-per-offense | stack
}

// arbGroup is one contested offense.
type arbGroup struct {
	Team    string      `json:"team"`
	Targets []arbTarget `json:"targets"`
	Pairs   []arbPair   `json:"pairs"`
	// Costs names, per target, the other targets on this offense that taking
	// him would rule out. The "take McMillan and lose Brooks" line, stated
	// rather than left to be inferred from the pairs.
	Costs map[string][]string `json:"costs"`
}

// arbStep is one pick in the best-fit walk.
type arbStep struct {
	Pick arbTarget `json:"pick"`
	// Slot is the starting slot he filled, FLEX included.
	Slot string `json:"slot"`
	// Cost is every target that was available before this pick and blocked
	// after it — the price of the pick, in targets rather than dollars.
	Cost []arbTarget `json:"cost,omitempty"`
	// Spend is the running sum of MyMaxBid down the chain.
	Spend int `json:"spend"`
}

// ArbitrageView is the whole page.
type ArbitrageView struct {
	Targets int        `json:"targets"`
	Groups  []arbGroup `json:"groups"`
	Chain   []arbStep  `json:"chain"`
	// Unfilled are the starting slots the chain could not fill from targets
	// alone. Expected and not a failure: it says where my reads run out.
	Unfilled []string `json:"unfilled,omitempty"`
	// Spend is the chain's total against BudgetLeft. Reported, never solved
	// against — see the note on buildChain.
	Spend      int `json:"spend"`
	BudgetLeft int `json:"budgetLeft"`
	// Held names who the walk started from: keepers and players already won.
	Held []arbTarget `json:"held,omitempty"`
	// Inactive is set when no preference filter is on, in which case no target
	// blocks any other and the page has nothing to say.
	Inactive bool `json:"inactive,omitempty"`
}

// isTarget is the page's definition: a player I starred or read up.
//
// Deliberately not must-haves as such. Every must-have is starred today, so
// the sets coincide; the star is the broader and more stable statement of
// "keep this name in front of me".
func isTarget(p draft.PlayerSignals) bool {
	return p.Lean.Favorite || p.Lean.Lean == draft.LeanUp
}

func toArbTarget(p draft.PlayerSignals) arbTarget {
	return arbTarget{
		PlayerID: p.PlayerID, Name: p.Name, Position: p.Position, Team: p.Team,
		Value: p.Value, MyMaxBid: p.MyMaxBid,
		Lean: string(p.Lean.Lean), Favorite: p.Lean.Favorite,
	}
}

// pairRelation asks the board how owning a would treat b.
//
// Through BlockedForMe rather than by reading the preferences here. A second
// copy of these rules is the one thing that could make this page confidently
// contradict the blocked flag on the row beside it, and the rules are subtle
// enough to drift: a handcuff outranks a stack, and a running back anchors no
// stack at all.
func pairRelation(a, b draft.PlayerSignals, prefs draft.Preferences) string {
	blocked := draft.BlockedForMe([]draft.PlayerSignals{a}, []draft.PlayerSignals{b}, prefs)
	reason, off := blocked[b.PlayerID]
	if !off {
		return "stack"
	}
	if strings.Contains(reason, "handcuff") {
		return "handcuff"
	}
	return "one-per-offense"
}

// buildGroups buckets targets by offense, keeping the contested ones.
func buildGroups(targets []draft.PlayerSignals, prefs draft.Preferences) []arbGroup {
	byTeam := map[string][]draft.PlayerSignals{}
	for _, p := range targets {
		if p.Team == "" {
			continue
		}
		byTeam[p.Team] = append(byTeam[p.Team], p)
	}

	var out []arbGroup
	for team, mates := range byTeam {
		if len(mates) < 2 {
			continue
		}
		sort.Slice(mates, func(i, j int) bool {
			if mates[i].Value != mates[j].Value {
				return mates[i].Value > mates[j].Value
			}
			return mates[i].Name < mates[j].Name
		})

		g := arbGroup{Team: team, Costs: map[string][]string{}}
		for _, m := range mates {
			g.Targets = append(g.Targets, toArbTarget(m))
		}
		for i, a := range mates {
			for _, b := range mates[i+1:] {
				rel := pairRelation(a, b, prefs)
				g.Pairs = append(g.Pairs, arbPair{A: a.PlayerID, B: b.PlayerID, Relation: rel})
				if rel == "stack" {
					continue
				}
				// Exclusion is symmetric in effect even where the rule is not
				// phrased that way: whichever I take, the other is gone.
				g.Costs[a.PlayerID] = append(g.Costs[a.PlayerID], b.Name)
				g.Costs[b.PlayerID] = append(g.Costs[b.PlayerID], a.Name)
			}
		}
		out = append(out, g)
	}

	// Most expensive contest first: where the biggest name sits is where the
	// decision actually costs something.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Targets[0].Value != out[j].Targets[0].Value {
			return out[i].Targets[0].Value > out[j].Targets[0].Value
		}
		return out[i].Team < out[j].Team
	})
	return out
}

// buildChain walks a best-fit starting lineup out of targets alone.
//
// Greedy by value, which is the point rather than a shortcut: the question is
// what the obvious pick costs, and answering it with a search would produce a
// lineup nobody can trace back to a decision.
//
// Spend is carried but never enforced. A hard budget cap turns a legible chain
// into a knapsack solve, and "this line costs more than you have" is more
// useful shown than silently avoided.
func (s *server) buildChain(held, targets []draft.PlayerSignals, prefs draft.Preferences,
	baselines map[string]float64, shape draft.PoolState) ([]arbStep, []string, int) {

	owned := append([]draft.PlayerSignals(nil), held...)
	roster := &draft.Roster{}
	for _, h := range held {
		roster.Add(h, 0)
	}
	unfilled := draft.Score(roster, baselines, shape).Unfilled

	remaining := append([]draft.PlayerSignals(nil), targets...)
	var chain []arbStep
	spend := 0

	for len(unfilled) > 0 {
		blocked := draft.BlockedForMe(owned, remaining, prefs)

		// Available, best first. Ties by name so the walk is deterministic.
		var open []draft.PlayerSignals
		for _, p := range remaining {
			if _, off := blocked[p.PlayerID]; !off {
				open = append(open, p)
			}
		}
		sort.Slice(open, func(i, j int) bool {
			if open[i].Value != open[j].Value {
				return open[i].Value > open[j].Value
			}
			return open[i].Name < open[j].Name
		})

		// The best one who actually shortens the unfilled list. Fitting is
		// asked of Score rather than worked out here, so FLEX behaves the way
		// it does everywhere else.
		var pick *draft.PlayerSignals
		var slot string
		for i := range open {
			trial := &draft.Roster{Players: append([]draft.RosterSpot(nil), roster.Players...)}
			trial.Add(open[i], 0)
			after := draft.Score(trial, baselines, shape).Unfilled
			if len(after) < len(unfilled) {
				pick = &open[i]
				slot = filledSlot(unfilled, after)
				break
			}
		}
		if pick == nil {
			break
		}

		before := availableIDs(blocked, remaining)
		roster.Add(*pick, 0)
		owned = append(owned, *pick)
		remaining = without(remaining, pick.PlayerID)
		spend += pick.MyMaxBid

		afterBlocked := draft.BlockedForMe(owned, remaining, prefs)
		var cost []arbTarget
		for _, p := range remaining {
			if _, off := afterBlocked[p.PlayerID]; off && before[p.PlayerID] {
				cost = append(cost, toArbTarget(p))
			}
		}
		sort.Slice(cost, func(i, j int) bool { return cost[i].Value > cost[j].Value })

		chain = append(chain, arbStep{Pick: toArbTarget(*pick), Slot: slot, Cost: cost, Spend: spend})
		unfilled = draft.Score(roster, baselines, shape).Unfilled
	}
	return chain, unfilled, spend
}

// filledSlot names the slot a pick closed, by difference.
func filledSlot(before, after []string) string {
	left := map[string]int{}
	for _, s := range after {
		left[s]++
	}
	for _, s := range before {
		if left[s] > 0 {
			left[s]--
			continue
		}
		return s
	}
	return ""
}

func availableIDs(blocked map[string]string, pool []draft.PlayerSignals) map[string]bool {
	out := make(map[string]bool, len(pool))
	for _, p := range pool {
		if _, off := blocked[p.PlayerID]; !off {
			out[p.PlayerID] = true
		}
	}
	return out
}

func without(pool []draft.PlayerSignals, id string) []draft.PlayerSignals {
	out := pool[:0:0]
	for _, p := range pool {
		if p.PlayerID != id {
			out = append(out, p)
		}
	}
	return out
}

// handleArbitrage serves the page's data against the current board.
func (s *server) handleArbitrage(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	prefs := s.static.prefs

	var targets []draft.PlayerSignals
	for _, p := range snap.Players {
		if isTarget(p) {
			targets = append(targets, p)
		}
	}

	view := ArbitrageView{
		Targets:    len(targets),
		BudgetLeft: snap.Me.Budget,
		Inactive:   !prefs.Active(),
	}

	// What the walk starts from: keepers, then players already won. The same
	// two sources the roster panel seeds from, so the two screens cannot
	// disagree about what I hold.
	var held []draft.PlayerSignals
	for _, h := range s.static.heldRoster(s.static.ownerID) {
		held = append(held, h.Player)
	}
	for _, o := range s.ownedPicks() {
		spot := s.static.wonSpot(o.ID, o.Price)
		held = append(held, spot.Player)
	}
	for _, h := range held {
		view.Held = append(view.Held, toArbTarget(h))
	}

	if !view.Inactive {
		view.Groups = buildGroups(targets, prefs)
	}
	view.Chain, view.Unfilled, view.Spend = s.buildChain(
		held, targets, prefs, s.scoringBaselines(), s.static.shape)

	writeJSON(w, view)
}

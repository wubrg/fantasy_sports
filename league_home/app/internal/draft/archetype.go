package draft

import (
	"fmt"
	"sort"
)

// PriceFunc says what a player costs in a simulation.
//
// A seam rather than a direct read of Cost, so the later noise modes —
// random, value, overpay — drop in without touching the roster or archetype
// code that consumes them.
type PriceFunc func(PlayerSignals) int

// BoardPrice charges what the cost board says he will go for. The only mode
// today.
func BoardPrice(p PlayerSignals) int {
	if p.Cost > 0 {
		return p.Cost
	}
	return 1
}

// Archetype is a roster shape: a named constraint on what may be bought.
type Archetype struct {
	Name string
	// Why is the one-line argument for the shape.
	Why string
	// Allows reports whether adding a player at a price keeps the roster
	// inside the shape.
	Allows func(r Roster, p PlayerSignals, price int) bool
	// Satisfied reports whether a finished roster actually achieved the
	// shape, as opposed to merely never violating it. "At least three
	// backs over $25" cannot be checked player by player.
	Satisfied func(r Roster) bool
	// Seen records how often this league has actually built the shape, so
	// a threshold carries the evidence that set it rather than looking
	// like a number somebody liked.
	Seen string
	// Anchors are the expensive players a shape is built around, bought
	// before anything else.
	//
	// Needed because a per-pick veto can only forbid, never pursue, and
	// because upgrading by value per dollar never makes a large jump: one
	// $50 buy always has a worse ratio than five $10 ones. Left to itself
	// the fill covered Hero RB with a $13 back and reported the shape
	// unachievable while $130 sat there — it was not unachievable, it was
	// unattempted.
	Anchors []Anchor
}

// Anchor is a player a shape must go and buy: so many at a position, each
// costing at least this much.
type Anchor struct {
	// Position is the position required, or empty for any.
	Position string
	MinPrice int
	Count    int
}

// Archetypes are the shapes worth comparing.
//
// The thresholds are calibrated against this league's own 2023-2025 drafts
// — 36 rosters — rather than borrowed from national strategy writing, and
// `draftroom calibrate` re-derives them. Each one now describes a real,
// populated region: between 14% and 28% of rosters built each shape, where
// before Zero RB described one roster in three years and Robust RB two.
//
// They describe the space; they do not rank it. Nothing in three seasons
// separates these shapes on results — every correlation between spending
// shape and points sits under 0.2 at n=36, and the best-looking bucket is
// six rosters at p≈0.1 before correcting for having looked at five. Use
// them to see what your money can buy, not to pick a winner.
func Archetypes() []Archetype {
	spendAt := func(r Roster, pos string) int {
		total := 0
		for _, s := range r.Players {
			if pos == "" || s.Player.Position == pos {
				total += s.Price
			}
		}
		return total
	}
	countOver := func(r Roster, pos string, price int) int {
		n := 0
		for _, s := range r.Players {
			if (pos == "" || s.Player.Position == pos) && s.Price > price {
				n++
			}
		}
		return n
	}

	return []Archetype{
		{
			Name: "Stars & Scrubs",
			Why:  "three elite players and a dollar bench; wins the weeks your stars play",
			Allows: func(r Roster, p PlayerSignals, price int) bool {
				if price > 40 {
					return countOver(r, "", 40) < 3
				}
				return price <= 5
			},
			Satisfied: func(r Roster) bool { return countOver(r, "", 40) >= 2 },
			Seen:      "10 of 36 rosters, 2023-2025",
			Anchors:   []Anchor{{MinPrice: 41, Count: 2}},
		},
		{
			Name: "Balanced",
			Why:  "nobody over $35; no single injury sinks the season",
			Allows: func(r Roster, p PlayerSignals, price int) bool {
				return price <= 35
			},
			Satisfied: func(r Roster) bool { return countOver(r, "", 35) == 0 },
			Seen:      "10 of 36 rosters, 2023-2025",
		},
		{
			Name: "Hero RB",
			Why:  "one back over $40 and no second back over $20; spend the rest on receivers",
			Allows: func(r Roster, p PlayerSignals, price int) bool {
				if p.Position != "RB" {
					return true
				}
				if price > 20 {
					return price > 40 && countOver(r, "RB", 40) < 1
				}
				return true
			},
			// Both halves, because the shape is as much about the backs you
			// do not buy as the one you do. The old check counted only the
			// hero, so a roster with a $60 back and a $45 back behind him
			// passed — which is not this shape under any reading, and was
			// the opposite of what the per-pick rule enforced.
			Satisfied: func(r Roster) bool {
				return countOver(r, "RB", 40) == 1 && countOver(r, "RB", 20) == 1
			},
			Seen:    "9 of 36 rosters, 2023-2025",
			Anchors: []Anchor{{Position: "RB", MinPrice: 41, Count: 1}},
		},
		{
			Name: "Zero RB",
			// Total backfield spend rather than a cap on any one back, for
			// two reasons. It is what the strategy actually means — how much
			// of the budget went to the position — and a per-player cap
			// collided with Robust RB: three $19 backs read as both "no
			// expensive back" and "three real backs" at once.
			//
			// Both a cap and a total, because either alone lets the wrong
			// roster through. On the total alone a single $55 back with
			// nothing behind him passes, and that is Hero RB wearing this
			// name; on a per-player cap alone, three $19 backs read as both
			// this shape and Robust RB at once.
			//
			// $61 is this league's bottom quartile of backfield spend. A
			// national write-up would cap a back at $12; the cheapest
			// backfield ever assembled here still had a $31 lead back, and
			// at that line the shape described one roster in three seasons.
			Why: "no back over $35 and under $61 on backs all told — this league's cheapest quarter",
			Allows: func(r Roster, p PlayerSignals, price int) bool {
				if p.Position != "RB" {
					return true
				}
				return price <= 35 && spendAt(r, "RB")+price <= 61
			},
			Satisfied: func(r Roster) bool {
				return countOver(r, "RB", 35) == 0 && spendAt(r, "RB") <= 61
			},
			Seen: "7 of 36 rosters, 2023-2025",
		},
		{
			Name: "Robust RB",
			// $96 is this league's top quartile of backfield spend. Stated
			// as a total for the same reason Zero RB is: it is the quantity
			// the strategy is about, and it cannot collide with the cheap
			// shape the way two per-player thresholds did.
			//
			// Three backs at $32 is the cheapest way to reach it, which is
			// what the anchors go and buy.
			Why: "at least $96 on backs; Menton's league-winners come from the first four rounds",
			// An "at least" requirement cannot be expressed as a per-pick
			// veto — permitting three real backs is not the same as
			// pursuing them, and a fill that only maximizes value will
			// never bother. Requiring the first three backs to clear the
			// bar turns the goal into something each pick can enforce.
			// No per-pick veto: the shape is a floor to reach, not a ceiling
			// to stay under, and a veto can only forbid. The anchors pursue
			// it, and three backs at the anchor price clear the floor
			// exactly — so achieving the anchors is achieving the shape.
			Allows:    func(r Roster, p PlayerSignals, price int) bool { return true },
			Satisfied: func(r Roster) bool { return spendAt(r, "RB") >= 96 },
			Seen:      "9 of 36 rosters, 2023-2025",
			Anchors:   []Anchor{{Position: "RB", MinPrice: 32, Count: 3}},
		},
	}
}

// Shape is a filled archetype and how it turned out.
type Shape struct {
	Archetype Archetype
	Roster    Roster
	Metrics   RosterMetrics
	// Achieved reports whether the finished roster met the shape.
	Achieved bool
	// Possible reports whether the board could supply the shape at all,
	// which is a different claim from whether this fill found it.
	//
	// Worth separating: the fill is greedy, so "not achieved" can mean the
	// players do not exist or merely that the heuristic did not get there.
	// Reporting the second as the first would blame the board for the
	// algorithm.
	Possible bool
	// Leftover is money the fill could not spend inside the constraint.
	Leftover int
}

// FillOptions configure how a shape is built.
type FillOptions struct {
	Budget int
	// Slots is how many roster spots to fill.
	Slots int
	Price PriceFunc
	Shape PoolState
	// Baselines are the pinned scoring baselines; see ScoringBaselines.
	Baselines map[string]float64
}

// Fill builds the best roster it can inside an archetype's constraint.
//
// Two passes, because one greedy pass does not work. Maximizing points per
// dollar means never spending dollars: a $1 player barely above replacement
// scores enormously on that measure, so a single pass buys thirteen of them,
// leaves three quarters of the budget unspent, and still fails to cover the
// lineup. The passes are:
//
//  1. Cover every starting slot with the cheapest acceptable player, which
//     guarantees a legal lineup for almost nothing.
//  2. Spend what is left on whichever single upgrade buys the most points
//     above replacement per dollar, repeatedly, until nothing affordable
//     improves the roster.
//
// This is still a heuristic and not an optimum — the exact problem is a
// multi-constraint knapsack — so reports should present these as
// illustrations of a shape rather than the best possible roster of it.
//
// Two rules are absolute regardless of value: a do-not-draft player is never
// bought, and starting slots are covered before any bench spot, since a hole
// in the lineup costs more than a weak bench.
func Fill(a Archetype, available []PlayerSignals, opts FillOptions) Shape {
	if opts.Price == nil {
		opts.Price = BoardPrice
	}
	pool := make([]PlayerSignals, 0, len(available))
	for _, p := range available {
		if p.Lean.Lean != LeanDND {
			pool = append(pool, p)
		}
	}

	r := Roster{}
	budget := opts.Budget

	// Pass 0: buy the players the shape is built around.
	//
	// Anchors after the first have to be paid for too. Buying the best
	// affordable player each time spends the whole budget on the first one
	// and then reports the shape unachievable — three $26 backs cost $78 of
	// $130, which is affordable, but not if the first back takes $63.
	for _, anchor := range a.Anchors {
		for n := 0; n < anchor.Count; n++ {
			remaining := (anchor.Count - n - 1) * anchor.MinPrice
			reserve := opts.Slots - len(r.Players) - 1 + remaining
			pick := bestAnchor(a, r, pool, anchor, opts, budget-reserve)
			if pick < 0 {
				break
			}
			price := opts.Price(pool[pick])
			r.Add(pool[pick], price)
			budget -= price
		}
	}

	// Pass 1: cover whatever the lineup still lacks, as cheaply as the
	// constraint allows. Which slots those are is asked of the scorer
	// rather than tracked by hand, so anchors already covering a slot are
	// not paid for twice.
	for {
		probe := r
		probe.Players = append([]RosterSpot{}, r.Players...)
		unfilled := Score(&probe, opts.Baselines, opts.Shape).Unfilled
		if len(unfilled) == 0 || len(r.Players) >= opts.Slots {
			break
		}
		reserve := opts.Slots - len(r.Players) - 1
		pick := cheapestFor(a, r, pool, unfilled[0], opts, budget-reserve)
		if pick < 0 {
			// Try any remaining hole before giving up on the lineup.
			progressed := false
			for _, slot := range unfilled[1:] {
				if pick = cheapestFor(a, r, pool, slot, opts, budget-reserve); pick >= 0 {
					progressed = true
					break
				}
			}
			if !progressed {
				break
			}
		}
		price := opts.Price(pool[pick])
		r.Add(pool[pick], price)
		budget -= price
	}

	// Pass 2: spend the rest, best gain per dollar first.
	for {
		gain, cost, swapOut, swapIn := bestUpgrade(a, r, pool, opts, budget)
		if swapIn < 0 || gain <= 0 || cost > budget {
			break
		}
		if swapOut >= 0 {
			budget += r.Players[swapOut].Price
			r.Players = append(r.Players[:swapOut], r.Players[swapOut+1:]...)
		}
		price := opts.Price(pool[swapIn])
		r.Add(pool[swapIn], price)
		budget -= price
	}

	// Fill any bench room left with the best cheap players available.
	for len(r.Players) < opts.Slots {
		reserve := opts.Slots - len(r.Players) - 1
		pick := cheapestFor(a, r, pool, "", opts, budget-reserve)
		if pick < 0 {
			break
		}
		price := opts.Price(pool[pick])
		r.Add(pool[pick], price)
		budget -= price
	}

	metrics := Score(&r, opts.Baselines, opts.Shape)
	return Shape{
		Archetype: a,
		Roster:    r,
		Metrics:   metrics,
		Achieved:  a.Satisfied(r) && metrics.Filled(),
		Possible:  anchorsAffordable(a, pool, opts),
		Leftover:  budget,
	}
}

// anchorsAffordable reports whether the board holds enough players to meet
// every anchor within the budget, ignoring how the fill actually went.
func anchorsAffordable(a Archetype, pool []PlayerSignals, opts FillOptions) bool {
	spend := opts.Budget - (opts.Slots - 1)
	for _, anchor := range a.Anchors {
		found := 0
		cheapest := 0
		for _, p := range pool {
			if anchor.Position != "" && p.Position != anchor.Position {
				continue
			}
			if price := opts.Price(p); price >= anchor.MinPrice {
				found++
				if cheapest == 0 || price < cheapest {
					cheapest = price
				}
			}
		}
		if found < anchor.Count || cheapest*anchor.Count > spend {
			return false
		}
	}
	return true
}

// bestAnchor finds the strongest affordable player meeting an anchor. Best
// rather than cheapest, since the whole point of an anchor is to spend.
func bestAnchor(a Archetype, r Roster, pool []PlayerSignals, anchor Anchor, opts FillOptions, spend int) int {
	best, bestAbove := -1, 0.0
	for i, p := range pool {
		if r.Has(p.PlayerID) {
			continue
		}
		if anchor.Position != "" && p.Position != anchor.Position {
			continue
		}
		price := opts.Price(p)
		if price < anchor.MinPrice || price > spend || !a.Allows(r, p, price) {
			continue
		}
		if above := aboveReplacement(p, opts.Baselines); best < 0 || above > bestAbove {
			best, bestAbove = i, above
		}
	}
	return best
}

// eligible reports whether a player can fill a slot. An empty slot means
// any position will do, for bench filling.
func eligible(shape PoolState, slot, pos string) bool {
	switch slot {
	case "":
		return true
	case "FLEX":
		return isFlexEligible(shape, pos)
	}
	return slot == pos
}

// cheapestFor finds the cheapest allowed player for a slot within a budget.
func cheapestFor(a Archetype, r Roster, pool []PlayerSignals, slot string, opts FillOptions, spend int) int {
	best, bestPrice := -1, 0
	for i, p := range pool {
		if r.Has(p.PlayerID) || !eligible(opts.Shape, slot, p.Position) {
			continue
		}
		price := opts.Price(p)
		if price > spend || !a.Allows(r, p, price) {
			continue
		}
		if best < 0 || price < bestPrice {
			best, bestPrice = i, price
		}
	}
	return best
}

// bestUpgrade finds the single change that buys the most points above
// replacement per dollar: either swapping a rostered player for a better one
// at the same slot, or adding a player to an empty spot.
func bestUpgrade(a Archetype, r Roster, pool []PlayerSignals, opts FillOptions, budget int) (gain float64, cost int, swapOut, swapIn int) {
	swapOut, swapIn = -1, -1
	bestRatio := 0.0
	spare := budget

	consider := func(out int, in int, delta int, gained float64) {
		if gained <= 0 || delta > spare {
			return
		}
		d := delta
		if d < 1 {
			d = 1
		}
		if ratio := gained / float64(d); ratio > bestRatio {
			bestRatio, gain, cost, swapOut, swapIn = ratio, gained, delta, out, in
		}
	}

	for i, p := range pool {
		if r.Has(p.PlayerID) {
			continue
		}
		price := opts.Price(p)
		above := aboveReplacement(p, opts.Baselines)

		// Straight add, if there is room.
		if len(r.Players) < opts.Slots {
			reserve := opts.Slots - len(r.Players) - 1
			if price <= budget-reserve && a.Allows(r, p, price) {
				consider(-1, i, price, above)
			}
		}
		// Or an upgrade over someone already held at the same position.
		for j, held := range r.Players {
			if held.Player.Position != p.Position {
				continue
			}
			trimmed := r
			trimmed.Players = append(append([]RosterSpot{}, r.Players[:j]...), r.Players[j+1:]...)
			if !a.Allows(trimmed, p, price) {
				continue
			}
			delta := price - held.Price
			consider(j, i, delta, above-aboveReplacement(held.Player, opts.Baselines))
		}
	}
	return gain, cost, swapOut, swapIn
}

func aboveReplacement(p PlayerSignals, baselines map[string]float64) float64 {
	if above := p.CielyPoints - baselines[p.Position]; above > 0 {
		return above
	}
	return 0
}

func isFlexEligible(shape PoolState, pos string) bool {
	for _, p := range shape.FlexPositions {
		if p == pos {
			return true
		}
	}
	return false
}

// CompareShapes fills every archetype against the same board and budget.
func CompareShapes(available []PlayerSignals, opts FillOptions) []Shape {
	out := make([]Shape, 0, len(Archetypes()))
	for _, a := range Archetypes() {
		out = append(out, Fill(a, available, opts))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Metrics.POPR > out[j].Metrics.POPR })
	return out
}

// Summary is a one-line description of how a shape turned out.
func (s Shape) Summary() string {
	note := ""
	if !s.Achieved {
		note = "  (the board cannot supply this shape)"
		if s.Possible {
			note = "  (the board could supply this; the greedy fill did not find it)"
		}
	}
	return fmt.Sprintf("%.0f POPR · $%d spent · %d my guys%s",
		s.Metrics.POPR, s.Metrics.Spend, s.Metrics.MyGuys, note)
}

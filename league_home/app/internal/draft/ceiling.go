package draft

import "sort"

// StarterCandidate is one player the ceiling may reserve a slot for: what he
// costs, and whether he is good enough to start.
type StarterCandidate struct {
	PlayerID string
	Position string
	Cost     int
	Points   float64
}

// AffordableCeiling is the most you can bid on one player and still put a real
// starter in every remaining starting slot.
//
// It replaces a league-relative ceiling that answered a different question.
// That one walked down from the hard limit looking for a bid that kept your
// per-starter budget level with the room's, and once you had spent ahead of
// the room no bid qualified and it returned a dollar — true, in that no bid
// keeps pace, and useless where a ceiling goes. Worse, the figure also caps
// must-have and favourite bids, so a collapsed ceiling silently flattened
// every premium on the board. The league-relative measure still colours the
// risk band; it is only wrong as the number you read before bidding.
//
// "Real" means above replacement: PrimaryPoints at or above the position's
// scarcity threshold, the same bar as the board's own filter. A dollar is held
// for every remaining slot beyond the starters — the bench, and the defense
// this pool does not price.
//
// The reserve takes the cheapest qualifying player at each slot, so the figure
// is a bound and not a plan: it assumes those players are still there when you
// reach them, and several of them are usually the same kind of bet. Read it as
// the point past which a real starter somewhere becomes arithmetically
// impossible, not as a shopping list.
func AffordableCeiling(me MyState, pool []StarterCandidate, thresholds map[string]float64, flexPositions []string) int {
	hard := me.MaxBid()
	if hard <= 0 {
		return 0
	}

	// Cheapest first, so taking from the head of each position takes the
	// cheapest still unclaimed.
	byPos := map[string][]StarterCandidate{}
	for _, c := range pool {
		if t, ok := thresholds[c.Position]; ok && c.Points < t {
			continue // a bin player: exactly what the ceiling exists to avoid
		}
		byPos[c.Position] = append(byPos[c.Position], c)
	}
	for pos := range byPos {
		sort.Slice(byPos[pos], func(i, j int) bool {
			if byPos[pos][i].Cost != byPos[pos][j].Cost {
				return byPos[pos][i].Cost < byPos[pos][j].Cost
			}
			return byPos[pos][i].PlayerID < byPos[pos][j].PlayerID
		})
	}

	used := map[string]bool{}
	take := func(positions []string) (int, bool) {
		best, found := 0, false
		var bestID string
		for _, pos := range positions {
			for _, c := range byPos[pos] {
				if used[c.PlayerID] {
					continue
				}
				if !found || c.Cost < best {
					best, bestID, found = c.Cost, c.PlayerID, true
				}
				break // sorted, so only the cheapest unclaimed one matters
			}
		}
		if found {
			used[bestID] = true
		}
		return best, found
	}

	// Positional starters first. Each slot claims its own player: two starting
	// receivers reserve the two cheapest, never the cheapest twice.
	reserve, starting := 0, 0
	for pos, n := range me.StartersNeeded {
		for i := 0; i < n; i++ {
			starting++
			// A position with nothing startable left reserves a dollar rather
			// than nothing, so an empty shelf cannot read as a free slot.
			if cost, ok := take([]string{pos}); ok {
				reserve += cost
			} else {
				reserve++
			}
		}
	}

	// Then the flex, over the positions the roster shape allows there — so a
	// preference that keeps tight ends out of it is honoured here too.
	//
	// StartersNeeded does not track the flex and nothing decrements one, so
	// this always reserves for one. Where it is already filled the ceiling
	// comes out a few dollars low, which is the right direction to be wrong
	// in for a number labelled safe.
	if len(flexPositions) > 0 {
		starting++
		if cost, ok := take(flexPositions); ok {
			reserve += cost
		} else {
			reserve++
		}
	}

	// A dollar apiece for whatever is left: bench, and the defense.
	if rest := me.OpenSlots - starting; rest > 0 {
		reserve += rest
	}

	ceiling := me.Budget - reserve
	if ceiling > hard {
		ceiling = hard
	}
	// A dollar is always biddable while a dollar is affordable.
	//
	// Late on, the reserve swallows the budget whole and the arithmetic goes
	// to zero or below — every slot is spoken for at the minimum. That reads
	// as "do not bid", and it is wrong: the slots are still empty and the
	// minimum bid is $1, so bidding is not optional. Zero belongs only where
	// there is genuinely no dollar to spend.
	// A broke roster returned zero at the top, where hard is zero; past that
	// point a dollar is always both affordable and biddable.
	if ceiling < 1 {
		return 1
	}
	return ceiling
}

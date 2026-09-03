package draft

import "testing"

func cand(id, pos string, cost int, pts float64) StarterCandidate {
	return StarterCandidate{PlayerID: id, Position: pos, Cost: cost, Points: pts}
}

// bar puts every position's replacement level at 100 points, so a candidate
// below that is a bin player.
func bar() map[string]float64 {
	return map[string]float64{"QB": 100, "RB": 100, "WR": 100, "TE": 100}
}

// TestCeilingReservesADifferentPlayerPerSlot.
//
// Two starting receivers need the two cheapest, not the cheapest counted
// twice — reserving one player for two slots would leave a slot that can only
// be filled from the bin, which is the whole thing this figure exists to
// prevent.
func TestCeilingReservesADifferentPlayerPerSlot(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 2, StartersNeeded: map[string]int{"WR": 2}}
	pool := []StarterCandidate{
		cand("a", "WR", 5, 200),
		cand("b", "WR", 9, 200),
	}

	got := AffordableCeiling(me, pool, bar(), nil)

	// $100 less the two cheapest receivers ($5 + $9); no flex, no bench.
	if got != 86 {
		t.Errorf("ceiling $%d, want $86 — $5 and $9 reserved, not $5 twice", got)
	}
}

// A bin player is not a starter. Reserving his price would say a slot is
// covered when it is covered by someone you would not field.
func TestCeilingIgnoresPlayersBelowReplacement(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 1, StartersNeeded: map[string]int{"WR": 1}}
	pool := []StarterCandidate{
		cand("cheap", "WR", 1, 10),  // below the bar
		cand("real", "WR", 20, 200), // the cheapest one you would start
	}

	if got := AffordableCeiling(me, pool, bar(), nil); got != 80 {
		t.Errorf("ceiling $%d, want $80 — the $1 player is not a starter", got)
	}
}

// A position with nothing startable left reserves a dollar rather than
// nothing: an empty shelf must not read as a free slot.
func TestCeilingReservesADollarWhenNothingQualifies(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 1, StartersNeeded: map[string]int{"QB": 1}}
	pool := []StarterCandidate{cand("bin", "QB", 3, 10)} // below the bar

	if got := AffordableCeiling(me, pool, bar(), nil); got != 99 {
		t.Errorf("ceiling $%d, want $99 — a dollar held for the empty slot", got)
	}
}

// The flex is reserved for out of the positions the roster shape allows, so a
// preference keeping tight ends out of it is honoured here too.
func TestCeilingPricesTheFlexFromTheAllowedPositions(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 1, StartersNeeded: map[string]int{}}
	pool := []StarterCandidate{
		cand("te", "TE", 2, 200), // cheaper, but not flex-eligible for us
		cand("wr", "WR", 7, 200),
	}

	if got := AffordableCeiling(me, pool, bar(), []string{"RB", "WR"}); got != 93 {
		t.Errorf("ceiling $%d, want $93 — the $7 receiver, not the $2 tight end", got)
	}
}

// Every slot past the starters is a dollar: the bench, and the defense this
// pool does not price.
func TestCeilingHoldsADollarForEveryRemainingSlot(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 6, StartersNeeded: map[string]int{"WR": 1}}
	pool := []StarterCandidate{cand("wr", "WR", 10, 200)}

	// $10 for the receiver, $1 for the flex nobody can fill, $4 for the rest.
	if got := AffordableCeiling(me, pool, bar(), []string{"WR"}); got != 85 {
		t.Errorf("ceiling $%d, want $85", got)
	}
}

// The physical limit still binds: you cannot bid money that leaves the roster
// unfillable, however cheap the starters are.
func TestCeilingNeverExceedsTheHardMax(t *testing.T) {
	me := MyState{Budget: 100, OpenSlots: 10, StartersNeeded: map[string]int{}}
	pool := []StarterCandidate{}

	got := AffordableCeiling(me, pool, bar(), nil)
	if hard := me.MaxBid(); got > hard {
		t.Errorf("ceiling $%d exceeds the hard max $%d", got, hard)
	}
}

// An unaffordable roster floors at zero rather than going negative, which
// would render as a nonsense ceiling rather than as "you cannot".
func TestCeilingFloorsAtZero(t *testing.T) {
	me := MyState{Budget: 5, OpenSlots: 3, StartersNeeded: map[string]int{"WR": 3}}
	pool := []StarterCandidate{
		cand("a", "WR", 40, 200), cand("b", "WR", 40, 200), cand("c", "WR", 40, 200),
	}

	if got := AffordableCeiling(me, pool, bar(), nil); got != 0 {
		t.Errorf("ceiling $%d, want 0", got)
	}
}

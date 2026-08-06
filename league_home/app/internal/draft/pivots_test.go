package draft

import (
	"strings"
	"testing"
)

func healthyState() MyState {
	return MyState{
		Budget: 120, OpenSlots: 8,
		StartersNeeded: map[string]int{"RB": 1, "WR": 1},
	}
}

func roomyScarcity() map[string]PositionScarcity {
	return map[string]PositionScarcity{
		"RB": {Position: "RB", Startable: 40, StartersLeft: 20, Cover: 2, TopScarcityPct: 85},
		"WR": {Position: "WR", Startable: 60, StartersLeft: 30, Cover: 2, TopScarcityPct: 90},
		"QB": {Position: "QB", Startable: 16, StartersLeft: 8, Cover: 2, TopScarcityPct: 70},
		"TE": {Position: "TE", Startable: 16, StartersLeft: 8, Cover: 2, TopScarcityPct: 70},
	}
}

func TestMaxBidReservesADollarPerOpenSlot(t *testing.T) {
	cases := []struct {
		budget, slots, want int
	}{
		{120, 8, 113},
		{10, 10, 1},
		{1, 1, 1},
		{0, 0, 0},
		{5, 9, 0}, // broke with spots left
	}
	for _, tc := range cases {
		got := MyState{Budget: tc.budget, OpenSlots: tc.slots}.MaxBid()
		if got != tc.want {
			t.Errorf("MaxBid(%d, %d) = %d, want %d", tc.budget, tc.slots, got, tc.want)
		}
	}
}

// TestQuietWhenNothingIsWrong — the board must not cry wolf. A healthy
// mid-draft state should produce no banner at all.
func TestQuietWhenNothingIsWrong(t *testing.T) {
	got := Pivots(nil, roomyScarcity(), healthyState(), DraftTempo{})
	if _, fired := Top(got); fired {
		t.Errorf("expected silence, got %+v", got)
	}
}

func TestBudgetPaceOutranksEverything(t *testing.T) {
	me := MyState{Budget: 6, OpenSlots: 5, StartersNeeded: map[string]int{"RB": 2, "WR": 1}}
	scarce := roomyScarcity()
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 3, StartersLeft: 20, Cover: 0.15, TopScarcityPct: 5}

	pivots := Pivots(nil, scarce, me, DraftTempo{})
	top, fired := Top(pivots)
	if !fired {
		t.Fatal("expected a pivot")
	}
	if top.Name != "BUDGET PACE" {
		t.Errorf("top pivot = %q, want BUDGET PACE — a hard constraint beats an opportunity", top.Name)
	}
	if !strings.Contains(top.Reason, "$6") {
		t.Errorf("reason should name the money: %q", top.Reason)
	}
}

func TestScarcityBreakFiresOnlyForPositionsYouNeed(t *testing.T) {
	scarce := roomyScarcity()
	scarce["TE"] = PositionScarcity{Position: "TE", Startable: 4, StartersLeft: 8, Cover: 0.5, TopScarcityPct: 8}

	// TE is thin, but the lineup is already set there.
	me := MyState{Budget: 120, OpenSlots: 6, StartersNeeded: map[string]int{"RB": 1}}
	if got := Pivots(nil, scarce, me, DraftTempo{}); len(got) != 0 {
		t.Errorf("should not fire for a position already filled: %+v", got)
	}

	// Now the same board with a TE still to start.
	me.StartersNeeded["TE"] = 1
	top, fired := Top(Pivots(nil, scarce, me, DraftTempo{}))
	if !fired || top.Name != "SCARCITY BREAK" || top.Position != "TE" {
		t.Fatalf("expected a TE scarcity break, got %+v", top)
	}
}

func TestTierCliffNeedsAMeaningfulDrop(t *testing.T) {
	players := []PlayerSignals{
		{Name: "RB1", Position: "RB", CielyPoints: 300},
		{Name: "RB2", Position: "RB", CielyPoints: 295},
	}
	scarce := roomyScarcity()

	// A 5-point drop off a 300-point player is noise.
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 40, StartersLeft: 20, Cover: 2, TopScarcityPct: 85, Cliff: 5}
	if got := Pivots(players, scarce, healthyState(), DraftTempo{}); len(got) != 0 {
		t.Errorf("a trivial drop should not fire: %+v", got)
	}

	// A 60-point drop is a chasm.
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 40, StartersLeft: 20, Cover: 2, TopScarcityPct: 85, Cliff: 60}
	top, fired := Top(Pivots(players, scarce, healthyState(), DraftTempo{}))
	if !fired || top.Name != "TIER CLIFF" {
		t.Fatalf("expected a tier cliff, got %+v", top)
	}
}

// TestRB33 encodes Ciely's rule and, importantly, that it stays quiet
// while backs are plentiful.
func TestRB33(t *testing.T) {
	scarce := roomyScarcity()
	if got := Pivots(nil, scarce, healthyState(), DraftTempo{}); len(got) != 0 {
		t.Errorf("twice as many startable backs as spots should not fire RB33: %+v", got)
	}

	// Fewer backs worth starting than starting spots left to fill.
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 12, StartersLeft: 20, Cover: 0.6, TopScarcityPct: 85}
	top, fired := Top(Pivots(nil, scarce, healthyState(), DraftTempo{}))
	if !fired || top.Name != "RB33" {
		t.Fatalf("expected RB33 once cover drops below 1, got %+v", top)
	}
	if !strings.Contains(top.Reason, "12") {
		t.Errorf("reason should say how many are startable: %q", top.Reason)
	}

	// Exactly enough is not yet scarce.
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 20, StartersLeft: 20, Cover: 1, TopScarcityPct: 85}
	for _, p := range Pivots(nil, scarce, healthyState(), DraftTempo{}) {
		if p.Name == "RB33" {
			t.Errorf("cover of exactly 1 should not fire RB33: %+v", p)
		}
	}
}

// TestBarbellFiresOnTheLastEliteOption — Ciely's "if I'm not first, I'm
// last": the pivot is the moment the tier-one shelf empties.
func TestBarbellFiresOnTheLastEliteOption(t *testing.T) {
	me := MyState{Budget: 120, OpenSlots: 8, StartersNeeded: map[string]int{"QB": 1}}

	plenty := []PlayerSignals{
		{Name: "QB1", Position: "QB", Cost: 35},
		{Name: "QB2", Position: "QB", Cost: 30},
		{Name: "QB3", Position: "QB", Cost: 25},
		{Name: "QB4", Position: "QB", Cost: 22},
	}
	if got := Pivots(plenty, roomyScarcity(), me, DraftTempo{}); len(got) != 0 {
		t.Errorf("four elite QBs is not a pivot: %+v", got)
	}

	last := []PlayerSignals{
		{Name: "QB1", Position: "QB", Cost: 30},
		{Name: "QB2", Position: "QB", Cost: 8},
		{Name: "QB3", Position: "QB", Cost: 6},
	}
	top, fired := Top(Pivots(last, roomyScarcity(), me, DraftTempo{}))
	if !fired || top.Name != "BARBELL" || top.Position != "QB" {
		t.Fatalf("expected a QB barbell pivot, got %+v", top)
	}
}

func TestDislocationNeedsASample(t *testing.T) {
	hot := DraftTempo{Spent: 300, Expected: 200, Picks: 5}
	if got := dislocationPivot(hot); len(got) != 0 {
		t.Errorf("five picks is too few to call the room: %+v", got)
	}

	hot.Picks = 20
	got := dislocationPivot(hot)
	if len(got) != 1 || got[0].Name != "ROOM IS HOT" {
		t.Fatalf("expected ROOM IS HOT, got %+v", got)
	}
	if !strings.Contains(got[0].Reason, "50%") {
		t.Errorf("reason should quantify the overpay: %q", got[0].Reason)
	}

	cheap := DraftTempo{Spent: 140, Expected: 200, Picks: 20}
	if got := dislocationPivot(cheap); len(got) != 1 || got[0].Name != "ROOM IS CHEAP" {
		t.Fatalf("expected ROOM IS CHEAP, got %+v", got)
	}

	normal := DraftTempo{Spent: 205, Expected: 200, Picks: 20}
	if got := dislocationPivot(normal); len(got) != 0 {
		t.Errorf("a 2%% drift is not a dislocation: %+v", got)
	}
}

func TestTempoRatioHandlesEmptyBoard(t *testing.T) {
	if got := (DraftTempo{}).Ratio(); got != 1 {
		t.Errorf("Ratio() = %v, want 1 with nothing to compare", got)
	}
}

// TestOnlyOneBannerShows — several pivots can be live at once, and the
// board shows exactly one. A wall of advice during live bidding is the
// same as no advice.
func TestOnlyOneBannerShows(t *testing.T) {
	scarce := roomyScarcity()
	scarce["RB"] = PositionScarcity{Position: "RB", Startable: 20, StartersLeft: 20, Cover: 1, TopScarcityPct: 5, Cliff: 80}
	players := []PlayerSignals{{Name: "RB1", Position: "RB", CielyPoints: 300}}
	me := MyState{Budget: 100, OpenSlots: 6, StartersNeeded: map[string]int{"RB": 1}}

	pivots := Pivots(players, scarce, me, DraftTempo{Spent: 300, Expected: 200, Picks: 20})
	if len(pivots) < 3 {
		t.Fatalf("expected several live pivots to choose between, got %+v", pivots)
	}
	top, _ := Top(pivots)
	if top.Name != "SCARCITY BREAK" {
		t.Errorf("top = %q, want the highest-priority live pivot", top.Name)
	}
	// Sorted descending, so nothing after the first outranks it.
	for _, p := range pivots[1:] {
		if p.Priority > top.Priority {
			t.Errorf("%q outranks the displayed pivot", p.Name)
		}
	}
}

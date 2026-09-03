package main

import (
	"testing"

	"leaguehome/internal/draft"
)

func slotsOf(picks []arbPick) []string {
	out := make([]string, 0, len(picks))
	for _, p := range picks {
		out = append(out, p.Slot)
	}
	return out
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// line builds a reported line straight from picks, bypassing the search, so
// the ordering can be tested without depending on what the beam happens to
// find.
func line(srv *server, picks []arbPick) arbBestFit {
	return srv.lineOf(beamState{roster: &draft.Roster{}, picks: picks}, nil, draft.PoolState{})
}

func pick(slot, pos, name string, cost int) arbPick {
	return arbPick{Slot: slot, Pick: arbTarget{
		PlayerID: name, Name: name, Position: pos, Cost: cost, Value: cost,
	}}
}

// TestALineReadsInLineupOrder — the reported symptom.
//
// The beam adds players by descending value, so the line arrived in an order
// that looked arbitrary on screen: TE, WR, QB, WR, RB, FLEX. A lineup is read
// the way it is said out loud, and the shape is what you recognise mid-auction.
func TestALineReadsInLineupOrder(t *testing.T) {
	srv := scratchServer(t)
	got := line(srv, []arbPick{
		pick("TE", "TE", "Fannin", 8),
		pick("WR", "WR", "Burden", 14),
		pick("QB", "QB", "Hurts", 14),
		pick("WR", "WR", "Watson", 9),
		pick("RB", "RB", "Henry", 54),
		pick("FLEX", "WR", "Harrison", 8),
	})

	want := []string{"QB", "RB", "WR", "WR", "TE", "FLEX"}
	if !sameOrder(slotsOf(got.Picks), want) {
		t.Errorf("slots %v, want %v", slotsOf(got.Picks), want)
	}
}

// Two players in one slot read most expensive first, matching the board's own
// roster panel.
func TestWithinASlotTheDearerPlayerLeads(t *testing.T) {
	srv := scratchServer(t)
	got := line(srv, []arbPick{
		pick("RB", "RB", "Cheap", 8),
		pick("RB", "RB", "Dear", 54),
		pick("WR", "WR", "Receiver", 20),
	})

	if got.Picks[0].Pick.Name != "Dear" || got.Picks[1].Pick.Name != "Cheap" {
		t.Errorf("backs read %s then %s, want the dearer first",
			got.Picks[0].Pick.Name, got.Picks[1].Pick.Name)
	}
}

// FLEX is last even holding a back, because it is a lineup slot and not a
// position. Sorting on Position rather than Slot would put him with the backs.
func TestFlexReadsLastEvenHoldingABack(t *testing.T) {
	srv := scratchServer(t)
	got := line(srv, []arbPick{
		pick("FLEX", "RB", "Flex Back", 26),
		pick("RB", "RB", "Starter Back", 8),
		pick("QB", "QB", "Passer", 14),
	})

	want := []string{"QB", "RB", "FLEX"}
	if !sameOrder(slotsOf(got.Picks), want) {
		t.Errorf("slots %v, want %v", slotsOf(got.Picks), want)
	}
}

// A slot this code has not been taught about still renders, and does not lead
// the lineup ahead of the quarterback.
func TestAnUnknownSlotReadsLast(t *testing.T) {
	srv := scratchServer(t)
	got := line(srv, []arbPick{
		pick("SUPERFLEX", "RB", "Stranger", 30),
		pick("QB", "QB", "Passer", 14),
	})

	if got.Picks[0].Slot != "QB" {
		t.Errorf("line led with %q, want the quarterback", got.Picks[0].Slot)
	}
	if len(got.Picks) != 2 {
		t.Errorf("%d picks, want the unknown slot kept", len(got.Picks))
	}
}

func isOrdered(picks []arbPick) bool {
	for i := 1; i < len(picks); i++ {
		a, b := draft.SlotRank(picks[i-1].Slot), draft.SlotRank(picks[i].Slot)
		if a > b {
			return false
		}
		if a == b && picks[i-1].Pick.Cost < picks[i].Pick.Cost {
			return false
		}
	}
	return true
}

// TestEveryReportedLineIsOrdered.
//
// Best, the per-dollar line and each runner-up all pass through lineOf, which
// is the whole reason to sort there. A test that only checked the headline
// would not notice if the other two came out in beam order, and those are the
// lines being compared against it.
func TestEveryReportedLineIsOrdered(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 120)

	if !isOrdered(lines.Best.Picks) {
		t.Errorf("best line out of lineup order: %v", slotsOf(lines.Best.Picks))
	}
	if lines.PerDollar == nil {
		t.Fatal("no per-dollar line on a fixture that should produce one")
	}
	if !isOrdered(lines.PerDollar.Picks) {
		t.Errorf("per-dollar line out of lineup order: %v", slotsOf(lines.PerDollar.Picks))
	}
	if len(lines.Alternatives) == 0 {
		t.Fatal("no alternatives on a fixture that should produce them")
	}
	for i, a := range lines.Alternatives {
		if !isOrdered(a.Picks) {
			t.Errorf("alternative %d out of lineup order: %v", i, slotsOf(a.Picks))
		}
	}
}

// The chain is a sequence, and its order is the content: which pick comes off
// the board next and what taking him costs you. Sorting it would destroy the
// thing it shows.
func TestTheChainKeepsItsPickOrder(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Dear Back", "RB", "DET", 80, 40),
		costed("2", "Cheap Passer", "QB", "CIN", 20, 5),
		costed("3", "Mid Receiver", "WR", "BUF", 40, 15),
	}

	chain, _, _ := srv.buildChain(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape)

	if len(chain) < 2 {
		t.Fatalf("chain of %d steps, need at least two to check ordering", len(chain))
	}
	// Greedy by value, so the most valuable target leads regardless of slot.
	if chain[0].Pick.Name != "Dear Back" {
		t.Errorf("chain led with %q, want the most valuable target — it is not slot-sorted",
			chain[0].Pick.Name)
	}
}

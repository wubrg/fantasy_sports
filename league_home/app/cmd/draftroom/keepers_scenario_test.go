package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// keeperStatic is one team whose three rostered players carry a clean spread
// of keeper surplus, so the scenario tiers are checkable by hand:
//
//	Gibbs   aav 68, keeper $20  -> surplus 48  (lock, > lockThreshold)
//	Chase   aav 62, keeper $55  -> surplus  7  (expected, but not a lock)
//	Bowers  aav 34, keeper $30  -> surplus  4  (3rd by surplus, past the 2-cap)
//
// Expected keeps Gibbs+Chase ($75 off the pool); Locks keeps only Gibbs ($20).
func keeperStatic() *staticData {
	s := testStatic()
	for _, k := range []struct {
		id, name, pos string
		price         int
	}{
		{"1", "Jahmyr Gibbs", "RB", 20},
		{"2", "Ja'Marr Chase", "WR", 55},
		{"3", "Brock Bowers", "TE", 30},
	} {
		s.projected = append(s.projected, draft.Entry{
			PlayerID: k.id, Name: k.name, Position: k.pos, OwnerID: "me", LeaguePrice: k.price,
		})
		s.keeperOf[k.id] = k.price
	}
	return s
}

func aavOf(s *staticData) map[string]float64 {
	aav := map[string]float64{}
	for _, m := range s.market {
		aav[m.PlayerID] = m.AAV
	}
	return aav
}

func ids(entries []draft.Entry) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		out[e.PlayerID] = true
	}
	return out
}

func TestLeagueKeepersTiersAndCap(t *testing.T) {
	s := keeperStatic()
	aav := aavOf(s)

	expected := ids(leagueKeepers(s.projected, aav, 0, nil))
	if !expected["1"] || !expected["2"] {
		t.Errorf("expected should keep Gibbs and Chase, got %v", expected)
	}
	if expected["3"] {
		t.Error("Bowers is a third keeper past the 2-cap and must not be kept")
	}

	locks := ids(leagueKeepers(s.projected, aav, lockThreshold, nil))
	if !locks["1"] || len(locks) != 1 {
		t.Errorf("locks should be Gibbs alone, got %v", locks)
	}
	// Locks must be a subset of Expected — a lock is only ever a stronger read.
	for id := range locks {
		if !expected[id] {
			t.Errorf("lock %q is not in the expected set", id)
		}
	}
}

func board(t *testing.T, s *staticData, scenario string) draft.Snapshot {
	t.Helper()
	snap, err := s.Build(nil, nil, scenario)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func onBoard(snap draft.Snapshot, name string) bool {
	for _, p := range snap.Players {
		if p.Name == name {
			return true
		}
	}
	return false
}

// TestDraftNightLeavesKeepersVisible — with no scenario, keeper money leaves
// the pool (poolAfterKeepers) but the kept players stay on the board, exactly
// as before. This is the draft-night regression guard.
func TestDraftNightLeavesKeepersVisible(t *testing.T) {
	snap := board(t, keeperStatic(), "")
	if !onBoard(snap, "Jahmyr Gibbs") || !onBoard(snap, "Ja'Marr Chase") {
		t.Error("draft night must leave projected keepers on the board")
	}
	// $200 pool less the expected keepers' $20 + $55.
	if snap.Dollars != 125 {
		t.Errorf("draft-night pool = $%d, want $125 (expected keepers' money off)", snap.Dollars)
	}
	if len(snap.Kept) != 0 || snap.KeeperScenario != "" {
		t.Errorf("draft night is not a scenario: kept=%d scenario=%q", len(snap.Kept), snap.KeeperScenario)
	}
}

// TestResearchExpectedRemovesKeepersOnce — the expected scenario takes the kept
// players off the board and deducts their money exactly once.
func TestResearchExpectedRemovesKeepersOnce(t *testing.T) {
	snap := board(t, keeperStatic(), "expected")
	if onBoard(snap, "Jahmyr Gibbs") || onBoard(snap, "Ja'Marr Chase") {
		t.Error("research/expected must remove the kept players from the pool")
	}
	if !onBoard(snap, "Brock Bowers") {
		t.Error("Bowers is not kept and must remain available")
	}
	// $200 less $75, deducted once — not $200 - $75 - $75.
	if snap.Dollars != 125 {
		t.Errorf("research pool = $%d, want $125 (money deducted once, not twice)", snap.Dollars)
	}
	if len(snap.Kept) != 2 {
		t.Errorf("kept list = %d, want 2", len(snap.Kept))
	}
	if snap.KeeperScenario != "expected" {
		t.Errorf("scenario = %q, want expected", snap.KeeperScenario)
	}
}

// TestResearchLocksIsNarrower — locks removes only the near-certain keeper, so
// Chase returns to the pool and more money is in the room.
func TestResearchLocksIsNarrower(t *testing.T) {
	snap := board(t, keeperStatic(), "locks")
	if onBoard(snap, "Jahmyr Gibbs") {
		t.Error("Gibbs is a lock and must be removed")
	}
	if !onBoard(snap, "Ja'Marr Chase") {
		t.Error("Chase is not a lock and must be back on the board under locks")
	}
	if snap.Dollars != 180 {
		t.Errorf("locks pool = $%d, want $180 (only Gibbs' $20 off)", snap.Dollars)
	}
	if len(snap.Kept) != 1 || snap.Kept[0].Tier != "lock" {
		t.Errorf("kept = %+v, want one lock", snap.Kept)
	}
}

// TestResearchNoneIsTheFullPool — the none scenario assumes nobody keeps, so
// every player is available and the whole budget is in the room.
func TestResearchNoneIsTheFullPool(t *testing.T) {
	snap := board(t, keeperStatic(), "none")
	for _, name := range []string{"Jahmyr Gibbs", "Ja'Marr Chase", "Brock Bowers"} {
		if !onBoard(snap, name) {
			t.Errorf("none scenario must keep %s available", name)
		}
	}
	if snap.Dollars != 200 {
		t.Errorf("none pool = $%d, want the full $200", snap.Dollars)
	}
	if len(snap.Kept) != 0 {
		t.Errorf("none keeps nobody, got %d kept", len(snap.Kept))
	}
}

// TestForcedKeeperLockedRegardlessOfSurplus — a hand-declared keeper lock is
// kept even below the surplus a lock would normally need, and still counts
// against the two-per-team cap.
func TestForcedKeeperLockedRegardlessOfSurplus(t *testing.T) {
	s := keeperStatic()
	aav := aavOf(s)
	forced := map[string]bool{"3": true} // Bowers, surplus 4, well under lockThreshold

	locks := ids(leagueKeepers(s.projected, aav, lockThreshold, forced))
	if !locks["3"] {
		t.Error("a forced keeper must be kept even below the lock threshold")
	}
	if !locks["1"] {
		t.Error("the remaining slot should still go to the highest surplus (Gibbs)")
	}
	if locks["2"] {
		t.Error("only two keepers per team; Chase is past the cap once Bowers is forced")
	}
	if len(locks) != 2 {
		t.Errorf("want two keepers, got %v", locks)
	}
}

// TestEveryResearchScenarioCarriesABadge: the terminal board's only tell that
// its numbers are hypothetical is the RESEARCH line, which is driven off this
// map rather than off validKeeperScenario. Adding a scenario to one and not the
// other would print a research pool that reads exactly like the live board —
// the same class of silent-wrong-pool failure validKeeperScenario exists to
// prevent, just moved one step later.
func TestEveryResearchScenarioCarriesABadge(t *testing.T) {
	for name := range keeperScenarios {
		if name == "" {
			continue // draft night is the live view and gets no badge
		}
		if keeperScenarioNote[name] == "" {
			t.Errorf("scenario %q is valid but has no RESEARCH note", name)
		}
	}
	for name := range keeperScenarioNote {
		if !validKeeperScenario(name) {
			t.Errorf("note for %q, which is not a valid scenario", name)
		}
	}
	// Draft night must stay unbadged: a RESEARCH line on the live board would
	// be worse than none on a research one.
	if keeperScenarioNote[""] != "" {
		t.Error(`draft night ("") must carry no RESEARCH note`)
	}
}

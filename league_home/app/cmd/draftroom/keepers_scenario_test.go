package main

import (
	"os"
	"path/filepath"
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

	expected := ids(leagueKeepers(s.projected, aav, 0, nil, nil))
	if !expected["1"] || !expected["2"] {
		t.Errorf("expected should keep Gibbs and Chase, got %v", expected)
	}
	if expected["3"] {
		t.Error("Bowers is a third keeper past the 2-cap and must not be kept")
	}

	locks := ids(leagueKeepers(s.projected, aav, lockThreshold, nil, nil))
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

	locks := ids(leagueKeepers(s.projected, aav, lockThreshold, forced, nil))
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

// --- declared keeper sets ---------------------------------------------------
//
// Keepers lock a few days before the draft. Up to that point the board guesses
// with the surplus heuristic; after it, the file is the answer. The switch is
// per owner rather than global, because declarations arrive one team at a time
// and a half-filled file must not pretend to be either state.

// TestDeclaredOwnerIsNotToppedUp is the core of it. Our fixture team would be
// projected to keep Gibbs and Chase; declaring Gibbs alone must leave Chase in
// the auction rather than the heuristic filling the second slot back in.
func TestDeclaredOwnerIsNotToppedUp(t *testing.T) {
	s := keeperStatic()
	aav := aavOf(s)
	forced := map[string]bool{"1": true}    // Gibbs declared
	declared := map[string]bool{"me": true} // and his owner has filed

	kept := ids(leagueKeepers(s.projected, aav, 0, forced, declared))
	if !kept["1"] {
		t.Error("a declared keeper must be kept")
	}
	if kept["2"] {
		t.Error("Chase was topped up onto a declared team — the heuristic must not guess past a filed list")
	}
	if len(kept) != 1 {
		t.Errorf("declared team keeps exactly what it filed, got %v", kept)
	}
}

// TestUndeclaredOwnerIsStillProjected: the same call with nobody declared has
// to behave exactly as it did before any of this existed. An owner missing
// from the file has not decided, which is not the same as keeping nobody.
func TestUndeclaredOwnerIsStillProjected(t *testing.T) {
	s := keeperStatic()
	aav := aavOf(s)
	kept := ids(leagueKeepers(s.projected, aav, 0, map[string]bool{"1": true}, nil))
	if !kept["1"] || !kept["2"] {
		t.Errorf("an undeclared team is still projected up to the cap, got %v", kept)
	}
}

// TestDeclaredOwnerKeepingNobody: a team that files an empty list keeps nobody
// and its whole budget stays in the pool. Without this the only way to say
// "I'm keeping none" would be silence, which means the opposite.
func TestDeclaredOwnerKeepingNobody(t *testing.T) {
	s := keeperStatic()
	aav := aavOf(s)
	kept := ids(leagueKeepers(s.projected, aav, 0, nil, map[string]bool{"me": true}))
	if len(kept) != 0 {
		t.Errorf("a team declaring no keepers keeps nobody, got %v", kept)
	}
}

// TestNoneRowNeedsAnOwner: a `none` row carries no player, so the owner column
// is the only thing identifying the roster. Accepting it blank would silently
// record a declaration against nobody, and the team would stay projected while
// looking like it had filed.
func TestNoneRowNeedsAnOwner(t *testing.T) {
	dir := t.TempDir()
	writeKeeperLocks(t, dir, "owner,player\n,none\n")
	if _, err := loadKeeperLocks(dir); err == nil {
		t.Fatal("a none row with no owner must be refused")
	}
}

// TestNoneRowIsADeclaration: the sentinel spellings all mean the same thing,
// and a normal row is untouched by them.
func TestNoneRowIsADeclaration(t *testing.T) {
	dir := t.TempDir()
	writeKeeperLocks(t, dir, "owner,player\nSam,none\nBob,-\nGreg,NOBODY\nAdam,Jahmyr Gibbs\n")
	locks, err := loadKeeperLocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 4 {
		t.Fatalf("locks = %d, want 4", len(locks))
	}
	for _, lk := range locks[:3] {
		if !lk.Declared() {
			t.Errorf("%q should be a keeps-nobody declaration", lk.Owner)
		}
	}
	if locks[3].Declared() || locks[3].Player != "Jahmyr Gibbs" {
		t.Errorf("a normal row must survive unchanged, got %+v", locks[3])
	}
}

// writeKeeperLocks puts a keeper-locks.csv in dir for the loader to read.
func writeKeeperLocks(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, keeperLocksFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDraftNightRemovesDeclaredKeepers is the other half of
// TestDraftNightLeavesKeepersVisible, and the reason the two can coexist: a
// projection is a guess and stays biddable, a declaration is a fact and does
// not. Before keepers lock the board shows everyone; after his owner files,
// that player is gone from the live board, not only from a research scenario.
func TestDraftNightRemovesDeclaredKeepers(t *testing.T) {
	s := keeperStatic()
	s.forcedKeepers = map[string]bool{"1": true} // Gibbs declared
	s.declaredOwners = map[string]bool{"me": true}

	snap := board(t, s, "")
	if onBoard(snap, "Jahmyr Gibbs") {
		t.Error("a declared keeper must leave the live board — he cannot be drafted")
	}
	if !onBoard(snap, "Ja'Marr Chase") {
		t.Error("his team-mate was not declared and must stay biddable")
	}
	// $200 less Gibbs' $20 alone: declaring one keeper stops the heuristic
	// guessing a second, so Chase's $55 stays in the room.
	if snap.Dollars != 180 {
		t.Errorf("pool = $%d, want $180 (only the declared keeper's money out)", snap.Dollars)
	}
}

package main

import (
	"testing"
)

// TestAMockHasNoKeepers.
//
// A *league* mock — what Sleeper gives you when you mock from the league page —
// resolves owners.csv, rulings.csv and keeper-locks.csv exactly as the real
// draft does, while Sleeper itself hands all twelve teams a flat budget and
// leaves the whole pool biddable. Every keeper this board knows about is
// therefore a fiction during a rehearsal, and an expensive one: it deducts
// money nobody has spent and takes players off a board where they can be
// bought.
//
// This had already been fixed three times in three places — the pool the board
// opens on, the -keepers flag, and the Draft night button, which quietly
// overrode that flag. Emptying the inputs is the fix that does not need a
// fourth.
func TestAMockHasNoKeepers(t *testing.T) {
	s := keeperStatic()
	s.ownerID = "me"

	// Draft night on the real league: keeper money comes off the pool and off
	// your budget. This is the behaviour that must survive the change.
	real, err := s.Build(map[string]gone{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if real.Me.Budget >= s.budget {
		t.Fatalf("the real board stopped deducting keeper money: $%d of $%d",
			real.Me.Budget, s.budget)
	}
	if len(s.heldRoster("me")) == 0 {
		t.Fatal("the real board stopped seating keepers on your roster")
	}

	// The same board following a mock has none of it.
	s.forgetKeepers()

	mock, err := s.Build(map[string]gone{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if mock.Me.Budget != s.budget {
		t.Errorf("budget on a mock = $%d, want the full $%d: money was deducted "+
			"for keepers the mock does not have", mock.Me.Budget, s.budget)
	}
	if got := len(s.heldRoster("me")); got != 0 {
		t.Errorf("heldRoster seated %d keepers on a mock roster, want 0", got)
	}
	if mock.Dollars <= real.Dollars {
		t.Errorf("pool on a mock = $%d, want more than the keeper-deducted $%d",
			mock.Dollars, real.Dollars)
	}
	if mock.Me.OpenSlots <= real.Me.OpenSlots {
		t.Errorf("open slots on a mock = %d, want more than the %d a keeper "+
			"roster leaves", mock.Me.OpenSlots, real.Me.OpenSlots)
	}
}

// A declared keeper is biddable on a mock, which is the whole point: the
// rehearsal has to price the pool Sleeper is actually running.
func TestADeclaredKeeperIsBiddableOnAMock(t *testing.T) {
	s := keeperStatic()
	s.ownerID = "me"
	s.forcedKeepers = map[string]bool{"1": true}
	s.declaredOwners = map[string]bool{"me": true}

	real, err := s.Build(map[string]gone{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if onBoard(real, "Jahmyr Gibbs") {
		t.Fatal("a declared keeper should be off the real board")
	}

	s.forgetKeepers()
	mock, err := s.Build(map[string]gone{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !onBoard(mock, "Jahmyr Gibbs") {
		t.Error("a declared keeper is still off the board on a mock, where he is biddable")
	}
}

package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// keepGibbs makes Jahmyr Gibbs a keeper of mine, the way the keeper file does.
func keepGibbs(srv *server) {
	srv.static.projected = []draft.Entry{{
		Season: "2026", OwnerID: srv.static.ownerID, PlayerID: "1",
		Name: "Jahmyr Gibbs", Position: "RB", LeaguePrice: 35, KeepCount: 1,
	}}
	srv.static.forcedKeepers = map[string]bool{"1": true}
	srv.static.declaredOwners = map[string]bool{srv.static.ownerID: true}
}

// TestAKeeperEnteredAsAPickAppearsOnce — the reported bug.
//
// Once the league entered keepers into Sleeper they came back through the
// picks feed as well, and the panel added each of them twice: once from the
// keeper file and once as a player won. That is not only a doubled row. Score
// then read two De'Von Achanes as two filled running back slots, so the panel
// reported RB done while the board correctly still wanted one, and spend,
// counts and POPR all doubled with it.
func TestAKeeperEnteredAsAPickAppearsOnce(t *testing.T) {
	srv := scratchServer(t)
	// Gibbs is a keeper of mine...
	keepGibbs(srv)
	// ...and the league has now entered him as a draft pick.
	srv.mu.Lock()
	srv.taken["1"] = gone{price: 35, mine: true}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())

	seen := map[string]int{}
	for _, s := range append(append([]ScratchSpot(nil), view.Starters...), view.Bench...) {
		seen[s.Name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times on the roster panel, want once", name, n)
		}
	}
	if got := view.Metrics.Counts["RB"]; got > 1 {
		t.Errorf("panel counts %d running backs from one keeper", got)
	}
}

// And he reads as kept, because that is what he is. The feed reporting him as
// a pick does not turn a keeper into a player you bought at auction.
func TestAKeeperEnteredAsAPickStillReadsAsKept(t *testing.T) {
	srv := scratchServer(t)
	keepGibbs(srv)
	srv.mu.Lock()
	srv.taken["1"] = gone{price: 35, mine: true}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())

	var found bool
	for _, s := range append(append([]ScratchSpot(nil), view.Starters...), view.Bench...) {
		if s.Name != "Jahmyr Gibbs" {
			continue
		}
		found = true
		if s.Won {
			t.Error("a keeper is labelled won")
		}
		if !s.Kept {
			t.Error("a keeper is not labelled kept")
		}
	}
	if !found {
		t.Fatal("the keeper is not on the panel at all")
	}
}

// A player actually bought at auction is still won, not kept — the fix must
// not swallow real purchases into the keeper set.
func TestAPlayerBoughtAtAuctionStillReadsAsWon(t *testing.T) {
	srv := scratchServer(t)
	srv.mu.Lock()
	srv.taken["2"] = gone{price: 62, mine: true}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())

	var found bool
	for _, s := range append(append([]ScratchSpot(nil), view.Starters...), view.Bench...) {
		if s.Name != "Ja'Marr Chase" {
			continue
		}
		found = true
		if !s.Won {
			t.Error("a player bought at auction is not labelled won")
		}
		if s.Kept {
			t.Error("a player bought at auction is labelled kept")
		}
		if s.Price != 62 {
			t.Errorf("price $%d, want the $62 paid", s.Price)
		}
	}
	if !found {
		t.Fatal("the purchase is not on the panel")
	}
}

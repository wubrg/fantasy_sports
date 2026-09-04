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

// TestTheArbitragePageHoldsEachKeeperOnce.
//
// The roster panel and the arbitrage page ask the same question and used to
// answer it with their own copy of the same two-source walk. Fixing the panel
// alone left the arbitrage page holding each keeper twice, which read as extra
// filled slots — so best fit solved for four slots where six were open and the
// lineup came up short.
func TestTheArbitragePageHoldsEachKeeperOnce(t *testing.T) {
	srv := scratchServer(t)
	keepGibbs(srv)
	srv.mu.Lock()
	srv.taken["1"] = gone{price: 35, mine: true}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	view := srv.arbitrageView(defaultBestFitPct)

	seen := map[string]int{}
	for _, h := range view.Held {
		seen[h.Name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("arbitrage holds %s %d times, want once", name, n)
		}
	}
}

// Both screens answer from the same list, so they cannot disagree about what
// I hold — which is what the comment above the old duplicate claimed and the
// code did not deliver.
func TestBothScreensAgreeOnWhatIHold(t *testing.T) {
	srv := scratchServer(t)
	keepGibbs(srv)
	srv.mu.Lock()
	srv.taken["1"] = gone{price: 35, mine: true}
	srv.taken["2"] = gone{price: 62, mine: true}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	panelNames := map[string]bool{}
	pv := srv.scratchView(srv.snapshot())
	for _, s := range append(append([]ScratchSpot(nil), pv.Starters...), pv.Bench...) {
		panelNames[s.Name] = true
	}
	arbNames := map[string]bool{}
	for _, h := range srv.arbitrageView(defaultBestFitPct).Held {
		arbNames[h.Name] = true
	}

	if len(panelNames) != len(arbNames) {
		t.Errorf("panel holds %v, arbitrage holds %v", panelNames, arbNames)
	}
	for n := range panelNames {
		if !arbNames[n] {
			t.Errorf("%s is on the roster panel but not held by arbitrage", n)
		}
	}
}

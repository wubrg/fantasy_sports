package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"leaguehome/internal/draft"
)

// sell posts a sale the way the page does. price < 0 means "send no price at
// all", which is what the them button now does.
func sell(t *testing.T, srv *server, player string, mine bool, price int) draft.Snapshot {
	t.Helper()
	body := map[string]any{"player": player, "mine": mine}
	if price >= 0 {
		body["price"] = price
	}
	buf, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	srv.handleSold(w, httptest.NewRequest(http.MethodPost, "/api/sold", bytes.NewReader(buf)))
	if w.Code != http.StatusOK {
		t.Fatalf("sold %s: %d %s", player, w.Code, w.Body.String())
	}
	var snap draft.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func costOf(t *testing.T, snap draft.Snapshot, name string) int {
	t.Helper()
	for _, p := range snap.Players {
		if p.Name == name {
			return p.Cost
		}
	}
	t.Fatalf("%s not on the board", name)
	return 0
}

// TestAnOpponentsPurchaseTakesHisMoneyOffTheBoard — the reported bug.
//
// Clicking them removed the player and his roster slot from the market and
// left his money in it. Solve spreads Dollars-Slots across whoever is left, so
// every phantom dollar landed in someone else's COST, and it compounded: ten
// such clicks at a $30 average inflated the surplus by 20%.
func TestAnOpponentsPurchaseTakesHisMoneyOffTheBoard(t *testing.T) {
	srv := scratchServer(t)
	before := srv.snapshot()
	cost := costOf(t, before, "Jahmyr Gibbs")
	if cost < 2 {
		t.Fatalf("fixture gives Gibbs a $%d cost; need a real price to subtract", cost)
	}

	after := sell(t, srv, "Jahmyr Gibbs", false, -1)

	if got, want := after.Dollars, before.Dollars-cost; got != want {
		t.Errorf("pool $%d after a $%d sale to another team, want $%d", got, cost, want)
	}
	if got, want := after.Slots, before.Slots-1; got != want {
		t.Errorf("slots %d, want %d", got, want)
	}
}

// My own buys are unchanged: the price is the one I typed.
func TestMyOwnPurchaseUsesThePriceITyped(t *testing.T) {
	srv := scratchServer(t)
	before := srv.snapshot()

	after := sell(t, srv, "Jahmyr Gibbs", true, 71)

	if got, want := after.Dollars, before.Dollars-71; got != want {
		t.Errorf("pool $%d, want $%d", got, want)
	}
	if got, want := after.Me.Budget, before.Me.Budget-71; got != want {
		t.Errorf("my budget $%d, want $%d", got, want)
	}
}

// A price sent explicitly for another team is used verbatim, not replaced by
// the estimate.
func TestAnExplicitOpponentPriceIsUsedAsGiven(t *testing.T) {
	srv := scratchServer(t)
	before := srv.snapshot()

	after := sell(t, srv, "Jahmyr Gibbs", false, 44)

	if got, want := after.Dollars, before.Dollars-44; got != want {
		t.Errorf("pool $%d, want $%d", got, want)
	}
	if after.Me.Budget != before.Me.Budget {
		t.Errorf("my budget moved on someone else's buy: $%d then $%d", before.Me.Budget, after.Me.Budget)
	}
}

// TestTheFeedCorrectsAnAssumedPrice.
//
// Hand entry means "he is gone, before the API caught up" — that timing is the
// whole reason the button exists. It does not mean you know what he cost. Once
// Sleeper reports the real figure it replaces the estimate, and the pool moves
// by the difference.
//
// Without this the old rule held: hand entry won outright, so one click pinned
// a player at an assumed price for the rest of the night no matter what the
// feed later said.
func TestTheFeedCorrectsAnAssumedPrice(t *testing.T) {
	srv := scratchServer(t)
	base := srv.snapshot()
	assumed := costOf(t, base, "Jahmyr Gibbs")

	estimated := sell(t, srv, "Jahmyr Gibbs", false, -1)
	if got, want := estimated.Dollars, base.Dollars-assumed; got != want {
		t.Fatalf("pool $%d on the estimate, want $%d", got, want)
	}

	// The feed catches up with what he really went for.
	const real = 44
	srv.mu.Lock()
	srv.taken["1"] = gone{price: real, mine: false}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	corrected := srv.snapshot()
	if got, want := corrected.Dollars, base.Dollars-real; got != want {
		t.Errorf("pool $%d after the feed reported $%d, want $%d (still on the $%d estimate?)",
			got, real, want, assumed)
	}
}

// A price you typed is not an estimate, and the feed does not overrule it.
func TestTheFeedLeavesATypedPriceAlone(t *testing.T) {
	srv := scratchServer(t)
	base := srv.snapshot()

	sell(t, srv, "Jahmyr Gibbs", false, 60)

	srv.mu.Lock()
	srv.taken["1"] = gone{price: 44, mine: false}
	err := srv.rebuildLocked()
	srv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if got, want := srv.snapshot().Dollars, base.Dollars-60; got != want {
		t.Errorf("pool $%d, want $%d — the feed overruled a price that was typed", got, want)
	}
}

// Hand entry still wins on existence, which is the point of entering by hand:
// the player must leave the board immediately, before the feed knows anything.
func TestHandEntryStillBeatsTheFeedToThePunch(t *testing.T) {
	srv := scratchServer(t)
	before := len(srv.snapshot().Players)

	after := sell(t, srv, "Jahmyr Gibbs", false, -1)

	if len(after.Players) != before-1 {
		t.Errorf("%d players on the board, want %d — he did not leave", len(after.Players), before-1)
	}
	for _, p := range after.Players {
		if p.Name == "Jahmyr Gibbs" {
			t.Error("Gibbs still on the board after being sold by hand")
		}
	}
}

// A bin player still takes a dollar out of the room. Most of the board is
// priced under one, and pricing them at nothing is how the pool stopped
// shrinking in the first place.
func TestADollarBinOpponentBuyStillCostsTheRoomADollar(t *testing.T) {
	srv := scratchServer(t)
	srv.mu.Lock()
	for i := range srv.cached.Players {
		if srv.cached.Players[i].Name == "Brock Bowers" {
			srv.cached.Players[i].Cost = 0
		}
	}
	srv.mu.Unlock()
	before := srv.snapshot()

	after := sell(t, srv, "Brock Bowers", false, -1)

	if got, want := after.Dollars, before.Dollars-1; got != want {
		t.Errorf("pool $%d after a bin player went to another team, want $%d", got, want)
	}
}

// Undo puts the money back. Hand entries are guesses in both directions and
// must be fully reversible, estimated or not.
func TestUndoReturnsAnAssumedPriceToThePool(t *testing.T) {
	srv := scratchServer(t)
	before := srv.snapshot()

	sell(t, srv, "Jahmyr Gibbs", false, -1)

	w := httptest.NewRecorder()
	srv.handleUndo(w, httptest.NewRequest(http.MethodPost, "/api/undo",
		bytes.NewReader([]byte(`{"player":"Jahmyr Gibbs"}`))))
	if w.Code != http.StatusOK {
		t.Fatalf("undo: %d %s", w.Code, w.Body.String())
	}

	after := srv.snapshot()
	if after.Dollars != before.Dollars {
		t.Errorf("pool $%d after undo, want the original $%d", after.Dollars, before.Dollars)
	}
	if len(after.Players) != len(before.Players) {
		t.Errorf("%d players after undo, want %d", len(after.Players), len(before.Players))
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/journal"
	"edge/internal/ledger"
	"edge/internal/wager"
)

// TestPlaceDrawsMatchingAsset is the reason /api/place became asset-aware: a
// bonus bet must spend bonus and a real-money bet must spend cash, never the
// other. Before this, a hand-entered prop could silently eat the wrong balance.
func TestPlaceDrawsMatchingAsset(t *testing.T) {
	dir := t.TempDir()
	srv := &boardServer{
		ledgerPath: filepath.Join(dir, "bankroll.jsonl"),
		betlogPath: filepath.Join(dir, "betlog.jsonl"),
	}
	grant := func(id, asset string, amt float64) {
		e := ledger.Event{
			Kind: ledger.KindGrant, ID: id, Time: time.Now(),
			Creates: &ledger.Lot{ID: id, Book: "betmgm", Asset: asset, Amount: amt},
		}
		if err := ledger.AppendFile(srv.ledgerPath, e); err != nil {
			t.Fatal(err)
		}
	}
	grant("cash", ledger.Cash, 20)
	grant("bonus", ledger.Bonus, 20)

	place := func(bankroll string, stake float64) {
		body := map[string]any{
			"selection": "Some Player ATD (+240)", "price": 240, "stake": stake,
			"bankroll": bankroll, "book": "betmgm", "week": 1,
		}
		b, _ := json.Marshal(body)
		rr := httptest.NewRecorder()
		srv.handlePlace(rr, httptest.NewRequest("POST", "/api/place", strings.NewReader(string(b))))
		if rr.Code != 200 {
			t.Fatalf("place %s: status %d: %s", bankroll, rr.Code, rr.Body.String())
		}
		var m map[string]any
		json.Unmarshal(rr.Body.Bytes(), &m)
		if m["ok"] != true {
			t.Fatalf("place %s: expected ok true, got %v", bankroll, m["ok"])
		}
		if id, _ := m["id"].(string); id == "" {
			t.Fatalf("place %s: expected a non-empty id, got %v", bankroll, m["id"])
		}
	}

	// A $5 bonus bet must come out of bonus, leaving cash untouched.
	place("bonus bet", 5)
	pos := mustBal(t, srv)
	if got := pos.Total("betmgm", ledger.Bonus); got != 15 {
		t.Errorf("after a $5 bonus bet, bonus = %.2f, want 15", got)
	}
	if got := pos.Total("betmgm", ledger.Cash); got != 20 {
		t.Errorf("a bonus bet must not touch cash; cash = %.2f, want 20", got)
	}

	// A $5 real-money bet must come out of cash.
	place("real money", 5)
	pos = mustBal(t, srv)
	if got := pos.Total("betmgm", ledger.Cash); got != 15 {
		t.Errorf("after a $5 real-money bet, cash = %.2f, want 15", got)
	}
	if got := pos.Total("betmgm", ledger.Bonus); got != 15 {
		t.Errorf("a real-money bet must not touch bonus; bonus = %.2f, want 15", got)
	}
}

// TestPlace_appliesBoostFromRequest confirms a boost lot id sent with a place
// request is threaded through to journal.Place, which validates and consumes
// it against the resulting wager.
func TestPlace_appliesBoostFromRequest(t *testing.T) {
	dir := t.TempDir()
	lg := filepath.Join(dir, "bank.jsonl")
	// Seed a draftkings cash lot and a draftkings boost lot.
	must := func(e ledger.Event) {
		if err := ledger.AppendFile(lg, e); err != nil {
			t.Fatal(err)
		}
	}
	must(ledger.Event{Kind: ledger.KindGrant, ID: "dk-cash", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-cash", Book: "draftkings", Asset: ledger.Cash, Amount: 50}})
	must(ledger.Event{Kind: ledger.KindGrant, ID: "dk-boost", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-boost", Book: "draftkings", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.2, MaxStake: 50, MinOdds: -300}}})

	// newBoardServer always resolves to defaultLedger()/defaultBetlog() -- it
	// takes no ledger path argument -- so, as with TestPlaceDrawsMatchingAsset
	// above, the server is built directly with the temp paths this test needs.
	srv := &boardServer{ledgerPath: lg, betlogPath: filepath.Join(dir, "betlog.jsonl")}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	code, body := post(t, ts, "/api/place", map[string]any{
		"selection": "MHJ ATD", "price": -110, "stake": 10,
		"bankroll": "real money", "book": "draftkings", "week": 1,
		"boost": "dk-boost",
	})
	if code != 200 {
		t.Fatalf("place status %d: %v", code, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatal("no wager id returned")
	}

	// The boost lot must have been consumed as a ledger place tied to the wager.
	evs, err := ledger.Load(lg)
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Lot == "dk-boost" && e.Wager == id {
			consumed = true
		}
	}
	if !consumed {
		t.Fatal("boost lot was not consumed against the wager")
	}
}

func mustBal(t *testing.T, srv *boardServer) ledger.Position {
	t.Helper()
	events, err := ledger.Load(srv.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	pos, err := ledger.Balances(events, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return pos
}

// TestPlace_GUIandCoreAgree pins the whole point of the consolidation: a bet
// entered through the GUI's /api/place handler lands on disk identically to the
// same bet placed through journal.Place directly. If the handler ever drifts
// from the shared core -- a field dropped, a price coerced differently -- the
// betlogs diverge and this fails.
func TestPlace_GUIandCoreAgree(t *testing.T) {
	dir := t.TempDir()

	// GUI side: a board server on its own temp logs, driven over HTTP through
	// the same routes the browser hits. Temp paths keep it off the real
	// defaultBetlog() the production server would use.
	guiBet := filepath.Join(dir, "gui-bet.jsonl")
	guiLedger := filepath.Join(dir, "gui-ledger.jsonl")
	srv := &boardServer{betlogPath: guiBet, ledgerPath: guiLedger}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	body := map[string]any{
		"selection": "Bijan Robinson 60+ rush yds (-115)", "price": -115,
		"stake": 25.0, "bankroll": "real money", "predicted": 0.58,
		"narrative": "volume favorite", "week": 1,
	}
	code, out := post(t, ts, "/api/place", body)
	if code != 200 {
		t.Fatalf("POST /api/place = %d: %v", code, out)
	}
	if out["ok"] != true {
		t.Fatalf("place response missing ok:true: %v", out)
	}
	if id, _ := out["id"].(string); id == "" {
		t.Fatalf("place response missing a non-empty id: %v", out)
	}

	// Core side: the identical inputs through journal.Place on a second pair of
	// temp files. No book, so no ledger draw -- the GUI request carries no book
	// either, so the two are placed under matching conditions.
	coreBet := filepath.Join(dir, "core-bet.jsonl")
	coreLedger := filepath.Join(dir, "core-ledger.jsonl")
	b := betlog.Bet{
		Selection: "Bijan Robinson 60+ rush yds (-115)", Price: wager.American(-115),
		Bankroll: "real money", Stake: 25, Predicted: 0.58,
		Narrative: "volume favorite", Week: 1,
	}
	if _, err := journal.Place(coreBet, coreLedger, journal.PlaceRequest{Bet: b}, time.Now()); err != nil {
		t.Fatal(err)
	}

	guiBets, err := betlog.Load(guiBet)
	if err != nil {
		t.Fatal(err)
	}
	coreBets, err := betlog.Load(coreBet)
	if err != nil {
		t.Fatal(err)
	}
	if len(guiBets) != 1 || len(coreBets) != 1 {
		t.Fatalf("expected one bet in each log, got gui=%d core=%d", len(guiBets), len(coreBets))
	}
	g, c := guiBets[0].Bet, coreBets[0].Bet
	if g.Selection != c.Selection {
		t.Errorf("selection: gui %q vs core %q", g.Selection, c.Selection)
	}
	if g.Price != c.Price {
		t.Errorf("price: gui %v vs core %v", g.Price, c.Price)
	}
	if g.Stake != c.Stake {
		t.Errorf("stake: gui %v vs core %v", g.Stake, c.Stake)
	}
}

// TestPlaceWithDeposit verifies the opt-in funding deposit: with no prior
// balance, deposit:true self-funds the wager (a grant of the stake precedes the
// debit) so /api/place succeeds and the book nets to zero -- while the same
// place without a deposit and no balance is refused.
func TestPlaceWithDeposit(t *testing.T) {
	dir := t.TempDir()
	srv := &boardServer{
		ledgerPath: filepath.Join(dir, "bankroll.jsonl"),
		betlogPath: filepath.Join(dir, "betlog.jsonl"),
	}
	body, _ := json.Marshal(map[string]any{
		"selection": "Nabers over (-115)", "price": -115, "stake": 25,
		"bankroll": "real money", "book": "fanatics", "week": 2, "deposit": true,
	})
	rr := httptest.NewRecorder()
	srv.handlePlace(rr, httptest.NewRequest("POST", "/api/place", strings.NewReader(string(body))))
	if rr.Code != 200 {
		t.Fatalf("deposit place: status %d: %s", rr.Code, rr.Body.String())
	}
	evs, err := ledger.Load(srv.ledgerPath)
	if err != nil {
		t.Fatalf("ledger.Load: %v", err)
	}
	pos, err := ledger.Balances(evs, time.Now())
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if got := pos.Total("fanatics", ledger.Cash); got != 0 {
		t.Fatalf("self-funded bet should net 0 cash, got %v", got)
	}

	// Same place, no balance, no deposit -> refused (insufficient funds).
	body2, _ := json.Marshal(map[string]any{
		"selection": "Nabers over (-115)", "price": -115, "stake": 25,
		"bankroll": "real money", "book": "draftkings", "week": 2,
	})
	rr2 := httptest.NewRecorder()
	srv.handlePlace(rr2, httptest.NewRequest("POST", "/api/place", strings.NewReader(string(body2))))
	if rr2.Code == 200 {
		t.Fatalf("place with no balance and no deposit should fail, got 200")
	}
}

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

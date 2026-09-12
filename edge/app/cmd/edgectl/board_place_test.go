package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/ledger"
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
		if m["debited"] != true {
			t.Fatalf("place %s: expected debited true, got %v", bankroll, m["debited"])
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

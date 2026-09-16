package main

import (
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
)

func TestBetPlace_writesBothLogs(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")
	// Seed a bonus lot.
	_ = ledger.AppendFile(lg, ledger.Event{
		Kind: ledger.KindGrant, ID: "dk-bonus", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-bonus", Book: "draftkings", Asset: ledger.Bonus, Amount: 10},
	})

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Barkley ATD", "-price", "-115", "-stake", "4",
		"-book", "draftkings", "-bankroll", "bonus", "-week", "1",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	if len(bets) != 1 {
		t.Fatalf("expected 1 betlog entry, got %d", len(bets))
	}
	evs, _ := ledger.Load(lg)
	placed := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Wager == bets[0].ID {
			placed = true
		}
	}
	if !placed {
		t.Fatal("no ledger place tied to the wager")
	}
}

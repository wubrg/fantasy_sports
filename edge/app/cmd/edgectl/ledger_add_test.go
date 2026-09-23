package main

import (
	"path/filepath"
	"testing"
	"time"

	"edge/internal/ledger"
)

// TestLedgerAddWithdrawAcceptsWeek is the CLI half of the Tuesday zero-out fix:
// -week used to apply to `place` only, so `ledger add -kind withdraw -week N`
// silently dropped the tag and left the withdrawal to be bucketed by its own
// timestamp -- landing it in the wrong week's report exactly as the GUI button
// did before this fix. deposit/grant/convert get the same tag now, for the
// same reason.
func TestLedgerAddWithdrawAcceptsWeek(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bankroll.jsonl")

	if err := ledgerAdd([]string{"-file", path, "-kind", "deposit",
		"-book", "fanatics", "-asset", "cash", "-amount", "50"}); err != nil {
		t.Fatalf("deposit: %v", err)
	}

	events, err := ledger.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Creates == nil {
		t.Fatalf("deposit did not create a lot: %+v", events)
	}
	lotID := events[0].Creates.ID
	if lotID == "" {
		lotID = events[0].ID
	}

	if err := ledgerAdd([]string{"-file", path, "-kind", "withdraw",
		"-lot", lotID, "-amount", "50", "-week", "2"}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	events, err = ledger.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Kind != ledger.KindWithdraw {
		t.Fatalf("last event kind = %q, want withdraw", last.Kind)
	}
	if last.Week != 2 {
		t.Errorf("withdraw event Week = %d, want 2 -- -week must reach a withdraw, not just a place", last.Week)
	}

	// A fixed window far from "now" -- the assertion can only pass because the
	// tag overrides the timestamp, never by date coincidence.
	start := time.Date(2020, 1, 7, 0, 0, 0, 0, time.UTC)
	rep, err := ledger.Period(events, 2, start, start.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Withdrawals != 50 {
		t.Errorf("week 2 Withdrawals = %.2f, want 50", rep.Withdrawals)
	}
}

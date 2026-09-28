package main

import (
	"os"
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

// TestLedgerAddDerivesBoostExpiryFromWeek is the fix for a boost/no-sweat
// grant's -expiry defaulting to a hand-typed date that happened to land on
// the right Tuesday. With -week given and -expiry omitted, the grant should
// derive its expiry from weekWindow's own Tuesday-rollover boundary -- the
// same one the period report already uses -- so it survives that week's
// Monday night game instead of an arbitrary guess.
func TestLedgerAddDerivesBoostExpiryFromWeek(t *testing.T) {
	dir := t.TempDir()
	week03 := "season: 2026\nweek: 3\ngames:\n  2026_03_A_B:\n    away: A\n    home: B\n    kickoff: 2026-09-24T20:15\n    books:\n"
	week04 := "season: 2026\nweek: 4\ngames:\n  2026_04_A_B:\n    away: A\n    home: B\n    kickoff: 2026-10-01T20:15\n    books:\n"
	if err := os.WriteFile(filepath.Join(dir, "week03.yaml"), []byte(week03), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "week04.yaml"), []byte(week04), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "bankroll.jsonl")
	if err := ledgerAdd([]string{"-file", path, "-dir", dir, "-kind", "grant",
		"-book", "draftkings", "-boost-pct", ".5", "-boost-max", "10",
		"-boost-min-odds", "100", "-boost-market", "sgp", "-boost-needs-cash",
		"-week", "3"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	events, err := ledger.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Creates == nil || events[0].Creates.Expires == nil {
		t.Fatalf("grant did not create a lot with an expiry: %+v", events)
	}
	// Week 3's window ends at the Tuesday on-or-before week 4's first kickoff
	// (2026-10-01) -- 2026-09-29 00:00 local, not the raw week-03 kickoff date.
	got := *events[0].Creates.Expires
	want := time.Date(2026, 9, 29, 0, 0, 0, 0, got.Location())
	if !got.Equal(want) {
		t.Errorf("derived expiry = %v, want %v (week 3's Tuesday rollover boundary)", got, want)
	}
}

// TestLedgerAddBoostWithoutWeekOrExpiryStaysUnset confirms the old behavior
// is unchanged when there's nothing to derive a default from: a boost grant
// with neither -expiry nor -week simply carries no expiry, same as before
// this feature existed -- it does not error, and it does not guess.
func TestLedgerAddBoostWithoutWeekOrExpiryStaysUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bankroll.jsonl")
	if err := ledgerAdd([]string{"-file", path, "-kind", "grant",
		"-book", "draftkings", "-boost-pct", ".5", "-boost-max", "10",
		"-boost-min-odds", "100", "-boost-market", "sgp", "-boost-needs-cash"}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	events, err := ledger.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Creates == nil {
		t.Fatalf("grant did not create a lot: %+v", events)
	}
	if events[0].Creates.Expires != nil {
		t.Errorf("Expires = %v, want nil (no -expiry and no -week to derive from)", events[0].Creates.Expires)
	}
}

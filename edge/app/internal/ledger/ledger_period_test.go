package ledger

import (
	"math"
	"testing"
	"time"
)

// TestPeriodReport walks a two-week campaign and checks that each flow lands in
// the window it belongs to, cash and bonus kept apart. The load-bearing case is
// the straddle: a bet placed in week one and settled in week two must count its
// stake in week one, its P&L in week two, and show as open at the week-one
// boundary. A period report that cannot place a straddling bet is one that would
// mis-attribute every bet graded after a Tuesday.
func TestPeriodReport(t *testing.T) {
	day := 24 * time.Hour
	events := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "fanduel", Asset: Cash, Amount: 100}),
		grant("g1", 0, Lot{ID: "bon", Book: "fanduel", Asset: Bonus, Amount: 50}),

		// Week one: a cash bet won, a bonus bet lost.
		place("p1", time.Hour, "c", "w1", 40),
		settle("s1", 2*time.Hour, "w1", Won, &Lot{ID: "r1", Book: "fanduel", Asset: Cash, Amount: 100}),
		place("p2", time.Hour, "bon", "w2", 50),
		settle("s2", 2*time.Hour, "w2", Lost, nil),

		// The straddle: placed in week one, settles in week two.
		place("p3", 3*time.Hour, "c", "w3", 30),
		settle("s3", 8*day, "w3", Won, &Lot{ID: "r3", Book: "fanduel", Asset: Cash, Amount: 60}),

		// The weekly zero-out: withdraw the week-one proceeds to the bank.
		withdraw("wd1", 6*day, "r1", 80),
	}

	week1, err := Period(events, at(0), at(7*day))
	if err != nil {
		t.Fatalf("week 1: %v", err)
	}
	wantReport(t, "week 1", week1, Report{
		Deposits: 100, Withdrawals: 80,
		StakedCash: 70, StakedBonus: 50, // p1 40 + p3 30 cash; p2 50 bonus
		RealizedCash: 60, RealizedBonus: 0, // w1: 100-40; w2 lost; w3 not yet graded
		OpenStakedCash: 30, // w3 placed, not settled at the boundary
	})

	week2, err := Period(events, at(7*day), at(14*day))
	if err != nil {
		t.Fatalf("week 2: %v", err)
	}
	wantReport(t, "week 2", week2, Report{
		RealizedCash: 30, // w3 settles here: 60 - 30 stake
	})

	// Derived lines.
	if got := week1.ExternalNet(); math.Abs(got-(-20)) > 1e-9 {
		t.Errorf("week 1 ExternalNet: got %.2f, want -20 (withdrew 80, deposited 100)", got)
	}
	if got := week2.RealizedNet(); math.Abs(got-30) > 1e-9 {
		t.Errorf("week 2 RealizedNet: got %.2f, want 30", got)
	}

	// A degenerate window is an error, not a silently empty report.
	if _, err := Period(events, at(7*day), at(7*day)); err == nil {
		t.Error("expected an error for an end that does not follow start")
	}
}

// wantReport asserts the flow buckets of a period, naming any that disagree.
func wantReport(t *testing.T, label string, got, want Report) {
	t.Helper()
	for _, f := range []struct {
		name      string
		got, want float64
	}{
		{"Deposits", got.Deposits, want.Deposits},
		{"Withdrawals", got.Withdrawals, want.Withdrawals},
		{"StakedCash", got.StakedCash, want.StakedCash},
		{"StakedBonus", got.StakedBonus, want.StakedBonus},
		{"RealizedCash", got.RealizedCash, want.RealizedCash},
		{"RealizedBonus", got.RealizedBonus, want.RealizedBonus},
		{"OpenStakedCash", got.OpenStakedCash, want.OpenStakedCash},
		{"OpenStakedBonus", got.OpenStakedBonus, want.OpenStakedBonus},
	} {
		if math.Abs(f.got-f.want) > 1e-9 {
			t.Errorf("%s %s: got %.2f, want %.2f", label, f.name, f.got, f.want)
		}
	}
}

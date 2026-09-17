package ledger

import (
	"math"
	"testing"
	"time"
)

// placeWk is place() with an explicit NFL-week tag, the field the period report
// attributes by.
func placeWk(id string, when time.Duration, lot, wagerID string, amount float64, week int) Event {
	e := place(id, when, lot, wagerID, amount)
	e.Week = week
	return e
}

// TestPeriodReport walks a two-week campaign with untagged bets, so attribution
// falls back to the SETTLEMENT date — the week a wager's games were played. The
// load-bearing case is the straddle: a bet placed in week one but graded in week
// two belongs to week two whole — its stake AND its P&L — because an untagged
// wager is attributed to the week it settles for, not the week it was struck.
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

		// The straddle: placed in week one, graded in week two.
		place("p3", 3*time.Hour, "c", "w3", 30),
		settle("s3", 8*day, "w3", Won, &Lot{ID: "r3", Book: "fanduel", Asset: Cash, Amount: 60}),

		// The weekly zero-out: withdraw the week-one proceeds to the bank.
		withdraw("wd1", 6*day, "r1", 80),
	}

	week1, err := Period(events, 0, at(0), at(7*day))
	if err != nil {
		t.Fatalf("week 1: %v", err)
	}
	wantReport(t, "week 1", week1, Report{
		Deposits: 100, Withdrawals: 80,
		StakedCash: 40, StakedBonus: 50, // p1 40 cash; p2 50 bonus (p3 settles in week 2)
		// w1 won 100-40=60; w2 bonus lost. w3 was placed this week but settles in
		// week 2, so its stake and P&L are attributed there, not here.
		RealizedCash: 60, RealizedBonus: 0,
	})

	week2, err := Period(events, 0, at(7*day), at(14*day))
	if err != nil {
		t.Fatalf("week 2: %v", err)
	}
	// The straddle settled in week two, so under settle-week attribution its whole
	// stake and P&L land here — the week its games were played — even though it was
	// placed in week one.
	wantReport(t, "week 2", week2, Report{
		StakedCash: 30, RealizedCash: 30, // w3 won 60-30
	})

	if got := week1.ExternalNet(); math.Abs(got-(-20)) > 1e-9 {
		t.Errorf("week 1 ExternalNet: got %.2f, want -20 (withdrew 80, deposited 100)", got)
	}

	if _, err := Period(events, 0, at(7*day), at(7*day)); err == nil {
		t.Error("expected an error for an end that does not follow start")
	}
}

// TestPeriodUntaggedAttributesBySettle is the opt-in-token case that motivated
// settle-week attribution: a bonus bet struck a month before the season (no week
// tag) but graded on the reported week's Sunday belongs to that week — the week
// its game was played — not to the window it was placed in, where under the old
// place-date fallback it vanished from every week's P&L entirely.
func TestPeriodUntaggedAttributesBySettle(t *testing.T) {
	day := 24 * time.Hour
	events := []Event{
		grant("g", -30*day, Lot{ID: "bon", Book: "fanatics", Asset: Bonus, Amount: 50}),
		place("p", -30*day, "bon", "w", 50), // placed a month before the window
		// ...but graded inside week 1's window: a +390 winner, $195 cash back.
		settle("s", 2*day, "w", Won, &Lot{ID: "r", Book: "fanatics", Asset: Cash, Amount: 195}),
	}
	wk1, err := Period(events, 0, at(0), at(7*day))
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	wantReport(t, "settle-in-window", wk1, Report{
		StakedBonus: 50, RealizedBonus: 195, // the won bonus's cash returns land in week 1
	})
}

// TestPeriodAttributesByTag is the reason the tag exists: a bet logged early for
// a later week must land in that week, and a bet logged for a week must not land
// in a neighbouring one just because its dates overlap. The tag wins over the
// window in both directions.
func TestPeriodAttributesByTag(t *testing.T) {
	day := 24 * time.Hour
	events := []Event{
		deposit("d", 0, Lot{ID: "c", Book: "fanduel", Asset: Cash, Amount: 100}),
		// A wager FOR week two, but placed and graded during week one's window.
		placeWk("p", time.Hour, "c", "w2bet", 20, 2),
		settle("s", 2*time.Hour, "w2bet", Won, &Lot{ID: "r", Book: "fanduel", Asset: Cash, Amount: 50}),
	}

	// Reporting week one over its own window must exclude the week-two bet,
	// despite it being placed inside that window.
	wk1, err := Period(events, 1, at(0), at(7*day))
	if err != nil {
		t.Fatalf("week 1: %v", err)
	}
	wantReport(t, "week 1 excludes a wk2-tagged bet", wk1, Report{Deposits: 100})

	// Reporting week two over a later window must include it, by tag, even
	// though nothing week-two happened in that window by date.
	wk2, err := Period(events, 2, at(7*day), at(14*day))
	if err != nil {
		t.Fatalf("week 2: %v", err)
	}
	wantReport(t, "week 2 includes a wk2-tagged bet", wk2, Report{
		StakedCash: 20, RealizedCash: 30, // 50 returned - 20 stake
	})
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

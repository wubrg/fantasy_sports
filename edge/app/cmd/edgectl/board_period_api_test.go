package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/ledger"
)

// wk writes a minimal week file with one game at the given local kickoff, so the
// period window can be derived from the schedule the way the real board does.
func wk(t *testing.T, dir string, week int, kickoff string) {
	t.Helper()
	body := "season: 2026\nweek: " + itoa(week) + "\ngames:\n" +
		"  2026_" + pad2(week) + "_AA_BB:\n" +
		"    away: AA\n    home: BB\n    kickoff: " + kickoff + "\n"
	if err := os.WriteFile(filepath.Join(dir, "week"+pad2(week)+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string { return string(rune('0'+n%10)) } // single digit is enough here
func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n/10) + itoa(n%10)
}

func localTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

type periodResp struct {
	Week            int     `json:"week"`
	Start           string  `json:"start"`
	End             string  `json:"end"`
	Deposits        float64 `json:"deposits"`
	Withdrawals     float64 `json:"withdrawals"`
	NetToBank       float64 `json:"net_to_bank"`
	RealizedCash    float64 `json:"realized_cash"`
	RealizedBonus   float64 `json:"realized_bonus"`
	RealizedNet     float64 `json:"realized_net"`
	StakedCash      float64 `json:"staked_cash"`
	StakedBonus     float64 `json:"staked_bonus"`
	OpenStakedCash  float64 `json:"open_staked_cash"`
	OpenStakedBonus float64 `json:"open_staked_bonus"`
	Weeks           []int   `json:"weeks"`
}

func TestHandlePeriodReportsWeek(t *testing.T) {
	dir := t.TempDir()
	// Week 1 kicks off Thu 2026-01-08; week 2 the following Thursday. The window
	// runs Tuesday-to-Tuesday: [2026-01-06, 2026-01-13).
	wk(t, dir, 1, "2026-01-08T13:00")
	wk(t, dir, 2, "2026-01-15T13:00")

	ledgerPath := filepath.Join(t.TempDir(), "bankroll.jsonl")
	for _, e := range []ledger.Event{
		{Kind: ledger.KindDeposit, ID: "d1", Time: localTime(t, "2026-01-07T10:00"),
			Creates: &ledger.Lot{ID: "c", Book: "fanduel", Asset: ledger.Cash, Amount: 100}},
		{Kind: ledger.KindPlace, ID: "p1", Time: localTime(t, "2026-01-07T12:00"),
			Lot: "c", Wager: "w1", Amount: 40},
		{Kind: ledger.KindSettle, ID: "s1", Time: localTime(t, "2026-01-08T20:00"),
			Wager: "w1", Result: ledger.Won,
			Returns: &ledger.Lot{ID: "r1", Book: "fanduel", Asset: ledger.Cash, Amount: 100}},
	} {
		if err := ledger.AppendFile(ledgerPath, e); err != nil {
			t.Fatal(err)
		}
	}

	srv := &boardServer{dir: dir, ledgerPath: ledgerPath}
	rr := httptest.NewRecorder()
	srv.handlePeriod(rr, httptest.NewRequest("GET", "/api/period?week=1", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r periodResp
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rr.Body.String())
	}

	if r.Start != "2026-01-06" || r.End != "2026-01-13" {
		t.Errorf("window = %s → %s, want 2026-01-06 → 2026-01-13", r.Start, r.End)
	}
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"deposits", r.Deposits, 100},
		{"staked_cash", r.StakedCash, 40},
		{"realized_cash", r.RealizedCash, 60}, // 100 returned - 40 stake
		{"realized_net", r.RealizedNet, 60},
		{"net_to_bank", r.NetToBank, -100}, // deposited 100, withdrew nothing
	} {
		if math.Abs(c.got-c.want) > 1e-9 {
			t.Errorf("%s = %.2f, want %.2f", c.name, c.got, c.want)
		}
	}
	if len(r.Weeks) != 2 {
		t.Errorf("weeks = %v, want the two scaffolded weeks", r.Weeks)
	}
}

func TestHandlePeriodHonorsWeekTag(t *testing.T) {
	dir := t.TempDir()
	wk(t, dir, 1, "2026-01-08T13:00") // window [2026-01-06, 2026-01-13)
	wk(t, dir, 2, "2026-01-15T13:00")

	ledgerPath := filepath.Join(t.TempDir(), "bankroll.jsonl")
	// A week-1 bet logged the week before its window opens. By date it would
	// fall in the prior period; its tag must pull it into week 1 anyway.
	for _, e := range []ledger.Event{
		{Kind: ledger.KindDeposit, ID: "d1", Time: localTime(t, "2026-01-02T10:00"),
			Creates: &ledger.Lot{ID: "c", Book: "fanduel", Asset: ledger.Cash, Amount: 100}},
		{Kind: ledger.KindPlace, ID: "p1", Time: localTime(t, "2026-01-02T12:00"),
			Lot: "c", Wager: "w1", Amount: 40, Week: 1},
		{Kind: ledger.KindSettle, ID: "s1", Time: localTime(t, "2026-01-03T20:00"),
			Wager: "w1", Result: ledger.Won,
			Returns: &ledger.Lot{ID: "r1", Book: "fanduel", Asset: ledger.Cash, Amount: 100}},
	} {
		if err := ledger.AppendFile(ledgerPath, e); err != nil {
			t.Fatal(err)
		}
	}

	srv := &boardServer{dir: dir, ledgerPath: ledgerPath}
	rr := httptest.NewRecorder()
	srv.handlePeriod(rr, httptest.NewRequest("GET", "/api/period?week=1", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r periodResp
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	// The stake and P&L are attributed to week 1 by the tag, though the deposit
	// (untagged capital) fell before the window and is not counted.
	if math.Abs(r.StakedCash-40) > 1e-9 || math.Abs(r.RealizedCash-60) > 1e-9 {
		t.Errorf("tagged wk1 bet not attributed: staked_cash=%.2f realized_cash=%.2f, want 40 and 60", r.StakedCash, r.RealizedCash)
	}
}

func TestHandlePeriodRejectsUnknownWeek(t *testing.T) {
	dir := t.TempDir()
	wk(t, dir, 1, "2026-01-08T13:00")
	srv := &boardServer{dir: dir, ledgerPath: filepath.Join(t.TempDir(), "none.jsonl")}
	rr := httptest.NewRecorder()
	srv.handlePeriod(rr, httptest.NewRequest("GET", "/api/period?week=9", nil))
	if rr.Code != 404 {
		t.Fatalf("status %d for a week with no file, want 404: %s", rr.Code, rr.Body.String())
	}
}

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

// TestZeroCashViaAdjustIsAWithdrawalNotALoss covers the Tuesday zero-out flow:
// a cash balance zeroed through /api/funds/adjust (what the funds tab's "zero
// out" button calls) must clear the balance and, in the period report, show
// up as a WITHDRAWAL -- not as realized cash or a loss. That distinction is
// the entire reason the period report exists: see its doc comment in
// board_period_api.go ("the weekly zero-out to the bank makes invisible to a
// balance"). Getting this wrong would make every future period report lie
// about how the week actually went the moment someone taps the button.
func TestZeroCashViaAdjustIsAWithdrawalNotALoss(t *testing.T) {
	dir := t.TempDir()
	// handleAdjust stamps events at the real time.Now(), unlike most other
	// tests in this package which build fixed-timestamp ledger.Event slices
	// directly -- this test goes through the live HTTP handler on purpose, to
	// prove the button's actual wiring, so the week's window is anchored to
	// today rather than a fixed date the test would otherwise silently drift
	// out of.
	kickoff := time.Now().Add(2 * time.Hour).Format("2006-01-02T15:04")
	wk(t, dir, 1, kickoff)
	led := filepath.Join(t.TempDir(), "bankroll.jsonl")
	srv := &boardServer{dir: dir, ledgerPath: led}

	do := func(handler string, method, body string) (int, map[string]any) {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/x", strings.NewReader(body))
		switch handler {
		case "funds":
			srv.handleFunds(rr, req)
		case "adjust":
			srv.handleAdjust(rr, req)
		}
		var m map[string]any
		if rr.Body.Len() > 0 {
			if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
				t.Fatalf("%s: bad json: %v (%s)", handler, err, rr.Body.String())
			}
		}
		return rr.Code, m
	}

	// Deposit $50 cash to fanatics -- the balance a Tuesday morning would find.
	if code, body := do("funds", "POST", `{"book":"fanatics","asset":"cash","amount":50}`); code != 200 {
		t.Fatalf("deposit: %d %v", code, body)
	}

	// Zero it out via the same endpoint the funds-tab button calls.
	if code, body := do("adjust", "POST",
		`{"book":"fanatics","asset":"cash","target":0,"note":"Tuesday zero-out to the bank"}`); code != 200 {
		t.Fatalf("adjust: %d %v", code, body)
	}

	// The balance is gone.
	rr := httptest.NewRecorder()
	srv.handleFunds(rr, httptest.NewRequest("GET", "/api/funds", nil))
	var got struct {
		Balances []balanceJSON `json:"balances"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	for _, b := range got.Balances {
		if b.Book == "fanatics" && b.Asset == "cash" && b.Amount > 0.005 {
			t.Fatalf("fanatics cash balance still %.2f after zeroing", b.Amount)
		}
	}

	// The period report must count the $50 as a WITHDRAWAL, and must NOT count
	// it as realized cash or a loss -- zeroing a balance did not lose the
	// money, it moved it to the bank.
	start, end, err := weekWindow(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	events, err := ledger.Load(led)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ledger.Period(events, 1, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Withdrawals != 50 {
		t.Errorf("Withdrawals = %.2f, want 50", rep.Withdrawals)
	}
	if rep.RealizedCash != 0 {
		t.Errorf("RealizedCash = %.2f, want 0 -- zeroing must not read as a loss", rep.RealizedCash)
	}
}

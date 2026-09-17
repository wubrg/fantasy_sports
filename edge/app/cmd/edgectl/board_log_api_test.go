package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"edge/internal/betlog"
	"edge/internal/wager"
)

// TestHandleLogSplitsAtRiskByBankroll pins that the log reports real-money and
// bonus stakes as separate at-risk figures — a lost bonus bet costs no cash, so
// $50 of bonus at risk is not $50 of real money at risk.
func TestHandleLogSplitsAtRiskByBankroll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if _, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "cash bet", Price: -110, Bankroll: "real money", Stake: 30, Predicted: 0.55}); err != nil {
		t.Fatal(err)
	}
	if _, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "bonus bet", Price: 300, Bankroll: "bonus bet", Stake: 50, Predicted: 0.25}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	(&boardServer{betlogPath: path}).handleLog(rr, httptest.NewRequest("GET", "/api/log", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r struct {
		OpenStaked      float64 `json:"open_staked"`
		OpenStakedCash  float64 `json:"open_staked_cash"`
		OpenStakedBonus float64 `json:"open_staked_bonus"`
		OpenPayout      float64 `json:"open_payout"`
		Entries         []struct {
			Selection string  `json:"selection"`
			Payout    float64 `json:"payout"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.OpenStakedCash != 30 || r.OpenStakedBonus != 50 || r.OpenStaked != 80 {
		t.Errorf("cash=%v bonus=%v total=%v, want 30/50/80", r.OpenStakedCash, r.OpenStakedBonus, r.OpenStaked)
	}
	// To-win: -110 on 30 pays 30*(100/110)=27.27; +300 on 50 pays 150. The
	// aggregate is the sum of the two, independent of any belief.
	if math.Abs(r.OpenPayout-177.2727) > 1e-3 {
		t.Errorf("open_payout=%.4f, want ~177.27", r.OpenPayout)
	}
	want := map[string]float64{"cash bet": 27.2727, "bonus bet": 150}
	for _, e := range r.Entries {
		if math.Abs(e.Payout-want[e.Selection]) > 1e-3 {
			t.Errorf("%q payout=%.4f, want %.4f", e.Selection, e.Payout, want[e.Selection])
		}
	}
}

// TestRealizedPnL pins the settled-P&L rules the log's "realized" figure sums:
// a win books its profit regardless of bankroll; only a real-money loss costs
// cash; a bonus loss, a push and a void are all zero.
func TestRealizedPnL(t *testing.T) {
	cases := []struct {
		name     string
		bankroll string
		result   betlog.Result
		price    wager.American
		stake    float64
		want     float64
	}{
		{"won real money", "real money", betlog.Won, 150, 50, 75},   // 50 * 1.5
		{"won bonus", "bonus bet", betlog.Won, 150, 50, 75},         // profit is cash either way
		{"won favorite", "real money", betlog.Won, -200, 50, 25},    // 50 * 0.5
		{"lost real money", "real money", betlog.Lost, 150, 50, -50}, // stake gone
		{"lost bonus", "bonus bet", betlog.Lost, 150, 50, 0},        // no cash was risked
		{"push", "real money", betlog.Pushed, 150, 50, 0},
		{"void", "real money", betlog.Void, 150, 50, 0},
		{"open counts as nothing", "real money", betlog.Open, 150, 50, 0},
	}
	for _, c := range cases {
		got := realizedPnL(c.bankroll, c.result, c.price, c.stake)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: realizedPnL = %+.2f, want %+.2f", c.name, got, c.want)
		}
	}
}

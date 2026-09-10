package main

import (
	"math"
	"testing"

	"edge/internal/betlog"
	"edge/internal/wager"
)

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

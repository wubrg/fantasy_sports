package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"edge/internal/ledger"
)

// The period report, over HTTP.
//
// Funds answer "what do I hold now"; this answers "what did a week cost or
// make", which the weekly zero-out to the bank makes invisible to a balance. It
// is the same ledger, replayed over a window instead of to a point -- see
// ledger.Period. The window for an NFL week comes from the schedule (weekWindow),
// so the two views agree on when a week begins.
type periodJSON struct {
	Week  int    `json:"week"`
	Start string `json:"start"`
	End   string `json:"end"`

	Deposits    float64 `json:"deposits"`
	Withdrawals float64 `json:"withdrawals"`
	NetToBank   float64 `json:"net_to_bank"`

	RealizedCash  float64 `json:"realized_cash"`
	RealizedBonus float64 `json:"realized_bonus"`
	RealizedNet   float64 `json:"realized_net"`

	StakedCash  float64 `json:"staked_cash"`
	StakedBonus float64 `json:"staked_bonus"`

	OpenStakedCash  float64 `json:"open_staked_cash"`
	OpenStakedBonus float64 `json:"open_staked_bonus"`

	// Weeks with a schedule file, so the tab can offer a selector without a
	// second request.
	Weeks []int `json:"weeks"`
}

func (s *boardServer) handlePeriod(w http.ResponseWriter, r *http.Request) {
	weeks := s.weekNumbers()
	if len(weeks) == 0 {
		httpError(w, http.StatusNotFound, "no week files found, so no period can be bounded")
		return
	}

	week := currentWeek(s, weeks, time.Now())
	if q := r.URL.Query().Get("week"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n <= 0 {
			httpError(w, http.StatusBadRequest, "week must be a positive integer")
			return
		}
		week = n
	}

	start, end, err := weekWindow(s.dir, week)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}

	events, err := ledger.Load(s.ledgerPath)
	if err != nil && !os.IsNotExist(err) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rep, err := ledger.Period(events, week, start, end)
	if err != nil {
		// A log that will not replay is a real problem, not an empty period.
		httpError(w, http.StatusConflict, "the bankroll log does not replay: "+err.Error())
		return
	}

	writeJSON(w, periodJSON{
		Week:  week,
		Start: rep.Start.Format("2006-01-02"),
		End:   rep.End.Format("2006-01-02"),

		Deposits:    rep.Deposits,
		Withdrawals: rep.Withdrawals,
		NetToBank:   rep.ExternalNet(),

		RealizedCash:  rep.RealizedCash,
		RealizedBonus: rep.RealizedBonus,
		RealizedNet:   rep.RealizedNet(),

		StakedCash:  rep.StakedCash,
		StakedBonus: rep.StakedBonus,

		OpenStakedCash:  rep.OpenStakedCash,
		OpenStakedBonus: rep.OpenStakedBonus,

		Weeks: weeks,
	})
}

// currentWeek is the default the tab opens on: the latest week whose window has
// already begun, so mid-season it lands on the week in play rather than on a
// future slate. Before the first week starts it falls back to the earliest.
func currentWeek(s *boardServer, weeks []int, now time.Time) int {
	best := weeks[0]
	for _, wk := range weeks {
		start, _, err := weekWindow(s.dir, wk)
		if err != nil {
			continue
		}
		if !start.After(now) {
			best = wk
		}
	}
	return best
}

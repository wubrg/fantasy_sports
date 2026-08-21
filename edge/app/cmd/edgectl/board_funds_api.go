package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"edge/internal/ledger"
)

// The bankroll, over HTTP.
//
// The ledger has existed and been unused: balances, expiry tracking and an
// impossible-state replay, imported by nothing but its own CLI. It was unused
// because entering a bankroll meant hand-writing JSONL, which nobody does from
// a phone, and because nothing read it back.
//
// These handlers close both ends. Funds are declared from the same screen the
// board is entered on, and a recorded wager draws its stake from them, so the
// balance the report plans against is the balance the log says you hold rather
// than a number retyped on the command line.

func defaultLedger() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "bankroll.jsonl"
	}
	return filepath.Join(home, "bankroll.jsonl")
}

type balanceJSON struct {
	Book   string  `json:"book"`
	Asset  string  `json:"asset"`
	Amount float64 `json:"amount"`
	Units  int     `json:"units"`
}

type expiryJSON struct {
	Book    string `json:"book"`
	Asset   string `json:"asset"`
	Label   string `json:"label"`
	At      string `json:"at"`
	InHours int    `json:"in_hours"`
	Expired bool   `json:"expired"`
}

func (s *boardServer) handleFunds(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.addFunds(w, r)
		return
	}
	events, err := ledger.Load(s.ledgerPath)
	if err != nil && !os.IsNotExist(err) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	pos, err := ledger.Balances(events, now)
	if err != nil {
		// A replay that will not fold is a real problem and must not be
		// smoothed into an empty bankroll: the balances would look merely
		// absent rather than wrong.
		httpError(w, http.StatusConflict, "the bankroll log does not replay: "+err.Error())
		return
	}

	out := struct {
		Path     string        `json:"path"`
		Balances []balanceJSON `json:"balances"`
		Expiring []expiryJSON  `json:"expiring"`
	}{Path: s.ledgerPath}

	for _, b := range pos.Balances() {
		out.Balances = append(out.Balances, balanceJSON{
			Book: b.Book, Asset: b.Asset, Amount: b.Amount, Units: b.Units,
		})
	}
	// Thirty days is generous for a promo but not for cash; anything further
	// out is not a deadline anyone is acting on this week.
	exp, err := ledger.Expiring(events, 30*24*time.Hour, now)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, e := range exp {
		label := e.Lot.Asset
		if e.Lot.Boost != nil {
			label = fmt.Sprintf("%.0f%% boost", e.Lot.Boost.Percent*100)
		}
		out.Expiring = append(out.Expiring, expiryJSON{
			Book: e.Lot.Book, Asset: e.Lot.Asset, Label: label,
			At: e.At.Format("2006-01-02"), InHours: int(e.In.Hours()), Expired: e.Expired(),
		})
	}
	writeJSON(w, out)
}

type addFundsReq struct {
	Book    string  `json:"book"`
	Asset   string  `json:"asset"`
	Amount  float64 `json:"amount"`
	Expires string  `json:"expires"`
	Note    string  `json:"note"`
}

// addFunds records money arriving.
//
// It appends a grant rather than setting a balance. A ledger that could be set
// would lose the distinction between "I was given $50" and "I have $50 left",
// and the second is derived from the first plus everything since -- which is
// the property that lets balances be reconstructed at a past date at all.
func (s *boardServer) addFunds(w http.ResponseWriter, r *http.Request) {
	var req addFundsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Book) == "" {
		httpError(w, http.StatusBadRequest, "which book holds it?")
		return
	}
	if req.Asset == "" {
		req.Asset = "bonus"
	}
	if req.Amount <= 0 {
		httpError(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	lot := ledger.Lot{Book: req.Book, Asset: req.Asset, Amount: req.Amount}
	if strings.TrimSpace(req.Expires) != "" {
		// Local time: a promo deadline is read off a book's page in the
		// operator's own timezone, and shifting it to UTC moves a Tuesday
		// deadline into Monday for anyone west of Greenwich.
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.Expires), time.Local)
		if err != nil {
			httpError(w, http.StatusBadRequest, "expires: want a date like 2026-08-26")
			return
		}
		lot.Expires = &t
	}

	now := time.Now()
	e := ledger.Event{
		Kind: "grant", ID: ledger.NewID(now, req.Book+"-"+req.Asset), Time: now,
		Creates: &lot, Note: req.Note,
	}
	if err := ledger.AppendFile(s.ledgerPath, e); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": e.ID})
}

// fundsFor reads deployable balances for the named books.
//
// Only amount-shaped assets count. A profit boost is a unit lot and is not a
// bankroll -- it multiplies a stake rather than being one -- so summing it into
// funds would put money on the board that cannot be wagered.
func (s *boardServer) fundsFor(books []string) (map[string]float64, error) {
	events, err := ledger.Load(s.ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	pos, err := ledger.Balances(events, time.Now())
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, b := range books {
		want[b] = true
	}
	out := map[string]float64{}
	for _, b := range pos.Balances() {
		if want[b.Book] && b.Amount > 0 {
			out[b.Book] += b.Amount
		}
	}
	return out, nil
}

// drawFrom picks the lots a stake should come out of, oldest deadline first.
//
// Soonest-expiring first is the only ordering that does not quietly waste
// money: spending a balance that lasts a year while one expiring on Tuesday
// sits untouched is how a bankroll leaks without any single decision looking
// wrong.
func (s *boardServer) drawFrom(book string, stake float64) ([]ledger.Event, error) {
	events, err := ledger.Load(s.ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no bankroll recorded at %s", s.ledgerPath)
		}
		return nil, err
	}
	now := time.Now()
	pos, err := ledger.Balances(events, now)
	if err != nil {
		return nil, err
	}

	var open []ledger.Lot
	for _, l := range pos.Lots {
		if l.Book == book && !l.Unit() && l.Amount > 0 {
			open = append(open, l)
		}
	}
	sort.SliceStable(open, func(i, j int) bool {
		ei, ej := open[i].Expires, open[j].Expires
		switch {
		case ei != nil && ej != nil:
			return ei.Before(*ej)
		case ei != nil:
			return true // a deadline outranks no deadline
		case ej != nil:
			return false
		}
		return open[i].ID < open[j].ID
	})

	var total float64
	for _, l := range open {
		total += l.Amount
	}
	if total+1e-9 < stake {
		return nil, fmt.Errorf(
			"%s holds %.2f, which will not cover a %.2f stake", book, total, stake)
	}

	var out []ledger.Event
	left := stake
	for _, l := range open {
		if left <= 1e-9 {
			break
		}
		take := l.Amount
		if take > left {
			take = left
		}
		out = append(out, ledger.Event{Kind: "place", Lot: l.ID, Amount: take})
		left -= take
	}
	return out, nil
}

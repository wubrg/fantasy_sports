package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"edge/internal/wager"
)

// The bets-tab calculator, over HTTP.
//
// `edgectl hitrate` and `edgectl parlay` already do this math, but both are
// terminal commands and the bets tab is where an operator actually has a
// player's game log or a book's leg prices in front of them (on a phone, with
// the book's app one switch away). These two endpoints wrap the same
// wager-package functions the CLI calls, so a hand-typed prop or parlay gets
// exactly the CLI's answer without a trip to a terminal.

// hitRateReq mirrors `edgectl hitrate`'s flags. Values is the same
// comma/space-separated game log string the CLI parses with parseValues, kept
// as one convention rather than inventing a JSON array for the client to build.
//
// Line is a pointer so an omitted field can be told apart from an explicit 0 --
// the same trap hitrateCmd guards against with fs.Visit: a silently defaulted
// line of 0 would score every game against zero and report a bogus 100% hit
// rate.
type hitRateReq struct {
	Values     string   `json:"values"`
	Line       *float64 `json:"line"`
	Side       string   `json:"side"`
	Confidence float64  `json:"confidence"`
	// Price is optional; when supplied a verdict/breakeven is returned too, the
	// same as hitrateCmd's -price.
	Price int `json:"price"`
}

type hitRateResp struct {
	Hits       int     `json:"hits"`
	N          int     `json:"n"`
	Pushes     int     `json:"pushes"`
	Rate       float64 `json:"rate"`
	Lower      float64 `json:"lower"`
	Upper      float64 `json:"upper"`
	Confidence float64 `json:"confidence"`
	Width      float64 `json:"width"`
	// Verdict/Breakeven are only set when the request supplied a price.
	Verdict   string  `json:"verdict,omitempty"`
	Breakeven float64 `json:"breakeven,omitempty"`
}

func (s *boardServer) handleHitRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req hitRateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Values == "" {
		httpError(w, http.StatusBadRequest, "values is required: a hit rate needs a game log")
		return
	}
	if req.Line == nil {
		httpError(w, http.StatusBadRequest, "line is required: without it every game is scored against 0")
		return
	}
	var side wager.Side
	switch strings.ToLower(strings.TrimSpace(req.Side)) {
	case "over", "o", "":
		side = wager.Over
	case "under", "u":
		side = wager.Under
	default:
		httpError(w, http.StatusBadRequest, "side must be 'over' or 'under', got "+req.Side)
		return
	}
	confidence := req.Confidence
	if confidence == 0 {
		confidence = 0.95
	}
	values, err := parseValues(req.Values)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := wager.ComputeHitRate(values, *req.Line, side, confidence)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := hitRateResp{
		Hits: h.Hits, N: h.N, Pushes: h.Pushes, Rate: h.Rate,
		Lower: h.Lower, Upper: h.Upper, Confidence: h.Confidence, Width: h.Width(),
	}
	if req.Price != 0 {
		price := wager.American(req.Price)
		if verdict, err := h.Assess(price); err == nil {
			resp.Verdict = verdict.String()
			if breakeven, err := price.Breakeven(); err == nil {
				resp.Breakeven = breakeven
			}
		}
	}
	writeJSON(w, resp)
}

// parlayLegReq is one leg of a proposed multi-leg wager: a description, the
// price offered on it, and the game/event it belongs to. Game is required for
// the same reason the CLI's parlayCmd requires it on every leg (see
// guardSameGame in ladder.go): without a tag there is no way to tell an
// independent cross-game leg from a correlated same-game one.
type parlayLegReq struct {
	Selection string `json:"selection"`
	Price     int    `json:"price"`
	Game      string `json:"game"`
}

type parlayCombineReq struct {
	Legs []parlayLegReq `json:"legs"`
}

type parlayCombineResp struct {
	Price   int     `json:"price"`
	Decimal float64 `json:"decimal"`
	Implied float64 `json:"implied"`
}

// handleParlayCombine wraps wager.CombineParlay for a set of legs the operator
// typed in, enforcing the same same-game-vs-cross-game split as `edgectl
// parlay`: two or more legs tagged to the same game are refused outright,
// with the same reasoning the CLI gives, rather than silently multiplied
// together as if independent.
func (s *boardServer) handleParlayCombine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req parlayCombineReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Legs) < 2 {
		httpError(w, http.StatusBadRequest, "a parlay needs at least 2 legs")
		return
	}
	legs := make([]parlayLeg, len(req.Legs))
	prices := make([]wager.American, len(req.Legs))
	for i, l := range req.Legs {
		legs[i] = parlayLeg{price: wager.American(l.Price), game: strings.TrimSpace(l.Game), label: l.Selection}
		prices[i] = wager.American(l.Price)
	}
	if err := guardSameGame(legs); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	combo, err := wager.CombineParlay(prices)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, parlayCombineResp{
		Price: int(combo.Price), Decimal: combo.Decimal, Implied: combo.Implied,
	})
}

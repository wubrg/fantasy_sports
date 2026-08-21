package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"edge/internal/betlog"
	"edge/internal/wager"
)

// The prediction log, over HTTP.
//
// Until now a placed wager was hand-appended as JSON. That is not merely
// tedious: the log's whole value is that a prediction demonstrably predates
// its outcome, and every hour between placing a bet and writing it down is an
// hour in which the writing down can fail to happen. Nine bets on this board
// were logged by hand, one of them five days late.
//
// Recording from the same screen that proposed the wager closes that gap. The
// numbers written are the ones the report computed, so a transcription step
// disappears with it.

// defaultBetlog is where the campaign's predictions live.
func defaultBetlog() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "fanatics-bonus.jsonl"
	}
	return filepath.Join(home, "fanatics-bonus.jsonl")
}

type logEntryJSON struct {
	ID        string  `json:"id"`
	Placed    string  `json:"placed"`
	Selection string  `json:"selection"`
	Price     int     `json:"price"`
	Stake     float64 `json:"stake"`
	Bankroll  string  `json:"bankroll"`
	Predicted float64 `json:"predicted"`
	Result    string  `json:"result"`
	Narrative string  `json:"narrative"`
}

func (s *boardServer) handleLog(w http.ResponseWriter, r *http.Request) {
	path := s.betlogPath
	bets, err := betlog.Load(path)
	if err != nil {
		// A log that does not exist yet is an empty log, not a failure: the
		// first bet recorded creates it.
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"path": path, "entries": []logEntryJSON{}})
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]logEntryJSON, 0, len(bets))
	var open, staked, evTotal float64
	for _, b := range bets {
		res := string(b.Result)
		if res == "" {
			res = "open"
		}
		if res == "open" {
			open++
		}
		staked += b.Bet.Stake
		if ev, err := wager.EV(mustBankroll(b.Bet.Bankroll), b.Bet.Predicted, b.Bet.Price, b.Bet.Stake); err == nil {
			evTotal += ev
		}
		out = append(out, logEntryJSON{
			ID: b.ID, Placed: b.Placed.Format("2006-01-02"),
			Selection: b.Bet.Selection, Price: int(b.Bet.Price), Stake: b.Bet.Stake,
			Bankroll: b.Bet.Bankroll, Predicted: b.Bet.Predicted,
			Result: res, Narrative: b.Bet.Narrative,
		})
	}
	// Newest first: the log is read to check what was just recorded far more
	// often than to review the beginning of a campaign.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	writeJSON(w, map[string]any{
		"path": path, "entries": out,
		"count": len(out), "open": open, "staked": staked, "ev": evTotal,
	})
}

// mustBankroll falls back to a bonus bet, which is what every entry in this
// campaign is. A bad value only costs the EV column, not the listing.
func mustBankroll(s string) wager.Bankroll {
	b, err := betlog.ParseBankroll(s)
	if err != nil {
		return wager.BonusBet
	}
	return b
}

type placeReq struct {
	Selection string  `json:"selection"`
	Price     int     `json:"price"`
	Stake     float64 `json:"stake"`
	Predicted float64 `json:"predicted"`
	Bankroll  string  `json:"bankroll"`
	Narrative string  `json:"narrative"`
}

func (s *boardServer) handlePlace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req placeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Bankroll == "" {
		req.Bankroll = "bonus bet"
	}
	if req.Stake <= 0 {
		httpError(w, http.StatusBadRequest, "stake must be positive")
		return
	}

	// The price and probability are taken from the request, which carries what
	// the report displayed, rather than being recomputed from the board here.
	//
	// That is the point of the whole exercise. A board cell holds only the
	// latest price, and re-entering prices is what the board is FOR. Recording
	// a reference to the cell, or recomputing from it later, would let a price
	// update silently rewrite what was predicted -- which is exactly the
	// hindsight the log exists to make impossible. The snapshot is frozen here
	// and the board is free to move underneath it.
	b := betlog.Bet{
		Selection: req.Selection,
		Price:     wager.American(req.Price),
		Bankroll:  req.Bankroll,
		Stake:     req.Stake,
		Predicted: req.Predicted,
		Narrative: req.Narrative,
	}
	// Book is deliberately left empty. betlog rejects an unknown book, and
	// while Fanatics is now recorded in wager.Book, the campaign's existing
	// nine entries all omit it -- adding it to new ones only would split the
	// log's history against itself for no gain.

	id, err := betlog.PlaceBet(s.betlogPath, b)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// placedTeams reads the log and returns the team codes already committed.
//
// This is what removes -exclude as something to remember. A wager already
// riding a game makes that game unavailable, and the log is the record of what
// is riding. Teams are recovered from the selection text, which is written by
// the report and so follows a known shape ("CAR ML + GB ML 2-leg parlay").
func (s *boardServer) placedTeams() []string {
	bets, err := betlog.Load(s.betlogPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, b := range bets {
		// A settled wager no longer ties up its game.
		if b.Result != "" && b.Result != betlog.Open {
			continue
		}
		for _, f := range strings.Fields(b.Bet.Selection) {
			// Team codes are the all-caps 2-3 letter words. "ML", "2-leg" and
			// "(Week" are not, and neither is a lowercase word.
			if len(f) < 2 || len(f) > 3 || f != strings.ToUpper(f) || f == "ML" {
				continue
			}
			ok := true
			for _, r := range f {
				if r < 'A' || r > 'Z' {
					ok = false
					break
				}
			}
			if ok && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

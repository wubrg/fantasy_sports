package main

import (
	"edge/internal/ledger"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// Two different questions, kept apart. openEV is the EXPECTED value of what
	// is still live; it must fall to zero as bets settle. realized is the P&L
	// already booked. Summing EV across settled bets (as this once did) made the
	// "expected" figure never move when a bet was settled -- it was answering
	// neither question.
	var open, openStaked, openStakedCash, openStakedBonus, openEV, realized float64
	for _, b := range bets {
		res := string(b.Result)
		if res == "" {
			res = "open"
		}
		if res == "open" {
			open++
			openStaked += b.Bet.Stake
			// Real money and a bonus bet are not the same exposure: a lost bonus
			// bet costs no cash, so the two are summed apart.
			if mustBankroll(b.Bet.Bankroll) == wager.BonusBet {
				openStakedBonus += b.Bet.Stake
			} else {
				openStakedCash += b.Bet.Stake
			}
			if ev, err := wager.EV(mustBankroll(b.Bet.Bankroll), b.Bet.Predicted, b.Bet.Price, b.Bet.Stake); err == nil {
				openEV += ev
			}
		} else {
			realized += realizedPnL(b.Bet.Bankroll, b.Result, b.Bet.Price, b.Bet.Stake)
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
		"path": path, "entries": out, "count": len(out), "open": open,
		// staked/ev keep their names but now carry OPEN semantics, so anything
		// still reading them sees live exposure rather than an all-time sum.
		"staked": openStaked, "ev": openEV,
		"open_staked": openStaked, "open_staked_cash": openStakedCash,
		"open_staked_bonus": openStakedBonus, "open_ev": openEV, "realized": realized,
	})
}

// realizedPnL is the cash already won or lost on a SETTLED wager. A won bet of
// either kind books its profit (stake x the price's profit multiple); the
// difference is the loss: real money loses the stake, a bonus bet loses nothing
// because the stake was never the bettor's. Push and void are washes.
func realizedPnL(bankroll string, result betlog.Result, price wager.American, stake float64) float64 {
	switch result {
	case betlog.Won:
		pm, err := price.ProfitMultiple()
		if err != nil {
			return 0
		}
		return stake * pm
	case betlog.Lost:
		if mustBankroll(bankroll) == wager.BonusBet {
			return 0
		}
		return -stake
	default: // push, void, open
		return 0
	}
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
	// Book is where it was placed. With it, the stake is drawn from that
	// book's balance so the bankroll follows the bet; without it the wager is
	// still recorded and the balance simply is not touched.
	Book string `json:"book"`
	// Week is the NFL week this wager is FOR. The board sends the week it is
	// showing; the period report attributes the bet by it rather than by the
	// date it was logged. Zero (unsent) leaves the bet untagged.
	Week int `json:"week"`
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
		Week:      req.Week,
	}
	// Book is deliberately left empty. betlog rejects an unknown book, and
	// while Fanatics is now recorded in wager.Book, the campaign's existing
	// nine entries all omit it -- adding it to new ones only would split the
	// log's history against itself for no gain.

	// The bankroll is debited BEFORE the prediction is written.
	//
	// Order matters and this is the safe one. A ledger draw that fails leaves
	// no betlog entry, so the operator retries and nothing is lost. The
	// reverse -- log the bet, then fail to debit -- leaves a prediction with
	// no funding behind it, and the two logs disagree with no record of why.
	draws, drawErr := s.debit(req.Book, assetForBankroll(req.Bankroll), req.Stake)
	if drawErr != nil {
		httpError(w, http.StatusConflict, drawErr.Error())
		return
	}

	id, err := betlog.PlaceBet(s.betlogPath, b)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Tie the ledger events to the wager now that it has an id. Several draws
	// can share one, which is how a stake spanning two lots stays one wager.
	for _, ev := range draws {
		ev.Wager = id
		ev.Week = req.Week
		if err := ledger.AppendFile(s.ledgerPath, ev); err != nil {
			// The bet is already recorded, so this cannot be undone by
			// refusing. Say what is inconsistent rather than pretending.
			httpError(w, http.StatusInternalServerError, fmt.Sprintf(
				"the wager was recorded as %s but the bankroll could not be debited: %v", id, err))
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "id": id, "debited": len(draws) > 0})
}

type settleReq struct {
	ID     string `json:"id"`
	Result string `json:"result"`
	Note   string `json:"note"`
}

// handleSettle records an outcome against an already-logged prediction.
//
// Settling APPENDS; it never rewrites the bet. That is the property the whole
// log turns on -- a prediction that could be edited after the result is known
// is not a prediction -- and it is why this endpoint takes an id and a result
// and nothing else. There is deliberately no way here to correct a price or a
// predicted probability: if one of those is wrong, the honest repair is a note
// on the record, not a quiet overwrite.
func (s *boardServer) handleSettle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req settleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == "" {
		httpError(w, http.StatusBadRequest, "which bet? an id is required")
		return
	}

	// Refuse to settle something already settled. betlog would append it
	// happily and Load folds the last one on top, so a double tap on a phone
	// could quietly flip a win to a loss.
	bets, err := betlog.Load(s.betlogPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	found := false
	for _, b := range bets {
		if b.ID != req.ID {
			continue
		}
		found = true
		if b.Result != "" && b.Result != betlog.Open {
			httpError(w, http.StatusConflict, fmt.Sprintf(
				"%s is already settled as %q; settling again would append a second "+
					"outcome and the later one would win", req.ID, b.Result))
			return
		}
	}
	if !found {
		httpError(w, http.StatusNotFound, "no bet with id "+req.ID)
		return
	}

	if err := betlog.Settle(s.betlogPath, req.ID, betlog.Result(req.Result), req.Note); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": req.ID, "result": req.Result})
}

// debit prepares the ledger events for a stake, or nothing when no book was
// named or no bankroll is recorded.
//
// Recording a wager against an unknown bankroll is normal -- the ledger is
// opt-in, and a board can be used without one -- so its absence is not an
// error. An insufficient balance IS: it means the operator believes they hold
// money they do not, and that is worth stopping for.
// assetForBankroll maps a betlog bankroll to the ledger asset a place should
// draw from: a bonus bet spends bonus, everything else spends cash.
func assetForBankroll(bankroll string) string {
	if strings.EqualFold(strings.TrimSpace(bankroll), "bonus bet") {
		return ledger.Bonus
	}
	return ledger.Cash
}

func (s *boardServer) debit(book, asset string, stake float64) ([]ledger.Event, error) {
	if strings.TrimSpace(book) == "" {
		return nil, nil
	}
	if _, err := os.Stat(s.ledgerPath); os.IsNotExist(err) {
		return nil, nil
	}
	now := time.Now()
	draws, err := s.drawFrom(book, asset, stake)
	if err != nil {
		return nil, err
	}
	for i := range draws {
		draws[i].ID = ledger.NewID(now, book+"-place")
		draws[i].Time = now
	}
	return draws, nil
}

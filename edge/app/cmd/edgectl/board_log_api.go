package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"edge/internal/betlog"
	"edge/internal/journal"
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
	Book      string  `json:"book,omitempty"` // empty for bets placed before this was tracked, or at a book with no rules recorded (see handlePlace)
	Bankroll  string  `json:"bankroll"`
	Predicted float64 `json:"predicted"`
	Result    string  `json:"result"`
	Narrative string  `json:"narrative"`
	Week      int     `json:"week"`
	Payout    float64 `json:"payout"` // profit a win pays: stake x profit multiple
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

	// An optional ?week filters the log to the bets FOR that NFL week, so the
	// listing and its at-risk/realized figures both scope to the week the header
	// selector is on. week 0 (absent) shows every bet.
	week, _ := strconv.Atoi(r.URL.Query().Get("week"))

	out := make([]logEntryJSON, 0, len(bets))
	// Two different questions, kept apart. openEV is the EXPECTED value of what
	// is still live; it must fall to zero as bets settle. realized is the P&L
	// already booked. Summing EV across settled bets (as this once did) made the
	// "expected" figure never move when a bet was settled -- it was answering
	// neither question.
	var open, openStaked, openStakedCash, openStakedBonus, openEV, openPayout, realized float64
	for _, b := range bets {
		if week > 0 && b.Bet.Week != week {
			continue // not this week's wager
		}
		res := string(b.Result)
		if res == "" {
			res = "open"
		}
		// Potential payout: the profit a win pays (stake x the price's profit
		// multiple), the same for cash and a bonus bet. Deterministic -- no belief
		// needed -- so it is always meaningful, unlike EV.
		var payout float64
		if pm, err := b.Bet.Price.ProfitMultiple(); err == nil {
			payout = b.Bet.Stake * pm
		}
		if res == "open" {
			open++
			openStaked += b.Bet.Stake
			openPayout += payout
			// Real money and a bonus bet are not the same exposure: a lost bonus
			// bet costs no cash, and a lost no-sweat costs only the part of its
			// stake the refund does not convert -- so each is summed by what is
			// genuinely at risk, not by nominal stake.
			switch mustBankroll(b.Bet.Bankroll) {
			case wager.BonusBet:
				openStakedBonus += b.Bet.Stake
			case wager.NoSweat:
				openStakedCash += (1 - wager.NoSweatConversion) * b.Bet.Stake
			default:
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
			Book:     string(b.Bet.Book),
			Bankroll: b.Bet.Bankroll, Predicted: b.Bet.Predicted,
			Result: res, Narrative: b.Bet.Narrative, Week: b.Bet.Week,
			Payout: payout,
		})
	}
	// Newest first: the log is read to check what was just recorded far more
	// often than to review the beginning of a campaign.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	writeJSON(w, map[string]any{
		"path": path, "week": week, "entries": out, "count": len(out), "open": open,
		// staked/ev keep their names but now carry OPEN semantics, so anything
		// still reading them sees live exposure rather than an all-time sum.
		"staked": openStaked, "ev": openEV,
		"open_staked": openStaked, "open_staked_cash": openStakedCash,
		"open_staked_bonus": openStakedBonus, "open_ev": openEV,
		"open_payout": openPayout, "realized": realized,
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
		switch mustBankroll(bankroll) {
		case wager.BonusBet:
			return 0
		case wager.NoSweat:
			// The stake refunds as a free bet; only the unconverted part is lost.
			return -(1 - wager.NoSweatConversion) * stake
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
	// Boost is an optional boost lot id to apply to this wager; the core
	// validates and consumes it.
	Boost string `json:"boost"`
	// Deposit, when true, funds the wager with a matching grant of the stake
	// (in the bankroll's asset) to Book before the debit -- so a bet can be
	// logged against fresh funds without a separate declare step. No effect
	// without a book.
	Deposit bool `json:"deposit"`
	// Legs records what makes up a multi-leg (e.g. same-game parlay) wager --
	// purely additive detail alongside the combined Selection/Price/Stake
	// above, same as betlog.Bet.Legs. Optional: a single-leg bet omits it.
	Legs []legReq `json:"legs"`
}

// legReq is one leg of a multi-leg wager, over the wire. It carries the same
// two fields as the CLI's "selection:price,selection:price" -legs convention
// (see parseBetLegs in bet.go) as a JSON array instead of that string
// encoding, since the client already builds legs as a list of form rows.
type legReq struct {
	Selection string `json:"selection"`
	Price     int    `json:"price"`
}

// toBetlogLegs converts the wire legs into betlog.Leg. nil in, nil out: an
// empty/absent Legs leaves the bet exactly as it was before legs existed.
func toBetlogLegs(legs []legReq) []betlog.Leg {
	if len(legs) == 0 {
		return nil
	}
	out := make([]betlog.Leg, len(legs))
	for i, l := range legs {
		out[i] = betlog.Leg{Selection: l.Selection, Price: wager.American(l.Price)}
	}
	return out
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
		Legs:      toBetlogLegs(req.Legs),
	}
	// Book is frozen onto the bet only when it's a book betlog.PlaceBet will
	// accept -- one wager.Book already has bonus-bet rules recorded for
	// (Known()). A book with no rules recorded (anything not in
	// wager.KnownBooks) must stay off the bet: setting it would make
	// PlaceBet reject the whole wager outright (see its Book validation),
	// turning "the log tab doesn't show the book" into "you can't log this
	// bet at all". req.Book still drives the ledger debit via journal.Place
	// either way; this only decides what gets frozen into the log entry
	// itself.
	if bk := wager.Book(strings.TrimSpace(req.Book)); bk.Known() {
		b.Book = bk
	}
	//
	// journal.Place writes the betlog and the ledger debit as one operation, in
	// the safe order: a failed debit leaves no betlog entry, so the two logs can
	// never disagree with no record of why.
	id, err := journal.Place(s.betlogPath, s.ledgerPath, journal.PlaceRequest{
		Bet: b, Book: req.Book, BoostLotID: req.Boost, Deposit: req.Deposit,
	}, time.Now())
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

type settleReq struct {
	ID     string `json:"id"`
	Result string `json:"result"`
	Note   string `json:"note"`
	// SettledPrice is what the book actually recomputed a wager's price to at
	// settlement -- e.g. a same-game parlay repriced after one leg voided out
	// from under it (an injury). It does not rewrite the bet's originally
	// quoted price (that stays frozen, same as ever); it is new information
	// recorded AT settlement, the same thing the CLI's `-settled-price` already
	// does. Optional: zero/omitted settles against the original quote,
	// unchanged from before this field existed.
	SettledPrice int `json:"settled_price"`
}

// handleSettle records an outcome against an already-logged prediction.
//
// Settling APPENDS; it never rewrites the bet. That is the property the whole
// log turns on -- a prediction that could be edited after the result is known
// is not a prediction. There is deliberately no way here to correct the
// ORIGINAL price or predicted probability: if one of those is wrong, the
// honest repair is a note on the record, not a quiet overwrite. SettledPrice
// is a different thing -- what the book actually paid, which is not knowable
// until settlement -- and recording it here is what lets a repriced SGP (a
// leg voided by an injury, the rest still live) settle against its REAL
// payout instead of the stale original quote.
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

	var settledPrice *wager.American
	if req.SettledPrice != 0 {
		p := wager.American(req.SettledPrice)
		settledPrice = &p
	}

	// journal.Settle appends the betlog outcome and, when an at-risk ledger
	// place exists for this wager, the ledger settle too -- so the bankroll
	// clears alongside the prediction. It refuses a double settle, since the
	// betlog folds the last outcome on top and a second tap could flip a result.
	if err := journal.Settle(s.betlogPath, s.ledgerPath, req.ID, betlog.Result(req.Result), settledPrice, nil, nil, req.Note, time.Now()); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": req.ID, "result": req.Result})
}

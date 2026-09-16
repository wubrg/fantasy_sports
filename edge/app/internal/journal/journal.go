// Package journal is the one place a wager is placed or settled. It writes the
// betlog (the prediction/log the GUI shows) and the ledger (the bankroll) as a
// single operation, so the two can never disagree. It sits above betlog and
// ledger; neither imports it.
package journal

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
)

// PlaceRequest is a wager to record. Bet is the frozen snapshot written to the
// betlog; Book is where it was placed and drives the ledger debit; BoostLotID,
// when set, names a boost lot to consume against this wager.
type PlaceRequest struct {
	Bet        betlog.Bet
	Book       string
	BoostLotID string
}

// AssetForBankroll maps a betlog bankroll to the ledger asset a place draws
// from: a bonus bet spends bonus, everything else spends cash.
func AssetForBankroll(bankroll string) string {
	if strings.EqualFold(strings.TrimSpace(bankroll), "bonus bet") {
		return ledger.Bonus
	}
	return ledger.Cash
}

// Place records a wager. It prepares and validates the ledger debit first, then
// writes the betlog, then ties the ledger draws (and any boost consumption) to
// the new wager id. The order matters: a failed debit leaves no betlog entry.
func Place(betlogPath, ledgerPath string, req PlaceRequest, now time.Time) (string, error) {
	if req.Bet.Stake <= 0 {
		return "", fmt.Errorf("journal: stake must be positive")
	}

	draws, err := draw(ledgerPath, req.Book, AssetForBankroll(req.Bet.Bankroll), req.Bet.Stake, now)
	if err != nil {
		return "", err
	}

	stakeIsCash := AssetForBankroll(req.Bet.Bankroll) == ledger.Cash
	if strings.TrimSpace(req.BoostLotID) != "" {
		if err := validateBoostLot(ledgerPath, req.BoostLotID, req.Book, stakeIsCash, now); err != nil {
			return "", err
		}
	}

	id, err := betlog.PlaceBet(betlogPath, req.Bet)
	if err != nil {
		return "", err
	}

	for _, ev := range draws {
		ev.Wager = id
		ev.Week = req.Bet.Week
		if err := ledger.AppendFile(ledgerPath, ev); err != nil {
			return "", fmt.Errorf("wager %s was recorded but the bankroll could not be debited: %w", id, err)
		}
	}

	if strings.TrimSpace(req.BoostLotID) != "" {
		// A boost applied to a wager is a KindPlace of the boost lot tied to that
		// wager, not a KindExpire. The ledger models "boost committed to a bet" as
		// a place (see the Event.Wager comment: one place spends the cash stake,
		// another spends the boost), and only a place -- recorded as a commitment
		// -- runs the RequiresCashStake safety check at settlement. Recording it as
		// an expire ("died unused") would bypass that check and misstate what
		// happened to the token. The boost is a unit lot, so the place carries
		// Amount 0; it spends the token whole.
		place := ledger.Event{
			Kind: ledger.KindPlace, ID: ledger.NewID(now, req.Book+"-boost-used"), Time: now,
			Lot: req.BoostLotID, Wager: id, Amount: 0, Week: req.Bet.Week,
			Note: "boost applied to " + id,
		}
		if err := ledger.AppendFile(ledgerPath, place); err != nil {
			return "", fmt.Errorf("wager %s was recorded but the boost could not be consumed: %w", id, err)
		}
	}

	return id, nil
}

// Settle records an outcome for a wager. It appends the betlog settle and, when
// an at-risk ledger place exists for the wager, a ledger settle too. A wager
// recorded without a book has no ledger place; settling it touches only the
// betlog. Double settles are refused: the betlog folds the last outcome on top,
// so a second tap could quietly flip a result.
func Settle(betlogPath, ledgerPath string, id string, result betlog.Result, returns *ledger.Lot, note string, now time.Time) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("journal: which bet? an id is required")
	}
	bets, err := betlog.Load(betlogPath)
	if err != nil {
		return err
	}
	found := false
	var bet betlog.Bet
	for _, b := range bets {
		if b.ID != id {
			continue
		}
		found = true
		bet = b.Bet
		if b.Result != "" && b.Result != betlog.Open {
			return fmt.Errorf("%s is already settled as %q; settling again would append a second outcome", id, b.Result)
		}
	}
	if !found {
		return fmt.Errorf("no bet with id %s", id)
	}

	if err := betlog.Settle(betlogPath, id, result, note); err != nil {
		return err
	}

	// Only write a ledger settle if a place for this wager exists.
	if _, statErr := os.Stat(ledgerPath); statErr == nil {
		evs, err := ledger.Load(ledgerPath)
		if err != nil {
			return err
		}
		// Resolve each lot's book from the events that created it, so a returns lot
		// can be booked to the same place the stake was drawn from. betlog does not
		// record the book, so it must come from the ledger.
		//
		// A lot is born two ways: a deposit/grant/convert mints it via Creates, and
		// a winning settle pays it out via Returns. Both must be mapped -- a stake
		// drawn from earlier winnings references a Returns-created lot, and missing
		// that leaves the computed returns lot with no book (the settle then fails).
		bookByLot := map[string]string{}
		record := func(l *ledger.Lot, eventID string) {
			if l == nil {
				return
			}
			lid := l.ID
			if lid == "" {
				lid = eventID
			}
			bookByLot[lid] = l.Book
		}
		for _, e := range evs {
			record(e.Creates, e.ID)
			record(e.Returns, e.ID)
		}
		hasPlace := false
		placeBook := ""
		for _, e := range evs {
			if e.Kind == ledger.KindPlace && e.Wager == id {
				hasPlace = true
				if placeBook == "" {
					placeBook = bookByLot[e.Lot]
				}
			}
		}
		if hasPlace {
			// When the caller passed no returns (the GUI path) and the result won,
			// pushed, or voided, compute what the book handed back from the wager's
			// own recorded price and stake. A caller-supplied returns (the CLI
			// -returns path) is authoritative and used unchanged. A loss returns
			// nothing, correctly, at zero.
			ret := returns
			if ret == nil {
				lot, err := computeReturns(bet, result, placeBook, now, id)
				if err != nil {
					return err
				}
				ret = lot // nil when the computed amount is zero
			}
			settle := ledger.Event{
				Kind: ledger.KindSettle, ID: ledger.NewID(now, "settle-"+id), Time: now,
				Wager: id, Result: ledger.Result(result), Returns: ret, Note: note,
			}
			if err := ledger.AppendFile(ledgerPath, settle); err != nil {
				return fmt.Errorf("betlog was settled but the ledger settle failed: %w", err)
			}
		}
	}
	return nil
}

// computeReturns derives the returns lot for a settling wager from its recorded
// price and stake, for the caller (the GUI) that does not pass an explicit
// returns. Winnings and refunds always land as CASH, at the book the stake was
// placed from.
//
// The result is nil (no lot, no error) when nothing is realized: a loss, or a
// bonus-funded push/void. A zero-amount returns lot is never written -- but the
// caller still writes the settle event itself, so the at-risk stake is cleared.
//
//   - won, cash bankroll:   stake back + profit  = stake + stake*pm
//   - won, bonus bankroll:  profit only          = stake*pm   (the bonus stake
//     was the book's money and is not returned; the winnings pay as cash)
//   - push/void, cash:      stake back           = stake
//   - push/void, bonus:     0 (nothing realized -- a bonus stake was never the
//     bettor's money, so a refunded-or-not bonus push realizes no cash here;
//     any re-granted bonus token is a separate grant event, not a return)
//   - lost:                 no returns
func computeReturns(bet betlog.Bet, result betlog.Result, book string, now time.Time, id string) (*ledger.Lot, error) {
	cash := AssetForBankroll(bet.Bankroll) == ledger.Cash

	var amount float64
	switch result {
	case betlog.Won:
		pm, err := bet.Price.ProfitMultiple()
		if err != nil {
			return nil, fmt.Errorf("cannot compute returns for %s: %w", id, err)
		}
		if cash {
			amount = bet.Stake + bet.Stake*pm // stake back plus profit
		} else {
			amount = bet.Stake * pm // profit only; bonus stake is not returned
		}
	case betlog.Pushed, betlog.Void:
		if cash {
			amount = bet.Stake // stake back
		}
		// bonus push/void realizes nothing; amount stays 0.
	default:
		// A loss (or anything else) returns nothing.
		return nil, nil
	}

	if amount <= 0 {
		return nil, nil
	}
	return &ledger.Lot{
		ID:     ledger.NewID(now, "returns-"+id),
		Book:   book,
		Asset:  ledger.Cash,
		Amount: amount,
	}, nil
}

// validateBoostLot confirms a boost lot named by id is still live, belongs to
// book, and -- when it requires a real-money stake -- is being applied to a cash
// wager rather than a bonus bet, before anything is written. Mirrors draw()'s
// validate-then-write discipline: without this check, a stale, already-consumed,
// or nonexistent BoostLotID would let Place return success while appending an
// unappliable line into the append-only ledger, breaking every future replay of
// that file.
//
// The RequiresCashStake check is enforced here, at placement, so a boost that
// cannot legally attach to this bet is refused BEFORE the boost lot is burned --
// the token is not consumed on a bet that could never carry it. (Replay also
// enforces it at settlement, as a second line of defence; this catches it early
// and keeps the token.)
func validateBoostLot(ledgerPath, id, book string, stakeIsCash bool, now time.Time) error {
	events, err := ledger.Load(ledgerPath)
	if err != nil {
		return err
	}
	pos, err := ledger.Balances(events, now)
	if err != nil {
		return err
	}
	for _, l := range pos.Lots {
		if l.ID == id {
			if l.Book != book {
				return fmt.Errorf("boost lot %q belongs to %s, not %s: a boost cannot be applied to a wager at a different book", id, l.Book, book)
			}
			if l.Boost != nil && l.Boost.RequiresCashStake && !stakeIsCash {
				return fmt.Errorf("boost lot %q requires a real-money stake, but this wager is funded from a bonus bet: a profit boost will not attach to a bonus bet", id)
			}
			return nil
		}
	}
	return fmt.Errorf("boost lot %q is not a live lot in the ledger (already consumed, expired, or never granted)", id)
}

// draw builds the ledger place events for a stake, oldest-deadline lot first.
// It returns nil (no error) when no book is named or no ledger exists: the
// bankroll is opt-in and a bet can be recorded without one. An insufficient
// balance IS an error — it means money is believed held that is not.
func draw(ledgerPath, book, asset string, stake float64, now time.Time) ([]ledger.Event, error) {
	if strings.TrimSpace(book) == "" {
		return nil, nil
	}
	if _, err := os.Stat(ledgerPath); os.IsNotExist(err) {
		return nil, nil
	}
	events, err := ledger.Load(ledgerPath)
	if err != nil {
		return nil, err
	}
	pos, err := ledger.Balances(events, now)
	if err != nil {
		return nil, err
	}

	var open []ledger.Lot
	for _, l := range pos.Lots {
		if l.Book == book && l.Asset == asset && !l.Unit() && l.Amount > 0 {
			open = append(open, l)
		}
	}
	sort.SliceStable(open, func(i, j int) bool {
		ei, ej := open[i].Expires, open[j].Expires
		switch {
		case ei != nil && ej != nil:
			return ei.Before(*ej)
		case ei != nil:
			return true
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
		return nil, fmt.Errorf("%s holds %.2f of %s, which will not cover a %.2f stake", book, total, asset, stake)
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
		out = append(out, ledger.Event{
			Kind: ledger.KindPlace, ID: ledger.NewID(now, book+"-place"), Time: now,
			Lot: l.ID, Amount: take,
		})
		left -= take
	}
	return out, nil
}

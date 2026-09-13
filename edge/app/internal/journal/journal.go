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

	if strings.TrimSpace(req.BoostLotID) != "" {
		if err := validateBoostLot(ledgerPath, req.BoostLotID, req.Book, now); err != nil {
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
		exp := ledger.Event{
			Kind: ledger.KindExpire, ID: ledger.NewID(now, req.Book+"-boost-used"), Time: now,
			Lot: req.BoostLotID, Wager: id, Week: req.Bet.Week,
			Note: "boost applied to " + id,
		}
		if err := ledger.AppendFile(ledgerPath, exp); err != nil {
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
	for _, b := range bets {
		if b.ID != id {
			continue
		}
		found = true
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
		hasPlace := false
		for _, e := range evs {
			if e.Kind == ledger.KindPlace && e.Wager == id {
				hasPlace = true
				break
			}
		}
		if hasPlace {
			settle := ledger.Event{
				Kind: ledger.KindSettle, ID: ledger.NewID(now, "settle-"+id), Time: now,
				Wager: id, Result: ledger.Result(result), Returns: returns, Note: note,
			}
			if err := ledger.AppendFile(ledgerPath, settle); err != nil {
				return fmt.Errorf("betlog was settled but the ledger settle failed: %w", err)
			}
		}
	}
	return nil
}

// validateBoostLot confirms a boost lot named by id is still live and belongs
// to book, before anything is written. Mirrors draw()'s validate-then-write
// discipline: without this check, a stale, already-consumed, or nonexistent
// BoostLotID would let Place return success while appending an unappliable
// KindExpire line into the append-only ledger, breaking every future replay of
// that file.
func validateBoostLot(ledgerPath, id, book string, now time.Time) error {
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

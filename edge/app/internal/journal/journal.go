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

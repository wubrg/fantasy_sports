package wager

import "fmt"

// Bankroll distinguishes cash from promotional credit. The two use different
// EV formulas and want opposite things from a price, so nothing in this
// package computes an expected value without being told which one it has.
//
// Getting this wrong is not hypothetical: edge-of-vigor.pdf Tables D and E
// apply the -200 profit multiple to the -500, -400 and -300 rows of a
// bonus-bet table, and the resulting figures invert the document's own thesis.
type Bankroll int

const (
	// RealMoney is cash. The stake is returned on a win and lost on a loss,
	// so EV carries a downside term and the wager needs a genuine edge to
	// clear the vig.
	RealMoney Bankroll = iota

	// BonusBet is a Stake Not Returned promotional credit. The stake was
	// never the bettor's money and is not returned on a win, so there is no
	// downside term. A bonus bet is positive EV on *any* market; the only
	// question is how much of its face value converts to cash.
	BonusBet
)

func (b Bankroll) String() string {
	switch b {
	case RealMoney:
		return "real money"
	case BonusBet:
		return "bonus bet"
	default:
		return fmt.Sprintf("Bankroll(%d)", int(b))
	}
}

func (b Bankroll) validate() error {
	if b == RealMoney || b == BonusBet {
		return nil
	}
	return fmt.Errorf("wager: unknown bankroll %d", int(b))
}

// EV computes expected value for either bankroll. Prefer this over calling
// EVRealMoney or EVBonusBet directly when the bankroll is a variable, so the
// choice of formula can never drift from the choice of funds.
func EV(b Bankroll, p float64, odds American, stake float64) (float64, error) {
	if err := b.validate(); err != nil {
		return 0, err
	}
	if b == BonusBet {
		return EVBonusBet(p, odds, stake)
	}
	return EVRealMoney(p, odds, stake)
}

// Book identifies a sportsbook. It exists because bonus-bet rules differ
// between books in ways that change which markets are safe to use them on --
// differences that are invisible in the odds themselves.
type Book string

const (
	FanDuel    Book = "fanduel"
	DraftKings Book = "draftkings"
	BetMGM     Book = "betmgm"
	Bet365     Book = "bet365"
)

// BonusLostOnPush reports whether a bonus bet is forfeited when the wager
// pushes (ties).
//
// FanDuel keeps the bonus bet on a push; DraftKings returns it. For a cash
// wager a push is a neutral non-event, so this rule is easy to overlook -- but
// for a FanDuel bonus bet a push is a total loss of the asset.
func (b Book) BonusLostOnPush() bool {
	return b == FanDuel
}

// BonusSplittable reports whether a bonus bet may be wagered in parts.
// FanDuel allows it; DraftKings requires the token be used whole, which
// creates a liquidity problem when hedging a large denomination.
func (b Book) BonusSplittable() bool {
	return b == FanDuel
}

// CheckBonusMarket rejects bonus bets on markets that can push at books which
// forfeit the bonus on a push. Whole-number spreads and totals (-3, -7, O/U
// 44) can push; half-point lines and most player-prop overs cannot.
//
// This is a hard eligibility rule rather than a preference. The downside is
// not a reduced conversion rate, it is losing the entire asset for nothing.
func CheckBonusMarket(b Book, marketCanPush bool) error {
	if marketCanPush && b.BonusLostOnPush() {
		return fmt.Errorf(
			"wager: %s forfeits the bonus bet on a push, and this market can push "+
				"(use a half-point line, a two-way moneyline, or place it on a book that returns the bonus)",
			b)
	}
	return nil
}

package wager

import (
	"fmt"
	"math"
)

// finite reports whether f is a real number.
//
// This exists because NaN defeats every ordinary range check: `p < 0 || p > 1`
// is FALSE for NaN, so a NaN sailed through every validator in this package and
// came out the far end as a printed price of -9223372036854775808. An audit
// found it reachable from the command line, since strconv.ParseFloat accepts
// "NaN" and "Inf" and every numeric flag goes through it.
//
// Range checks must therefore test finiteness first, not after.
func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// errNotReal is the shared message for a non-finite input, so every validator
// in this package reports the same failure the same way.
func errNotReal(name string, v float64) error {
	return fmt.Errorf("wager: %s = %v is not a real number", name, v)
}

// validProb rejects anything that is not a probability. A caller who has no
// estimate must not pass 0 — that is a claim the event never happens.
func validProb(p float64) error {
	if !finite(p) {
		return errNotReal("probability", p)
	}
	if p < 0 || p > 1 {
		return fmt.Errorf("wager: probability %v out of range [0,1]", p)
	}
	return nil
}

func validStake(stake float64) error {
	if !finite(stake) {
		return errNotReal("stake", stake)
	}
	if stake <= 0 {
		return fmt.Errorf("wager: stake %v must be positive", stake)
	}
	return nil
}

// EVRealMoney is the expected value of a cash wager.
//
//	EV = p·stake·(d−1) − (1−p)·stake
//
// The stake comes back on a win and is lost on a loss, so both terms are
// present. p is YOUR estimate of the true probability, not the book's implied
// probability — passing the implied probability yields zero by construction.
func EVRealMoney(p float64, odds American, stake float64) (float64, error) {
	if err := validProb(p); err != nil {
		return 0, err
	}
	if err := validStake(stake); err != nil {
		return 0, err
	}
	profit, err := odds.ProfitMultiple()
	if err != nil {
		return 0, err
	}
	return p*stake*profit - (1-p)*stake, nil
}

// EVBonusBet is the expected value of a Stake Not Returned (SNR) bonus bet.
//
//	EV = p·stake·(d−1)
//
// There is no downside term. The stake was never the bettor's money and is not
// returned on a win, so a loss costs nothing in cash. This asymmetry is the
// whole reason bonus bets belong on longshots and real money does not: EV
// rises without bound as the odds lengthen.
//
// Using EVRealMoney for a bonus bet understates its value by exactly
// (1−p)·stake.
func EVBonusBet(p float64, odds American, stake float64) (float64, error) {
	if err := validProb(p); err != nil {
		return 0, err
	}
	if err := validStake(stake); err != nil {
		return 0, err
	}
	profit, err := odds.ProfitMultiple()
	if err != nil {
		return 0, err
	}
	return p * stake * profit, nil
}

// BonusConversionAtFairOdds is the closed form for a bonus bet priced fairly:
//
//	EV = stake · (1 − p)
//
// A bonus bet is worth its face value times the probability it LOSES. None of
// the source documents state this, though all their worked examples satisfy
// it. It makes "longshots convert better" immediate: as p falls, value rises,
// approaching the full face value in the limit.
//
// This is a CEILING. It assumes the price carries no vig. Real longshot
// markets are heavily juiced, so realised conversion typically lands at
// 60–80% of face value rather than the 90%+ a fair +1000 line would imply.
func BonusConversionAtFairOdds(odds American, stake float64) (float64, error) {
	if err := validStake(stake); err != nil {
		return 0, err
	}
	p, err := odds.ImpliedRaw()
	if err != nil {
		return 0, err
	}
	return stake * (1 - p), nil
}

// EdgeReport is everything the operative prompt template needs for one side of
// one market. Every field is computed here so no model ever has to derive one.
type EdgeReport struct {
	Odds         American
	ImpliedRaw   float64 // hurdle rate: win rate needed to break even
	FairDevig    float64 // market's actual estimate, vig removed
	Breakeven    float64 // == ImpliedRaw, named for the question it answers
	Hold         float64 // book's margin on balanced action
	PTrue        float64 // caller's estimate
	EVReal       float64 // per unit stake
	EVBonus      float64 // per unit stake
	ClearsHurdle bool    // PTrue > ImpliedRaw
	BeatsMarket  bool    // PTrue > FairDevig
}

// Report computes the full picture for one side of a two-sided market.
//
// It deliberately requires BOTH sides: without the opposing price the vig
// cannot be measured, so FairDevig and Hold would be unknowable. Returning
// them as zero would be worse than refusing.
func Report(m Market, pTrue float64, stake float64) (EdgeReport, error) {
	if err := validProb(pTrue); err != nil {
		return EdgeReport{}, err
	}
	raw, err := m.A.ImpliedRaw()
	if err != nil {
		return EdgeReport{}, err
	}
	fair, _, err := m.FairDevig()
	if err != nil {
		return EdgeReport{}, err
	}
	hold, err := m.Hold()
	if err != nil {
		return EdgeReport{}, err
	}
	evReal, err := EVRealMoney(pTrue, m.A, stake)
	if err != nil {
		return EdgeReport{}, err
	}
	evBonus, err := EVBonusBet(pTrue, m.A, stake)
	if err != nil {
		return EdgeReport{}, err
	}
	return EdgeReport{
		Odds:         m.A,
		ImpliedRaw:   raw,
		FairDevig:    fair,
		Breakeven:    raw,
		Hold:         hold,
		PTrue:        pTrue,
		EVReal:       evReal,
		EVBonus:      evBonus,
		ClearsHurdle: pTrue > raw,
		BeatsMarket:  pTrue > fair,
	}, nil
}

// Hedge is a bonus bet converted to guaranteed cash by backing the opposite
// side elsewhere.
type Hedge struct {
	// Stake is the cash to put on the opposing outcome.
	Stake float64
	// Profit is what is kept whichever way the game goes.
	Profit float64
	// Conversion is Profit as a share of the bonus bet's face value. The
	// analytical-hobbyist framework calls 70%+ the goal, 60% acceptable, and
	// below 50% highly inefficient.
	Conversion float64
	// Hold is the combined vig across the two books. It is the whole story:
	// conversion is 100% determined by it, which is why a low-hold market
	// matters far more than a clever choice of side.
	Hold float64
}

// ConvertBonus computes the cash hedge that turns a bonus bet into guaranteed
// profit, given the price it was placed at and the price available on the
// other side at a DIFFERENT book.
//
// A stake-not-returned bonus bet pays face x m if it wins and costs nothing if
// it loses, so backing the other side for H equalises the two outcomes when
//
//	face x m_back  -  H  =  H x m_hedge
//	          H     =  face x m_back / decimal_hedge
//
// The guaranteed profit is then H x m_hedge, whichever result lands.
//
// Two things follow, and both are in the framework. The bonus bet still
// belongs on the LONGSHOT -- the hedge does not change what a bonus bet is
// worth, only its variance -- and the conversion rate is decided entirely by
// the combined hold of the two prices. A high-hold market cannot be rescued by
// sizing the hedge well.
//
// The hedge must be at a different book. Backing both sides at one is the
// "cardinal sin" of Part 5: it is the single clearest signal of an arber and
// is what gets an account promo-banned.
func ConvertBonus(face float64, back, hedge American) (Hedge, error) {
	if face <= 0 {
		return Hedge{}, fmt.Errorf("wager: bonus face %v must be positive", face)
	}
	mb, err := back.ProfitMultiple()
	if err != nil {
		return Hedge{}, fmt.Errorf("wager: back price: %w", err)
	}
	dh, err := hedge.Decimal()
	if err != nil {
		return Hedge{}, fmt.Errorf("wager: hedge price: %w", err)
	}
	mh, err := hedge.ProfitMultiple()
	if err != nil {
		return Hedge{}, err
	}

	h := face * mb / dh
	profit := h * mh

	// The combined hold across the two books, which is what conversion
	// actually depends on. Below zero it is an arbitrage: the two books
	// disagree enough that both sides can be backed at a profit.
	rb, err := back.ImpliedRaw()
	if err != nil {
		return Hedge{}, err
	}
	rh, err := hedge.ImpliedRaw()
	if err != nil {
		return Hedge{}, err
	}

	return Hedge{
		Stake: h, Profit: profit, Conversion: profit / face, Hold: rb + rh - 1,
	}, nil
}

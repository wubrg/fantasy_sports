package wager

import "fmt"

// validProb rejects anything that is not a probability. A caller who has no
// estimate must not pass 0 — that is a claim the event never happens.
func validProb(p float64) error {
	if p < 0 || p > 1 {
		return fmt.Errorf("wager: probability %v out of range [0,1]", p)
	}
	return nil
}

func validStake(stake float64) error {
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

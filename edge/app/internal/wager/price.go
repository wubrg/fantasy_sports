package wager

import (
	"fmt"
	"math"
)

// decimalToAmerican converts decimal odds to the least favourable American
// price that is still at least as good as d.
//
// The rounding direction is deliberate and load-bearing. These values are used
// as thresholds ("bet only at this price or better"), so rounding the wrong
// way would hand back a price that does not actually clear the target. Ceiling
// works for both signs: for a favourite, -122 is a better price than -123 and
// ceil(-122.7) = -122; for an underdog, +151 is better than +150 and
// ceil(150.7) = 151.
//
// Every American price has |value| >= 100, so there is no gap to fall into
// around even money.
func decimalToAmerican(d float64) (American, error) {
	if !finite(d) {
		return 0, fmt.Errorf("wager: decimal odds %v is not a real number", d)
	}
	if d <= 1 {
		return 0, fmt.Errorf("wager: decimal odds %v imply no profit (must exceed 1)", d)
	}
	var exact float64
	if d >= 2 {
		exact = (d - 1) * 100
	} else {
		exact = -100 / (d - 1)
	}

	// Snap to an exact integer before rounding up. Thresholds land on round
	// numbers far more often than chance would suggest -- -150 at 60%, +120
	// at 50% and +5% -- and float error puts those a hair off (-149.99…),
	// which a bare Ceil would push a full cent the wrong way. Only snap
	// within a relative epsilon, so a genuine 233.33 still ceilings to 234.
	if nearest := math.Round(exact); math.Abs(exact-nearest) < 1e-9*math.Max(1, math.Abs(exact)) {
		exact = nearest
	}

	if !finite(exact) {
		return 0, fmt.Errorf("wager: price computation produced %v", exact)
	}
	a := American(math.Ceil(exact))
	if err := a.validate(); err != nil {
		// Previously this returned a fabricated +100 with a nil error, on the
		// reasoning that only float noise at the boundary could reach it. A NaN
		// input reached it too, and the fabricated price was printed as advice.
		return 0, fmt.Errorf("wager: no representable price meets this target: %w", err)
	}
	return a, nil
}

// MinPrice returns the least favourable American price at which a wager with
// true win probability p returns at least targetROI per unit staked.
//
// This is the inverse of the usual question. Rather than taking a price off
// the board and asking whether it is worth betting, it takes what you believe
// and produces the number you must see before you act -- so the arithmetic
// happens before you are looking at a live line, not during.
//
// Real money:  ROI = p·d − 1        so  d = (1 + targetROI) / p
// Bonus bet:   ROI = p·(d − 1)      so  d = 1 + targetROI / p
//
// targetROI is a fraction of stake: 0 for breakeven, 0.05 for +5%. For a bonus
// bet it is the share of face value converted, so 0.70 asks for a 70%
// conversion.
func MinPrice(b Bankroll, p, targetROI float64) (American, error) {
	if err := b.validate(); err != nil {
		return 0, err
	}
	if err := validProb(p); err != nil {
		return 0, err
	}
	if p == 0 {
		return 0, fmt.Errorf("wager: no price makes a zero-probability wager profitable")
	}
	if !finite(targetROI) {
		return 0, fmt.Errorf("wager: targetROI %v is not a real number", targetROI)
	}
	if targetROI < 0 {
		return 0, fmt.Errorf("wager: targetROI %v must not be negative", targetROI)
	}

	var d float64
	if b == BonusBet {
		d = 1 + targetROI/p
	} else {
		d = (1 + targetROI) / p
	}
	if d <= 1 {
		return 0, fmt.Errorf("wager: target already met at any price (p=%v, target=%v)", p, targetROI)
	}
	return decimalToAmerican(d)
}

// MinPriceForConversion returns the least favourable price at which a fairly
// priced bonus bet converts at least the given share of its face value.
//
// At fair odds conversion is exactly 1 − p, so the requirement reduces to
// p <= 1 − target. No projection is involved: this depends only on the target,
// which is why the bonus-bet reference card is static and never needs a data
// pull.
//
// The result is a CEILING. It assumes the price carries no vig, and vig is
// heaviest on longshots -- so the overstatement is worst exactly where this
// function points. Realised conversion typically lands at 60-80%.
func MinPriceForConversion(target float64) (American, error) {
	if !finite(target) {
		return 0, fmt.Errorf("wager: conversion target %v is not a real number", target)
	}
	if target <= 0 || target >= 1 {
		return 0, fmt.Errorf("wager: conversion target %v must be between 0 and 1", target)
	}
	return decimalToAmerican(1 / (1 - target))
}

// ConversionCeiling is the share of a bonus bet's face value that converts to
// cash at this price, assuming the price is fair.
func (a American) ConversionCeiling() (float64, error) {
	p, err := a.ImpliedRaw()
	if err != nil {
		return 0, err
	}
	return 1 - p, nil
}

// TrueConversion is the share of face value a bonus bet converts using a
// de-vigged probability rather than the raw implied one.
//
// Raw implied probability is inflated by the vig, so multiplying it by the
// profit overstates conversion. Because books charge more vig on longshots,
// the overstatement grows exactly where the naive ceiling looks most
// attractive. This is the arithmetic behind the gap between a "90% conversion"
// +1000 longshot and the 60-80% that actually shows up.
func TrueConversion(m Market) (float64, error) {
	fair, _, err := m.FairDevig()
	if err != nil {
		return 0, err
	}
	profit, err := m.A.ProfitMultiple()
	if err != nil {
		return 0, err
	}
	return fair * profit, nil
}

// Package wager converts sportsbook prices into probabilities and expected
// values.
//
// Everything here is a pure function over numbers the caller supplies. Nothing
// in this package fetches, guesses, or defaults: an input it cannot interpret
// produces an error, never a zero. That is deliberate. The framework documents
// in edge/docs/frameworks/ were verified against these formulas and four
// arithmetic errors were found in them, so the arithmetic lives here — as
// executable, tested assertions — rather than in prose or in a model's head.
package wager

import (
	"errors"
	"fmt"
)

// ErrNoData is returned when a computation is asked for without the inputs it
// needs. Callers must surface it rather than substituting a zero; a silently
// zeroed EV reads as "no edge" when it actually means "no data".
var ErrNoData = errors.New("wager: required market data missing")

// American is a price in American odds: +150 pays 150 on a 100 stake, -150
// risks 150 to win 100. Values strictly between -100 and +100 are not
// expressible (there is no such price), and are rejected.
type American int

// maxAmerican bounds a price to something a book could plausibly post.
//
// The upper bound is not cosmetic. Without it, American(math.MinInt64) negates
// to itself under two's complement, and Decimal() then returned 1.0 -- a price
// implying certainty and zero profit, accepted with a nil error. That value was
// reachable, because MinPrice could produce it from a NaN input.
const maxAmerican = 1_000_000

func (a American) validate() error {
	if a >= 100 && a <= maxAmerican {
		return nil
	}
	if a <= -100 && a >= -maxAmerican {
		return nil
	}
	return fmt.Errorf(
		"wager: %d is not a valid American price (needs |price| >= 100 and <= %d)",
		int(a), maxAmerican)
}

// Decimal converts to decimal odds: total return per unit staked, stake
// included. -110 is 1.909…, +150 is 2.5.
func (a American) Decimal() (float64, error) {
	if err := a.validate(); err != nil {
		return 0, err
	}
	if a > 0 {
		return 1 + float64(a)/100, nil
	}
	return 1 + 100/float64(-a), nil
}

// ProfitMultiple is profit per unit staked on a win, excluding the returned
// stake — decimal minus one. This is the "win multiplier" column in
// edge-of-vigor.md: 0.91x at -110, 1.50x at +150.
func (a American) ProfitMultiple() (float64, error) {
	d, err := a.Decimal()
	if err != nil {
		return 0, err
	}
	return d - 1, nil
}

// ImpliedRaw is the probability implied by the price as offered, vig included.
//
// This is the *hurdle rate*: the win rate needed to break even at this price.
// It is NOT the market's estimate of the true probability — both sides of a
// real market sum to more than 1. Use Market.FairDevig for that. The source
// documents conflate the two; see edge-of-vigor.md, Verification item 1.
func (a American) ImpliedRaw() (float64, error) {
	d, err := a.Decimal()
	if err != nil {
		return 0, err
	}
	return 1 / d, nil
}

// Breakeven is the win rate required to break even at this price. It is
// numerically identical to ImpliedRaw and exists so calling code reads as the
// question it is actually asking.
func (a American) Breakeven() (float64, error) { return a.ImpliedRaw() }

// Market is a two-sided market. Both prices are required: vig cannot be
// measured from one side alone.
type Market struct {
	A American // e.g. the Over
	B American // e.g. the Under
}

// sum returns the two raw implied probabilities and their total.
func (m Market) sum() (pa, pb, total float64, err error) {
	pa, err = m.A.ImpliedRaw()
	if err != nil {
		return 0, 0, 0, err
	}
	pb, err = m.B.ImpliedRaw()
	if err != nil {
		return 0, 0, 0, err
	}
	return pa, pb, pa + pb, nil
}

// Overround is how far the two implied probabilities exceed 1, in probability
// points. A standard -110/-110 market is 0.0476 (4.76 points).
//
// This is the number the source documents call "the vig". It is not the
// book's margin — see Hold.
func (m Market) Overround() (float64, error) {
	_, _, total, err := m.sum()
	if err != nil {
		return 0, err
	}
	return total - 1, nil
}

// Hold is the book's expected margin on balanced action: overround divided by
// the total. At -110/-110 this is exactly 1/22 = 4.5455%, not the 4.76 points
// of overround, and not the 4.8% the source documents quote.
func (m Market) Hold() (float64, error) {
	_, _, total, err := m.sum()
	if err != nil {
		return 0, err
	}
	if total <= 0 {
		return 0, ErrNoData
	}
	return (total - 1) / total, nil
}

// FairDevig removes the vig multiplicatively, returning probabilities that sum
// to 1. These are the market's actual estimates, and the right benchmark for
// deciding whether your own number disagrees with the market.
//
// At -110/-110 this returns 0.5, 0.5 — the market says a coin flip, even
// though each side is quoted at an implied 52.4%.
func (m Market) FairDevig() (pa, pb float64, err error) {
	rawA, rawB, total, err := m.sum()
	if err != nil {
		return 0, 0, err
	}
	if total <= 0 {
		return 0, 0, ErrNoData
	}
	return rawA / total, rawB / total, nil
}

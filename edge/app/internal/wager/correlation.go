// Package wager, this file: pricing two or more CORRELATED (same-game) legs
// under an operator-supplied correlation factor, instead of refusing them the
// way parlay.go's independence contract requires.
//
// The joint probability of two events is mathematically bounded:
//
//	max(0, p1 + p2 - 1)  <=  P(both)  <=  min(p1, p2)
//
// -- the countermonotonic floor (the two never happen together beyond what
// overlap is forced) and the comonotonic ceiling (the rarer leg's own
// probability, if the two always happen together). The independent case,
// p1*p2, sits somewhere inside that range for any real positively- or
// negatively-associated pair. rho in [-1,1] is a correlation factor that
// linearly interpolates from independence toward whichever bound its sign
// names. This is exact and bounded by construction: no clamping anywhere,
// because the formula cannot produce an invalid probability for any rho in
// that range.
package wager

import "fmt"

// FrechetBlend computes the joint probability of two events under a
// correlation factor rho, via Frechet-Hoeffding bound interpolation.
//
// rho=0 reproduces p1*p2 exactly (today's existing independent-product
// result). rho=1 means the two events always happen together (P(both) =
// min(p1,p2)). rho=-1 means they happen together only as much as the floor
// forces (P(both) = max(0, p1+p2-1)).
func FrechetBlend(p1, p2, rho float64) (float64, error) {
	if err := validProb(p1); err != nil {
		return 0, fmt.Errorf("wager: p1: %w", err)
	}
	if err := validProb(p2); err != nil {
		return 0, fmt.Errorf("wager: p2: %w", err)
	}
	if rho < -1 || rho > 1 {
		return 0, fmt.Errorf("wager: rho = %v is out of range [-1,1]", rho)
	}
	indep := p1 * p2
	if rho >= 0 {
		upper := p1
		if p2 < upper {
			upper = p2
		}
		return indep + rho*(upper-indep), nil
	}
	lower := p1 + p2 - 1
	if lower < 0 {
		lower = 0
	}
	return indep + (-rho)*(lower-indep), nil
}

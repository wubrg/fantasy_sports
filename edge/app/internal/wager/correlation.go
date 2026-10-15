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

// CorrelationRequirement is the inverse of FrechetBlend: what rho a target
// joint probability (typically a book's own raw implied price) requires you
// to believe, mirroring RequiredScenarioProb's "what would you have to
// believe" shape (belief.go) for a different quantity.
type CorrelationRequirement struct {
	P1, P2   float64
	Target   float64
	Required float64 // rho*, in [-1,1]
}

// RequiredCorrelation solves for the rho that makes FrechetBlend(p1, p2, rho)
// equal pTarget. Refuses a target outside [max(0,p1+p2-1), min(p1,p2)] --
// that range is what any rho in [-1,1] can ever produce, so a target outside
// it is not a correlation question at all; the two input probabilities
// themselves disagree with the target.
func RequiredCorrelation(p1, p2, pTarget float64) (CorrelationRequirement, error) {
	if err := validProb(p1); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: p1: %w", err)
	}
	if err := validProb(p2); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: p2: %w", err)
	}
	if err := validProb(pTarget); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: target: %w", err)
	}
	indep := p1 * p2
	upper := p1
	if p2 < upper {
		upper = p2
	}
	lower := p1 + p2 - 1
	if lower < 0 {
		lower = 0
	}
	if pTarget > upper+1e-9 || pTarget < lower-1e-9 {
		return CorrelationRequirement{}, fmt.Errorf(
			"wager: target %.4f is outside [%.4f, %.4f], the range any correlation factor "+
				"in [-1,1] can produce for p1=%.4f, p2=%.4f -- no rho explains this gap",
			pTarget, lower, upper, p1, p2)
	}

	var rho float64
	switch {
	case pTarget >= indep && upper != indep:
		rho = (pTarget - indep) / (upper - indep)
	case pTarget < indep && indep != lower:
		rho = -(indep - pTarget) / (indep - lower)
	} // pTarget == indep, or upper/lower degenerately equal indep: rho stays 0

	return CorrelationRequirement{P1: p1, P2: p2, Target: pTarget, Required: rho}, nil
}

// ChainFrechet blends N probabilities pairwise, in listing order, one rho
// per chain step (len(rhos) must be len(probs)-1): blend probs[0] and
// probs[1] under rhos[0], then blend that result against probs[2] under
// rhos[1], and so on.
//
// For N=2 this is identical to one FrechetBlend call. For N>=3 it is a real,
// explicitly-acknowledged approximation: treating a chained intermediate
// result as a single Bernoulli probability for the next blend has no
// guarantee of landing within the TRUE N-way Frechet bounds, and the error
// is not characterized. The order the legs are chained in matters -- this is
// not a defect to fix, it is the nature of the approximation, which is why
// the CLI layer (ladder.go) prints a caveat on every 3+-leg correlated combo
// rather than presenting this as exact.
func ChainFrechet(probs []float64, rhos []float64) (float64, error) {
	if len(probs) < 2 {
		return 0, fmt.Errorf("wager: ChainFrechet needs at least 2 probabilities, got %d", len(probs))
	}
	if len(rhos) != len(probs)-1 {
		return 0, fmt.Errorf(
			"wager: %d probabilities need %d rho values (one per chain step), got %d",
			len(probs), len(probs)-1, len(rhos))
	}
	running := probs[0]
	for i := 1; i < len(probs); i++ {
		blended, err := FrechetBlend(running, probs[i], rhos[i-1])
		if err != nil {
			return 0, fmt.Errorf("chain step %d (blending running result against leg %d): %w", i, i+1, err)
		}
		running = blended
	}
	return running, nil
}

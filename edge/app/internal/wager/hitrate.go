package wager

import (
	"fmt"
	"math"
)

// Side is which way a prop is being taken.
type Side int

const (
	Over Side = iota
	Under
)

func (s Side) String() string {
	if s == Under {
		return "under"
	}
	return "over"
}

// HitRate is an empirical estimate of a prop's win probability, taken straight
// from a player's game log.
//
// This is the cheap route to p_true. It makes no distributional assumption at
// all -- it just counts how often the player actually cleared this number --
// which sidesteps the mean-vs-median trap that catches anyone comparing a mean
// projection to a median line. What it buys in honesty it pays for in
// variance, and at the sample sizes a season provides that price is steep.
// The interval is reported for exactly that reason.
type HitRate struct {
	Hits       int     // games clearing the line
	N          int     // games counted (pushes excluded)
	Pushes     int     // games landing exactly on the line
	Rate       float64 // Hits/N, the point estimate
	Lower      float64 // Wilson lower bound
	Upper      float64 // Wilson upper bound
	Confidence float64 // two-sided level the bounds were computed at
}

// Width is the size of the confidence interval. A width approaching 0.5 means
// the sample is barely more informative than a coin flip.
func (h HitRate) Width() float64 { return h.Upper - h.Lower }

// zFor returns the two-sided critical value for a confidence level.
func zFor(confidence float64) (float64, error) {
	if !finite(confidence) {
		return 0, fmt.Errorf("scenario: confidence %v is not a real number", confidence)
	}
	if confidence <= 0 || confidence >= 1 {
		return 0, fmt.Errorf("wager: confidence %v must be between 0 and 1", confidence)
	}
	return math.Sqrt2 * math.Erfinv(confidence), nil
}

// WilsonInterval returns a confidence interval for a binomial proportion.
//
// Wilson rather than the textbook normal approximation because the normal
// interval misbehaves badly at exactly the sample sizes and extreme rates this
// package sees -- it can produce bounds below 0 or above 1, and its coverage
// collapses when a player has gone 10-for-10. Wilson stays inside [0,1] and
// keeps roughly nominal coverage down to single-digit samples.
func WilsonInterval(hits, n int, confidence float64) (lower, upper float64, err error) {
	if n <= 0 {
		return 0, 0, fmt.Errorf("wager: cannot form an interval from %d games", n)
	}
	if hits < 0 || hits > n {
		return 0, 0, fmt.Errorf("wager: %d hits is impossible in %d games", hits, n)
	}
	z, err := zFor(confidence)
	if err != nil {
		return 0, 0, err
	}

	nf := float64(n)
	p := float64(hits) / nf
	z2 := z * z
	denom := 1 + z2/nf
	center := (p + z2/(2*nf)) / denom
	spread := (z / denom) * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))

	return math.Max(0, center-spread), math.Min(1, center+spread), nil
}

// ComputeHitRate counts how often observed values cleared the line and returns
// the rate with its confidence interval.
//
// Values landing exactly on the line are pushes and are excluded from the
// denominator rather than counted as losses -- a push returns the stake, so it
// is not a loss, and treating it as one would bias the estimate downward.
// Half-point lines cannot push, which is one reason to prefer them.
func ComputeHitRate(values []float64, line float64, side Side, confidence float64) (HitRate, error) {
	if len(values) == 0 {
		return HitRate{}, fmt.Errorf("wager: no game-log values supplied")
	}

	if !finite(line) {
		return HitRate{}, fmt.Errorf("wager: line %v is not a real number", line)
	}
	for i, v := range values {
		// A NaN game satisfied none of the comparisons below, so it stayed in
		// the denominator as a loss -- silently turning 3-of-4 into 3-of-5 and
		// moving the acceptable price by 150 cents.
		if !finite(v) {
			return HitRate{}, fmt.Errorf("wager: game-log value %d is %v, not a real number", i, v)
		}
	}

	var hits, pushes int
	for _, v := range values {
		switch {
		case v == line:
			pushes++
		case side == Over && v > line:
			hits++
		case side == Under && v < line:
			hits++
		}
	}

	n := len(values) - pushes
	if n == 0 {
		return HitRate{}, fmt.Errorf("wager: every one of %d games pushed on %.1f", len(values), line)
	}

	lower, upper, err := WilsonInterval(hits, n, confidence)
	if err != nil {
		return HitRate{}, err
	}
	return HitRate{
		Hits:       hits,
		N:          n,
		Pushes:     pushes,
		Rate:       float64(hits) / float64(n),
		Lower:      lower,
		Upper:      upper,
		Confidence: confidence,
	}, nil
}

// Verdict describes whether an estimate survives its own uncertainty at a
// given price.
type Verdict int

const (
	// Reject: even the optimistic end of the interval fails to clear the
	// price.
	Reject Verdict = iota
	// Unproven: the point estimate clears but the lower bound does not. The
	// apparent edge is not distinguishable from sampling noise.
	Unproven
	// Supported: the whole interval clears the price.
	Supported
)

func (v Verdict) String() string {
	switch v {
	case Supported:
		return "supported"
	case Unproven:
		return "unproven"
	default:
		return "reject"
	}
}

// Assess compares a hit rate against an offered price.
//
// The distinction that matters is Unproven. A point estimate above the
// breakeven rate is what every one of the source documents treats as an edge;
// it is really a claim that has not been separated from noise. Naming it as a
// third outcome rather than folding it into "bet" is the whole point of
// carrying the interval around.
func (h HitRate) Assess(price American) (Verdict, error) {
	breakeven, err := price.Breakeven()
	if err != nil {
		return Reject, err
	}
	switch {
	case h.Lower > breakeven:
		return Supported, nil
	case h.Rate > breakeven:
		return Unproven, nil
	default:
		return Reject, nil
	}
}

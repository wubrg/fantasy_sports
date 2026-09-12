package wager

import (
	"fmt"
	"math"
)

// A milestone ladder is ONE player/stat priced at several nested thresholds:
// clearing a higher rung implies clearing every lower one ("80+ yards" implies
// "50+"). Betting the ladder is therefore one bet at nested strikes, not several
// independent bets — the payoff is a curve over a single outcome, and spreading
// a stake across the rungs shapes that curve rather than diversifying anything.
//
// This file sizes a stake across the rungs and reports the payoff band by band.
// Two truths it makes hard to forget: splitting the stake does not change EV
// (total EV is the stake-weighted sum of each rung's own edge), and the popular
// "free roll" — size the floor rung so its return recovers the whole stake, the
// rest rides free — is only free if the floor hits, which it does not always do.

// LadderRung is one threshold's price. Rungs are given most-probable first: the
// shortest price (closest to the de-facto line) down to the longest, the order a
// book lists a milestone ladder in.
type LadderRung struct {
	Price American
	Label string // optional, e.g. "60+"
}

// Band is one outcome region of a nested ladder. ClearedThrough is how many
// rungs cashed (0 = missed even the first). Because the rungs nest, every rung
// at or below ClearedThrough pays and every rung above it loses.
type Band struct {
	ClearedThrough int     // 0..len(rungs)
	Return         float64 // total returned across paying rungs, stake included
	Net            float64 // Return minus the total stake
	Prob           float64 // P(this band); meaningful only when HasProb
	HasProb        bool
}

// LadderPlan is a staking of a ladder and the payoff it produces.
type LadderPlan struct {
	Rungs  []LadderRung
	Stakes []float64
	Total  float64
	Bands  []Band

	// FloorRecovers is the stake that, placed on the floor rung, returns the
	// whole Total (the free-roll pivot). Set for FreeRoll plans.
	FloorRecovers float64

	// Set only when per-rung beliefs are supplied.
	HasEV   bool
	EV      float64
	PNonNeg float64 // P(net >= 0)
	PProfit float64 // P(net > 0)
}

func ladderDecimals(rungs []LadderRung) ([]float64, error) {
	if len(rungs) < 2 {
		return nil, fmt.Errorf("wager: a ladder needs at least 2 rungs, got %d", len(rungs))
	}
	decs := make([]float64, len(rungs))
	prev := 0.0
	for i, r := range rungs {
		d, err := r.Price.Decimal()
		if err != nil {
			return nil, fmt.Errorf("rung %d: %w", i+1, err)
		}
		if i > 0 && d <= prev {
			return nil, fmt.Errorf(
				"wager: rungs must be ordered most-probable first (strictly increasing decimal odds); rung %d (%.3f) is not longer than rung %d (%.3f) — this is not a nested ladder",
				i+1, d, i, prev)
		}
		decs[i], prev = d, d
	}
	return decs, nil
}

// bands computes the payoff regions for a staking. It is the core the modes
// below all feed into: given stakes on nested rungs, the return in band k is the
// sum of the paying (at-or-below-k) rungs' returns.
func bands(decs, stakes []float64, total float64) []Band {
	n := len(decs)
	out := make([]Band, n+1)
	for k := 0; k <= n; k++ {
		var ret float64
		for i := 0; i < k; i++ {
			ret += stakes[i] * decs[i]
		}
		out[k] = Band{ClearedThrough: k, Return: ret, Net: ret - total}
	}
	return out
}

func plan(rungs []LadderRung, decs, stakes []float64) *LadderPlan {
	var total float64
	for _, s := range stakes {
		total += s
	}
	return &LadderPlan{Rungs: rungs, Stakes: stakes, Total: total, Bands: bands(decs, stakes, total)}
}

// FreeRoll sizes the floor rung (index floorIdx, default 0) so its return equals
// the whole stake, then puts the remainder on rollOnIdx (default the top rung).
// If the floor hits you are made whole; anything above it is house money — but
// the roll is conditional on the floor, which misses 1 - P(floor) of the time.
func FreeRoll(rungs []LadderRung, total float64, floorIdx, rollOnIdx int) (*LadderPlan, error) {
	if err := validStake(total); err != nil {
		return nil, err
	}
	decs, err := ladderDecimals(rungs)
	if err != nil {
		return nil, err
	}
	n := len(rungs)
	if floorIdx < 0 || floorIdx >= n {
		return nil, fmt.Errorf("wager: floor index %d out of range 0..%d", floorIdx, n-1)
	}
	if rollOnIdx < 0 || rollOnIdx >= n {
		rollOnIdx = n - 1
	}
	if rollOnIdx == floorIdx {
		return nil, fmt.Errorf("wager: free-roll needs the roll on a different rung than the floor")
	}
	floorStake := total / decs[floorIdx] // return = floorStake * dec = total
	remainder := total - floorStake
	stakes := make([]float64, n)
	stakes[floorIdx] = floorStake
	stakes[rollOnIdx] = remainder
	p := plan(rungs, decs, stakes)
	p.FloorRecovers = floorStake
	return p, nil
}

// Flat splits the stake equally across every rung.
func Flat(rungs []LadderRung, total float64) (*LadderPlan, error) {
	if err := validStake(total); err != nil {
		return nil, err
	}
	decs, err := ladderDecimals(rungs)
	if err != nil {
		return nil, err
	}
	n := len(rungs)
	stakes := make([]float64, n)
	for i := range stakes {
		stakes[i] = total / float64(n)
	}
	return plan(rungs, decs, stakes), nil
}

// Weighted splits the stake by the given weights (e.g. 4:3:2:1). Weights must be
// non-negative, not all zero, and one per rung.
func Weighted(rungs []LadderRung, total float64, weights []float64) (*LadderPlan, error) {
	if err := validStake(total); err != nil {
		return nil, err
	}
	decs, err := ladderDecimals(rungs)
	if err != nil {
		return nil, err
	}
	if len(weights) != len(rungs) {
		return nil, fmt.Errorf("wager: got %d weights for %d rungs", len(weights), len(rungs))
	}
	var sum float64
	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("wager: weights must be non-negative")
		}
		sum += w
	}
	if sum == 0 {
		return nil, fmt.Errorf("wager: weights cannot all be zero")
	}
	stakes := make([]float64, len(rungs))
	for i, w := range weights {
		stakes[i] = total * w / sum
	}
	return plan(rungs, decs, stakes), nil
}

// MaxEV puts the whole stake on the single rung with the highest expected value
// per dollar, given per-rung true probabilities. EV per dollar on a rung is
// p*dec - 1, and since a fixed total is being allocated across linear-EV rungs
// the optimum is a corner: everything on the best rung. It does not refuse when
// every rung is -EV (a ladder rarely beats the vig); it reports the least-bad
// allocation and the negative EV honestly.
func MaxEV(rungs []LadderRung, total float64, probs []float64) (*LadderPlan, error) {
	if err := validStake(total); err != nil {
		return nil, err
	}
	decs, err := ladderDecimals(rungs)
	if err != nil {
		return nil, err
	}
	if err := validLadderProbs(probs, len(rungs)); err != nil {
		return nil, err
	}
	best, bestEV := 0, math.Inf(-1)
	for i := range rungs {
		ev := probs[i]*decs[i] - 1
		if ev > bestEV {
			best, bestEV = i, ev
		}
	}
	stakes := make([]float64, len(rungs))
	stakes[best] = total
	p := plan(rungs, decs, stakes)
	return WithProbs(p, probs)
}

func validLadderProbs(probs []float64, n int) error {
	if len(probs) != n {
		return fmt.Errorf("wager: got %d probabilities for %d rungs", len(probs), n)
	}
	prev := 1.0001
	for i, p := range probs {
		if err := validProb(p); err != nil {
			return fmt.Errorf("rung %d: %w", i+1, err)
		}
		if p > prev {
			return fmt.Errorf("wager: rung %d probability %.3f exceeds rung %d's %.3f — a higher milestone cannot be more likely", i+1, p, i, prev)
		}
		prev = p
	}
	return nil
}

// WithProbs attaches band probabilities and EV to a plan, given per-rung true
// probabilities (most-probable first, monotonic decreasing). Band k (cleared
// through k, not k+1) has probability p[k]-p[k+1], with p[0]:=1 for "cleared
// nothing yet" and p[n+1]:=0. EV is the probability-weighted net.
func WithProbs(p *LadderPlan, probs []float64) (*LadderPlan, error) {
	if err := validLadderProbs(probs, len(p.Rungs)); err != nil {
		return nil, err
	}
	n := len(p.Rungs)
	// clearProb[k] = P(cleared through rung k) for k=0..n, with a sentinel 1 at
	// k=0 (everyone "clears" zero rungs) and probs[k-1] otherwise.
	clear := make([]float64, n+2)
	clear[0] = 1
	for i := 1; i <= n; i++ {
		clear[i] = probs[i-1]
	}
	clear[n+1] = 0
	var ev, pNonNeg, pProfit float64
	for k := 0; k <= n; k++ {
		bp := clear[k] - clear[k+1] // P(cleared exactly through k)
		p.Bands[k].Prob = bp
		p.Bands[k].HasProb = true
		ev += bp * p.Bands[k].Net
		if p.Bands[k].Net >= -1e-9 {
			pNonNeg += bp
		}
		if p.Bands[k].Net > 1e-9 {
			pProfit += bp
		}
	}
	p.HasEV, p.EV, p.PNonNeg, p.PProfit = true, ev, pNonNeg, pProfit
	return p, nil
}

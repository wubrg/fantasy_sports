package wager

import (
	"math"
	"testing"
)

// TestWilsonKnownValues pins the interval against values that can be checked
// by hand. 6-of-10 at 95% is the worked example used throughout the docs.
func TestWilsonKnownValues(t *testing.T) {
	cases := []struct {
		hits, n        int
		conf           float64
		wantLo, wantHi float64
		note           string
	}{
		{6, 10, 0.95, 0.3129, 0.8318, "the 6-of-10 example"},
		{5, 10, 0.95, 0.2366, 0.7634, "symmetric around 0.5"},
		{10, 10, 0.95, 0.7225, 1.0000, "perfect record stays inside [0,1]"},
		{0, 10, 0.95, 0.0000, 0.2775, "zero hits stays inside [0,1]"},
		{60, 100, 0.95, 0.5024, 0.6906, "same rate, 10x the sample"},
	}
	for _, c := range cases {
		lo, hi, err := WilsonInterval(c.hits, c.n, c.conf)
		if err != nil {
			t.Fatalf("%d/%d: %v", c.hits, c.n, err)
		}
		if math.Abs(lo-c.wantLo) > 5e-4 || math.Abs(hi-c.wantHi) > 5e-4 {
			t.Errorf("%d/%d @ %.2f = [%.4f, %.4f], want [%.4f, %.4f] (%s)",
				c.hits, c.n, c.conf, lo, hi, c.wantLo, c.wantHi, c.note)
		}
		if lo < 0 || hi > 1 {
			t.Errorf("%d/%d: interval [%.4f, %.4f] escaped [0,1]", c.hits, c.n, lo, hi)
		}
		if lo > hi {
			t.Errorf("%d/%d: inverted interval", c.hits, c.n)
		}
	}
}

// TestIntervalNarrowsWithSample is the property that matters for the strategy:
// the same rate becomes a much stronger claim with more games. This is the
// argument for a full season over a five-game window.
func TestIntervalNarrowsWithSample(t *testing.T) {
	var prev float64 = 2
	for _, n := range []int{5, 10, 17, 34, 100} {
		hits := int(math.Round(0.6 * float64(n)))
		lo, hi, err := WilsonInterval(hits, n, 0.95)
		if err != nil {
			t.Fatal(err)
		}
		width := hi - lo
		if width >= prev {
			t.Errorf("n=%d: width %.4f did not narrow from %.4f", n, width, prev)
		}
		prev = width
	}
}

// TestSmallSamplesAreUninformative states the uncomfortable finding in test
// form: at a full season of games, a 60% hit rate still cannot be separated
// from a coin flip.
func TestSmallSamplesAreUninformative(t *testing.T) {
	lo, _, err := WilsonInterval(10, 17, 0.95) // ~59% over a full season
	if err != nil {
		t.Fatal(err)
	}
	if lo > 0.5 {
		t.Errorf("lower bound %.4f: a season of 10-for-17 should NOT separate from a coin flip", lo)
	}
	// And it certainly cannot clear a standard -110 hurdle.
	breakeven, err := American(-110).Breakeven()
	if err != nil {
		t.Fatal(err)
	}
	if lo > breakeven {
		t.Errorf("lower bound %.4f should not clear the %.4f hurdle at n=17", lo, breakeven)
	}
}

// TestComputeHitRate covers counting, sides, and push handling.
func TestComputeHitRate(t *testing.T) {
	vals := []float64{61, 48, 55, 72, 39, 58, 44, 66, 51, 49}

	over, err := ComputeHitRate(vals, 52.5, Over, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if over.Hits != 5 || over.N != 10 {
		t.Errorf("over 52.5: got %d/%d, want 5/10", over.Hits, over.N)
	}

	under, err := ComputeHitRate(vals, 52.5, Under, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if under.Hits != 5 || under.N != 10 {
		t.Errorf("under 52.5: got %d/%d, want 5/10", under.Hits, under.N)
	}
	// With no pushes the two sides must partition the sample.
	if over.Hits+under.Hits != over.N {
		t.Error("over and under hits must sum to N when nothing pushes")
	}
}

// TestPushesLeaveTheDenominator guards the counting rule: a push returns the
// stake, so treating it as a loss would bias the estimate downward.
func TestPushesLeaveTheDenominator(t *testing.T) {
	vals := []float64{60, 50, 50, 70, 50} // three games land exactly on 50

	h, err := ComputeHitRate(vals, 50, Over, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if h.Pushes != 3 {
		t.Errorf("got %d pushes, want 3", h.Pushes)
	}
	if h.N != 2 || h.Hits != 2 {
		t.Errorf("got %d/%d, want 2/2 after excluding pushes", h.Hits, h.N)
	}
	if h.Rate != 1.0 {
		t.Errorf("rate = %.4f, want 1.0 — pushes must not count as losses", h.Rate)
	}

	if _, err := ComputeHitRate([]float64{50, 50}, 50, Over, 0.95); err == nil {
		t.Error("an all-push sample must fail rather than report a rate")
	}
}

// TestAssessNamesTheUnprovenCase is the reason the interval is carried at all.
// A point estimate above breakeven is what the source documents call an edge;
// separating "unproven" from "supported" is the correction.
func TestAssessNamesTheUnprovenCase(t *testing.T) {
	// 6-of-10 = 60%, interval [0.313, 0.832]. At -110 the hurdle is 52.4%:
	// the point estimate clears it, the lower bound does not.
	h, err := ComputeHitRate([]float64{1, 1, 1, 1, 1, 1, 0, 0, 0, 0}, 0.5, Over, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if h.Hits != 6 || h.N != 10 {
		t.Fatalf("setup wrong: got %d/%d", h.Hits, h.N)
	}

	v, err := h.Assess(-110)
	if err != nil {
		t.Fatal(err)
	}
	if v != Unproven {
		t.Errorf("6-of-10 at -110 should be Unproven, got %v", v)
	}

	// A rate below the hurdle is a straight reject.
	low, err := ComputeHitRate([]float64{1, 1, 1, 1, 0, 0, 0, 0, 0, 0}, 0.5, Over, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := low.Assess(-110); err != nil || v != Reject {
		t.Errorf("4-of-10 at -110 should be Reject, got %v", v)
	}

	// A large sample with a real edge is supported.
	big := make([]float64, 200)
	for i := range big {
		if i < 140 { // 70%
			big[i] = 1
		}
	}
	strong, err := ComputeHitRate(big, 0.5, Over, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := strong.Assess(-110); err != nil || v != Supported {
		t.Errorf("140-of-200 at -110 should be Supported, got %v", v)
	}
}

// TestHitRateRejectsNonsense keeps the fail-loudly contract.
func TestHitRateRejectsNonsense(t *testing.T) {
	if _, err := ComputeHitRate(nil, 50, Over, 0.95); err == nil {
		t.Error("an empty game log must fail, not report 0%")
	}
	for _, conf := range []float64{0, 1, -0.5, 1.5} {
		if _, err := ComputeHitRate([]float64{1, 2}, 1.5, Over, conf); err == nil {
			t.Errorf("confidence %v must be rejected", conf)
		}
	}
	if _, _, err := WilsonInterval(5, 0, 0.95); err == nil {
		t.Error("n=0 must be rejected")
	}
	if _, _, err := WilsonInterval(11, 10, 0.95); err == nil {
		t.Error("more hits than games must be rejected")
	}
}

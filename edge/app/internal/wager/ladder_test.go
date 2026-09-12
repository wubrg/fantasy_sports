package wager

import (
	"math"
	"testing"
)

func rungs(prices ...American) []LadderRung {
	out := make([]LadderRung, len(prices))
	for i, p := range prices {
		out[i] = LadderRung{Price: p}
	}
	return out
}

func near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s: got %.6f, want %.6f", label, got, want)
	}
}

// TestFreeRollRecoversStake is the construction the operator asked for: sizing
// the floor rung so its RETURN equals the whole stake means the floor-only band
// nets ~zero (made whole), the total-miss band loses everything, and the top
// band is house money. If this arithmetic drifts, the "free roll" quietly stops
// being free.
func TestFreeRollRecoversStake(t *testing.T) {
	// -153 floor (decimal 1.6536), +444 top (decimal 5.44), $10.
	p, err := FreeRoll(rungs(-153, 444), 10, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Floor stake returns the whole stake.
	near(t, "floor recovers", p.FloorRecovers*mustDec(t, -153), 10)
	// Band 0 (missed the floor): lose it all.
	near(t, "band0 net", p.Bands[0].Net, -10)
	// Band 1 (floor only): made whole.
	near(t, "band1 net (free roll pivot)", p.Bands[1].Net, 0)
	// Band 2 (top also hits): the floor's return cancels the total stake, so the
	// whole roll return is net profit.
	remainder := 10 - p.FloorRecovers
	near(t, "band2 net", p.Bands[2].Net, remainder*mustDec(t, 444))
}

// TestWeightedBandsMatchHandTable pins the 4:3:2:1 example from the discussion:
// the modal "clears only the safe rung" band still NETS A LOSS, which is the
// whole counter-intuitive point of bottom-weighting a nested ladder.
func TestWeightedBandsMatchHandTable(t *testing.T) {
	p, err := Weighted(rungs(-153, -101, 217, 444), 10, []float64{4, 3, 2, 1})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "band0 (miss all)", p.Bands[0].Net, -10)
	near(t, "band1 (floor only) still a LOSS", p.Bands[1].Net, 4*mustDec(t, -153)-10)
	near(t, "band2", p.Bands[2].Net, 4*mustDec(t, -153)+3*mustDec(t, -101)-10)
	near(t, "band4 (all)", p.Bands[4].Net,
		4*mustDec(t, -153)+3*mustDec(t, -101)+2*mustDec(t, 217)+1*mustDec(t, 444)-10)
	// The load-bearing assertion: clearing only the safe rung is under water.
	if p.Bands[1].Net >= 0 {
		t.Errorf("band1 net %.4f should be negative — bottom-weighting does not guarantee a green floor", p.Bands[1].Net)
	}
}

// TestLadderEVAndProbs checks that splitting does not invent EV: the structure's
// EV equals the stake-weighted sum of each rung's own edge, and the band
// probabilities form a distribution.
func TestLadderEVAndProbs(t *testing.T) {
	rs := rungs(-153, -101, 217, 444)
	p, err := Weighted(rs, 10, []float64{4, 3, 2, 1})
	if err != nil {
		t.Fatal(err)
	}
	probs := []float64{0.60, 0.50, 0.31, 0.18}
	p, err = WithProbs(p, probs)
	if err != nil {
		t.Fatal(err)
	}
	// EV two ways must agree: probability-weighted band nets, and Σ stakeᵢ(pᵢdecᵢ−1).
	var evDirect float64
	for i, r := range rs {
		evDirect += p.Stakes[i] * (probs[i]*mustDec(t, r.Price) - 1)
	}
	near(t, "EV agrees with per-rung sum", p.EV, evDirect)
	var total float64
	for _, b := range p.Bands {
		total += b.Prob
	}
	near(t, "band probabilities sum to 1", total, 1)
}

func TestLadderRejectsNonMonotonic(t *testing.T) {
	// Prices out of probability order (a longer price before a shorter one).
	if _, err := Flat(rungs(444, -153), 10); err == nil {
		t.Error("expected an error for rungs not ordered most-probable first")
	}
	// Probabilities that rise with the milestone are impossible.
	p, _ := Flat(rungs(-153, 444), 10)
	if _, err := WithProbs(p, []float64{0.4, 0.6}); err == nil {
		t.Error("expected an error for a higher milestone being more likely")
	}
}

func TestMaxEVConcentrates(t *testing.T) {
	rs := rungs(-153, 444)
	// Make the top rung the +EV one: 0.30 * 5.44 = 1.632 > 1; floor 0.55*1.6536=0.909 < 1.
	p, err := MaxEV(rs, 10, []float64{0.55, 0.30})
	if err != nil {
		t.Fatal(err)
	}
	if p.Stakes[1] != 10 || p.Stakes[0] != 0 {
		t.Errorf("max-EV should put the whole stake on the +EV top rung, got %v", p.Stakes)
	}
}

func mustDec(t *testing.T, a American) float64 {
	t.Helper()
	d, err := a.Decimal()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

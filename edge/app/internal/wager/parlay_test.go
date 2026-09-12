package wager

import (
	"math"
	"testing"
)

func TestAmericanFromDecimal(t *testing.T) {
	cases := []struct {
		dec  float64
		want American
	}{
		{2.5, 150},
		{2.0, 100},
		{1.0 + 100.0/110.0, -110}, // 1.909...
		{4.0, 300},
	}
	for _, c := range cases {
		got, err := AmericanFromDecimal(c.dec)
		if err != nil {
			t.Fatalf("dec %.4f: %v", c.dec, err)
		}
		if got != c.want {
			t.Errorf("dec %.4f: got %d, want %d", c.dec, got, c.want)
		}
	}
	if _, err := AmericanFromDecimal(1.0); err == nil {
		t.Error("decimal 1.0 is a certainty, not a price — should error")
	}
}

func TestCombineParlay(t *testing.T) {
	// +100 and +100 → decimal 2 * 2 = 4 → +300, implied 0.25.
	c, err := CombineParlay([]American{100, 100})
	if err != nil {
		t.Fatal(err)
	}
	if c.Price != 300 {
		t.Errorf("combined price = %d, want +300", c.Price)
	}
	if math.Abs(c.Implied-0.25) > 1e-9 {
		t.Errorf("combined implied = %.4f, want 0.25", c.Implied)
	}
}

func TestRoundRobinComboCount(t *testing.T) {
	legs := []American{100, 100, 100, 100}
	combos, err := RoundRobin(legs, []int{2, 3})
	if err != nil {
		t.Fatal(err)
	}
	// C(4,2)=6 plus C(4,3)=4 = 10.
	if len(combos) != 10 {
		t.Errorf("got %d combos, want 10 (6 doubles + 4 triples)", len(combos))
	}
}

// TestTicketDistributionIsExact checks the enumerated profit distribution: three
// independent +100 legs at p=0.5, all 2-leg combos, $30 total ($10/combo). EV
// per combo is zero (0.25*4-1=0), so the ticket EV is zero, and the win-count
// distribution must be the binomial(3, 0.5).
func TestTicketDistributionIsExact(t *testing.T) {
	legs := []American{100, 100, 100}
	combos, err := RoundRobin(legs, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := BuildTicket(legs, combos, 30, []float64{0.5, 0.5, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(tk.EV) > 1e-9 {
		t.Errorf("ticket EV = %.6f, want 0 (fair legs)", tk.EV)
	}
	// Distribution over number of legs won must sum to 1 and match binomial.
	var total float64
	for _, d := range tk.Dist {
		total += d.Prob
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("distribution sums to %.6f, want 1", total)
	}
	// Binomial(3,0.5): P(3 wins)=0.125. With all three legs up, all 3 doubles cash.
	if math.Abs(tk.Dist[3].Prob-0.125) > 1e-9 {
		t.Errorf("P(3 wins) = %.4f, want 0.125", tk.Dist[3].Prob)
	}
	// A combo cashes only if both its legs win → at least 2 of 3 legs.
	// P(≥2 of 3 at p=.5) = 0.5. That is P(any combo cashes).
	if math.Abs(tk.PAny-0.5) > 1e-9 {
		t.Errorf("P(any combo cashes) = %.4f, want 0.5", tk.PAny)
	}
}

func TestRoundRobinRejectsTooFewLegs(t *testing.T) {
	if _, err := RoundRobin([]American{100}, []int{2}); err == nil {
		t.Error("a round robin needs at least 2 legs")
	}
}

package wager

import (
	"math"
	"testing"
)

// TestRoundTripIdentity is the test that proves the decomposition is
// self-consistent: blending at exactly the required belief must reproduce the
// price's breakeven, and therefore zero EV.
func TestRoundTripIdentity(t *testing.T) {
	cases := []struct {
		price American
		q, r  float64
	}{
		{450, 0.40, 0.08},
		{-110, 0.65, 0.45},
		{150, 0.55, 0.30},
		{1000, 0.25, 0.04},
		{-200, 0.80, 0.60},
		{300, 0.20, 0.45}, // scenario hurts: q < r
	}
	for _, c := range cases {
		req, err := RequiredScenarioProb(c.price, c.q, c.r)
		if err != nil {
			t.Fatalf("%+d: %v", c.price, err)
		}
		if req.Required < 0 || req.Required > 1 {
			continue // outside the range s can take; identity still holds but is not meaningful
		}
		p, err := BlendedProb(req.Required, c.q, c.r)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(p-req.Breakeven) > 1e-9 {
			t.Errorf("%+d: blending at s*=%.6f gives P=%.9f, breakeven is %.9f",
				c.price, req.Required, p, req.Breakeven)
		}
		ev, err := EVRealMoney(p, c.price, 1)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(ev) > 1e-9 {
			t.Errorf("%+d: EV at the required belief should be 0, got %+.12f", c.price, ev)
		}
	}
}

// TestWorkedExample pins the example carried through the plan and the docs:
// 100+ receiving yards at +450, 40% in a shootout, 8% otherwise.
func TestWorkedExample(t *testing.T) {
	req, err := RequiredScenarioProb(450, 0.40, 0.08)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(req.Breakeven, 0.1818, 1e-4) {
		t.Errorf("breakeven at +450 = %.4f, want 0.1818", req.Breakeven)
	}
	// Exactly (100/550 − 0.08) / 0.32. Computing this from a breakeven rounded
	// to 0.182 gives 0.31875, which is close enough to quote as "32%" but not
	// close enough to assert on.
	if !closeTo(req.Required, 0.318182, 1e-6) {
		t.Errorf("required belief = %.6f, want 0.318182", req.Required)
	}
	if !req.AtLeast {
		t.Error("a shootout helps a 100+ yard over, so the requirement is a floor")
	}
	if req.AlwaysClears || req.Unreachable {
		t.Error("this example is an ordinary in-range requirement")
	}

	if !req.Satisfied(0.45) {
		t.Error("believing 45% should satisfy a 32% requirement")
	}
	if req.Satisfied(0.25) {
		t.Error("believing 25% should not satisfy a 32% requirement")
	}
}

// TestSensitivityFindsTheDominantInput records the counterintuitive result: for
// the worked example the baseline rate r moves the answer about twice as much
// as the scenario rate q.
func TestSensitivityFindsTheDominantInput(t *testing.T) {
	req, err := RequiredScenarioProb(450, 0.40, 0.08)
	if err != nil {
		t.Fatal(err)
	}
	s := req.Sensitivity()

	if !closeTo(s.PerPointOfQ, -0.994318, 1e-5) {
		t.Errorf("ds*/dq = %.6f, want -0.994318", s.PerPointOfQ)
	}
	if !closeTo(s.PerPointOfR, -2.130682, 1e-5) {
		t.Errorf("ds*/dr = %.6f, want -2.130682", s.PerPointOfR)
	}
	if s.Dominant() != "r" {
		t.Errorf("dominant input = %q, want r", s.Dominant())
	}

	// Verify the derivative numerically against a finite difference.
	const h = 1e-6
	bumped, err := RequiredScenarioProb(450, 0.40+h, 0.08)
	if err != nil {
		t.Fatal(err)
	}
	fd := (bumped.Required - req.Required) / h
	if math.Abs(fd-s.PerPointOfQ) > 1e-3 {
		t.Errorf("analytic ds*/dq = %.6f but finite difference = %.6f", s.PerPointOfQ, fd)
	}
}

// TestScenarioThatHurtsFlipsTheInequality covers the q < r case -- an Under
// that the scenario would break. The requirement becomes a ceiling on belief,
// not a floor.
func TestScenarioThatHurtsFlipsTheInequality(t *testing.T) {
	// P(hit) falls as the shootout becomes more likely.
	req, err := RequiredScenarioProb(-110, 0.30, 0.70)
	if err != nil {
		t.Fatal(err)
	}
	if req.AtLeast {
		t.Error("when q < r the requirement must be a ceiling, not a floor")
	}
	if !req.Satisfied(0.1) {
		t.Error("a low scenario probability should satisfy a ceiling")
	}
	if req.Satisfied(0.9) {
		t.Error("a high scenario probability should violate a ceiling")
	}

	// Blending above the ceiling must actually be -EV.
	p, err := BlendedProb(0.9, 0.30, 0.70)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := EVRealMoney(p, -110, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ev >= 0 {
		t.Errorf("violating the ceiling should be -EV, got %+.4f", ev)
	}
}

// TestAlwaysClearsAndUnreachable cover the two out-of-range solutions, which
// are meaningful answers rather than errors.
func TestAlwaysClearsAndUnreachable(t *testing.T) {
	// Baseline alone beats the price: the narrative is not carrying this bet.
	req, err := RequiredScenarioProb(150, 0.60, 0.50)
	if err != nil {
		t.Fatal(err)
	}
	if !req.AlwaysClears {
		t.Errorf("r=0.50 already beats a 40%% hurdle; want AlwaysClears (s*=%.3f)", req.Required)
	}
	if !req.Satisfied(0) {
		t.Error("an always-clearing wager is satisfied even at s=0")
	}

	// Even certainty is not enough.
	req, err = RequiredScenarioProb(-500, 0.50, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if !req.Unreachable {
		t.Errorf("a 83.3%% hurdle cannot be met with q=0.50; want Unreachable (s*=%.3f)", req.Required)
	}
	if req.Satisfied(1.0) {
		t.Error("an unreachable wager is not satisfied even at certainty")
	}
}

// TestBlendedEVRoutesByBankroll guards the error found in the source material:
// a scenario bet on a longshot is often a bonus bet, and pricing it with the
// cash formula understates it by exactly (1-p)·stake.
func TestBlendedEVRoutesByBankroll(t *testing.T) {
	const s, q, r = 0.4, 0.35, 0.05
	cash, err := BlendedEV(RealMoney, s, q, r, 450, 100)
	if err != nil {
		t.Fatal(err)
	}
	bonus, err := BlendedEV(BonusBet, s, q, r, 450, 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := BlendedProb(s, q, r)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(bonus-cash, (1-p)*100, 1e-9) {
		t.Errorf("bankroll gap = %.6f, want (1-p)·stake = %.6f", bonus-cash, (1-p)*100)
	}
	if bonus <= cash {
		t.Error("a bonus bet must be worth more than the same wager in cash")
	}
}

// TestBeliefRejectsNonsense keeps the fail-loudly contract.
func TestBeliefRejectsNonsense(t *testing.T) {
	if _, err := RequiredScenarioProb(-110, 0.5, 0.5); err == nil {
		t.Error("q == r carries no information and must be rejected, not divided by zero")
	}
	for _, bad := range []float64{-0.1, 1.1} {
		if _, err := RequiredScenarioProb(-110, bad, 0.3); err == nil {
			t.Errorf("q = %v must be rejected", bad)
		}
		if _, err := BlendedProb(bad, 0.5, 0.3); err == nil {
			t.Errorf("s = %v must be rejected", bad)
		}
	}
	if _, err := RequiredScenarioProb(0, 0.5, 0.3); err == nil {
		t.Error("an invalid price must be rejected")
	}
}

package wager

import (
	"math"
	"testing"
)

func TestFrechetBlend(t *testing.T) {
	cases := []struct {
		name        string
		p1, p2, rho float64
		want        float64
	}{
		{"independence (rho=0) reproduces the product", 0.6, 0.5, 0.0, 0.30},
		{"positive correlation interpolates toward the comonotonic ceiling", 0.6, 0.5, 0.5, 0.40},
		{"negative correlation interpolates toward the countermonotonic floor", 0.6, 0.5, -0.4, 0.22},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FrechetBlend(c.p1, c.p2, c.rho)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("FrechetBlend(%v, %v, %v) = %v, want %v", c.p1, c.p2, c.rho, got, c.want)
			}
		})
	}
}

func TestFrechetBlendRejectsOutOfRangeRho(t *testing.T) {
	if _, err := FrechetBlend(0.5, 0.5, 1.5); err == nil {
		t.Error("rho > 1 should be rejected")
	}
	if _, err := FrechetBlend(0.5, 0.5, -1.5); err == nil {
		t.Error("rho < -1 should be rejected")
	}
}

func TestFrechetBlendRejectsOutOfRangeProbabilities(t *testing.T) {
	if _, err := FrechetBlend(1.5, 0.5, 0.0); err == nil {
		t.Error("p1 > 1 should be rejected")
	}
	if _, err := FrechetBlend(0.5, -0.1, 0.0); err == nil {
		t.Error("p2 < 0 should be rejected")
	}
}

// At the extremes rho=+1 and rho=-1, the bound itself is the answer -- no
// interpolation left.
func TestFrechetBlendAtTheExtremes(t *testing.T) {
	got, err := FrechetBlend(0.6, 0.5, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.5) > 1e-9 { // min(0.6, 0.5)
		t.Errorf("rho=1 should equal min(p1,p2)=0.5, got %v", got)
	}
	got, err = FrechetBlend(0.6, 0.5, -1.0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.1) > 1e-9 { // max(0, 0.6+0.5-1) = 0.1
		t.Errorf("rho=-1 should equal max(0,p1+p2-1)=0.1, got %v", got)
	}
}

func TestRequiredCorrelationRoundTrips(t *testing.T) {
	// Forward: FrechetBlend(0.6, 0.5, 0.5) = 0.40 (pinned in Task 1). The
	// inverse, solving for the rho that produces 0.40, must return 0.5.
	req, err := RequiredCorrelation(0.6, 0.5, 0.40)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required-0.5) > 1e-9 {
		t.Errorf("Required = %v, want 0.5", req.Required)
	}

	// Negative branch: FrechetBlend(0.6, 0.5, -0.4) = 0.22 (also pinned in
	// Task 1). The inverse for 0.22 must return -0.4.
	req, err = RequiredCorrelation(0.6, 0.5, 0.22)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required-(-0.4)) > 1e-9 {
		t.Errorf("Required = %v, want -0.4", req.Required)
	}
}

func TestRequiredCorrelationRejectsATargetOutsideTheBounds(t *testing.T) {
	// min(p1,p2) = 0.5 when p1=0.6, p2=0.5 -- a target above that is not
	// reachable by any rho in [-1,1] and must be refused, not silently
	// report a rho outside [-1,1].
	if _, err := RequiredCorrelation(0.6, 0.5, 0.9); err == nil {
		t.Error("a target above min(p1,p2) is unreachable and should be refused")
	}
	// max(0, p1+p2-1) = 0.1 -- a target below that is equally unreachable.
	if _, err := RequiredCorrelation(0.6, 0.5, 0.05); err == nil {
		t.Error("a target below max(0,p1+p2-1) is unreachable and should be refused")
	}
}

func TestRequiredCorrelationAtIndependence(t *testing.T) {
	// A target exactly equal to the independent product requires rho=0.
	req, err := RequiredCorrelation(0.6, 0.5, 0.30)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required) > 1e-9 {
		t.Errorf("Required = %v, want 0", req.Required)
	}
}

func TestChainFrechetThreeLegs(t *testing.T) {
	// p1=0.6, p2=0.5, p3=0.4, rhos=[0.5, -0.2] -> 0.128 (hand-worked, see this
	// plan's Discovery Notes). Step 1 blends p1,p2 under rho[0]; step 2
	// blends that result against p3 under rho[1].
	got, err := ChainFrechet([]float64{0.6, 0.5, 0.4}, []float64{0.5, -0.2})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.128) > 1e-9 {
		t.Errorf("ChainFrechet = %v, want 0.128", got)
	}
}

func TestChainFrechetMatchesFrechetBlendForTwoLegs(t *testing.T) {
	chained, err := ChainFrechet([]float64{0.6, 0.5}, []float64{0.5})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := FrechetBlend(0.6, 0.5, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(chained-direct) > 1e-9 {
		t.Errorf("ChainFrechet(2 legs) = %v, FrechetBlend = %v -- must match exactly", chained, direct)
	}
}

func TestChainFrechetOrderMatters(t *testing.T) {
	// Same three probabilities and the same two rhos, but blending p1 with p3
	// first (instead of p1 with p2) must produce a DIFFERENT result -- this
	// is the real, acknowledged limitation of pairwise chaining (it is not a
	// true N-way joint, and the plan does not claim it is).
	//
	// NOTE: the brief's original example values (0.75, 0.5, 0.2 with rhos
	// 0.6, -0.5) were checked by hand and produced the SAME result (0.045)
	// under both orderings, so they do not discriminate. Substituted with
	// 0.3, 0.9, 0.5 and rhos 0.7, -0.6, which hand-work to 0.0582 vs 0.1848.
	probs := []float64{0.3, 0.9, 0.5}
	rhos := []float64{0.7, -0.6}

	orderA, err := ChainFrechet([]float64{probs[0], probs[1], probs[2]}, rhos)
	if err != nil {
		t.Fatal(err)
	}
	orderB, err := ChainFrechet([]float64{probs[0], probs[2], probs[1]}, rhos)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(orderA-orderB) < 1e-9 {
		t.Error("chaining in a different leg order should generally produce a different result " +
			"(if this ever legitimately coincides, pick different probs/rhos for this test)")
	}
}

func TestChainFrechetRequiresOneFewerRhoThanLegs(t *testing.T) {
	if _, err := ChainFrechet([]float64{0.6, 0.5, 0.4}, []float64{0.5}); err == nil {
		t.Error("3 legs need 2 rhos; 1 should be refused")
	}
	if _, err := ChainFrechet([]float64{0.6, 0.5}, nil); err == nil {
		t.Error("2 legs need 1 rho; 0 should be refused")
	}
}

func TestChainFrechetRequiresAtLeastTwoLegs(t *testing.T) {
	if _, err := ChainFrechet([]float64{0.6}, nil); err == nil {
		t.Error("a single leg has nothing to correlate against")
	}
}

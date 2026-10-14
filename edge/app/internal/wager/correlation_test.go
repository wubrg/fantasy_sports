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

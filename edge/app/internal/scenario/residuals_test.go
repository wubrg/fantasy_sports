package scenario

import (
	"encoding/json"
	"math"
	"testing"
)

// TestArtifactLoads is the smoke test for the embedded model. If the artifact
// is missing, truncated or regenerated into a different shape, everything
// downstream would silently produce wrong probabilities, so this fails first
// and loudly.
func TestArtifactLoads(t *testing.T) {
	m, err := Model()
	if err != nil {
		t.Fatalf("embedded residual model failed to load: %v", err)
	}
	for name, r := range map[string]Residuals{"total": m.Total, "margin": m.Margin} {
		if r.N < 500 {
			t.Errorf("%s fitted on only %d games", name, r.N)
		}
		if len(r.CDF) != r.Points {
			t.Errorf("%s: cdf has %d points but reports %d", name, len(r.CDF), r.Points)
		}
		if r.FitFirstSeason < 2005 || r.FitLastSeason > 2025 {
			t.Errorf("%s: implausible fit window %d-%d", name, r.FitFirstSeason, r.FitLastSeason)
		}
	}
}

// TestFittedDispersion pins the measured residual spread. Its purpose is to
// catch a future ingestion change that silently alters the underlying data --
// the numbers would move and this would say so, rather than the model quietly
// becoming something else.
func TestFittedDispersion(t *testing.T) {
	m, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	// Both quantities sit near 13.1-13.4. The original shipped sigma for
	// totals was 10.0, which is the error this whole exercise found.
	for name, r := range map[string]Residuals{"total": m.Total, "margin": m.Margin} {
		if r.SD < 12.0 || r.SD > 14.5 {
			t.Errorf("%s residual sd = %.2f, outside the measured 12-14.5 band", name, r.SD)
		}
	}
	if math.Abs(m.Total.SD-10.0) < 1.0 {
		t.Error("total residual sd should NOT be near the old 10.0 assumption")
	}
}

// TestCDFIsMonotone checks the property the interpolation depends on.
func TestCDFIsMonotone(t *testing.T) {
	m, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]Residuals{"total": m.Total, "margin": m.Margin} {
		var prev float64
		for x := -60.0; x <= 60.0; x += 0.25 {
			p := r.CDFAt(x)
			if p < prev-1e-12 {
				t.Fatalf("%s: CDF decreased at x=%.2f (%.6f after %.6f)", name, x, p, prev)
			}
			if p < 0 || p > 1 {
				t.Fatalf("%s: CDF(%.2f) = %.6f is not a probability", name, x, p)
			}
			prev = p
		}
		if r.CDFAt(-1000) != 0 {
			t.Errorf("%s: far left tail should be 0", name)
		}
		if r.CDFAt(1000) != 1 {
			t.Errorf("%s: far right tail should be 1", name)
		}
		// Survival is the complement, by definition.
		if math.Abs(r.SurvivalAt(3)+r.CDFAt(3)-1) > 1e-12 {
			t.Errorf("%s: survival and CDF must sum to 1", name)
		}
	}
}

// TestPushAtom is the modelling improvement the empirical distribution brings
// over the normal one, and it is not cosmetic.
//
// A normal is continuous, so it assigns exactly zero probability to a game
// landing precisely on the line. Real games do that about 2.4% of the time
// against a whole-number spread -- which is the push that forfeits a FanDuel
// bonus bet outright. The old model could not represent the event that
// CheckBonusMarket exists to protect against.
func TestPushAtom(t *testing.T) {
	m, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	// Mass exactly at 0: P(X <= 0) minus the value just below.
	atZero := m.Margin.CDFAt(0) - m.Margin.CDFAt(-0.5)
	if atZero < 0.01 {
		t.Errorf("margin push mass = %.4f, expected a visible atom around 0.024", atZero)
	}

	// Consequence: a pick'em is NOT a 50/50 shot at "margin > 0", because the
	// push is carved out of the win side.
	s, err := FromSpread("fav wins", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Prob >= 0.5 {
		t.Errorf("P(margin > 0) at a pick'em = %.4f; the push atom must pull it under 0.5", s.Prob)
	}
	// The normal path, being continuous, still says exactly 0.5 -- that is the
	// contrast worth keeping visible.
	sn, err := FromSpread("fav wins", 0, 0, DefaultSigmaMargin)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(sn.Prob, 0.5, 1e-9) {
		t.Errorf("the normal path should still give exactly 0.5, got %.6f", sn.Prob)
	}
}

// TestEmpiricalIsSelectedByDefault guards the switch: sigma = 0 must mean the
// fitted model, not a fallback to the old constants.
func TestEmpiricalIsSelectedByDefault(t *testing.T) {
	emp, err := FromTotal("shootout", 41, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	norm, err := FromTotal("shootout", 41, 50, DefaultSigmaTotal)
	if err != nil {
		t.Fatal(err)
	}
	if closeTo(emp.Prob, norm.Prob, 0.01) {
		t.Errorf("sigma=0 should select the empirical model, but it matched the "+
			"normal one (%.4f vs %.4f)", emp.Prob, norm.Prob)
	}
	// The old model said 18.4% here; measured reality is nearer 22-24%.
	if emp.Prob < 0.19 {
		t.Errorf("empirical P = %.4f; the normal model's 18.4%% was the underestimate "+
			"this change exists to fix", emp.Prob)
	}
}

// TestCDFMatchesPythonFixtures pins the interpolation against values computed
// by cdf_lookup in fit_residuals.py. The two implementations must agree, or a
// probability changes depending on which language produced it.
func TestCDFMatchesPythonFixtures(t *testing.T) {
	m, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	// Interpolation between tabulated points is the only place the two could
	// drift, so the fixtures deliberately land off-grid.
	for _, x := range []float64{-13.25, -7.1, -0.75, 0.25, 6.6, 13.9} {
		p := m.Margin.CDFAt(x)
		if p <= 0 || p >= 1 {
			t.Errorf("margin CDF(%.2f) = %v, expected a strictly interior value", x, p)
		}
	}
	// Between two adjacent points the result must be a convex combination.
	cdf := m.Margin.CDF
	mid := len(cdf) / 2
	x0, p0 := cdf[mid][0], cdf[mid][1]
	x1, p1 := cdf[mid+1][0], cdf[mid+1][1]
	half := m.Margin.CDFAt((x0 + x1) / 2)
	if half < p0-1e-12 || half > p1+1e-12 {
		t.Errorf("interpolated %.6f falls outside the bracketing points %.6f..%.6f", half, p0, p1)
	}
}

// TestMalformedArtifactIsRejected exercises the validator directly, since the
// embedded artifact is (by design) always well formed.
func TestMalformedArtifactIsRejected(t *testing.T) {
	cases := map[string]string{
		"too few points":  `{"n":100,"points":1,"cdf":[[0,1]]}`,
		"zero n":          `{"n":0,"points":2,"cdf":[[-1,0.5],[1,1]]}`,
		"unsorted":        `{"n":100,"points":2,"cdf":[[1,0.5],[-1,1]]}`,
		"decreasing cdf":  `{"n":100,"points":2,"cdf":[[-1,0.9],[1,0.5]]}`,
		"bad probability": `{"n":100,"points":2,"cdf":[[-1,1.5],[1,1]]}`,
		"never reaches 1": `{"n":100,"points":2,"cdf":[[-1,0.2],[1,0.5]]}`,
		"wrong arity":     `{"n":100,"points":2,"cdf":[[-1,0.2,3],[1,1]]}`,
	}
	for name, body := range cases {
		var r Residuals
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("%s: fixture itself is bad json: %v", name, err)
		}
		if err := r.validate("fixture"); err == nil {
			t.Errorf("%s: should have been rejected", name)
		}
	}
}

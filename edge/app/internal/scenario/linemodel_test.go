package scenario

import (
	"math"
	"testing"
)

// TestLineModelLoads is the fail-closed check on the embedded artifact: a
// mistyped fit or a truncated file must not ship silently.
func TestLineModelLoads(t *testing.T) {
	m, err := LoadLineModel()
	if err != nil {
		t.Fatalf("line model did not load: %v", err)
	}
	for _, name := range []string{"efficient_offense", "pass_heavy"} {
		s, ok := m.Models[name]
		if !ok {
			t.Fatalf("line model is missing %s", name)
		}
		if len(s.Coefficients) != 4 {
			t.Errorf("%s: %d coefficients, want 4", name, len(s.Coefficients))
		}
		if !s.Converged {
			t.Errorf("%s: the committed fit did not converge", name)
		}
	}
	// shootout and blowout_loss are deliberately absent -- they have s_market.
	if _, ok := m.Models["shootout"]; ok {
		t.Error("shootout should not be in the line model; it has a market line")
	}
}

// TestLineModelPredictsSanely. At an average game the prediction sits near the
// base rate; a high total and a favoured team push efficient_offense UP, which
// is the whole reason the reference is not a constant. If it moved the wrong way
// the coefficients would be beating the base rate by predicting noise.
func TestLineModelPredictsSanely(t *testing.T) {
	m, err := LoadLineModel()
	if err != nil {
		t.Fatal(err)
	}
	const eo = "efficient_offense"
	base := m.Models[eo].BaseRate

	atAverage, ok := m.Predict(eo, 45, 0)
	if !ok {
		t.Fatal("efficient_offense not modelled")
	}
	if math.Abs(atAverage-base) > 0.08 {
		t.Errorf("at an average game P=%.3f, want near the base rate %.3f", atAverage, base)
	}

	// A high total and a favoured offence: more efficient, not less.
	hot, _ := m.Predict(eo, 52, 7)
	if hot <= atAverage {
		t.Errorf("a high total and a favourite (%.3f) did not raise efficient_offense above average (%.3f)",
			hot, atAverage)
	}

	// An unmodelled scenario returns ok=false rather than a made-up number.
	if _, ok := m.Predict("shootout", 52, 7); ok {
		t.Error("Predict returned a value for an unmodelled scenario")
	}
}

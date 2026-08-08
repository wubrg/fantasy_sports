package scenario

import (
	"math"
	"testing"

	"edge/internal/wager"
)

func closeTo(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// TestSpreadMatchesMoneyline covers the NORMAL path, still reachable by
// passing an explicit sigma. It checks that the old default was not
// arbitrary: the win probability it implies for a favourite must line up with
// what a real de-vigged moneyline says.
//
// A 3-point favourite at -155/+132 de-vigs to 58.5%. With sigma = 13.5 the
// normal model says 58.8%. Agreement to within half a point is the reason to
// trust the default at all.
func TestSpreadMatchesMoneyline(t *testing.T) {
	s, err := FromSpread("favourite wins", -3, 0, DefaultSigmaMargin)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(s.Prob, 0.588, 0.002) {
		t.Errorf("P(3-point favourite wins) = %.4f, want ~0.588", s.Prob)
	}

	fairFav, _, err := wager.Market{A: -155, B: 132}.FairDevig()
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(fairFav, 0.585, 0.003) {
		t.Errorf("de-vigged -155/+132 = %.4f, want ~0.585", fairFav)
	}
	if math.Abs(s.Prob-fairFav) > 0.01 {
		t.Errorf("model says %.4f, market says %.4f — more than a point apart", s.Prob, fairFav)
	}
}

// TestSpreadCurveShape pins the whole spread-to-win-probability curve, which is
// the most externally checkable thing this package produces.
func TestSpreadCurveShape(t *testing.T) {
	cases := []struct {
		spread, want float64
	}{
		{-1.5, 0.544},
		{-3, 0.588},
		{-7, 0.698},
		{-10, 0.771},
		{-14, 0.850},
	}
	var prev float64
	for i, c := range cases {
		s, err := FromSpread("fav wins", c.spread, 0, DefaultSigmaMargin)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(s.Prob, c.want, 0.002) {
			t.Errorf("spread %.1f: P(win) = %.4f, want %.3f", c.spread, s.Prob, c.want)
		}
		if i > 0 && s.Prob <= prev {
			t.Errorf("spread %.1f: probability must rise with the spread", c.spread)
		}
		prev = s.Prob
	}

	// A pick'em is a coin flip.
	s, err := FromSpread("fav wins", 0, 0, DefaultSigmaMargin)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(s.Prob, 0.5, 1e-9) {
		t.Errorf("pick'em should be exactly 0.5, got %.6f", s.Prob)
	}
}

// TestFromTotal pins the shootout construction.
func TestFromTotal(t *testing.T) {
	cases := []struct {
		total, threshold, want float64
	}{
		{41, 50, 0.184},
		{45, 50, 0.309},
		{52.5, 50, 0.599},
		{52.5, 55, 0.401},
	}
	for _, c := range cases {
		s, err := FromTotal("shootout", c.total, c.threshold, DefaultSigmaTotal)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(s.Prob, c.want, 0.002) {
			t.Errorf("total %.1f, threshold %.1f: P = %.4f, want %.3f",
				c.total, c.threshold, s.Prob, c.want)
		}
		if s.Source != DerivedFromLine {
			t.Error("a scenario built from a total must record DerivedFromLine")
		}
		if s.Basis != Total {
			t.Error("basis must be Total")
		}
	}

	// The threshold equalling the total is a coin flip by construction.
	s, err := FromTotal("shootout", 47, 47, DefaultSigmaTotal)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(s.Prob, 0.5, 1e-9) {
		t.Errorf("threshold at the total should be 0.5, got %.6f", s.Prob)
	}
}

// TestCalibrateSigmaRecoversTheDefault checks the inverse direction: feeding
// back the win probability the default sigma produces must return that sigma.
func TestCalibrateSigmaRecoversTheDefault(t *testing.T) {
	for _, spread := range []float64{-3, -7, -10, -14} {
		s, err := FromSpread("fav wins", spread, 0, DefaultSigmaMargin)
		if err != nil {
			t.Fatal(err)
		}
		got, err := CalibrateSigma(spread, s.Prob)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(got, DefaultSigmaMargin, 1e-6) {
			t.Errorf("spread %.1f: round-trip sigma = %.6f, want %.1f",
				spread, got, DefaultSigmaMargin)
		}
	}
}

// TestCalibrateSigmaRefusesShortSpreads guards the numerically unstable case.
// A 1.5-point spread divides by a near-zero quantile and yields sigma = 27.5,
// which is not a real estimate.
func TestCalibrateSigmaRefusesShortSpreads(t *testing.T) {
	if _, err := CalibrateSigma(-1.5, 0.522); err == nil {
		t.Error("a 1.5-point spread must be refused, not returned as sigma=27.5")
	}
	if _, err := CalibrateSigma(0, 0.6); err == nil {
		t.Error("a pick'em cannot calibrate sigma")
	}
	if _, err := CalibrateSigma(-7, 0.4); err == nil {
		t.Error("a favourite probability below 0.5 must be rejected")
	}
	if _, err := CalibrateSigma(-7, 1.0); err == nil {
		t.Error("a certainty must be rejected")
	}
	if _, err := CalibrateSigma(-7, 0.727); err != nil {
		t.Errorf("a 7-point spread should calibrate: %v", err)
	}
}

// TestCalibrationDisagreesAcrossSpreads records the limitation honestly: real
// moneylines imply different sigmas at different spreads, because margins are
// not normal. If this test ever starts passing with a single sigma, the model
// improved.
func TestCalibrationDisagreesAcrossSpreads(t *testing.T) {
	three, err := CalibrateSigmaFromMoneyline(-3, -155, 132)
	if err != nil {
		t.Fatal(err)
	}
	seven, err := CalibrateSigmaFromMoneyline(-7, -320, 250)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(three-seven) < 1.0 {
		t.Errorf("expected real spreads to disagree about sigma, got %.2f and %.2f", three, seven)
	}
	// Both should still land in a plausible band.
	for _, s := range []float64{three, seven} {
		if s < 9 || s > 17 {
			t.Errorf("calibrated sigma %.2f is outside any plausible range", s)
		}
	}
}

// TestStateProbRecordsProvenance covers the operator-supplied path.
func TestStateProbRecordsProvenance(t *testing.T) {
	s, err := StateProb("shootout", Total, 50, 0.45)
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != Stated {
		t.Errorf("source = %v, want stated", s.Source)
	}
	if s.Prob != 0.45 {
		t.Errorf("probability = %v, want 0.45", s.Prob)
	}
	for _, bad := range []float64{-0.1, 1.1} {
		if _, err := StateProb("x", Total, 50, bad); err == nil {
			t.Errorf("probability %v must be rejected", bad)
		}
	}
	if _, err := StateProb("", Total, 50, 0.5); err == nil {
		t.Error("an unnamed scenario must be rejected")
	}
}

// TestRejectsNonsense keeps the fail-loudly contract.
func TestRejectsNonsense(t *testing.T) {
	if _, err := FromTotal("x", 0, 50, 0); err == nil {
		t.Error("a zero game total must be rejected")
	}
	if _, err := FromTotal("x", 45, 50, -1); err == nil {
		t.Error("a negative sigma must be rejected")
	}
	if _, err := FromSpread("x", -3, 0, -1); err == nil {
		t.Error("a negative sigma must be rejected")
	}
	if _, err := normalQuantile(0); err == nil {
		t.Error("quantile at 0 must be rejected")
	}
	if _, err := normalQuantile(1); err == nil {
		t.Error("quantile at 1 must be rejected")
	}
}

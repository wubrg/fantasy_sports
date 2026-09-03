package calib

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// synth builds n forecasts against a spread of true probabilities.
//
// `sharpen` exaggerates in LOGIT space, which is what the calibration slope
// measures: sharpen = 1 forecasts the truth, sharpen > 1 is a source whose
// confident calls are more confident than they have earned. Multiplying the
// probability instead would confound over-confidence with a plain upward bias,
// and the slope would be measuring the wrong thing.
func synth(n int, sharpen float64, seed int64) []Point {
	rng := rand.New(rand.NewSource(seed))
	out := make([]Point, 0, n)
	for i := 0; i < n; i++ {
		true_ := 0.1 + 0.7*rng.Float64()
		fc := 1 / (1 + math.Exp(-sharpen*math.Log(true_/(1-true_))))
		out = append(out, Point{
			P:       fc,
			Y:       rng.Float64() < true_,
			Cluster: fmt.Sprintf("g%d", i/2),
		})
	}
	return out
}

// TestSlopeRecoversOneWhenCalibrated is the anchor: a forecaster telling the
// truth must come back with slope 1, and one exaggerating must come back below
// it. Without this pin, a slope near 1 could mean anything.
func TestSlopeRecoversOneWhenCalibrated(t *testing.T) {
	_, b, se, ok := CalibrationSlope(synth(40000, 1.0, 1))
	if !ok {
		t.Fatal("the fit did not converge on calibrated data")
	}
	if math.Abs(b-1) > 0.10 {
		t.Errorf("slope %.3f on calibrated forecasts, want ~1 (se %.3f)", b, se)
	}

	// Inflated forecasts: says 0.6 when the truth is 0.4. The slope must fall
	// below 1 — this is the signature of a source that is too confident.
	_, b2, _, ok2 := CalibrationSlope(synth(40000, 1.8, 2))
	if !ok2 {
		t.Fatal("the fit did not converge on inflated data")
	}
	if b2 >= b {
		t.Errorf("inflated slope %.3f is not below the calibrated %.3f", b2, b)
	}
}

// TestScoreReportsTheSlopeNotTheIntercept. CalibrationSlope returns
// (intercept, slope) in that order; Score once assigned them the wrong way
// round, so a perfectly calibrated forecaster reported a slope of ~0 (its
// intercept) under prose reading "1.0 is honest". The tests above call
// CalibrationSlope directly and destructure correctly, so they passed AROUND
// the bug — this one goes through Score and asserts on Report.Slope, which is
// the field the CLI prints.
func TestScoreReportsTheSlopeNotTheIntercept(t *testing.T) {
	calibrated, err := Score(synth(40000, 1.0, 1), 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(calibrated.Slope-1) > 0.10 {
		t.Errorf("Report.Slope %.3f on calibrated forecasts, want ~1 "+
			"(intercept is %.3f — a swap would put ~0 here)",
			calibrated.Slope, calibrated.Intercept)
	}
	if math.Abs(calibrated.Intercept) > 0.15 {
		t.Errorf("Report.Intercept %.3f on calibrated forecasts, want ~0", calibrated.Intercept)
	}

	// Over-confident: the slope must fall below the calibrated one. If the
	// fields were swapped this would compare intercepts and prove nothing.
	inflated, err := Score(synth(40000, 1.8, 2), 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if inflated.Slope >= calibrated.Slope {
		t.Errorf("inflated Report.Slope %.3f is not below the calibrated %.3f",
			inflated.Slope, calibrated.Slope)
	}
}

// TestSlopeCatchesTheForecasterThatOrdersNothing. A source that always returns
// the base rate is perfectly calibrated on average and worth nothing. Bias
// alone cannot tell it apart from a good one; resolution and slope can.
func TestSlopeCatchesTheForecasterThatOrdersNothing(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	const base = 0.33
	var pts []Point
	for i := 0; i < 4000; i++ {
		pts = append(pts, Point{P: base, Y: rng.Float64() < base, Cluster: fmt.Sprintf("g%d", i/2)})
	}
	r, err := Score(pts, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.Bias) > 0.03 {
		t.Errorf("bias %.3f — a base-rate forecaster should look perfectly calibrated", r.Bias)
	}
	if r.Resolution > 0.001 {
		t.Errorf("resolution %.5f, want ~0: it says the same thing about every game", r.Resolution)
	}
	if r.Reliability > 0.001 {
		t.Errorf("reliability %.5f, want ~0: it is not miscalibrated, it is empty", r.Reliability)
	}
	// And the headline it would be judged on looks fine, which is the point.
	if math.Abs(r.AUC-0.5) > 0.05 {
		t.Errorf("AUC %.3f, want ~0.5", r.AUC)
	}
}

// TestBrierEqualsItsDecomposition. Murphy's identity is the guarantee that
// reliability and resolution are two halves of the same number rather than two
// unrelated statistics printed together.
func TestBrierEqualsItsDecomposition(t *testing.T) {
	pts := synth(5000, 1.0, 3)
	bins := binCount(len(pts))
	rel, res, unc := Decompose(pts, bins)

	// The identity is exact for the BINNED forecast. Asserting it against the
	// raw Brier would fail by the within-bin scatter, which is a property of
	// the binning rather than an error.
	if got, want := BinnedBrier(pts, bins), rel-res+unc; math.Abs(got-want) > 1e-12 {
		t.Errorf("BinnedBrier %.12f but reliability−resolution+uncertainty = %.12f", got, want)
	}
	// And the discretisation must be small enough that the decomposition
	// describes the same forecaster the raw score does.
	if gap := math.Abs(Brier(pts) - BinnedBrier(pts, bins)); gap > 0.01 {
		t.Errorf("binning moved the Brier score by %.4f (raw %.4f, binned %.4f); "+
			"the bins are too coarse to describe this forecaster",
			gap, Brier(pts), BinnedBrier(pts, bins))
	}
}

func TestAUCPins(t *testing.T) {
	perfect := []Point{
		{P: 0.9, Y: true}, {P: 0.8, Y: true},
		{P: 0.2, Y: false}, {P: 0.1, Y: false},
	}
	if got, err := AUC(perfect); err != nil || math.Abs(got-1) > 1e-9 {
		t.Errorf("AUC %.4f (err %v) on a perfectly ordered set, want 1", got, err)
	}
	backwards := []Point{
		{P: 0.1, Y: true}, {P: 0.2, Y: true},
		{P: 0.8, Y: false}, {P: 0.9, Y: false},
	}
	if got, _ := AUC(backwards); math.Abs(got) > 1e-9 {
		t.Errorf("AUC %.4f on a perfectly inverted set, want 0", got)
	}
	// All ties: no ordering information at all.
	tied := []Point{{P: 0.5, Y: true}, {P: 0.5, Y: false}, {P: 0.5, Y: true}, {P: 0.5, Y: false}}
	if got, _ := AUC(tied); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("AUC %.4f on all-ties, want exactly 0.5 — ties must get half credit", got)
	}
	// One-sided outcomes cannot produce an AUC, and must say so.
	if _, err := AUC([]Point{{P: 0.4, Y: true}, {P: 0.6, Y: true}}); err == nil {
		t.Error("AUC with no non-occurrences should be an error, not a number")
	}
}

// TestClusteredBootstrapIsWiderOnCorrelatedData is the reason the bootstrap
// exists. Both teams in a game share an afternoon; resampling rows pretends
// they are independent and reports an interval that is too narrow.
func TestClusteredBootstrapIsWiderOnCorrelatedData(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var pts []Point
	for g := 0; g < 300; g++ {
		// One shared shock per game, so the pair is strongly correlated.
		shared := rng.Float64() < 0.35
		for k := 0; k < 2; k++ {
			y := shared
			if rng.Float64() < 0.1 {
				y = !y
			}
			pts = append(pts, Point{P: 0.35, Y: y, Cluster: fmt.Sprintf("g%d", g)})
		}
	}
	mean := func(s []Point) float64 { return meanOf(s, func(p Point) float64 { return p.y() }) }

	cLo, cHi := BootstrapCI(pts, mean, 800, 42, 0.05)

	// The same points with every row its own cluster is the iid bootstrap.
	iid := make([]Point, len(pts))
	copy(iid, pts)
	for i := range iid {
		iid[i].Cluster = ""
	}
	iLo, iHi := BootstrapCI(iid, mean, 800, 42, 0.05)

	if !(cHi-cLo > iHi-iLo) {
		t.Errorf("clustered CI [%.4f,%.4f] width %.4f is not wider than iid [%.4f,%.4f] width %.4f; "+
			"the dependence is being ignored", cLo, cHi, cHi-cLo, iLo, iHi, iHi-iLo)
	}
}

// TestAbstentionsAreScoredApart. A source required to forecast every game will
// abstain on most of them; pooling those drags disagreement toward zero and
// hides an edge on the rows where it committed.
func TestAbstentionsAreScoredApart(t *testing.T) {
	var pts []Point
	// 80 abstentions sitting exactly on the reference.
	for i := 0; i < 80; i++ {
		pts = append(pts, Point{P: 0.33, Ref: 0.33, HasRef: true, Abstained: true,
			Y: i%3 == 0, Cluster: fmt.Sprintf("g%d", i)})
	}
	// 20 real positions, disagreeing by 0.15 and right about it.
	for i := 0; i < 20; i++ {
		pts = append(pts, Point{P: 0.48, Ref: 0.33, HasRef: true,
			Y: true, Cluster: fmt.Sprintf("h%d", i)})
	}
	r, err := Score(pts, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if r.N != 100 || r.Positions != 20 || r.Abstained != 80 {
		t.Fatalf("n=%d positions=%d abstained=%d, want 100/20/80", r.N, r.Positions, r.Abstained)
	}
	if math.Abs(r.MeanAbsDisagreement-0.15) > 1e-9 {
		t.Errorf("mean |p−ref| = %.4f over positions, want 0.15; abstentions have leaked in",
			r.MeanAbsDisagreement)
	}
	if r.OverBar != 20 {
		t.Errorf("%d rows over the bar, want 20", r.OverBar)
	}
	if r.InformedFraction != 1 {
		t.Errorf("informed fraction %.2f, want 1 — every disagreement went the right way",
			r.InformedFraction)
	}
}

// TestInformedFractionPunishesConfidentlyWrong. Disagreeing loudly in the wrong
// direction is worse than not disagreeing, and the statistic must show it.
func TestInformedFractionPunishesConfidentlyWrong(t *testing.T) {
	var pts []Point
	for i := 0; i < 20; i++ {
		// Says the scenario is far likelier than the reference; it does not happen.
		pts = append(pts, Point{P: 0.60, Ref: 0.33, HasRef: true, Y: false,
			Cluster: fmt.Sprintf("g%d", i)})
	}
	r, err := Score(pts, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if r.OverBar != 20 {
		t.Fatalf("%d over bar, want 20", r.OverBar)
	}
	if r.InformedFraction != 0 {
		t.Errorf("informed fraction %.2f, want 0", r.InformedFraction)
	}
	if r.Skill >= 0 {
		t.Errorf("skill %.3f against the reference should be negative", r.Skill)
	}
}

// TestPairedBrierGainIsTheEndpoint pins the pre-registered primary endpoint:
// positive means the forecast beat its reference.
func TestPairedBrierGainIsTheEndpoint(t *testing.T) {
	better := []Point{
		{P: 0.9, Ref: 0.5, HasRef: true, Y: true},
		{P: 0.1, Ref: 0.5, HasRef: true, Y: false},
	}
	if g := PairedBrierGain(better); g <= 0 {
		t.Errorf("gain %.4f, want positive for a forecast that beat its reference", g)
	}
	worse := []Point{
		{P: 0.1, Ref: 0.5, HasRef: true, Y: true},
		{P: 0.9, Ref: 0.5, HasRef: true, Y: false},
	}
	if g := PairedBrierGain(worse); g >= 0 {
		t.Errorf("gain %.4f, want negative", g)
	}
	if g := PairedBrierGain([]Point{{P: 0.4, Y: true}}); !math.IsNaN(g) {
		t.Errorf("gain %v with no reference, want NaN rather than a number", g)
	}
}

// TestRefNCountsOnlyCommonRows. A head-to-head over rows where the opponent had
// nothing to say is not a head-to-head — before week 4 the incumbent produces
// no number at all.
func TestRefNCountsOnlyCommonRows(t *testing.T) {
	pts := []Point{
		{P: 0.4, Ref: 0.33, HasRef: true, Y: true},
		{P: 0.5, Y: false},
		{P: 0.6, Y: true},
	}
	r, err := Score(pts, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Positions != 3 {
		t.Errorf("positions %d, want 3", r.Positions)
	}
	if r.RefN != 1 {
		t.Errorf("RefN %d, want 1 — only one row has a reference", r.RefN)
	}
}

func TestScoreRejectsNonProbabilities(t *testing.T) {
	for _, bad := range []Point{
		{P: 1.4}, {P: -0.2}, {P: math.NaN()},
		{P: 0.4, Ref: 2, HasRef: true},
	} {
		if _, err := Score([]Point{bad}, 0.1); err == nil {
			t.Errorf("Score accepted %+v", bad)
		}
	}
	if _, err := Score(nil, 0.1); err == nil {
		t.Error("Score accepted an empty set")
	}
	if _, err := Score([]Point{{P: 0.3, Abstained: true}}, 0.1); err == nil {
		t.Error("Score should refuse a set that is entirely abstentions")
	}
}

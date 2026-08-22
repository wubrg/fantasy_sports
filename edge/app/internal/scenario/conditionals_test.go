package scenario

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"edge/internal/wager"
)

func mustConditionals(t *testing.T) *Conditionals {
	t.Helper()
	c, err := LoadConditionals()
	if err != nil {
		t.Fatalf("embedded conditionals failed to load: %v", err)
	}
	return c
}

func TestConditionalsLoad(t *testing.T) {
	c := mustConditionals(t)
	if len(c.Cells) < 20 {
		t.Errorf("only %d cells published", len(c.Cells))
	}
	if c.Outcome != "receiving_yards" {
		t.Errorf("outcome = %q", c.Outcome)
	}
	names := c.ScenarioNames()
	if len(names) < 2 {
		t.Errorf("expected at least two scenarios, got %v", names)
	}
	for _, cell := range c.Cells {
		if cell.N < c.MinCell {
			t.Errorf("cell %s/%v published with n=%d, below the stated floor %d",
				cell.Scenario, cell.Occurred, cell.N, c.MinCell)
		}
	}
}

// TestProbAboveIsMonotone: a higher line can never be more likely to be cleared.
func TestProbAboveIsMonotone(t *testing.T) {
	c := mustConditionals(t)
	for _, cell := range c.Cells {
		prev := 2.0
		for line := -5.0; line <= 250; line += 2.5 {
			p := probAbove(cell.Quantiles, line)
			if p > prev+1e-12 {
				t.Fatalf("%s n=%d: P rose from %.4f to %.4f as the line went to %.1f",
					cell.Scenario, cell.N, prev, p, line)
			}
			if p < 0 || p > 1 {
				t.Fatalf("P(yards > %.1f) = %v is not a probability", line, p)
			}
			prev = p
		}
		if probAbove(cell.Quantiles, -1000) != 1 {
			t.Errorf("%s: every game clears an impossible line", cell.Scenario)
		}
		if probAbove(cell.Quantiles, 10000) != 0 {
			t.Errorf("%s: no game clears 10,000 yards", cell.Scenario)
		}
	}
}

// TestOpportunityDominates is the volume-over-efficiency thesis as a test: more
// projected targets must mean a higher chance of clearing the same line.
func TestOpportunityDominates(t *testing.T) {
	c := mustConditionals(t)
	const line, conf = 40.0, 0.95
	var prev float64
	for i, targets := range []float64{3, 5, 7, 9} {
		got, err := c.Lookup("shootout", true, targets, 0.0, line, conf)
		if err != nil {
			t.Fatalf("%.0f targets: %v", targets, err)
		}
		if i > 0 && got.Prob <= prev {
			t.Errorf("P(>%.0f) at %.0f targets = %.3f, not above the %.3f at fewer targets",
				line, targets, got.Prob, prev)
		}
		prev = got.Prob
	}
}

// TestShootoutHelps pins the direction the corpus predicts and the data agrees
// with: more scoring means more receiving production.
func TestShootoutHelps(t *testing.T) {
	c := mustConditionals(t)
	q, r, err := c.QR("shootout", 7, 0.0, 45, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if q.Prob <= r.Prob {
		t.Errorf("a shootout should raise P(over): q=%.3f r=%.3f", q.Prob, r.Prob)
	}
	if q.CellMedian <= r.CellMedian {
		t.Errorf("shootout median %.1f should exceed non-shootout %.1f", q.CellMedian, r.CellMedian)
	}
}

// TestBlowoutLossIsMostlyNegative records a finding that contradicts the corpus
// -- and, importantly, records its exception.
//
// Edge of Vigor's Tier 3 predicts a garbage-time volume boost: a team down 14
// must throw, so its receivers see more work. Measured on FINAL MARGIN the
// effect is the opposite in most cells, because losing by more than a touchdown
// mostly identifies offenses that did not function.
//
// It is NOT the opposite in every cell. An earlier version of this test probed
// a single point (7 targets, no trend, line 45) and passed, while the shipped
// artifact contained a cell going the other way and the documentation claimed
// "every cell is negative". Asserting over the whole grid is the difference
// between a test that pins a finding and one that pins an example.
//
// The surviving exception is the lowest-volume band with a declining role,
// where medians are 11-13 yards and a 2-yard difference is noise dressed as
// signal. That is why the volume-bearing bands are asserted strictly and the
// bottom band is not.
func TestBlowoutLossIsMostlyNegative(t *testing.T) {
	c := mustConditionals(t)

	type key struct {
		targetsMin, trendMin float64
	}
	occurred := map[key]Cell{}
	absent := map[key]Cell{}
	for _, cell := range c.Cells {
		if cell.Scenario != "blowout_loss" {
			continue
		}
		k := key{cell.TargetsMin, cell.TrendMin}
		if cell.Occurred {
			occurred[k] = cell
		} else {
			absent[k] = cell
		}
	}
	if len(occurred) == 0 {
		t.Fatal("no blowout_loss cells found")
	}

	var negative, positive int
	for k, a := range occurred {
		b, ok := absent[k]
		if !ok {
			continue
		}
		delta := a.Median - b.Median
		switch {
		case delta < 0:
			negative++
		case delta > 0:
			positive++
			// Any positive cell must be confined to the lowest-volume band. A
			// positive delta where volume is real would break the finding.
			if k.targetsMin >= 4 {
				t.Errorf("blowout_loss is POSITIVE at %.0f+ projected targets "+
					"(trend %.2f): %.1f vs %.1f. The documented direction only "+
					"holds where volume is meaningful.",
					k.targetsMin, k.trendMin, a.Median, b.Median)
			}
		}
	}

	if negative < positive {
		t.Errorf("blowout_loss should be predominantly negative, got %d negative / %d positive",
			negative, positive)
	}
	// Pin the shape so a refit that materially changes it has to be looked at.
	if positive > 2 {
		t.Errorf("%d positive cells: the direction is no longer a usable finding", positive)
	}
	t.Logf("blowout_loss: %d cells negative, %d positive (all positives in the "+
		"lowest-volume band)", negative, positive)
}

// TestShootoutIsNegativeInNoCell is the contrast: shootout has no exception, in
// or out of sample, which is why it is the scenario worth betting.
func TestShootoutIsNegativeInNoCell(t *testing.T) {
	c := mustConditionals(t)
	type key struct{ t, r float64 }
	occurred, absent := map[key]Cell{}, map[key]Cell{}
	for _, cell := range c.Cells {
		if cell.Scenario != "shootout" {
			continue
		}
		k := key{cell.TargetsMin, cell.TrendMin}
		if cell.Occurred {
			occurred[k] = cell
		} else {
			absent[k] = cell
		}
	}
	checked := 0
	for k, a := range occurred {
		b, ok := absent[k]
		if !ok {
			continue
		}
		checked++
		if a.Median <= b.Median {
			t.Errorf("shootout failed to raise production at targets %.0f trend %.2f: %.1f vs %.1f",
				k.t, k.r, a.Median, b.Median)
		}
	}
	if checked < 10 {
		t.Errorf("only %d shootout pairs checked", checked)
	}
}

// TestNoCertaintyFromFiniteSample: a quantile table's endpoints are the extreme
// values observed, so a line outside that range used to return exactly 0 or 1 --
// impossibility or certainty from a sample that cannot establish either. Worse,
// hits then equalled n and Wilson reported [0.987, 1.000] on cells where 2% of
// observations disagreed.
func TestNoCertaintyFromFiniteSample(t *testing.T) {
	c := mustConditionals(t)
	for _, cell := range c.Cells {
		mid := (cell.TargetsMin + cell.TargetsMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		for _, line := range []float64{0, 0.5, 4.5, 9.5, 19.5, 124.5, 149.5, 400} {
			got, err := c.Lookup(cell.Scenario, cell.Occurred, mid, tr, line, 0.95)
			if err != nil {
				continue
			}
			if got.Prob <= 0 || got.Prob >= 1 {
				t.Errorf("%s n=%d line %.1f: P = %v claims certainty a finite sample cannot",
					cell.Scenario, cell.N, line, got.Prob)
			}
		}
	}

	// The clamp is half an observation, so it must scale with sample size.
	if clampToSupport(1.0, 100) >= clampToSupport(1.0, 1000) {
		t.Error("a larger sample should permit a probability closer to 1")
	}
	if got := clampToSupport(0.5, 100); got != 0.5 {
		t.Errorf("mid-range probabilities must be untouched, got %v", got)
	}
}

// TestUnvalidatedScenariosCannotBePriced is the gate.
//
// blowout_loss is fitted and its cells ship, but it failed validation three
// ways: the direction inverts at ordinary lines (q > r at 6.5/20.5/24.5 yards
// in the 6-8 target band), only 3 of 15 cells are bootstrap-resolved, and it
// holds in 10 of 13 cells out of sample against shootout's 14 of 14. Measurable
// is not the same as usable.
func TestUnvalidatedScenariosCannotBePriced(t *testing.T) {
	c := mustConditionals(t)

	if _, _, err := c.QR("blowout_loss", 7, 0.0, 45, 0.95); err == nil {
		t.Error("blowout_loss failed validation and must not be priceable")
	}
	if _, err := c.Lookup("blowout_loss", true, 7, 0.0, 45, 0.95); err == nil {
		t.Error("Lookup must refuse an unvalidated scenario too, not just QR")
	}
	// The refusal has to explain itself; a bare error would leave the operator
	// guessing whether it is a typo or a finding.
	_, err := c.Lookup("blowout_loss", true, 7, 0.0, 45, 0.95)
	if err != nil && !strings.Contains(err.Error(), "NOT validated") {
		t.Errorf("refusal should say why: %v", err)
	}

	// An unknown scenario is refused rather than assumed good.
	if _, err := c.Lookup("shootoot", true, 7, 0.0, 45, 0.95); err == nil {
		t.Error("a misspelled scenario must not be assumed valid")
	}

	// The validated one still works, or the gate has eaten everything.
	if _, _, err := c.QR("shootout", 7, 0.0, 45, 0.95); err != nil {
		t.Errorf("shootout passed validation and must remain priceable: %v", err)
	}
	if got := c.ValidatedScenarioNames(); len(got) != 1 || got[0] != "shootout" {
		t.Errorf("validated scenarios = %v, want [shootout]", got)
	}
	// Cells are still present -- gating is about pricing, not deletion.
	blowout := 0
	for _, cell := range c.Cells {
		if cell.Scenario == "blowout_loss" {
			blowout++
		}
	}
	if blowout == 0 {
		t.Error("unvalidated cells should still ship for future validation work")
	}
}

// TestClampSurvivesRounding guards the interaction that made the previous fix
// cosmetic.
//
// clampToSupport bounds p to 1-1/(2n), but `int(p*n + 0.5)` then computes
// exactly n and hands the half observation straight back, putting Wilson's
// upper bound at 1.0000 again. That shipped once, described as fixed, because
// the test checked the probability and never the interval.
func TestClampSurvivesRounding(t *testing.T) {
	for _, n := range []int{2, 50, 99, 197, 304, 1028, 5766} {
		if got := clampHits(clampToSupport(1.0, n), n); got >= n {
			t.Errorf("n=%d: hits=%d reached n, undoing the clamp", n, got)
		}
		if got := clampHits(clampToSupport(0.0, n), n); got < 1 {
			t.Errorf("n=%d: hits=%d fell to zero, undoing the clamp", n, got)
		}
	}
	// Mid-range counts must be untouched.
	if got := clampHits(0.5, 100); got != 50 {
		t.Errorf("mid-range hits = %d, want 50", got)
	}

	// The property that matters, through the public API: no interval may claim
	// certainty at either end.
	c := mustConditionals(t)
	for _, cell := range c.Cells {
		if st, ok := c.ScenarioStatus[cell.Scenario]; !ok || !st.Validated {
			continue
		}
		mid := (cell.TargetsMin + cell.TargetsMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		for _, line := range []float64{0, 0.5, 4.5, 200, 400} {
			got, err := c.Lookup(cell.Scenario, cell.Occurred, mid, tr, line, 0.95)
			if err != nil {
				continue
			}
			if got.Upper >= 1.0 {
				t.Errorf("%s n_eff=%d line %.1f: upper bound %.4f claims certainty",
					cell.Scenario, got.NEff, line, got.Upper)
			}
			if got.Lower <= 0.0 {
				t.Errorf("%s n_eff=%d line %.1f: lower bound %.4f claims impossibility",
					cell.Scenario, got.NEff, line, got.Lower)
			}
		}
	}
}

// TestEffectiveNActuallyNarrows is the behavioural guard the previous version
// lacked. That one validated JSON fields, so the whole n_eff plumbing could be
// reverted to raw N with the suite still green.
//
// This compares the interval Lookup actually produces against the one raw N
// would give, and fails if they match — which is only possible if Lookup is
// ignoring the discount.
func TestEffectiveNActuallyNarrows(t *testing.T) {
	c := mustConditionals(t)
	widened := 0
	for _, cell := range c.Cells {
		st, ok := c.ScenarioStatus[cell.Scenario]
		if !ok || !st.Validated || cell.NEff >= float64(cell.N) {
			continue
		}
		mid := (cell.TargetsMin + cell.TargetsMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		got, err := c.Lookup(cell.Scenario, cell.Occurred, mid, tr, cell.Median, 0.95)
		if err != nil {
			continue
		}
		if got.NEff >= cell.N {
			t.Errorf("%s: Lookup used n=%d despite n_eff=%.1f", cell.Scenario, got.NEff, cell.NEff)
			continue
		}
		// The same estimate on raw N must give a strictly tighter interval.
		rawLo, rawHi, err := wager.WilsonInterval(clampHits(got.Prob, cell.N), cell.N, 0.95)
		if err != nil {
			continue
		}
		if (rawHi - rawLo) >= (got.Upper - got.Lower) {
			t.Errorf("%s: raw-N interval %.5f is not tighter than the n_eff one %.5f — "+
				"the clustering discount is not reaching the output",
				cell.Scenario, rawHi-rawLo, got.Upper-got.Lower)
		}
		widened++
	}
	if widened < 5 {
		t.Errorf("only %d cells exercised the discount; the guard is too weak", widened)
	}
}

// TestIntervalsUseEffectiveN pins the clustering correction. Cells pool repeat
// players, so the raw count overstates precision; intervals are built on the
// measured effective sample size instead.
func TestIntervalsUseEffectiveN(t *testing.T) {
	c := mustConditionals(t)
	discounted := 0
	for _, cell := range c.Cells {
		if cell.NEff <= 0 {
			t.Errorf("%s cell has no n_eff; refit with fit_conditionals.py", cell.Scenario)
			continue
		}
		if cell.NEff > float64(cell.N)+0.5 {
			t.Errorf("%s: n_eff %.1f exceeds n %d", cell.Scenario, cell.NEff, cell.N)
		}
		if cell.Players <= 0 || cell.Players > cell.N {
			t.Errorf("%s: %d distinct players in a cell of %d", cell.Scenario, cell.Players, cell.N)
		}
		if cell.NEff < float64(cell.N) {
			discounted++
		}

		got, err := c.Lookup(cell.Scenario, cell.Occurred,
			(cell.TargetsMin+cell.TargetsMax)/2, (cell.TrendMin+cell.TrendMax)/2,
			cell.Median, 0.95)
		if err != nil {
			continue
		}
		if got.NEff > got.N {
			t.Errorf("reported n_eff %d exceeds n %d", got.NEff, got.N)
		}
	}
	if discounted == 0 {
		t.Error("no cell was discounted for clustering; the correction is not in effect")
	}
	t.Logf("%d of %d cells discounted for repeat players", discounted, len(c.Cells))
}

// TestQAndRDiffer guards the input the decomposition rejects: if a scenario
// leaves the outcome unchanged, RequiredScenarioProb divides by zero.
func TestQAndRDiffer(t *testing.T) {
	c := mustConditionals(t)
	for _, name := range c.ScenarioNames() {
		q, r, err := c.QR(name, 7, 0.0, 45, 0.95)
		if err != nil {
			continue // that combination may be unpublished; covered elsewhere
		}
		if q.Prob == r.Prob {
			t.Errorf("%s: q and r are identical, which carries no information", name)
		}
	}
}

// TestIntervalsWidenOnThinCells: uncertainty must track sample size.
func TestIntervalsWidenOnThinCells(t *testing.T) {
	c := mustConditionals(t)
	// Only validated scenarios can be priced, so only they can be compared.
	var thin, thick Cell
	for _, cell := range c.Cells {
		if st, ok := c.ScenarioStatus[cell.Scenario]; !ok || !st.Validated {
			continue
		}
		if thin.N == 0 || cell.N < thin.N {
			thin = cell
		}
		if cell.N > thick.N {
			thick = cell
		}
	}
	if thin.N == 0 || thick.N == 0 {
		t.Fatal("no validated cells to compare")
	}
	widthOf := func(cell Cell) float64 {
		got, err := c.Lookup(cell.Scenario, cell.Occurred,
			(cell.TargetsMin+cell.TargetsMax)/2, (cell.TrendMin+cell.TrendMax)/2,
			cell.Median, 0.95)
		if err != nil {
			t.Fatal(err)
		}
		return got.Upper - got.Lower
	}
	if widthOf(thin) <= widthOf(thick) {
		t.Errorf("thin cell (n=%d) interval is not wider than thick (n=%d)", thin.N, thick.N)
	}
}

// TestMissingCellFailsLoudly is the contract: an unfitted combination is an
// error, never a quietly substituted neighbour or a zero.
func TestMissingCellFailsLoudly(t *testing.T) {
	c := mustConditionals(t)
	if _, err := c.Lookup("shootout", true, 500, 0, 45, 0.95); err == nil {
		t.Error("500 projected targets should have no cell")
	}
	if _, err := c.Lookup("no-such-scenario", true, 7, 0, 45, 0.95); err == nil {
		t.Error("an unknown scenario must be rejected")
	}
	if _, _, err := c.QR("shootout", -5, 0, 45, 0.95); err == nil {
		t.Error("negative targets should have no cell")
	}
}

func TestMalformedConditionalsRejected(t *testing.T) {
	for name, body := range map[string]string{
		"no cells":       `{"cells":[]}`,
		"thin quantiles": `{"cells":[{"n":100,"quantiles":[[0,1]]}]}`,
		"zero n":         `{"cells":[{"n":0,"quantiles":[[0,1],[1,2]]}]}`,
		"bad arity":      `{"cells":[{"n":100,"quantiles":[[0,1,2],[1,2]]}]}`,
	} {
		var c Conditionals
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("%s: fixture is bad json: %v", name, err)
		}
		// Mirror the validation LoadConditionals performs on the embedded blob.
		bad := len(c.Cells) == 0
		for _, cell := range c.Cells {
			if len(cell.Quantiles) < 2 || cell.N <= 0 {
				bad = true
			}
			for _, q := range cell.Quantiles {
				if len(q) != 2 {
					bad = true
				}
			}
		}
		if !bad {
			t.Errorf("%s: should have been rejected", name)
		}
	}
}

// TestExtrapolatedLineIsRefused pins the case that produced a confident verdict
// from nothing.
//
// A 250-yard line is past anything any cell ever produced, so probAbove
// returned 0 and clampToSupport turned it into a small positive probability.
// The wager layer then divided by (q - r) -- two clamped endpoints -- and
// printed BEYOND-YOUR-READ "short by 690.4 pts" with a sensitivity of -2899
// points per point of q. Every figure was arithmetic on the absence of data.
func TestExtrapolatedLineIsRefused(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	// 300 is past both sides. 250 is deliberately NOT used here: it sits
	// inside the scenario cell (which reached 266 yards once) and outside the
	// baseline cell (196), so it exercises QR but not Lookup.
	if _, err := c.Lookup("shootout", true, 7, 0.0, 300, 0.95); err == nil {
		t.Fatal("priced a line beyond anything the cell ever observed")
	}
	// QR must refuse when EITHER side is out of range -- which is the real
	// shape of the bug, since s* needs both.
	if _, _, err := c.QR("shootout", 7, 0.0, 250, 0.95); err == nil {
		t.Error("QR priced a line the baseline cell never reached")
	}
	// A line below everything observed is the same failure, mirrored.
	if _, err := c.Lookup("shootout", true, 7, 0.0, -500, 0.95); err == nil {
		t.Error("priced a line below anything the cell ever observed")
	}
	// This is NOT a scenario problem, and must not be reported as one.
	_, err = c.Lookup("shootout", true, 7, 0.0, 300, 0.95)
	if errors.Is(err, ErrScenarioNotPriceable) {
		t.Error("an out-of-range line was classified as an unpriceable scenario")
	}
}

// TestTailNMeasuresTheSparserSide pins what the interval cannot say.
//
// At a deep line the probability is small, so Wilson is narrow in absolute
// terms no matter how little evidence there is. Two estimates that print
// almost identically can rest on an order of magnitude different support, and
// the thin one has the TIGHTER interval.
func TestTailNMeasuresTheSparserSide(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	const line = 100.5

	thin, err := c.Lookup("shootout", true, 3, 0.07, line, 0.95) // 0-4 targets
	if err != nil {
		t.Fatal(err)
	}
	solid, err := c.Lookup("shootout", true, 9, 0.0, line, 0.95) // 8-11 targets
	if err != nil {
		t.Fatal(err)
	}

	if !thin.Thin() {
		t.Errorf("0-4 targets at %.1f: TailN %.1f, expected below the %d threshold",
			line, thin.TailN, MinTailN)
	}
	if solid.Thin() {
		t.Errorf("8-11 targets at %.1f: TailN %.1f, expected at or above %d",
			line, solid.TailN, MinTailN)
	}

	// The trap, stated as an assertion: the thin estimate's interval is the
	// narrower of the two. Anything keying off width alone gets this backwards.
	thinWidth, solidWidth := thin.Upper-thin.Lower, solid.Upper-solid.Lower
	if thinWidth >= solidWidth {
		t.Fatalf("expected the thin estimate to print the tighter interval "+
			"(thin %.4f vs solid %.4f); if this ever stops holding, the THIN "+
			"label may be redundant", thinWidth, solidWidth)
	}
}

// TestTailNIsSymmetric checks that an estimate near 1 is treated as thinly
// evidenced as one near 0. The sparse side is what the wager rests on, and
// which side that is depends only on which way the line falls.
func TestTailNIsSymmetric(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ line float64 }{{2.5}, {150.5}} {
		got, err := c.Lookup("shootout", true, 9, 0.0, tc.line, 0.95)
		if err != nil {
			t.Fatal(err)
		}
		want := math.Min(got.Prob, 1-got.Prob) * float64(got.NEff)
		if math.Abs(got.TailN-want) > 1e-9 {
			t.Errorf("line %.1f: TailN = %.4f, want min(p,1-p)*nEff = %.4f",
				tc.line, got.TailN, want)
		}
		if got.TailN > float64(got.NEff)/2 {
			t.Errorf("line %.1f: TailN %.1f exceeds half of nEff %d, so it is not "+
				"reporting the sparser side", tc.line, got.TailN, got.NEff)
		}
	}
}

// TestDefinitionMismatchIsRefused pins the case where s and the grid's q/r
// described different events.
//
// q and r are fitted once against a fixed condition; s is derived per query
// from whatever -threshold the caller passed. Nothing connected them, so
// `-name shootout -threshold 65` blended s = P(total > 65) against a q measured
// on total > 50. The result was well-formed, confident, and a probability of
// nothing -- printed under a header that stated the contradiction outright.
func TestDefinitionMismatchIsRefused(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}

	def, ok := c.Definitions["shootout"]
	if !ok {
		t.Fatal("the grid records no definition for shootout")
	}
	if def.Basis != "total" || def.Threshold != 50 {
		t.Fatalf("shootout is fitted as %s; the tests below assume total > 50", def)
	}

	if err := c.CheckDefinition("shootout", "total", 50); err != nil {
		t.Errorf("the definition the grid was fitted on was rejected: %v", err)
	}
	for _, bad := range []float64{65, 49.5, 0, -7} {
		err := c.CheckDefinition("shootout", "total", bad)
		if err == nil {
			t.Errorf("threshold %.1f was accepted against a grid fitted at %.1f",
				bad, def.Threshold)
			continue
		}
		if !errors.Is(err, ErrDefinitionMismatch) {
			t.Errorf("threshold %.1f: got %v, want ErrDefinitionMismatch", bad, err)
		}
	}

	// Right threshold, wrong quantity: -7 margin is blowout_loss's definition,
	// not a shootout, and the number alone does not make it one.
	if err := c.CheckDefinition("blowout_loss", "total", -7); !errors.Is(err, ErrDefinitionMismatch) {
		t.Errorf("blowout_loss accepted on the total; it is fitted on the margin: %v", err)
	}
	if err := c.CheckDefinition("blowout_loss", "margin", -7); err != nil {
		t.Errorf("blowout_loss rejected on its own definition: %v", err)
	}
}

// TestMissingDefinitionsFailClosed guards the upgrade path. An artifact fitted
// before definitions were recorded cannot be checked, and the failure it would
// otherwise permit is silent -- so it must refuse rather than wave the query
// through on the grounds that nothing contradicted it.
func TestMissingDefinitionsFailClosed(t *testing.T) {
	old := &Conditionals{ScenarioStatus: map[string]ScenarioStatus{
		"shootout": {Validated: true},
	}}
	err := old.CheckDefinition("shootout", "total", 50)
	if err == nil {
		t.Fatal("an artifact with no recorded definitions accepted a query")
	}
	if !errors.Is(err, ErrDefinitionMismatch) {
		t.Errorf("got %v, want ErrDefinitionMismatch", err)
	}
}

// TestEveryFittedScenarioHasADefinition stops a scenario being added to the fit
// without saying what it means -- which is how the gap arose in the first place.
func TestEveryFittedScenarioHasADefinition(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range c.ScenarioNames() {
		def, ok := c.Definitions[name]
		if !ok {
			t.Errorf("%q has cells but no recorded definition", name)
			continue
		}
		if def.Basis != "total" && def.Basis != "margin" {
			t.Errorf("%q: basis %q is neither total nor margin", name, def.Basis)
		}
		if def.Op != ">" && def.Op != "<" {
			t.Errorf("%q: op %q is neither > nor <", name, def.Op)
		}
	}
}

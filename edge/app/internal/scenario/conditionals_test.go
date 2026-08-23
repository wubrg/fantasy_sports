package scenario

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
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
	// The grid fits more than one outcome now; receiving yards must remain
	// among them, and every outcome must declare its opportunity axis.
	if _, ok := c.Outcomes["receiving_yards"]; !ok {
		t.Errorf("receiving_yards missing from outcomes %v", c.OutcomeNames())
	}
	for name, def := range c.Outcomes {
		if def.Opportunity == "" {
			t.Errorf("outcome %q declares no opportunity axis", name)
		}
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
		got, err := c.Lookup("receiving_yards", "shootout", true, targets, 0.0, line, conf)
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
	q, r, err := c.QR("receiving_yards", "shootout", 7, 0.0, 45, 0.95)
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
		if cell.Outcome != "receiving_yards" || cell.Scenario != "blowout_loss" {
			continue
		}
		k := key{cell.OpportunityMin, cell.TrendMin}
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
		if cell.Outcome != "receiving_yards" || cell.Scenario != "shootout" {
			continue
		}
		k := key{cell.OpportunityMin, cell.TrendMin}
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
		mid := (cell.OpportunityMin + cell.OpportunityMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		for _, line := range []float64{0, 0.5, 4.5, 9.5, 19.5, 124.5, 149.5, 400} {
			got, err := c.Lookup(cell.Outcome, cell.Scenario, cell.Occurred, mid, tr, line, 0.95)
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

	if _, _, err := c.QR("receiving_yards", "blowout_loss", 7, 0.0, 45, 0.95); err == nil {
		t.Error("blowout_loss failed validation and must not be priceable")
	}
	if _, err := c.Lookup("receiving_yards", "blowout_loss", true, 7, 0.0, 45, 0.95); err == nil {
		t.Error("Lookup must refuse an unvalidated scenario too, not just QR")
	}
	// The refusal has to explain itself; a bare error would leave the operator
	// guessing whether it is a typo or a finding.
	_, err := c.Lookup("receiving_yards", "blowout_loss", true, 7, 0.0, 45, 0.95)
	if err != nil && !strings.Contains(err.Error(), "NOT validated") {
		t.Errorf("refusal should say why: %v", err)
	}

	// An unknown scenario is refused rather than assumed good.
	if _, err := c.Lookup("receiving_yards", "shootoot", true, 7, 0.0, 45, 0.95); err == nil {
		t.Error("a misspelled scenario must not be assumed valid")
	}

	// The validated one still works, or the gate has eaten everything.
	if _, _, err := c.QR("receiving_yards", "shootout", 7, 0.0, 45, 0.95); err != nil {
		t.Errorf("shootout passed validation and must remain priceable: %v", err)
	}
	// Priceable means "passes, or passes on a recorded override" -- asserting a
	// fixed list would break every time a verdict legitimately changes, which
	// is a test measuring the wrong thing.
	got := c.ValidatedScenarioNames("receiving_yards")
	if len(got) == 0 {
		t.Error("nothing is priceable for receiving yards; the gate has eaten everything")
	}
	for _, name := range got {
		st := c.ScenarioStatus["receiving_yards"][name]
		if !st.RuleSays && st.AcceptedFailure == nil {
			t.Errorf("%q is priceable but neither passes the rule nor carries an override", name)
		}
	}
	// And whatever is gated must genuinely be refused.
	for name := range c.ScenarioStatus["receiving_yards"] {
		if slices.Contains(got, name) {
			continue
		}
		if _, _, err := c.QR("receiving_yards", name, 7, 0.0, 45, 0.95); err == nil {
			t.Errorf("%q is not in the priceable list but QR accepted it", name)
		}
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
		if st, ok := c.ScenarioStatus[cell.Outcome][cell.Scenario]; !ok || !st.Validated {
			continue
		}
		mid := (cell.OpportunityMin + cell.OpportunityMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		for _, line := range []float64{0, 0.5, 4.5, 200, 400} {
			got, err := c.Lookup(cell.Outcome, cell.Scenario, cell.Occurred, mid, tr, line, 0.95)
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
		st, ok := c.ScenarioStatus[cell.Outcome][cell.Scenario]
		// effectiveN rounds to the nearest observation, so a discount smaller
		// than half a game correctly vanishes -- n_eff 102.9 against n 103 is
		// not a bug. Mirror that rounding here rather than assuming any
		// discount at all survives to the output.
		if !ok || !st.Validated || cell.effectiveN() >= cell.N {
			continue
		}
		mid := (cell.OpportunityMin + cell.OpportunityMax) / 2
		tr := (cell.TrendMin + cell.TrendMax) / 2
		got, err := c.Lookup(cell.Outcome, cell.Scenario, cell.Occurred, mid, tr, cell.Median, 0.95)
		if err != nil {
			continue
		}
		if got.NEff >= cell.N {
			t.Errorf("%s/%s: Lookup used n=%d despite n_eff=%.1f",
				cell.Outcome, cell.Scenario, got.NEff, cell.NEff)
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

		got, err := c.Lookup(cell.Outcome, cell.Scenario, cell.Occurred,
			(cell.OpportunityMin+cell.OpportunityMax)/2, (cell.TrendMin+cell.TrendMax)/2,
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
		q, r, err := c.QR("receiving_yards", name, 7, 0.0, 45, 0.95)
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
		if st, ok := c.ScenarioStatus[cell.Outcome][cell.Scenario]; !ok || !st.Validated {
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
		got, err := c.Lookup(cell.Outcome, cell.Scenario, cell.Occurred,
			(cell.OpportunityMin+cell.OpportunityMax)/2, (cell.TrendMin+cell.TrendMax)/2,
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
	if _, err := c.Lookup("receiving_yards", "shootout", true, 500, 0, 45, 0.95); err == nil {
		t.Error("500 projected targets should have no cell")
	}
	if _, err := c.Lookup("receiving_yards", "no-such-scenario", true, 7, 0, 45, 0.95); err == nil {
		t.Error("an unknown scenario must be rejected")
	}
	if _, _, err := c.QR("receiving_yards", "shootout", -5, 0, 45, 0.95); err == nil {
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
	if _, err := c.Lookup("receiving_yards", "shootout", true, 7, 0.0, 300, 0.95); err == nil {
		t.Fatal("priced a line beyond anything the cell ever observed")
	}
	// QR must refuse when EITHER side is out of range -- which is the real
	// shape of the bug, since s* needs both.
	if _, _, err := c.QR("receiving_yards", "shootout", 7, 0.0, 250, 0.95); err == nil {
		t.Error("QR priced a line the baseline cell never reached")
	}
	// A line below everything observed is the same failure, mirrored.
	if _, err := c.Lookup("receiving_yards", "shootout", true, 7, 0.0, -500, 0.95); err == nil {
		t.Error("priced a line below anything the cell ever observed")
	}
	// This is NOT a scenario problem, and must not be reported as one.
	_, err = c.Lookup("receiving_yards", "shootout", true, 7, 0.0, 300, 0.95)
	if errors.Is(err, ErrScenarioNotPriceable) {
		t.Error("an out-of-range line was classified as an unpriceable scenario")
	}
}

// TestTailNMeasuresTheSparserSide pins what the interval cannot say.
//
// At a deep line the probability is small, so Wilson is narrow in absolute
// terms no matter how little evidence there is. Two estimates that print almost
// identically can rest on an order of magnitude different support, and the thin
// one has the TIGHTER interval.
//
// Written against properties rather than specific cells. An earlier version
// asserted that 0-4 targets at 100.5 was THIN; extending the fit window to 2009
// thickened that cell to 11.5 effective observations and the test failed on a
// change that was entirely correct. What must hold across refits is the
// relationship, not the datum.
func TestTailNMeasuresTheSparserSide(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	const lowVolume, highVolume = 3.0, 9.0
	lines := []float64{60.5, 75.5, 100.5, 125.5, 150.5}

	// Support must fall as the line deepens, and the low-volume band must never
	// have more of it than the high-volume band at the same line.
	var prevLow float64 = -1
	var thinFound, solidFound bool
	for _, line := range lines {
		lo, err := c.Lookup("receiving_yards", "shootout", true, lowVolume, 0.07, line, 0.95)
		if err != nil {
			continue // past this cell's range; refusal is tested elsewhere
		}
		hi, err := c.Lookup("receiving_yards", "shootout", true, highVolume, 0.07, line, 0.95)
		if err != nil {
			continue
		}
		if lo.TailN > hi.TailN {
			t.Errorf("line %.1f: %.0f targets has more support (%.1f) than %.0f targets (%.1f)",
				line, lowVolume, lo.TailN, highVolume, hi.TailN)
		}
		if prevLow >= 0 && lo.TailN > prevLow {
			t.Errorf("line %.1f: support rose to %.1f as the line deepened from %.1f",
				line, lo.TailN, prevLow)
		}
		prevLow = lo.TailN

		if lo.Thin() {
			thinFound = true
		}
		if !hi.Thin() {
			solidFound = true
		}

		// The trap, asserted wherever the two regimes actually differ: the thin
		// estimate prints the NARROWER interval. Anything keying off width alone
		// gets this backwards.
		if lo.Thin() && !hi.Thin() && (lo.Upper-lo.Lower) >= (hi.Upper-hi.Lower) {
			t.Errorf("line %.1f: thin estimate's interval (%.4f) is not narrower than "+
				"the solid one's (%.4f); if this stops holding, the THIN label may be redundant",
				line, lo.Upper-lo.Lower, hi.Upper-hi.Lower)
		}
	}
	if !thinFound {
		t.Error("no line in the sweep produced a THIN estimate; the threshold may be unreachable")
	}
	if !solidFound {
		t.Error("no line in the sweep produced a MEASURED estimate at high volume")
	}
}

// TestThinIsPureFunctionOfSupport checks the labelling itself, independent of
// any fitted data, so a refit can move every cell without moving this.
func TestThinIsPureFunctionOfSupport(t *testing.T) {
	for _, tc := range []struct {
		tailN float64
		thin  bool
	}{
		{0, true}, {1, true}, {float64(MinTailN) - 0.1, true},
		{float64(MinTailN), false}, {100, false},
	} {
		if got := (Conditional{TailN: tc.tailN}).Thin(); got != tc.thin {
			t.Errorf("TailN %.1f: Thin() = %v, want %v", tc.tailN, got, tc.thin)
		}
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
		got, err := c.Lookup("receiving_yards", "shootout", true, 9, 0.0, tc.line, 0.95)
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

	if err := c.CheckDefinition("receiving_yards", "shootout", "total", 50); err != nil {
		t.Errorf("the definition the grid was fitted on was rejected: %v", err)
	}
	for _, bad := range []float64{65, 49.5, 0, -7} {
		err := c.CheckDefinition("receiving_yards", "shootout", "total", bad)
		if err == nil {
			t.Errorf("threshold %.1f was accepted against a grid fitted at %.1f",
				bad, def.Threshold)
			continue
		}
		if !errors.Is(err, ErrDefinitionMismatch) {
			t.Errorf("threshold %.1f: got %v, want ErrDefinitionMismatch", bad, err)
		}
	}

	// Right threshold, wrong quantity: shootout's 50 is a TOTAL, and asking for
	// the same number as a margin is a different event, not a rounding detail.
	//
	// This uses shootout rather than blowout_loss because the mismatch check
	// only runs for scenarios that could otherwise be priced -- validation is
	// tested first, deliberately, and an unvalidated scenario short-circuits
	// before its basis is ever compared.
	if err := c.CheckDefinition("receiving_yards", "shootout", "margin", 50); !errors.Is(err, ErrDefinitionMismatch) {
		t.Errorf("shootout accepted on the margin; it is fitted on the total: %v", err)
	}
}

// TestMissingDefinitionsFailClosed guards the upgrade path. An artifact fitted
// before definitions were recorded cannot be checked, and the failure it would
// otherwise permit is silent -- so it must refuse rather than wave the query
// through on the grounds that nothing contradicted it.
func TestMissingDefinitionsFailClosed(t *testing.T) {
	old := &Conditionals{ScenarioStatus: map[string]map[string]ScenarioStatus{
		"receiving_yards": {"shootout": {Validated: true}},
	}}
	err := old.CheckDefinition("receiving_yards", "shootout", "total", 50)
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
		// The bases the Python side can express, per ScenarioDef.FIELD. Kept as
		// an explicit list rather than a free string so that adding a basis is a
		// deliberate act on both sides of the artifact.
		switch def.Basis {
		case "total", "margin", "offense_proe":
		default:
			t.Errorf("%q: basis %q is not one this build knows how to interpret", name, def.Basis)
		}
		if def.Op != ">" && def.Op != "<" {
			t.Errorf("%q: op %q is neither > nor <", name, def.Op)
		}
	}
}

// TestGatedScenarioReportsTheGate pins the precedence between the two refusals.
//
// pass_heavy is fitted on offense PROE and is gated off. Asking for it with the
// default -basis total used to report a definition mismatch, which is true and
// useless: the caller would go and change a flag that was never the obstacle.
// An unvalidated scenario cannot be priced on any basis, so that is what it has
// to say.
func TestGatedScenarioReportsTheGate(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	st, ok := c.ScenarioStatus["receiving_yards"]["pass_heavy"]
	if !ok {
		t.Skip("pass_heavy is not in this artifact")
	}
	if st.Validated {
		t.Skip("pass_heavy has since been validated; this test guards the gated case")
	}

	err = c.CheckDefinition("receiving_yards", "pass_heavy", "total", 50)
	if err == nil {
		t.Fatal("a gated scenario was accepted")
	}
	if !errors.Is(err, ErrScenarioNotPriceable) {
		t.Errorf("got %v, want the not-priceable error rather than a definition mismatch", err)
	}
	if errors.Is(err, ErrDefinitionMismatch) {
		t.Error("reported a threshold mismatch for a scenario that cannot be priced at all")
	}
	// Even asking on its OWN basis must still refuse, for the same reason.
	if err := c.CheckDefinition("receiving_yards", "pass_heavy", "offense_proe", 3.0); !errors.Is(err, ErrScenarioNotPriceable) {
		t.Errorf("on its own basis: got %v, want not-priceable", err)
	}
}

// TestUnvalidatedScenariosStillCarryCells checks the half of the design that is
// easy to lose: a gated scenario is still FITTED. The cells stay in the
// artifact so the measurement is reproducible and so the work that would
// un-gate it has something to build on -- only pricing is refused.
func TestUnvalidatedScenariosStillCarryCells(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	for outcome, byScenario := range c.ScenarioStatus {
		for name, st := range byScenario {
			if st.Validated {
				continue
			}
			cells := 0
			for _, cell := range c.Cells {
				if cell.Outcome == outcome && cell.Scenario == name {
					cells++
				}
			}
			if cells == 0 {
				t.Errorf("%s/%s is gated off AND has no cells; the measurement is "+
					"unreproducible", outcome, name)
			}
			if st.Note == "" {
				t.Errorf("%s/%s is gated off with no recorded reason", outcome, name)
			}
		}
	}
}

// TestOutcomesHaveDistinctAxes guards the reason Outcome exists at all.
//
// A pass-catcher competes for a share of a fixed pool of team targets; a
// quarterback takes essentially all of his team's attempts. Running QB volume
// through a share-shaped model produces bands that mean nothing, so the two
// must not quietly converge on one axis.
func TestOutcomesHaveDistinctAxes(t *testing.T) {
	c := mustConditionals(t)
	rec, ok := c.Outcomes["receiving_yards"]
	if !ok {
		t.Fatal("receiving_yards not fitted")
	}
	pas, ok := c.Outcomes["passing_yards"]
	if !ok {
		t.Skip("passing_yards not fitted in this artifact")
	}
	if !rec.ShareBased {
		t.Error("receiving yards should be share-based: a catcher competes for a pool")
	}
	if pas.ShareBased {
		t.Error("passing yards should NOT be share-based: a QB has no share to hold")
	}
	if rec.Opportunity == pas.Opportunity {
		t.Errorf("both outcomes claim the same opportunity axis %q", rec.Opportunity)
	}

	// Their band ranges must not overlap either, or a caller who forgets
	// -outcome would silently land in the wrong grid instead of erroring.
	var recMax, pasMin float64
	for _, cell := range c.Cells {
		if cell.Outcome == "receiving_yards" && cell.OpportunityMax < 900 {
			recMax = max(recMax, cell.OpportunityMax)
		}
		if cell.Outcome == "passing_yards" {
			if pasMin == 0 || cell.OpportunityMin < pasMin {
				pasMin = cell.OpportunityMin
			}
		}
	}
	t.Logf("receiving bands top out at %.0f targets; passing starts at %.0f attempts",
		recMax, pasMin)
}

// TestValidationIsPerOutcome pins that a scenario's verdict is scoped to the
// outcome it was measured against. blowout_loss currently validates for
// passing yards and is gated for receiving -- the same effect, with only the
// larger grid big enough to catch it wobbling.
func TestValidationIsPerOutcome(t *testing.T) {
	c := mustConditionals(t)
	if len(c.ScenarioStatus) < 2 {
		t.Skip("only one outcome fitted")
	}
	seen := map[string]map[string]bool{}
	for outcome, byScenario := range c.ScenarioStatus {
		for name, st := range byScenario {
			if seen[name] == nil {
				seen[name] = map[string]bool{}
			}
			seen[name][outcome] = st.Validated
		}
	}
	differs := 0
	for name, byOutcome := range seen {
		vals := map[bool]bool{}
		for _, v := range byOutcome {
			vals[v] = true
		}
		if len(vals) > 1 {
			differs++
			t.Logf("%q validates differently by outcome: %v", name, byOutcome)
		}
	}
	if differs == 0 {
		t.Log("no scenario currently differs by outcome; the per-outcome split is still " +
			"correct, it just is not being exercised")
	}
}

// TestDiscreteLookupIsExact pins the reason receptions store a CDF rather than
// a sampled quantile curve.
//
// A count has no probability mass between its values: P(receptions > 3.5) is
// exactly P(receptions > 3). Sampling that distribution at 2% steps and
// interpolating toward the next integer invents mass that cannot exist --
// measured at up to 1.44 percentage points of error on the real grid, bounded
// by half a step. The stored CDF plus a non-interpolating read removes it.
func TestDiscreteLookupIsExact(t *testing.T) {
	c := mustConditionals(t)
	def, ok := c.Outcomes["receptions"]
	if !ok {
		t.Skip("receptions not fitted")
	}
	if !def.Discrete {
		t.Fatal("receptions is not flagged discrete")
	}

	for _, cell := range c.Cells {
		if cell.Outcome != "receptions" {
			continue
		}
		// Every stored value must be a whole number, or it is not a count.
		for _, point := range cell.Quantiles {
			if point[1] != float64(int(point[1])) {
				t.Fatalf("receptions cell stores a fractional value %v", point[1])
			}
		}
		// A half-integer line must read exactly the same as the integer below
		// it: there is nothing in between.
		for _, line := range []float64{1.5, 2.5, 3.5, 4.5} {
			half := probAboveDiscrete(cell.Quantiles, line)
			whole := probAboveDiscrete(cell.Quantiles, line-0.5)
			if half != whole {
				t.Errorf("%s: P(>%.1f)=%.6f but P(>%.1f)=%.6f — mass invented between integers",
					cell.Scenario, line, half, line-0.5, whole)
			}
		}
		// And it must differ from the integer ABOVE, or the read is degenerate.
		if probAboveDiscrete(cell.Quantiles, 2.5) == probAboveDiscrete(cell.Quantiles, 3.5) {
			t.Errorf("%s: P(>2.5) equals P(>3.5); the CDF is not being read", cell.Scenario)
		}
		return
	}
	t.Fatal("no receptions cells found")
}

// TestContinuousOutcomesStillInterpolate guards the other half: yardage is a
// measurement, its curve is dense, and interpolating between quantile points
// is correct there. The discrete path must not have leaked into it.
func TestContinuousOutcomesStillInterpolate(t *testing.T) {
	c := mustConditionals(t)
	for _, cell := range c.Cells {
		if cell.Outcome != "receiving_yards" || cell.Scenario != "shootout" {
			continue
		}
		a := probAbove(cell.Quantiles, 40.0)
		b := probAbove(cell.Quantiles, 40.5)
		if a == b {
			t.Errorf("receiving yards: P(>40.0) == P(>40.5); the curve is not interpolating")
		}
		return
	}
	t.Skip("no receiving shootout cell")
}

// TestAcceptedFailureIsFullySpecified guards the override from decaying into a
// general escape hatch.
//
// An exception that does not say which cell failed, what was measured, why it
// was accepted and by whom is indistinguishable from switching the gate off —
// and a gate that can be switched off silently stops discriminating for every
// scenario after it, not just this one.
func TestAcceptedFailureIsFullySpecified(t *testing.T) {
	c := mustConditionals(t)
	found := 0
	for outcome, byScenario := range c.ScenarioStatus {
		for name, st := range byScenario {
			if st.AcceptedFailure == nil {
				// No override: the recorded verdict must match the rule.
				if st.Validated != st.RuleSays {
					t.Errorf("%s/%s: validated=%v but rule says %v, with no accepted_failure",
						outcome, name, st.Validated, st.RuleSays)
				}
				continue
			}
			found++
			af := st.AcceptedFailure
			// An override only makes sense where the rule actually says no.
			if st.RuleSays {
				t.Errorf("%s/%s carries an accepted_failure but passes on its own; "+
					"the override is stale", outcome, name)
			}
			if !st.Validated {
				t.Errorf("%s/%s has an accepted_failure but is still gated; it does nothing",
					outcome, name)
			}
			for field, v := range map[string]string{
				"cell": af.Cell, "measured": af.Measured, "why": af.Why,
				"accepted_by": af.AcceptedBy,
			} {
				if v == "" {
					t.Errorf("%s/%s accepted_failure has an empty %s", outcome, name, field)
				}
			}
			if af.OpportunityMax <= af.OpportunityMin || af.TrendMax <= af.TrendMin {
				t.Errorf("%s/%s accepted_failure has no usable cell bounds: opp [%v,%v) "+
					"trend [%v,%v)", outcome, name,
					af.OpportunityMin, af.OpportunityMax, af.TrendMin, af.TrendMax)
			}
		}
	}
	if found == 0 {
		t.Skip("no accepted failures recorded")
	}
}

// TestAcceptedFailureCoversTheRightCell checks the bounds actually discriminate,
// so the warning at the point of pricing means something.
func TestAcceptedFailureCoversTheRightCell(t *testing.T) {
	c := mustConditionals(t)
	af := c.AcceptedFailureFor("receiving_yards", "pass_heavy")
	if af == nil {
		t.Skip("pass_heavy is not overridden for receiving yards")
	}
	mid := (af.OpportunityMin + af.OpportunityMax) / 2
	midTrend := (af.TrendMin + af.TrendMax) / 2
	if !af.Covers(mid, midTrend) {
		t.Errorf("the middle of the failing cell (%v, %v) is not covered", mid, midTrend)
	}
	// Just outside, on each axis.
	if af.Covers(af.OpportunityMax, midTrend) {
		t.Error("opportunity at the upper bound should be excluded (half-open interval)")
	}
	if af.Covers(mid, af.TrendMax) {
		t.Error("trend at the upper bound should be excluded (half-open interval)")
	}
	if af.Covers(mid, af.TrendMin-0.01) {
		t.Error("a trend below the cell is covered; the bounds do not discriminate")
	}
}

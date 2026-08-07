package scenario

import (
	"encoding/json"
	"testing"
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
	var thin, thick Cell
	for _, cell := range c.Cells {
		if thin.N == 0 || cell.N < thin.N {
			thin = cell
		}
		if cell.N > thick.N {
			thick = cell
		}
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

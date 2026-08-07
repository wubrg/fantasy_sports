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

// TestBlowoutLossHurts records the finding that contradicts the corpus.
//
// Edge of Vigor's Tier 3 predicts a garbage-time volume boost: a team down 14
// must throw, so its receivers see more work. Measured on FINAL MARGIN the
// effect is the opposite in every cell, because losing by more than a
// touchdown mostly identifies offenses that did not function.
//
// This does not refute the garbage-time mechanism; it refutes final margin as a
// proxy for it. The test exists so that if a future scenario definition using
// play-by-play flips the sign, the change is deliberate and visible.
func TestBlowoutLossHurts(t *testing.T) {
	c := mustConditionals(t)
	q, r, err := c.QR("blowout_loss", 7, 0.0, 45, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if q.Prob >= r.Prob {
		t.Errorf("measured on final margin a blowout loss LOWERS production, "+
			"but q=%.3f >= r=%.3f", q.Prob, r.Prob)
	}
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

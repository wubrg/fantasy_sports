package wager

import "testing"

// TestClassifyThreeWay covers the central distinction: whether a wager is
// carried by the market's own view, by your disagreement, or by neither.
func TestClassifyThreeWay(t *testing.T) {
	// +450, q=0.40, r=0.08 requires believing 31.8%.
	req, err := RequiredScenarioProb(450, 0.40, 0.08)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		market, yours float64
		want          EdgeSource
		note          string
	}{
		{0.40, 0.45, MarketAlone, "the game line already implies more than 31.8%"},
		{0.20, 0.45, DisagreementRequired, "market says 20%, you say 45%, need 31.8%"},
		{0.20, 0.25, BeyondYourRead, "even your 25% falls short of 31.8%"},
		{0.318182, 0.5, MarketAlone, "market exactly at the requirement"},
	}
	for _, c := range cases {
		got, err := Classify(req, c.market, c.yours)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != c.want {
			t.Errorf("market=%.2f yours=%.2f: got %v, want %v (%s)",
				c.market, c.yours, got.Source, c.want, c.note)
		}
	}
}

// TestClassifyRespectsDirection is the test that would catch an inverted
// verdict. When the scenario hurts the wager the requirement is a ceiling, and
// every comparison flips.
func TestClassifyRespectsDirection(t *testing.T) {
	// q < r: a shootout breaks this Under.
	req, err := RequiredScenarioProb(-110, 0.30, 0.70)
	if err != nil {
		t.Fatal(err)
	}
	if req.AtLeast {
		t.Fatal("setup wrong: this should be a ceiling")
	}

	// Market thinks a shootout is likely; you think it is not. Your
	// disagreement is what makes the Under work.
	c, err := Classify(req, 0.60, 0.20)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != DisagreementRequired {
		t.Errorf("got %v, want disagreement-required", c.Source)
	}
	if c.Disagreement <= 0 {
		t.Errorf("disagreement %.3f should be positive: believing a shootout is "+
			"LESS likely than the market helps this Under", c.Disagreement)
	}
	if c.Margin() <= 0 {
		t.Errorf("margin %.3f should be positive when the requirement is met", c.Margin())
	}

	// If the market already thinks a shootout is unlikely, the market carries it.
	c, err = Classify(req, 0.05, 0.20)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != MarketAlone {
		t.Errorf("got %v, want market-alone", c.Source)
	}
}

// TestDisagreementSignIsAlwaysHelpful pins the sign convention: positive means
// "in the direction that helps this wager", regardless of requirement direction.
func TestDisagreementSignIsAlwaysHelpful(t *testing.T) {
	floor, err := RequiredScenarioProb(450, 0.40, 0.08) // helps
	if err != nil {
		t.Fatal(err)
	}
	ceiling, err := RequiredScenarioProb(-110, 0.30, 0.70) // hurts
	if err != nil {
		t.Fatal(err)
	}

	// Believing the scenario is MORE likely than the market helps the floor...
	c, _ := Classify(floor, 0.20, 0.45)
	if c.Disagreement <= 0 {
		t.Error("believing more than the market should help an Over")
	}
	// ...and hurts the ceiling.
	c, _ = Classify(ceiling, 0.20, 0.45)
	if c.Disagreement >= 0 {
		t.Error("believing more than the market should hurt an Under")
	}
}

// TestAnalyzeLadder walks a realistic alternate-line ladder.
func TestAnalyzeLadder(t *testing.T) {
	rungs := []Rung{
		{Line: 52.5, Price: -110, Q: 0.72, R: 0.48},
		{Line: 75.5, Price: 145, Q: 0.55, R: 0.26},
		{Line: 100.5, Price: 450, Q: 0.40, R: 0.08},
		{Line: 125.5, Price: 900, Q: 0.22, R: 0.03},
	}
	steps, err := AnalyzeLadder(rungs, 0.25, 0.45)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != len(rungs) {
		t.Fatalf("got %d steps, want %d", len(steps), len(rungs))
	}
	for i, s := range steps {
		if s.Err != nil {
			t.Errorf("rung %d (%.1f): %v", i, s.Line, s.Err)
		}
	}

	idx, ok := BestRung(steps)
	if !ok {
		t.Fatal("no rung qualified")
	}
	best := steps[idx]
	// Every qualifying rung must have no more margin than the chosen one.
	for _, s := range steps {
		if s.Err != nil || s.Classification.Source == BeyondYourRead {
			continue
		}
		if s.Classification.Margin() > best.Classification.Margin()+1e-12 {
			t.Errorf("rung %.1f has margin %.4f, more than the chosen %.4f",
				s.Line, s.Classification.Margin(), best.Classification.Margin())
		}
	}
}

// TestLadderSurvivesOneBadRung guards the decision not to abort: a single
// unusable q/r pair must not hide the rest of the ladder.
func TestLadderSurvivesOneBadRung(t *testing.T) {
	rungs := []Rung{
		{Line: 52.5, Price: -110, Q: 0.72, R: 0.48},
		{Line: 75.5, Price: 145, Q: 0.40, R: 0.40}, // q == r, unusable
		{Line: 100.5, Price: 450, Q: 0.40, R: 0.08},
	}
	steps, err := AnalyzeLadder(rungs, 0.25, 0.45)
	if err != nil {
		t.Fatal(err)
	}
	if steps[1].Err == nil {
		t.Error("the q == r rung should have recorded an error")
	}
	if steps[0].Err != nil || steps[2].Err != nil {
		t.Error("a bad rung must not spoil the others")
	}
	if _, ok := BestRung(steps); !ok {
		t.Error("the remaining rungs should still yield a best")
	}
}

// TestBestRungFindsNothingWhenNothingQualifies covers the empty case, which is
// a valid answer rather than an error.
func TestBestRungFindsNothingWhenNothingQualifies(t *testing.T) {
	rungs := []Rung{
		{Line: 100.5, Price: 450, Q: 0.40, R: 0.08},
		{Line: 125.5, Price: 900, Q: 0.22, R: 0.03},
	}
	// A belief far too low for either rung.
	steps, err := AnalyzeLadder(rungs, 0.02, 0.05)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.Classification.Source != BeyondYourRead {
			t.Errorf("rung %.1f should be beyond-your-read, got %v", s.Line, s.Classification.Source)
		}
	}
	if _, ok := BestRung(steps); ok {
		t.Error("no rung should qualify")
	}
}

// TestVerdictRejectsNonsense keeps the fail-loudly contract.
func TestVerdictRejectsNonsense(t *testing.T) {
	req, err := RequiredScenarioProb(450, 0.40, 0.08)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []float64{-0.1, 1.1} {
		if _, err := Classify(req, bad, 0.4); err == nil {
			t.Errorf("market probability %v must be rejected", bad)
		}
		if _, err := Classify(req, 0.2, bad); err == nil {
			t.Errorf("your probability %v must be rejected", bad)
		}
	}
	if _, err := AnalyzeLadder(nil, 0.2, 0.4); err == nil {
		t.Error("an empty ladder must be rejected")
	}
}

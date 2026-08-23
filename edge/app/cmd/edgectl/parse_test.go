package main

import (
	"testing"

	"edge/internal/scenario"
	"edge/internal/wager"
)

// TestParseRungs covers the ladder spec, which is the only place this binary
// does non-trivial parsing. A silently misparsed rung would move a price or a
// probability into the wrong field and produce a plausible, wrong verdict.
func TestParseRungs(t *testing.T) {
	got, err := parseRungs("52.5:-110:0.72:0.48, 100.5:+450:0.40:0.08")
	if err != nil {
		t.Fatal(err)
	}
	want := []wager.Rung{
		{Line: 52.5, Price: -110, Q: 0.72, R: 0.48},
		{Line: 100.5, Price: 450, Q: 0.40, R: 0.08},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rungs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rung %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRungsRejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"52.5:-110:0.72",          // too few fields
		"52.5:-110:0.72:0.48:0.1", // too many
		"52.5:abc:0.72:0.48",      // price not a number
		"x:-110:0.72:0.48",        // line not a number
	} {
		if _, err := parseRungs(bad); err == nil {
			t.Errorf("%q should have been rejected", bad)
		}
	}
}

func TestParseBankrollAndBasis(t *testing.T) {
	for in, want := range map[string]wager.Bankroll{
		"real": wager.RealMoney, "cash": wager.RealMoney,
		"bonus": wager.BonusBet, "snr": wager.BonusBet,
		"BONUS": wager.BonusBet,
	} {
		got, err := parseBankroll(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("parseBankroll(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := parseBankroll("chips"); err == nil {
		t.Error("an unknown bankroll must be rejected, not defaulted to cash")
	}

	for in, want := range map[string]scenario.Basis{
		"total": scenario.Total, "t": scenario.Total,
		"margin": scenario.Margin, "M": scenario.Margin,
	} {
		got, err := parseBasis(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("parseBasis(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := parseBasis("score"); err == nil {
		t.Error("an unknown basis must be rejected")
	}
}

// TestMarketScenarioRequiresInput guards the case where neither a game line nor
// an override is supplied: the tool must not invent a market view.
func TestMarketScenarioRequiresInput(t *testing.T) {
	if _, err := marketScenario("s", scenario.Total, 0, 0, 50, 0, -1, false); err == nil {
		t.Error("a total-based scenario with no total and no override must fail")
	}
	// An explicit override is enough on its own.
	s, err := marketScenario("s", scenario.Total, 0, 0, 50, 0, 0.3, false)
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != scenario.Stated || s.Prob != 0.3 {
		t.Errorf("override should produce a stated 0.3, got %v", s)
	}
}

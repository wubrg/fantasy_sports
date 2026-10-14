package main

import (
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/scenario"
)

func TestResolveConditionalsStatedHasNoMedian(t *testing.T) {
	q, r, source, median, hasMedian, err := resolveConditionals(
		"receiving_yards", "shootout", 0.6, 0.4,
		scenario.Site{Posted: 49, Baseline: 50, Trend: 0.01}, 52.5, 0.95, "total", 50, "over")
	if err != nil {
		t.Fatal(err)
	}
	if source != "stated" {
		t.Errorf("source = %q, want stated", source)
	}
	if hasMedian {
		t.Errorf("expected hasMedian = false when q/r are operator-stated, got median %v", median)
	}
	if q != 0.6 || r != 0.4 {
		t.Errorf("q/r = %v/%v, want 0.6/0.4 (passed straight through, unchanged)", q, r)
	}
}

func TestScenarioProjectionsFlagWritesAProjection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/projections.jsonl"
	kickoff := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	err := scenarioCmd([]string{
		"-outcome", "receiving_yards", "-name", "shootout",
		"-total", "40.5", "-threshold", "50",
		"-baseline", "25.8", "-trend", "0.0007",
		"-line", "21", "-price", "-116", "-belief", "0.5",
		"-label", "Cooper Kupp",
		"-projections", path, "-season", "2026", "-week", "6", "-kickoff", kickoff,
	})
	if err != nil {
		t.Fatalf("scenarioCmd: %v", err)
	}
	loaded, err := betlog.LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 projection written, got %d", len(loaded))
	}
	if loaded[0].Projection.Player != "Cooper Kupp" || loaded[0].Projection.MarketLine != 21 {
		t.Errorf("unexpected projection content: %+v", loaded[0].Projection)
	}
}

func TestScenarioProjectionsFlagRefusedWhenQRStated(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/projections.jsonl"
	kickoff := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	err := scenarioCmd([]string{
		"-outcome", "receiving_yards", "-name", "shootout",
		"-total", "49", "-threshold", "50",
		"-q", "0.6", "-r", "0.4", // stated, not looked up -- no median exists
		"-line", "21", "-price", "-116", "-belief", "0.5",
		"-projections", path, "-season", "2026", "-week", "6", "-kickoff", kickoff,
	})
	if err == nil {
		t.Error("expected an error: -projections has nothing to log when q/r are stated")
	}
}

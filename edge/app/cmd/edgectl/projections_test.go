package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
)

func writeOutcomesFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "outcomes.json")
	body := map[string]any{
		"season": 2026, "week": 6,
		"rows": []map[string]any{
			{"player": "Cooper Kupp", "outcome": "receiving_yards", "status": "settled", "actual": 34.0, "reason": ""},
			{"player": "Sam Darnold", "outcome": "passing_yards", "status": "unavailable", "actual": nil, "reason": "inactive"},
		},
	}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProjectionsSettle(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)

	kupp := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 21,
		Kickoff: past.Add(time.Minute)}
	darnold := betlog.Projection{Season: 2026, Week: 6, Player: "Sam Darnold",
		Outcome: "passing_yards", Scenario: "shootout", OurMedian: 200, MarketLine: 220,
		Kickoff: past.Add(time.Minute)}
	if _, err := betlog.RecordProjection(logPath, kupp, past); err != nil {
		t.Fatal(err)
	}
	if _, err := betlog.RecordProjection(logPath, darnold, past); err != nil {
		t.Fatal(err)
	}

	outcomesPath := writeOutcomesFixture(t, dir)
	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}

	loaded, err := betlog.LoadProjections(logPath)
	if err != nil {
		t.Fatal(err)
	}
	byPlayer := map[string]betlog.SettledProjection{}
	for _, p := range loaded {
		byPlayer[p.Projection.Player] = p
	}
	if byPlayer["Cooper Kupp"].Result != betlog.Measured || *byPlayer["Cooper Kupp"].Actual != 34.0 {
		t.Errorf("Kupp not settled correctly: %+v", byPlayer["Cooper Kupp"])
	}
	if byPlayer["Sam Darnold"].Result != betlog.Void {
		t.Errorf("Darnold should be void, got %+v", byPlayer["Sam Darnold"])
	}
}

func TestProjectionsSettleIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)
	kupp := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 21,
		Kickoff: past.Add(time.Minute)}
	betlog.RecordProjection(logPath, kupp, past)
	outcomesPath := writeOutcomesFixture(t, dir)

	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}
	// Running it again must not error or double-settle.
	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}
}

// TestProjectionsSettleVoidReasonSurvivesInterleavedOrder pins the bug a
// reviewer found in this command's first draft: the void loop looked up each
// row's reason from a `reasons` slice indexed by position (reasons[len(settle)+i]),
// which only lines up when every settle-matched projection happens to precede
// every void-matched one in iteration order over the log. Here the
// void-matched projection ("Zach Void") is RECORDED FIRST, so under the buggy
// formula the void loop's reasons[len(settle)+0] would read an empty string
// (there is no second settle row to also go wrong against) and silently fall
// back to "unmeasurable" instead of the real reason. The fix reads the reason
// straight from the outcomes-file result map by (player, outcome), the same
// pattern the settle loop already uses for Actual.
func TestProjectionsSettleVoidReasonSurvivesInterleavedOrder(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)

	// Write order matters here: the void-matched projection goes into the log
	// BEFORE the settle-matched one.
	voided := betlog.Projection{Season: 2026, Week: 6, Player: "Zach Void",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 40, MarketLine: 38,
		Kickoff: past.Add(time.Minute)}
	settled := betlog.Projection{Season: 2026, Week: 6, Player: "Amy Settle",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 60, MarketLine: 58,
		Kickoff: past.Add(time.Minute)}
	if _, err := betlog.RecordProjection(logPath, voided, past); err != nil {
		t.Fatal(err)
	}
	if _, err := betlog.RecordProjection(logPath, settled, past); err != nil {
		t.Fatal(err)
	}

	outcomesPath := filepath.Join(dir, "outcomes.json")
	body := map[string]any{
		"season": 2026, "week": 6,
		"rows": []map[string]any{
			{"player": "Zach Void", "outcome": "receiving_yards", "status": "unavailable",
				"actual": nil, "reason": "specific-reason-xyz"},
			{"player": "Amy Settle", "outcome": "receiving_yards", "status": "settled",
				"actual": 50.0, "reason": ""},
		},
	}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(outcomesPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}

	loaded, err := betlog.LoadProjections(logPath)
	if err != nil {
		t.Fatal(err)
	}
	byPlayer := map[string]betlog.SettledProjection{}
	for _, p := range loaded {
		byPlayer[p.Projection.Player] = p
	}
	zach := byPlayer["Zach Void"]
	if zach.Result != betlog.Void {
		t.Fatalf("Zach Void should be void, got %+v", zach)
	}
	if zach.Note != "specific-reason-xyz" {
		t.Errorf("Zach Void's reason was lost to interleaved ordering: got %q, want %q",
			zach.Note, "specific-reason-xyz")
	}
}

func TestProjectionsScore(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)
	p := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 25,
		Kickoff: past.Add(time.Minute)}
	id, _ := betlog.RecordProjection(logPath, p, past)
	if err := betlog.SettleProjection(logPath, id, 30, ""); err != nil {
		t.Fatal(err)
	}
	if err := projectionsCmd([]string{"score", "-log", logPath}); err != nil {
		t.Fatal(err)
	}
}

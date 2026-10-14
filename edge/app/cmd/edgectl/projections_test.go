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

// seedRobustProjection records one projection row under a given scenario for a
// player/outcome, failing the test on any error.
func seedRobustProjection(t *testing.T, logPath, player, outcome, scenario string, median, line float64) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	p := betlog.Projection{Season: 2026, Week: 7, Player: player, Outcome: outcome,
		Scenario: scenario, OurMedian: median, MarketLine: line, Kickoff: past.Add(time.Minute)}
	if _, err := betlog.RecordProjection(logPath, p, past); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionsRobustThresholdAndSort(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")

	// Four distinct scenarios -> should rank first.
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "shootout", 70, 60)
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "blowout_loss", 68, 60)
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "pass_heavy", 72, 60)
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "efficient_offense", 69, 60)

	// Three distinct scenarios (one duplicated scenario to prove dedup) -> should
	// still clear the default threshold of 3 and rank below the four-scenario group.
	seedRobustProjection(t, logPath, "Bravo Three", "rushing_yards", "shootout", 45, 40)
	seedRobustProjection(t, logPath, "Bravo Three", "rushing_yards", "blowout_loss", 46, 40)
	seedRobustProjection(t, logPath, "Bravo Three", "rushing_yards", "pass_heavy", 44, 40)
	seedRobustProjection(t, logPath, "Bravo Three", "rushing_yards", "pass_heavy", 44, 40) // dup scenario

	// Only two distinct scenarios -> must NOT appear at the default threshold.
	seedRobustProjection(t, logPath, "Charlie Two", "passing_yards", "shootout", 250, 240)
	seedRobustProjection(t, logPath, "Charlie Two", "passing_yards", "blowout_loss", 255, 240)

	rows, err := robustGroups(logPath, 2026, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 groups clearing the threshold, got %d: %+v", len(rows), rows)
	}
	if rows[0].player != "Alpha Four" || rows[0].count != 4 {
		t.Errorf("first group should be Alpha Four with 4 scenarios, got %q with %d", rows[0].player, rows[0].count)
	}
	if rows[1].player != "Bravo Three" || rows[1].count != 3 {
		t.Errorf("second group should be Bravo Three with 3 scenarios, got %q with %d", rows[1].player, rows[1].count)
	}
	for _, r := range rows {
		if r.player == "Charlie Two" {
			t.Errorf("Charlie Two has only 2 scenarios and must not clear a threshold of 3")
		}
	}
}

func TestProjectionsRobustTieBreaksByGap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")

	// Both groups have exactly 3 scenarios; the tie breaks on the largest
	// |OurMedian - MarketLine| gap within the group, descending.
	seedRobustProjection(t, logPath, "Small Gap", "receiving_yards", "shootout", 61, 60)
	seedRobustProjection(t, logPath, "Small Gap", "receiving_yards", "blowout_loss", 59, 60)
	seedRobustProjection(t, logPath, "Small Gap", "receiving_yards", "pass_heavy", 60, 60)

	seedRobustProjection(t, logPath, "Big Gap", "rushing_yards", "shootout", 80, 60) // gap 20
	seedRobustProjection(t, logPath, "Big Gap", "rushing_yards", "blowout_loss", 61, 60)
	seedRobustProjection(t, logPath, "Big Gap", "rushing_yards", "pass_heavy", 59, 60)

	rows, err := robustGroups(logPath, 2026, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 groups, got %d", len(rows))
	}
	if rows[0].player != "Big Gap" {
		t.Errorf("Big Gap should rank first on the larger gap tie-break, got %q", rows[0].player)
	}
}

func TestProjectionsRobustCmdRuns(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "shootout", 70, 60)
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "blowout_loss", 68, 60)
	seedRobustProjection(t, logPath, "Alpha Four", "receiving_yards", "pass_heavy", 72, 60)
	if err := projectionsCmd([]string{"robust", "-log", logPath, "-season", "2026", "-week", "7"}); err != nil {
		t.Fatal(err)
	}
	// Nothing clears a threshold set absurdly high -- must not error.
	if err := projectionsCmd([]string{"robust", "-log", logPath, "-season", "2026", "-week", "7", "-min-scenarios", "9"}); err != nil {
		t.Fatal(err)
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

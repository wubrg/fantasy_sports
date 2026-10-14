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

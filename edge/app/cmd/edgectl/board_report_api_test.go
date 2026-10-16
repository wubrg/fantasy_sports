package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
)

// writeTestProjections appends two same-player, different-scenario rows to
// the server's projections log, so the robust finder has something to find.
func writeTestProjections(t *testing.T, beliefsDir string) {
	t.Helper()
	if err := os.MkdirAll(beliefsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(beliefsDir, "projections.jsonl")
	kickoff := time.Now().Add(72 * time.Hour)
	now := time.Now()
	for _, scenario := range []string{"efficient_offense", "shootout"} {
		p := betlog.Projection{
			Season: 2026, Week: 1, Player: "Cooper Kupp", Outcome: "receiving_yards",
			Scenario: scenario, OurMedian: 70, MarketLine: 55.5, Baseline: 60,
			GeneratedAt: now, Kickoff: kickoff,
		}
		if _, err := betlog.RecordProjection(path, p, now); err != nil {
			t.Fatalf("seeding projection: %v", err)
		}
	}
}

func TestHandleReportIncludesRobustGroups(t *testing.T) {
	ts, boardPath := newTestServer(t)
	beliefsDir := filepath.Join(filepath.Dir(filepath.Dir(boardPath)), "beliefs")
	writeTestProjections(t, beliefsDir)

	res, err := http.Get(ts.URL + "/api/report?week=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /api/report = %d", res.StatusCode)
	}

	var out reportJSON
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(out.RobustGroups) != 1 {
		t.Fatalf("expected 1 robust group, got %d: %+v", len(out.RobustGroups), out.RobustGroups)
	}
	g := out.RobustGroups[0]
	if g.Player != "Cooper Kupp" || g.Outcome != "receiving_yards" || g.Count != 2 {
		t.Errorf("unexpected group: %+v", g)
	}
	if len(g.Scenarios) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(g.Scenarios))
	}
}

func TestHandleReportRobustGroupsEmptyWhenNoProjections(t *testing.T) {
	ts, _ := newTestServer(t)

	res, err := http.Get(ts.URL + "/api/report?week=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var out reportJSON
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(out.RobustGroups) != 0 {
		t.Errorf("expected no robust groups with no projections log, got %+v", out.RobustGroups)
	}
}

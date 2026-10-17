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

// writeTestProjectionsWithSide is writeTestProjections's sibling for
// asserting the robust/not-robust JSON field: it lets each scenario state its
// own side/EV/edge-source, so a caller can seed either a consistent lean or a
// mixed/refused one.
func writeTestProjectionsWithSide(t *testing.T, beliefsDir, player string,
	rows []struct {
		scenario, side string
		ev             float64
		edgeSource     string
	}) {
	t.Helper()
	if err := os.MkdirAll(beliefsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(beliefsDir, "projections.jsonl")
	kickoff := time.Now().Add(72 * time.Hour)
	now := time.Now()
	for _, row := range rows {
		p := betlog.Projection{
			Season: 2026, Week: 1, Player: player, Outcome: "receiving_yards",
			Scenario: row.scenario, OurMedian: 70, MarketLine: 55.5, Baseline: 60,
			Side: row.side, EV: row.ev, EdgeSource: row.edgeSource,
			GeneratedAt: now, Kickoff: kickoff,
		}
		if _, err := betlog.RecordProjection(path, p, now); err != nil {
			t.Fatalf("seeding projection: %v", err)
		}
	}
}

func TestHandleReportRobustGroupsCarriesRobustFlag(t *testing.T) {
	ts, boardPath := newTestServer(t)
	beliefsDir := filepath.Join(filepath.Dir(filepath.Dir(boardPath)), "beliefs")
	writeTestProjectionsWithSide(t, beliefsDir, "Cooper Kupp", []struct {
		scenario, side string
		ev             float64
		edgeSource     string
	}{
		{"efficient_offense", "under", 0.1, "market-alone"},
		{"shootout", "under", 0.15, "market-alone"},
	})
	writeTestProjectionsWithSide(t, beliefsDir, "Mixed Guy", []struct {
		scenario, side string
		ev             float64
		edgeSource     string
	}{
		{"efficient_offense", "under", 0.1, "market-alone"},
		{"shootout", "over", 0.1, "market-alone"},
	})

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

	byPlayer := map[string]robustGroupJSON{}
	for _, g := range out.RobustGroups {
		byPlayer[g.Player] = g
	}
	kupp, ok := byPlayer["Cooper Kupp"]
	if !ok {
		t.Fatalf("expected Cooper Kupp in robust groups: %+v", out.RobustGroups)
	}
	if !kupp.Robust {
		t.Errorf("Cooper Kupp agrees on side across scenarios and should be robust: %+v", kupp)
	}
	mixed, ok := byPlayer["Mixed Guy"]
	if !ok {
		t.Fatalf("expected Mixed Guy in robust groups: %+v", out.RobustGroups)
	}
	if mixed.Robust {
		t.Errorf("Mixed Guy disagrees on side across scenarios and should not be robust: %+v", mixed)
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

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/board"
)

// beliefsTestServer serves a board plus a temp beliefs dir holding one week's
// pack, so the beliefs endpoints have real files to read.
func beliefsTestServer(t *testing.T) (*httptest.Server, *boardServer, string) {
	t.Helper()
	boardDir := t.TempDir()
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_DEN_KC": {Away: "DEN", Home: "KC", Kickoff: "2026-09-13T20:20",
			Books: map[string]board.Lines{"consensus": {}, "fanatics": {}}},
	}}
	if err := writeDoc(filepath.Join(boardDir, "week01.yaml"), doc); err != nil {
		t.Fatal(err)
	}
	srv, err := newBoardServer(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	srv.beliefsDir = t.TempDir()

	// A pack whose kickoff is in the future, so the ingest clock gate does not
	// fire, and a pasteable prompt beside it.
	pack := testPack(time.Now().Add(48 * time.Hour))
	writeTestJSON(t, srv.beliefsDir, "week01.input.json", pack)
	if err := os.WriteFile(filepath.Join(srv.beliefsDir, "week01.prompt.md"),
		[]byte("# BELIEF PACK\npredictions: paste JSON\nabstain where you have no read\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, srv, srv.beliefsDir
}

func TestBeliefsPackEndpoint(t *testing.T) {
	ts, _, _ := beliefsTestServer(t)
	res, err := http.Get(ts.URL + "/api/beliefs/pack?week=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("pack endpoint returned %d", res.StatusCode)
	}
	var p struct {
		Week   int    `json:"week"`
		SHA    string `json:"sha"`
		Prompt string `json:"prompt"`
		Games  []any  `json:"games"`
	}
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Week != 1 || p.SHA == "" || p.Prompt == "" || len(p.Games) == 0 {
		t.Errorf("pack response is thin: %+v", p)
	}

	// A week with no pack is a clean 404, not a panic.
	miss, _ := http.Get(ts.URL + "/api/beliefs/pack?week=9")
	if miss.StatusCode != http.StatusNotFound {
		t.Errorf("missing pack returned %d, want 404", miss.StatusCode)
	}
}

// completeForecast builds a forecast covering every row of the test pack, all
// abstaining, bound to the pack's sha — the shape ingest accepts.
func completeForecast(t *testing.T, beliefsDir string) forecastFile {
	t.Helper()
	sum, err := fileSHA(filepath.Join(beliefsDir, "week01.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	pack := testPack(time.Time{})
	var preds []forecast
	for _, g := range pack.Games {
		for name, def := range pack.Definitions {
			if def.Basis == "total" {
				preds = append(preds, forecast{GameID: g.GameID, Scenario: name,
					Belief: 0.3, Abstained: true})
				continue
			}
			for _, team := range []string{g.Away, g.Home} {
				preds = append(preds, forecast{GameID: g.GameID, Team: team, Scenario: name,
					Belief: 0.3, Abstained: true})
			}
		}
	}
	return forecastFile{
		Season: 2026, Week: 1, InputPackSHA: sum,
		Model: "test", Prompt: "belief-v1", GeneratedAt: time.Now(),
		Predictions: preds,
	}
}

func TestBeliefsIngestEndpoint(t *testing.T) {
	ts, _, beliefsDir := beliefsTestServer(t)
	ff := completeForecast(t, beliefsDir)
	blob, _ := json.Marshal(ff)

	// Preview: nothing written, ready count reported.
	code, body := post(t, ts, "/api/beliefs/ingest",
		map[string]any{"week": 1, "blob": string(blob), "apply": false})
	if code != 200 {
		t.Fatalf("preview returned %d: %v", code, body)
	}
	result, _ := body["result"].(map[string]any)
	if result == nil || result["ready"].(float64) != float64(len(ff.Predictions)) {
		t.Errorf("preview ready count wrong: %v", body)
	}
	if _, err := os.Stat(filepath.Join(beliefsDir, "log.jsonl")); !os.IsNotExist(err) {
		t.Error("preview wrote the log, but it must not")
	}

	// Apply: writes the log.
	code, body = post(t, ts, "/api/beliefs/ingest",
		map[string]any{"week": 1, "blob": string(blob), "apply": true})
	if code != 200 || body["applied"] != true {
		t.Fatalf("apply returned %d: %v", code, body)
	}
	if _, err := os.Stat(filepath.Join(beliefsDir, "log.jsonl")); err != nil {
		t.Errorf("apply did not write the log: %v", err)
	}

	// A garbage blob is a 400, not a 500 or a panic.
	code, _ = post(t, ts, "/api/beliefs/ingest",
		map[string]any{"week": 1, "blob": "{not json", "apply": false})
	if code != http.StatusBadRequest {
		t.Errorf("bad blob returned %d, want 400", code)
	}
}

func TestBeliefsScoreEndpoint(t *testing.T) {
	ts, _, _ := beliefsTestServer(t)
	// Nothing settled yet: the endpoint returns 200 with a notice, not an error.
	res, err := http.Get(ts.URL + "/api/beliefs/score?from=1&to=18")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("score endpoint returned %d", res.StatusCode)
	}
	var s ScoreResult
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.Notice == "" {
		t.Errorf("empty-log score should carry a notice, got %+v", s)
	}
}

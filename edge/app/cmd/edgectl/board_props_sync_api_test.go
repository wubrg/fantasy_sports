package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edge/internal/board"
)

// newTestServerWithIngest is newTestServer plus a configured ingest folder,
// for the props-sync endpoints.
func newTestServerWithIngest(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	ingest := t.TempDir()
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{
				"consensus":  {ML: "+140/-165", Spread: "3.5 -110/-110", Total: "44.5 -110/-110"},
				"draftkings": {},
			}},
	}}
	path := filepath.Join(dir, "week01.yaml")
	if err := writeDoc(path, doc); err != nil {
		t.Fatal(err)
	}
	srv, err := newBoardServer(dir, ingest)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, path, ingest
}

// writeDKCapture drops a DK sportscontent moneyline+spread capture for
// SF @ LA into the ingest folder as raw JSON (no HAR wrapper needed --
// oddspull.Parse accepts either shape directly).
func writeDKCapture(t *testing.T, dir, name string) {
	t.Helper()
	body := `{"events":[{"id":"E1","name":"SF @ LA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Moneyline"},` +
		`{"id":"M2","eventId":"E1","name":"Point Spread"}],` +
		`"selections":[` +
		`{"marketId":"M1","label":"SF 49ers","displayOdds":{"american":"-150"}},` +
		`{"marketId":"M1","label":"LA Rams","displayOdds":{"american":"+130"}},` +
		`{"marketId":"M2","label":"SF 49ers -3.5","displayOdds":{"american":"-110"},"points":-3.5},` +
		`{"marketId":"M2","label":"LA Rams +3.5","displayOdds":{"american":"-110"},"points":3.5}]}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServePropsSyncPreviewThenApply(t *testing.T) {
	ts, path, ingest := newTestServerWithIngest(t)
	writeDKCapture(t, ingest, "dk.json")

	code, body := post(t, ts, "/api/props/sync/preview", propsSyncRequest{Week: 1, Book: "draftkings"})
	if code != 200 {
		t.Fatalf("preview: %d %v", code, body)
	}
	changes, _ := body["changes"].([]any)
	if len(changes) != 2 { // ml + spread; the capture carries no total
		t.Fatalf("preview changes = %v", body["changes"])
	}

	// Preview must not write.
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "-150/+130") {
		t.Fatal("preview wrote to the file")
	}

	code, body = post(t, ts, "/api/props/sync/apply", propsSyncRequest{Week: 1, Book: "draftkings"})
	if code != 200 {
		t.Fatalf("apply: %d %v", code, body)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), "-150/+130") || !strings.Contains(string(raw), "-3.5 -110/-110") {
		t.Errorf("sync not persisted:\n%s", raw)
	}
}

func TestServePropsSyncDefaultsBookAndRejectsWithoutIngestDir(t *testing.T) {
	ts, _, ingest := newTestServerWithIngest(t)
	writeDKCapture(t, ingest, "dk.json")

	// No book named: defaults to draftkings (board.DefaultBook), same as the
	// bets tab.
	code, body := post(t, ts, "/api/props/sync/preview", propsSyncRequest{Week: 1})
	if code != 200 {
		t.Fatalf("preview: %d %v", code, body)
	}
	if got, _ := body["book"].(string); got != "draftkings" {
		t.Errorf("book = %q, want draftkings default", got)
	}

	// No ingest folder configured: the request must be refused before it ever
	// tries to load a week file.
	srv2 := &boardServer{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/props/sync/preview",
		strings.NewReader(`{"week":1,"book":"draftkings"}`))
	srv2.handlePropsSyncPreview(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("no ingest dir configured: got %d %s, want 400", rr.Code, rr.Body.String())
	}
}

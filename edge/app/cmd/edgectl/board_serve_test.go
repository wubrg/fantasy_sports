package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edge/internal/board"
)

// newTestServer writes a two-game week into a temp dir and serves it.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{
				"consensus": {ML: "+140/-165", Spread: "3.5 -110/-110", Total: "44.5 -110/-110"},
				"fanatics":  {},
			}},
		"2026_01_DAL_NYG": {Away: "DAL", Home: "NYG", Kickoff: "2026-09-13T20:20",
			Books: map[string]board.Lines{"consensus": {}, "fanatics": {}}},
	}}
	path := filepath.Join(dir, "week01.yaml")
	if err := writeDoc(path, doc); err != nil {
		t.Fatal(err)
	}

	srv, err := newBoardServer(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, path
}

func post(t *testing.T, ts *httptest.Server, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(ts.URL+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return res.StatusCode, out
}

func TestServeStaticAndBoard(t *testing.T) {
	ts, _ := newTestServer(t)

	// The UI is embedded, so a missing file would only ever show up at runtime.
	for _, p := range []string{"/", "/app.js", "/style.css"} {
		res, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Errorf("GET %s = %d", p, res.StatusCode)
		}
	}

	res, err := http.Get(ts.URL + "/api/board?week=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got boardJSON
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Games) != 2 {
		t.Fatalf("got %d games", len(got.Games))
	}
	// Kickoff order, not map or alphabetical order.
	if got.Games[0].ID != "2026_01_SF_LA" || got.Games[1].ID != "2026_01_DAL_NYG" {
		t.Errorf("games out of kickoff order: %s, %s", got.Games[0].ID, got.Games[1].ID)
	}
	if got.Games[0].Books["consensus"].ML != "+140/-165" {
		t.Errorf("consensus missing: %+v", got.Games[0].Books)
	}
	if len(got.Weeks) != 1 || got.Weeks[0] != 1 {
		t.Errorf("weeks = %v", got.Weeks)
	}
}

func TestServeRereadsFileChangedUnderneath(t *testing.T) {
	// The board is also a text file the operator edits by hand. A cached copy
	// that ignored that would silently revert those edits on the next save.
	ts, path := newTestServer(t)

	// Seed a price through the import-apply endpoint -- the enter tab and its
	// direct /api/price write are gone; every write now goes through a
	// preview-then-apply sync, the same as the props tab's board sync.
	if _, body := post(t, ts, "/api/import/apply", importRequest{
		Week: 1, Book: "fanatics", Blob: "SF +150, LAR -175"}); body["ok"] != true {
		t.Fatalf("seed save failed: %v", body)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "+150/-175", "+200/-240", 1)
	if edited == string(raw) {
		t.Fatal("test did not manage to edit the file")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(ts.URL + "/api/board?week=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got boardJSON
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if v := got.Games[0].Books["fanatics"].ML; v != "+200/-240" {
		t.Errorf("hand edit not picked up: got %q", v)
	}
}

func TestServeImportPreviewThenApply(t *testing.T) {
	ts, path := newTestServer(t)

	code, body := post(t, ts, "/api/import/preview", importRequest{
		Week: 1, Book: "fanatics", Blob: "SF +150, LAR -175, DAL -165, NYG +140"})
	if code != 200 {
		t.Fatalf("preview: %d %v", code, body)
	}
	changes, _ := body["changes"].([]any)
	if len(changes) != 2 {
		t.Fatalf("preview changes = %v", body["changes"])
	}

	// Preview must not write.
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "+150/-175") {
		t.Fatal("preview wrote to the file")
	}

	code, body = post(t, ts, "/api/import/apply", importRequest{
		Week: 1, Book: "fanatics", Blob: "SF +150, LAR -175, DAL -165, NYG +140"})
	if code != 200 {
		t.Fatalf("apply: %d %v", code, body)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), "+150/-175") || !strings.Contains(string(raw), "-165/+140") {
		t.Errorf("import not persisted:\n%s", raw)
	}

	// A shifted blob must be refused outright, not partially applied.
	code, body = post(t, ts, "/api/import/preview", importRequest{
		Week: 1, Book: "fanatics", Blob: "SF +150, NYG -175"})
	if code != http.StatusBadRequest {
		t.Errorf("mismatched blob: got %d %v, want 400", code, body)
	}
}

func TestServeReportDefaultsToDraftKings(t *testing.T) {
	ts, _ := newTestServer(t) // existing helper at board_serve_test.go:16 (seeds a 2-game week 1, serves it; ts auto-closes via t.Cleanup)
	// No ?book / ?books — the report must default to draftkings, not consensus.
	res, err := http.Get(ts.URL + "/api/report?week=1")
	if err != nil {
		t.Fatalf("GET /api/report: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var out struct {
		Book string `json:"book"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Book != "draftkings" {
		t.Fatalf("report book = %q, want draftkings", out.Book)
	}
}

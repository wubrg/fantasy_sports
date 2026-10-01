package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"edge/internal/board"
)

// TestHandleEmitMarketMatchesFunctions is the parity check that matters: the
// HTTP handler must produce byte-for-byte the same MARKET block and provenance
// as calling the plain functions directly (the path the already-tested CLI
// `emit market` also takes). If the two ever drift, a report built from the GUI
// would not match one built from the terminal.
func TestHandleEmitMarketMatchesFunctions(t *testing.T) {
	boardDir := t.TempDir()
	ingest := t.TempDir()

	// A board with one draftkings-priced game line, so gameLineRows has a
	// moneyline pair to de-vig.
	doc := &board.Doc{Season: 2026, Week: 4, Games: map[string]*board.Game{
		"2026_04_PIT_CLE": {Away: "PIT", Home: "CLE", Kickoff: "2026-09-28T13:00",
			Books: map[string]board.Lines{
				"draftkings": {ML: "+140/-166"},
			}},
	}}
	if err := writeDoc(filepath.Join(boardDir, "week04.yaml"), doc); err != nil {
		t.Fatal(err)
	}
	// The same PIT @ CLE capture used across the props tests: two-sided props and
	// a one-sided rung, plus the capture's own (ignored) game lines.
	writeIngestFixture(t, ingest)

	srv := &boardServer{dir: boardDir, ingestDir: ingest, docs: map[int]*weekFile{}}

	rr := httptest.NewRecorder()
	srv.handleEmitMarket(rr, httptest.NewRequest("GET", "/api/emit/market?week=4", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		AsOf        string `json:"as_of"`
		MarketBlock string `json:"market_block"`
		Provenance  string `json:"provenance"`
		RowCount    int    `json:"row_count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rr.Body.String())
	}

	// Reproduce the exact assembly the handler (and the CLI) perform.
	gameRows, err := gameLineRows(boardDir, 4, board.DefaultBook, "")
	if err != nil {
		t.Fatal(err)
	}
	propRowsAll, ing, err := propsRows(ingest, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	var propRows []marketRow
	for _, pr := range propRowsAll {
		if pr.category != "Game" {
			propRows = append(propRows, pr)
		}
	}
	rows := append(gameRows, propRows...)
	asOf := ing.newest.Format("2006-01-02 15:04")

	if resp.AsOf != asOf {
		t.Errorf("as_of = %q, want %q", resp.AsOf, asOf)
	}
	if resp.RowCount != len(rows) {
		t.Errorf("row_count = %d, want %d", resp.RowCount, len(rows))
	}
	if want := renderMarketBlock(rows, asOf); resp.MarketBlock != want {
		t.Errorf("market_block mismatch:\n got %q\nwant %q", resp.MarketBlock, want)
	}
	if want := renderProvenance(asOf, len(rows), len(propRows)); resp.Provenance != want {
		t.Errorf("provenance mismatch:\n got %q\nwant %q", resp.Provenance, want)
	}
	// Sanity: the block must carry the moneyline pair (2 game rows) and the
	// two-sided prop, and the one-sided rung, so it is not trivially empty.
	if resp.RowCount < 3 {
		t.Errorf("row_count = %d, expected game line + props", resp.RowCount)
	}
}

// TestHandleEmitMarketWeekRequired pins that a missing or non-positive week is a
// 400, matching handleBoard's contract rather than silently emitting week 0.
func TestHandleEmitMarketWeekRequired(t *testing.T) {
	srv := &boardServer{dir: t.TempDir(), ingestDir: t.TempDir(), docs: map[int]*weekFile{}}
	for _, q := range []string{"", "?week=0", "?week=-2", "?week=abc"} {
		rr := httptest.NewRecorder()
		srv.handleEmitMarket(rr, httptest.NewRequest("GET", "/api/emit/market"+q, nil))
		if rr.Code != 400 {
			t.Errorf("week %q: status %d, want 400", q, rr.Code)
		}
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"edge/internal/board"
)

// loadWeek re-reads a week file the same way board_import.go does.
func loadWeek(t *testing.T, dir string, week int) *board.Doc {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, fmt.Sprintf("week%02d.yaml", week)))
	if err != nil {
		t.Fatalf("open week: %v", err)
	}
	defer f.Close()
	doc, err := board.Parse(f)
	if err != nil {
		t.Fatalf("parse week: %v", err)
	}
	return doc
}

func TestBoardMirror_copiesConsensusToDraftKings(t *testing.T) {
	dir := t.TempDir()
	// Seed a week file directly, the same shape newTestServer uses: a consensus
	// ML present, draftkings empty.
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{
				"consensus":  {ML: "+145/-175"},
				"draftkings": {},
			}},
	}}
	path := filepath.Join(dir, "week01.yaml")
	if err := writeDoc(path, doc); err != nil {
		t.Fatalf("writeDoc: %v", err)
	}

	if err := boardMirror([]string{"-dir", dir, "-week", "1"}); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	got := loadWeek(t, dir, 1)
	if got.Games["2026_01_SF_LA"].Books["draftkings"].ML != "+145/-175" {
		t.Fatalf("draftkings ML = %q, want the mirrored consensus value",
			got.Games["2026_01_SF_LA"].Books["draftkings"].ML)
	}
}

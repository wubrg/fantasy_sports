package main

import (
	"os"
	"path/filepath"
	"testing"

	"edge/internal/board"
)

func TestPlacedCommitmentsFiltersByWeek(t *testing.T) {
	log := filepath.Join(t.TempDir(), "bets.jsonl")
	// Two bets on the same team (SEA), one tagged week 1, one tagged week 2 --
	// the real-world case of a team playing every week.
	body := `{"kind":"bet","id":"w1","time":"2026-09-08T10:00:00Z","bet":{"selection":"SEA ML","price":150,"bankroll":"bonus bet","stake":10,"week":1,"predicted":0.4}}
{"kind":"bet","id":"w2","time":"2026-09-15T10:00:00Z","bet":{"selection":"SEA ML","price":-120,"bankroll":"bonus bet","stake":10,"week":2,"predicted":0.6}}
`
	if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &board.Doc{Week: 2, Games: map[string]*board.Game{
		"2026_02_SEA_NE": {Away: "SEA", Home: "NE"},
	}}

	_, teams, err := PlacedCommitments(log, doc, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0] != "SEA" {
		t.Fatalf("week 2: teams = %v, want [SEA] (only the week-2 bet should count)", teams)
	}

	// Week 1's board should NOT see itself excluded by the week-2 bet.
	_, teams1, err := PlacedCommitments(log, doc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams1) != 1 || teams1[0] != "SEA" {
		t.Fatalf("week 1: teams = %v, want [SEA] (only the week-1 bet should count)", teams1)
	}
}

func TestPlacedCommitmentsUntaggedFallsBackToTeamMatch(t *testing.T) {
	log := filepath.Join(t.TempDir(), "bets.jsonl")
	// A legacy bet with no week tag at all still falls back to the old
	// team-name heuristic, unchanged from before week-scoping existed.
	body := `{"kind":"bet","id":"legacy","time":"2026-09-08T10:00:00Z","bet":{"selection":"SEA ML","price":150,"bankroll":"bonus bet","stake":10,"predicted":0.4}}
`
	if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &board.Doc{Week: 2, Games: map[string]*board.Game{
		"2026_02_SEA_NE": {Away: "SEA", Home: "NE"},
	}}
	_, teams, err := PlacedCommitments(log, doc, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0] != "SEA" {
		t.Fatalf("teams = %v, want [SEA] (untagged bet should still match by team name)", teams)
	}
}

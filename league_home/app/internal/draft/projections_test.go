package draft

import (
	"os"
	"path/filepath"
	"testing"
)

func testProjIndex() *PlayerIndex {
	return BuildPlayerIndex(map[string]PlayerInfo{
		"1": {Name: "Ja'Marr Chase", Position: "WR", Team: "CIN"},
		"2": {Name: "Bijan Robinson", Position: "RB", Team: "ATL"},
		"6": {Name: "Rams", Position: "DEF", Team: "LAR"},
	})
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadProjectionsPrimaryExcludesDST is the primary source's contract: it
// prices and orders the pool from non-DST rows, but keeps every resolved row
// (DST included) for the trait classifier that reads their components.
func TestLoadProjectionsPrimaryExcludesDST(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ciely-2026.csv",
		"player,position,league_points,auction_value\n"+
			"Ja'Marr Chase,WR,300,60\n"+
			"Bijan Robinson,RB,290,58\n"+
			"Los Angeles Rams,DST,120,3\n")
	// FantasyPros deliberately absent — an optional source degrades to a warning.

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.PrimaryRows) != 3 {
		t.Errorf("PrimaryRows = %d, want 3 (DST kept for traits)", len(pd.PrimaryRows))
	}
	if len(pd.Projections) != 2 {
		t.Errorf("Projections = %d, want 2 (DST excluded)", len(pd.Projections))
	}
	if _, ok := pd.Points["6"]; ok {
		t.Error("DST should not carry a projection point total")
	}
	if pd.Points["1"] != 300 {
		t.Errorf("Chase points = %v, want 300", pd.Points["1"])
	}
	if len(pd.SecondOpinions) != 0 {
		t.Errorf("SecondOpinions = %d, want 0 (FP absent)", len(pd.SecondOpinions))
	}
	want := "FantasyPros source absent — FP column and sharp flags off"
	if len(pd.SecondWarnings) != 1 || pd.SecondWarnings[0] != want {
		t.Errorf("SecondWarnings = %v, want [%q]", pd.SecondWarnings, want)
	}
}

// TestLoadProjectionsSecondOpinionFilterAndSharp covers the FantasyPros
// wrinkles the primary path never sees: only consensus rows belong on the
// board, and the sharp-expert move rides along as a sidecar.
func TestLoadProjectionsSecondOpinionFilterAndSharp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ciely-2026.csv",
		"player,position,league_points,auction_value\nJa'Marr Chase,WR,300,60\nBijan Robinson,RB,290,58\n")
	writeFile(t, dir, "fantasypros-2026.csv",
		"player,position,league_points,baseline,pos_rank,rank_vs_top10,rank_vs_top20\n"+
			"Ja'Marr Chase,WR,305,consensus,1,3,2\n"+
			"Ja'Marr Chase,WR,305,top10,1,0,0\n"+ // subset row: filtered out
			"Bijan Robinson,RB,285,consensus,2,-4,-3\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.SecondOpinions) != 1 {
		t.Fatalf("SecondOpinions = %d, want 1", len(pd.SecondOpinions))
	}
	so := pd.SecondOpinions[0]
	if len(so.Projections) != 2 {
		t.Errorf("second-opinion projections = %d, want 2 (top10 subset row filtered)", len(so.Projections))
	}
	if so.Rank["1"] != 1 || so.Rank["2"] != 2 {
		t.Errorf("ranks = %v, want Chase 1 / Bijan 2", so.Rank)
	}
	// SharpDelta keeps the larger-magnitude of the two subset moves.
	if so.Sharp["1"] != 3 || so.Sharp["2"] != -4 {
		t.Errorf("sharp = %v, want Chase 3 / Bijan -4", so.Sharp)
	}
}

// TestLoadProjectionsRequiredPrimaryMissingIsFatal: the backbone is not
// optional. A board with no primary projection would price everything at zero
// and still render, so its absence must fail loudly.
func TestLoadProjectionsRequiredPrimaryMissingIsFatal(t *testing.T) {
	dir := t.TempDir() // no ciely file written
	if _, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex()); err == nil {
		t.Fatal("expected an error when the required primary source is missing")
	}
}

// TestLoadProjectionsUnmatchedWarns: a name the index misses is a player
// silently absent from the board, so it is surfaced rather than dropped.
func TestLoadProjectionsUnmatchedWarns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ciely-2026.csv",
		"player,position,league_points,auction_value\nJa'Marr Chase,WR,300,60\nNobody At All,WR,10,1\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.PrimaryWarnings) != 1 || pd.PrimaryWarnings[0] != "1 Ciely rows unmatched" {
		t.Errorf("PrimaryWarnings = %v, want [\"1 Ciely rows unmatched\"]", pd.PrimaryWarnings)
	}
}

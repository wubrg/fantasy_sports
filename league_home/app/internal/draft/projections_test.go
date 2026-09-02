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

// fpHeader is the FantasyPros normalized shape the primary source reads. The
// baseline column is what separates consensus from the two sharp subsets, all
// three of which live in this one file.
const fpHeader = "player,position,league_points,baseline,pos_rank," +
	"rank_vs_top10,rank_vs_top20,points_low,points_high\n"

// TestLoadProjectionsPrimaryExcludesDST is the primary source's contract: it
// prices and orders the pool from non-DST rows, but keeps every resolved row
// (DST included) for the trait classifier that reads their components.
func TestLoadProjectionsPrimaryExcludesDST(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,300,consensus,1,3,2,280,320\n"+
		"Bijan Robinson,RB,290,consensus,2,-4,-3,270,310\n"+
		"Los Angeles Rams,DST,120,consensus,1,0,0,110,130\n")
	// Ciely deliberately absent — an optional source degrades to a warning.

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
		t.Errorf("SecondOpinions = %d, want 0 (Ciely absent, no subset rows)", len(pd.SecondOpinions))
	}
	want := "Ciely source absent — Ciely column off"
	if len(pd.SecondWarnings) == 0 || pd.SecondWarnings[0] != want {
		t.Errorf("SecondWarnings = %v, want to begin with %q", pd.SecondWarnings, want)
	}
}

// TestPrimaryDropsRankedButUnprojected guards the trap that opened up when the
// primary became a source that ranks deeper than it projects. FantasyPros
// lists roughly twice as many players as it projects; without RequirePoints on
// the primary path each of those enters the solve at zero, leaves at the
// dollar floor, and reads as a considered opinion that he is worth a dollar.
func TestPrimaryDropsRankedButUnprojected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,300,consensus,1,0,0,280,320\n"+
		"Bijan Robinson,RB,0,consensus,90,0,0,0,0\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.Projections) != 1 {
		t.Fatalf("Projections = %d, want 1 (the unprojected row must not enter the solve)", len(pd.Projections))
	}
	if pd.Projections[0].PlayerID != "1" {
		t.Errorf("kept %q, want Chase", pd.Projections[0].PlayerID)
	}
	if _, ok := pd.Points["2"]; ok {
		t.Error("a ranked-but-unprojected player must carry no point total, not a zero")
	}
}

// TestPrimaryCarriesTheBand: the primary is the only source whose range the
// board can price, so dropping it here would leave every player a bare number.
// Harmless while the primary published a median alone, which is why it went
// unnoticed until a banded source became primary.
func TestPrimaryCarriesTheBand(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,300,consensus,1,0,0,280,320\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.Projections) != 1 {
		t.Fatalf("Projections = %d, want 1", len(pd.Projections))
	}
	p := pd.Projections[0]
	if p.PointsLow != 280 || p.PointsHigh != 320 {
		t.Errorf("band = %v/%v, want 280/320", p.PointsLow, p.PointsHigh)
	}
}

// TestLoadProjectionsSecondOpinionFilterAndSharp covers the wrinkles the
// primary path never sees: the sharp subsets are the same file under a
// different baseline filter, and the sharp-expert move rides along as a
// sidecar on the consensus spine.
func TestLoadProjectionsSecondOpinionFilterAndSharp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,305,consensus,1,3,2,290,320\n"+
		"Bijan Robinson,RB,285,consensus,2,-4,-3,270,300\n"+
		"Ja'Marr Chase,WR,310,top10,1,0,0,295,325\n"+
		"Bijan Robinson,RB,280,top20,3,0,0,265,295\n")
	writeFile(t, dir, "ciely-2026.csv",
		"player,position,league_points,auction_value\nJa'Marr Chase,WR,300,60\nBijan Robinson,RB,290,58\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Consensus is the primary; the subset rows are filtered off it.
	if len(pd.Projections) != 2 {
		t.Errorf("primary projections = %d, want 2 (subset rows filtered)", len(pd.Projections))
	}
	// The sharp delta lives on the consensus spine, keeping the
	// larger-magnitude of the two subset moves.
	byName := map[string]SecondOpinion{}
	for _, so := range pd.SecondOpinions {
		byName[so.Name] = so
	}
	if len(pd.SecondOpinions) != 3 {
		t.Fatalf("SecondOpinions = %d, want 3 (ciely, top10, top20)", len(pd.SecondOpinions))
	}
	if got := len(byName["ciely"].Projections); got != 2 {
		t.Errorf("ciely projections = %d, want 2", got)
	}
	if got := len(byName["fantasypros-top10"].Projections); got != 1 {
		t.Errorf("top10 projections = %d, want 1", got)
	}
	if got := byName["fantasypros-top20"].Rank["2"]; got != 3 {
		t.Errorf("top20 rank for Bijan = %d, want 3", got)
	}
}

// TestLoadProjectionsRequiredPrimaryMissingIsFatal: the backbone is not
// optional. A board with no primary projection would price everything at zero
// and still render, so its absence must fail loudly.
func TestLoadProjectionsRequiredPrimaryMissingIsFatal(t *testing.T) {
	dir := t.TempDir() // no fantasypros file written
	if _, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex()); err == nil {
		t.Fatal("expected an error when the required primary source is missing")
	}
}

// TestLoadProjectionsUnmatchedWarns: a name the index misses is a player
// silently absent from the board, so it is surfaced rather than dropped.
func TestLoadProjectionsUnmatchedWarns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,300,consensus,1,0,0,280,320\n"+
		"Nobody At All,WR,10,consensus,99,0,0,5,15\n")

	pd, err := LoadProjections(func(n string) string { return filepath.Join(dir, n) }, testProjIndex())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pd.PrimaryWarnings) != 1 || pd.PrimaryWarnings[0] != "1 FantasyPros rows unmatched" {
		t.Errorf("PrimaryWarnings = %v, want [\"1 FantasyPros rows unmatched\"]", pd.PrimaryWarnings)
	}
}

// TestPrimaryRankIsTheConsensusNotWhicheverBaselineIsLast is the regression for
// a bug hand-verification caught on the live board.
//
// FantasyPros ships consensus, top-10 and top-20 in one file, and PrimaryRows
// is deliberately raw — Include has not been applied to it. Reading a rank out
// of those rows keys them by player and keeps whichever baseline came last,
// which is top-20. On Travis Hunter that was consensus WR75 against top-20
// WR69: a divergence of -40 reported as -46, wrong by exactly the gap between
// two baselines, and invisible on a board.
//
// So the rank is built where Include is already applied, beside Points.
func TestPrimaryRankIsTheConsensusNotWhicheverBaselineIsLast(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fantasypros-2026.csv", fpHeader+
		"Ja'Marr Chase,WR,300,consensus,75,3,2,280,320\n"+
		"Ja'Marr Chase,WR,300,top10,73,3,2,280,320\n"+
		"Ja'Marr Chase,WR,300,top20,69,3,2,280,320\n")

	pd, err := loadProjections(ProjectionSources, func(f string) string {
		return filepath.Join(dir, f)
	}, testProjIndex())
	if err != nil {
		t.Fatal(err)
	}

	if got := pd.PrimaryRank["1"]; got != 75 {
		t.Errorf("PrimaryRank = %d, want the consensus 75 — a later baseline in "+
			"the same file overwrote it", got)
	}
}

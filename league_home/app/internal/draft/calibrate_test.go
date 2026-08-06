package draft

import (
	"math"
	"strings"
	"testing"
)

func team(season string, points float64, rank int, picks ...DraftedPlayer) TeamSeason {
	return TeamSeason{Season: season, Points: points, Rank: rank, Picks: picks}
}

func rb(price int) DraftedPlayer   { return DraftedPlayer{Position: "RB", Price: price} }
func wr(price int) DraftedPlayer   { return DraftedPlayer{Position: "WR", Price: price} }
func any_(price int) DraftedPlayer { return DraftedPlayer{Position: "TE", Price: price} }

// TestEveryArchetypeRecordsWhatItWasCalibratedAgainst.
//
// The threshold and the evidence for it have to travel together. Before
// calibration Zero RB described one roster in three seasons and nothing in
// the source said so; this fails if a threshold is changed without
// re-deriving what it now describes.
func TestEveryArchetypeRecordsWhatItWasCalibratedAgainst(t *testing.T) {
	for _, a := range Archetypes() {
		if strings.TrimSpace(a.Seen) == "" {
			t.Errorf("%s has no record of how often it has been built — run draftroom calibrate", a.Name)
		}
		if a.Satisfied == nil {
			t.Errorf("%s cannot be checked against a finished roster", a.Name)
		}
	}
}

// TestZeroAndRobustRBCannotBothHold is the collision the calibration had to
// design out.
//
// Loosening both until each described a real slice of the league first made
// them overlap, and a roster that is at once "no back over $25" and "three
// real backs" means the two labels have stopped saying anything.
func TestZeroAndRobustRBCannotBothHold(t *testing.T) {
	var zero, robust Archetype
	for _, a := range Archetypes() {
		switch a.Name {
		case "Zero RB":
			zero = a
		case "Robust RB":
			robust = a
		}
	}
	if zero.Name == "" || robust.Name == "" {
		t.Fatal("expected both RB shapes to exist")
	}
	// Every backfield worth $1 to $60 a man, three deep.
	for a := 1; a <= 60; a++ {
		for b := 1; b <= a; b++ {
			for c := 1; c <= b; c++ {
				r := team("x", 0, 0, rb(a), rb(b), rb(c)).Roster()
				if zero.Satisfied(r) && robust.Satisfied(r) {
					t.Fatalf("backfield $%d/$%d/$%d reads as both Zero RB and Robust RB", a, b, c)
				}
			}
		}
	}
}

// TestHeroRBChecksBothHalves — the shape is as much about the backs you do
// not buy as the one you do, and the finished-roster check used to count
// only the hero while the per-pick rule enforced both.
func TestHeroRBChecksBothHalves(t *testing.T) {
	var hero Archetype
	for _, a := range Archetypes() {
		if a.Name == "Hero RB" {
			hero = a
		}
	}
	if hero.Name == "" {
		t.Fatal("Hero RB missing")
	}
	if !hero.Satisfied(team("x", 0, 0, rb(55), rb(8), wr(30)).Roster()) {
		t.Error("one big back and a cheap one behind him is the shape")
	}
	if hero.Satisfied(team("x", 0, 0, rb(55), rb(45), wr(30)).Roster()) {
		t.Error("two expensive backs is not Hero RB under any reading")
	}
	if hero.Satisfied(team("x", 0, 0, rb(12), rb(8)).Roster()) {
		t.Error("no hero at all is not Hero RB")
	}
}

func TestTeamSeasonAccounting(t *testing.T) {
	ts := team("2025", 1500, 3, rb(40), rb(12), wr(35), any_(5))

	if got := ts.Spend(""); got != 92 {
		t.Errorf("total spend = %d, want 92", got)
	}
	if got := ts.Spend("RB"); got != 52 {
		t.Errorf("RB spend = %d, want 52", got)
	}
	if got := ts.PricesAt("RB"); len(got) != 2 || got[0] != 40 || got[1] != 12 {
		t.Errorf("RB prices = %v, want [40 12] descending", got)
	}
	if got := ts.CountOver("RB", 20); got != 1 {
		t.Errorf("RBs over $20 = %d, want 1", got)
	}
	if got := ts.CountOver("", 10); got != 3 {
		t.Errorf("players over $10 = %d, want 3", got)
	}
}

func TestFitArchetypesCountsAndFinishes(t *testing.T) {
	shape := []Archetype{{
		Name:      "Big Back",
		Satisfied: func(r Roster) bool { return len(r.Players) > 0 && r.Players[0].Price > 40 },
	}}
	seasons := []TeamSeason{
		team("2025", 1600, 1, rb(50)),
		team("2025", 1500, 5, rb(45)),
		team("2025", 1400, 9, rb(10)),
	}
	got := FitArchetypes(seasons, shape)
	if len(got) != 1 {
		t.Fatalf("expected one fit, got %d", len(got))
	}
	f := got[0]
	if f.Seen != 2 || f.Total != 3 {
		t.Errorf("seen %d of %d, want 2 of 3", f.Seen, f.Total)
	}
	if f.MedianRank != 3 {
		t.Errorf("median rank = %v, want 3", f.MedianRank)
	}
	// Ranks 1 and 5 match the shape; only the first is a top-four finish.
	if f.TopQuarter != 1 {
		t.Errorf("top-4 finishes = %d, want 1", f.TopQuarter)
	}
	if math.Abs(f.Share()-2.0/3.0) > 1e-9 {
		t.Errorf("share = %v", f.Share())
	}
}

// TestArchetypeNeverBuiltIsReported — the failure mode the whole command
// exists to surface.
func TestArchetypeNeverBuiltIsReported(t *testing.T) {
	impossible := []Archetype{{
		Name:      "Impossible",
		Satisfied: func(r Roster) bool { return false },
	}}
	seasons := []TeamSeason{team("2025", 1500, 1, rb(50))}

	var sb strings.Builder
	if err := WriteCalibration(&sb, seasons, impossible); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "never built") {
		t.Errorf("a shape nobody has built must say so:\n%s", sb.String())
	}
}

func TestSpearman(t *testing.T) {
	asc := []float64{1, 2, 3, 4, 5}
	if got := Spearman(asc, []float64{10, 20, 30, 40, 50}); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical ordering = %v, want 1", got)
	}
	if got := Spearman(asc, []float64{50, 40, 30, 20, 10}); math.Abs(got+1) > 1e-9 {
		t.Errorf("reversed ordering = %v, want -1", got)
	}
	if got := Spearman(asc, []float64{1, 2}); got != 0 {
		t.Errorf("mismatched lengths = %v, want 0", got)
	}
}

// TestSpearmanTiesDoNotManufactureCorrelation — a column where every team
// spent the same has no ordering, and averaging tied ranks is what keeps it
// from reporting one.
func TestSpearmanTiesDoNotManufactureCorrelation(t *testing.T) {
	flat := []float64{7, 7, 7, 7, 7}
	if got := Spearman(flat, []float64{1, 2, 3, 4, 5}); got != 0 {
		t.Errorf("a constant series correlates %v with everything, want 0", got)
	}
	got := ranks([]float64{5, 5, 1})
	if got[0] != 2.5 || got[1] != 2.5 || got[2] != 1 {
		t.Errorf("tied ranks = %v, want [2.5 2.5 1]", got)
	}
}

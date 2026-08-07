package draft

import (
	"math"
	"testing"
)

func team(season string, points float64, rank int, picks ...DraftedPlayer) TeamSeason {
	return TeamSeason{Season: season, Points: points, Rank: rank, Picks: picks}
}

func rb(price int) DraftedPlayer   { return DraftedPlayer{Position: "RB", Price: price} }
func wr(price int) DraftedPlayer   { return DraftedPlayer{Position: "WR", Price: price} }
func any_(price int) DraftedPlayer { return DraftedPlayer{Position: "TE", Price: price} }

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

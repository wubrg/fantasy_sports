package draft

import "testing"

// band builds a descending projection list from explicit points.
func band(pts ...float64) []float64 { return pts }

// TestTierMedianRisesWhenTheSlotSitsLowInItsTier is the case that motivates
// the whole measure.
//
// The last starting slot lands at the bottom of a tight cluster, so the
// typical member of that cluster is meaningfully better than the man in the
// slot — and the count of players that good is smaller than the slot count.
// A point estimate at the slot itself reports the opposite.
func TestTierMedianRisesWhenTheSlotSitsLowInItsTier(t *testing.T) {
	// Ranks 1-4 are a top tier, 5-8 a cluster, then a break to the bench.
	pts := band(400, 360, 340, 302, 296, 293, 290, 288, 260, 258, 256, 254, 252, 250, 248, 246)
	got := tierMedianAt(pts, 8)

	if got <= pts[7] {
		t.Errorf("tier median %.1f should sit above the slot player at %.1f", got, pts[7])
	}
	if got != 291.5 {
		t.Errorf("tier median = %.1f, want 291.5 (median of the 296-288 cluster)", got)
	}
	startable := 0
	for _, p := range pts {
		if p >= got {
			startable++
		}
	}
	if startable != 6 {
		t.Errorf("startable = %d, want 6 — fewer than the 8 starting slots", startable)
	}
}

// TestTierMedianFallsWhenTheSlotSitsHighInItsTier — the mirror image. The
// slot falls at the top of a long flat band, so plenty of players are as
// good as the typical member and the position is deeper than the slot count.
func TestTierMedianFallsWhenTheSlotSitsHighInItsTier(t *testing.T) {
	pts := band(400, 360, 340, 200, 199, 198, 197, 196, 195, 194, 193, 192, 150, 148, 146, 144)
	got := tierMedianAt(pts, 4)

	if got >= pts[3] {
		t.Errorf("tier median %.1f should sit below the slot player at %.1f", got, pts[3])
	}
	startable := 0
	for _, p := range pts {
		if p >= got {
			startable++
		}
	}
	if startable <= 4 {
		t.Errorf("startable = %d, want more than the 4 starting slots", startable)
	}
}

// TestTierStraddlesTheSlot — a cluster sitting across the last starting spot
// is one cluster. Clipping it at the slot would make the median an artifact
// of where the league's roster rules happen to fall.
func TestTierStraddlesTheSlot(t *testing.T) {
	pts := band(400, 360, 250, 249, 248, 247, 246, 245, 200, 198, 196, 194)
	// Slot 4 sits inside the 250-245 band, which runs to rank 8.
	got := tierMedianAt(pts, 4)
	if got != 247.5 {
		t.Errorf("median = %.1f, want 247.5 from the whole 250-245 band", got)
	}
}

// TestIsolatedSlotFallsBackToThePlayer — a marginal starter with a break on
// both sides has no cluster, so the point estimate is the only line there
// is. Worth pinning: it is how the measure behaves for tight ends this year.
func TestIsolatedSlotFallsBackToThePlayer(t *testing.T) {
	// A lone player with a chasm on both sides and tight packs either end.
	pts := band(400, 398, 396, 394, 300, 200, 198, 196, 194, 192)
	got := tierMedianAt(pts, 5)
	if got != 300 {
		t.Errorf("median = %.1f, want the slot player at 300", got)
	}
}

// TestSmoothPositionHasNoTiers — an even ramp has no breaks to find, so the
// tier is the whole contended region and its median is the bar. Worth
// pinning because it is the fallback the measure lands on when the position
// genuinely has no structure.
func TestSmoothPositionHasNoTiers(t *testing.T) {
	pts := band(400, 380, 360, 340, 320, 300, 280, 260, 240, 220)
	got := tierMedianAt(pts, 5)
	if got != 310 {
		t.Errorf("median = %.1f, want 310 — the middle of the whole ramp", got)
	}
	startable := 0
	for _, p := range pts {
		if p >= got {
			startable++
		}
	}
	if startable != 5 {
		t.Errorf("startable = %d, want 5 — a structureless position reports its slot count", startable)
	}
}

// TestScarcityCountsMeetOrExceed — a player projecting exactly at the tier
// median is as good as the typical member of it.
func TestScarcityCountsMeetOrExceed(t *testing.T) {
	players := []PlayerSignals{
		{Name: "over", Position: "RB", CielyPoints: 201},
		{Name: "exactly", Position: "RB", CielyPoints: 200},
		{Name: "under", Position: "RB", CielyPoints: 199},
	}
	got := Scarcity(players, HitOrMissPool(), map[string]float64{"RB": 200})
	if got["RB"].Startable != 2 {
		t.Errorf("startable = %d, want 2 — the exact match counts", got["RB"].Startable)
	}
}

func TestTierMedianEdgeCases(t *testing.T) {
	if got := tierMedianAt(nil, 5); got != 0 {
		t.Errorf("no players = %v, want 0", got)
	}
	if got := tierMedianAt(band(300, 200), 0); got != 300 {
		t.Errorf("no starting demand = %v, want the best available", got)
	}
	// Demand outruns supply: everyone left is startable by this measure.
	if got := tierMedianAt(band(300, 200), 9); got > 300 {
		t.Errorf("threshold %v is above the whole pool", got)
	}
	// A position with no separation anywhere has no tiers to find.
	flat := band(100, 100, 100, 100, 100, 100)
	if got := tierMedianAt(flat, 3); got != 100 {
		t.Errorf("flat position = %v, want 100", got)
	}
}

// TestThresholdSinksIfRecomputedFromTheRemainingPool shows why the caller
// has to pin it.
//
// Recomputed against what is left, the threshold chases the pool down, so
// the count above it barely moves and scarcity can never register. This is
// the failure the pinning exists to prevent, asserted directly rather than
// trusted to a comment.
func TestThresholdSinksIfRecomputedFromTheRemainingPool(t *testing.T) {
	pts := band(400, 360, 340, 302, 296, 293, 290, 288, 260, 258, 256, 254,
		240, 238, 236, 234, 200, 198, 196, 194, 160, 158, 156, 154)
	var all []Projection
	for _, p := range pts {
		all = append(all, Projection{Position: "QB", Points: p})
	}
	shape := HitOrMissPool()

	pinned := ScarcityThresholds(all, shape)["QB"]
	// The top eight are drafted.
	recomputed := ScarcityThresholds(all[8:], shape)["QB"]

	if recomputed >= pinned {
		t.Fatalf("recomputed threshold %.1f did not sink below the pinned %.1f",
			recomputed, pinned)
	}

	remaining := all[8:]
	countAgainst := func(th float64) int {
		n := 0
		for _, p := range remaining {
			if p.Points >= th {
				n++
			}
		}
		return n
	}
	if countAgainst(pinned) >= countAgainst(recomputed) {
		t.Errorf("pinned count %d should be the smaller, honest one; recomputed reports %d",
			countAgainst(pinned), countAgainst(recomputed))
	}
}

// TestStartableFallsAsTheTierIsPickedOver, against a pinned threshold.
func TestStartableFallsAsTheTierIsPickedOver(t *testing.T) {
	th := map[string]float64{"WR": 200}
	var board []PlayerSignals
	for _, p := range band(300, 280, 260, 240, 220, 180, 160) {
		board = append(board, PlayerSignals{Position: "WR", CielyPoints: p})
	}
	before := Scarcity(board, HitOrMissPool(), th)["WR"].Startable
	after := Scarcity(board[3:], HitOrMissPool(), th)["WR"].Startable

	if before != 5 {
		t.Errorf("before = %d startable, want 5", before)
	}
	if after != 2 {
		t.Errorf("after three went = %d startable, want 2", after)
	}
}

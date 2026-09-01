package calib

import (
	"math"
	"testing"
)

// TestRealisedEdgeTakesTheRightSide. The direction of the disagreement decides
// which side is bet, and getting that backwards would invert the whole
// endpoint — the same class of error as a spread sign, which this repository
// has already shipped once.
func TestRealisedEdgeTakesTheRightSide(t *testing.T) {
	const hold, bar = 0.06, 0.10

	// Betting the scenario happens: p above ref, and it happened.
	// breakeven = 0.30 * 1.06 = 0.318; realised 1; edge +0.682.
	yes := []Point{{P: 0.50, Ref: 0.30, HasRef: true, Y: true}}
	if got := RealisedEdge(yes, bar, hold); math.Abs(got-0.682) > 1e-9 {
		t.Errorf("betting yes and winning gave %.4f, want +0.682", got)
	}
	// The same wager, lost.
	no := []Point{{P: 0.50, Ref: 0.30, HasRef: true, Y: false}}
	if got := RealisedEdge(no, bar, hold); math.Abs(got+0.318) > 1e-9 {
		t.Errorf("betting yes and losing gave %.4f, want -0.318", got)
	}

	// Betting AGAINST: p below ref, and it did not happen.
	// breakeven = (1-0.60) * 1.06 = 0.424; realised 1; edge +0.576.
	against := []Point{{P: 0.40, Ref: 0.60, HasRef: true, Y: false}}
	if got := RealisedEdge(against, bar, hold); math.Abs(got-0.576) > 1e-9 {
		t.Errorf("betting against and winning gave %.4f, want +0.576", got)
	}
	// Betting against and being wrong.
	againstLost := []Point{{P: 0.40, Ref: 0.60, HasRef: true, Y: true}}
	if got := RealisedEdge(againstLost, bar, hold); math.Abs(got+0.424) > 1e-9 {
		t.Errorf("betting against and losing gave %.4f, want -0.424", got)
	}
}

// Only rows over the bar become wagers. A forecaster that agrees with the
// market everywhere places no bets, and the statistic must say so rather than
// returning zero — which would read as "broke even".
func TestRealisedEdgeIgnoresRowsUnderTheBar(t *testing.T) {
	agrees := []Point{
		{P: 0.33, Ref: 0.32, HasRef: true, Y: true},
		{P: 0.31, Ref: 0.33, HasRef: true, Y: false},
	}
	if got := RealisedEdge(agrees, 0.10, 0.06); !math.IsNaN(got) {
		t.Errorf("a forecaster that places no wagers returned %.4f, want NaN", got)
	}
	if n := OverBarCount(agrees, 0.10); n != 0 {
		t.Errorf("over-bar count %d, want 0", n)
	}

	// Abstentions never become wagers, however far they sit from the reference.
	abstained := []Point{{P: 0.60, Ref: 0.30, HasRef: true, Y: true, Abstained: true}}
	if got := RealisedEdge(abstained, 0.10, 0.06); !math.IsNaN(got) {
		t.Errorf("an abstention was counted as a wager: %.4f", got)
	}
}

// A row with no reference has no price to beat, so it cannot be a wager.
func TestRealisedEdgeNeedsAReference(t *testing.T) {
	if got := RealisedEdge([]Point{{P: 0.9, Y: true}}, 0.10, 0.06); !math.IsNaN(got) {
		t.Errorf("a row with no reference produced an edge of %.4f", got)
	}
}

// The hold is proportional, so a bigger one must cost more.
func TestRealisedEdgeRespondsToTheHold(t *testing.T) {
	pts := []Point{{P: 0.60, Ref: 0.40, HasRef: true, Y: true}}
	cheap := RealisedEdge(pts, 0.10, 0.02)
	dear := RealisedEdge(pts, 0.10, 0.10)
	if !(cheap > dear) {
		t.Errorf("a 10%% hold (%.4f) did not cost more than a 2%% hold (%.4f)", dear, cheap)
	}
}

// TestTheTwoEndpointsCanDisagree is the whole reason both are registered.
//
// A forecaster better than the reference by a hair on every row is more
// accurate and never places a wager. One right about a few big calls and
// mediocre elsewhere places wagers that win and may not look more accurate.
// Registering either alone would let the wrong conclusion be drawn.
func TestTheTwoEndpointsCanDisagree(t *testing.T) {
	const bar, hold = 0.10, 0.06

	// Accurate everywhere, never disagrees enough to bet.
	var timid []Point
	for i := 0; i < 40; i++ {
		y := i%3 == 0
		p := 0.32
		if y {
			p = 0.36
		}
		timid = append(timid, Point{P: p, Ref: 0.33, HasRef: true, Y: y})
	}
	if g := PairedBrierGain(timid); g <= 0 {
		t.Errorf("the timid forecaster should be more accurate, gain %.5f", g)
	}
	if e := RealisedEdge(timid, bar, hold); !math.IsNaN(e) {
		t.Errorf("the timid forecaster placed wagers it should not have: %.4f", e)
	}
	if n := OverBarCount(timid, bar); n != 0 {
		t.Errorf("%d wagers from a forecaster that never clears the bar", n)
	}
}

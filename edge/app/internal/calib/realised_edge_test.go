package calib

import (
	"math"
	"testing"
)

// A wagerable row carries a frozen site (q, r). These helpers build one on each
// side so the tests read as the wagers they describe.
func over(sYou, sRef, q, r float64, occurred bool) Point {
	return Point{P: sYou, Ref: sRef, HasRef: true, Q: q, R: r, HasQR: true, Y: occurred}
}

// TestRealisedEdgeIsROIPerUnitStaked pins the units to FINDINGS §16's, which is
// the whole point of the rewrite: the old statistic returned per unit of payout
// and could not be compared to the +7%..+18% oracle bound.
//
// q=0.60 r=0.30, s_ref=0.30 s_you=0.60, hold 6%:
//
//	P_book = .60*.30 + .30*.70 = 0.39 ;  breakeven 0.39*1.06 = 0.4134
//	P_you  = .60*.60 + .30*.40 = 0.48 > 0.4134, so the over is a wager
//	scenario occurred -> realised hit rate q = 0.60
//	ROI = (0.60 - 0.4134) / 0.4134 = +0.4514
func TestRealisedEdgeIsROIPerUnitStaked(t *testing.T) {
	won := []Point{over(0.60, 0.30, 0.60, 0.30, true)}
	if got := RealisedEdge(won, 0.0, 0.06); math.Abs(got-0.4514) > 1e-3 {
		t.Errorf("winning over bet gave %.4f, want +0.4514 (ROI per unit staked)", got)
	}
	// The same wager, scenario did not occur: realised hit rate r = 0.30.
	// ROI = (0.30 - 0.4134)/0.4134 = -0.2743.
	lost := []Point{over(0.60, 0.30, 0.60, 0.30, false)}
	if got := RealisedEdge(lost, 0.0, 0.06); math.Abs(got+0.2743) > 1e-3 {
		t.Errorf("losing over bet gave %.4f, want -0.2743", got)
	}
}

// TestRealisedEdgeTakesTheRightSide. Below the reference bets the UNDER, and the
// breakeven and realised rate both flip with it. Getting this backwards inverts
// the endpoint -- the same class of error as a spread sign.
//
// q=0.60 r=0.30, s_ref=0.60 s_you=0.30:
//
//	P_book = 0.48 ; under breakeven (1-0.48)*1.06 = 0.5512
//	P_you  = 0.39 ; 1-P_you = 0.61 > 0.5512, the under is a wager
//	scenario did NOT occur -> hit rate r=0.30, the under wins at 1-0.30=0.70
//	ROI = (0.70 - 0.5512)/0.5512 = +0.270
func TestRealisedEdgeTakesTheRightSide(t *testing.T) {
	won := []Point{over(0.30, 0.60, 0.60, 0.30, false)}
	if got := RealisedEdge(won, 0.0, 0.06); math.Abs(got-0.270) > 1e-3 {
		t.Errorf("winning under bet gave %.4f, want +0.270", got)
	}
	// The under, but the scenario occurred: hit rate q=0.60, under loses at 0.40.
	// ROI = (0.40 - 0.5512)/0.5512 = -0.2743.
	lost := []Point{over(0.30, 0.60, 0.60, 0.30, true)}
	if got := RealisedEdge(lost, 0.0, 0.06); math.Abs(got+0.2743) > 1e-3 {
		t.Errorf("losing under bet gave %.4f, want -0.2743", got)
	}
}

// TestRealisedEdgeNeedsAWagerableSite. A row with no frozen q/r is scored for
// accuracy but never becomes a wager -- blowout_loss and pass_heavy have no
// site to deploy on, and inventing one would be the fictional market again.
func TestRealisedEdgeNeedsAWagerableSite(t *testing.T) {
	noSite := []Point{{P: 0.60, Ref: 0.30, HasRef: true, Y: true}} // HasQR false
	if got := RealisedEdge(noSite, 0.0, 0.06); !math.IsNaN(got) {
		t.Errorf("a row with no wagerable site produced an edge of %.4f", got)
	}
	if n := OverBarCount(noSite, 0.0, 0.06); n != 0 {
		t.Errorf("a row with no site counted as %d wagers, want 0", n)
	}

	// A site where the scenario does not move the prop (q == r) cannot be bet.
	flat := []Point{over(0.60, 0.30, 0.40, 0.40, true)}
	if got := RealisedEdge(flat, 0.0, 0.06); !math.IsNaN(got) {
		t.Errorf("a site with q==r produced an edge of %.4f, want no wager", got)
	}
}

// TestRealisedEdgeRequiresClearingTheVig is the fix for the endpoint that passed
// losing strategies: a disagreement that does not beat the book's price after
// its hold is not a wager, even though it is a real disagreement.
func TestRealisedEdgeRequiresClearingTheVig(t *testing.T) {
	// q=0.60 r=0.30, s_ref=0.30 s_you=0.34. P_you = .60*.34+.30*.66 = 0.402.
	// P_book = 0.39, breakeven at 6% = 0.4134. P_you 0.402 < 0.4134: no wager.
	thin := []Point{over(0.34, 0.30, 0.60, 0.30, true)}
	if got := RealisedEdge(thin, 0.0, 0.06); !math.IsNaN(got) {
		t.Errorf("a sub-vig disagreement was counted as a wager: %.4f", got)
	}
	if n := OverBarCount(thin, 0.0, 0.06); n != 0 {
		t.Errorf("sub-vig row counted as %d wagers, want 0", n)
	}
	// At zero hold the same edge clears, because breakeven falls to P_book.
	if got := RealisedEdge(thin, 0.0, 0.0); math.IsNaN(got) {
		t.Error("at zero hold the disagreement should clear and be a wager")
	}
}

// TestRealisedEdgeIgnoresAbstentions. However far an abstention sits from the
// reference, it never becomes a wager.
func TestRealisedEdgeIgnoresAbstentions(t *testing.T) {
	p := over(0.60, 0.30, 0.60, 0.30, true)
	p.Abstained = true
	if got := RealisedEdge([]Point{p}, 0.0, 0.06); !math.IsNaN(got) {
		t.Errorf("an abstention was counted as a wager: %.4f", got)
	}
}

// TestRealisedEdgeRespondsToTheHold. A bigger hold raises the price the wager
// must clear, so it costs more.
func TestRealisedEdgeRespondsToTheHold(t *testing.T) {
	pts := []Point{over(0.60, 0.30, 0.60, 0.30, true)}
	cheap := RealisedEdge(pts, 0.0, 0.02)
	dear := RealisedEdge(pts, 0.0, 0.10)
	if !(cheap > dear) {
		t.Errorf("a 10%% hold (%.4f) did not cost more than a 2%% hold (%.4f)", dear, cheap)
	}
}

// TestTheTwoEndpointsCanDisagree is the whole reason both are registered.
//
// A forecaster better than the reference by a hair on every row is more
// accurate and never clears the vig to place a wager. Registering accuracy
// alone would let it be called a betting edge.
func TestTheTwoEndpointsCanDisagree(t *testing.T) {
	const hold = 0.06

	// Accurate everywhere, never disagrees enough to clear the vig.
	var timid []Point
	for i := 0; i < 40; i++ {
		y := i%3 == 0
		p := 0.32
		if y {
			p = 0.35 // three points off the reference: real accuracy, no wager
		}
		timid = append(timid, over(p, 0.33, 0.55, 0.25, y))
	}
	if g := PairedBrierGain(timid); g <= 0 {
		t.Errorf("the timid forecaster should be more accurate, gain %.5f", g)
	}
	if e := RealisedEdge(timid, 0.0, hold); !math.IsNaN(e) {
		t.Errorf("the timid forecaster placed wagers it should not have: %.4f", e)
	}
	if n := OverBarCount(timid, 0.0, hold); n != 0 {
		t.Errorf("%d wagers from a forecaster that never clears the vig", n)
	}
}

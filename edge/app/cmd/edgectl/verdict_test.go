package main

import (
	"fmt"
	"testing"

	"edge/internal/calib"
)

// beatsBy builds a reference subgroup where the forecaster is more accurate than
// the reference by roughly `edge` on `n` game-clustered rows. A positive edge is
// a forecaster that beats this opponent; a negative one loses to it.
func beatsBy(mode string, binding bool, n int, edge float64) refSet {
	var pts []calib.Point
	base := 0.35
	for i := 0; i < n; i++ {
		y := i%3 == 0
		// The reference sits at the base rate; the forecaster leans the right way
		// by `edge` when it beats the opponent, the wrong way when it does not.
		p := base
		if y {
			p = base + edge
		} else {
			p = base - edge
		}
		pts = append(pts, calib.Point{P: p, Ref: base, HasRef: true, Y: y,
			Cluster: fmt.Sprintf("g%d", i/2)})
	}
	return refSet{name: mode, mode: mode, binding: binding, pts: pts}
}

// TestEvaluateE1BindsOnTheHardestOpponent is the C-B fix: a forecaster that beats
// the incumbent but loses to the line must FAIL E1, because the line binds. The
// old verdict scored one auto-picked reference and would have passed it.
func TestEvaluateE1BindsOnTheHardestOpponent(t *testing.T) {
	// Beats the incumbent handily, loses to the line.
	sets := []refSet{
		beatsBy("belief-json (incumbent)", true, 300, +0.20),
		beatsBy("line", true, 300, -0.20),
		beatsBy("base-rate", false, 300, +0.20), // floor: strong, must not rescue
	}
	ev := evaluateE1(sets)
	if !ev.decidable {
		t.Fatal("E1 should be decidable with two measurable opponents")
	}
	if ev.pass {
		t.Error("E1 passed while losing to the line — the hardest opponent did not bind")
	}
	if ev.name != "line" {
		t.Errorf("binding opponent was %q, want the line (the one it loses to)", ev.name)
	}

	// Beating every real opponent passes, and the floor is irrelevant either way.
	all := []refSet{
		beatsBy("market", true, 300, +0.15),
		beatsBy("line", true, 300, +0.15),
		beatsBy("base-rate", false, 300, -0.50), // a failing floor must NOT fail E1
	}
	if ev := evaluateE1(all); !ev.pass {
		t.Errorf("E1 failed while beating every real opponent (bound on %q, lo %+.4f)", ev.name, ev.lo)
	}

	// No binding opponent measurable → not decidable, not a pass.
	if ev := evaluateE1([]refSet{beatsBy("base-rate", false, 300, +0.2)}); ev.pass || ev.decidable {
		t.Error("E1 with only the floor present should be undecidable, not a pass")
	}
}

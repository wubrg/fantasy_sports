package main

import (
	"fmt"
	"testing"
	"time"

	"leaguehome/internal/draft"
)

// crowdedBoard is a target set built to collapse a value-greedy beam.
//
// One whale far above everyone, one genuinely competitive mid-priced anchor,
// and enough cheap filler that every slot in a top-N-by-value beam can be
// taken by a whale-containing line before any mid-anchored line is considered.
// Each player gets his own offense so one-per-offense never does the pruning
// for us.
func crowdedBoard() []draft.PlayerSignals {
	targets := []draft.PlayerSignals{
		costed("whale", "Whale Back", "RB", "WHL", 90, 50),
		costed("mid", "Mid Back", "RB", "MID", 60, 20),
	}
	pos := []string{"WR", "WR", "TE", "QB"}
	for i := 0; i < 64; i++ {
		targets = append(targets, costed(
			fmt.Sprintf("f%02d", i),
			fmt.Sprintf("Filler %02d", i),
			pos[i%len(pos)],
			fmt.Sprintf("F%02d", i),
			8, 2))
	}
	return targets
}

func anchorsOf(lines []arbBestFit) map[string]bool {
	got := map[string]bool{}
	for _, l := range lines {
		got[anchorOf(l.Picks)] = true
	}
	return got
}

// TestAlternativesOfferADifferentAnchor.
//
// The whole point of the runner-up list: "if not him, then who". A list of
// four whale-anchored lines differing by a $2 receiver answers nothing, and
// that is exactly what a value-greedy beam produces — every slot fills with a
// whale line before a mid-anchored one is ever weighed.
func TestAlternativesOfferADifferentAnchor(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 80)

	if got := anchorOf(lines.Best.Picks); got != "whale" {
		t.Fatalf("best line anchored by %q, want the whale", got)
	}
	if len(lines.Alternatives) == 0 {
		t.Fatal("no alternatives offered")
	}
	anchors := anchorsOf(lines.Alternatives)
	if anchors["whale"] {
		t.Error("an alternative repeats the winning anchor; they must differ")
	}
	if !anchors["mid"] {
		t.Errorf("alternatives anchored by %v, want a mid-anchored line", keysOf(anchors))
	}
}

// Each alternative is a distinct anchor, not a near-copy. Without this the
// list fills with the same line wearing different dollar-bin bodies.
func TestAlternativesDoNotRepeatAnAnchor(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 80)

	seen := map[string]bool{}
	for _, alt := range lines.Alternatives {
		a := anchorOf(alt.Picks)
		if seen[a] {
			t.Errorf("anchor %q appears twice in the alternatives", a)
		}
		seen[a] = true
	}
}

// The margin is against the winner, and the winner is the winner: no
// alternative may be worth more than the line it is an alternative to.
func TestAlternativesRankBelowTheWinner(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 80)

	for _, alt := range lines.Alternatives {
		if alt.Value > lines.Best.Value {
			t.Errorf("alternative worth $%d beats the best line at $%d", alt.Value, lines.Best.Value)
		}
	}
	for i := 1; i < len(lines.Alternatives); i++ {
		if lines.Alternatives[i].Value > lines.Alternatives[i-1].Value {
			t.Error("alternatives are not ordered by value")
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The winner must not get worse.
//
// Anchor-diverse pruning spends width keeping rivals alive that a flat beam
// spent deepening the leader, so the line the page leads with could quietly
// degrade in exchange for the runner-up list. It may improve; it may not
// regress. $198 is what the flat top-60 beam found on this fixture.
func TestBestLineIsNotWorseThanAFlatBeam(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 120)

	const flatBeamValue = 198
	if lines.Best.Value < flatBeamValue {
		t.Errorf("best line worth $%d, flat beam found $%d — the winner regressed",
			lines.Best.Value, flatBeamValue)
	}
}

// The solve stays inside a pick.
//
// Not a frame budget: the solve runs outside the board lock and only when the
// board changes, so it costs this page a slower first refresh after a pick and
// costs the board nothing. What it may not do is still be running when the
// next nomination lands, and this fixture is deliberately harsher than a real
// board, where one-per-offense prunes most pairings away.
func TestTheSolveStaysInsideAPick(t *testing.T) {
	srv := scratchServer(t)
	targets := crowdedBoard()

	start := time.Now()
	const runs = 5
	for i := 0; i < runs; i++ {
		srv.bestFitLines(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 120)
	}
	per := time.Since(start) / runs

	t.Logf("solve %v with %d targets", per, len(targets))
	if per > 500*time.Millisecond {
		t.Errorf("solve took %v, over the 500ms budget", per)
	}
}

// The per-dollar line stops where value stops paying for itself.
//
// It cannot come from the finished lines alone: adding a player always adds
// value, so the most valuable line is always the longest one the cap allows,
// and the most profitable line is usually one that stopped earlier. This is
// the whole reason the two objectives are reported separately.
func TestThePerDollarLineStopsWhereValueStopsPayingForItself(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("bargain", "Bargain Back", "RB", "BAR", 40, 10),
		costed("fair", "Fair Receiver", "WR", "FAI", 20, 8),
		costed("whale", "Overpriced Whale", "RB", "WHL", 60, 90),
	}

	lines := srv.bestFitLines(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 120)

	if lines.PerDollar == nil {
		t.Fatal("no per-dollar line where bargains exist")
	}
	for _, p := range lines.PerDollar.Picks {
		if p.Pick.PlayerID == "whale" {
			t.Error("per-dollar line bought a player priced $30 above his value")
		}
	}
	if lines.PerDollar.Surplus <= 0 {
		t.Errorf("per-dollar surplus $%d, want a line worth more than it costs", lines.PerDollar.Surplus)
	}
	if lines.PerDollar.Surplus < lines.Best.Surplus {
		t.Errorf("per-dollar surplus $%d below the value line's $%d",
			lines.PerDollar.Surplus, lines.Best.Surplus)
	}
}

// Where nothing is worth what it costs there is no per-dollar line, and the
// page must say so rather than quietly showing the value line under the
// heading of a question it does not answer.
func TestNoPerDollarLineWhenEveryTargetIsOverpriced(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Dear One", "RB", "DET", 10, 40),
		costed("2", "Dear Two", "WR", "CIN", 8, 30),
		costed("3", "Dear Three", "TE", "BUF", 5, 25),
	}

	lines := srv.bestFitLines(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 200)

	if lines.PerDollar != nil {
		t.Errorf("offered a per-dollar line worth $%d for $%d, where every target is overpriced",
			lines.PerDollar.Value, lines.PerDollar.Spend)
	}
	if len(lines.Best.Picks) == 0 {
		t.Error("no value line either; the cap affords these")
	}
}

// Surplus is reported on every line, not derived on the page.
func TestEveryLineCarriesItsSurplus(t *testing.T) {
	srv := scratchServer(t)
	lines := srv.bestFitLines(nil, crowdedBoard(), arbPrefs(), srv.scoringBaselines(), srv.static.shape, 80)

	check := func(what string, l arbBestFit) {
		if l.Surplus != l.Value-l.Spend {
			t.Errorf("%s surplus $%d, want $%d - $%d", what, l.Surplus, l.Value, l.Spend)
		}
	}
	check("best", lines.Best)
	for i, a := range lines.Alternatives {
		check(fmt.Sprintf("alternative %d", i), a)
	}
	if lines.PerDollar != nil {
		check("per-dollar", *lines.PerDollar)
	}
}

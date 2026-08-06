package draft

import (
	"fmt"
	"testing"
)

// sig builds a board player for roster tests.
func sig(id, name, pos string, points float64, cost int) PlayerSignals {
	return PlayerSignals{
		PlayerID: id, Name: name, Position: pos,
		CielyPoints: points, Cost: cost, Value: cost,
	}
}

func rosterShape() PoolState {
	p := HitOrMissPool()
	p.Dollars, p.Slots = 2400, 168
	return p
}

// scoringBaselines for tests: a full board so replacement lands somewhere
// sensible at each position.
func testBaselines() map[string]float64 {
	var all []Projection
	for _, spec := range []struct {
		pos  string
		n    int
		top  float64
		step float64
	}{
		{"QB", 40, 380, 5}, {"RB", 70, 330, 3.5},
		{"WR", 100, 310, 2.5}, {"TE", 40, 200, 3.5},
	} {
		for i := 0; i < spec.n; i++ {
			all = append(all, Projection{
				PlayerID: fmt.Sprintf("%s%d", spec.pos, i),
				Position: spec.pos, Points: spec.top - float64(i)*spec.step,
			})
		}
	}
	return ScoringBaselines(all, rosterShape())
}

func TestScoringBaselinesUseVOLSNotTheBoardsCurve(t *testing.T) {
	var all []Projection
	for i := 0; i < 70; i++ {
		all = append(all, Projection{
			PlayerID: fmt.Sprintf("RB%d", i), Position: "RB", Points: 330 - float64(i)*3.5,
		})
	}
	shape := rosterShape()

	// Whatever the board is set to, scoring uses VOLS.
	for _, b := range []Baseline{BaselineBEER, BaselineBEERPlus, BaselineVOLS} {
		s := shape
		s.Baseline = b
		got := ScoringBaselines(all, s)["RB"]

		vols := shape
		vols.Baseline = BaselineVOLS
		want := ReplacementLevels(all, vols)["RB"]

		if got != want {
			t.Errorf("board on %s scored against %.1f, want the VOLS %.1f", b, got, want)
		}
	}
}

// TestScoringBaselinesIgnoreWhatIsAlreadyDrafted — replacement level moves
// as players leave, so two rosters built at different points in a draft have
// to be judged against the same yardstick.
func TestScoringBaselinesIgnoreWhatIsAlreadyDrafted(t *testing.T) {
	var all []Projection
	for i := 0; i < 70; i++ {
		all = append(all, Projection{
			PlayerID: fmt.Sprintf("RB%d", i), Position: "RB", Points: 330 - float64(i)*3.5,
		})
	}
	early := rosterShape()
	late := rosterShape()
	late.Filled = map[string]int{"RB": 18}

	if ScoringBaselines(all, early)["RB"] != ScoringBaselines(all, late)["RB"] {
		t.Error("scoring baselines must not move with draft progress")
	}
}

// TestLineupUsesTwoBacksAndFlexesTheThird is the core lineup rule.
func TestLineupUsesTwoBacksAndFlexesTheThird(t *testing.T) {
	r := &Roster{}
	r.Add(sig("1", "RB A", "RB", 300, 60), 60)
	r.Add(sig("2", "RB B", "RB", 280, 50), 50)
	r.Add(sig("3", "RB C", "RB", 260, 40), 40)
	r.Add(sig("4", "RB D", "RB", 100, 5), 5)
	r.Add(sig("5", "QB A", "QB", 350, 20), 20)
	r.Add(sig("6", "WR A", "WR", 290, 55), 55)
	r.Add(sig("7", "WR B", "WR", 250, 35), 35)
	r.Add(sig("8", "WR C", "WR", 200, 20), 20)
	r.Add(sig("9", "TE A", "TE", 180, 15), 15)

	m := Score(r, testBaselines(), rosterShape())
	if !m.Filled() {
		t.Fatalf("lineup should be complete, missing %v", m.Unfilled)
	}

	slots := map[string]int{}
	for _, s := range r.Starters() {
		slots[s.Slot]++
	}
	if slots["RB"] != 2 {
		t.Errorf("RB slots filled = %d, want 2", slots["RB"])
	}
	if slots["FLEX"] != 1 {
		t.Errorf("FLEX slots filled = %d, want 1", slots["FLEX"])
	}
	// The third-best back flexes; the fourth sits.
	for _, s := range r.Players {
		if s.Player.Name == "RB C" && s.Slot != "FLEX" {
			t.Errorf("RB C should take the flex, got %q", s.Slot)
		}
		if s.Player.Name == "RB D" && s.Starting {
			t.Error("RB D should be on the bench")
		}
	}
}

// TestLineupReportsHolesRatherThanFieldingAShortRoster
func TestLineupReportsHolesRatherThanFieldingAShortRoster(t *testing.T) {
	r := &Roster{}
	r.Add(sig("1", "QB A", "QB", 350, 20), 20)
	r.Add(sig("2", "RB A", "RB", 300, 60), 60)

	m := Score(r, testBaselines(), rosterShape())
	if m.Filled() {
		t.Fatal("a two-player roster cannot field a lineup")
	}
	// One RB short, three WRs, a TE and the flex.
	want := map[string]int{"RB": 1, "WR": 3, "TE": 1, "FLEX": 1}
	got := map[string]int{}
	for _, u := range m.Unfilled {
		got[u]++
	}
	for slot, n := range want {
		if got[slot] != n {
			t.Errorf("unfilled %s = %d, want %d (all: %v)", slot, got[slot], n, m.Unfilled)
		}
	}
}

// TestPOPRIsIndependentOfBuildOrder — the same roster assembled differently
// must score identically, or shapes cannot be compared.
func TestPOPRIsIndependentOfBuildOrder(t *testing.T) {
	players := []PlayerSignals{
		sig("1", "RB A", "RB", 300, 60), sig("2", "RB B", "RB", 280, 50),
		sig("3", "QB A", "QB", 350, 20), sig("4", "WR A", "WR", 290, 55),
		sig("5", "WR B", "WR", 250, 35), sig("6", "WR C", "WR", 200, 20),
		sig("7", "TE A", "TE", 180, 15), sig("8", "RB C", "RB", 260, 40),
	}
	baselines, shape := testBaselines(), rosterShape()

	forward := &Roster{}
	for _, p := range players {
		forward.Add(p, p.Cost)
	}
	backward := &Roster{}
	for i := len(players) - 1; i >= 0; i-- {
		backward.Add(players[i], players[i].Cost)
	}

	a := Score(forward, baselines, shape)
	b := Score(backward, baselines, shape)
	if a.POPR != b.POPR {
		t.Errorf("POPR depends on build order: %.1f vs %.1f", a.POPR, b.POPR)
	}
	if a.StartingPoints != b.StartingPoints {
		t.Errorf("starting points depend on build order: %.1f vs %.1f", a.StartingPoints, b.StartingPoints)
	}
}

// TestPOPRNeutralizesThePositionalSkew is the reason POPR was chosen over
// raw points. Ciely prices 29 tight ends above the floor against 12 starting
// spots, so a TE-heavy roster flatters itself on raw points; scoring against
// positional replacement cancels most of that.
func TestPOPRNeutralizesThePositionalSkew(t *testing.T) {
	baselines, shape := testBaselines(), rosterShape()

	// Two rosters of identical cost. One loads a position whose baseline
	// is low (TE), the other one whose baseline is high (WR).
	teHeavy := &Roster{}
	teHeavy.Add(sig("q", "QB", "QB", 350, 20), 20)
	teHeavy.Add(sig("r1", "RB1", "RB", 250, 30), 30)
	teHeavy.Add(sig("r2", "RB2", "RB", 240, 25), 25)
	teHeavy.Add(sig("w1", "WR1", "WR", 220, 25), 25)
	teHeavy.Add(sig("w2", "WR2", "WR", 200, 20), 20)
	teHeavy.Add(sig("w3", "WR3", "WR", 180, 15), 15)
	teHeavy.Add(sig("t1", "TE1", "TE", 195, 40), 40)
	teHeavy.Add(sig("t2", "TE2", "TE", 185, 35), 35)

	wrHeavy := &Roster{}
	wrHeavy.Add(sig("q", "QB", "QB", 350, 20), 20)
	wrHeavy.Add(sig("r1", "RB1", "RB", 250, 30), 30)
	wrHeavy.Add(sig("r2", "RB2", "RB", 240, 25), 25)
	wrHeavy.Add(sig("w1", "WR1", "WR", 300, 40), 40)
	wrHeavy.Add(sig("w2", "WR2", "WR", 290, 35), 35)
	wrHeavy.Add(sig("w3", "WR3", "WR", 280, 25), 25)
	wrHeavy.Add(sig("w4", "WR4", "WR", 270, 20), 20)
	wrHeavy.Add(sig("t1", "TE1", "TE", 150, 15), 15)

	te := Score(teHeavy, baselines, shape)
	wr := Score(wrHeavy, baselines, shape)

	if te.Spend != wr.Spend {
		t.Fatalf("fixture broken: costs differ ($%d vs $%d)", te.Spend, wr.Spend)
	}
	// Raw points make the receiver build look far better because receivers
	// simply score more; POPR closes most of that gap by measuring each
	// against his own position's replacement.
	rawGap := wr.StartingPoints - te.StartingPoints
	poprGap := wr.POPR - te.POPR
	if rawGap <= 0 {
		t.Fatalf("fixture broken: expected the WR build to lead on raw points, got %.1f", rawGap)
	}
	if poprGap >= rawGap {
		t.Errorf("POPR should narrow the positional gap: raw %.1f, POPR %.1f", rawGap, poprGap)
	}
}

func TestBenchIsScoredSeparately(t *testing.T) {
	r := &Roster{}
	r.Add(sig("1", "QB A", "QB", 350, 20), 20)
	r.Add(sig("2", "QB B", "QB", 300, 5), 5) // cannot start, only one QB slot

	m := Score(r, testBaselines(), rosterShape())
	if m.StartingPoints != 350 {
		t.Errorf("starting points = %.1f, want 350", m.StartingPoints)
	}
	if m.BenchPoints != 300 {
		t.Errorf("bench points = %.1f, want 300 — reported, not folded in", m.BenchPoints)
	}
}

func TestMetricsCountSoftSignals(t *testing.T) {
	must := sig("1", "My Guy", "RB", 300, 60)
	must.Lean = PlayerLean{Player: "My Guy", Lean: LeanMust}
	up := sig("2", "Liked", "WR", 280, 40)
	up.Lean = PlayerLean{Player: "Liked", Lean: LeanUp}
	banned := sig("3", "Banned", "TE", 200, 20)
	banned.Lean = PlayerLean{Player: "Banned", Lean: LeanDND}
	split := sig("4", "Contested", "WR", 250, 30)
	split.ECR = ECRContested
	hurt := sig("5", "Hurt", "WR", 240, 25)
	hurt.Availability = "PUP"

	r := &Roster{}
	for _, p := range []PlayerSignals{must, up, banned, split, hurt} {
		r.Add(p, p.Cost)
	}
	m := Score(r, testBaselines(), rosterShape())

	if m.MyGuys != 2 {
		t.Errorf("my guys = %d, want 2 (must + up)", m.MyGuys)
	}
	if m.DNDViolations != 1 {
		t.Errorf("dnd violations = %d, want 1 — a shape must never hide one", m.DNDViolations)
	}
	if m.Contested != 1 || m.Injured != 1 {
		t.Errorf("contested=%d injured=%d, want 1/1", m.Contested, m.Injured)
	}
	if m.SpendPosition["WR"] != 95 {
		t.Errorf("WR spend = %d, want 95", m.SpendPosition["WR"])
	}
}

func TestRosterAddRemoveAndSpend(t *testing.T) {
	r := &Roster{}
	r.Add(sig("1", "A", "RB", 300, 60), 60)
	r.Add(sig("2", "B", "WR", 280, 40), 40)
	if r.Spend() != 100 {
		t.Errorf("spend = %d, want 100", r.Spend())
	}
	if !r.Has("1") {
		t.Error("expected to hold player 1")
	}
	if !r.Remove("1") || r.Has("1") {
		t.Error("remove failed")
	}
	if r.Spend() != 40 {
		t.Errorf("spend after remove = %d, want 40", r.Spend())
	}
	if r.Remove("nobody") {
		t.Error("removing an absent player should report false")
	}
}

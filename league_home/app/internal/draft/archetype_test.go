package draft

import (
	"fmt"
	"strings"
	"testing"
)

// marketPool builds a board with a realistic shape: a steep top and a long
// cheap tail at every position.
func marketPool() []PlayerSignals {
	var out []PlayerSignals
	specs := []struct {
		pos     string
		n       int
		topPts  float64
		stepPts float64
		topCost int
	}{
		{"QB", 30, 380, 5, 35},
		{"RB", 60, 330, 4, 70},
		{"WR", 80, 310, 3, 65},
		{"TE", 30, 200, 4, 35},
	}
	for _, s := range specs {
		for i := 0; i < s.n; i++ {
			cost := s.topCost - i*3
			if cost < 1 {
				cost = 1
			}
			out = append(out, sig(
				fmt.Sprintf("%s%d", s.pos, i),
				fmt.Sprintf("%s %d", s.pos, i+1),
				s.pos, s.topPts-float64(i)*s.stepPts, cost))
		}
	}
	return out
}

func fillOpts() FillOptions {
	return FillOptions{
		Budget: 200, Slots: 13, Price: BoardPrice,
		Shape: rosterShape(), Baselines: testBaselines(),
	}
}

func TestFillRespectsBudgetAndFillsTheLineup(t *testing.T) {
	for _, a := range Archetypes() {
		got := Fill(a, marketPool(), fillOpts())
		if got.Metrics.Spend > 200 {
			t.Errorf("%s spent $%d, over budget", a.Name, got.Metrics.Spend)
		}
		if len(got.Roster.Players) > 13 {
			t.Errorf("%s took %d players, over the slot count", a.Name, len(got.Roster.Players))
		}
		if !got.Metrics.Filled() {
			t.Errorf("%s left starting slots empty: %v", a.Name, got.Metrics.Unfilled)
		}
		// Every remaining spot needs a dollar, so a fill may never leave
		// the roster unaffordable.
		if got.Leftover < 0 {
			t.Errorf("%s overspent: $%d left", a.Name, got.Leftover)
		}
	}
}

// TestArchetypeConstraintsHold — each shape must actually be the shape.
func TestArchetypeConstraintsHold(t *testing.T) {
	byName := map[string]Shape{}
	for _, s := range CompareShapes(marketPool(), fillOpts()) {
		byName[s.Archetype.Name] = s
	}

	zero := byName["Zero RB"]
	for _, p := range zero.Roster.Players {
		if p.Player.Position == "RB" && p.Price > 12 {
			t.Errorf("Zero RB bought %s at $%d", p.Player.Name, p.Price)
		}
	}

	balanced := byName["Balanced"]
	for _, p := range balanced.Roster.Players {
		if p.Price > 35 {
			t.Errorf("Balanced bought %s at $%d", p.Player.Name, p.Price)
		}
	}

	hero := byName["Hero RB"]
	over50 := 0
	for _, p := range hero.Roster.Players {
		if p.Player.Position != "RB" {
			continue
		}
		if p.Price > 50 {
			over50++
		} else if p.Price > 15 {
			t.Errorf("Hero RB bought a second-tier back: %s at $%d", p.Player.Name, p.Price)
		}
	}
	if hero.Achieved && over50 != 1 {
		t.Errorf("Hero RB claims success with %d backs over $50", over50)
	}

	stars := byName["Stars & Scrubs"]
	elite := 0
	for _, p := range stars.Roster.Players {
		if p.Price > 40 {
			elite++
		} else if p.Price > 5 {
			t.Errorf("Stars & Scrubs bought a middling player: %s at $%d", p.Player.Name, p.Price)
		}
	}
	if elite > 3 {
		t.Errorf("Stars & Scrubs took %d elite players, cap is 3", elite)
	}
}

// TestFillNeverBuysADoNotDraft — dnd is absolute, whatever the value.
func TestFillNeverBuysADoNotDraft(t *testing.T) {
	pool := marketPool()
	// Ban the single best value on the board.
	for i := range pool {
		if pool[i].Name == "RB 1" {
			pool[i].Lean = PlayerLean{Player: pool[i].Name, Lean: LeanDND}
		}
	}
	for _, a := range Archetypes() {
		got := Fill(a, pool, fillOpts())
		for _, p := range got.Roster.Players {
			if p.Player.Name == "RB 1" {
				t.Errorf("%s bought a do-not-draft player", a.Name)
			}
		}
		if got.Metrics.DNDViolations != 0 {
			t.Errorf("%s reports %d dnd violations", a.Name, got.Metrics.DNDViolations)
		}
	}
}

// TestFillPrefersYourGuysOnATie — a shape should reflect your reads rather
// than fight them.
func TestFillPrefersYourGuysOnATie(t *testing.T) {
	// Two identical receivers; one is a must-have.
	a := sig("x1", "Twin A", "WR", 250, 20)
	b := sig("x2", "Twin B", "WR", 250, 20)
	b.Lean = PlayerLean{Player: "Twin B", Lean: LeanMust}

	pool := append(marketPool(), a, b)
	shape := Fill(Archetypes()[1], pool, fillOpts()) // Balanced

	sawA, sawB := false, false
	for _, p := range shape.Roster.Players {
		switch p.Player.Name {
		case "Twin A":
			sawA = true
		case "Twin B":
			sawB = true
		}
	}
	if sawA && !sawB {
		t.Error("between identical players the one you want should win")
	}
}

// TestUnachievableShapeSaysSo — a board that cannot supply a shape must
// report that rather than returning something else under the same name.
func TestUnachievableShapeSaysSo(t *testing.T) {
	// Strip every back worth over $25, so Robust RB cannot be built.
	var thin []PlayerSignals
	for _, p := range marketPool() {
		if p.Position == "RB" && p.Cost > 25 {
			continue
		}
		thin = append(thin, p)
	}
	var robust Archetype
	for _, a := range Archetypes() {
		if a.Name == "Robust RB" {
			robust = a
		}
	}
	got := Fill(robust, thin, fillOpts())
	if got.Achieved {
		t.Error("Robust RB cannot be achieved without backs over $25, but claims it was")
	}
	if got.Summary() == "" {
		t.Error("expected a summary explaining the outcome")
	}
}

// TestPriceFuncIsASeam — the later noise modes must drop in without
// touching the fill logic.
func TestPriceFuncIsASeam(t *testing.T) {
	opts := fillOpts()
	cheap := Fill(Archetypes()[1], marketPool(), opts)

	opts.Price = func(p PlayerSignals) int {
		// An "overpay" mode: everything costs half again as much.
		c := BoardPrice(p) * 3 / 2
		if c < 1 {
			c = 1
		}
		return c
	}
	dear := Fill(Archetypes()[1], marketPool(), opts)

	if dear.Metrics.POPR >= cheap.Metrics.POPR {
		t.Errorf("paying more for the same board should buy less: %.1f vs %.1f",
			dear.Metrics.POPR, cheap.Metrics.POPR)
	}
}

func TestCompareShapesRanksByPOPR(t *testing.T) {
	got := CompareShapes(marketPool(), fillOpts())
	if len(got) != len(Archetypes()) {
		t.Fatalf("got %d shapes, want %d", len(got), len(Archetypes()))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Metrics.POPR < got[i].Metrics.POPR {
			t.Errorf("shapes out of order at %d: %.1f then %.1f",
				i, got[i-1].Metrics.POPR, got[i].Metrics.POPR)
		}
	}
}

// TestFillSpendsTheBudget guards the flaw that a single value-per-dollar
// pass has: a $1 player barely above replacement scores enormously on that
// measure, so one greedy pass bought thirteen of them, left three quarters
// of the budget unspent, and still failed to cover the lineup.
func TestFillSpendsTheBudget(t *testing.T) {
	for _, s := range CompareShapes(marketPool(), fillOpts()) {
		if s.Metrics.Spend < 150 {
			t.Errorf("%s spent only $%d of $200 — the fill is hoarding",
				s.Archetype.Name, s.Metrics.Spend)
		}
		if s.Leftover < 0 {
			t.Errorf("%s overspent by $%d", s.Archetype.Name, -s.Leftover)
		}
	}
}

// TestAnchorsPursueAShapeRatherThanPermittingIt — a per-pick veto can only
// forbid, and upgrading by value per dollar never makes a large jump, so a
// shape built on concentration has to buy its stars outright. Without this
// Hero RB covered the position with a $13 back and called itself
// unachievable while the budget sat unspent.
func TestAnchorsPursueAShapeRatherThanPermittingIt(t *testing.T) {
	var robust Archetype
	for _, a := range Archetypes() {
		if a.Name == "Robust RB" {
			robust = a
		}
	}
	if len(robust.Anchors) != 1 || robust.Anchors[0].Count != 3 {
		t.Fatalf("Robust RB should anchor three backs, got %+v", robust.Anchors)
	}

	got := Fill(robust, marketPool(), fillOpts())
	real := 0
	for _, p := range got.Roster.Players {
		if p.Player.Position == "RB" && p.Price > 25 {
			real++
		}
	}
	if real < 3 {
		t.Errorf("Robust RB ended with %d backs over $25, want 3", real)
	}
	if !got.Achieved {
		t.Error("Robust RB should be achievable from this board")
	}
}

// TestShapesDifferFromEachOther — five archetypes that all produce the same
// roster would be a comparison of nothing.
func TestShapesDifferFromEachOther(t *testing.T) {
	shapes := CompareShapes(marketPool(), fillOpts())
	rbSpend := map[string]int{}
	for _, s := range shapes {
		rbSpend[s.Archetype.Name] = s.Metrics.SpendPosition["RB"]
	}
	if rbSpend["Zero RB"] >= rbSpend["Robust RB"] {
		t.Errorf("Zero RB spent $%d at the position and Robust RB $%d",
			rbSpend["Zero RB"], rbSpend["Robust RB"])
	}
	if rbSpend["Zero RB"] > 12*3 {
		t.Errorf("Zero RB put $%d into backs", rbSpend["Zero RB"])
	}
	// Stars & Scrubs should concentrate: a few big buys, the rest at the floor.
	var stars Shape
	for _, s := range shapes {
		if s.Archetype.Name == "Stars & Scrubs" {
			stars = s
		}
	}
	cheap := 0
	for _, p := range stars.Roster.Players {
		if p.Price <= 5 {
			cheap++
		}
	}
	if cheap < len(stars.Roster.Players)/2 {
		t.Errorf("Stars & Scrubs should be mostly dollar players, got %d of %d",
			cheap, len(stars.Roster.Players))
	}
}

// TestPossibleSeparatesTheBoardFromTheHeuristic — "not achieved" has two
// very different causes, and reporting the heuristic's limits as the board's
// would blame the wrong thing.
func TestPossibleSeparatesTheBoardFromTheHeuristic(t *testing.T) {
	var robust Archetype
	for _, a := range Archetypes() {
		if a.Name == "Robust RB" {
			robust = a
		}
	}

	// A board with plenty of $26+ backs can supply the shape.
	rich := Fill(robust, marketPool(), fillOpts())
	if !rich.Possible {
		t.Error("a board full of expensive backs can supply Robust RB")
	}

	// Strip them out and it genuinely cannot.
	var thin []PlayerSignals
	for _, p := range marketPool() {
		if p.Position == "RB" && p.Cost > 25 {
			continue
		}
		thin = append(thin, p)
	}
	poor := Fill(robust, thin, fillOpts())
	if poor.Possible {
		t.Error("without backs over $25 the shape is not possible")
	}
	if poor.Achieved {
		t.Error("and it certainly was not achieved")
	}
	if !strings.Contains(poor.Summary(), "cannot supply") {
		t.Errorf("summary should blame the board: %q", poor.Summary())
	}

	// A budget too small for three anchors is also the board's problem.
	broke := fillOpts()
	broke.Budget = 40
	if Fill(robust, marketPool(), broke).Possible {
		t.Error("$40 cannot buy three backs over $25")
	}
}

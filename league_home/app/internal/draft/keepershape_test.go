package draft

import "testing"

// heldBack is a keeper priced where it changes what shapes are reachable:
// over Hero RB's second-back cap of $20, under its hero line of $40.
func heldBack(price int) RosterSpot {
	return RosterSpot{
		Player: PlayerSignals{PlayerID: "keeper-rb", Name: "Kept Back", Position: "RB", CielyPoints: 280},
		Price:  price,
	}
}

func heldWR(price int) RosterSpot {
	return RosterSpot{
		Player: PlayerSignals{PlayerID: "keeper-wr", Name: "Kept Wideout", Position: "WR", CielyPoints: 270},
		Price:  price,
	}
}

func withHeld(held ...RosterSpot) FillOptions {
	opts := fillOpts()
	opts.Held = held
	// Budget is what is left after the keepers are charged; slots are the
	// whole roster, keepers included.
	for _, h := range held {
		opts.Budget -= h.Price
	}
	return opts
}

func shapeNamed(name string) Archetype {
	for _, a := range Archetypes() {
		if a.Name == name {
			return a
		}
	}
	panic("no archetype " + name)
}

// TestHeldPlayersCountAgainstTheShape is the whole point.
//
// A shape is a claim about the finished fourteen. Filling the other twelve
// in isolation had Zero RB pass at $27 on backs while the keeper made it
// $62, and had Robust RB report failure at $62 when the keeper had already
// carried it past the line.
func TestHeldPlayersCountAgainstTheShape(t *testing.T) {
	zero := shapeNamed("Zero RB")
	got := Fill(zero, marketPool(), withHeld(heldBack(35)))

	rbSpend := 0
	held := false
	for _, p := range got.Roster.Players {
		if p.Player.Position == "RB" {
			rbSpend += p.Price
		}
		if p.Player.PlayerID == "keeper-rb" {
			held = true
		}
	}
	if !held {
		t.Fatal("the keeper is not on the roster the shape was measured against")
	}
	if rbSpend > 61 {
		t.Errorf("RB spend $%d includes the keeper and breaks the $61 ceiling", rbSpend)
	}
	if rbSpend <= 35 {
		t.Errorf("RB spend $%d does not even cover the keeper", rbSpend)
	}
}

// TestKeeperRulingOutAShapeIsNamed — Hero RB wants exactly one back over
// $40 and no other over $20. A $35 keeper is over $20 and under $40, so the
// shape needs a hero who would then be a second back over $20. There is no
// way out, and saying "the fill did not get there" would send you chasing it.
func TestKeeperRulingOutAShapeIsNamed(t *testing.T) {
	opts := withHeld(heldBack(35))
	shapes := CompareShapes(marketPool(), opts)

	var hero Shape
	for _, s := range shapes {
		if s.Archetype.Name == "Hero RB" {
			hero = s
		}
	}
	if hero.Achieved {
		t.Fatal("Hero RB cannot be achieved while holding a $35 back")
	}
	if hero.BlockedBy != "Kept Back" {
		t.Errorf("BlockedBy = %q, want the keeper responsible", hero.BlockedBy)
	}
}

// TestAKeeperIsNotBlamedForAShapeItDoesNotBlock is the regression.
//
// The first cut replayed keepers through each shape's per-pick veto, but a
// veto is a fill heuristic rather than the shape's definition: Stars &
// Scrubs refuses every mid-priced buy, so a $35 keeper read as blocking it
// while two stars were still perfectly affordable.
func TestAKeeperIsNotBlamedForAShapeItDoesNotBlock(t *testing.T) {
	opts := withHeld(heldWR(35))
	for _, s := range CompareShapes(marketPool(), opts) {
		if s.Archetype.Name != "Stars & Scrubs" {
			continue
		}
		if s.BlockedBy != "" {
			t.Errorf("Stars & Scrubs blamed on %q, but a $35 keeper leaves the budget for stars",
				s.BlockedBy)
		}
		if !s.Achieved {
			t.Error("two stars are still affordable around a $35 keeper")
		}
	}
}

// TestHeldPlayerSatisfiesAnAnchor — a kept back over the anchor price is one
// the auction does not have to supply, and buying a second would waste the
// slot the keeper already fills.
func TestHeldPlayerSatisfiesAnAnchor(t *testing.T) {
	robust := shapeNamed("Robust RB")
	opts := withHeld(heldBack(40))
	got := Fill(robust, marketPool(), opts)

	backs := 0
	for _, p := range got.Roster.Players {
		if p.Player.Position == "RB" {
			backs++
		}
	}
	if !got.Achieved {
		t.Errorf("Robust RB should be reachable with a $40 back already held: %+v", got.Metrics)
	}
	// Three anchors, one already held, so at most two more were bought.
	if backs > 6 {
		t.Errorf("bought %d backs; the held anchor should have reduced the buying", backs)
	}
}

// TestHeldPlayersAreNotChargedTwice — the keeper's price is already out of
// the budget, and his slot is already filled.
func TestHeldPlayersAreNotChargedTwice(t *testing.T) {
	opts := withHeld(heldBack(35), heldWR(35))
	if opts.Budget != 130 {
		t.Fatalf("fixture budget $%d, want $130 after two $35 keepers", opts.Budget)
	}
	for _, a := range Archetypes() {
		got := Fill(a, marketPool(), opts)
		bought := got.Metrics.Spend - 70
		if bought > opts.Budget {
			t.Errorf("%s bought $%d of players with $%d to spend", a.Name, bought, opts.Budget)
		}
		if len(got.Roster.Players) > opts.Slots {
			t.Errorf("%s took %d players into %d slots", a.Name, len(got.Roster.Players), opts.Slots)
		}
		held := 0
		for _, p := range got.Roster.Players {
			if p.Player.PlayerID == "keeper-rb" || p.Player.PlayerID == "keeper-wr" {
				held++
			}
		}
		if held != 2 {
			t.Errorf("%s carries %d of the 2 keepers", a.Name, held)
		}
	}
}

// TestNoKeepersBehavesAsBefore — the generic view, with no owner given.
func TestNoKeepersBehavesAsBefore(t *testing.T) {
	for _, s := range CompareShapes(marketPool(), fillOpts()) {
		if s.BlockedBy != "" {
			t.Errorf("%s blamed on %q with no keepers held", s.Archetype.Name, s.BlockedBy)
		}
	}
}

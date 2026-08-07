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

// heldFloorWR is a keeper who is a floor player: the case the shapes made of
// player types are about. Points high enough that he makes the lineup, since
// a trait on a bench player changes nothing about the season.
func heldFloorWR(price int) RosterSpot {
	return RosterSpot{
		Player: PlayerSignals{
			PlayerID: "keeper-wr", Name: "Kept Wideout", Position: "WR",
			CielyPoints: 270, Traits: TraitSet{TraitFloor},
		},
		Price: price,
	}
}

// floorMarket is the market board with TraitFloor stamped on the named
// players — one per starting slot a floor lineup would fill, so the shape is
// reachable through the ordinary fill rather than only in principle.
func floorMarket(ids ...string) []PlayerSignals {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	pool := marketPool()
	found := 0
	for i := range pool {
		if want[pool[i].PlayerID] {
			pool[i].Traits = TraitSet{TraitFloor}
			found++
		}
	}
	if found != len(ids) {
		panic("floorMarket: named a player the market pool does not have")
	}
	return pool
}

func traitShapeNamed(name string) Archetype {
	for _, a := range TraitArchetypes() {
		if a.Name == name {
			return a
		}
	}
	panic("no trait archetype " + name)
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

// TestHeldTraitCarrierSatisfiesATraitAnchor — the same economy on the trait
// side. A kept floor player is one the auction does not have to supply, and
// buying a fifth would spend a slot on a requirement already met.
func TestHeldTraitCarrierSatisfiesATraitAnchor(t *testing.T) {
	floor := traitShapeNamed("Floor Build")
	pool := floorMarket("RB12", "RB13", "WR12", "TE6", "WR14")
	got := Fill(floor, pool, withHeld(heldFloorWR(35)))

	bought := 0
	for _, p := range got.Roster.Players {
		if p.Player.PlayerID != "keeper-wr" && p.Player.Traits.Has(TraitFloor) {
			bought++
		}
	}
	if !got.Achieved {
		t.Errorf("Floor Build should be reachable with a floor player already held: %+v", got.Metrics)
	}
	// Five wanted, one already held, so the anchors should have gone after
	// four rather than the full five.
	if bought > 4 {
		t.Errorf("bought %d floor players; the held one should have reduced the buying", bought)
	}
}

// TestAKeepersTraitsReachTheShape is the regression that shipped.
//
// A shape made of player types asks what kind of players are in the lineup.
// A keeper arrived at the fill with his traits stripped, so he occupied the
// slot and the money while measuring as a blank: a fourteen-man roster was
// graded as though two of its starters were nobody in particular, and every
// trait shape read one or two short of what the roster actually was.
//
// Four floor players are on the board and the shape wants five, so the fill
// can only get there if the keeper's own trait is counted.
func TestAKeepersTraitsReachTheShape(t *testing.T) {
	floor := traitShapeNamed("Floor Build")
	pool := floorMarket("RB12", "RB13", "WR12", "TE6")
	got := Fill(floor, pool, withHeld(heldFloorWR(35)))

	keeper := false
	for _, p := range got.Roster.Players {
		if p.Player.PlayerID != "keeper-wr" {
			continue
		}
		keeper = true
		if !p.Starting {
			t.Fatal("the keeper did not make the lineup, so this proves nothing about traits")
		}
		if !p.Player.Traits.Has(TraitFloor) {
			t.Error("the keeper reached the roster without the trait he was held for")
		}
	}
	if !keeper {
		t.Fatal("the keeper is not on the roster the shape was measured against")
	}
	if n := startersWith(got.Roster, TraitFloor); n != 5 {
		t.Errorf("%d floor starters, want 5: four bought and the keeper", n)
	}
	if !got.Achieved {
		t.Errorf("Floor Build is reachable when the kept floor player counts: %+v", got.Metrics)
	}

	// The control: the same keeper with nothing on him is the bug. If this
	// still passes, the assertions above are measuring something else.
	blank := heldFloorWR(35)
	blank.Player.Traits = nil
	if Fill(floor, pool, withHeld(blank)).Achieved {
		t.Error("Floor Build came out reachable with a traitless keeper, so the trait was never what counted")
	}
}

// TestAKeeperIsNotBlamedForTheShapeHeIsMadeFor is the false accusation.
//
// Naming a keeper as the reason a shape is out of reach is a strong claim —
// it tells you to stop chasing the shape. The first counterfactual refunded
// the keeper's price and left him on the board, which asks "would this work
// if I rebought him at market" rather than "would this work without him".
// With the board's best floor player kept dear, the answer was yes, and the
// report accused a floor player of ruling out the floor build.
func TestAKeeperIsNotBlamedForTheShapeHeIsMadeFor(t *testing.T) {
	// Expensive carriers, so the shape fails on money rather than supply.
	pool := floorMarket("RB8", "RB9", "WR8", "TE2")
	// The keeper is on the board too, and cheaper there than he was kept —
	// exactly the refund-and-rebuy the old counterfactual walked into.
	pool = append(pool, PlayerSignals{
		PlayerID: "keeper-wr", Name: "Kept Wideout", Position: "WR",
		CielyPoints: 270, Cost: 25, Value: 25, Traits: TraitSet{TraitFloor},
	})
	opts := withHeld(heldFloorWR(60))

	var floor Shape
	for _, s := range CompareTraitShapes(pool, opts) {
		if s.Archetype.Name == "Floor Build" {
			floor = s
		}
	}
	if floor.Achieved {
		t.Fatalf("fixture no longer fails: Floor Build was reached, so there is no blame to test (%+v)",
			floor.Metrics)
	}
	if floor.BlockedBy != "" {
		t.Errorf("Floor Build blamed on %q, who is himself a floor player and the best one held",
			floor.BlockedBy)
	}
}

// TestHeldPlayersAreNotChargedTwice — the keeper's price is already out of
// the budget, and his slot is already filled.
//
// Both families, because the shapes made of player types take the same
// keepers through the same fill and have their own way of counting them.
func TestHeldPlayersAreNotChargedTwice(t *testing.T) {
	opts := withHeld(heldBack(35), heldFloorWR(35))
	if opts.Budget != 130 {
		t.Fatalf("fixture budget $%d, want $130 after two $35 keepers", opts.Budget)
	}
	for _, a := range append(Archetypes(), TraitArchetypes()...) {
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

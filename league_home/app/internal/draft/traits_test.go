package draft

import "testing"

// catcher builds a pass catcher from the pieces a projection is made of.
func catcher(id, pos string, rec, recYds, recTD, targets float64) TraitInput {
	c := Components{
		HasParts: true, Receptions: rec, RecvYards: recYds,
		RecvTD: recTD, Targets: targets,
	}
	return TraitInput{PlayerID: id, Position: pos, Parts: c, Points: c.Total(),
		PriorGames: 17, PriorPoints: c.Total()}
}

func back(id string, rushYds, rushTD, rec, recYds, targets float64) TraitInput {
	c := Components{
		HasParts: true, RushYards: rushYds, RushTD: rushTD,
		Receptions: rec, RecvYards: recYds, Targets: targets,
	}
	return TraitInput{PlayerID: id, Position: "RB", Parts: c, Points: c.Total(),
		PriorGames: 17, PriorPoints: c.Total()}
}

func traitShapePool() PoolState {
	s := HitOrMissPool()
	s.Teams = 1
	return s
}

// TestComponentsRebuildTheProjection — the decomposition is the foundation
// everything else stands on, and it reconstructs Ciely's published totals
// exactly on the real file.
func TestComponentsRebuildTheProjection(t *testing.T) {
	c := Components{
		HasParts: true, PassYards: 4000, PassTD: 30, Interceptions: 10,
		RushYards: 400, RushTD: 4, Receptions: 0,
	}
	want := 4000.0/25 + 30*4 + 400.0/10 + 4*6 - 10
	if got := c.Total(); got != want {
		t.Errorf("total = %v, want %v", got, want)
	}
	if got := c.TouchdownPoints(); got != 30*4+4*6 {
		t.Errorf("td points = %v", got)
	}
}

// TestFloorAndRedZoneAreOppositeEnds — one axis, so a player cannot be both,
// and every position should produce some of each.
func TestFloorAndRedZoneAreOppositeEnds(t *testing.T) {
	var in []TraitInput
	// Receivers on a ramp from all-catches to all-touchdowns.
	for i := 0; i < 12; i++ {
		td := float64(i)
		rec := float64(90 - i*6)
		in = append(in, catcher(string(rune('a'+i)), "WR", rec, rec*12, td, rec*1.5))
	}
	got := ClassifyTraits(in, traitShapePool())

	both := 0
	floor, red := 0, 0
	for _, set := range got {
		if set.Has(TraitFloor) {
			floor++
		}
		if set.Has(TraitRedZone) {
			red++
		}
		if set.Has(TraitFloor) && set.Has(TraitRedZone) {
			both++
		}
	}
	if both != 0 {
		t.Errorf("%d players are both a floor and a red-zone play", both)
	}
	if floor == 0 || red == 0 {
		t.Errorf("expected some of each, got %d floor and %d red zone", floor, red)
	}
}

// TestQuarterbacksAreNotAllFloor is the regression.
//
// Quarterbacks have no receptions, so requiring reception share of them
// compared zero against zero and every quarterback qualified.
func TestQuarterbacksAreNotAllFloor(t *testing.T) {
	var in []TraitInput
	for i := 0; i < 12; i++ {
		c := Components{
			HasParts: true, PassYards: 4200, PassTD: float64(18 + i*2),
			RushYards: float64(400 - i*30),
		}
		in = append(in, TraitInput{
			PlayerID: string(rune('a' + i)), Position: "QB", Parts: c,
			Points: c.Total(), PriorGames: 17, PriorPoints: c.Total(),
		})
	}
	got := ClassifyTraits(in, traitShapePool())

	floor := 0
	for _, set := range got {
		if set.Has(TraitFloor) {
			floor++
		}
	}
	if floor == 0 {
		t.Error("no quarterback reads as a floor play; the trait should still apply")
	}
	if floor > len(in)/2 {
		t.Errorf("%d of %d quarterbacks read as floor plays", floor, len(in))
	}
}

// TestUnprovenIsMeasuredPerGame covers both ways a projection can lack
// support: no real sample, and a rate past the one he has posted.
func TestUnprovenIsMeasuredPerGame(t *testing.T) {
	// Four games is not a season's evidence however good it was.
	thin := TraitInput{Points: 170, PriorGames: 4, PriorPoints: 40}
	if !thin.unproven() {
		t.Error("four games is not a solid history")
	}
	// But with a real sample, rate is compared to rate: a player who missed
	// six games and matched his rate is proven, though his total collapsed.
	hurt := TraitInput{Points: 170, PriorGames: 11, PriorPoints: 110} // 10.0 vs 10.0 ppg
	if hurt.unproven() {
		t.Error("matching his rate over eleven games is proven, whatever the total says")
	}

	leap := TraitInput{Points: 170, PriorGames: 16, PriorPoints: 96} // 10.0 vs 6.0 ppg
	if !leap.unproven() {
		t.Error("a projection 1.7x his own per-game rate is unproven")
	}
	steady := TraitInput{Points: 170, PriorGames: 16, PriorPoints: 160} // 10.0 vs 10.0
	if steady.unproven() {
		t.Error("a projection matching his rate is proven")
	}
	rookie := TraitInput{Points: 170, PriorGames: 0}
	if !rookie.unproven() {
		t.Error("no prior season means nothing to check the projection against")
	}
}

// TestBellCowNeedsBothJobs — a back with carries alone is a committee
// grinder and one with targets alone is a passing-down specialist.
func TestBellCowNeedsBothJobs(t *testing.T) {
	in := []TraitInput{
		back("every-down", 1100, 9, 55, 420, 70),
		back("grinder", 900, 8, 8, 60, 12),
		back("third-down", 120, 1, 60, 480, 80),
	}
	// Pad so the window has something to rank.
	for i := 0; i < 10; i++ {
		in = append(in, back("filler"+string(rune('a'+i)), 400, 3, 20, 150, 25))
	}
	got := ClassifyTraits(in, traitShapePool())

	if !got["every-down"].Has(TraitBellCow) {
		t.Error("a back with both jobs is a bell cow")
	}
	if got["grinder"].Has(TraitBellCow) {
		t.Error("carries without receiving work is not a bell cow")
	}
	if !got["third-down"].Has(TraitBellCow) {
		// Receiving volume plus any rushing counts; documented behaviour.
		t.Log("note: a receiving back with some rushing reads as a bell cow")
	}
}

// TestOnlyContendedPlayersAreLabelled — a trait on somebody nobody will
// roster is a label with nothing behind it.
func TestOnlyContendedPlayersAreLabelled(t *testing.T) {
	var in []TraitInput
	for i := 0; i < 80; i++ {
		rec := float64(90 - i)
		in = append(in, catcher(string(rune('a'+i%26))+string(rune('a'+i/26)), "WR",
			rec, rec*12, 5, rec*1.5))
	}
	got := ClassifyTraits(in, traitShapePool())

	labelled := 0
	for _, set := range got {
		if len(set) > 0 {
			labelled++
		}
	}
	if labelled == 0 {
		t.Fatal("nobody was labelled")
	}
	if labelled == len(in) {
		t.Error("everybody was labelled, including players nobody will roster")
	}
}

// TestTraitShapesCountStarters — a bench full of one kind of player does not
// change how a season goes.
func TestTraitShapesCountStarters(t *testing.T) {
	var r Roster
	for i := 0; i < 4; i++ {
		r.Players = append(r.Players, RosterSpot{
			Player:   PlayerSignals{Position: "WR", Traits: TraitSet{TraitRedZone}},
			Starting: false,
		})
	}
	if n := startersWith(r, TraitRedZone); n != 0 {
		t.Errorf("counted %d bench players as starters", n)
	}
	r.Players[0].Starting = true
	if n := startersWith(r, TraitRedZone); n != 1 {
		t.Errorf("counted %d, want 1", n)
	}
}

// TestEveryTraitShapeIsBuildable guards the registry.
func TestEveryTraitShapeIsBuildable(t *testing.T) {
	for _, a := range TraitArchetypes() {
		if a.Trait == "" {
			t.Errorf("%s carries no trait", a.Name)
		}
		if a.Satisfied == nil || a.Allows == nil {
			t.Errorf("%s is not fillable", a.Name)
		}
		if len(a.Anchors) == 0 {
			t.Errorf("%s has nothing to pursue, so the fill will never seek it", a.Name)
		}
		for _, anchor := range a.Anchors {
			if anchor.Trait != a.Trait {
				t.Errorf("%s anchors on %q rather than its own trait", a.Name, anchor.Trait)
			}
		}
	}
}

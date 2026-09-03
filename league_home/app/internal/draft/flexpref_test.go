package draft

import "testing"

// TestRosterShapeNarrowsTheFlex.
//
// A tight end in the flex is a bad season, not a plan, so the roster this
// board assembles should not put one there. The narrowing is a preference and
// applies to lineup construction only.
func TestRosterShapeNarrowsTheFlex(t *testing.T) {
	league := HitOrMissPool()
	mine := Preferences{FlexPositions: []string{"RB", "WR"}}.RosterShape(league)

	if got := mine.FlexPositions; len(got) != 2 || got[0] != "RB" || got[1] != "WR" {
		t.Errorf("flex = %v, want [RB WR]", got)
	}
	// The league's own shape must be untouched: it is what the pool is priced
	// against, and eleven other managers will start a tight end in theirs.
	if len(league.FlexPositions) != 3 {
		t.Errorf("the league shape was mutated: %v", league.FlexPositions)
	}
}

// No preference is the league's rule, unchanged.
func TestRosterShapeWithoutAPreferenceIsTheLeagues(t *testing.T) {
	league := HitOrMissPool()
	if got := (Preferences{}).RosterShape(league); len(got.FlexPositions) != len(league.FlexPositions) {
		t.Errorf("flex = %v, want the league's %v", got.FlexPositions, league.FlexPositions)
	}
}

// A preference may narrow the league's flex, never widen it past what the
// lineup would actually accept — asking to flex a quarterback does not make it
// legal.
func TestRosterShapeCannotWidenPastTheLeagueRule(t *testing.T) {
	league := HitOrMissPool()
	mine := Preferences{FlexPositions: []string{"RB", "QB"}}.RosterShape(league)

	for _, pos := range mine.FlexPositions {
		if pos == "QB" {
			t.Error("a quarterback became flex-eligible by preference")
		}
	}
	if len(mine.FlexPositions) != 1 || mine.FlexPositions[0] != "RB" {
		t.Errorf("flex = %v, want just [RB] — QB is not eligible", mine.FlexPositions)
	}
}

// Asking for nothing the league allows falls back to the league rule rather
// than leaving a flex no player can fill, which would read as a lineup that
// can never be completed.
func TestRosterShapeFallsBackWhenNothingIsEligible(t *testing.T) {
	league := HitOrMissPool()
	mine := Preferences{FlexPositions: []string{"K", "DEF"}}.RosterShape(league)

	if len(mine.FlexPositions) != len(league.FlexPositions) {
		t.Errorf("flex = %v, want the league's %v", mine.FlexPositions, league.FlexPositions)
	}
}

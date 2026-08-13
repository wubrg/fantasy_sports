package draft

import (
	"strings"
	"testing"
)

func psig(id, name, pos, team string) PlayerSignals {
	return PlayerSignals{PlayerID: id, Name: name, Position: pos, Team: team}
}

// Lions offense, plus a receiver on another team as a control that must never
// be blocked.
var (
	goff     = psig("goff", "Jared Goff", "QB", "DET")
	gibbs    = psig("gibbs", "Jahmyr Gibbs", "RB", "DET")
	montgo   = psig("montgo", "David Montgomery", "RB", "DET")
	amonra   = psig("amonra", "Amon-Ra St. Brown", "WR", "DET")
	jwilliam = psig("jwilliam", "Jameson Williams", "WR", "DET")
	laporta  = psig("laporta", "Sam LaPorta", "TE", "DET")
	chase    = psig("chase", "Ja'Marr Chase", "WR", "CIN")
)

func board() []PlayerSignals {
	return []PlayerSignals{goff, gibbs, montgo, amonra, jwilliam, laporta, chase}
}

// TestOwningTheBackBlocksTheOffense is the motivating case from the notes: a
// running back anchors no stack, so owning Gibbs takes the rest of the Lions
// off your board while a receiver on another team is untouched.
func TestOwningTheBackBlocksTheOffense(t *testing.T) {
	blocked := BlockedForMe([]PlayerSignals{gibbs}, board(), DefaultPreferences())

	for _, id := range []string{"amonra", "jwilliam", "laporta", "montgo"} {
		if _, ok := blocked[id]; !ok {
			t.Errorf("%s should be blocked when you own Gibbs", id)
		}
	}
	if _, ok := blocked["chase"]; ok {
		t.Errorf("a receiver on another team must never be blocked")
	}
	if _, ok := blocked["gibbs"]; ok {
		t.Errorf("a player you own is not a candidate against himself")
	}
}

// TestQuarterbackStacksArePermitted: owning the quarterback opens the passing
// game — his receiver and tight end may join — but not the running back, who
// stacks with no one.
func TestQuarterbackStacksArePermitted(t *testing.T) {
	blocked := BlockedForMe([]PlayerSignals{goff}, board(), DefaultPreferences())

	if _, ok := blocked["amonra"]; ok {
		t.Errorf("QB+WR is an allowed stack; Amon-Ra should not be blocked")
	}
	if _, ok := blocked["laporta"]; ok {
		t.Errorf("QB+TE is an allowed stack; LaPorta should not be blocked")
	}
	if _, ok := blocked["gibbs"]; !ok {
		t.Errorf("a running back anchors no stack; Gibbs should be blocked behind the QB")
	}
}

// TestSecondSamePositionIsAHandcuff: even on an offense you are stacking, a
// second player at a position you already own is a handcuff and blocked.
func TestSecondSamePositionIsAHandcuff(t *testing.T) {
	blocked := BlockedForMe([]PlayerSignals{goff, amonra}, board(), DefaultPreferences())

	if reason, ok := blocked["jwilliam"]; !ok {
		t.Errorf("a second Lions receiver should be blocked as a handcuff")
	} else if !strings.Contains(reason, "handcuff") {
		t.Errorf("reason %q should mention handcuff", reason)
	}
	if _, ok := blocked["laporta"]; ok {
		t.Errorf("the tight end still stacks with the QB and should not be blocked")
	}
}

// TestHandcuffOnlyLeavesOtherOffenseMatesAlone: with one-per-offense off, only
// the same-position duplicate is blocked; the rest of the offense is fair game.
func TestHandcuffOnlyLeavesOtherOffenseMatesAlone(t *testing.T) {
	prefs := Preferences{NoHandcuffs: true} // OnePerOffense off, no stacks
	blocked := BlockedForMe([]PlayerSignals{gibbs}, board(), prefs)

	if _, ok := blocked["montgo"]; !ok {
		t.Errorf("a second running back is a handcuff and should be blocked")
	}
	if _, ok := blocked["amonra"]; ok {
		t.Errorf("with one-per-offense off, a receiver on the offense is not blocked")
	}
}

// TestInactivePreferencesBlockNothing: with both filters off there is no
// personal board at all.
func TestInactivePreferencesBlockNothing(t *testing.T) {
	if blocked := BlockedForMe([]PlayerSignals{gibbs}, board(), Preferences{}); blocked != nil {
		t.Errorf("inactive preferences should block nothing, got %v", blocked)
	}
}

// TestEffectiveScarcityDropsBelowLeague: once a piece of an offense is owned,
// the startable count over your board is lower than the room's, measured at the
// same threshold. Nil when nothing is blocked.
func TestEffectiveScarcityDropsBelowLeague(t *testing.T) {
	state := HitOrMissPool()
	state.Teams = 12

	// Every listed player clears the bar, so the count is just "how many are
	// on my board" — the block filter is the only thing that can move it.
	b := board()
	withPoints := make([]PlayerSignals, len(b))
	for i, p := range b {
		p.CielyPoints = 200
		withPoints[i] = p
	}
	thresholds := map[string]float64{"WR": 100, "QB": 100, "RB": 100, "TE": 100}

	if EffectiveScarcity(withPoints, nil, state, thresholds) != nil {
		t.Errorf("no blocked players should yield nil effective scarcity")
	}

	blocked := BlockedForMe([]PlayerSignals{gibbs}, withPoints, DefaultPreferences())
	eff := EffectiveScarcity(withPoints, blocked, state, thresholds)
	league := Scarcity(withPoints, state, thresholds)

	if eff["WR"].Startable >= league["WR"].Startable {
		t.Errorf("effective WR startable (%d) should be below league (%d) after owning a Lion",
			eff["WR"].Startable, league["WR"].Startable)
	}
}

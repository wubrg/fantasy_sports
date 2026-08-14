package draft

import "testing"

// samplePool is a small but legal draft pool: enough at every position to fill
// QB1/RB2/WR3/TE1/FLEX and a bench, with a clean price/points spread.
func samplePool() []PlayerSignals {
	mk := func(id, name, pos, team string, cost, pts int) PlayerSignals {
		return PlayerSignals{
			PlayerID: id, Name: name, Position: pos, Team: team,
			Cost: cost, MyMaxBid: cost, CielyPoints: float64(pts),
		}
	}
	return []PlayerSignals{
		mk("qb1", "QB One", "QB", "AA", 20, 320),
		mk("qb2", "QB Two", "QB", "BB", 4, 280),
		mk("rb1", "RB One", "RB", "CC", 40, 300),
		mk("rb2", "RB Two", "RB", "DD", 28, 280),
		mk("rb3", "RB Three", "RB", "EE", 10, 240),
		mk("rb4", "RB Four", "RB", "FF", 3, 205),
		mk("rb5", "RB Five", "RB", "GG", 1, 185),
		mk("wr1", "WR One", "WR", "HH", 45, 290),
		mk("wr2", "WR Two", "WR", "II", 33, 275),
		mk("wr3", "WR Three", "WR", "JJ", 18, 250),
		mk("wr4", "WR Four", "WR", "KK", 8, 230),
		mk("wr5", "WR Five", "WR", "LL", 2, 200),
		mk("wr6", "WR Six", "WR", "MM", 1, 175),
		mk("te1", "TE One", "TE", "NN", 15, 185),
		mk("te2", "TE Two", "TE", "OO", 3, 150),
		mk("te3", "TE Three", "TE", "PP", 1, 130),
	}
}

func withLean(pool []PlayerSignals, id string, lean Lean, myMax int) []PlayerSignals {
	for i := range pool {
		if pool[i].PlayerID == id {
			pool[i].Lean = PlayerLean{Lean: lean}
			if myMax > 0 {
				pool[i].MyMaxBid = myMax
			}
		}
	}
	return pool
}

func sampleShape() PoolState {
	sh := HitOrMissPool()
	sh.Teams = 12
	return sh
}

// countStarters checks a sampled team fields a full skill lineup or says why.
func TestSampleFillsALegalLineupUnderBudget(t *testing.T) {
	pool := samplePool()
	teams := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{},
		Preferences{}, ObjectiveStrategy, 50, 1)
	if len(teams) == 0 {
		t.Fatal("expected at least one sampled team")
	}
	for _, tm := range teams {
		if len(tm.Unfilled) != 0 {
			t.Errorf("team left slots unfilled: %v", tm.Unfilled)
		}
		// 8 skill starters: QB1 RB2 WR3 TE1 + FLEX1.
		if len(tm.Starters) != 8 {
			t.Errorf("starters = %d, want 8", len(tm.Starters))
		}
		// Skill spend plus the reserved defense dollar must stay in budget.
		if tm.Spend > 199 {
			t.Errorf("spend $%d exceeds the $199 skill budget", tm.Spend)
		}
	}
}

func TestSampleNeverBuysDoNotDraft(t *testing.T) {
	pool := withLean(samplePool(), "wr3", LeanDND, 0)
	teams := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{},
		Preferences{}, ObjectiveLeansMax, 50, 7)
	for _, tm := range teams {
		for _, s := range append(append([]RosterSpot{}, tm.Starters...), tm.Bench...) {
			if s.Player.PlayerID == "wr3" {
				t.Error("a do-not-draft player must never be rostered")
			}
		}
	}
}

func TestSampleRespectsMustHaveCeiling(t *testing.T) {
	// A must-have priced near your walk-away ($25): he lands on the draws that
	// stay under it, and is never bought above it — the draws that push past
	// the ceiling are ones you were outbid on.
	pool := samplePool()
	for i := range pool {
		if pool[i].PlayerID == "rb1" {
			pool[i].Cost = 22
			pool[i].Lean = PlayerLean{Lean: LeanMust}
			pool[i].MyMaxBid = 25
		}
	}
	teams := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{},
		Preferences{}, ObjectiveLeansMax, 100, 3)
	landed := false
	for _, tm := range teams {
		for _, s := range append(append([]RosterSpot{}, tm.Starters...), tm.Bench...) {
			if s.Player.PlayerID == "rb1" {
				landed = true
				if s.Price > 25 {
					t.Errorf("must-have bought at $%d, above the $25 ceiling", s.Price)
				}
			}
		}
	}
	if !landed {
		t.Error("a must-have inside budget should land on at least one sampled team")
	}
}

func TestStrategyRespectsOnePerOffense(t *testing.T) {
	// Two receivers on one offense; one-per-offense means no strategy team may
	// roster both. Leans-max, which relaxes preferences, is free to.
	pool := samplePool()
	for i := range pool {
		if pool[i].PlayerID == "wr2" || pool[i].PlayerID == "wr3" {
			pool[i].Team = "SAME"
		}
	}
	prefs := Preferences{OnePerOffense: true}

	strat := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{},
		prefs, ObjectiveStrategy, 100, 11)
	for _, tm := range strat {
		var same int
		for _, s := range append(append([]RosterSpot{}, tm.Starters...), tm.Bench...) {
			if s.Player.Team == "SAME" {
				same++
			}
		}
		if same > 1 {
			t.Errorf("strategy team holds %d players from one blocked offense", same)
		}
	}
}

func TestLeansMaxLandsAtLeastAsManyGuys(t *testing.T) {
	// Tag several players as up; leans-max should land at least as many of them
	// as strategy on the same pool, since it optimizes for exactly that.
	pool := samplePool()
	for _, id := range []string{"rb1", "wr1", "wr2", "te1"} {
		pool = withLean(pool, id, LeanUp, 0)
	}
	best := func(o Objective) int {
		teams := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{},
			Preferences{}, o, 100, 5)
		top := 0
		for _, tm := range teams {
			if tm.MyGuys > top {
				top = tm.MyGuys
			}
		}
		return top
	}
	if best(ObjectiveLeansMax) < best(ObjectiveStrategy) {
		t.Errorf("leans-max landed fewer of my guys (%d) than strategy (%d)",
			best(ObjectiveLeansMax), best(ObjectiveStrategy))
	}
}

func TestSampleIsReproducible(t *testing.T) {
	pool := samplePool()
	a := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{}, Preferences{}, ObjectiveStrategy, 30, 42)
	b := SampleTeams(pool, nil, 200, 14, sampleShape(), map[string]float64{}, Preferences{}, ObjectiveStrategy, 30, 42)
	if len(a) != len(b) {
		t.Fatalf("same seed gave %d and %d teams", len(a), len(b))
	}
	for i := range a {
		if teamKeyOf(a[i]) != teamKeyOf(b[i]) {
			t.Errorf("team %d differs between identical seeds", i)
		}
	}
}

func teamKeyOf(tm TeamSample) string {
	key := ""
	for _, s := range append(append([]RosterSpot{}, tm.Starters...), tm.Bench...) {
		key += s.Player.PlayerID + ","
	}
	return key
}

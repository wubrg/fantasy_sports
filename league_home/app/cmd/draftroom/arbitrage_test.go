package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// arbPrefs are the real preferences this board runs on: one per offense, no
// handcuffs, and a quarterback allowed to share his offense with a pass
// catcher.
func arbPrefs() draft.Preferences {
	return draft.Preferences{
		OnePerOffense: true,
		NoHandcuffs:   true,
		Stacks:        []draft.Stack{{A: "QB", B: "WR"}, {A: "QB", B: "TE"}},
	}
}

func sig(id, name, pos, team string, value int) draft.PlayerSignals {
	return draft.PlayerSignals{
		PlayerID: id, Name: name, Position: pos, Team: team,
		Value: value, MyMaxBid: value + 5,
		Lean: draft.PlayerLean{Favorite: true},
	}
}

// TestPairRelationMatchesTheBoardsOwnRules.
//
// The relations shown here have to be the board's verdict, not this page's
// opinion. A second copy of the preference rules is the one thing that could
// make the page confidently contradict the blocked flag on the row beside it,
// and the rules are subtle: a handcuff outranks a stack, and a running back
// anchors no stack at all.
func TestPairRelationMatchesTheBoardsOwnRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b draft.PlayerSignals
		want string
	}{
		{"two receivers on one team are a handcuff",
			sig("1", "Chase", "WR", "CIN", 64), sig("2", "Higgins", "WR", "CIN", 24), "handcuff"},
		{"a back and a receiver share an offense",
			sig("3", "Gibbs", "RB", "DET", 87), sig("4", "St. Brown", "WR", "DET", 58), "one-per-offense"},
		{"a quarterback and his receiver are a stack",
			sig("5", "Lamar", "QB", "BAL", 20), sig("6", "Lane", "WR", "BAL", 1), "stack"},
		{"a quarterback and his tight end are a stack",
			sig("7", "Allen", "QB", "BUF", 40), sig("8", "Kincaid", "TE", "BUF", 11), "stack"},
		{"a back anchors no stack, so he blocks the tight end",
			sig("9", "Bijan", "RB", "ATL", 79), sig("10", "Pitts", "TE", "ATL", 19), "one-per-offense"},
	} {
		if got := pairRelation(tc.a, tc.b, arbPrefs()); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// A group is only interesting where two targets contend, and the cost of each
// has to name the others he would rule out.
func TestGroupsKeepOnlyContestedOffensesAndNameTheCost(t *testing.T) {
	targets := []draft.PlayerSignals{
		sig("1", "Gibbs", "RB", "DET", 87),
		sig("2", "St. Brown", "WR", "DET", 58),
		sig("3", "LaPorta", "TE", "DET", 19),
		sig("4", "Alone", "WR", "NYJ", 30), // single target, no contest
	}

	groups := buildGroups(targets, arbPrefs())

	if len(groups) != 1 || groups[0].Team != "DET" {
		t.Fatalf("want only the contested DET group, got %+v", groups)
	}
	g := groups[0]
	if g.Targets[0].Name != "Gibbs" {
		t.Errorf("group ordered %s first, want the most valuable", g.Targets[0].Name)
	}
	if len(g.Costs["1"]) != 2 {
		t.Errorf("taking Gibbs costs %v, want both teammates", g.Costs["1"])
	}
	// Symmetric: whichever is taken, the other is gone.
	if len(g.Costs["3"]) != 2 {
		t.Errorf("taking LaPorta costs %v, want both teammates", g.Costs["3"])
	}
}

// A legal stack is not a cost. Owning the quarterback still allows his
// receiver, so neither should appear in the other's casualty list.
func TestAStackCostsNothing(t *testing.T) {
	targets := []draft.PlayerSignals{
		sig("1", "Lamar", "QB", "BAL", 20),
		sig("2", "Lane", "WR", "BAL", 1),
	}
	g := buildGroups(targets, arbPrefs())
	if len(g) != 1 {
		t.Fatalf("want the BAL group, got %+v", g)
	}
	if len(g[0].Costs) != 0 {
		t.Errorf("a stack was charged a cost: %v", g[0].Costs)
	}
	if g[0].Pairs[0].Relation != "stack" {
		t.Errorf("pair relation %q, want stack", g[0].Pairs[0].Relation)
	}
}

// TestChainRecordsWhatEachPickCost is the point of the page.
//
// Taking the best back on an offense has to report the teammates it just ruled
// out, against the pick that ruled them out — that is the "take Gibbs, lose
// St. Brown and LaPorta" line the board cannot say on its own.
func TestChainRecordsWhatEachPickCost(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		sig("1", "Gibbs", "RB", "DET", 87),
		sig("2", "St. Brown", "WR", "DET", 58),
		sig("3", "LaPorta", "TE", "DET", 19),
		sig("4", "Chase", "WR", "CIN", 64),
	}

	chain, _, spend := srv.buildChain(nil, targets, arbPrefs(),
		srv.scoringBaselines(), srv.static.shape)

	if len(chain) == 0 {
		t.Fatal("the walk picked nobody")
	}
	if chain[0].Pick.Name != "Gibbs" {
		t.Errorf("first pick %s, want the most valuable target", chain[0].Pick.Name)
	}
	lost := map[string]bool{}
	for _, c := range chain[0].Cost {
		lost[c.Name] = true
	}
	if !lost["St. Brown"] || !lost["LaPorta"] {
		t.Errorf("taking Gibbs cost %v, want both DET teammates", chain[0].Cost)
	}
	if lost["Chase"] {
		t.Error("a player on another offense was charged to the Gibbs pick")
	}
	if spend != chain[len(chain)-1].Spend {
		t.Errorf("total spend %d disagrees with the last step's %d", spend, chain[len(chain)-1].Spend)
	}
}

// Seeding from what I already hold has to collapse a group before the walk
// starts: once Gibbs is mine, no other Lion can be picked.
func TestHeldPlayersBlockTheirOffenseFromTheStart(t *testing.T) {
	srv := scratchServer(t)
	held := []draft.PlayerSignals{sig("1", "Gibbs", "RB", "DET", 87)}
	targets := []draft.PlayerSignals{
		sig("2", "St. Brown", "WR", "DET", 58),
		sig("3", "Chase", "WR", "CIN", 64),
	}

	chain, _, _ := srv.buildChain(held, targets, arbPrefs(),
		srv.scoringBaselines(), srv.static.shape)

	for _, step := range chain {
		if step.Pick.Team == "DET" {
			t.Errorf("picked %s on an offense already held", step.Pick.Name)
		}
	}
	if len(chain) == 0 || chain[0].Pick.Name != "Chase" {
		t.Errorf("want Chase first, got %+v", chain)
	}
}

// The walk stops when it runs out of targets rather than inventing players,
// and says which starting slots it could not fill.
func TestChainReportsTheSlotsItCouldNotFill(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{sig("1", "Chase", "WR", "CIN", 64)}

	chain, unfilled, _ := srv.buildChain(nil, targets, arbPrefs(),
		srv.scoringBaselines(), srv.static.shape)

	if len(chain) != 1 {
		t.Fatalf("want one pick from one target, got %d", len(chain))
	}
	if len(unfilled) == 0 {
		t.Error("one target filled a whole starting lineup")
	}
}

// A sold player is not a target any more.
//
// The page reads the snapshot's players, which already excludes anyone off the
// board, so this needs no code of its own — but it is the property the whole
// page depends on during a draft, and asserting it costs less than discovering
// mid-auction that the page is still offering someone who went twenty minutes
// ago.
func TestSoldTargetsLeaveThePage(t *testing.T) {
	srv := scratchServer(t)
	// The shared fixture carries no reads, so give it one: a skipped test here
	// would assert nothing about the property the page depends on.
	// Keyed the way Leans is keyed — normalized, not raw. WalkAway looks up
	// normalizeName(player), so a raw key silently matches nobody.
	srv.static.leans = draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs"): {Player: "Jahmyr Gibbs", Favorite: true},
	}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	before := 0
	for _, p := range srv.snapshot().Players {
		if isTarget(p) {
			before++
		}
	}
	if before == 0 {
		t.Skip("fixture carries no targets")
	}

	var sold string
	for _, p := range srv.snapshot().Players {
		if isTarget(p) {
			sold = p.PlayerID
			break
		}
	}
	srv.taken[sold] = gone{price: 30, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	after := 0
	for _, p := range srv.snapshot().Players {
		if isTarget(p) {
			if p.PlayerID == sold {
				t.Errorf("%s was sold and is still a target", p.Name)
			}
			after++
		}
	}
	if after != before-1 {
		t.Errorf("targets %d -> %d, want one fewer", before, after)
	}
}

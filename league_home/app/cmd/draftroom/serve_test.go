package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// testStatic is a miniature league: three players, a $200 pool, and no
// keepers, so Build's arithmetic is checkable by hand.
func testStatic() *staticData {
	shape := draft.HitOrMissPool()
	shape.Teams = 1
	s := &staticData{
		shape: shape, teams: 1, budget: 200, ownerID: "me", season: "2026",
		points: map[string]float64{}, availability: map[string]string{},
		keeperOf: map[string]int{}, leans: draft.Leans{},
	}
	for _, p := range []struct {
		id, name, pos string
		pts, aav      float64
	}{
		{"1", "Jahmyr Gibbs", "RB", 324, 68},
		{"2", "Ja'Marr Chase", "WR", 300, 62},
		{"3", "Brock Bowers", "TE", 190, 34},
	} {
		s.projections = append(s.projections, draft.Projection{
			PlayerID: p.id, Name: p.name, Position: p.pos, Points: p.pts,
		})
		s.market = append(s.market, draft.MarketPrice{
			PlayerID: p.id, Name: p.name, Position: p.pos, AAV: p.aav,
		})
		s.points[p.id] = p.pts
	}
	return s
}

// TestBuildRemovesDraftedPlayers — a player who has gone leaves the board
// and takes his money and slot out of the pool.
func TestBuildRemovesDraftedPlayers(t *testing.T) {
	s := testStatic()
	base, err := s.Build(nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Build(map[string]gone{"1": {price: 45, mine: false}})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Players) != len(base.Players)-1 {
		t.Errorf("board went %d -> %d, want one fewer", len(base.Players), len(after.Players))
	}
	for _, p := range after.Players {
		if p.Name == "Jahmyr Gibbs" {
			t.Error("a drafted player must leave the board")
		}
	}
	if after.Dollars != base.Dollars-45 || after.Slots != base.Slots-1 {
		t.Errorf("pool = $%d/%d, want $%d/%d", after.Dollars, after.Slots, base.Dollars-45, base.Slots-1)
	}
}

// TestBuildDebitsOnlyMyOwnPurchases — a rival buying someone shrinks the
// pool but must not touch my budget or my lineup needs.
func TestBuildDebitsOnlyMyOwnPurchases(t *testing.T) {
	s := testStatic()
	theirs, err := s.Build(map[string]gone{"1": {price: 45, mine: false}})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := s.Build(map[string]gone{"1": {price: 45, mine: true}})
	if err != nil {
		t.Fatal(err)
	}
	if theirs.Me.Budget != 200 {
		t.Errorf("a rival's buy changed my budget: $%d", theirs.Me.Budget)
	}
	if mine.Me.Budget != 155 {
		t.Errorf("my budget = $%d, want $155", mine.Me.Budget)
	}
	if mine.Me.StartersNeeded["RB"] >= theirs.Me.StartersNeeded["RB"] {
		t.Error("buying a back should reduce my remaining RB requirement")
	}
}

// TestBuildLowersTheCeilingAfterSpending
func TestBuildLowersTheCeilingAfterSpending(t *testing.T) {
	s := testStatic()
	base, _ := s.Build(nil)
	after, _ := s.Build(map[string]gone{"1": {price: 100, mine: true}})
	if after.Recommended >= base.Recommended {
		t.Errorf("spending $100 should lower the safe ceiling: %d -> %d",
			base.Recommended, after.Recommended)
	}
}

// TestManualSalesOverrideTheLiveFeed — you know a sale closed before the
// API does, so a hand entry must survive the next poll.
func TestManualSalesOverrideTheLiveFeed(t *testing.T) {
	srv, err := newServer(testStatic())
	if err != nil {
		t.Fatal(err)
	}
	srv.manual["1"] = gone{price: 45, mine: true}
	// A later poll reports the same player at a different price.
	srv.taken["1"] = gone{price: 40, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	if got := srv.snapshot().Me.Budget; got != 155 {
		t.Errorf("budget = $%d, want $155 — the hand entry should win", got)
	}
}

// TestPlayerIDByNameNormalizes — the page posts whatever it rendered, so
// punctuation must not defeat the lookup.
func TestPlayerIDByNameNormalizes(t *testing.T) {
	s := testStatic()
	for _, name := range []string{"Ja'Marr Chase", "JaMarr Chase", "jamarr chase"} {
		if got := s.playerIDByName(name); got != "2" {
			t.Errorf("playerIDByName(%q) = %q, want 2", name, got)
		}
	}
	if got := s.playerIDByName("Nobody At All"); got != "" {
		t.Errorf("unknown name should resolve to empty, got %q", got)
	}
}

// TestTempoComparesRealPricesToTheCostBoard — this is the live calibration
// that three usable seasons of history cannot provide.
func TestTempoComparesRealPricesToTheCostBoard(t *testing.T) {
	s := testStatic()
	costs := map[string]int{"1": 60, "2": 55}
	got := s.tempo(map[string]gone{
		"1": {price: 90, mine: false},
		"2": {price: 70, mine: true},
		"3": {price: 0, mine: false}, // nobody paid, so it says nothing
	}, costs)

	if got.Picks != 2 {
		t.Errorf("picks = %d, want 2 — a $0 removal is not a price signal", got.Picks)
	}
	if got.Spent != 160 || got.Expected != 115 {
		t.Errorf("spent/expected = %d/%d, want 160/115", got.Spent, got.Expected)
	}
	if got.Ratio() <= 1 {
		t.Errorf("ratio = %.2f, want above 1 for a hot room", got.Ratio())
	}
}

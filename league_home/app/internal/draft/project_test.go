package draft

import (
	"slices"
	"strings"
	"testing"

	"leaguehome/internal/sleeper"
)

func projectionFixture(t *testing.T) *Ledger {
	t.Helper()
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{
			pick("alice", "keep2", "20", false),
			pick("alice", "fresh", "40", false),
			pick("bob", "hurt", "12", false),
		}),
		season("2025", []sleeper.DraftPick{
			pick("alice", "keep2", "25", true),
		}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestProjectPricesEveryRosteredPlayer(t *testing.T) {
	l := projectionFixture(t)
	rosters := []sleeper.Roster{
		{RosterID: 1, OwnerID: "alice", Players: []string{"keep2", "fresh"}},
		{RosterID: 2, OwnerID: "bob", Players: []string{"hurt"}},
	}
	info := map[string]PlayerInfo{
		"keep2": {Name: "Second Keep", Position: "RB", GamesPlayed: 17},
		"fresh": {Name: "First Keep", Position: "WR", GamesPlayed: 15},
		"hurt":  {Name: "Missed The Year", Position: "TE", GamesPlayed: 0},
	}

	entries, err := Project(l, "2026", rosters, info)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 projections, got %d", len(entries))
	}

	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.PlayerID] = e
	}
	// Kept once at $25, so a second keep costs $25 + $10.
	if got := byID["keep2"]; got.KeepCount != 2 || got.LeaguePrice != 35 {
		t.Errorf("keep2: keepCount=%d price=%d, want 2/35", got.KeepCount, got.LeaguePrice)
	}
	// Never kept, drafted at $40, so a first keep costs $40 + $5.
	if got := byID["fresh"]; got.KeepCount != 1 || got.LeaguePrice != 45 {
		t.Errorf("fresh: keepCount=%d price=%d, want 1/45", got.KeepCount, got.LeaguePrice)
	}
}

// TestProjectFlagsPlayersWhoDidNotPlay covers rosters.md's eligibility
// rule: a player who did not play last season cannot be kept at all.
func TestProjectFlagsPlayersWhoDidNotPlay(t *testing.T) {
	l := projectionFixture(t)
	rosters := []sleeper.Roster{{RosterID: 2, OwnerID: "bob", Players: []string{"hurt"}}}
	info := map[string]PlayerInfo{"hurt": {Name: "Missed The Year", GamesPlayed: 0}}

	entries, err := Project(l, "2026", rosters, info)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(entries[0].Flags, FlagDidNotPlay) {
		t.Errorf("expected %q flag, got %v", FlagDidNotPlay, entries[0].Flags)
	}

	// And the renderer must leave him out, since quoting a price for an
	// ineligible player would invite keeping him.
	var sb strings.Builder
	if err := WriteProjection(&sb, entries, Names{"bob": "Team Bob"}, "2026", 2, 200); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "Missed The Year") {
		t.Errorf("ineligible player should not be listed:\n%s", sb.String())
	}
}

// TestProjectUnknownGamesPlayedIsNotIneligible distinguishes "played zero
// games" from "we have no stats for him" — only the former is a rules
// violation, and treating the latter the same would hide real options.
func TestProjectUnknownGamesPlayedIsNotIneligible(t *testing.T) {
	l := projectionFixture(t)
	rosters := []sleeper.Roster{{RosterID: 1, OwnerID: "alice", Players: []string{"fresh"}}}
	info := map[string]PlayerInfo{"fresh": {Name: "No Stats Recorded", GamesPlayed: -1}}

	entries, err := Project(l, "2026", rosters, info)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(entries[0].Flags, FlagDidNotPlay) {
		t.Errorf("unknown games played must not be treated as ineligible: %v", entries[0].Flags)
	}
}

func TestWriteProjectionShowsCheapestFirstAndBudgetFloor(t *testing.T) {
	entries := []Entry{
		{OwnerID: "alice", Name: "Pricey", LeaguePrice: 74, KeepCount: 1},
		{OwnerID: "alice", Name: "Cheap", LeaguePrice: 10, KeepCount: 1},
		{OwnerID: "alice", Name: "Middle", LeaguePrice: 35, KeepCount: 3},
	}
	var sb strings.Builder
	if err := WriteProjection(&sb, entries, Names{"alice": "Team Alice"}, "2026", 2, 200); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if strings.Index(out, "Cheap") > strings.Index(out, "Middle") {
		t.Errorf("cheapest option should list first:\n%s", out)
	}
	// $200 less the two cheapest ($10 + $35).
	if !strings.Contains(out, "$155") {
		t.Errorf("expected a $155 budget floor:\n%s", out)
	}
}

func keeperEntries() []Entry {
	return []Entry{
		{OwnerID: "alice", PlayerID: "steal", Name: "Cheap Star", Position: "WR", LeaguePrice: 20},
		{OwnerID: "alice", PlayerID: "fair", Name: "Fairly Priced", Position: "RB", LeaguePrice: 40},
		{OwnerID: "alice", PlayerID: "trap", Name: "Hyped Dud", Position: "RB", LeaguePrice: 15},
		{OwnerID: "alice", PlayerID: "bad", Name: "Overpriced Keep", Position: "TE", LeaguePrice: 30},
		{OwnerID: "bob", PlayerID: "onlyone", Name: "Bob's Guy", Position: "WR", LeaguePrice: 10},
	}
}

var keeperCosts = map[string]int{"steal": 68, "fair": 41, "trap": 25, "bad": 12, "onlyone": 9}
var keeperValues = map[string]int{"steal": 61, "fair": 44, "trap": 9, "bad": 14, "onlyone": 20}

func TestRankKeepersRecommendsBySavings(t *testing.T) {
	got := RankKeepers(keeperEntries(), keeperCosts, keeperValues, 2)
	rec := map[string]bool{}
	for _, k := range got {
		if k.Recommended {
			rec[k.PlayerID] = true
		}
	}
	// alice: steal saves +48, trap +10, fair +1, bad -18. Top two win.
	if !rec["steal"] || !rec["trap"] {
		t.Errorf("expected steal and trap recommended, got %v", rec)
	}
	if rec["bad"] {
		t.Error("a keeper who costs more than the market should never be recommended")
	}
	// bob's only option saves nothing, so he keeps nobody.
	if rec["onlyone"] {
		t.Error("a keeper saving $-1 is not worth a roster spot")
	}
}

// TestKeeperTrapFlagsCheapButBadPlayers — savings alone can recommend a
// player the market overprices, and locking that in is not a bargain.
func TestKeeperTrapFlagsCheapButBadPlayers(t *testing.T) {
	got := RankKeepers(keeperEntries(), keeperCosts, keeperValues, 2)
	byID := map[string]KeeperOption{}
	for _, k := range got {
		byID[k.PlayerID] = k
	}
	trap := byID["trap"]
	if !trap.Trap() {
		t.Errorf("cost $25 / value $9 at a $15 keep is a trap: saves %+d edge %+d",
			trap.Saves(), trap.Edge())
	}
	if byID["steal"].Trap() {
		t.Error("a genuinely cheap star is not a trap")
	}
}

func TestRankKeepersSkipsIneligiblePlayers(t *testing.T) {
	entries := append(keeperEntries(), Entry{
		OwnerID: "alice", PlayerID: "hurt", Name: "Missed The Year",
		Position: "RB", LeaguePrice: 10, Flags: []string{FlagDidNotPlay},
	})
	for _, k := range RankKeepers(entries, keeperCosts, keeperValues, 2) {
		if k.PlayerID == "hurt" {
			t.Error("a player who did not play cannot be kept and must not be offered")
		}
	}
}

func TestWriteKeeperOptionsShowsTheDecision(t *testing.T) {
	var sb strings.Builder
	options := RankKeepers(keeperEntries(), keeperCosts, keeperValues, 2)
	err := WriteKeeperOptions(&sb, options, Names{"alice": "Team Alice", "bob": "Team Bob"}, "2026", 2, 200)
	if err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"Cheap Star", "KEEP", "SAVES", "overpriced"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q:\n%s", want, out)
		}
	}
	// Bob has nothing worth keeping and the report should say so plainly.
	if !strings.Contains(out, "keep nobody") {
		t.Errorf("expected an explicit no-keeper line:\n%s", out)
	}
}

// shareEntries is one league's worth of keeper candidates across two teams.
func shareEntries() []Entry {
	return []Entry{
		{OwnerID: "a", Name: "Chris Olave", Position: "WR", LeaguePrice: 16},
		{OwnerID: "a", Name: "Kenneth Walker", Position: "RB", LeaguePrice: 46},
		{OwnerID: "a", Name: "Bench Guy", Position: "TE", LeaguePrice: 10,
			Flags: []string{FlagDidNotPlay}},
		{OwnerID: "b", Name: "George Pickens", Position: "WR", LeaguePrice: 19},
	}
}

// TestShareableKeepersLeaksNoAnalysis is the whole reason this is a separate
// renderer.
//
// The keeper report carries what a keeper would cost to win back, what he is
// worth on median projections, and which one to keep. Sending that to the
// league hands eleven opponents the model. What they need is the half that
// is theirs anyway: their players and what the rules charge.
func TestShareableKeepersLeaksNoAnalysis(t *testing.T) {
	var b strings.Builder
	names := Names{"a": "Aaron Jones Schadenfreude", "b": "Bishop Sycamore"}
	if err := WriteShareableKeepers(&b, shareEntries(), names, "2026", 2, 200); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, leak := range []string{"COST", "VALUE", "SAVES", "KEEP ", "recommend"} {
		if strings.Contains(got, leak) {
			t.Errorf("shareable output contains %q, which is your analysis:\n%s", leak, got)
		}
	}
}

// TestShareableKeepersCoversEveryTeam — it is sent to the league, so an
// owner missing from it is an owner who cannot check his own prices.
func TestShareableKeepersCoversEveryTeam(t *testing.T) {
	var b strings.Builder
	names := Names{"a": "Aaron Jones Schadenfreude", "b": "Bishop Sycamore"}
	if err := WriteShareableKeepers(&b, shareEntries(), names, "2026", 2, 200); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Aaron Jones Schadenfreude", "Bishop Sycamore",
		"Chris Olave", "Kenneth Walker", "George Pickens",
		"$16", "$46", "$19", "2026",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	// An ineligible player must not appear with a price, which would be a
	// price nobody can actually pay.
	if strings.Contains(got, "Bench Guy") {
		t.Errorf("an ineligible player was published:\n%s", got)
	}
}

// TestShareableKeepersOrdersByPrice — an owner reads his own block looking
// for the cheap ones, so cheapest first is the order that answers the
// question being asked.
func TestShareableKeepersOrdersByPrice(t *testing.T) {
	var b strings.Builder
	names := Names{"a": "Team A"}
	entries := []Entry{
		{OwnerID: "a", Name: "Dear", Position: "RB", LeaguePrice: 46},
		{OwnerID: "a", Name: "Cheap", Position: "WR", LeaguePrice: 10},
		{OwnerID: "a", Name: "Middle", Position: "TE", LeaguePrice: 20},
	}
	if err := WriteShareableKeepers(&b, entries, names, "2026", 2, 200); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Index(got, "Cheap") > strings.Index(got, "Middle") ||
		strings.Index(got, "Middle") > strings.Index(got, "Dear") {
		t.Errorf("expected cheapest first:\n%s", got)
	}
}

package main

import (
	"net/http/httptest"
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

// costed is sig with a market price, since best fit budgets against Cost.
func costed(id, name, pos, team string, value, cost int) draft.PlayerSignals {
	p := sig(id, name, pos, team, value)
	p.Cost = cost
	return p
}

// TestBestFitRespectsTheCap.
//
// The whole point of this block over the greedy one: it may only pick what the
// budget can actually buy. The greedy walk takes the best player regardless
// and reports the damage afterwards.
func TestBestFitRespectsTheCap(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Expensive", "RB", "DET", 90, 95),
		costed("2", "Affordable", "RB", "CIN", 40, 20),
	}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 30)

	for _, p := range fit.Picks {
		if p.Pick.Name == "Expensive" {
			t.Error("picked a $95 player against a $30 cap")
		}
	}
	if fit.Spend > 30 {
		t.Errorf("spend $%d over the $30 cap", fit.Spend)
	}
}

// It maximises value, not count: one good player beats two cheap ones when
// the money only stretches to one.
func TestBestFitPrefersValueOverCount(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Star", "RB", "DET", 80, 40),
		costed("2", "Filler A", "WR", "CIN", 5, 20),
		costed("3", "Filler B", "TE", "BUF", 5, 20),
	}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 40)

	got := map[string]bool{}
	for _, p := range fit.Picks {
		got[p.Pick.Name] = true
	}
	if !got["Star"] {
		t.Errorf("took %v, want the high-value player the cap affords", fit.Picks)
	}
}

// Exclusions bind here exactly as they do everywhere else: an affordable pair
// on one offense is still only one player.
func TestBestFitHonoursExclusions(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Back", "RB", "DET", 50, 10),
		costed("2", "Receiver", "WR", "DET", 50, 10),
	}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 100)

	det := 0
	for _, p := range fit.Picks {
		if p.Pick.Team == "DET" {
			det++
		}
	}
	if det > 1 {
		t.Errorf("took %d Lions; one per offense allows one", det)
	}
}

// TestBenchReserveCountsOnlyTheSlotsStartersWillNotFill.
//
// Measured against what is still unfilled on the roster already held, not the
// whole starting lineup — keepers have taken some of those spots, and counting
// them twice reserves too little. On the live board that was the difference
// between holding back $4 and the $6 the defense and five bench spots need.
func TestBenchReserveCountsOnlyTheSlotsStartersWillNotFill(t *testing.T) {
	srv := scratchServer(t)
	me := draft.MyState{OpenSlots: 12}

	empty := benchReserve(me, nil, srv.scoringBaselines(), srv.static.shape)
	held := []draft.PlayerSignals{
		sig("h1", "Kept Back", "RB", "MIA", 50),
		sig("h2", "Kept Receiver", "WR", "LAR", 50),
	}
	seeded := benchReserve(me, held, srv.scoringBaselines(), srv.static.shape)

	if seeded <= empty {
		t.Errorf("reserve %d with keepers against %d without: holding a starter "+
			"leaves fewer starting slots to fill, so more of the budget is bench",
			seeded, empty)
	}
}

// The control is a slider on a page, so an impossible number is clamped rather
// than rejected: mid-auction a sane line-up beats an error where one should be.
func TestPctParamClamps(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"", defaultBestFitPct}, {"bogus", defaultBestFitPct},
		{"50", 50}, {"0", 1}, {"-10", 1}, {"250", 100}, {"100", 100},
	} {
		r := httptest.NewRequest("GET", "/api/arbitrage?pct="+tc.raw, nil)
		if tc.raw == "" {
			r = httptest.NewRequest("GET", "/api/arbitrage", nil)
		}
		if got := pctParam(r, defaultBestFitPct); got != tc.want {
			t.Errorf("pct=%q -> %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// TestArbitrageIsCachedUntilTheBoardChanges.
//
// The page polls on the board's cadence, and the solve behind it costs twenty
// times what serving the board does — a beam search over every target. Without
// a cache that search runs sixty times a minute to produce the same answer,
// on the machine running the draft.
func TestArbitrageIsCachedUntilTheBoardChanges(t *testing.T) {
	srv := scratchServer(t)

	first := httptest.NewRecorder()
	srv.handleArbitrage(first, httptest.NewRequest("GET", "/api/arbitrage?pct=90", nil))
	if first.Code != 200 {
		t.Fatalf("first request: %d", first.Code)
	}
	srv.arbMu.Lock()
	cached := len(srv.arbCache)
	srv.arbMu.Unlock()
	if cached != 1 {
		t.Errorf("cache holds %d views after one request, want 1", cached)
	}

	// A different ceiling is a different answer and must not reuse the first.
	srv.handleArbitrage(httptest.NewRecorder(),
		httptest.NewRequest("GET", "/api/arbitrage?pct=50", nil))
	srv.arbMu.Lock()
	cached = len(srv.arbCache)
	srv.arbMu.Unlock()
	if cached != 2 {
		t.Errorf("cache holds %d views for two ceilings, want 2", cached)
	}

	// A rebuild is the board changing, which is exactly when the answer can
	// differ — every ceiling has to be dropped, not just the one that moved.
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	srv.arbMu.Lock()
	cached = len(srv.arbCache)
	srv.arbMu.Unlock()
	if cached != 0 {
		t.Errorf("cache kept %d views across a rebuild — the page would serve a stale board", cached)
	}
}

// A sale is a rebuild, so the page must not answer from the cache afterwards.
func TestSellingAPlayerDropsTheCachedView(t *testing.T) {
	srv := scratchServer(t)
	srv.handleArbitrage(httptest.NewRecorder(),
		httptest.NewRequest("GET", "/api/arbitrage", nil))

	srv.taken["1"] = gone{price: 40, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	srv.arbMu.Lock()
	defer srv.arbMu.Unlock()
	if len(srv.arbCache) != 0 {
		t.Error("a sale left the arbitrage view cached")
	}
}

// TestDollarBinPlayersCostADollar — no auction sells anyone for nothing.
//
// Cost is a market model and lands at zero for most of the board. Priced
// straight off it, a line-up leaning on dollar-bin players reads cheaper here
// than it could ever be bought for, by exactly the number of them it uses.
// The error is a dollar early and grows late, when the targets left are mostly
// dollar-bin and the budget is tightest.
func TestDollarBinPlayersCostADollar(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Free A", "RB", "DET", 6, 0),
		costed("2", "Free B", "WR", "CIN", 5, 0),
		costed("3", "Free C", "TE", "BUF", 4, 0),
	}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 50)

	if len(fit.Picks) == 0 {
		t.Fatal("took nobody from three affordable targets")
	}
	for _, p := range fit.Picks {
		if p.Pick.Cost < 1 {
			t.Errorf("%s priced at $%d; the minimum bid is $1", p.Pick.Name, p.Pick.Cost)
		}
	}
	if fit.Spend != len(fit.Picks) {
		t.Errorf("spend $%d for %d dollar-bin picks, want $%d",
			fit.Spend, len(fit.Picks), len(fit.Picks))
	}
}

// The floor binds the cap too: three free players against a $2 cap is two
// players, not three. Without it the search would take the whole board for
// nothing.
func TestTheDollarFloorBindsTheCap(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{
		costed("1", "Free A", "RB", "DET", 6, 0),
		costed("2", "Free B", "WR", "CIN", 5, 0),
		costed("3", "Free C", "TE", "BUF", 4, 0),
	}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, 2)

	if len(fit.Picks) > 2 {
		t.Errorf("took %d players against a $2 cap", len(fit.Picks))
	}
	if fit.Spend > 2 {
		t.Errorf("spend $%d over the $2 cap", fit.Spend)
	}
}

// TestTheCapKeepsADollarNearTheEnd — the percentage rounds a real dollar away.
//
// $7 left against a $6 bench reserve leaves $1 of room, and 90% of it truncates
// to nothing. Paired with the $1 asking-price floor that means an empty panel
// while the slots it is meant to fill are still open.
func TestTheCapKeepsADollarNearTheEnd(t *testing.T) {
	if got := spendCap(1, 90); got != 1 {
		t.Errorf("spendCap(room $1, 90%%) = $%d, want $1", got)
	}
	if got := spendCap(7, 90); got != 6 {
		t.Errorf("spendCap(room $7, 90%%) = $%d, want $6", got)
	}
	// No room is no cap: the floor is affordability, not optimism.
	if got := spendCap(0, 90); got != 0 {
		t.Errorf("spendCap(room $0, 90%%) = $%d, want $0", got)
	}
	if got := spendCap(-4, 90); got != 0 {
		t.Errorf("spendCap(negative room) = $%d, want $0", got)
	}
}

// And the search spends that dollar rather than returning nothing.
func TestBestFitBuysABodyWithItsLastDollar(t *testing.T) {
	srv := scratchServer(t)
	targets := []draft.PlayerSignals{costed("1", "Body", "WR", "DET", 3, 0)}

	fit := srv.bestFit(nil, targets, arbPrefs(), srv.scoringBaselines(), srv.static.shape, spendCap(1, 90))

	if len(fit.Picks) == 0 {
		t.Fatal("empty line against $1 of room with a dollar-bin target available")
	}
	if fit.Spend != 1 {
		t.Errorf("spend $%d, want $1", fit.Spend)
	}
}

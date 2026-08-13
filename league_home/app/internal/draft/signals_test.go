package draft

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestPlayerSignalsJSONKeys guards the contract with the web board: app.js
// reads these fields by their Go name, since PlayerSignals carries no json
// tags. Renaming a field without updating app.js would silently blank a
// column, so pin the keys the page depends on here.
func TestPlayerSignalsJSONKeys(t *testing.T) {
	b, err := json.Marshal(PlayerSignals{FPValue: 64, ECRRank: 1, SharpRankDelta: -6})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"FPValue"`, `"ECRRank"`, `"SharpRankDelta"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("PlayerSignals JSON is missing %s that app.js reads: %s", key, b)
		}
	}
}

func svRow(id, name, pos, baseline string, value, aav, ps float64, up, down bool) SourceRow {
	return SourceRow{
		Source: "subvertadown", PlayerID: id, Player: name, Position: pos,
		Baseline: baseline, AuctionValue: value, AAV: aav, ScarcityPct: ps,
		ECRUp: up, ECRDown: down,
	}
}

func signalFixture() []PlayerSignals {
	in := SignalInputs{
		Values: []PlayerValue{
			{PlayerID: "1", Name: "Jahmyr Gibbs", Position: "RB", Price: 76},
			{PlayerID: "2", Name: "Justin Jefferson", Position: "WR", Price: 50},
			{PlayerID: "3", Name: "George Kittle", Position: "TE", Price: 14},
			{PlayerID: "4", Name: "Nobody Known", Position: "WR", Price: 3},
		},
		Subvertadown: []SourceRow{
			svRow("1", "Jahmyr Gibbs", "RB", "beer", 73, 68, 93, false, false),
			svRow("1", "Jahmyr Gibbs", "RB", "beerplus", 71, 68, 93, false, false),
			svRow("1", "Jahmyr Gibbs", "RB", "vols", 91, 68, 93, false, false),
			svRow("2", "Justin Jefferson", "WR", "beerplus", 44, 47, 67, true, true),
			svRow("3", "George Kittle", "TE", "beerplus", 6, 3, 6, false, true),
		},
		CielyPoints:    map[string]float64{"1": 324, "2": 240, "3": 155},
		Availability:   map[string]string{"3": "PUP"},
		Costs:          map[string]int{"1": 72, "2": 50, "3": 4},
		RecommendedBid: 90,
		Leans: Leans{
			normalizeName("Jahmyr Gibbs"):     {Player: "Jahmyr Gibbs", Lean: LeanUp, Note: "believe"},
			normalizeName("George Kittle"):    {Player: "George Kittle", Lean: LeanDND, Note: "age"},
			normalizeName("Justin Jefferson"): {Player: "Justin Jefferson", Lean: LeanDown},
		},
	}
	return BuildSignals(in)
}

func find(t *testing.T, ps []PlayerSignals, name string) PlayerSignals {
	t.Helper()
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no signals for %s", name)
	return PlayerSignals{}
}

func TestBuildSignalsFoldsBaselinesIntoOneRow(t *testing.T) {
	ps := signalFixture()
	if len(ps) != 4 {
		t.Fatalf("expected one row per priced player, got %d", len(ps))
	}
	gibbs := find(t, ps, "Jahmyr Gibbs")
	if len(gibbs.VBD) != 3 {
		t.Errorf("expected 3 baselines, got %v", gibbs.VBD)
	}
	if gibbs.VBD[BaselineVOLS] != 91 || gibbs.VBD[BaselineBEER] != 73 {
		t.Errorf("baseline values wrong: %v", gibbs.VBD)
	}
	// AAV and PS% are per player, not per baseline.
	if gibbs.AAV != 68 || gibbs.ScarcityPct != 93 {
		t.Errorf("aav=%v ps=%v, want 68/93", gibbs.AAV, gibbs.ScarcityPct)
	}
}

// TestBuildSignalsJoinsFantasyPros — the second projection's value, rank, and
// sharp move ride onto each player, and a player FantasyPros is silent on
// keeps the zero read rather than a fabricated one.
func TestBuildSignalsJoinsFantasyPros(t *testing.T) {
	in := SignalInputs{
		Values: []PlayerValue{
			{PlayerID: "1", Name: "Bijan Robinson", Position: "RB", Price: 60},
			{PlayerID: "2", Name: "Jahmyr Gibbs", Position: "RB", Price: 66},
			{PlayerID: "3", Name: "Nobody Known", Position: "WR", Price: 3},
		},
		Costs: map[string]int{"1": 58, "2": 68, "3": 2},
		FantasyPros: map[string]FPRead{
			// Sharps rank Bijan higher than consensus (positive) and Gibbs
			// lower (negative) — the real 2026 flip.
			"1": {Value: 64, Rank: 1, SharpDelta: 6},
			"2": {Value: 61, Rank: 2, SharpDelta: -7},
		},
	}
	ps := BuildSignals(in)

	bijan := find(t, ps, "Bijan Robinson")
	if bijan.FPValue != 64 || bijan.ECRRank != 1 || bijan.SharpRankDelta != 6 {
		t.Errorf("Bijan FP join = $%d rank %d delta %d, want 64/1/6",
			bijan.FPValue, bijan.ECRRank, bijan.SharpRankDelta)
	}
	if bijan.Sharp() != SharpUp {
		t.Errorf("Bijan Sharp() = %v, want SharpUp (sharps rank him higher)", bijan.Sharp())
	}
	if gibbs := find(t, ps, "Jahmyr Gibbs"); gibbs.Sharp() != SharpDown {
		t.Errorf("Gibbs Sharp() = %v, want SharpDown", gibbs.Sharp())
	}
	nobody := find(t, ps, "Nobody Known")
	if nobody.FPValue != 0 || nobody.ECRRank != 0 || nobody.Sharp() != SharpNone {
		t.Errorf("uncovered player should carry no FP read, got $%d rank %d %v",
			nobody.FPValue, nobody.ECRRank, nobody.Sharp())
	}
}

// TestSharpRespectsThreshold — a move smaller than the threshold is noise and
// must not flag, in either direction.
func TestSharpRespectsThreshold(t *testing.T) {
	for _, tc := range []struct {
		delta int
		want  SharpState
	}{
		{6, SharpUp}, {5, SharpUp}, {4, SharpNone},
		{0, SharpNone}, {-4, SharpNone}, {-5, SharpDown}, {-7, SharpDown},
	} {
		if got := (PlayerSignals{SharpRankDelta: tc.delta}).Sharp(); got != tc.want {
			t.Errorf("Sharp(delta=%d) = %v, want %v", tc.delta, got, tc.want)
		}
	}
}

// TestBuildSignalsKeepsSourcesSeparate — nothing may be silently blended.
// The market price leads and the references sit beside it.
func TestBuildSignalsKeepsSourcesSeparate(t *testing.T) {
	gibbs := find(t, signalFixture(), "Jahmyr Gibbs")
	if gibbs.Value != 76 {
		t.Errorf("value = %d, want the re-solved 76", gibbs.Value)
	}
	if gibbs.Cost != 72 {
		t.Errorf("cost = %d, want 72 — a separate quantity from value", gibbs.Cost)
	}
	if gibbs.Edge() != 4 {
		t.Errorf("Edge = %d, want +4 (worth more than he costs)", gibbs.Edge())
	}
	if gibbs.AAV != 68 {
		t.Errorf("AAV should stay 68 alongside, got %v", gibbs.AAV)
	}
}

func TestBaselineSpreadFlagsFragileBuys(t *testing.T) {
	ps := signalFixture()
	// Gibbs swings $71 (BEER+) to $91 (VOLS): his price depends heavily
	// on where replacement level is drawn, which makes him fragile.
	if got := find(t, ps, "Jahmyr Gibbs").BaselineSpread(); got != 20 {
		t.Errorf("spread = %v, want 20", got)
	}
	// A single baseline has no spread, and must not report a false one.
	if got := find(t, ps, "Justin Jefferson").BaselineSpread(); got != 0 {
		t.Errorf("spread = %v, want 0 for a single baseline", got)
	}
	if got := find(t, ps, "Nobody Known").BaselineSpread(); got != 0 {
		t.Errorf("spread = %v, want 0 when no source covers him", got)
	}
}

func TestContestedSurvivesTheFold(t *testing.T) {
	ps := signalFixture()
	if !find(t, ps, "Justin Jefferson").Contested() {
		t.Error("Jefferson carries both ECR arrows and should read contested")
	}
	if find(t, ps, "George Kittle").ECR != ECRDownside {
		t.Error("Kittle carries only the down arrow")
	}
	if find(t, ps, "Jahmyr Gibbs").ECR != ECRConsensus {
		t.Error("Gibbs is covered and carries neither arrow, so consensus")
	}
}

func TestLeansApplyToMyBidNotTheMarket(t *testing.T) {
	ps := signalFixture()
	gibbs := find(t, ps, "Jahmyr Gibbs")
	if gibbs.Value != 76 {
		t.Errorf("a lean must not move the value, got %d", gibbs.Value)
	}
	if gibbs.MyMaxBid != 87 {
		t.Errorf("my max bid = %d, want 87 (76 x 1.15)", gibbs.MyMaxBid)
	}
	kittle := find(t, ps, "George Kittle")
	if kittle.Value != 14 || kittle.MyMaxBid != 0 {
		t.Errorf("dnd should zero my bid but leave the value at 14: %d/%d",
			kittle.Value, kittle.MyMaxBid)
	}
}

// TestBuildSignalsHandlesUncoveredPlayers — a player no source mentions
// still belongs on the board at his market price, with silence read as
// "no opinion" rather than "worthless".
func TestBuildSignalsHandlesUncoveredPlayers(t *testing.T) {
	p := find(t, signalFixture(), "Nobody Known")
	if p.Value != 3 || p.MyMaxBid != 3 {
		t.Errorf("uncovered player should keep his value, got %d/%d", p.Value, p.MyMaxBid)
	}
	// "Nobody looked" must not read as "everyone agrees".
	if p.AAV != 0 || p.ECR != ECRUnknown {
		t.Errorf("expected unknown references, got aav=%v ecr=%q", p.AAV, p.ECR)
	}
}

func TestAvailabilityCarriesThrough(t *testing.T) {
	if got := find(t, signalFixture(), "George Kittle").Availability; got != "PUP" {
		t.Errorf("availability = %q, want PUP", got)
	}
	if got := find(t, signalFixture(), "Jahmyr Gibbs").Availability; got != "" {
		t.Errorf("availability = %q, want empty for a healthy player", got)
	}
}

// TestEdgesRankBargainsFirst is the primary decision view: worth more than
// he costs, versus paying for something a median cannot see.
func TestEdgesRankBargainsFirst(t *testing.T) {
	got := Edges(signalFixture(), 1)
	if len(got) == 0 {
		t.Fatal("expected edges")
	}
	// Kittle: value 14 against cost 4 = +10, the best bargain here.
	if got[0].Name != "George Kittle" || got[0].Edge() != 10 {
		t.Errorf("best edge = %s %+d, want Kittle +10", got[0].Name, got[0].Edge())
	}
	// A player no market source covers has no cost and cannot have an edge.
	for _, p := range got {
		if p.Name == "Nobody Known" {
			t.Error("an uncovered player must not appear in the edge list")
		}
	}
}

func TestEdgesRespectMinimum(t *testing.T) {
	if got := Edges(signalFixture(), 100); len(got) != 0 {
		t.Errorf("expected nothing above a $100 threshold, got %d", len(got))
	}
}

func TestGapsSkipSilentSources(t *testing.T) {
	for _, g := range Gaps(signalFixture(), "aav", 1) {
		if g.Player == "Nobody Known" {
			t.Error("a silent source must not produce a gap")
		}
	}
}

func TestGapsRespectMinimum(t *testing.T) {
	if got := Gaps(signalFixture(), "aav", 100); len(got) != 0 {
		t.Errorf("expected no gaps above a $100 threshold, got %v", got)
	}
}

func TestGapsAgainstABaseline(t *testing.T) {
	gaps := Gaps(signalFixture(), string(BaselineVOLS), 1)
	// Only Gibbs carries a VOLS value: cost 72 against 91 = -19.
	if len(gaps) != 1 || gaps[0].Delta != -19 {
		t.Errorf("expected one -23 gap against VOLS, got %+v", gaps)
	}
}

func TestScarcityMeasuresDepthAndCliff(t *testing.T) {
	players := []PlayerSignals{
		{Name: "RB1", Position: "RB", CielyPoints: 320, ScarcityPct: 90},
		{Name: "RB2", Position: "RB", CielyPoints: 315, ScarcityPct: 80},
		{Name: "RB3", Position: "RB", CielyPoints: 250, ScarcityPct: 40},
		{Name: "WR1", Position: "WR", CielyPoints: 300, ScarcityPct: 95},
	}
	state := HitOrMissPool()
	// A pinned replacement level of 260 makes RB3 waiver fodder.
	baselines := map[string]float64{"RB": 260, "WR": 260}
	got := Scarcity(players, state, baselines)

	rb := got["RB"]
	if rb.Startable != 2 {
		t.Errorf("RB startable = %d, want 2 — RB3 sits below replacement", rb.Startable)
	}
	// The cliff is the biggest single drop among the top few: 315 -> 250.
	if rb.Cliff != 65 {
		t.Errorf("RB cliff = %v, want 65", rb.Cliff)
	}
	if rb.TopScarcityPct != 90 {
		t.Errorf("top PS%% = %v, want 90", rb.TopScarcityPct)
	}
	if got["WR"].Cliff != 0 {
		t.Errorf("a lone player has no cliff, got %v", got["WR"].Cliff)
	}
}

// TestScarcityCountsStartableNotBodies is the point of the measure.
//
// A position can hold a hundred players and still be empty: the ones below
// replacement are waiver fodder, and counting them makes the thinnest
// position on the board read as the deepest.
func TestScarcityCountsStartableNotBodies(t *testing.T) {
	players := []PlayerSignals{{Name: "WR1", Position: "WR", CielyPoints: 300}}
	for i := 0; i < 150; i++ {
		players = append(players, PlayerSignals{Name: "scrub", Position: "WR", CielyPoints: 40})
	}
	got := Scarcity(players, HitOrMissPool(), map[string]float64{"WR": 180})

	wr := got["WR"]
	if wr.Startable != 1 {
		t.Errorf("startable = %d, want 1 despite 151 bodies", wr.Startable)
	}
	if wr.StartersLeft <= wr.Startable {
		t.Fatalf("fixture needs more demand than supply, got %d spots", wr.StartersLeft)
	}
	if wr.Cover >= 1 {
		t.Errorf("cover = %v, want below 1 with one startable player", wr.Cover)
	}
}

// TestScarcityFallsAsAPositionIsPickedOver — measured against a pinned
// baseline the count decays, which is the whole signal. Against a baseline
// recomputed from the remaining pool it could not.
func TestScarcityFallsAsAPositionIsPickedOver(t *testing.T) {
	baselines := map[string]float64{"RB": 200}
	full := []PlayerSignals{
		{Name: "RB1", Position: "RB", CielyPoints: 320},
		{Name: "RB2", Position: "RB", CielyPoints: 300},
		{Name: "RB3", Position: "RB", CielyPoints: 260},
		{Name: "RB4", Position: "RB", CielyPoints: 120},
	}
	before := Scarcity(full, HitOrMissPool(), baselines)["RB"]
	after := Scarcity(full[2:], HitOrMissPool(), baselines)["RB"]

	if before.Startable != 3 {
		t.Errorf("before = %d startable, want 3", before.Startable)
	}
	if after.Startable != 1 {
		t.Errorf("after two backs went = %d startable, want 1", after.Startable)
	}
	if after.Cover >= before.Cover {
		t.Errorf("cover must fall as the position empties: %v -> %v", before.Cover, after.Cover)
	}
}

// TestScarcityCarriesTheThresholdItCounted — the draft board filters rows
// on Threshold and prints Startable next to them. If the two ever came from
// different lines the board would show "8 startable" above a list of nine,
// and the number that is supposed to be reassuring becomes the thing you
// stop trusting mid-auction.
func TestScarcityCarriesTheThresholdItCounted(t *testing.T) {
	players := []PlayerSignals{
		{Name: "RB1", Position: "RB", CielyPoints: 320},
		{Name: "RB2", Position: "RB", CielyPoints: 260},
		// Exactly at the line: meet-or-exceed counts him, so a consumer
		// filtering with >= has to land on the same answer.
		{Name: "RB3", Position: "RB", CielyPoints: 200},
		{Name: "RB4", Position: "RB", CielyPoints: 199},
		{Name: "WR1", Position: "WR", CielyPoints: 300},
		{Name: "WR2", Position: "WR", CielyPoints: 100},
	}
	thresholds := map[string]float64{"RB": 200, "WR": 180}
	got := Scarcity(players, HitOrMissPool(), thresholds)

	for pos, want := range thresholds {
		s := got[pos]
		if s.Threshold != want {
			t.Errorf("%s threshold = %v, want %v", pos, s.Threshold, want)
		}
		above := 0
		for _, p := range players {
			if p.Position == pos && p.CielyPoints >= s.Threshold {
				above++
			}
		}
		if s.Startable != above {
			t.Errorf("%s: startable = %d but %d players sit at or above the "+
				"reported threshold %v", pos, s.Startable, above, s.Threshold)
		}
	}
}

// biasFixture is a board where one whole position is lifted by a source
// artifact, plus one player who is genuinely better than his position.
func biasFixture() []PlayerSignals {
	var out []PlayerSignals
	// Every TE reads about +14 because the value source prices the
	// position far deeper than the market does. None of it is real.
	for i, edge := range []int{15, 14, 13, 14} {
		out = append(out, PlayerSignals{
			Name: fmt.Sprintf("TE%d", i+1), Position: "TE",
			Cost: 10, Value: 10 + edge,
		})
	}
	// Running backs sit near zero, so a standout there is genuine.
	for i, edge := range []int{1, 0, -1, 12} {
		out = append(out, PlayerSignals{
			Name: fmt.Sprintf("RB%d", i+1), Position: "RB",
			Cost: 40, Value: 40 + edge,
		})
	}
	return out
}

func TestPositionalBiasFindsTheCommonComponent(t *testing.T) {
	bias := PositionalBias(biasFixture())
	if bias["TE"] != 14 {
		t.Errorf("TE bias = %v, want the median 14", bias["TE"])
	}
	if bias["RB"] != 0.5 {
		t.Errorf("RB bias = %v, want the median 0.5", bias["RB"])
	}
}

// TestAdjustedEdgeStripsThePositionalStory is the point of the whole
// exercise: a +15 tight end on a board where every tight end reads +14 is
// not a bargain, and a +12 back on a flat position is.
func TestAdjustedEdgeStripsThePositionalStory(t *testing.T) {
	players := biasFixture()
	bias := PositionalBias(players)

	ranked := RankByAdjustedEdge(players, bias, 0)
	if ranked[0].Name != "RB4" {
		t.Errorf("best adjusted edge = %s, want RB4 — the only genuine outlier", ranked[0].Name)
	}

	byName := map[string]PlayerSignals{}
	for _, p := range players {
		byName[p.Name] = p
	}
	// Raw edge says the tight end is the better buy...
	if byName["TE1"].Edge() <= byName["RB4"].Edge() {
		t.Fatal("fixture no longer demonstrates the trap")
	}
	// ...and adjusting says the opposite.
	if AdjustedEdge(byName["TE1"], bias) >= AdjustedEdge(byName["RB4"], bias) {
		t.Errorf("adjustment failed: TE1 %.1f vs RB4 %.1f",
			AdjustedEdge(byName["TE1"], bias), AdjustedEdge(byName["RB4"], bias))
	}
}

func TestPositionalBiasIgnoresUncoveredPlayers(t *testing.T) {
	players := append(biasFixture(), PlayerSignals{Name: "Ghost", Position: "TE", Cost: 0, Value: 99})
	if got := PositionalBias(players)["TE"]; got != 14 {
		t.Errorf("a player with no cost must not shift the bias: %v", got)
	}
}

func TestRankByAdjustedEdgeRespectsMinimum(t *testing.T) {
	players := biasFixture()
	bias := PositionalBias(players)
	if got := RankByAdjustedEdge(players, bias, 100); len(got) != 0 {
		t.Errorf("expected nothing above a 100 threshold, got %d", len(got))
	}
}

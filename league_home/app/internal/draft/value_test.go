package draft

import (
	"fmt"
	"strings"
	"testing"
)

// projections builds n players at a position on a simple descending curve.
func projections(pos string, n int, top, step float64) []Projection {
	out := make([]Projection, n)
	for i := 0; i < n; i++ {
		out[i] = Projection{
			PlayerID: fmt.Sprintf("%s%d", pos, i+1),
			Name:     fmt.Sprintf("%s %d", pos, i+1),
			Position: pos,
			Points:   top - float64(i)*step,
		}
	}
	return out
}

func fullBoard() []Projection {
	var out []Projection
	out = append(out, projections("QB", 30, 380, 6)...)
	out = append(out, projections("RB", 60, 330, 4)...)
	out = append(out, projections("WR", 90, 310, 3)...)
	out = append(out, projections("TE", 30, 200, 4)...)
	return out
}

func testPool(dollars, slots int) PoolState {
	p := HitOrMissPool()
	p.Dollars, p.Slots = dollars, slots
	return p
}

// TestSolveConservesThePool is the invariant that makes the board
// trustworthy: the players who will actually be rostered account for every
// dollar in the room, no more and no fewer.
//
// The sum is taken over the top `slots` prices rather than the whole board,
// because a 210-player board and 60 open spots means 150 of those players
// are never bought — they sit at the $1 floor and are not claims on the
// pool.
func TestSolveConservesThePool(t *testing.T) {
	for _, tc := range []struct{ dollars, slots int }{
		{2400, 168}, {1903, 146}, {1977, 151}, {500, 60},
	} {
		vals, err := Solve(fullBoard(), testPool(tc.dollars, tc.slots))
		if err != nil {
			t.Fatal(err)
		}
		rostered := tc.slots
		if rostered > len(vals) {
			rostered = len(vals)
		}
		total := 0
		for _, v := range vals[:rostered] {
			total += v.Price
		}
		if total != tc.dollars {
			t.Errorf("pool $%d over %d slots: rostered prices sum to $%d", tc.dollars, tc.slots, total)
		}
	}
}

// TestSolveLeavesUndraftablePlayersAtADollar — players deeper than the
// league can roster are not competing for the pool.
func TestSolveLeavesUndraftablePlayersAtADollar(t *testing.T) {
	vals, err := Solve(fullBoard(), testPool(2400, 60))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vals[60:] {
		if v.Price != 1 {
			t.Fatalf("%s is beyond the rostered pool but priced $%d", v.Name, v.Price)
		}
	}
}

func TestSolveNeverPricesBelowADollar(t *testing.T) {
	vals, err := Solve(fullBoard(), testPool(1903, 146))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vals {
		if v.Price < 1 {
			t.Fatalf("%s priced at $%d", v.Name, v.Price)
		}
	}
}

// TestSurplusConcentratesAtTheTop is the behaviour a scalar inflation
// factor cannot produce. Adding money to the room cannot raise a $1 player,
// so it has to land on the best players — and proportionally more of it
// than a uniform multiplier would give them.
func TestSurplusConcentratesAtTheTop(t *testing.T) {
	board := fullBoard()
	lean, err := Solve(board, testPool(1500, 168))
	if err != nil {
		t.Fatal(err)
	}
	rich, err := Solve(board, testPool(2400, 168))
	if err != nil {
		t.Fatal(err)
	}

	leanTop, richTop := lean[0].Price, rich[0].Price
	if richTop <= leanTop {
		t.Fatalf("top price should rise with the pool: $%d -> $%d", leanTop, richTop)
	}
	// The pool grew 60%; the top player should gain more than that, since
	// the floor players absorbed none of it.
	poolGrowth := 2400.0 / 1500.0
	topGrowth := float64(richTop) / float64(leanTop)
	if topGrowth <= poolGrowth {
		t.Errorf("top grew %.2fx vs pool %.2fx — surplus is not concentrating", topGrowth, poolGrowth)
	}
}

// TestRemovingKeepersLowersReplacementLevel is the additive shift a scalar
// cannot represent: take the best backs off the board and the replacement
// back is worse, so every remaining back gains value.
func TestRemovingKeepersLowersReplacementLevel(t *testing.T) {
	board := fullBoard()
	state := testPool(2400, 168)

	before := ReplacementLevels(board, state)

	// Remove the top 12 RBs, as a keeper season would.
	var thinner []Projection
	removed := 0
	for _, p := range board {
		if p.Position == "RB" && removed < 12 {
			removed++
			continue
		}
		thinner = append(thinner, p)
	}
	after := ReplacementLevels(thinner, state)

	if after["RB"] >= before["RB"] {
		t.Errorf("RB replacement level should drop: %.1f -> %.1f", before["RB"], after["RB"])
	}
	// Positions untouched by the removal must not move.
	if after["WR"] != before["WR"] {
		t.Errorf("WR replacement level moved without cause: %.1f -> %.1f", before["WR"], after["WR"])
	}
}

// TestBaselineDepthOrdersTheCurves checks the documented tradeoff: VOLS
// values against the last starter and so concentrates hardest on the top,
// while deeper baselines spread money toward depth.
func TestBaselineDepthOrdersTheCurves(t *testing.T) {
	board := fullBoard()
	top := map[Baseline]int{}
	for _, b := range []Baseline{BaselineVOLS, BaselineBEERPlus, BaselineBEER} {
		state := testPool(2400, 168)
		state.Baseline = b
		vals, err := Solve(board, state)
		if err != nil {
			t.Fatal(err)
		}
		top[b] = vals[0].Price
	}
	if !(top[BaselineVOLS] > top[BaselineBEERPlus] && top[BaselineBEERPlus] > top[BaselineBEER]) {
		t.Errorf("expected VOLS > BEER+ > BEER at the top of the board, got %v", top)
	}
}

// TestFlexDeepensReplacementLevel — the flex slot means the league starts
// more RB/WR/TE than the base requirements imply, so replacement sits
// deeper than starters alone would put it.
func TestFlexDeepensReplacementLevel(t *testing.T) {
	board := fullBoard()
	with := testPool(2400, 168)
	without := testPool(2400, 168)
	without.FlexSlots = 0

	a := ReplacementLevels(board, with)
	b := ReplacementLevels(board, without)
	if a["RB"] >= b["RB"] {
		t.Errorf("flex should deepen RB replacement: with=%.1f without=%.1f", a["RB"], b["RB"])
	}
	// QB is not flex-eligible and must be unaffected.
	if a["QB"] != b["QB"] {
		t.Errorf("QB replacement should ignore flex: %.1f vs %.1f", a["QB"], b["QB"])
	}
}

// TestFilledSlotsHoldReplacementLevelSteady is the keeper case, and the
// bug it guards is subtle: keeping a player removes him from supply *and*
// removes the slot he fills from demand. Count only the first and the
// baseline sinks — you end up asking for the 36th-best available receiver
// when just 33 are still needed, which flattens the whole board.
func TestFilledSlotsHoldReplacementLevelSteady(t *testing.T) {
	board := fullBoard()

	// Take the top 6 WRs off the board as keepers, and record that they
	// filled 6 WR starting slots.
	var remaining []Projection
	removed := 0
	for _, p := range board {
		if p.Position == "WR" && removed < 6 {
			removed++
			continue
		}
		remaining = append(remaining, p)
	}

	full := testPool(2400, 168)
	afterKeepers := testPool(2000, 162)
	afterKeepers.Filled = map[string]int{"WR": 6}

	before := ReplacementLevels(board, full)
	after := ReplacementLevels(remaining, afterKeepers)
	if before["WR"] != after["WR"] {
		t.Errorf("WR replacement should hold when supply and demand fall together: %.1f -> %.1f",
			before["WR"], after["WR"])
	}

	// Without the Filled accounting the baseline drops, which is the bug.
	naive := testPool(2000, 162)
	if ReplacementLevels(remaining, naive)["WR"] >= before["WR"] {
		t.Error("expected the unaccounted case to sink the baseline; the test no longer proves anything")
	}
}

// TestKeeperRemovalInflatesRemainingPlayers — keepers are bought below
// market, so more value leaves the pool than money does, and what is left
// must cost more. This is the effect a scalar inflation factor exists to
// approximate; here it falls out of re-solving.
func TestKeeperRemovalInflatesRemainingPlayers(t *testing.T) {
	board := fullBoard()
	var remaining []Projection
	filled := map[string]int{}
	for i, p := range board {
		// Take a spread of mid-value players, as real keepers are.
		if i%7 == 3 && filled[p.Position] < 6 {
			filled[p.Position]++
			continue
		}
		remaining = append(remaining, p)
	}
	kept := 0
	for _, n := range filled {
		kept += n
	}

	full := testPool(2400, 168)
	after := testPool(2400-kept*20, 168-kept)
	after.Filled = filled

	a, err := Solve(board, full)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Solve(remaining, after)
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Price <= a[0].Price {
		t.Errorf("top price should rise once cheap keepers leave the pool: $%d -> $%d",
			a[0].Price, b[0].Price)
	}
}

func TestReplacementHandlesExhaustedPosition(t *testing.T) {
	// Only 5 RBs available but the league starts 24 plus flex — the pool
	// is exhausted and the worst available player defines replacement.
	board := projections("RB", 5, 300, 10)
	got := ReplacementLevels(board, testPool(2400, 168))
	if got["RB"] != 260 {
		t.Errorf("replacement = %.1f, want the worst available (260)", got["RB"])
	}
}

func TestSolveRejectsBadPool(t *testing.T) {
	bad := testPool(2400, 168)
	bad.Teams = 0
	if _, err := Solve(fullBoard(), bad); err == nil {
		t.Error("expected an error for a zero team count")
	}
	neg := testPool(2400, -1)
	if _, err := Solve(fullBoard(), neg); err == nil {
		t.Error("expected an error for negative slots")
	}
}

func TestSolveEmptyBoard(t *testing.T) {
	vals, err := Solve(nil, testPool(2400, 168))
	if err != nil || vals != nil {
		t.Errorf("empty board should return nothing without erroring: %v, %v", vals, err)
	}
}

// TestSolveHandlesMoreSlotsThanDollars — late in a draft the room can be
// nearly broke with spots left. Everyone should floor to $1 and the total
// must still match what's actually there.
func TestSolveHandlesMoreSlotsThanDollars(t *testing.T) {
	board := projections("WR", 10, 200, 5)
	vals, err := Solve(board, testPool(3, 10))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, v := range vals {
		if v.Price < 1 {
			t.Fatalf("%s priced below $1", v.Name)
		}
		total += v.Price
	}
	// Ten players cannot cost less than $10 even with $3 in the room; the
	// floor wins, and the board says so rather than inventing a discount.
	if total != 10 {
		t.Errorf("total = $%d, want $10 (the floor for 10 players)", total)
	}
}

func marketBoard() []MarketPrice {
	// A national-market shape: steep at the top, long cheap tail.
	var out []MarketPrice
	for i := 0; i < 200; i++ {
		aav := 70.0 - float64(i)*0.9
		if aav < 1 {
			aav = 1
		}
		pos := []string{"RB", "WR", "TE", "QB"}[i%4]
		out = append(out, MarketPrice{
			PlayerID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("P%d", i),
			Position: pos, AAV: aav,
		})
	}
	return out
}

func TestSolveCostConservesThePool(t *testing.T) {
	for _, tc := range []struct{ dollars, slots int }{{2400, 168}, {2055, 153}, {900, 90}} {
		got, err := SolveCost(marketBoard(), testPool(tc.dollars, tc.slots))
		if err != nil {
			t.Fatal(err)
		}
		rostered := tc.slots
		if rostered > len(got) {
			rostered = len(got)
		}
		total := 0
		for _, m := range got[:rostered] {
			total += m.Cost
		}
		if total != tc.dollars {
			t.Errorf("pool $%d/%d slots: rostered costs sum to $%d", tc.dollars, tc.slots, total)
		}
	}
}

// TestSolveCostScalesToTheRoom — a room with more money per slot than the
// national market pays more, with no separate premium multiplier.
func TestSolveCostScalesToTheRoom(t *testing.T) {
	lean, err := SolveCost(marketBoard(), testPool(1500, 153))
	if err != nil {
		t.Fatal(err)
	}
	rich, err := SolveCost(marketBoard(), testPool(2400, 153))
	if err != nil {
		t.Fatal(err)
	}
	if rich[0].Cost <= lean[0].Cost {
		t.Errorf("a richer room should cost more at the top: $%d vs $%d", rich[0].Cost, lean[0].Cost)
	}
}

// TestCostAndValueCanDisagree is the whole reason they are separate
// quantities: a player the market loves and the median does not.
func TestCostAndValueCanDisagree(t *testing.T) {
	state := testPool(2400, 168)

	// One player: unremarkable median, top-of-market AAV.
	proj := append(projections("RB", 40, 200, 1),
		Projection{PlayerID: "hype", Name: "Hyped Back", Position: "RB", Points: 180})
	values, err := Solve(proj, state)
	if err != nil {
		t.Fatal(err)
	}
	var value int
	for _, v := range values {
		if v.PlayerID == "hype" {
			value = v.Price
		}
	}

	var market []MarketPrice
	for _, p := range proj {
		aav := 5.0
		if p.PlayerID == "hype" {
			aav = 70
		}
		market = append(market, MarketPrice{PlayerID: p.PlayerID, Name: p.Name, Position: p.Position, AAV: aav})
	}
	costs, err := SolveCost(market, state)
	if err != nil {
		t.Fatal(err)
	}
	var cost int
	for _, m := range costs {
		if m.PlayerID == "hype" {
			cost = m.Cost
		}
	}

	if cost <= value {
		t.Errorf("the market should price him above his median value: cost $%d, value $%d", cost, value)
	}
}

// TestSolveRejectsUnknownBaseline guards a silent-wrong-answer: an
// unrecognized curve finds no entry in the depth table, reads as zero, and
// prices the whole board as VOLS while looking perfectly normal.
func TestSolveRejectsUnknownBaseline(t *testing.T) {
	state := testPool(2400, 168)
	state.Baseline = "nonsense"
	_, err := Solve(fullBoard(), state)
	if err == nil {
		t.Fatal("expected an error for an unknown baseline")
	}
	for _, want := range []string{"nonsense", "beerplus"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name the bad value and the valid ones: %v", err)
		}
	}
	for _, b := range Baselines() {
		if !b.Valid() {
			t.Errorf("%q should be valid", b)
		}
	}
	if Baseline("").Valid() {
		t.Error("the empty baseline must not be valid")
	}
}

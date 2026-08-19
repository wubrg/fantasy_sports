package draft

import (
	"fmt"
	"math"
	"sort"
)

// Baseline selects how deep the replacement level sits, which sets how
// steeply value concentrates on starters.
//
// Subvertadown's guide frames the tradeoff: VOLS values a player against
// the last starter, which inflates the top of the board and leaves almost
// nothing for the bench; deeper baselines spread money toward depth. BEER+
// is their recommended default because it balances strong starters against
// a de-risked bench.
//
// This is a live strategy switch rather than configuration — which curve
// you want can reasonably change mid-draft once you see whether the room
// is paying up at the top or hunting value.
type Baseline string

const (
	// BaselineVOLS is value over the last starter.
	BaselineVOLS Baseline = "vols"
	// BaselineBEER sits a full bench round deeper than VOLS.
	BaselineBEER Baseline = "beer"
	// BaselineBEERPlus splits the difference and is the default.
	BaselineBEERPlus Baseline = "beerplus"
)

// benchRoundsBehindStarters is how many additional rounds of players past
// the last starter each baseline reaches before calling someone
// replacement level, expressed per team.
//
// These depths are approximations of Subvertadown's published curves, not
// their proprietary formulas. They are deliberately explicit and adjustable
// so the shape can be checked against their sheets rather than trusted.
var benchRoundsBehindStarters = map[Baseline]float64{
	BaselineVOLS:     0,
	BaselineBEERPlus: 0.5,
	BaselineBEER:     1,
}

// Valid reports whether b is a baseline this tool knows how to price.
//
// Worth checking explicitly: an unrecognized baseline would otherwise find
// no entry in the depth table, read as zero, and silently price the whole
// board as VOLS — a wrong answer that looks exactly like a right one.
func (b Baseline) Valid() bool {
	_, ok := benchRoundsBehindStarters[b]
	return ok
}

// Baselines lists the supported curves, for error messages and help text.
func Baselines() []Baseline {
	return []Baseline{BaselineBEER, BaselineBEERPlus, BaselineVOLS}
}

// PlayerValue is one player's projection and the price the current pool
// implies for him.
type PlayerValue struct {
	PlayerID string
	Name     string
	Position string
	Points   float64
	// Baseline is the replacement-level points for his position under the
	// active curve and the currently available pool.
	BaselinePts float64
	// VOB is points above that baseline, floored at zero.
	VOB float64
	// Price is his share of the money actually left in the room.
	Price int
	// PointsLow and PointsHigh carry the source's own projection range
	// through the solve unpriced, for callers that want to read the range
	// against the price. Zero where the source published a single number.
	PointsLow  float64
	PointsHigh float64
}

// PriceBand converts a projection's own point range into a dollar range at
// this player's marginal rate — the dollars per point above replacement that
// his solved price implies.
//
// Deliberately not a second and third call to Solve at the low and the high.
// Solve shares out a fixed pot, so moving every player to his high at once
// inflates the whole pool and the prices barely move: that answers "what if
// everyone overperformed", which is not the question. The question is what
// this player's range is worth against a pool priced at its median, and that
// is what the marginal rate gives. It is an approximation, and a linear one —
// good enough to read a range from, not a number to bid to.
//
// Returns zeroes where there is no band or no room above replacement to
// measure a rate against.
func (v PlayerValue) PriceBand() (low, high int) {
	if v.VOB <= 0 || v.PointsHigh <= 0 || v.PointsLow <= 0 {
		return 0, 0
	}
	rate := float64(v.Price) / v.VOB
	low = int(math.Round(float64(v.Price) - (v.Points-v.PointsLow)*rate))
	high = int(math.Round(float64(v.Price) + (v.PointsHigh-v.Points)*rate))
	if low < 1 {
		low = 1
	}
	if high < low {
		high = low
	}
	return low, high
}

// PoolState is everything the valuation depends on. Change any of it and
// the whole board is re-solved; there is no cached base table to scale.
type PoolState struct {
	// Dollars is the money still in the room: every team's budget less
	// what keepers cost them, less what has already been spent.
	Dollars int
	// Slots is the number of roster spots still to be filled league-wide.
	// Each one reserves $1, which is why surplus concentrates upward.
	Slots int
	// Teams is the league size, used to size replacement level.
	Teams int
	// Starters maps position to starting slots per team, flex excluded.
	Starters map[string]int
	// FlexPositions are the positions eligible for the flex slot, and
	// FlexSlots how many each team starts.
	FlexPositions []string
	FlexSlots     int
	// Filled counts starting spots already taken league-wide by position,
	// through keepers or picks already made.
	//
	// This has to be tracked or replacement level drifts too deep. Keeping
	// a player removes him from supply *and* removes the slot he fills
	// from demand; counting only the first would ask for the 36th-best
	// available receiver when just 33 are still needed, dragging the
	// baseline down and flattening the whole board.
	Filled map[string]int
	// Baseline is the active curve.
	Baseline Baseline
}

// HitOrMissPool returns the league's roster shape with dollars and slots
// left for the caller to fill in.
func HitOrMissPool() PoolState {
	return PoolState{
		Teams:         12,
		Starters:      map[string]int{"QB": 1, "RB": 2, "WR": 3, "TE": 1},
		FlexPositions: []string{"RB", "WR", "TE"},
		FlexSlots:     1,
		Baseline:      BaselineBEERPlus,
	}
}

// Projection is a player's projected points, the only input the valuation
// needs about him.
//
// Points come from a projection source restated in league scoring — Ciely's
// median projections are the current source, used for points rather than
// his dollars, since his dollars are a linear map of these same points
// against a full pool and cannot see keeper effects.
type Projection struct {
	PlayerID string
	Name     string
	Position string
	Points   float64
	// PointsLow and PointsHigh bound the projection where the source
	// published a range, both zero where it published only a median. They ride
	// through Solve untouched: the solve prices the pool at its median, and the
	// band is read against that price rather than solved for separately. See
	// FPBand in signals.go for why.
	PointsLow  float64
	PointsHigh float64
}

// Solve prices every available player against the money and roster slots
// actually left in the room.
//
// This replaces the more common "compute a base table, then multiply by an
// inflation factor" approach, which is an approximation of exactly this
// calculation. Two things a scalar gets wrong and this does not: a $1
// player cannot absorb inflation, so surplus has to concentrate on the top
// of the board; and removing players from the pool moves replacement level,
// which shifts value additively within a position rather than scaling it.
func Solve(available []Projection, state PoolState) ([]PlayerValue, error) {
	if state.Teams <= 0 {
		return nil, fmt.Errorf("draft: pool needs a positive team count, got %d", state.Teams)
	}
	if state.Slots < 0 {
		return nil, fmt.Errorf("draft: pool has negative slots (%d)", state.Slots)
	}
	if !state.Baseline.Valid() {
		return nil, fmt.Errorf("draft: unknown baseline %q (use %v)", state.Baseline, Baselines())
	}
	if len(available) == 0 {
		return nil, nil
	}

	baselines := ReplacementLevels(available, state)

	values := make([]PlayerValue, 0, len(available))
	var totalVOB float64
	for _, p := range available {
		base := baselines[p.Position]
		vob := p.Points - base
		if vob < 0 {
			vob = 0
		}
		totalVOB += vob
		values = append(values, PlayerValue{
			PlayerID: p.PlayerID, Name: p.Name, Position: p.Position,
			Points: p.Points, BaselinePts: base, VOB: vob,
			PointsLow: p.PointsLow, PointsHigh: p.PointsHigh,
		})
	}

	prices := allocate(values, state, func(v PlayerValue) float64 { return v.VOB })
	for i := range values {
		values[i].Price = prices[i]
	}

	sort.SliceStable(values, func(i, j int) bool { return values[i].Price > values[j].Price })
	return values, nil
}

// allocate shares a pool across players in proportion to some weight,
// reserving a dollar for every roster spot.
//
// Both the value board and the cost board run through here so they obey the
// same two rules: every remaining spot costs at least a dollar, and only as
// many players as there are spots actually compete for the rest. Players
// deeper than the league can roster sit at the floor, because nobody has to
// bid for them.
func allocate[T any](items []T, state PoolState, weight func(T) float64) []int {
	prices := make([]int, len(items))
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return weight(items[order[a]]) > weight(items[order[b]]) })

	rostered := state.Slots
	if rostered > len(items) {
		rostered = len(items)
	}

	var total float64
	for i := 0; i < rostered; i++ {
		if w := weight(items[order[i]]); w > 0 {
			total += w
		}
	}

	surplus := state.Dollars - state.Slots
	if surplus < 0 {
		surplus = 0
	}

	assigned := 0
	for rank, idx := range order {
		prices[idx] = 1
		if rank < rostered && total > 0 {
			if w := weight(items[idx]); w > 0 {
				prices[idx] += int(float64(surplus) * w / total)
			}
		}
		if rank < rostered {
			assigned += prices[idx]
		}
	}

	// Integer truncation leaves a few dollars unallocated; hand them to the
	// best players, which is where a real auction puts them.
	for n := 0; n < state.Dollars-assigned && rostered > 0; n++ {
		prices[order[n%rostered]]++
	}
	return prices
}

// ReplacementLevels returns the points a replacement-level player scores at
// each position, given who is still available and how deep the active
// baseline reaches.
//
// This is why the board must be re-solved rather than scaled: as players
// leave the pool the replacement player gets worse, the baseline drops, and
// every remaining player at that position gains the same amount of value.
func ReplacementLevels(available []Projection, state PoolState) map[string]float64 {
	byPos := map[string][]float64{}
	for _, p := range available {
		byPos[p.Position] = append(byPos[p.Position], p.Points)
	}
	for pos := range byPos {
		sort.Sort(sort.Reverse(sort.Float64Slice(byPos[pos])))
	}

	depth := replacementDepth(state)
	out := map[string]float64{}
	for pos, points := range byPos {
		n := depth[pos]
		switch {
		case n <= 0:
			// No starting demand at this position, so anyone available is
			// replacement level.
			out[pos] = points[0]
		case n >= len(points):
			// Demand outruns supply: the pool is exhausted, so the worst
			// available player defines replacement.
			out[pos] = points[len(points)-1]
		default:
			out[pos] = points[n-1]
		}
	}
	return out
}

// replacementDepth is how many players deep at each position the league
// consumes before reaching replacement level, under the active baseline.
func replacementDepth(state PoolState) map[string]int {
	depth := map[string]int{}
	for pos, perTeam := range state.Starters {
		depth[pos] = perTeam * state.Teams
	}

	// Flex demand is shared, so split it across the eligible positions in
	// proportion to how many starters each already claims. A league that
	// starts 3 WR absorbs more of the flex than one starting 1 TE.
	if state.FlexSlots > 0 && len(state.FlexPositions) > 0 {
		var totalStarters int
		for _, pos := range state.FlexPositions {
			totalStarters += state.Starters[pos]
		}
		flexTotal := state.FlexSlots * state.Teams
		for _, pos := range state.FlexPositions {
			share := 1.0 / float64(len(state.FlexPositions))
			if totalStarters > 0 {
				share = float64(state.Starters[pos]) / float64(totalStarters)
			}
			depth[pos] += int(float64(flexTotal) * share)
		}
	}

	// Then push past the last starter by however deep the baseline reaches.
	rounds := benchRoundsBehindStarters[state.Baseline]
	if rounds > 0 {
		for pos := range depth {
			depth[pos] += int(rounds * float64(state.Teams))
		}
	}

	// Spots already filled are demand that no longer has to be bought, so
	// they come off the depth. Without this the baseline sinks as players
	// leave the board and the curve flattens for no real reason.
	for pos, filled := range state.Filled {
		if d, ok := depth[pos]; ok {
			depth[pos] = max(0, d-filled)
		}
	}
	return depth
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MarketPrice is one player's expected auction price in this room.
type MarketPrice struct {
	PlayerID string
	Name     string
	Position string
	// AAV is the national average auction value he was priced from.
	AAV float64
	// Cost is that restated against this room's actual money and slots.
	Cost int
}

// SolveCost estimates what players will actually go for, as distinct from
// what they are worth.
//
// Value and cost are different questions and must not share a number. Value
// comes from median projections and answers "pay this and you should get
// this production". Cost comes from what humans actually pay and answers
// "this is what it will take to win him". They diverge sharply wherever the
// market is buying a range of outcomes that a median cannot see — Ashton
// Jeanty is worth $24 on Ciely's median and costs $43 in real drafts.
//
// The national AAV is the input; running it through the same pool
// allocation as the value board converts it to this room, which is both
// smaller than a full pool after keepers and spent by twelve specific
// people. No separate league-premium multiplier is needed: if this room has
// more money per slot than the national average, prices scale up on their
// own.
func SolveCost(available []MarketPrice, state PoolState) ([]MarketPrice, error) {
	if state.Slots < 0 {
		return nil, fmt.Errorf("draft: pool has negative slots (%d)", state.Slots)
	}
	if len(available) == 0 {
		return nil, nil
	}
	out := make([]MarketPrice, len(available))
	copy(out, available)

	prices := allocate(out, state, func(m MarketPrice) float64 { return m.AAV })
	for i := range out {
		out[i].Cost = prices[i]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out, nil
}

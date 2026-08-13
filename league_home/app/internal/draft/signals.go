package draft

import (
	"sort"
)

// PlayerSignals is every read on one player, gathered in one row.
//
// Nothing here is blended into a single score. Each source measures a
// different thing and they are kept apart on purpose: the market price says
// what a player will cost, AAV says what the wider world pays, the VBD
// baselines say what a model thinks he is worth, and the flags say how much
// to trust any of it. Averaging them would destroy exactly the
// disagreements that make a board useful.
type PlayerSignals struct {
	PlayerID string
	Name     string
	Position string
	// Team is the player's NFL team abbreviation, from Sleeper's dictionary.
	// Empty for a free agent. Used only by the personal preference filter —
	// one-per-offense and no-handcuff both ask which offense a player is on.
	Team string
	// BlockedReason names why this player is off *your* board given who you
	// already own and your declared preferences — empty when he is not, or
	// when no preferences are set. Set after the board is built, in Build,
	// because it depends on your roster rather than on the player alone.
	BlockedReason string

	// Value is what he is worth on median projections, re-solved against
	// the live pool. Pay this and you should get this production.
	Value int
	// Cost is what he will actually go for here: the national market
	// restated against this room's money and slots.
	//
	// Kept separate from Value on purpose. They answer different questions
	// and diverge wherever the market is buying a range of outcomes a
	// median cannot see.
	Cost int
	// MyMaxBid is the most you will bid: the model price, a conviction
	// adjustment, or a declared must-have cap. Zero means do-not-draft.
	MyMaxBid int
	Lean     PlayerLean
	// BidRule names which rule produced MyMaxBid, so the board can show
	// whether you are buying on value or paying up to secure him.
	BidRule BidRule

	// AAV is the national average auction value from real drafts.
	AAV float64
	// VBD holds the model price under each Subvertadown baseline.
	VBD map[Baseline]float64
	// CielyPoints is a median projection restated in league scoring. Used
	// for ordering and corroboration, never as a dollar figure.
	CielyPoints float64
	// FPValue is the FantasyPros second projection re-solved into dollars
	// against the same live pool as Value, so the two are directly
	// comparable. Zero means FantasyPros did not cover him.
	//
	// A genuinely second opinion, kept beside Value rather than blended into
	// it: where the two projection sources disagree is exactly what a single
	// number would hide.
	FPValue int
	// ECRRank is the player's FantasyPros positional rank (his consensus
	// expert-ranking within his position). Zero means FantasyPros did not
	// cover him.
	ECRRank int
	// SharpRankDelta is how far the most-accurate-expert subset moves him
	// against the full consensus: negative means the sharps rank him better
	// than the field. Zero is agreement or no coverage. Rank-based, so it is
	// unaffected by the FantasyPros points recompute.
	//
	// Distinct from ECR below, which is Subvertadown's industry-deviation
	// flag. These two never share a code path.
	SharpRankDelta int
	// ScarcityPct is the fraction of positive value left at the position
	// after this player goes. Lower means the position is drying up.
	ScarcityPct float64
	// ECR is how much the industry disagrees about him.
	ECR ECRState
	// Availability is Sleeper's injury designation. Meaningless in August,
	// meaningful by draft day once cutdowns and practice reports land.
	Availability string
	// Traits are what kind of player he is — see ClassifyTraits.
	Traits TraitSet
}

// Edge is value minus cost: how much production you get per dollar over
// what he will take to win.
//
// Positive is a bargain — worth more than he costs. Negative means you are
// paying for something a median projection cannot see, which is legitimate
// when the range signals support it and a trap when they do not.
func (p PlayerSignals) Edge() int { return p.Value - p.Cost }

// MarketGap is how much this room's cost exceeds the national market.
func (p PlayerSignals) MarketGap() float64 { return float64(p.Cost) - p.AAV }

// BaselineSpread is the distance between the most and least generous VBD
// baseline for this player.
//
// A wide spread means his value depends heavily on where replacement level
// is drawn, which makes him a fragile buy: the price you pay is only right
// if your baseline choice is right. Gibbs swings $73 to $91 across
// baselines; Chase swings $54 to $67 in the other direction.
func (p PlayerSignals) BaselineSpread() float64 {
	if len(p.VBD) == 0 {
		return 0
	}
	first := true
	var lo, hi float64
	for _, v := range p.VBD {
		if first {
			lo, hi, first = v, v, false
			continue
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return hi - lo
}

// Contested reports whether the industry disagrees about him in both
// directions, which calls for a hard ceiling rather than a different price.
func (p PlayerSignals) Contested() bool { return p.ECR == ECRContested }

// sharpRankThreshold is how many positions the sharp subset must move a
// player before it is worth a flag. Below it the move is noise — two expert
// panels ordering the middle of a tier slightly differently — rather than a
// disagreement about the player.
const sharpRankThreshold = 5

// SharpState is which way the most-accurate experts lean against consensus.
type SharpState int

const (
	// SharpNone is no meaningful move, or no FantasyPros coverage.
	SharpNone SharpState = iota
	// SharpUp means the sharps rank him better than the full field.
	SharpUp
	// SharpDown means the sharps rank him worse than the full field.
	SharpDown
)

// Sharp reports whether the most-accurate-expert subset moved this player far
// enough against consensus to flag, and in which direction. A negative
// SharpRankDelta is a move to a better (lower) rank, so it reads as up.
func (p PlayerSignals) Sharp() SharpState {
	switch {
	case p.SharpRankDelta <= -sharpRankThreshold:
		return SharpUp
	case p.SharpRankDelta >= sharpRankThreshold:
		return SharpDown
	}
	return SharpNone
}

// SignalInputs are the assembled sources a board is built from.
type SignalInputs struct {
	// Values come from Solve against the live pool.
	Values []PlayerValue
	// Costs come from SolveCost against the same pool, keyed by player ID.
	Costs map[string]int
	// Subvertadown rows across every baseline, already ID-resolved.
	Subvertadown []SourceRow
	// CielyPoints maps player ID to a projection in league scoring.
	CielyPoints map[string]float64
	// FantasyPros holds the second projection's read on each player, keyed by
	// player ID: the dollar value it re-solves to, its positional rank, and
	// the sharp-expert move. A player absent here carries no FantasyPros
	// opinion, which the board leaves blank rather than showing as zero worth.
	FantasyPros map[string]FPRead
	// Teams maps player ID to an NFL team abbreviation, for the personal
	// preference filter. A player absent here carries no team, which the
	// filter treats as "no offense to be one-per".
	Teams map[string]string
	// Availability maps player ID to a Sleeper injury designation.
	Availability map[string]string
	// Leans are the personal reads.
	Leans Leans
	// Traits label what kind of player each man is, keyed by player ID.
	Traits map[string]TraitSet
	// RecommendedBid is the most you can pay for any one player before the
	// rest of your roster is at risk. It is the hard ceiling on a must-have's
	// swing-based default cap — see WalkAway — not the must-have price itself.
	RecommendedBid int
}

// FPRead is the FantasyPros second projection's read on one player: the value
// it re-solves to in dollars, its positional consensus rank, and how far the
// most-accurate experts move him against that consensus.
type FPRead struct {
	Value      int
	Rank       int
	SharpDelta int
}

// BuildSignals joins every source onto the priced board.
//
// The market price leads: a player who is not on the priced board is not
// biddable, so sources that mention him are ignored rather than inventing
// a row. Sources that are silent about a player on the board leave their
// fields zero, which reads as "no opinion" rather than "worth nothing".
func BuildSignals(in SignalInputs) []PlayerSignals {
	// Subvertadown ships one row per player per baseline; fold them into
	// one entry each.
	type svAgg struct {
		aav, ps float64
		vbd     map[Baseline]float64
		ecrUp   bool
		ecrDown bool
	}
	sv := map[string]*svAgg{}
	for _, r := range in.Subvertadown {
		if r.PlayerID == "" {
			continue
		}
		a, ok := sv[r.PlayerID]
		if !ok {
			a = &svAgg{vbd: map[Baseline]float64{}}
			sv[r.PlayerID] = a
		}
		// AAV and PS% are published per player, not per baseline, so the
		// last one wins and they all agree.
		a.aav, a.ps = r.AAV, r.ScarcityPct
		a.vbd[Baseline(r.Baseline)] = r.AuctionValue
		// ECR flags are per player too, but OR them so a flag present on
		// any sheet survives.
		a.ecrUp = a.ecrUp || r.ECRUp
		a.ecrDown = a.ecrDown || r.ECRDown
	}

	out := make([]PlayerSignals, 0, len(in.Values))
	for _, v := range in.Values {
		p := PlayerSignals{
			PlayerID:    v.PlayerID,
			Name:        v.Name,
			Position:    v.Position,
			Value:       v.Price,
			Cost:        in.Costs[v.PlayerID],
			MyMaxBid:    v.Price,
			Team:        in.Teams[v.PlayerID],
			VBD:         map[Baseline]float64{},
			CielyPoints: in.CielyPoints[v.PlayerID],
		}
		if a, ok := sv[v.PlayerID]; ok {
			p.AAV, p.ScarcityPct, p.VBD = a.aav, a.ps, a.vbd
			p.ECR = SourceRow{ECRUp: a.ecrUp, ECRDown: a.ecrDown}.ECR()
		}
		if fp, ok := in.FantasyPros[v.PlayerID]; ok {
			p.FPValue, p.ECRRank, p.SharpRankDelta = fp.Value, fp.Rank, fp.SharpDelta
		}
		if s, ok := in.Availability[v.PlayerID]; ok {
			p.Availability = s
		}
		p.Traits = in.Traits[v.PlayerID]
		bid, lean, rule := in.Leans.WalkAway(v.Name, v.Price, in.RecommendedBid, p.BaselineSpread())
		p.MyMaxBid, p.Lean, p.BidRule = bid, lean, rule
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out
}

// Gap is one player where this room's price and an outside reference
// disagree enough to be worth a look.
type Gap struct {
	Player   string
	Position string
	Cost     int
	Compare  float64
	// Delta is Market - Compare. Positive means we pay more than the
	// reference.
	Delta float64
	// Source names what Compare came from.
	Source string
}

// Edges returns the players whose value and cost disagree most, best
// bargains first.
//
// This is the primary decision view. Positive edge means he is worth more
// than he will cost — buy on the median alone. Negative means the market is
// paying for a range of outcomes the median cannot see, which is a fair bet
// when the upside flags support it and a trap when they do not.
func Edges(players []PlayerSignals, minEdge int) []PlayerSignals {
	var out []PlayerSignals
	for _, p := range players {
		// A player no market source covers has no cost to compare against.
		if p.Cost <= 0 {
			continue
		}
		if p.Edge() >= minEdge || p.Edge() <= -minEdge {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Edge() > out[j].Edge() })
	return out
}

// Gaps returns the largest disagreements between our cost board and a
// reference, biggest absolute difference first.
//
// Useful against a VBD baseline, where it contrasts what the market pays
// with what a model says. Against raw AAV it is close to tautological,
// since cost is that same AAV restated for this room.
func Gaps(players []PlayerSignals, source string, minDelta float64) []Gap {
	var out []Gap
	for _, p := range players {
		var compare float64
		switch source {
		case "aav":
			compare = p.AAV
		default:
			if v, ok := p.VBD[Baseline(source)]; ok {
				compare = v
			}
		}
		if compare <= 0 {
			continue
		}
		delta := float64(p.Cost) - compare
		if delta < 0 {
			if -delta < minDelta {
				continue
			}
		} else if delta < minDelta {
			continue
		}
		out = append(out, Gap{
			Player: p.Name, Position: p.Position, Cost: p.Cost,
			Compare: compare, Delta: delta, Source: source,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return absF(out[i].Delta) > absF(out[j].Delta) })
	return out
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// PositionScarcity summarizes how thin a position has become.
type PositionScarcity struct {
	Position string
	// Startable is how many players still on the board meet or exceed the
	// scarcity threshold at the position — the median projection of the
	// tier the last starting slot falls in. See ScarcityThresholds.
	//
	// Bodies are not the measure. 182 receivers remain in a room that needs
	// 43 more starters, which reads as the deepest position on the board;
	// only 40 of them project above replacement, which is the opposite
	// reading. The other 140 are waiver fodder whose presence says nothing
	// except that the NFL employs a lot of receivers.
	Startable int
	// StartersLeft is how many starting spots the league still has to fill.
	StartersLeft int
	// Gone is how many players at the position are already off the board,
	// through keepers or picks already made.
	//
	// Separate from Startable because rules about draft *progress* count
	// what has left, not what is left. Ciely's running back rule is stated
	// as "before the 33rd comes off the board", and 33 gone is nothing like
	// 33 remaining.
	Gone int
	// Cover is Startable over StartersLeft: below 1 the room cannot field
	// this position from players worth starting.
	//
	// Read it as a surplus gauge rather than an alarm. It starts near 1 —
	// the baseline is drawn at the last starter, so supply and demand begin
	// level — and then tends to rise, because a drafted player takes one
	// off both sides while a drafted scrub takes one off demand alone. A
	// reading below 1 is therefore rare and means something has gone badly
	// wrong at the position, not that it is getting picked over. For
	// picked-over, read Cliff and TopScarcityPct.
	Cover float64
	// TopScarcityPct is the lowest PS% among the position's best remaining
	// players — how little value is left behind them.
	TopScarcityPct float64
	// Cliff is the points drop from the best available to the next tier.
	Cliff float64
	// Threshold is the projection a player must meet to be counted in
	// Startable — the median of the tier the last starting slot falls in.
	// See ScarcityThresholds for why the tier's median rather than the
	// player sitting in the slot.
	//
	// Carried on the summary so a consumer can sort players against the
	// same bar the count was taken at. Without it a board showing "8
	// startable quarterbacks" cannot say *which* eight, and anything that
	// re-derived the line from the remaining pool would draw it somewhere
	// else — the pinned threshold is the whole reason the count decays.
	Threshold float64
}

// Scarcity measures how many players worth starting are left at each
// position, which is what the pivot triggers watch.
//
// thresholds are the pinned pre-draft tier medians from ScarcityThresholds.
// Pinned matters here more than anywhere else: a threshold recomputed from
// the pool that remains sinks as the pool empties, so the count above it
// barely changes, and a scarcity measure that cannot fall has nothing to
// say. Against a fixed threshold the count decays as the position is picked
// over, which is the entire signal.
func Scarcity(players []PlayerSignals, state PoolState, thresholds map[string]float64) map[string]PositionScarcity {
	byPos := map[string][]PlayerSignals{}
	for _, p := range players {
		byPos[p.Position] = append(byPos[p.Position], p)
	}
	// Demand is counted at the same line as supply. The baselines are
	// pinned to VOLS, whose depth is the starting spots themselves with no
	// bench rounds behind them; measuring startable players against the
	// active curve's deeper line would compare a VOLS count to a BEER+
	// requirement and report a shortage that is only the two curves
	// disagreeing about where replacement sits.
	starting := state
	starting.Baseline = BaselineVOLS
	depth := replacementDepth(starting)

	out := map[string]PositionScarcity{}
	for pos, list := range byPos {
		sort.SliceStable(list, func(i, j int) bool { return list[i].CielyPoints > list[j].CielyPoints })
		startable := 0
		for _, p := range list {
			// Meet or exceed: a player who projects exactly at the tier
			// median is as good as the typical member of it.
			if p.CielyPoints >= thresholds[pos] {
				startable++
			}
		}
		s := PositionScarcity{
			Position:     pos,
			Startable:    startable,
			StartersLeft: depth[pos],
			Gone:         state.Filled[pos],
			Threshold:    thresholds[pos],
		}
		if depth[pos] > 0 {
			s.Cover = float64(startable) / float64(depth[pos])
		}
		if len(list) > 0 {
			s.TopScarcityPct = list[0].ScarcityPct
			// The cliff is the biggest single drop inside the next few
			// players — the "draft chasm" worth pivoting for.
			window := 5
			if len(list) < window {
				window = len(list)
			}
			for i := 0; i+1 < window; i++ {
				if d := list[i].CielyPoints - list[i+1].CielyPoints; d > s.Cliff {
					s.Cliff = d
				}
			}
		}
		out[pos] = s
	}
	return out
}

// contestedFloor is the price below which a player is not really being
// bid on, and so says nothing about how the sources disagree.
const contestedFloor = 1

// PositionalBias is the median edge at each position — the part of every
// player's edge that is really a statement about his position rather than
// about him.
//
// It exists because the value source and the cost source draw replacement
// level in different places. Ciely prices 29 tight ends above the floor
// against 13 in the market and 12 starting spots, so his tight end board
// carries value the market does not believe in. Every TE therefore shows a
// large positive edge, and none of it is about the player.
func PositionalBias(players []PlayerSignals) map[string]float64 {
	byPos := map[string][]float64{}
	for _, p := range players {
		// Only players actually being bid on. Hundreds sit at the $1 floor
		// on both boards with an edge of zero, and including them drags
		// every median to nothing — the distortion lives entirely among
		// the players anyone is competing for.
		if p.Cost <= contestedFloor && p.Value <= contestedFloor {
			continue
		}
		byPos[p.Position] = append(byPos[p.Position], float64(p.Edge()))
	}
	out := make(map[string]float64, len(byPos))
	for pos, edges := range byPos {
		sort.Float64s(edges)
		n := len(edges)
		switch {
		case n == 0:
			out[pos] = 0
		case n%2 == 1:
			out[pos] = edges[n/2]
		default:
			out[pos] = (edges[n/2-1] + edges[n/2]) / 2
		}
	}
	return out
}

// AdjustedEdge is a player's edge with his position's common component
// removed: how much better he looks than the rest of his own position,
// rather than how good the source thinks his position is.
//
// This is the number to act on. A raw +$15 on a tight end when every tight
// end reads +$14 says nothing; the same +$15 when his position sits at +$1
// is a real disagreement about that player.
func AdjustedEdge(p PlayerSignals, bias map[string]float64) float64 {
	return float64(p.Edge()) - bias[p.Position]
}

// RankByAdjustedEdge sorts players by edge with positional bias removed,
// best first, keeping only those clearing minEdge in either direction.
func RankByAdjustedEdge(players []PlayerSignals, bias map[string]float64, minEdge float64) []PlayerSignals {
	var out []PlayerSignals
	for _, p := range players {
		if p.Cost <= 0 {
			continue
		}
		if adj := AdjustedEdge(p, bias); adj >= minEdge || adj <= -minEdge {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return AdjustedEdge(out[i], bias) > AdjustedEdge(out[j], bias)
	})
	return out
}

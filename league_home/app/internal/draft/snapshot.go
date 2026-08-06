package draft

// Snapshot is a complete picture of the draft at one moment: what is
// available, what it costs, what you can spend, and what to do about it.
//
// One type serves both the terminal board and the web UI so they cannot
// drift. Everything a front end needs is here; neither is allowed to
// recompute anything for itself.
type Snapshot struct {
	// Season the draft belongs to.
	Season string `json:"season"`
	// Baseline is the active valuation curve.
	Baseline Baseline `json:"baseline"`

	// Dollars and Slots describe the whole room's remaining pool.
	Dollars int `json:"dollars"`
	Slots   int `json:"slots"`
	// LeaguePerStarter is what the room can spend on each remaining
	// starting spot — the yardstick every bid is measured against.
	LeaguePerStarter float64 `json:"leaguePerStarter"`

	// Me is your own budget and roster needs.
	Me MyState `json:"me"`
	// MaxBid is the most you can physically pay and still fill a roster.
	MaxBid int `json:"maxBid"`
	// Recommended is the most you can pay before the rest of the roster
	// is at risk, and Risk explains what that bid would do.
	Recommended int     `json:"recommended"`
	Risk        BidRisk `json:"risk"`

	// Players is the board, most expensive first.
	Players []PlayerSignals `json:"players"`
	// Bias is each position's median edge, the part of every edge that is
	// about the position rather than the player.
	Bias map[string]float64 `json:"bias"`
	// Scarcity is how thin each position has become.
	Scarcity map[string]PositionScarcity `json:"scarcity"`
	// MustHaves totals what your declared must-haves commit you to.
	MustHaves MustHaveCost `json:"mustHaves"`
	// Pivot is the single highest-priority trigger, if any fired.
	Pivot    Pivot `json:"pivot"`
	HasPivot bool  `json:"hasPivot"`
	// Pivots are the rest, for a prep view of what is close to firing.
	Pivots []Pivot `json:"pivots"`

	// LeanSets names the opinion sets applied, in precedence order.
	LeanSets []string `json:"leanSets"`

	// Warnings are non-fatal problems worth showing rather than hiding —
	// unmatched source rows, stale snapshots.
	Warnings []string `json:"warnings"`
}

// AdjustedEdges returns the board ranked by edge with the positional
// component removed, which is the number to act on.
func (s Snapshot) AdjustedEdges(minEdge float64) []PlayerSignals {
	return RankByAdjustedEdge(s.Players, s.Bias, minEdge)
}

// Assemble builds a Snapshot from already-computed parts.
//
// Kept as a plain constructor rather than something that fetches: the
// caller owns loading, so this stays testable and the web server can
// rebuild a snapshot on every request without hidden network calls.
func Assemble(season string, state PoolState, me MyState, players []PlayerSignals,
	leans Leans, tempo DraftTempo, warnings []string) Snapshot {

	bias := PositionalBias(players)
	scarcity := Scarcity(players, state)
	leaguePerStarter := state.LeaguePerStarter()
	recommended := me.MaxRecommendedBid(leaguePerStarter, DefaultRiskFloor)

	bids := map[string]int{}
	for _, p := range players {
		if p.Lean.Lean == LeanMust {
			bids[normalizeName(p.Name)] = p.MyMaxBid
		}
	}

	pivots := Pivots(players, scarcity, me, tempo)
	top, has := Top(pivots)

	return Snapshot{
		Season:           season,
		Baseline:         state.Baseline,
		Dollars:          state.Dollars,
		Slots:            state.Slots,
		LeaguePerStarter: leaguePerStarter,
		Me:               me,
		MaxBid:           me.MaxBid(),
		Recommended:      recommended,
		Risk:             me.RiskOf(recommended, leaguePerStarter),
		Players:          players,
		Bias:             bias,
		Scarcity:         scarcity,
		MustHaves:        leans.MustHaves(me.Budget, me.OpenSlots, bids),
		Pivot:            top,
		HasPivot:         has,
		Pivots:           pivots,
		Warnings:         warnings,
	}
}

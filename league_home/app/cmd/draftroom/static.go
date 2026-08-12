package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"leaguehome/internal/draft"
	"leaguehome/internal/sleeper"
)

// staticData is everything that cannot change once a draft is underway.
//
// Loading it costs 114 Sleeper calls and about three seconds — five seasons
// of picks and weekly transactions, the 5 MB player dictionary, and last
// season's stats. None of it moves while you are drafting, so it is fetched
// once and then reused.
//
// Splitting it out is what makes fast polling possible: the live loop needs
// exactly one call, for the current draft's picks. Rebuilding everything on
// a timer would burn 345 calls a minute to learn nothing, and would put a
// three-second stall between clicking "sold" and seeing the board move.
type staticData struct {
	client  *sleeper.Client
	draftID string

	// Roster shape and pool size before anything is drafted.
	shape       draft.PoolState
	teams       int
	budget      int
	fullDollars int
	fullSlots   int

	// Priced keeper projections and the resulting per-owner budgets.
	projected []draft.Entry
	keeperOf  map[string]int

	projections []draft.Projection
	market      []draft.MarketPrice
	points      map[string]float64
	subvert     []draft.SourceRow

	availability map[string]string
	// traits label what kind of player each man is; see ClassifyTraits.
	traits map[string]draft.TraitSet
	// priceHistory is what each rank tier has cost in past drafts, the
	// reference the live lines are read against. Computed from the seasons
	// already loaded for the keeper ledger, so it costs no extra calls.
	priceHistory map[string][]int
	// baselines are the pinned pre-draft replacement points that rosters
	// are scored against; thresholds are the pinned tier medians scarcity
	// is counted against. Both computed once, because the projection set
	// and the league shape do not change during a draft, and because
	// everything measuring against them has to use the same ones.
	baselines  map[string]float64
	thresholds map[string]float64
	leans      draft.Leans
	// leanSets names the opinion sets in precedence order, so the board
	// can say whose reads it is applying rather than leaving you to
	// remember which flags you asked for.
	leanSets []string
	ownerID  string
	season   string
	warnings []string
}

// loadStatic fetches everything that will not change during the draft.
func loadStatic(leagueID, configDir, dataDir, ownerID string, baseline draft.Baseline, leanSets []string) (*staticData, error) {
	c := sleeper.New()
	c.HTTPClient = &http.Client{Timeout: 180 * time.Second}

	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return nil, err
	}
	root, err := draft.ResolveDataRoot(dataDir)
	if err != nil {
		return nil, err
	}

	rules := draft.DefaultRules()
	seasons, err := draft.LoadSeasons(c, leagueID, rules)
	if err != nil {
		return nil, err
	}
	overrides, err := draft.LoadOverrides(filepath.Join(cfg, rulingsFile))
	if err != nil {
		return nil, err
	}
	ledger, err := draft.BuildLedgerWithOverrides(seasons, rules, overrides)
	if err != nil {
		return nil, err
	}
	last := ledger.Seasons[len(ledger.Seasons)-1]

	info, err := playerInfo(c, last)
	if err != nil {
		return nil, err
	}
	rosters, err := c.Rosters(leagueID)
	if err != nil {
		return nil, fmt.Errorf("loading rosters: %w", err)
	}
	season, err := upcomingSeason(c, leagueID, last)
	if err != nil {
		return nil, err
	}
	projected, err := draft.Project(ledger, season, rosters, info)
	if err != nil {
		return nil, err
	}

	ciely, err := draft.LoadSourceCSV(root.Normalized("ciely-2026.csv"))
	if err != nil {
		return nil, err
	}
	sv, err := draft.LoadSourceCSV(root.Normalized("subvertadown-2026.csv"))
	if err != nil {
		return nil, err
	}
	aliases, err := draft.LoadAliases(filepath.Join(cfg, aliasesFile))
	if err != nil {
		return nil, err
	}
	leans, sets, err := loadLeanSets(cfg, leanSets)
	if err != nil {
		return nil, err
	}

	idx := draft.BuildPlayerIndexWithAliases(info, aliases)
	var warnings []string
	for _, pl := range leans.Contested() {
		var against []string
		for _, o := range pl.Disagreement() {
			against = append(against, fmt.Sprintf("%s says %s", o.Source, o.Lean))
		}
		warnings = append(warnings, fmt.Sprintf("%s: you say %s, %s",
			pl.Player, pl.Lean, strings.Join(against, ", ")))
	}
	if bad := idx.Resolve(ciely); len(bad) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d Ciely rows unmatched", len(bad)))
	}
	if bad := idx.Resolve(sv); len(bad) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d Subvertadown rows unmatched", len(bad)))
	}

	s := &staticData{
		client: c, ownerID: ownerID, season: season, warnings: warnings,
		leans: leans, subvert: sv, points: map[string]float64{},
		leanSets:     setNames(sets),
		availability: map[string]string{}, keeperOf: map[string]int{},
		projected: projected,
	}
	s.teams, s.budget = auctionShape(c, leagueID)
	s.shape = draft.HitOrMissPool()
	s.shape.Teams, s.shape.Baseline = s.teams, baseline
	s.fullDollars, s.fullSlots = s.teams*s.budget, s.teams*14

	for _, r := range ciely {
		if r.PlayerID == "" || r.Position == "DST" {
			continue
		}
		s.points[r.PlayerID] = r.Points
		s.projections = append(s.projections, draft.Projection{
			PlayerID: r.PlayerID, Name: r.Player, Position: r.Position, Points: r.Points,
		})
	}
	// Pinned now that the projection set is complete, and never
	// recomputed: replacement level measured against the pool that remains
	// falls as the pool empties, so a count above it could never drop.
	s.baselines = draft.ScoringBaselines(s.projections, s.shape)
	s.traits = classifyTraits(ciely, sv, info, s.shape)
	s.priceHistory = draft.HistoricalPriceLines(seasons, func(id string) string {
		return info[id].Position
	}, minSpendForUsableSeason)
	s.thresholds = draft.ScarcityThresholds(s.projections, s.shape)

	for _, r := range sv {
		if r.Baseline != "beerplus" || r.PlayerID == "" || r.AAV <= 0 {
			continue
		}
		s.market = append(s.market, draft.MarketPrice{
			PlayerID: r.PlayerID, Name: r.Player, Position: r.Position, AAV: r.AAV,
		})
	}
	for id, p := range info {
		if p.Injury != "" {
			s.availability[id] = p.Injury
		}
	}
	for _, e := range projected {
		s.keeperOf[e.PlayerID] = e.LeaguePrice
	}

	// The draft to watch, if one exists yet.
	if drafts, err := c.Drafts(leagueID); err == nil && len(drafts) > 0 {
		s.draftID = drafts[0].DraftID
	}
	return s, nil
}

// Picks fetches the current draft's picks. This is the only call the live
// loop makes — one request, roughly 130ms.
func (s *staticData) Picks() ([]sleeper.DraftPick, error) {
	if s.draftID == "" {
		return nil, nil
	}
	return s.client.DraftPicks(s.draftID)
}

// Drafting reports whether the draft is actually under way.
//
// One small call, and it decides how hard the poll loop works. Sleeper's
// draft status is "pre_draft" until the commissioner starts it and
// "complete" when it ends, so outside that window there is nothing to
// discover by asking every two seconds.
func (s *staticData) Drafting() bool {
	if s.draftID == "" {
		return false
	}
	d, err := s.client.Draft(s.draftID)
	if err != nil {
		// Unknown is treated as live: a blip must not stall the board on
		// the one night it matters.
		return true
	}
	return d.Status == "drafting"
}

// gone describes a player who has left the board, and what it cost.
type gone struct {
	price int
	mine  bool
}

// Build recomputes the whole board from the cached statics plus whatever
// has been drafted. Pure computation — no network, microseconds.
// Build assembles the whole board. edits are personal reads made since
// startup, overriding the loaded lean sets per player; nil for callers with
// none.
//
// Passed in rather than stored, because staticData is shared and immutable
// by contract — see the type comment. Threading them through the one seam
// that consumes them keeps that true and keeps the dependency visible.
func (s *staticData) Build(taken map[string]gone, edits draft.Leans) (draft.Snapshot, error) {
	// Start from the projected keeper set, then remove anyone already off
	// the board for real.
	aav := map[string]float64{}
	for _, m := range s.market {
		aav[m.PlayerID] = m.AAV
	}
	dollars, slots, filled := poolAfterKeepers(s.projected, aav, s.teams, s.budget)
	me := myState(s.projected, aav, s.ownerID, s.budget)

	for id, g := range taken {
		dollars -= g.price
		slots--
		if g.mine {
			me.Budget -= g.price
			me.OpenSlots--
			if pos := s.positionOf(id); pos != "" {
				if n := me.StartersNeeded[pos]; n > 0 {
					me.StartersNeeded[pos] = n - 1
				}
			}
		}
		if pos := s.positionOf(id); pos != "" {
			filled[pos]++
		}
	}

	state := s.shape
	state.Dollars, state.Slots, state.Filled = dollars, slots, filled

	available := make([]draft.Projection, 0, len(s.projections))
	for _, p := range s.projections {
		if _, off := taken[p.PlayerID]; !off {
			available = append(available, p)
		}
	}
	values, err := draft.Solve(available, state)
	if err != nil {
		return draft.Snapshot{}, err
	}

	openMarket := make([]draft.MarketPrice, 0, len(s.market))
	for _, m := range s.market {
		if _, off := taken[m.PlayerID]; !off {
			openMarket = append(openMarket, m)
		}
	}
	costBoard, err := draft.SolveCost(openMarket, state)
	if err != nil {
		return draft.Snapshot{}, err
	}
	costs := make(map[string]int, len(costBoard))
	for _, m := range costBoard {
		costs[m.PlayerID] = m.Cost
	}

	recommended := me.MaxRecommendedBid(state.LeaguePerStarter(), draft.DefaultRiskFloor)
	// Resolved once and used by both consumers below. Passing the raw set
	// to either would put a read on the board that the must-have budget
	// line does not know about — the two would disagree about the same
	// player on the same screen.
	leans := s.effectiveLeans(edits)
	players := draft.BuildSignals(draft.SignalInputs{
		Values: values, Costs: costs, Subvertadown: s.subvert,
		CielyPoints: s.points, Availability: s.availability,
		Leans: leans, Traits: s.traits, RecommendedBid: recommended,
	})
	snap := draft.Assemble(s.season, state, me, players, leans, s.tempo(taken, costs), s.thresholds, s.warnings)
	snap.LeanSets = s.leanSets
	// Players already gone price the curve at what they actually went for,
	// which is why this is assembled here where taken is in scope.
	sold := map[string][]int{}
	for id, g := range taken {
		if pos := s.positionOf(id); pos != "" && g.price > 0 {
			sold[pos] = append(sold[pos], g.price)
		}
	}
	snap.PriceLines = draft.PriceLines(players, sold, s.priceHistory)
	return snap, nil
}

// tempo compares what the room actually paid against what the cost board
// predicted, which is the live calibration three seasons of history cannot
// provide.
func (s *staticData) tempo(taken map[string]gone, costs map[string]int) draft.DraftTempo {
	var t draft.DraftTempo
	for id, g := range taken {
		if g.price <= 0 {
			continue
		}
		expected, ok := costs[id]
		if !ok {
			continue
		}
		t.Spent += g.price
		t.Expected += expected
		t.Picks++
	}
	return t
}

// effectiveLeans is the loaded sets with any live edits laid over the top.
//
// Copies rather than mutating: s.leans is shared with every other reader of
// this staticData, and a board rebuild must not be able to change what the
// next one starts from.
func (s *staticData) effectiveLeans(edits draft.Leans) draft.Leans {
	if len(edits) == 0 {
		return s.leans
	}
	out := make(draft.Leans, len(s.leans)+len(edits))
	for k, v := range s.leans {
		out[k] = v
	}
	for k, v := range edits {
		if v.Lean == "" {
			// An edit back to nothing removes the read rather than leaving
			// a blank one, which WalkAway would treat as an unknown lean.
			delete(out, k)
			continue
		}
		// Only the read changes. Replacing the whole record erased two
		// things that were not the board's to erase: a hand-written cap,
		// which quietly raised the ceiling on a must-have from $24 to $49
		// with nothing on screen to say so, and the losing opinions from
		// other sets, which is what the "vs menton" split flag is made of.
		//
		// It could not be avoided by care either — every route back around
		// the cycle passes through the clear, so four clicks turned a $20
		// hard cap into no cap at all.
		merged, known := out[k]
		if !known {
			merged.Player = v.Player
		}
		merged.Lean, merged.Source = v.Lean, v.Source
		out[k] = merged
	}
	return out
}

// heldRoster is the owner's projected keepers as roster spots, priced at
// what the league will charge rather than at what they are worth.
//
// The charge is the number that matters to a shape: it is the money already
// committed and the slot already filled.
func (s *staticData) heldRoster(ownerID string) []draft.RosterSpot {
	aav := map[string]float64{}
	for _, m := range s.market {
		aav[m.PlayerID] = m.AAV
	}
	var out []draft.RosterSpot
	for _, e := range projectedKeepers(s.projected, aav, ownerID) {
		out = append(out, draft.RosterSpot{
			Player: draft.PlayerSignals{
				PlayerID:    e.PlayerID,
				Name:        e.Name,
				Position:    e.Position,
				CielyPoints: s.points[e.PlayerID],
				Cost:        int(aav[e.PlayerID] + 0.5),
				// Traits matter as much as the price. Without them a
				// keeper occupies the slot and the money while being
				// invisible to every shape made of player types, so a
				// lineup measures as though two of its fourteen were
				// blanks — and a keeper carrying the exact trait a shape
				// wants gets reported as ruling that shape out.
				Traits: s.traits[e.PlayerID],
			},
			Price: e.LeaguePrice,
		})
	}
	return out
}

func (s *staticData) positionOf(playerID string) string {
	for _, p := range s.projections {
		if p.PlayerID == playerID {
			return p.Position
		}
	}
	return ""
}

// nameOf returns a player's display name, for reporting someone who has
// left the board.
func (s *staticData) nameOf(playerID string) string {
	for _, p := range s.projections {
		if p.PlayerID == playerID {
			return p.Name
		}
	}
	for _, m := range s.market {
		if m.PlayerID == playerID {
			return m.Name
		}
	}
	return playerID
}

// playerIDByName resolves a board name to a Sleeper ID, normalizing so
// punctuation in what the page rendered cannot defeat the match.
func (s *staticData) playerIDByName(name string) string {
	want := draft.NormalizeName(name)
	for _, p := range s.projections {
		if draft.NormalizeName(p.Name) == want {
			return p.PlayerID
		}
	}
	for _, m := range s.market {
		if draft.NormalizeName(m.Name) == want {
			return m.PlayerID
		}
	}
	return ""
}

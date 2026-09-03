package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
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
	// offLeagueDraft is set when draftID is a draft this league does not own
	// -- a mock, followed by an explicit -draft. The board works the same
	// either way; what changes is that state written during a rehearsal must
	// not land where the live board would read it.
	offLeagueDraft bool
	// myRosterID and mySlot are the two other ways Sleeper can say a pick is
	// yours, resolved once at load. Zero means unknown, and an unknown rung
	// is skipped rather than guessed at — an unset roster_id and an unset
	// draft_slot are both 0, so a 0 that matched would claim every pick
	// nobody else had claimed. See isMine.
	myRosterID int
	mySlot     int

	// Roster shape and pool size before anything is drafted.
	shape       draft.PoolState
	teams       int
	budget      int
	fullDollars int
	fullSlots   int

	// Priced keeper projections and the resulting per-owner budgets.
	projected []draft.Entry
	keeperOf  map[string]int
	// forcedKeepers are players declared kept by hand (keeper-locks.csv),
	// keyed by player ID. The research keeper scenarios treat them as locks
	// regardless of surplus, for keepers the value math would not guess.
	forcedKeepers map[string]bool
	// declaredOwners are the owners who have finished deciding, keyed by
	// owner id. Their keeper sets are taken as final: the surplus heuristic
	// stops guessing for them, and on draft night their keepers come off the
	// board rather than staying biddable. An owner absent here has not filed
	// yet and is still projected.
	declaredOwners map[string]bool
	// ownerNameByID and ownerIDByName carry the league's managers both ways:
	// the first to name who has not declared yet, the second to resolve a
	// "keeps nobody" row, which has no player to read a roster from.
	ownerNameByID map[string]string
	ownerIDByName map[string]string

	projections []draft.Projection
	market      []draft.MarketPrice
	points      map[string]float64
	subvert     []draft.SourceRow

	// cielyDelta is Ciely's positional rank against the primary's, per player,
	// resolved at load because neither side moves afterwards.
	cielyDelta map[string]int

	// cielyRank is Ciely's positional rank per player ID, the one thing of his
	// the board reads. His dollars are a linear map of his medians and cannot
	// see keeper inflation, but where his ordering parts from the field is a
	// real second look — the same claim the dell flags make for one expert.
	cielyRank map[string]int

	// fpProjections is the FantasyPros second projection, re-solved into
	// dollars against the live pool in Build so it is comparable to the Ciely
	// value. fpRank and fpSharp are the consensus positional rank and the
	// sharp-expert move, carried separately because they survive the solve
	// unchanged. All three are empty when the FantasyPros source is absent.
	fpProjections []draft.Projection
	fpRank        map[string]int
	fpSharp       map[string]int
	// dellSharp is Chris Dell's rank-vs-consensus gap per player ID, for the
	// dell+/dell- flag. Empty when his source is absent.
	dellSharp map[string]int
	// borisTier is Boris Chen's within-position half-PPR tier per player ID,
	// shown on the web board beside our own gap-based tiering. Empty when his
	// source is absent.
	borisTier map[string]int

	availability map[string]string
	// team maps a player ID to his NFL team abbreviation, from the Sleeper
	// dictionary. The personal preference filter is the only reader: it asks
	// which offense a player is on.
	team map[string]string
	// prefs are the personal one-per-offense / no-handcuff filters that make
	// scarcity your own rather than the league's. An absent file loads to the
	// zero value, which filters nothing.
	prefs draft.Preferences
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
	// leanSetInfo is those same sets whole — path, undecided names, and
	// whether a generator owns the file. The board needs only the names; the
	// leans page needs to say where a read lives and whether it is safe to
	// edit there.
	leanSetInfo []draft.LeanSet
	// matcher resolves a written player name to the pool's own spelling, so
	// a read spelled reasonably still lands. Kept so the lean-edit endpoint
	// resolves names the same way the board did.
	matcher *draft.PoolMatcher
	// minePath is the file the first set was actually read from, and it is
	// where a read set on the board is written back.
	//
	// Carried rather than recomputed because the reader has fallbacks the
	// writer cannot see: an unmigrated config resolves "mine" to my-guys.csv
	// outside the leans directory entirely. A writer that guessed would put
	// the read in a file that startup does not consult, and the board would
	// come back without it.
	minePath string
	ownerID  string
	season   string
	// warnings are problems fixed at load: source rows that did not resolve,
	// and anything else that cannot change without a restart.
	warnings []string
	// leanWarnings are the ones that depend on the reads, so they are
	// recomputed whenever the sets are reloaded. Kept apart from warnings
	// precisely so a reload can replace them without disturbing the rest —
	// a contested read that has since been resolved must stop being
	// reported, or the strip becomes a list of things that used to be true.
	leanWarnings []string
}

// loadStatic fetches everything that will not change during the draft.
func loadStatic(leagueID, draftID, configDir, dataDir, ownerID string, baseline draft.Baseline, leanSets []string) (*staticData, error) {
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

	sv, err := draft.LoadSourceCSV(root.Normalized("subvertadown-2026.csv"), draft.SubvertadownColumns)
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
	prefs, err := draft.LoadPreferences(filepath.Join(cfg, preferencesFile))
	if err != nil {
		return nil, err
	}

	idx := draft.BuildPlayerIndexWithAliases(info, aliases)
	var warnings []string
	// Projection sources — the sheets solved into comparable dollar values —
	// load from the registry rather than by name here; adding one is an entry
	// in draft.ProjectionSources, and which sheet is the backbone is its Role.
	// Subvertadown stays separate: it is VBD baselines plus a market AAV, a
	// different shape the registry deliberately does not carry.
	proj, err := draft.LoadProjections(root.Normalized, idx)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, proj.PrimaryWarnings...)
	if bad := idx.Resolve(sv); len(bad) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d Subvertadown rows unmatched", len(bad)))
	}
	warnings = append(warnings, proj.SecondWarnings...)

	s := &staticData{
		client: c, ownerID: ownerID, season: season, warnings: warnings,
		leans: leans, subvert: sv, points: proj.Points,
		leanSets:     setNames(sets),
		leanSetInfo:  sets,
		minePath:     writableSetPath(cfg, sets),
		availability: map[string]string{}, keeperOf: map[string]int{},
		team:      map[string]string{},
		prefs:     prefs,
		projected: projected,
		fpRank:    map[string]int{}, fpSharp: map[string]int{},
	}
	s.teams, s.budget = auctionShape(c, leagueID)
	s.shape = draft.HitOrMissPool()
	s.shape.Teams, s.shape.Baseline = s.teams, baseline
	s.fullDollars, s.fullSlots = s.teams*s.budget, s.teams*14

	// The primary source is the board's projection backbone; each second
	// opinion is re-solved against the same pool in Build for a comparable
	// value.
	//
	// Second opinions are picked by name, not by position. This loop used to
	// keep whichever came last, which meant the FP column's meaning depended
	// on the order of a registry that says nothing about it — and it is how
	// Ciely came to be loaded and silently thrown away for a season. The
	// dollar column has room for one, and fpSharpSource is the one it shows;
	// Ciely contributes his ordering instead, see cielyRank.
	s.projections = proj.Projections
	fp, cielyRank := assignSecondOpinions(proj.SecondOpinions)
	s.fpProjections, s.fpRank, s.fpSharp = fp.Projections, fp.Rank, fp.Sharp
	s.cielyRank = cielyRank
	// Now that the pool exists, reads can be matched to it. A lean is
	// applied by name, and the pool is spelled the projection source's way,
	// so "Kenneth Walker III" — Sleeper's spelling and the natural one to
	// type — had to be rewritten to reach the board at all.
	s.matcher = draft.NewPoolMatcher(poolNames(s.projections), aliases)
	s.leans = matchAndMerge(sets, s.matcher)
	s.refreshLeanWarnings()

	// The league's managers, both ways round. Needed to resolve a "keeps
	// nobody" declaration, which names an owner and no player, and to say who
	// has not declared yet. Neither is fatal: without the mapping the file
	// still works for every owner who declares an actual player, since a
	// player's roster settles his owner on its own.
	s.ownerNameByID, s.ownerIDByName = map[string]string{}, map[string]string{}
	if owners, err := draft.LoadOwners(filepath.Join(cfg, ownersFile)); err == nil {
		if users, err := c.Users(leagueID); err == nil {
			for _, u := range users {
				name := owners[strings.ToLower(u.DisplayName)]
				if name == "" {
					name = u.DisplayName
				}
				s.ownerNameByID[u.UserID] = name
				s.ownerIDByName[strings.ToLower(name)] = u.UserID
			}
		}
	}

	// Resolve hand-declared keeper locks against the pool now the matcher
	// exists. A name that reaches no rostered player is surfaced rather than
	// dropped — a keeper you think is locked and is not would quietly leave
	// him in the auction pool.
	locks, err := loadKeeperLocks(cfg)
	if err != nil {
		return nil, err
	}
	s.forcedKeepers = map[string]bool{}
	s.declaredOwners = map[string]bool{}
	ownerOf := map[string]string{} // player id -> owner id, from the rosters
	for _, e := range s.projected {
		ownerOf[e.PlayerID] = e.OwnerID
	}
	for _, lk := range locks {
		// A `none` row declares an owner with no keepers. There is no player
		// to read the roster from, so the owner column is the only handle on
		// it and has to resolve.
		if lk.Declared() {
			id := s.ownerIDByName[strings.ToLower(lk.Owner)]
			if id == "" {
				s.warnings = append(s.warnings, fmt.Sprintf(
					"keeper locks: %q keeps nobody, but no such owner — check the spelling against owners.csv", lk.Owner))
				continue
			}
			s.declaredOwners[id] = true
			continue
		}
		id := s.playerIDByName(lk.Player)
		if id == "" {
			s.warnings = append(s.warnings, fmt.Sprintf("keeper lock %q reaches no rostered player", lk.Player))
			continue
		}
		s.forcedKeepers[id] = true
		// Appearing in the file is the declaration. Taken from the roster
		// rather than the owner column, which is documentation and can be
		// wrong; a player sits on exactly one roster and that settles it.
		if owner := ownerOf[id]; owner != "" {
			s.declaredOwners[owner] = true
		}
	}
	// Say who is still outstanding. A half-filled file prices a board that is
	// part fact and part guess, and the difference is invisible on the page.
	if len(s.declaredOwners) > 0 {
		// Only owners who actually hold a roster. A league's Sleeper user list
		// can carry more accounts than teams — a co-manager, or someone who
		// left and was never removed — and counting those would report more
		// teams than the league has and ask for declarations that can never
		// come.
		rostered := map[string]bool{}
		for _, e := range s.projected {
			if e.OwnerID != "" {
				rostered[e.OwnerID] = true
			}
		}
		var pending []string
		for id := range rostered {
			if s.declaredOwners[id] {
				continue
			}
			name := s.ownerNameByID[id]
			if name == "" {
				name = id
			}
			pending = append(pending, name)
		}
		sort.Strings(pending)
		if len(pending) > 0 {
			s.warnings = append(s.warnings, fmt.Sprintf(
				"keepers declared for %d of %d teams — still projected for %s",
				len(rostered)-len(pending), len(rostered), strings.Join(pending, ", ")))
		}
	}

	// Chris Dell's rankings drive the dell+/dell- flag, resolved through the
	// same matcher so a name lands where his lean set does. Absent is fine.
	dellSharp, dellWarn := loadChrisDellSharp(root.Normalized("chrisdell-2026.csv"), s.playerIDByName)
	s.dellSharp = dellSharp
	if dellWarn != "" {
		s.warnings = append(s.warnings, dellWarn)
	}

	// Boris Chen's half-PPR tiers, resolved through the same matcher. Absent
	// is fine — the board simply shows no Boris tier.
	borisTier, borisWarn := loadBorisChenTiers(root.Normalized("borischen-2026.csv"), s.playerIDByName)
	s.borisTier = borisTier
	if borisWarn != "" {
		s.warnings = append(s.warnings, borisWarn)
	}

	// Pinned now that the projection set is complete, and never
	// recomputed: replacement level measured against the pool that remains
	// falls as the pool empties, so a count above it could never drop.
	s.baselines = draft.ScoringBaselines(s.projections, s.shape)
	s.traits = classifyTraits(proj.PrimaryRows, sv, info, s.shape)
	// The primary's positional rank, which is what Ciely's ordering is
	// measured against. Taken from the same rows the trait classifier reads.
	// Resolved once here rather than per rebuild: both ranks are fixed for
	// the life of the process, and Build runs on every poll.
	s.cielyDelta = cielyDivergence(proj.PrimaryRank, s.cielyRank)
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
		if p.Team != "" {
			s.team[id] = p.Team
		}
	}
	for _, e := range projected {
		s.keeperOf[e.PlayerID] = e.LeaguePrice
	}

	// The draft to watch, if one exists yet. A failed lookup is not fatal:
	// there may simply be no draft, which is most of the year.
	drafts, err := c.Drafts(leagueID)
	if err != nil {
		drafts = nil
	}
	s.draftID, s.offLeagueDraft = watchedDraft(draftID, drafts)

	// The two fallback rungs for pick ownership, resolved once here rather
	// than per poll. Both are best-effort: a board keyed to nobody — Jeff's
	// runs under a name, not a Sleeper id — resolves neither, and a failed
	// lookup leaves the rung disabled rather than the board broken.
	for _, r := range rosters {
		if r.OwnerID == ownerID {
			s.myRosterID = r.RosterID
			break
		}
	}
	if s.draftID != "" && ownerID != "" {
		if d, err := c.Draft(s.draftID); err == nil {
			s.mySlot = d.DraftOrder[ownerID]
		}
	}

	if s.offLeagueDraft {
		s.forgetKeepers()
	}
	return s, nil
}

// rosterShape is the league shape narrowed by your own lineup preferences.
//
// Use it wherever a roster of yours is assembled or scored; use s.shape where
// the pool is priced. The two differ because a preference is not a rule: the
// league's flex takes a tight end and eleven other managers will use it that
// way, so replacement depth — and every price that follows from it — has to be
// computed against the league's flex, not against the one you intend to field.
func (s *staticData) rosterShape() draft.PoolState {
	return s.prefs.RosterShape(s.shape)
}

// isMine reports whether a pick is yours, down a ladder of what Sleeper
// actually stamps on one. Each rung is tried only when the one above it says
// nothing, because each is a weaker claim than the last:
//
//	picked_by   the manager who made the pick. Set on every pick in a real
//	            draft — 168 of 168 in this league's last one — so on draft
//	            night the ladder stops here and the rest never runs.
//	roster_id   an autopick leaves picked_by empty. This league has
//	            cpu_autopick on, so being away from the keyboard must not
//	            drop the player off your own board.
//	draft_slot  a mock leaves both empty and carries the seat instead. It is
//	            the only thing a rehearsal can go on, and without it the
//	            budget a rehearsal exists to exercise never moves.
//
// A zero rung is unknown, not a match. An unset roster_id and an unset
// draft_slot are both 0, so a board that compared them would claim every
// unowned pick in the draft.
func (s *staticData) isMine(p sleeper.DraftPick) bool {
	if p.PickedBy != "" {
		return p.PickedBy == s.ownerID
	}
	if s.myRosterID != 0 && p.RosterID != 0 {
		return p.RosterID == s.myRosterID
	}
	if s.mySlot != 0 && p.DraftSlot != 0 {
		return p.DraftSlot == s.mySlot
	}
	return false
}

// Picks fetches the current draft's picks. This is the only call the live
// loop makes — one request, roughly 130ms.
func (s *staticData) Picks() ([]sleeper.DraftPick, error) {
	if s.draftID == "" {
		return nil, nil
	}
	return s.client.DraftPicks(s.draftID)
}

// Drafting reports whether a draft is in session.
//
// One small call, and it decides how hard the poll loop works. Sleeper's
// draft status is "pre_draft" until the commissioner starts it and
// "complete" when it ends, so outside that window there is nothing to
// discover by asking every second.
//
// "paused" counts as in session, which is not obvious and was found the hard
// way. A pause is a break in a draft you are sitting in — someone took a
// phone call — not the eleven months when no draft exists, and it ends
// without warning. Treating it as idle dropped the board to the minute
// cadence at exactly the moment a fast one matters, so the first bids after
// a resume landed on a board still showing the pool from before the break.
// The cost of being wrong the other way is a minute of polling a draft that
// is not moving; the cost of being wrong this way is bidding blind.
func (s *staticData) Drafting() bool {
	live, _ := s.DraftState()
	return live
}

// DraftState is one look at the draft: whether it is in session, and who is up
// for auction.
//
// Both come from the same object, so the nomination costs no request the poll
// was not already making. That matters, because a nomination lives inside a
// ten-second timer and has to be read every tick to be seen at all — a second
// call per tick to learn the same thing twice would double the board's share of
// Sleeper's budget for nothing.
//
// The nomination here is only the name Sleeper has on the block; whether the
// bidding is still running is settled downstream, when the picks feed reports
// him sold. See nominationFrom for why the draft object cannot answer that
// itself.
func (s *staticData) DraftState() (bool, *draft.Nomination) {
	if s.draftID == "" {
		return false, nil
	}
	d, err := s.client.Draft(s.draftID)
	if err != nil {
		// Unknown is treated as live: a blip must not stall the board on
		// the one night it matters. No nomination, though — a stale name is
		// a claim, where a missing banner is only silence.
		return true, nil
	}
	live := d.Status == "drafting" || d.Status == "paused"
	if !live {
		return false, nil
	}
	return true, s.nominationFrom(d)
}

// nominationFrom reads the nomination out of a draft, or nil if none is set.
//
// Note what this does NOT do: it does not try to decide from the draft object
// whether the bidding is still going. That was tried and it was wrong.
//
// timer_end_at looks like the answer and is not. Sleeper stamps it once and
// never advances it, so a player who stays nominated for twenty seconds — and
// they do, because bidding extends — carries a timer that went into the past
// after the first one or two. Measured against the live mock: one player held
// the block for twenty seconds while his timer drifted from +0.1s to -18.7s.
// Treating that as expiry blanked the banner about a second into every
// nomination, which reads from the outside as a banner that will not update.
//
// What actually ends a nomination is the player being sold, and that arrives
// on the picks feed. rebuildLocked drops him from the snapshot the moment he
// is in taken, which also covers the gap between nominations, when this field
// still holds the player who just went. So liveness is decided by the one
// source that reports it, and this reads the name.
func (s *staticData) nominationFrom(d sleeper.Draft) *draft.Nomination {
	id := d.Metadata.NominatedPlayerID
	if id == "" {
		return nil
	}
	slot, _ := strconv.Atoi(d.Metadata.OfferingSlot)
	offer, _ := strconv.Atoi(d.Metadata.HighestOffer)
	return &draft.Nomination{
		PlayerID:     id,
		Name:         s.nameOf(id),
		Position:     s.positionOf(id),
		Team:         s.team[id],
		HighestOffer: offer,
		Leader:       slot,
		Mine:         s.ownsSeat(d.Metadata.OfferingUserID, slot),
	}
}

// ownsSeat reports whether a user id or a draft slot is yours, down the same
// ladder isMine walks for a completed pick and for the same reason: Sleeper
// names the manager on a real draft and leaves the field empty in a mock,
// where the seat is all there is. A zero seat is unknown rather than a match,
// or an unbid nomination would read as yours.
func (s *staticData) ownsSeat(userID string, slot int) bool {
	if userID != "" {
		return userID == s.ownerID
	}
	return s.mySlot != 0 && slot != 0 && slot == s.mySlot
}

// The second-opinion sources this board reads, named so the selection above
// cannot depend on registry order. They must match ProjectionSources in
// internal/draft/projections.go.
const (
	// fpSharpSource fills the FP dollar column: the top-20 experts by past
	// accuracy, whose parting from consensus is what that column is for.
	fpSharpSource = "fantasypros-top20"
	// cielySource contributes ordering only.
	cielySource = "ciely"
)

// assignSecondOpinions decides which loaded second opinion fills which role.
//
// By name, never by position. The caller used to keep whichever came last,
// so the FP column's meaning depended on the order of a registry that says
// nothing about ordering — and reordering it would have changed the board
// without changing a line of board code.
//
// A source that is absent leaves its role empty, which reads as that signal
// being off rather than as an error: every consumer already handles it.
func assignSecondOpinions(sos []draft.SecondOpinion) (fp draft.SecondOpinion, cielyRank map[string]int) {
	for _, so := range sos {
		switch so.Name {
		case fpSharpSource:
			fp = so
		case cielySource:
			cielyRank = so.Rank
		}
	}
	return fp, cielyRank
}

// cielyDivergence is Ciely's positional rank against the primary's, per player.
//
// Sign follows DellDelta: positive where Ciely rates a player above the field,
// since a better rank is a smaller number. A player either source does not
// cover is absent rather than zero — zero is agreement, and the two must not
// be spelled the same way or every uncovered player reads as a consensus.
func cielyDivergence(primary, ciely map[string]int) map[string]int {
	out := make(map[string]int, len(ciely))
	for id, cr := range ciely {
		pr, ok := primary[id]
		if !ok || cr == 0 || pr == 0 {
			continue
		}
		if d := pr - cr; d != 0 {
			out[id] = d
		}
	}
	return out
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
// keeperScenarioSet resolves a research keeper scenario to the league keeper
// set it takes off the board. "" (draft night) and "none" (the research
// baseline) keep nobody through this path; "locks" keeps only the near-certain
// keepers, "expected" the standard surplus projection.
func (s *staticData) keeperScenarioSet(scenario string, aav map[string]float64) []draft.Entry {
	switch scenario {
	case "locks":
		return leagueKeepers(s.projected, aav, lockThreshold, s.forcedKeepers, s.declaredOwners)
	case "expected":
		return leagueKeepers(s.projected, aav, 0, s.forcedKeepers, s.declaredOwners)
	}
	return nil
}

func (s *staticData) Build(taken map[string]gone, edits boardEdits, keeperScenario string) (draft.Snapshot, error) {
	aav := map[string]float64{}
	for _, m := range s.market {
		aav[m.PlayerID] = m.AAV
	}

	// Resolve the keeper scenario. "" is draft night: the standard projection
	// deducts keeper money but leaves the kept players on the board, exactly as
	// before. A named scenario is research mode — the same set is deducted AND
	// its players leave the pool, so the money and the board agree about who is
	// kept. Keepers never arrive through the live feed, so this is the only
	// place they come off; pushing them through taken would double-count.
	keeperSet := s.keeperScenarioSet(keeperScenario, aav)
	scenarioActive := keeperScenario != ""

	var dollars, slots int
	var filled map[string]int
	var me draft.MyState
	if scenarioActive {
		dollars, slots, filled = poolFromKeepers(keeperSet, s.teams, s.budget)
		var mine []draft.Entry
		for _, e := range keeperSet {
			if e.OwnerID == s.ownerID {
				mine = append(mine, e)
			}
		}
		me = myStateFrom(mine, s.budget)
	} else {
		dollars, slots, filled = poolAfterKeepers(s.projected, aav, s.teams, s.budget, s.forcedKeepers, s.declaredOwners)
		me = myState(s.projected, aav, s.ownerID, s.budget, s.forcedKeepers, s.declaredOwners)
	}

	keeperIDs := make(map[string]bool, len(keeperSet))
	for _, e := range keeperSet {
		keeperIDs[e.PlayerID] = true
	}
	// A declared keeper leaves the live board too, not only a research one.
	// Draft night deliberately keeps *projected* keepers biddable, because a
	// projection is a guess and the team may yet let him go — but once his
	// owner has filed, he is a fact, and leaving him priced would solve the
	// pool against players nobody can buy. His money is already out either
	// way, deducted by poolAfterKeepers above, and the taken loop below skips
	// anyone in here, so nothing is counted twice.
	//
	// Every forced keeper's owner is declared by construction (that is what
	// putting him in the file means), so the forced set is exactly the set of
	// declared keepers.
	if !scenarioActive {
		for id := range s.forcedKeepers {
			keeperIDs[id] = true
		}
	}
	// A player off the board for either reason. Keepers are money-accounted in
	// the pool setup above, so the taken loop skips them to avoid deducting
	// twice; both still leave the value and cost pools below.
	offBoard := func(id string) bool {
		if keeperIDs[id] {
			return true
		}
		_, t := taken[id]
		return t
	}

	for id, g := range taken {
		if keeperIDs[id] {
			continue
		}
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
		if !offBoard(p.PlayerID) {
			available = append(available, p)
		}
	}
	values, err := draft.Solve(available, state)
	if err != nil {
		return draft.Snapshot{}, err
	}

	// The FantasyPros second projection, re-solved into dollars against the
	// same pool and state so its value is directly comparable to Value above.
	// Empty when the source was absent at load, which reads as no FP column.
	fantasyPros := map[string]draft.FPRead{}
	// The rank and the sharp move stand on their own, ahead of any solve:
	// FantasyPros ranks several hundred players it does not project, and an
	// ECR is a real read even with no dollar figure beside it.
	for id, rank := range s.fpRank {
		if offBoard(id) {
			continue
		}
		fantasyPros[id] = draft.FPRead{Rank: rank, SharpDelta: s.fpSharp[id]}
	}
	if len(s.fpProjections) > 0 {
		fpAvailable := make([]draft.Projection, 0, len(s.fpProjections))
		for _, p := range s.fpProjections {
			if !offBoard(p.PlayerID) {
				fpAvailable = append(fpAvailable, p)
			}
		}
		fpValues, err := draft.Solve(fpAvailable, state)
		if err != nil {
			return draft.Snapshot{}, err
		}
		for _, v := range fpValues {
			fp := fantasyPros[v.PlayerID]
			fp.Value = v.Price
			fp.Low, fp.High = v.PriceBand()
			fantasyPros[v.PlayerID] = fp
		}
	}

	openMarket := make([]draft.MarketPrice, 0, len(s.market))
	for _, m := range s.market {
		if !offBoard(m.PlayerID) {
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

	// The ceiling is what you can bid and still start a real player
	// everywhere — not what keeps you level with the room. The league-relative
	// figure still colours the risk band below; it collapses to a dollar the
	// moment you spend ahead of the field, and it also caps must-have and
	// favourite bids, so as a ceiling it took every premium down with it.
	//
	// Costs and points are already resolved here, and the flex is asked of the
	// roster shape rather than the pricing one so a preference about what may
	// start there is honoured.
	candidates := make([]draft.StarterCandidate, 0, len(values))
	for _, v := range values {
		if c, priced := costs[v.PlayerID]; priced {
			candidates = append(candidates, draft.StarterCandidate{
				PlayerID: v.PlayerID, Position: v.Position,
				Cost: c, Points: s.points[v.PlayerID],
			})
		}
	}
	recommended := draft.AffordableCeiling(me, candidates, s.thresholds,
		s.prefs.RosterShape(s.shape).FlexPositions)
	// Resolved once and used by both consumers below. Passing the raw set
	// to either would put a read on the board that the must-have budget
	// line does not know about — the two would disagree about the same
	// player on the same screen.
	leans := s.effectiveLeans(edits)
	players := draft.BuildSignals(draft.SignalInputs{
		Values: values, Costs: costs, Subvertadown: s.subvert,
		PrimaryPoints: s.points, Teams: s.team, Availability: s.availability,
		Leans: leans, Traits: s.traits, RecommendedBid: recommended,
		FantasyPros: fantasyPros, Offenses: s.prefs.OffenseSet(),
		DellSharp: s.dellSharp, CielySharp: s.cielyDelta,
		BorisTier: s.borisTier,
	})
	snap := draft.Assemble(s.season, state, me, players, leans, s.tempo(taken, costs), s.thresholds, recommended, append(append([]string(nil), s.warnings...), s.leanWarnings...))
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

	// Effective scarcity: the board as *you* see it once your own preferences
	// have spent the offenses you already have a piece of. Only when a filter
	// is set and you own something for it to bite on.
	if s.prefs.Active() {
		owned := s.ownedRoster(taken)
		if blocked := draft.BlockedForMe(owned, snap.Players, s.prefs); len(blocked) > 0 {
			snap.Blocked = blocked
			snap.EffectiveScarcity = draft.EffectiveScarcity(snap.Players, blocked, state, s.thresholds)
			for i := range snap.Players {
				if r, ok := blocked[snap.Players[i].PlayerID]; ok {
					snap.Players[i].BlockedReason = r
				}
			}
		}
	}

	// Tell the page which scenario it is showing and who left the pool because
	// of it, marquee keepers first, so it can name the hypothetical and list
	// the players gone.
	snap.KeeperScenario = keeperScenario
	for _, e := range keeperSet {
		tier := "likely"
		if s.forcedKeepers[e.PlayerID] || aav[e.PlayerID]-float64(e.LeaguePrice) >= lockThreshold {
			tier = "lock"
		}
		snap.Kept = append(snap.Kept, draft.KeptPlayer{
			Name: e.Name, Position: e.Position, Price: e.LeaguePrice, Tier: tier,
		})
	}
	sort.Slice(snap.Kept, func(i, j int) bool { return snap.Kept[i].Price > snap.Kept[j].Price })
	return snap, nil
}

// ownedRoster is who you already have: your projected keepers plus anyone you
// have bought live. The personal preference filter measures every other player
// against this set. Minimal signals — position and team are all the filter
// reads.
func (s *staticData) ownedRoster(taken map[string]gone) []draft.PlayerSignals {
	var out []draft.PlayerSignals
	seen := map[string]bool{}
	add := func(id, pos string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, draft.PlayerSignals{
			PlayerID: id,
			Name:     s.nameOf(id),
			Position: pos,
			Team:     s.team[id],
		})
	}

	aav := map[string]float64{}
	for _, m := range s.market {
		aav[m.PlayerID] = m.AAV
	}
	for _, e := range projectedKeepers(s.projected, aav, s.ownerID, s.forcedKeepers, s.declaredOwners) {
		add(e.PlayerID, e.Position)
	}
	for id, g := range taken {
		if g.mine {
			add(id, s.positionOf(id))
		}
	}
	return out
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
func (s *staticData) effectiveLeans(edits boardEdits) draft.Leans {
	if len(edits) == 0 {
		return s.leans
	}
	out := make(draft.Leans, len(s.leans)+len(edits))
	for k, v := range s.leans {
		out[k] = v
	}
	for k, e := range edits {
		if e.lean != nil && *e.lean == "" {
			// An edit back to nothing removes the read rather than leaving
			// a blank one, which WalkAway would treat as an unknown lean.
			//
			// Unless he is tagged a favorite, which is not a read and outlives
			// one: WalkAway reads a blank lean as no conviction and still
			// applies the favorite stretch, which is exactly right for a name
			// you want without an opinion on his range. The tag is resolved
			// the same way the save path resolves it — this edit if it spoke
			// to it, otherwise whatever the loaded set already says.
			prior := out[k]
			if !favorite(e, prior) {
				delete(out, k)
				continue
			}
			prior.Player, prior.Lean, prior.Source, prior.Favorite = e.player, "", boardSource, true
			out[k] = prior
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
			merged.Player = e.player
		}
		if e.lean != nil {
			merged.Lean, merged.Source = *e.lean, boardSource
		}
		if e.favorite != nil {
			merged.Favorite = *e.favorite
		}
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
	for _, e := range projectedKeepers(s.projected, aav, ownerID, s.forcedKeepers, s.declaredOwners) {
		out = append(out, draft.RosterSpot{
			Player: draft.PlayerSignals{
				PlayerID:      e.PlayerID,
				Name:          e.Name,
				Position:      e.Position,
				Team:          s.team[e.PlayerID],
				PrimaryPoints: s.points[e.PlayerID],
				Cost:          int(aav[e.PlayerID] + 0.5),
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

// forgetKeepers empties everything this board knows about keepers, because a
// mock has none.
//
// The files that decide them — owners.csv, rulings.csv, keeper-locks.csv —
// are keyed to the real league, and a *league* mock resolves every one of them
// while Sleeper hands all twelve teams a flat budget and the whole pool. Left
// in, they deduct money nobody has spent, take players off a board where they
// are biddable, and seat keepers on a roster you do not have.
//
// Done once here rather than at each of those sites on purpose. Three separate
// symptoms of this had already been fixed one at a time — the pool, the
// board's opening scenario, and the Draft night button — and a fourth path
// that reads a keeper field would have been wrong again. Emptying the inputs
// makes every reader correct without having to know it is a rehearsal.
func (s *staticData) forgetKeepers() {
	s.projected = nil
	s.forcedKeepers = nil
	s.declaredOwners = nil
	s.keeperOf = map[string]int{}
}

// wonSpot builds the roster line for a player you bought at auction.
//
// He is off the board by the time this is asked — that is what winning him
// means — so his signals cannot be read from the pool and are assembled here
// the way heldRoster assembles a keeper's. Price is what you actually paid,
// not what anything thinks he is worth.
func (s *staticData) wonSpot(playerID string, price int) draft.RosterSpot {
	var aav float64
	for _, m := range s.market {
		if m.PlayerID == playerID {
			aav = m.AAV
			break
		}
	}
	return draft.RosterSpot{
		Player: draft.PlayerSignals{
			PlayerID:      playerID,
			Name:          s.nameOf(playerID),
			Position:      s.positionOf(playerID),
			Team:          s.team[playerID],
			PrimaryPoints: s.points[playerID],
			Cost:          int(aav + 0.5),
			// Traits for the same reason a keeper carries them: a player
			// who occupies a slot while being invisible to every shape made
			// of player types makes the lineup measure as though one of its
			// fourteen were a blank.
			Traits: s.traits[playerID],
		},
		Price: price,
	}
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
	// Through the same matcher the board's reads went through, or the
	// lean-edit endpoint would reject a name the board itself resolves.
	if s.matcher != nil {
		if canonical, ok := s.matcher.Canonical(name); ok {
			name = canonical
		}
	}
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

// poolNames lists the projection source's spelling of every player, which
// is what a lean has to match.
func poolNames(projections []draft.Projection) []string {
	out := make([]string, 0, len(projections))
	for _, p := range projections {
		out = append(out, p.Name)
	}
	return out
}

// refreshLeanWarnings recomputes everything the strip says about the reads.
//
// Called at load and again after every reload, because both facts it reports
// move with the file: a disagreement can be settled by an edit, and a name
// can be corrected.
func (s *staticData) refreshLeanWarnings() {
	var out []string
	for _, pl := range s.leans.Contested() {
		var against []string
		for _, o := range pl.Disagreement() {
			against = append(against, fmt.Sprintf("%s says %s", o.Source, o.Lean))
		}
		out = append(out, fmt.Sprintf("%s: you say %s, %s",
			pl.Player, pl.Lean, strings.Join(against, ", ")))
	}
	// A read naming nobody on the board can never fire. `draftroom leans`
	// reports this too, but it works from the source file alone and the
	// board's pool is the smaller thing: a source row that failed to match a
	// Sleeper id is in that file and not on this board. Only here is the
	// difference knowable, so only here can it be said.
	//
	// One line per read rather than a count, because "2 leans unmatched"
	// says a thing is wrong without saying which conviction you have lost.
	for _, u := range s.leans.Unmatched(poolNames(s.projections), s.matcher) {
		line := fmt.Sprintf("%s: %s reaches no player on the board", u.Lean.Player, u.Lean.Lean)
		if u.Suggestion != "" {
			line += fmt.Sprintf(" — did you mean %s?", u.Suggestion)
		}
		out = append(out, line)
	}
	s.leanWarnings = out
}

// watchedDraft picks the draft to follow.
//
// An explicit id wins, and exists for a mock: Sleeper's standalone mock drafts
// belong to no league, so /league/<id>/drafts never returns one and the only
// way to follow it is to be told. Discovery stays the default because a real
// league has exactly one draft that matters, and Sleeper returns them newest
// first, so the current season's is the one in front.
//
// The second return says the draft is not one of the league's own. That is
// what makes a board a rehearsal rather than the real thing, and the runtime
// state it keeps has to be kept apart from the state the live board depends
// on. A league mock reports true as readily as a standalone one: it hangs off
// the league in Sleeper's metadata, but /league/<id>/drafts does not return
// it, and nothing about it is the draft you will actually play.
func watchedDraft(explicit string, drafts []sleeper.Draft) (string, bool) {
	if explicit == "" {
		if len(drafts) > 0 {
			return drafts[0].DraftID, false
		}
		return "", false
	}
	for _, d := range drafts {
		if d.DraftID == explicit {
			return explicit, false
		}
	}
	return explicit, true
}

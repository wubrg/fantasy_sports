package draft

import (
	"fmt"
	"sort"

	"leaguehome/internal/sleeper"
)

// How a player came to be on a roster in a given season. These are the
// possible sources of the "prior value" a keeper price is built from.
const (
	MethodDraft     = "draft"
	MethodKeeper    = "keeper"
	MethodWaiver    = "waiver"
	MethodFreeAgent = "free_agent"
)

// Ledger flags. Each marks something worth a human glance, not a defect.
const (
	// FlagNoPriorRecord means the player has no recorded value in any
	// earlier season, so the price falls back to the league minimum.
	// Expected for rookies kept off a practice-squad stash.
	FlagNoPriorRecord = "no-prior-season-record"
	// FlagPriorSeasonCorrupt means the prior value came from a season
	// marked unusable, so the price inherits that season's bad data.
	FlagPriorSeasonCorrupt = "prior-season-data-corrupt"
	// FlagChangedHands means the player is being kept by a different
	// owner than the one who established the cost basis. This is
	// informational: league practice carries the basis with the player
	// through a trade, which is what this ledger does.
	FlagChangedHands = "changed-hands-since-basis"
	// FlagResetByPickup means an in-season waiver or free agent pickup
	// reset the player's basis to undrafted, restarting the ladder.
	FlagResetByPickup = "basis-reset-by-pickup"
	// FlagEscalationResetByTrade means a change of hands restarted the
	// keep count under the league's current trade rule.
	FlagEscalationResetByTrade = "escalation-reset-by-trade"
)

// SeasonData is one season's already-fetched inputs. Keeping the network
// out of this package makes the whole ledger testable from fixtures, which
// matters because its correctness is a rules question, not an API question.
type SeasonData struct {
	Year         string
	LeagueID     string
	Picks        []sleeper.DraftPick
	Transactions []sleeper.Transaction
	Rosters      []sleeper.Roster
	// Corrupt marks a season whose recorded dollar values cannot be
	// trusted. Entries chaining through it are flagged rather than
	// silently believed.
	Corrupt bool
	// CorruptReason explains why, and is echoed into reports.
	CorruptReason string
}

// Acquisition records how a player's current cost basis was established.
//
// Crucially this is keyed by player, not by owner. Hit or Miss carries a
// player's cost basis and keep count with the player through trades: Saquon
// Barkley was drafted for $51 by one manager in 2024 and kept for $56 by a
// different manager in 2025, and Amon-Ra St. Brown's keep count kept
// climbing ($10 -> $20 -> $35) as he changed hands. A pickup off waivers or
// free agency resets the basis instead; that is why De'Von Achane, bought
// with a large FAAB bid, was still kept at the $10 minimum.
type Acquisition struct {
	Method string
	Cost   int
	Season string
	// Owner is who held the player when the basis was set, used only to
	// flag that a keeper has since changed hands.
	Owner string
}

// Entry is one keeper decision priced under league rules, alongside what
// Sleeper recorded, so the two can be compared.
type Entry struct {
	Season      string
	OwnerID     string
	PlayerID    string
	Name        string
	Position    string
	KeepCount   int
	PriorValue  int
	PriorMethod string
	// PriorSeason is the season PriorValue was charged in, empty when no
	// prior record was found.
	PriorSeason string
	// LeaguePrice is what the league's rules say this keeper costs.
	LeaguePrice int
	// SleeperAmount is what Sleeper recorded on the keeper pick. Sleeper
	// does not apply the escalating ladder, so it is only as right as
	// whoever typed it in.
	SleeperAmount int
	Flags         []string
}

// Variance is how much the league price exceeds what Sleeper charged.
// Positive means the manager was undercharged in Sleeper.
func (e Entry) Variance() int { return e.LeaguePrice - e.SleeperAmount }

// state is the league-wide cost basis and keep count for every player at a
// point in time, keyed by player ID.
type state struct {
	acq       map[string]Acquisition
	keepCount map[string]int
}

func newState() state {
	return state{acq: map[string]Acquisition{}, keepCount: map[string]int{}}
}

func (s state) clone() state {
	out := newState()
	for k, v := range s.acq {
		out.acq[k] = v
	}
	for k, v := range s.keepCount {
		out.keepCount[k] = v
	}
	return out
}

// Ledger is the keeper history of the league: every keeper priced under
// league rules, plus enough carried state to price next year's keepers
// before they appear in Sleeper.
type Ledger struct {
	Entries []Entry
	// Seasons are the years processed, in ascending order.
	Seasons []string

	rules     Rules
	overrides Overrides
	// stateAfter maps a season year to the league state at its end.
	stateAfter map[string]state
	lastSeason string
}

// BuildLedger walks the seasons in chronological order, chaining each
// keeper's price off the price actually charged the previous season.
//
// The chaining is the point: a player drafted at $10 and kept three times
// costs $15, then $25, then $40, and each step depends on the one before.
// Sleeper's recorded amounts are never chain inputs, only comparison
// targets, because Sleeper does not apply the ladder.
//
// Cost basis persists across seasons until something resets it, so a
// player drafted in 2023 and kept in 2025 after sitting on a roster
// through 2024 still chains off the 2023 price.
func BuildLedger(seasons []SeasonData, r Rules) (*Ledger, error) {
	return BuildLedgerWithOverrides(seasons, r, nil)
}

// BuildLedgerWithOverrides is BuildLedger plus commissioner rulings.
//
// An override replaces the computed price and then becomes the basis the
// next season chains off, so a single ruling corrects everything
// downstream of it rather than having to be repeated every year.
func BuildLedgerWithOverrides(seasons []SeasonData, r Rules, overrides Overrides) (*Ledger, error) {
	ordered := make([]SeasonData, len(seasons))
	copy(ordered, seasons)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Year < ordered[j].Year })

	l := &Ledger{rules: r, overrides: overrides, stateAfter: make(map[string]state)}
	carried := newState()
	prevCorrupt := false
	prevYear := ""

	for _, s := range ordered {
		cur := carried.clone()
		owners := ownerByRoster(s.Rosters)

		for _, p := range s.Picks {
			owner := pickOwner(p, owners)
			if owner == "" {
				return nil, fmt.Errorf("draft: %s pick %d has no resolvable owner", s.Year, p.PickNo)
			}

			if !p.IsKeeper {
				// A player redrafted in the auction establishes a fresh
				// basis and restarts the keeper ladder.
				cur.acq[p.PlayerID] = Acquisition{
					Method: MethodDraft, Cost: p.Metadata.Dollars(), Season: s.Year, Owner: owner,
				}
				cur.keepCount[p.PlayerID] = 0
				continue
			}

			entry, price := l.priceKeeper(s, p, owner, carried, prevCorrupt, prevYear)
			l.Entries = append(l.Entries, entry)
			cur.acq[p.PlayerID] = Acquisition{
				Method: MethodKeeper, Cost: price, Season: s.Year, Owner: owner,
			}
			cur.keepCount[p.PlayerID] = entry.KeepCount
		}

		// In-season pickups happen after the draft and reset the basis.
		// Trades are deliberately absent: they move a player without
		// disturbing the basis that follows him.
		for playerID, a := range inSeasonAcquisitions(s, owners) {
			// Re-adding a player you already own is roster churn, not an
			// acquisition. Managers drop and re-add their own players
			// constantly for bye weeks and injuries, and the league does
			// not treat that as resetting a keeper's ladder position.
			if prior, ok := cur.acq[playerID]; ok && !l.rules.SelfPickupResets && prior.Owner == a.Owner {
				continue
			}
			cur.acq[playerID] = a
			cur.keepCount[playerID] = 0
		}

		l.stateAfter[s.Year] = cur
		l.Seasons = append(l.Seasons, s.Year)
		l.lastSeason = s.Year
		carried, prevCorrupt, prevYear = cur, s.Corrupt, s.Year
	}
	return l, nil
}

// priceKeeper resolves one keeper pick's prior value and applies the ladder.
func (l *Ledger) priceKeeper(s SeasonData, p sleeper.DraftPick, owner string, prev state, prevCorrupt bool, prevYear string) (Entry, int) {
	keepCount := prev.keepCount[p.PlayerID] + 1
	prior, hadPrior := prev.acq[p.PlayerID]

	var flags []string
	if !hadPrior {
		flags = append(flags, FlagNoPriorRecord)
	}
	changedHands := hadPrior && prior.Owner != "" && prior.Owner != owner
	if changedHands {
		flags = append(flags, FlagChangedHands)
		// Under the current rule a trade restarts the ladder, though the
		// basis still follows the player.
		if l.rules.tradeResets(s.Year) {
			keepCount = 1
			flags = append(flags, FlagEscalationResetByTrade)
		}
	}
	if hadPrior && prior.Method == MethodWaiver {
		flags = append(flags, FlagResetByPickup)
	}
	// A prior value read out of a corrupt season poisons everything
	// downstream of it, so say so even though the arithmetic "worked".
	if hadPrior && prevCorrupt && prior.Season == prevYear {
		flags = append(flags, FlagPriorSeasonCorrupt)
	}

	// A ruling can pin the ladder position as well as the price, for
	// values established outside Sleeper such as an expansion draft.
	if ov, ok := l.overrides.Lookup(s.Year, p.PlayerID); ok && ov.KeepCount > 0 {
		keepCount = ov.KeepCount
	}

	priorValue := l.rules.priorValueOf(prior)
	price, priceFlags, err := l.rules.Price(priorValue, keepCount)
	if err != nil {
		// Price only errors on a keep count below 1, which cannot happen
		// here since keepCount is at least 1 by construction.
		price = l.rules.Minimum
	}
	flags = append(flags, priceFlags...)
	price, flags = l.overrides.apply(s.Year, p.PlayerID, price, flags)

	method, priorSeason := prior.Method, prior.Season
	if !hadPrior {
		method, priorSeason = "unknown", ""
	}

	return Entry{
		Season:        s.Year,
		OwnerID:       owner,
		PlayerID:      p.PlayerID,
		Name:          p.Metadata.Name(),
		Position:      p.Metadata.Position,
		KeepCount:     keepCount,
		PriorValue:    priorValue,
		PriorMethod:   method,
		PriorSeason:   priorSeason,
		LeaguePrice:   price,
		SleeperAmount: p.Metadata.Dollars(),
		Flags:         flags,
	}, price
}

// inSeasonAcquisitions returns the last basis-resetting pickup of each
// player during the season, keyed by player ID.
//
// Only completed transactions count: Sleeper also returns losing waiver
// bids, and counting those would overstate acquisition costs. Trades are
// excluded on purpose — league practice carries a player's basis and keep
// count with him, so a trade changes who holds the player without
// resetting anything.
func inSeasonAcquisitions(s SeasonData, owners map[int]string) map[string]Acquisition {
	txns := make([]sleeper.Transaction, 0, len(s.Transactions))
	for _, t := range s.Transactions {
		if t.Status != "complete" || len(t.Adds) == 0 || t.Type == "trade" {
			continue
		}
		txns = append(txns, t)
	}
	// Chronological order so that the final pickup of a player wins.
	sort.SliceStable(txns, func(i, j int) bool { return txns[i].Created < txns[j].Created })

	out := map[string]Acquisition{}
	for _, t := range txns {
		for playerID, rosterID := range t.Adds {
			owner, ok := owners[rosterID]
			if !ok {
				continue
			}
			a := Acquisition{Season: s.Year, Owner: owner}
			if t.Type == "waiver" {
				a.Method, a.Cost = MethodWaiver, t.Bid()
			} else {
				// free_agent and commissioner adds cost nothing.
				a.Method, a.Cost = MethodFreeAgent, 0
			}
			out[playerID] = a
		}
	}
	return out
}

// ownerByRoster maps roster IDs to owner IDs for a season.
func ownerByRoster(rosters []sleeper.Roster) map[int]string {
	m := make(map[int]string, len(rosters))
	for _, r := range rosters {
		m[r.RosterID] = r.OwnerID
	}
	return m
}

// pickOwner resolves the owner of a pick, preferring the user ID Sleeper
// stamps on it and falling back to the roster map for autopicked selections
// where picked_by is empty.
func pickOwner(p sleeper.DraftPick, owners map[int]string) string {
	if p.PickedBy != "" {
		return p.PickedBy
	}
	return owners[p.RosterID]
}

// Declared is a keeper an owner intends to keep for an upcoming draft,
// before it exists in Sleeper's picks feed. This is how 2026 keepers get
// priced from a hand-maintained list.
type Declared struct {
	OwnerID  string
	PlayerID string
	// Name and Position are optional, for readable reports when the
	// player dictionary isn't loaded.
	Name     string
	Position string
}

// PriceDeclared prices keepers for an upcoming season using the state at
// the end of the last season in the ledger. Entries come back in the same
// shape as historical ones, with SleeperAmount left at zero because Sleeper
// has not recorded anything yet.
func (l *Ledger) PriceDeclared(season string, declared []Declared) ([]Entry, error) {
	if l.lastSeason == "" {
		return nil, fmt.Errorf("draft: cannot price %s keepers, ledger has no completed seasons", season)
	}
	prev := l.stateAfter[l.lastSeason]

	entries := make([]Entry, 0, len(declared))
	for _, d := range declared {
		keepCount := prev.keepCount[d.PlayerID] + 1
		prior, hadPrior := prev.acq[d.PlayerID]

		var flags []string
		if !hadPrior {
			flags = append(flags, FlagNoPriorRecord)
		}
		if hadPrior && prior.Owner != "" && prior.Owner != d.OwnerID {
			flags = append(flags, FlagChangedHands)
			if l.rules.tradeResets(season) {
				keepCount = 1
				flags = append(flags, FlagEscalationResetByTrade)
			}
		}
		if hadPrior && prior.Method == MethodWaiver {
			flags = append(flags, FlagResetByPickup)
		}
		// An expansion or new-owner draft sets a keeper's value outside
		// Sleeper entirely, so a ruling can pin the ladder position too.
		if ov, ok := l.overrides.Lookup(season, d.PlayerID); ok && ov.KeepCount > 0 {
			keepCount = ov.KeepCount
		}

		priorValue := l.rules.priorValueOf(prior)
		price, priceFlags, err := l.rules.Price(priorValue, keepCount)
		if err != nil {
			return nil, fmt.Errorf("draft: pricing %s for owner %s: %w", d.PlayerID, d.OwnerID, err)
		}
		flags = append(flags, priceFlags...)
		price, flags = l.overrides.apply(season, d.PlayerID, price, flags)

		method, priorSeason := prior.Method, prior.Season
		if !hadPrior {
			method, priorSeason = "unknown", ""
		}
		entries = append(entries, Entry{
			Season:      season,
			OwnerID:     d.OwnerID,
			PlayerID:    d.PlayerID,
			Name:        d.Name,
			Position:    d.Position,
			KeepCount:   keepCount,
			PriorValue:  priorValue,
			PriorMethod: method,
			PriorSeason: priorSeason,
			LeaguePrice: price,
			Flags:       flags,
		})
	}
	return entries, nil
}

// Budgets returns each owner's remaining auction budget for a season,
// given the full per-team budget and that season's priced keepers. Owners
// with no keepers are absent; callers should treat a miss as the full
// budget.
//
// This is the number the draft room runs on. Sleeper shows every team a
// flat $200 because it doesn't apply the ladder, so using Sleeper's figure
// would misjudge which rivals can still afford a bid.
func Budgets(entries []Entry, fullBudget int) map[string]int {
	spent := map[string]int{}
	for _, e := range entries {
		spent[e.OwnerID] += e.LeaguePrice
	}
	out := make(map[string]int, len(spent))
	for owner, s := range spent {
		out[owner] = fullBudget - s
	}
	return out
}

// EntriesForSeason filters the ledger to one season's keepers.
func (l *Ledger) EntriesForSeason(season string) []Entry {
	var out []Entry
	for _, e := range l.Entries {
		if e.Season == season {
			out = append(out, e)
		}
	}
	return out
}

package board

import (
	"fmt"
	"sort"
	"strings"

	"edge/internal/oddspull"
	"edge/internal/wager"
)

// An odds capture (a DraftKings HAR/JSON drop) prices the whole game -- a
// line and both sides' prices together -- unlike a pasted blob, which is one
// price per team with no matchup punctuation at all. That is why this sync
// path can cover spread and total as well as the moneyline: there is no
// positional-pairing risk to guard against the way ParsePaste/PlanImport do,
// only the ordinary job of matching a capture's event and market names
// against this week's schedule.

// gameSlot locates a schedule entry and records whether it was found with the
// two teams reversed from how the source listed them.
type gameSlot struct {
	id       string
	reversed bool
}

// teamIndex indexes the schedule both ways round, away-home and home-away, so
// a source that lists the home team first still resolves unambiguously -- a
// team plays at most once a week, so the reversed match cannot collide with
// another game.
func (d *Doc) teamIndex() map[[2]string]gameSlot {
	idx := map[[2]string]gameSlot{}
	for id, g := range d.Games {
		idx[[2]string{g.Away, g.Home}] = gameSlot{id, false}
		idx[[2]string{g.Home, g.Away}] = gameSlot{id, true}
	}
	return idx
}

// matchEvent resolves an odds capture's event string (e.g. "NE @ SEA") to a
// game on this week's schedule.
//
// DraftKings' sportscontent event names are consistently "AWAY @ HOME" -- see
// oddspull_test.go's fixture, where Seattle hosting New England prints as
// "NE @ SEA" -- matching the universal away-at-home convention sportsbooks and
// broadcasts use everywhere. A capture that listed the home team first would
// still resolve correctly: the schedule is checked both ways round, the same
// defence PlanImport already applies to a pasted blob, so nothing here has to
// assume the order rather than verifying it.
func (d *Doc) matchEvent(event string) (gameID, away, home string, reversed bool, err error) {
	var tokens []string
	for _, f := range strings.FieldsFunc(event, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		tokens = append(tokens, CanonicalTeam(f))
	}
	known := d.Teams()
	var teams []string
	seen := map[string]bool{}
	for _, t := range tokens {
		if known[t] && !seen[t] {
			seen[t] = true
			teams = append(teams, t)
		}
	}
	if len(teams) != 2 {
		return "", "", "", false, fmt.Errorf("could not resolve two teams from event %q", event)
	}

	s, ok := d.teamIndex()[[2]string{teams[0], teams[1]}]
	if !ok {
		return "", "", "", false, fmt.Errorf(
			"%s/%s is not a week %d matchup", teams[0], teams[1], d.Week)
	}
	g := d.Games[s.id]
	return s.id, g.Away, g.Home, s.reversed, nil
}

// OddsPair is one matchup's game-line price, resolved against this week's
// schedule from an odds capture and ready to diff against the board.
type OddsPair struct {
	GameID string
	Market string // "ml", "spread" or "total"
	// Line is nil for ml. For spread it is the AWAY team's signed number, the
	// board's storage convention (see the header comment in board.go). For
	// total it is the over/under number, which both sides share.
	Line *float64
	// AwayPrice/HomePrice hold the away and home prices for ml and spread. For
	// total, which has no away or home side, they hold the over and under
	// prices respectively -- the same A/B order FormatHandicap already uses
	// for a total cell ("line over/under").
	AwayPrice, HomePrice wager.American
}

// marketKind reads an odds capture's market name into one of the board's
// three market keys. It only ever sees Category=="Game" outcomes (the caller
// filters), so this just tells the three apart; anything it cannot place --
// including the bare "point" match categorise() also treats as a game line --
// is left unclassified rather than guessed at.
func marketKind(name string) string {
	m := strings.ToLower(name)
	switch {
	case strings.Contains(m, "moneyline"):
		return "ml"
	case strings.Contains(m, "spread"):
		return "spread"
	case strings.Contains(m, "total"):
		return "total"
	default:
		return ""
	}
}

// splitAwayHome tells the two sides of a moneyline or spread pair apart by
// matching each selection's label against the game's away and home teams
// (e.g. "NE Patriots" mentions NE). A selection that mentions zero or both
// teams cannot be placed and leaves that side missing.
func splitAwayHome(d *Doc, away, home string, sides []oddspull.Outcome) (a, h oddspull.Outcome, ok bool) {
	var haveA, haveH bool
	for _, s := range sides {
		mentioned := d.TeamsMentioned(s.Selection)
		if len(mentioned) != 1 {
			continue
		}
		switch mentioned[0] {
		case away:
			a, haveA = s, true
		case home:
			h, haveH = s, true
		}
	}
	return a, h, haveA && haveH
}

// splitOverUnder tells the two sides of a total pair apart by the "over"/
// "under" word in the selection label -- a total carries no team name.
func splitOverUnder(sides []oddspull.Outcome) (over, under oddspull.Outcome, ok bool) {
	var haveO, haveU bool
	for _, s := range sides {
		l := strings.ToLower(s.Selection)
		switch {
		case strings.Contains(l, "over"):
			over, haveO = s, true
		case strings.Contains(l, "under"):
			under, haveU = s, true
		}
	}
	return over, under, haveO && haveU
}

// OddsPairsFromOutcomes turns the Game-category outcomes from an odds
// capture into pairs ready for PlanOddsSync, matching each one's event and
// market against this week's schedule.
//
// An outcome that cannot be matched -- an event that does not resolve to two
// known teams, a market whose two sides cannot be told apart, a market this
// board does not track -- is skipped rather than erroring. A capture is messy
// by nature (extra markets, alt lines sharing an event, unexpected labels),
// and one unreadable group must not block every other game's clean pair the
// way a shifted paste blob would; PlanOddsSync still requires GameID to exist
// on the schedule and validates every formatted cell before it is offered as
// a change.
func (d *Doc) OddsPairsFromOutcomes(outs []oddspull.Outcome) []OddsPair {
	groups := map[string][]oddspull.Outcome{}
	var order []string
	for _, o := range outs {
		if o.Category != "Game" {
			continue
		}
		key := o.Event + "|" + o.MarketID
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], o)
	}

	var pairs []OddsPair
	for _, key := range order {
		sides := groups[key]
		if len(sides) != 2 {
			continue // de-vig (and this sync) only handles a clean two-sided market
		}
		kind := marketKind(sides[0].Market)
		if kind == "" {
			continue
		}
		gameID, away, home, _, err := d.matchEvent(sides[0].Event)
		if err != nil {
			continue
		}

		p := OddsPair{GameID: gameID, Market: kind}
		switch kind {
		case "ml":
			a, h, ok := splitAwayHome(d, away, home, sides)
			if !ok {
				continue
			}
			p.AwayPrice, p.HomePrice = a.Price, h.Price
		case "spread":
			a, h, ok := splitAwayHome(d, away, home, sides)
			if !ok || a.Line == nil {
				continue
			}
			p.AwayPrice, p.HomePrice = a.Price, h.Price
			p.Line = a.Line
		case "total":
			over, under, ok := splitOverUnder(sides)
			if !ok || over.Line == nil {
				continue
			}
			p.AwayPrice, p.HomePrice = over.Price, under.Price
			p.Line = over.Line
		}
		pairs = append(pairs, p)
	}
	return pairs
}

// PlanOddsSync matches odds-capture pairs against this week's schedule and
// returns the cells they would change. It writes nothing.
//
// Unlike PlanImport (moneyline only, from a paste blob with no line data),
// this covers all three markets: each pair already carries a line and both
// sides' prices together, resolved by OddsPairsFromOutcomes.
func (d *Doc) PlanOddsSync(pairs []OddsPair, book string) ([]ImportChange, error) {
	if book == "" {
		return nil, fmt.Errorf("no book named")
	}
	if book == Consensus {
		return nil, fmt.Errorf("consensus is a generated reference column, not a book you can import into")
	}
	if !KnownBook(book) {
		return nil, fmt.Errorf("unknown book %q", book)
	}

	var changes []ImportChange
	for _, p := range pairs {
		g, ok := d.Games[p.GameID]
		if !ok {
			return nil, fmt.Errorf("no game %q in week %d", p.GameID, d.Week)
		}

		var value string
		switch p.Market {
		case "ml":
			value = FormatMarket(wager.Market{A: p.AwayPrice, B: p.HomePrice})
			if _, _, err := ParseMarket(value); err != nil {
				return nil, fmt.Errorf("%s: %w", p.GameID, err)
			}
		case "spread", "total":
			if p.Line == nil {
				return nil, fmt.Errorf("%s %s: no line", p.GameID, p.Market)
			}
			value = FormatHandicap(*p.Line, wager.Market{A: p.AwayPrice, B: p.HomePrice})
			if _, _, _, err := ParseHandicap(value); err != nil {
				return nil, fmt.Errorf("%s: %w", p.GameID, err)
			}
		default:
			return nil, fmt.Errorf("%s: unknown market %q (want ml, spread or total)", p.GameID, p.Market)
		}

		var old string
		switch p.Market {
		case "ml":
			old = g.Books[book].ML
		case "spread":
			old = g.Books[book].Spread
		case "total":
			old = g.Books[book].Total
		}
		if old == value {
			continue // no-op; nothing to confirm
		}
		changes = append(changes, ImportChange{
			GameID: p.GameID, Away: g.Away, Home: g.Home,
			Book: book, Market: p.Market, Old: old, New: value,
		})
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].GameID != changes[j].GameID {
			return changes[i].GameID < changes[j].GameID
		}
		return changes[i].Market < changes[j].Market
	})
	return changes, nil
}

// ApplyOddsSync writes a plan produced by PlanOddsSync into the document. A
// separate step from PlanOddsSync so the caller can show the diff first --
// see ApplyImport for why.
func (d *Doc) ApplyOddsSync(changes []ImportChange) error {
	for _, c := range changes {
		if err := d.SetPrice(c.GameID, c.Book, c.Market, c.New); err != nil {
			return err
		}
	}
	return nil
}

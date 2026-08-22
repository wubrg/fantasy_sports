package main

import (
	"edge/internal/ledger"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"edge/internal/board"
)

// The report over HTTP.
//
// `edgectl board report` already prints all of this, but the terminal is not
// where the board is used. Prices are typed on a phone while looking at a
// book's app, and the answer to "so which of these should I actually bet" has
// to arrive in the same place -- otherwise every entry session ends by walking
// to a desk, which is the friction this page exists to remove.
//
// The shape is deliberately close to what the screen shows rather than a
// faithful dump of board.Analysis: a phone has room for the wagers, the dogs
// behind them, and anything that looks mistyped. Missing games and the raw
// per-game de-vig table stay in the CLI, where there is width for them.

type reportJSON struct {
	Week    int          `json:"week"`
	Book    string       `json:"book"`
	Priced  int          `json:"priced"`
	Total   int          `json:"total"`
	Target  float64      `json:"target"`
	Floor   int          `json:"floor"`
	Suspect []suspectRow `json:"suspect"`
	Dogs    []dogRow     `json:"dogs"`
	Set     []parlayRow  `json:"set"`
	AvgConv float64      `json:"avg_conversion"`
	AnyHit  float64      `json:"any_hit"`
	Unfille int          `json:"unfilled"`
	Shop    []shopRow    `json:"shop"`
	Notes   []string     `json:"notes"`
	// Provisional says the set is the best pairing of what is entered, which
	// is not the best pairing of the week. A board is partly filled nearly
	// always, so this is the normal case and the UI has to show it rather
	// than leave it to be inferred from a count.
	Provisional bool `json:"provisional"`
	// Committed is what the log shows already wagered, so the UI can say why a
	// team it can see priced never appears in a set.
	Committed   []commitJSON `json:"committed"`
	Missing     int          `json:"missing"`
	PricedBooks []string     `json:"priced_books"`

	// The bankroll half. Books is what was pooled; Funds what each holds;
	// Alloc what the set asks of each; Frontier the ways it could be split.
	Books     []string           `json:"books"`
	Funds     map[string]float64 `json:"funds"`
	Alloc     []allocJSON        `json:"alloc"`
	Frontier  []frontJSON        `json:"frontier"`
	Advice    []string           `json:"advice"`
	FreeSplit bool               `json:"free_split"`
	// Boosts matched to tickets, and what applying each is worth. Prop-only
	// tokens are absent by design: the board has no prop prices to match them
	// against.
	Boosts     []boostPick `json:"boosts"`
	BoostAdds  float64     `json:"boost_adds"`
	PropBoosts int         `json:"prop_boosts"`
	// CashBoosts is boosts held that require a real-money stake while this
	// bankroll is bonus money. Counted and reported rather than filtered
	// silently: a token quietly dropped is a token still believed in, and four
	// of these were carried as $19.03 apiece until someone tested one.
	CashBoosts int `json:"cash_boosts"`
}

type allocJSON struct {
	Book     string  `json:"book"`
	Tickets  int     `json:"tickets"`
	Funds    float64 `json:"funds"`
	Stake    float64 `json:"stake"`
	Unfunded bool    `json:"unfunded"`
	Idle     bool    `json:"idle"`
}

type frontJSON struct {
	Shots     int     `json:"shots"`
	Stake     float64 `json:"stake"`
	Conv      float64 `json:"conversion"`
	AnyHit    float64 `json:"any_hit"`
	EV        float64 `json:"ev"`
	Dominated bool    `json:"dominated"`
}

type commitJSON struct {
	Selection string   `json:"selection"`
	Teams     []string `json:"teams"`
}

type suspectRow struct {
	Game      string  `json:"game"`
	Price     string  `json:"price"`
	Overround float64 `json:"overround"`
	Why       string  `json:"why"`
}

type dogRow struct {
	Team       string  `json:"team"`
	Game       string  `json:"game"`
	Price      int     `json:"price"`
	Fair       float64 `json:"fair"`
	Conversion float64 `json:"conversion"`
	Clears     bool    `json:"clears"`
	Suspect    bool    `json:"suspect"`
}

type parlayRow struct {
	Teams      []string `json:"teams"`
	Book       string   `json:"book"`
	Price      int      `json:"price"`
	TrueProb   float64  `json:"true_prob"`
	Conversion float64  `json:"conversion"`
}

type shopRow struct {
	Team string `json:"team"`
	Best int    `json:"best"`
	Book string `json:"book"`
	Cons int    `json:"cons"`
	// Points is the gap to consensus in American points, omitted when the two
	// prices straddle zero -- there the scale is not continuous and the
	// difference would read as a 200-point gap that means nothing.
	Points      int  `json:"points"`
	PointsValid bool `json:"points_valid"`
}

func (s *boardServer) handleReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	week, err := strconv.Atoi(q.Get("week"))
	if err != nil || week <= 0 {
		httpError(w, http.StatusBadRequest, "week must be a positive number")
		return
	}
	books := []string{}
	for _, b := range strings.Split(q.Get("books"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			books = append(books, b)
		}
	}
	if len(books) == 0 {
		if b := q.Get("book"); b != "" {
			books = []string{b}
		} else {
			books = []string{board.Consensus}
		}
	}
	shots := 4
	if v, err := strconv.Atoi(q.Get("shots")); err == nil && v > 0 && v <= 12 {
		shots = v
	}
	lined := q.Get("lined") == "1" || q.Get("lined") == "true"
	obj, err := board.ParseObjective(q.Get("objective"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	wf, err := s.load(week)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}

	// Same default as the CLI: teams carrying an open wager are excluded unless
	// explicitly ignored. The UI has no way to retype a list every time, so a
	// derived exclusion is the only one it can have.
	var committed []Commitment
	var excl []string
	if q.Get("ignore_log") != "1" {
		c, teams, err := PlacedCommitments(s.betlogPath, wf.doc)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		committed, excl = c, teams
	}
	for _, t := range strings.Split(q.Get("exclude"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			excl = append(excl, t)
		}
	}

	// Funds come from the ledger unless the caller overrides. That is the
	// point of connecting them: the balance the report plans against should be
	// the balance the log says is held, not a number typed twice.
	funds, err := s.fundsFor(books)
	if err != nil {
		httpError(w, http.StatusConflict, "the bankroll log does not replay: "+err.Error())
		return
	}
	for _, part := range strings.Split(q.Get("funds"), ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if amt, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && amt >= 0 {
			if funds == nil {
				funds = map[string]float64{}
			}
			funds[strings.TrimSpace(k)] = amt
		}
	}

	a, err := board.Analyze(wf.doc, board.Options{
		Books: books, Target: board.DefaultTarget, Shots: shots, Objective: obj, Lined: lined,
		Exclude: excl, Funds: funds, MinBet: 1,
	})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := reportJSON{
		Week: a.Week, Book: a.Book, Target: a.Target, Floor: int(a.Floor),
		Priced: len(a.Lines), Total: len(a.Lines) + len(a.Missing),
		AvgConv: a.Set.AvgConversion, AnyHit: a.Set.AnyHit, Unfille: a.Set.Unfilled,
		Notes: a.Problems, Provisional: a.Provisional, Missing: len(a.Missing),
		Committed:   toCommitJSON(committed),
		PricedBooks: a.PricedBooks,
		Books:       books,
		Funds:       funds,
	}

	// A suspect line is held out of the parlay pool but still shown, because
	// the operator may know the price is right and the de-vig wrong.
	suspect := map[string]bool{}
	for _, l := range a.Suspect {
		suspect[l.GameID] = true
		out.Suspect = append(out.Suspect, suspectRow{
			Game:      l.Away.Team + " @ " + l.Home.Team,
			Price:     fmt.Sprintf("%+d/%+d", int(l.Market.A), int(l.Market.B)),
			Overround: l.Overround, Why: l.Why,
		})
	}

	for _, d := range a.Dogs {
		out.Dogs = append(out.Dogs, dogRow{
			Team: d.Team, Game: d.GameID, Price: int(d.Price), Fair: d.Fair,
			Conversion: d.Conversion, Clears: d.Clears(a.Target), Suspect: suspect[d.GameID],
		})
	}

	for _, p := range a.Set.Parlays {
		out.Set = append(out.Set, parlayRow{
			Teams: p.Tickets(), Price: int(p.Price),
			TrueProb: p.TrueProb, Conversion: p.Conversion, Book: p.Book(),
		})
	}

	total := 0.0
	for _, v := range funds {
		total += v
	}
	// Boosts join the campaign math whenever they can actually be used. The
	// bankroll here is bonus money, so a cash-only token is correctly excluded
	// rather than counted and later discovered to be worthless.
	if lots, err := s.boostLots(books); err == nil && len(lots) > 0 {
		stakes := map[string]float64{}
		for _, al := range board.Allocate(a.Set, funds) {
			if al.Stake > 0 {
				stakes[al.Book] = al.Stake
			}
		}
		out.Boosts = matchBoosts(a.Set, lots, stakes, false)
		for _, bp := range out.Boosts {
			out.BoostAdds += bp.Adds
		}
		for _, l := range lots {
			switch {
			case l.Boost == nil:
			case l.Boost.PropOnly():
				out.PropBoosts++
			case l.Boost.RequiresCashStake:
				out.CashBoosts++
			}
		}
	}

	if total > 0 {
		for _, al := range board.Allocate(a.Set, funds) {
			out.Alloc = append(out.Alloc, allocJSON{
				Book: al.Book, Tickets: al.Tickets, Funds: al.Funds,
				Stake: al.Stake, Unfunded: al.Unfunded, Idle: al.Idle,
			})
		}
		if f, err := board.Frontier(a.Legs(), total, 1, obj, a.Target); err == nil {
			out.FreeSplit = board.FreeToSplit(f)
			for _, d := range f {
				out.Frontier = append(out.Frontier, frontJSON{
					Shots: d.Shots, Stake: d.Stake, Conv: d.Set.AvgConversion,
					AnyHit: d.Set.AnyHit, EV: d.EV, Dominated: d.Dominated,
				})
			}
			out.Advice = board.Advise(f, shots, total, a.Target,
				time.Time{}, wf.doc.LastKickoff(), time.Now()).Reasons
		}
	}

	for _, sh := range a.Shop {
		out.Shop = append(out.Shop, shopRow{
			Team: sh.Team, Best: int(sh.Best.Price), Book: sh.Best.Book,
			Cons: int(sh.Consensus), Points: sh.Points, PointsValid: sh.PointsValid && sh.HasCons,
		})
	}

	writeJSON(w, out)
}

func toCommitJSON(c []Commitment) []commitJSON {
	out := make([]commitJSON, 0, len(c))
	for _, x := range c {
		out = append(out, commitJSON{Selection: x.Selection, Teams: x.Teams})
	}
	return out
}

// boostPick is a boost matched to one proposed ticket.
type boostPick struct {
	Ticket  int     `json:"ticket"`
	Book    string  `json:"book"`
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
	// Adds is what applying it is worth on THIS ticket at its stake, which is
	// the only figure that means anything. A boost quoted as a flat dollar
	// value states a number that depends on a wager not yet chosen.
	Adds float64 `json:"adds"`
	// Capped marks a ticket staked above the boost's maximum, where the boost
	// applies to part of the stake only.
	Capped bool `json:"capped"`
}

// matchBoosts assigns held boosts to proposed tickets, best first.
//
// Only boosts that can actually be used: the price must clear the minimum
// line, the market must match, and a cash-only boost is excluded outright
// while the bankroll is bonus money. That last rule is the one that matters --
// four boosts were once carried as $19.03 apiece before anyone checked it.
//
// Prop-restricted boosts are held out entirely for now. They are real and
// often the most valuable thing on offer, but the board carries no prop
// prices, so there is nothing here to match them against. Reporting them as
// unusable would be wrong; reporting them as applicable would be worse.
//
// One boost per ticket, greedily from the most valuable. A token is spent
// whole, so two on one wager is not a thing, and the greedy order is exact
// here because every ticket can take any eligible boost.
func matchBoosts(set board.ParlaySet, boosts []ledger.Lot, stakes map[string]float64, cash bool) []boostPick {
	type cand struct {
		lot    ledger.Lot
		adds   float64
		idx    int
		capped bool
	}
	var all []cand
	for i, p := range set.Parlays {
		stake := stakes[p.Book()]
		if stake <= 0 {
			continue
		}
		for _, l := range boosts {
			b := l.Boost
			if b == nil || l.Book != p.Book() || b.PropOnly() {
				continue
			}
			if !b.UsableOn("ml", cash) {
				continue
			}
			if ok, err := b.Allows(p.Price); err != nil || !ok {
				continue
			}
			// The boost applies to at most its own maximum stake.
			eff := stake
			capped := false
			if b.MaxStake > 0 && eff > b.MaxStake {
				eff, capped = b.MaxStake, true
			}
			m, err := p.Price.ProfitMultiple()
			if err != nil {
				continue
			}
			all = append(all, cand{l, b.Percent * eff * m * p.TrueProb, i, capped})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].adds > all[j].adds })

	usedLot, usedTicket := map[string]bool{}, map[int]bool{}
	var out []boostPick
	for _, c := range all {
		if usedLot[c.lot.ID] || usedTicket[c.idx] {
			continue
		}
		usedLot[c.lot.ID], usedTicket[c.idx] = true, true
		label := c.lot.Boost.Label
		if label == "" {
			label = fmt.Sprintf("%.0f%% boost", c.lot.Boost.Percent*100)
		}
		out = append(out, boostPick{
			Ticket: c.idx, Book: c.lot.Book, Label: label,
			Percent: c.lot.Boost.Percent, Adds: c.adds, Capped: c.capped,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ticket < out[j].Ticket })
	return out
}

// boostLots reads the held boosts for the named books.
func (s *boardServer) boostLots(books []string) ([]ledger.Lot, error) {
	events, err := ledger.Load(s.ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	pos, err := ledger.Balances(events, time.Now())
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, b := range books {
		want[b] = true
	}
	var out []ledger.Lot
	for _, l := range pos.Lots {
		if l.Boost != nil && want[l.Book] {
			out = append(out, l)
		}
	}
	return out, nil
}

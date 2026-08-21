package main

import (
	"fmt"
	"net/http"
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

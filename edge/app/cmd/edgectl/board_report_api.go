package main

import (
	"fmt"
	"net/http"
	"strconv"

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
	Provisional bool     `json:"provisional"`
	Missing     int      `json:"missing"`
	PricedBooks []string `json:"priced_books"`
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
	book := q.Get("book")
	if book == "" {
		book = board.Consensus
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

	a, err := board.Analyze(wf.doc, board.Options{
		Book: book, Target: board.DefaultTarget, Shots: shots, Objective: obj, Lined: lined,
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
		PricedBooks: a.PricedBooks,
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
			Teams: p.Teams(), Price: int(p.Price),
			TrueProb: p.TrueProb, Conversion: p.Conversion,
		})
	}

	for _, sh := range a.Shop {
		out.Shop = append(out.Shop, shopRow{
			Team: sh.Team, Best: int(sh.Best.Price), Book: sh.Best.Book,
			Cons: int(sh.Consensus), Points: sh.Points, PointsValid: sh.PointsValid && sh.HasCons,
		})
	}

	writeJSON(w, out)
}

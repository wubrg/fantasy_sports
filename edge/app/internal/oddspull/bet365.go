package oddspull

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"edge/internal/wager"
)

// Book names an Outcome can carry. An Outcome with no Book is DraftKings: that
// was the only source before bet365 captures were supported, and every DK
// extractor leaves the field empty so existing keys and snapshots are unchanged.
const (
	DraftKings = "draftkings"
	Bet365     = "bet365"
)

// BookName is the book an outcome was read from, DraftKings when unset.
func (o Outcome) BookName() string {
	if o.Book == "" {
		return DraftKings
	}
	return o.Book
}

// bet365 serves its market pages as text/xml in its own wire format, not JSON:
// records are separated by '|', fields by ';' and each field is key=value. A
// HAR of a bet365 session therefore looks empty to the JSON extractors. This
// file reads that format. The same two rules as the rest of the package hold:
// it reads a price, it never invents one, and a row it cannot place is skipped
// rather than guessed at.
//
// What a capture holds (verified on a Week 10 2026 capture):
//   - one response per market tab (TD Scorers, Pass Yds, Pass TDs, Receiving
//     Yds, Receptions, Rush Yds, Game Lines), each covering EVERY game on the
//     page, which can include next week's games too;
//   - fractional odds ("10/11"), converted to American here;
//   - player ladders (rungs 20, 30, 40, ...), which this parser reads as "N+"
//     rungs, the same convention as the DraftKings ladders -- bet365 does not
//     label them, so that reading is an assumption worth confirming on screen;
//   - the TD Scorers tab shows only the first handful of players per game.

// bet365Marker is how a bet365 wire-format response starts ("F|CL;ID=12;...").
const bet365Marker = "F|"

// isBet365Text reports whether a response body is bet365's wire format.
func isBet365Text(text string) bool {
	return strings.HasPrefix(text, bet365Marker) && strings.Contains(text, ";")
}

type b365rec map[string]string

func b365records(text string) []b365rec {
	parts := strings.Split(text, "|")
	out := make([]b365rec, 0, len(parts))
	for _, p := range parts {
		fields := strings.Split(p, ";")
		r := b365rec{"_t": fields[0]}
		for _, f := range fields[1:] {
			if k, v, ok := strings.Cut(f, "="); ok {
				r[k] = v
			}
		}
		out = append(out, r)
	}
	return out
}

// b365FracToAmerican converts bet365's fractional odds ("10/11", "17/2") to an
// American price. Anything that is not a positive n/d is rejected.
func b365FracToAmerican(od string) (wager.American, bool) {
	ns, ds, ok := strings.Cut(od, "/")
	if !ok {
		return 0, false
	}
	n, err1 := strconv.ParseFloat(ns, 64)
	d, err2 := strconv.ParseFloat(ds, 64)
	if err1 != nil || err2 != nil || n <= 0 || d <= 0 {
		return 0, false
	}
	dec := 1 + n/d
	var a int
	if dec >= 2 {
		a = int(math.Round((dec - 1) * 100))
	} else {
		a = -int(math.Round(100 / (dec - 1)))
	}
	if abs(a) < 100 {
		return 0, false
	}
	return wager.American(a), true
}

var b365EventRe = regexp.MustCompile(`#E(\d+)#`)

// b365TabNames maps each tab id to every name the page header gives it (the
// "MA;NA=Pass Yds;PD=...#E121155#..." records every response repeats). The
// header lists generic tabs ("Lines", "Props") with the same ids as the
// specific ones ("Game Lines", "TD Scorers"), so one id can carry several names.
func b365TabNames(recs []b365rec) map[string][]string {
	names := map[string][]string{}
	for _, r := range recs {
		if r["_t"] != "MA" || r["NA"] == "" || r["PD"] == "" {
			continue
		}
		if m := b365EventRe.FindStringSubmatch(r["PD"]); m != nil {
			names[m[1]] = append(names[m[1]], r["NA"])
		}
	}
	return names
}

// b365SelectedTab returns the recognised tab this response is the content of:
// the first MG record of type "cmx" carries its id, and the tab is whichever of
// that id's names is one this parser reads. An id with no recognised name gives
// "" and the response is skipped.
func b365SelectedTab(recs []b365rec, names map[string][]string) string {
	for _, r := range recs {
		if r["_t"] == "MG" && r["SY"] == "cmx" && r["ID"] != "" {
			for _, n := range names[r["ID"]] {
				if n == "Game Lines" || n == "TD Scorers" || b365Stat[n] != "" {
					return n
				}
			}
			return ""
		}
	}
	return ""
}

var b365Stat = map[string]string{
	"Pass Yds":      "Passing Yards",
	"Pass TDs":      "Passing Touchdowns",
	"Receiving Yds": "Receiving Yards",
	"Receptions":    "Receptions",
	"Rush Yds":      "Rushing Yards",
}

// extractBet365 reads every bet365 wire-format response and appends its
// outcomes, each stamped Book=bet365. A response whose tab it does not
// recognise is skipped.
func extractBet365(texts []string, out *[]Outcome) {
	for _, text := range texts {
		recs := b365records(text)
		names := b365TabNames(recs)
		tab := b365SelectedTab(recs, names)
		switch {
		case tab == "Game Lines":
			extractBet365Lines(recs, out)
		case tab == "TD Scorers":
			extractBet365TD(recs, out)
		case b365Stat[tab] != "":
			extractBet365Ladder(recs, b365Stat[tab], out)
		}
	}
}

func b365line(f float64) *float64 { return &f }

// extractBet365Ladder reads a player-ladder tab. Per game (an MG of type "sgh"):
// the players are defined once, in order, by "PA" records whose ID starts with
// "PC"; then each rung is a "CO" record (NA = the threshold) followed by one
// odds record per player IN THE SAME ORDER. The odds records' own IDs change
// from rung to rung, so they cannot be joined to a player by ID -- the position
// is the join. An odds record with no price (SU=1, empty OD) holds its place so
// the positions after it stay aligned, and is skipped.
func extractBet365Ladder(recs []b365rec, stat string, out *[]Outcome) {
	type player struct{ name, team string }
	var game string
	var players []player
	var rung string
	var run []b365rec

	flush := func() {
		if game != "" && rung != "" {
			th, err := strconv.ParseFloat(rung, 64)
			if err == nil {
				for i, r := range run {
					if i >= len(players) || r["OD"] == "" || r["SU"] == "1" {
						continue
					}
					price, ok := b365FracToAmerican(r["OD"])
					if !ok {
						continue
					}
					p := players[i]
					*out = append(*out, Outcome{
						Event:     game,
						Market:    p.name + " " + stat,
						Selection: fmt.Sprintf("%s %s+", p.name, rung),
						Line:      b365line(th),
						Price:     price,
						Category:  categorise(stat),
						// One id per rung: a ladder is many-sided, and giving each rung
						// its own id keeps a player who happens to show exactly two rungs
						// from being paired and de-vigged as an Over/Under.
						MarketID: "b365:" + game + ":" + p.name + ":" + stat + ":" + rung,
						Book:     Bet365,
					})
				}
			}
		}
		run = nil
	}

	for _, r := range recs {
		switch r["_t"] {
		case "MG":
			if r["SY"] == "sgh" {
				flush()
				rung, game, players = "", r["NA"], nil
			}
		case "PA":
			switch {
			case strings.HasPrefix(r["ID"], "PC") && r["N2"] != "" && rung == "":
				players = append(players, player{r["NA"], r["N2"]})
			case game != "" && rung != "":
				if _, has := r["OD"]; has {
					run = append(run, r)
				}
			}
		case "CO":
			flush()
			rung = r["NA"]
		}
	}
	flush()
}

// extractBet365TD reads the TD Scorers tab: per game (an MG of type "fd") the
// players are defined once by "PA" records whose ID starts with a bare "P", then
// an "MA" named First or Anytime heads a run of odds records in the same order.
// bet365 lists only the first few players per game here.
func extractBet365TD(recs []b365rec, out *[]Outcome) {
	type player struct{ name, team string }
	var game, col string
	var players []player
	idx := 0
	for _, r := range recs {
		switch r["_t"] {
		case "MG":
			if r["SY"] == "fd" {
				game, col, players, idx = r["NA"], "", nil, 0
			}
		case "MA":
			if r["NA"] == "First" || r["NA"] == "Anytime" {
				col, idx = r["NA"], 0
			}
		case "PA":
			id := r["ID"]
			if game == "" {
				continue
			}
			if col == "" && strings.HasPrefix(id, "P") && !strings.HasPrefix(id, "PC") && r["N2"] != "" {
				players = append(players, player{r["NA"], r["N2"]})
				continue
			}
			if col != "" && r["OD"] != "" {
				if idx < len(players) {
					if price, ok := b365FracToAmerican(r["OD"]); ok {
						market := "Anytime TD Scorer"
						if col == "First" {
							market = "First TD Scorer"
						}
						*out = append(*out, Outcome{
							Event: game, Market: market, Selection: players[idx].name,
							Price: price, Category: categorise(market),
							MarketID: "b365:" + game + ":" + market + ":" + players[idx].name,
							Book:     Bet365,
						})
					}
				}
				idx++
			}
		}
	}
}

// extractBet365Lines reads the Game Lines tab. The games are defined once, in
// order, by "PA" records carrying FD ("AWAY @ HOME"); then Spread, Total and
// Money blocks each hold two odds records per game, tied to the game by PZ (its
// index). The first record of a pair is the away side and the second the home
// side -- checked on a real capture, where the moneyline favourite always
// carries the minus spread. Totals carry "O 41.5" / "U 41.5".
func extractBet365Lines(recs []b365rec, out *[]Outcome) {
	type game struct{ away, home string }
	var games []game
	for _, r := range recs {
		if r["_t"] == "PA" && strings.HasPrefix(r["ID"], "PC") && r["FD"] != "" {
			games = append(games, game{r["NA"], r["N2"]})
		}
	}

	type side struct {
		hd    string
		price wager.American
	}
	byGame := map[string]map[int][]side{"Spread": {}, "Total": {}, "Money": {}}
	cur := ""
	for _, r := range recs {
		switch r["_t"] {
		case "MA":
			if _, ok := byGame[r["NA"]]; ok {
				cur = r["NA"]
			}
		case "PA":
			if cur == "" || r["OD"] == "" || r["PZ"] == "" || strings.HasPrefix(r["ID"], "PC") {
				continue
			}
			z, err := strconv.Atoi(r["PZ"])
			if err != nil {
				continue
			}
			price, ok := b365FracToAmerican(r["OD"])
			if !ok {
				continue
			}
			byGame[cur][z] = append(byGame[cur][z], side{r["HD"], price})
		}
	}

	for z, g := range games {
		event := g.away + " @ " + g.home
		if s := byGame["Money"][z]; len(s) == 2 {
			id := "b365:" + event + ":Moneyline"
			*out = append(*out,
				Outcome{Event: event, Market: "Moneyline", Selection: g.away, Price: s[0].price, Category: "Game", MarketID: id, Book: Bet365},
				Outcome{Event: event, Market: "Moneyline", Selection: g.home, Price: s[1].price, Category: "Game", MarketID: id, Book: Bet365})
		}
		if s := byGame["Spread"][z]; len(s) == 2 {
			a, errA := strconv.ParseFloat(strings.TrimSpace(s[0].hd), 64)
			h, errH := strconv.ParseFloat(strings.TrimSpace(s[1].hd), 64)
			if errA == nil && errH == nil && a == -h {
				id := "b365:" + event + ":Spread"
				*out = append(*out,
					Outcome{Event: event, Market: "Spread", Selection: g.away, Line: b365line(a), Price: s[0].price, Category: "Game", MarketID: id, Book: Bet365},
					Outcome{Event: event, Market: "Spread", Selection: g.home, Line: b365line(h), Price: s[1].price, Category: "Game", MarketID: id, Book: Bet365})
			}
		}
		if s := byGame["Total"][z]; len(s) == 2 {
			o, okO := b365TotalLine(s[0].hd, "O")
			u, okU := b365TotalLine(s[1].hd, "U")
			if okO && okU && o == u {
				id := "b365:" + event + ":Total"
				*out = append(*out,
					Outcome{Event: event, Market: "Total", Selection: "Over", Line: b365line(o), Price: s[0].price, Category: "Game", MarketID: id, Book: Bet365},
					Outcome{Event: event, Market: "Total", Selection: "Under", Line: b365line(u), Price: s[1].price, Category: "Game", MarketID: id, Book: Bet365})
			}
		}
	}
}

// b365TotalLine reads "O 41.5" / "U 41.5" given the side's expected prefix.
func b365TotalLine(hd, prefix string) (float64, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(hd), prefix)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	return f, err == nil
}

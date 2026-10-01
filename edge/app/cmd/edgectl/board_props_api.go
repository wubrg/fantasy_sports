package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"edge/internal/oddspull"
	"edge/internal/wager"
)

// captureWeekRe pulls an NFL week out of an ingest capture's filename, e.g.
// "dk_week2_tnf.har" -> 2. It matches week / wk / w immediately followed by the
// number (leading zeros allowed), so a capture named for a week is scoped to it;
// a filename with no such token returns 0 and shows in every week.
var captureWeekRe = regexp.MustCompile(`(?i)(?:week|wk|w)0*(\d+)`)

func captureWeek(name string) int {
	m := captureWeekRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// The props tab reads DraftKings captures the operator drops into an ingest
// folder and prices them. Nothing is uploaded through the browser: the operator
// saves a .har (or the raw JSON) into the folder and hits reload. Newer files
// win, so a fresh capture of the same game updates the price in place.
//
// This is read-only. It never writes a wager or touches the bankroll; it prices
// what the capture says and, like the rest of edgectl, refuses to show a zero
// for a price it could not read.

const defaultBoostPct = 0.30 // the operator's standing 30% profit boost

func defaultIngestDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "ingest"
	}
	return filepath.Join(home, "adori", "Edge", "ingest")
}

type propRow struct {
	Event     string   `json:"event,omitempty"`
	Market    string   `json:"market"`
	Selection string   `json:"selection"`
	Line      *float64 `json:"line,omitempty"`
	Price     int      `json:"price"`
	Implied   float64  `json:"implied"`            // raw implied, vig included
	BoostBE   *float64 `json:"boost_be,omitempty"` // cash-lens boosted breakeven (one-sided)
	Fair      *float64 `json:"fair,omitempty"`     // de-vigged fair prob (two-sided game lines)
}

type propGroup struct {
	Category string    `json:"category"`
	Rows     []propRow `json:"rows"`
}

// ingestResult is the merged view of every odds capture in the ingest folder,
// scoped to one week. handleProps and the props-sync endpoints both start
// from it, so a capture is read and merged exactly one way everywhere it is
// used.
type ingestResult struct {
	outcomes []oddspull.Outcome // newest price per (event, market, selection, line), in first-seen order
	sources  []map[string]string
	newest   time.Time
	nFiles   int // files found in the folder, whether or not any parsed
}

// readIngest reads every .har/.json in dir, oldest first so a newer capture
// of the same market overwrites the older one, and merges them into one set
// of outcomes. An optional week (0 means "no filter") scopes it to captures
// named for that week; a capture with no week token in its name is read
// regardless.
func readIngest(dir string, week int) ingestResult {
	type fileAt struct {
		path string
		mod  time.Time
	}
	var files []fileAt
	for _, pat := range []string{"*.har", "*.json"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, p := range matches {
			if st, err := os.Stat(p); err == nil {
				files = append(files, fileAt{p, st.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })

	merged := map[string]oddspull.Outcome{}
	var order []string
	var res ingestResult
	res.nFiles = len(files)
	for _, f := range files {
		if week > 0 {
			if cw := captureWeek(filepath.Base(f.path)); cw != 0 && cw != week {
				continue // a capture named for a different week
			}
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		outs, err := oddspull.Parse(data)
		if err != nil {
			continue // a junk / odds-less file is skipped, noted below if nothing parses
		}
		res.sources = append(res.sources, map[string]string{
			"name": filepath.Base(f.path), "at": f.mod.Format("2006-01-02 15:04")})
		if f.mod.After(res.newest) {
			res.newest = f.mod
		}
		for _, o := range outs {
			k := outcomeKey(o)
			if _, seen := merged[k]; !seen {
				order = append(order, k)
			}
			merged[k] = o
		}
	}
	res.outcomes = make([]oddspull.Outcome, 0, len(order))
	for _, k := range order {
		res.outcomes = append(res.outcomes, merged[k])
	}
	return res
}

func (s *boardServer) handleProps(w http.ResponseWriter, r *http.Request) {
	if s.ingestDir == "" {
		writeJSON(w, map[string]any{"dir": "", "groups": []propGroup{},
			"note": "no ingest folder configured (-ingest-dir)"})
		return
	}

	// An optional ?week scopes the tab to captures named for that week; a capture
	// with no week token in its name is shown regardless.
	week, _ := strconv.Atoi(r.URL.Query().Get("week"))

	ing := readIngest(s.ingestDir, week)
	order := make([]string, 0, len(ing.outcomes))
	merged := make(map[string]oddspull.Outcome, len(ing.outcomes))
	for _, o := range ing.outcomes {
		k := outcomeKey(o)
		order = append(order, k)
		merged[k] = o
	}

	// De-vig two-sided game lines: group Game outcomes by (event, marketID) and
	// pair them. A market with exactly two sides gets a fair prob per side.
	//
	// Only Game outcomes are passed in. A two-sided PROP (an Over/Under
	// yardage line) could be de-vigged the same way, but the props tab has
	// always shown props on the boosted-breakeven path, and `props-report` is
	// where the prop de-vig lives -- the tab's behaviour is deliberately left
	// as it was. See devigPairs for the pairing rule itself.
	games := make([]oddspull.Outcome, 0, len(order))
	for _, k := range order {
		if merged[k].Category == "Game" {
			games = append(games, merged[k])
		}
	}
	fair := devigPairs(games)

	byCat := map[string][]propRow{}
	for _, k := range order {
		o := merged[k]
		implied, err := o.Price.ImpliedRaw()
		if err != nil {
			continue
		}
		row := propRow{Event: o.Event, Market: o.Market, Selection: o.Selection,
			Line: o.Line, Price: int(o.Price), Implied: implied}
		if d, ok := fair[k]; ok {
			f := d.Fair
			row.Fair = &f
		} else if be, err := wager.BoostedBreakeven(o.Price, defaultBoostPct); err == nil {
			row.BoostBE = &be
		}
		byCat[o.Category] = append(byCat[o.Category], row)
	}

	var groups []propGroup
	for _, c := range []string{"Passing", "Rushing", "Receiving", "Touchdown", "Game", "Other"} {
		if rows := byCat[c]; len(rows) > 0 {
			groups = append(groups, propGroup{Category: c, Rows: rows})
		}
	}

	note := ""
	if ing.nFiles > 0 && len(ing.sources) == 0 {
		note = "found files in the folder but none contained odds — re-check the capture"
	}
	asOf := ""
	if !ing.newest.IsZero() {
		asOf = ing.newest.Format("2006-01-02 15:04")
	}
	writeJSON(w, map[string]any{
		"dir": s.ingestDir, "week": week, "groups": groups, "sources": ing.sources,
		"boost_pct": defaultBoostPct, "as_of": asOf, "note": note,
	})
}

// devigged is one side of a two-sided market with the vig removed: the fair
// probability this side is worth, plus the book's margin on the market it
// belongs to. Hold and Overround are the WHOLE market's, so both sides of a
// pair carry the same two numbers.
type devigged struct {
	Fair      float64 // de-vigged probability for this side
	Hold      float64 // book's margin on the market (both sides share it)
	Overround float64 // how far the two raw implied probs exceed 1
}

// devigPairs groups outcomes by (event, marketID) and, for every market that
// has EXACTLY two sides, removes the vig and returns the de-vigged figures
// keyed by outcomeKey.
//
// Two sides is the whole rule, and it is doing real work on a props capture.
// A straight Over/Under -- "Receiving Yards O/U 49.5", an Over and an Under
// sharing one marketId -- pairs and de-vigs. An alternate-line ladder --
// "Receiving Yards" with eleven rungs at 15+, 25+, ... all sharing a single
// marketId -- does NOT: it has eleven sides, not two, and there is no opposing
// price to measure the vig against, so it is left for the caller to price as
// one-sided. A lone outcome is skipped for the same reason. This is why the
// helper pairs by marketId rather than by line: the book models a yardage
// Over/Under as one two-sided market, and a rung ladder as one many-sided one.
//
// Both callers share this so the arithmetic and the two-sided rule live in one
// place; each caller decides which outcomes to hand in (handleProps passes only
// Game lines, props-report passes every outcome). Hold and Overround go unused
// by handleProps and are there for props-report, which reports the book's
// margin and flags an implausible overround the way the game-line board does.
func devigPairs(outcomes []oddspull.Outcome) map[string]devigged {
	sides := map[string][]oddspull.Outcome{}
	var order []string
	for _, o := range outcomes {
		k := o.Event + "|" + o.MarketID
		if _, seen := sides[k]; !seen {
			order = append(order, k)
		}
		sides[k] = append(sides[k], o)
	}
	out := map[string]devigged{}
	for _, k := range order {
		pair := sides[k]
		if len(pair) != 2 {
			continue
		}
		m := wager.Market{A: pair[0].Price, B: pair[1].Price}
		pa, pb, err := m.FairDevig()
		if err != nil {
			continue
		}
		hold, err := m.Hold()
		if err != nil {
			continue
		}
		over, err := m.Overround()
		if err != nil {
			continue
		}
		out[outcomeKey(pair[0])] = devigged{Fair: pa, Hold: hold, Overround: over}
		out[outcomeKey(pair[1])] = devigged{Fair: pb, Hold: hold, Overround: over}
	}
	return out
}

func outcomeKey(o oddspull.Outcome) string {
	line := ""
	if o.Line != nil {
		line = strconv.FormatFloat(*o.Line, 'f', -1, 64)
	}
	return o.Event + "|" + o.Market + "|" + o.Selection + "|" + line
}

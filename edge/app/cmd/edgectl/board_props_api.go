package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"edge/internal/oddspull"
	"edge/internal/wager"
)

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
	Implied   float64  `json:"implied"`             // raw implied, vig included
	BoostBE   *float64 `json:"boost_be,omitempty"`  // cash-lens boosted breakeven (one-sided)
	Fair      *float64 `json:"fair,omitempty"`      // de-vigged fair prob (two-sided game lines)
}

type propGroup struct {
	Category string    `json:"category"`
	Rows     []propRow `json:"rows"`
}

func (s *boardServer) handleProps(w http.ResponseWriter, r *http.Request) {
	if s.ingestDir == "" {
		writeJSON(w, map[string]any{"dir": "", "groups": []propGroup{},
			"note": "no ingest folder configured (-ingest-dir)"})
		return
	}

	// Oldest first, so a newer capture of the same market overwrites the older
	// one and the tab shows the latest price.
	type fileAt struct {
		path string
		mod  time.Time
	}
	var files []fileAt
	for _, pat := range []string{"*.har", "*.json"} {
		matches, _ := filepath.Glob(filepath.Join(s.ingestDir, pat))
		for _, p := range matches {
			if st, err := os.Stat(p); err == nil {
				files = append(files, fileAt{p, st.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })

	merged := map[string]oddspull.Outcome{}
	var order []string
	var sources []map[string]string
	var newest time.Time
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		outs, err := oddspull.Parse(data)
		if err != nil {
			continue // a junk / odds-less file is skipped, noted below if nothing parses
		}
		sources = append(sources, map[string]string{
			"name": filepath.Base(f.path), "at": f.mod.Format("2006-01-02 15:04")})
		if f.mod.After(newest) {
			newest = f.mod
		}
		for _, o := range outs {
			k := outcomeKey(o)
			if _, seen := merged[k]; !seen {
				order = append(order, k)
			}
			merged[k] = o
		}
	}

	// De-vig two-sided game lines: group Game outcomes by (event, marketID) and
	// pair them. A market with exactly two sides gets a fair prob per side.
	fair := map[string]float64{}
	sides := map[string][]oddspull.Outcome{}
	for _, k := range order {
		if o := merged[k]; o.Category == "Game" {
			sides[o.Event+"|"+o.MarketID] = append(sides[o.Event+"|"+o.MarketID], o)
		}
	}
	for _, pair := range sides {
		if len(pair) != 2 {
			continue
		}
		if pa, pb, err := (wager.Market{A: pair[0].Price, B: pair[1].Price}).FairDevig(); err == nil {
			fair[outcomeKey(pair[0])] = pa
			fair[outcomeKey(pair[1])] = pb
		}
	}

	byCat := map[string][]propRow{}
	for _, k := range order {
		o := merged[k]
		implied, err := o.Price.ImpliedRaw()
		if err != nil {
			continue
		}
		row := propRow{Event: o.Event, Market: o.Market, Selection: o.Selection,
			Line: o.Line, Price: int(o.Price), Implied: implied}
		if f, ok := fair[k]; ok {
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
	if len(files) > 0 && len(sources) == 0 {
		note = "found files in the folder but none contained odds — re-check the capture"
	}
	asOf := ""
	if !newest.IsZero() {
		asOf = newest.Format("2006-01-02 15:04")
	}
	writeJSON(w, map[string]any{
		"dir": s.ingestDir, "groups": groups, "sources": sources,
		"boost_pct": defaultBoostPct, "as_of": asOf, "note": note,
	})
}

func outcomeKey(o oddspull.Outcome) string {
	line := ""
	if o.Line != nil {
		line = strconv.FormatFloat(*o.Line, 'f', -1, 64)
	}
	return o.Event + "|" + o.Market + "|" + o.Selection + "|" + line
}

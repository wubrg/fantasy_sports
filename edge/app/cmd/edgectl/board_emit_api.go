package main

import (
	"net/http"
	"strconv"

	"edge/internal/board"
)

// handleEmitMarket is the browser face of `emit market`: it renders the MARKET
// block and its DATA PROVENANCE footer for one week and hands them back as
// JSON so the Market tab can show them in a <pre> and copy them whole.
//
// It calls the very same plain functions the CLI wrapper calls -- gameLineRows,
// propsRows, renderMarketBlock, renderProvenance -- so the HTTP path and the
// terminal path produce byte-for-byte identical output. There is no second copy
// of the assembly here; this handler only adapts query params in and JSON out.
//
// Game-line markets come from the git-tracked board (moneyline only, see
// gameLineRows); prop markets come from the ingest capture, minus the capture's
// own Game-category lines, which duplicate the board's tracked source of truth.
// This mirrors emitMarket exactly.
func (s *boardServer) handleEmitMarket(w http.ResponseWriter, r *http.Request) {
	week, err := strconv.Atoi(r.URL.Query().Get("week"))
	if err != nil || week <= 0 {
		httpError(w, http.StatusBadRequest, "week must be a positive number")
		return
	}
	game := r.URL.Query().Get("game")
	book := r.URL.Query().Get("book")
	if book == "" {
		book = board.DefaultBook
	}

	gameRows, err := gameLineRows(s.dir, week, book, game)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.ingestDir == "" {
		httpError(w, http.StatusBadRequest, "no ingest folder configured (-ingest-dir); the MARKET block's props come from a capture")
		return
	}
	propRowsAll, ing, err := propsRows(s.ingestDir, week, game)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The capture carries its own Game-category lines (moneyline/spread/total),
	// but those duplicate the board's hand-entered ones, which are the tracked
	// source of truth. Keep only the actual props from the capture -- the same
	// filter emitMarket applies.
	var propRows []marketRow
	for _, pr := range propRowsAll {
		if pr.category != "Game" {
			propRows = append(propRows, pr)
		}
	}

	rows := append(gameRows, propRows...)

	asOf := "(no capture read)"
	if !ing.newest.IsZero() {
		asOf = ing.newest.Format("2006-01-02 15:04")
	}

	writeJSON(w, map[string]any{
		"as_of":        asOf,
		"market_block": renderMarketBlock(rows, asOf),
		"provenance":   renderProvenance(asOf, len(rows), len(propRows)),
		"row_count":    len(rows),
	})
}

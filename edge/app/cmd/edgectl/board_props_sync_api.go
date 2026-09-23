package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"edge/internal/board"
)

// The bets tab reads only the week YAML, so a HAR-derived price the props tab
// shows never fed it -- the board had to be typed by hand for it to count.
// These two endpoints close that gap the same way the paste-import flow
// already does: plan a diff from the ingest folder's captures, show it, and
// only write once it is confirmed.

type propsSyncRequest struct {
	Week int    `json:"week"`
	Book string `json:"book"`
}

// planPropsSync re-reads the ingest folder and plans the sync from scratch --
// it never trusts a client-sent diff. Mirrors planImport's reasoning: the
// folder's newest capture, and the file on disk, may both have changed
// between a preview and its confirmation.
func (s *boardServer) planPropsSync(r *http.Request) (propsSyncRequest, []board.ImportChange, error) {
	var req propsSyncRequest
	if r.Method != http.MethodPost {
		return req, nil, fmt.Errorf("POST required")
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, nil, err
	}
	if req.Book == "" {
		req.Book = board.DefaultBook
	}
	if s.ingestDir == "" {
		return req, nil, fmt.Errorf("no ingest folder configured (-ingest-dir)")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	wf, err := s.load(req.Week)
	if err != nil {
		return req, nil, err
	}

	ing := readIngest(s.ingestDir, req.Week)
	pairs := wf.doc.OddsPairsFromOutcomes(ing.outcomes)
	changes, err := wf.doc.PlanOddsSync(pairs, req.Book)
	return req, changes, err
}

// handlePropsSyncPreview parses the ingest folder's captures and returns the
// diff syncing them into the board would make. Nothing is written.
func (s *boardServer) handlePropsSyncPreview(w http.ResponseWriter, r *http.Request) {
	req, changes, err := s.planPropsSync(r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"changes": changes, "book": req.Book, "week": req.Week})
}

// handlePropsSyncApply re-plans from the ingest folder rather than trusting a
// diff sent back by the browser -- see planPropsSync and handleImportApply's
// comment for why.
func (s *boardServer) handlePropsSyncApply(w http.ResponseWriter, r *http.Request) {
	req, changes, err := s.planPropsSync(r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	wf, err := s.load(req.Week)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := wf.doc.ApplyOddsSync(changes); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.save(wf); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "applied": len(changes), "changes": changes})
}

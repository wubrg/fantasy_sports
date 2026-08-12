package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"leaguehome/internal/draft"
)

// leanEdits are personal reads made from the board while it is running.
//
// A separate map with its own lock, mirroring scratchpad, and for the same
// reason: staticData is shared and immutable by contract, so the thing that
// changes lives beside it rather than inside it. The board reads the two
// together at rebuild time and never writes back.
//
// Keyed the way ParseLeans keys its map — by normalized player name — so an
// edit and a CSV row for the same player collide as they should.
type leanEdits struct {
	mu    sync.Mutex
	edits draft.Leans
}

func newLeanEdits() *leanEdits { return &leanEdits{edits: draft.Leans{}} }

// set records a read, or clears one when lean is empty.
//
// A cleared read is kept in the map as a blank rather than deleted, because
// it has to out-rank the CSV row it is overriding. effectiveLeans turns it
// into a deletion when the two are merged.
func (l *leanEdits) set(name string, lean draft.Lean) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.edits[draft.NormalizeName(name)] = draft.PlayerLean{
		Player: name, Lean: lean, Source: "board",
	}
}

// snapshot copies the edits for a rebuild to read without holding the lock.
func (l *leanEdits) snapshot() draft.Leans {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.edits) == 0 {
		return nil
	}
	out := make(draft.Leans, len(l.edits))
	for k, v := range l.edits {
		out[k] = v
	}
	return out
}

// leanCycle is the order the board steps through on each click.
//
// A cycle rather than a menu: during an auction you have a few seconds, and
// four taps back to where you started beats finding a control. Cap and note
// are left to the CSV — the walk-away for a must-have comes from the risk
// ceiling, so a read set at the board never needs a number typed.
var leanCycle = []draft.Lean{draft.LeanMust, draft.LeanUp, draft.LeanDown, draft.LeanDND, ""}

// nextLean returns the read that follows the current one.
func nextLean(current draft.Lean) draft.Lean {
	for i, l := range leanCycle {
		if l == current {
			return leanCycle[(i+1)%len(leanCycle)]
		}
	}
	return leanCycle[0]
}

// handleLean records a personal read and writes it back to mine.csv.
//
// Same shape as handleSold: lock, mutate, rebuild, unlock, return the board.
// The file write happens outside the lock — it is the slow part, and the
// board is already correct without it.
func (s *server) handleLean(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Player string `json:"player"`
		Lean   string `json:"lean"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.static.playerIDByName(body.Player) == "" {
		http.Error(w, fmt.Sprintf("no player named %q on the board", body.Player), http.StatusBadRequest)
		return
	}

	lean := draft.Lean(body.Lean)
	switch lean {
	case draft.LeanMust, draft.LeanUp, draft.LeanDown, draft.LeanDND, "":
	default:
		http.Error(w, fmt.Sprintf("unknown lean %q (use must, up, down, dnd, or empty)", body.Lean),
			http.StatusBadRequest)
		return
	}

	s.leans.set(body.Player, lean)

	s.mu.Lock()
	err := s.rebuildLocked()
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist after the board is right. A failed write must be visible —
	// silently keeping a read that vanishes on restart is worse than saying
	// so — but it must not cost you the read you just made.
	if err := s.saveLeans(); err != nil {
		http.Error(w, fmt.Sprintf("read applied to the board but not saved: %v", err),
			http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.snapshot())
}

// saveLeans folds the live edits into mine.csv on disk.
//
// Read-modify-write rather than a dump of memory. That file is hand-edited
// and may well be open in your editor; anything added to it since the server
// started would be lost by a blind overwrite, and the whole point of saving
// is that reads survive.
func (s *server) saveLeans() error {
	path := filepath.Join(s.configDir, "leans", "mine.csv")

	onDisk, err := draft.LoadLeans(path)
	if err != nil {
		return err
	}
	merged := make(draft.Leans, len(onDisk))
	for k, v := range onDisk {
		merged[k] = v
	}
	for k, v := range s.leans.snapshot() {
		if v.Lean == "" {
			// Deleting a row that was never in this file changes nothing,
			// and the read comes straight back from whichever set owns it.
			// A none row is how mine.csv says "I have no opinion on him",
			// and it outranks the set that does.
			if _, inherited := s.static.leans[k]; inherited {
				if _, ours := onDisk[k]; !ours {
					merged[k] = draft.PlayerLean{Player: v.Player, Lean: draft.LeanNone}
					continue
				}
			}
			delete(merged, k)
			continue
		}
		// Keep whatever cap and note he came with: the board cannot set
		// them, so it must not erase them either.
		//
		// Falling back to the startup state matters as much as reading the
		// file. Cycling past "none" deletes the row, so by the time the
		// cycle comes back around to a read there is nothing on disk left
		// to preserve — and a $20 hard cap would be quietly dropped by
		// four clicks that end where they began.
		if old, ok := merged[k]; ok {
			v.Cap, v.Note = old.Cap, old.Note
		} else if old, ok := s.static.leans[k]; ok {
			v.Cap, v.Note = old.Cap, old.Note
		}
		merged[k] = v
	}
	return draft.WriteLeans(path, merged)
}

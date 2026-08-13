package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
// edit and a row in the file for the same player collide as they should.
type leanEdits struct {
	mu    sync.Mutex
	edits draft.Leans
}

func newLeanEdits() *leanEdits { return &leanEdits{edits: draft.Leans{}} }

// set records a read, or clears one when lean is empty.
//
// A cleared read is kept in the map as a blank rather than deleted, because
// it has to out-rank the row in the file it is overriding. effectiveLeans turns it
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
// are left to the file — the walk-away for a must-have comes from the risk
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

// handleLean records a personal read and writes it back to your lean set.
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
	// Store the read under the board's spelling, not the caller's. Validation
	// resolves a name through the matcher, so a spelling it accepts must key
	// the same way the board's own reads do — otherwise the request succeeds,
	// the file gains the alternate spelling, and the read reaches nobody.
	if s.static.matcher != nil {
		if canonical, ok := s.static.matcher.Canonical(body.Player); ok {
			body.Player = canonical
		}
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

// saveLeans folds the live edits into your own lean set on disk.
//
// Read-modify-write rather than a dump of memory. That file is hand-edited
// and may well be open in your editor; anything added to it since the server
// started would be lost by a blind overwrite, and the whole point of saving
// is that reads survive.
func (s *server) saveLeans() error {
	// The file startup actually read, not one worked out from the config
	// directory. Two formats are readable and the reader has fallbacks the
	// writer cannot see, so a guessed path can be a real file the board
	// never consults: the click looks saved and the read is gone on restart.
	//
	// Taken under the lock and used after it. A reload replaces both of
	// these while this runs, and reaching into s.static unlocked is a data
	// race on a live map rather than merely a stale value.
	s.mu.Lock()
	path := s.static.minePath
	inherited := s.static.leans
	s.mu.Unlock()

	if path == "" {
		path = draft.LeanSetPath(s.configDir, "mine")
	}

	// Undecided names live only in the file, never in Leans, so they have to
	// be carried across the write or clicking one lean deletes the list.
	onDisk, undecided, err := draft.LoadLeansFile(path)
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
			// A none row is how your own set says "I have no opinion on him",
			// and it outranks the set that does.
			if _, held := inherited[k]; held {
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
		} else if old, ok := inherited[k]; ok {
			v.Cap, v.Note = old.Cap, old.Note
		}
		merged[k] = v
	}
	return draft.WriteLeans(path, merged, undecided)
}

// reloadLeans re-reads the lean sets from disk and rebuilds the board.
//
// Lean sets are otherwise read once, at startup, because nothing else about
// them moves during a draft. But they are edited as research — often in a
// notes vault, from a phone — and a read added there reached the board only
// after a restart, with nothing on screen to say the file and the board had
// diverged.
//
// On demand rather than on a timer, deliberately. There is no second writer
// to race and no reason to rebuild the board every two seconds to discover
// nothing changed.
//
// A file that does not parse leaves the loaded reads exactly as they were
// and returns the error. Losing every conviction you hold because a file was
// caught half-typed would be a worse failure than the one this fixes.
func (s *server) reloadLeans() error {
	// Which sets to read is itself shared state a previous reload may have
	// replaced, so it comes from under the lock. The file reads happen
	// outside it: they are the slow part, and nothing else writes them.
	s.mu.Lock()
	names := append([]string(nil), s.static.leanSets...)
	s.mu.Unlock()

	_, sets, err := loadLeanSets(s.configDir, names)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Matched to the pool the same way startup matched it, or a read
	// spelled reasonably would survive a restart but not a reload.
	s.static.leans = matchAndMerge(sets, s.static.matcher)
	s.static.leanSets = setNames(sets)
	s.static.minePath = writableSetPath(s.configDir, sets)
	s.static.refreshLeanWarnings()
	return s.rebuildLocked()
}

// handleLeanReload re-reads the lean files and returns the rebuilt board.
func (s *server) handleLeanReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if err := s.reloadLeans(); err != nil {
		// The reads you had are still on the board; say what went wrong
		// rather than pretending the reload happened.
		http.Error(w, fmt.Sprintf("leans not reloaded, previous reads kept: %v", err),
			http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.snapshot())
}

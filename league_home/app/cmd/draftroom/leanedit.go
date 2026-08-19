package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"leaguehome/internal/draft"
)

// boardSource names the edits this file makes, so a read set on screen is
// distinguishable from one that came out of a file.
const boardSource = "board"

// boardEdit is one change made from the board or the leans page.
//
// Both fields are pointers because "untouched" and "cleared" are different
// answers and the save path has to tell them apart: an untouched field falls
// back to whatever the file already says, a cleared one overwrites it. The
// board only ever sets a read; the leans page can set either.
type boardEdit struct {
	player   string
	lean     *draft.Lean
	favorite *bool
}

// boardEdits are personal reads made while the server is running, keyed the
// way ParseLeans keys its map — by normalized player name — so an edit and a
// row in the file for the same player collide as they should.
type boardEdits map[string]boardEdit

// leanEdits holds those edits behind a lock.
//
// A separate map with its own lock, mirroring scratchpad, and for the same
// reason: staticData is shared and immutable by contract, so the thing that
// changes lives beside it rather than inside it. The board reads the two
// together at rebuild time; saveLeans is what puts them on disk.
type leanEdits struct {
	mu    sync.Mutex
	edits boardEdits
}

func newLeanEdits() *leanEdits { return &leanEdits{edits: boardEdits{}} }

// set records a read, or clears one when lean is empty.
//
// A cleared read is kept in the map as a blank rather than deleted, because
// it has to out-rank the row in the file it is overriding. effectiveLeans turns it
// into a deletion when the two are merged.
//
// The favorite tag is left alone: it is not a read, and cycling through the
// reads on the board must not drop one.
func (l *leanEdits) set(name string, lean draft.Lean) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := draft.NormalizeName(name)
	e := l.edits[key]
	e.player, e.lean = name, &lean
	l.edits[key] = e
}

// setFavorite tags or untags a player, leaving whatever read he carries.
func (l *leanEdits) setFavorite(name string, favorite bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := draft.NormalizeName(name)
	e := l.edits[key]
	e.player, e.favorite = name, &favorite
	l.edits[key] = e
}

// snapshot copies the edits for a rebuild to read without holding the lock.
func (l *leanEdits) snapshot() boardEdits {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.edits) == 0 {
		return nil
	}
	out := make(boardEdits, len(l.edits))
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
		// Favorite is a pointer so the board, which knows nothing about the
		// tag, can leave it alone by omitting it — while the leans page can
		// send false and mean it. Absent and false are different requests.
		Favorite *bool `json:"favorite"`
		// SetLean distinguishes "no read given" from "clear his read", since
		// the empty string is a legitimate value for Lean. The board always
		// sends a read; the leans page sends only what it changed.
		SetLean *bool `json:"setLean"`
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

	// A request that changes the read by default, so the board's existing
	// {player, lean} body keeps working untouched.
	setLean := body.SetLean == nil || *body.SetLean
	if !setLean && body.Favorite == nil {
		http.Error(w, "nothing to change: send a lean, a favorite, or both", http.StatusBadRequest)
		return
	}

	if setLean {
		lean := draft.Lean(body.Lean)
		switch lean {
		case draft.LeanMust, draft.LeanUp, draft.LeanDown, draft.LeanDND, "":
		default:
			http.Error(w, fmt.Sprintf("unknown lean %q (use must, up, down, dnd, or empty)", body.Lean),
				http.StatusBadRequest)
			return
		}
		s.leans.set(body.Player, lean)
	}
	if body.Favorite != nil {
		s.leans.setFavorite(body.Player, *body.Favorite)
	}

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
	for k, e := range s.leans.snapshot() {
		// What he already carries, from the file if it names him and from the
		// startup state if it does not.
		//
		// Falling back to the startup state matters as much as reading the
		// file. Cycling past "none" deletes the row, so by the time the
		// cycle comes back around to a read there is nothing on disk left
		// to preserve — and a $20 hard cap would be quietly dropped by
		// four clicks that end where they began.
		prior, held := merged[k]
		if !held {
			prior, held = inherited[k]
		}

		if e.lean != nil && *e.lean == "" {
			// A tagged favorite survives losing his read: the tag is not a
			// read, and the file has a place for a name under favorites with
			// no heading of its own.
			if favorite(e, prior) {
				merged[k] = draft.PlayerLean{Player: e.player, Favorite: true, Cap: prior.Cap, Note: prior.Note}
				continue
			}
			// Deleting a row that was never in this file changes nothing,
			// and the read comes straight back from whichever set owns it.
			// A none row is how your own set says "I have no opinion on him",
			// and it outranks the set that does.
			if _, inherited := inherited[k]; inherited {
				if _, ours := onDisk[k]; !ours {
					merged[k] = draft.PlayerLean{Player: e.player, Lean: draft.LeanNone}
					continue
				}
			}
			delete(merged, k)
			continue
		}

		// Keep whatever cap and note he came with: neither the board nor the
		// leans page can set them, so neither must erase them either. The
		// favorite tag is kept the same way unless this edit spoke to it.
		next := prior
		next.Player = e.player
		if e.lean != nil {
			next.Lean, next.Source = *e.lean, boardSource
		} else if !held {
			// Tagged a favorite with no read anywhere. Legal, and WalkAway
			// reads it as no conviction plus the favorite stretch.
			next.Lean = ""
		}
		next.Favorite = favorite(e, prior)
		merged[k] = next
	}
	return draft.WriteLeans(path, merged, undecided)
}

// favorite resolves the tag: what this edit says if it said anything, and
// otherwise what he already carried.
func favorite(e boardEdit, prior draft.PlayerLean) bool {
	if e.favorite != nil {
		return *e.favorite
	}
	return prior.Favorite
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
	s.static.leanSetInfo = sets
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

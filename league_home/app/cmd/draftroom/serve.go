package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"leaguehome/internal/draft"
)

//go:embed static
var staticFS embed.FS

// pollInterval is how often the live draft is checked.
//
// One request, about 130ms. Sleeper asks callers to stay under 1000 calls a
// minute, so at 2s this uses 30 — three percent of the budget. The earlier
// design rebuilt everything on a timer, which cost 115 calls and three
// seconds each time; splitting the immutable history out (see static.go) is
// what makes a tight interval both safe and useful.
const pollInterval = 2 * time.Second

// server holds the immutable draft data and the live picks on top of it.
type server struct {
	static *staticData

	mu sync.Mutex
	// taken is every player off the board, from the live feed or entered
	// by hand, keyed by Sleeper player ID.
	taken map[string]gone
	// manual records hand-entered sales separately so a later poll cannot
	// erase them, and so they can be undone.
	manual map[string]gone
	cached draft.Snapshot
	polled time.Time
	pollEr string

	// scratch is a hypothetical roster, deliberately not part of the live
	// state above. See scratch.go for why the separation is load-bearing.
	scratch *scratchpad
	// baselines are the pinned VOLS scoring baselines, computed once.
	baselineOnce sync.Once
	baselines    map[string]float64
}

func newServer(s *staticData) (*server, error) {
	srv := &server{
		static: s, taken: map[string]gone{}, manual: map[string]gone{},
		scratch: newScratchpad(),
	}
	return srv, srv.rebuild()
}

// rebuild recomputes the board. Caller must hold no lock; this takes it.
func (s *server) rebuild() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rebuildLocked()
}

func (s *server) rebuildLocked() error {
	combined := make(map[string]gone, len(s.taken)+len(s.manual))
	for id, g := range s.taken {
		combined[id] = g
	}
	// Hand-entered sales win: you knew before the API did.
	for id, g := range s.manual {
		combined[id] = g
	}
	snap, err := s.static.Build(combined)
	if err != nil {
		return err
	}
	if s.pollEr != "" {
		snap.Warnings = append(snap.Warnings, s.pollEr)
	}
	s.cached = snap
	return nil
}

// poll reads the live draft and folds any new picks into the board.
func (s *server) poll() {
	picks, err := s.static.Picks()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.polled = time.Now()
	if err != nil {
		// A blip must not blank the board; keep serving the last good one
		// and say so.
		s.pollEr = fmt.Sprintf("live feed unavailable: %v", err)
		return
	}
	s.pollEr = ""

	changed := false
	for _, p := range picks {
		if p.PlayerID == "" {
			continue
		}
		if _, known := s.taken[p.PlayerID]; known {
			continue
		}
		s.taken[p.PlayerID] = gone{
			price: p.Metadata.Dollars(),
			mine:  p.PickedBy != "" && p.PickedBy == s.static.ownerID,
		}
		changed = true
	}
	if changed {
		if err := s.rebuildLocked(); err != nil {
			log.Printf("draftroom: rebuilding after picks: %v", err)
		}
	}
}

func (s *server) snapshot() draft.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cached
}

func (s *server) handleBoard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshot())
}

// handleSold records a purchase by hand. Posting mine=false marks a player
// as bought by someone else: he leaves the board without costing you.
func (s *server) handleSold(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Player string `json:"player"`
		Price  int    `json:"price"`
		Mine   bool   `json:"mine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := s.static.playerIDByName(body.Player)
	if id == "" {
		http.Error(w, fmt.Sprintf("no player named %q on the board", body.Player), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	price := body.Price
	if !body.Mine && price < 0 {
		price = 0
	}
	s.manual[id] = gone{price: price, mine: body.Mine}
	err := s.rebuildLocked()
	s.mu.Unlock()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.snapshot())
}

// handleUndo drops hand-entered sales. Picks that came from the live feed
// are left alone, since they really did happen.
func (s *server) handleUndo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Player string `json:"player"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.mu.Lock()
	if body.Player == "" {
		s.manual = map[string]gone{}
	} else if id := s.static.playerIDByName(body.Player); id != "" {
		delete(s.manual, id)
	}
	err := s.rebuildLocked()
	s.mu.Unlock()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.snapshot())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("draftroom: encoding response: %v", err)
	}
}

// runServe serves the draft board over HTTP.
func runServe(addr, leagueID, configDir, dataDir, ownerID string, baseline draft.Baseline) error {
	log.Printf("loading draft history and sources...")
	static, err := loadStatic(leagueID, configDir, dataDir, ownerID, baseline)
	if err != nil {
		return err
	}
	srv, err := newServer(static)
	if err != nil {
		return err
	}
	for _, warning := range static.warnings {
		log.Printf("note: %s", warning)
	}

	go func() {
		for range time.Tick(pollInterval) {
			srv.poll()
		}
	}()

	content, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(content)))
	mux.HandleFunc("/api/board", srv.handleBoard)
	mux.HandleFunc("/api/sold", srv.handleSold)
	mux.HandleFunc("/api/undo", srv.handleUndo)
	mux.HandleFunc("/api/scratch", srv.handleScratch)
	mux.HandleFunc("/api/scratch/view", srv.handleScratchView)

	snap := srv.snapshot()
	log.Printf("draft board on http://localhost%s  (%d available, $%d pool, polling every %s)",
		addr, len(snap.Players), snap.Dollars, pollInterval)
	return http.ListenAndServe(addr, mux)
}

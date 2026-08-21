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

// idleInterval is how often the draft is checked when it is not running.
//
// The board is a draft-night tool but it can be left mounted all year, and
// a flat two-second poll would be forty-three thousand requests a day at
// Sleeper for a draft that happens once. Outside the draft window there is
// nothing to find, so it asks once a minute — enough to notice the
// commissioner starting without pretending anything is happening.
const idleInterval = 60 * time.Second

// pollInterval is how often the live draft is checked while it is running.
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

	// leans are personal reads made from the board since startup, laid over
	// the sets loaded from disk. Its own lock, like scratch, because
	// staticData is shared and does not change. See leanedit.go.
	leans *leanEdits
	// configDir is where the lean CSVs live, kept so a read set at the
	// board can be written back to the file it came from.
	configDir string

	// keeperScenario is the research keeper scenario the board is showing:
	// "" on the live draft-night board, or "none"/"locks"/"expected" while
	// exploring the pool the league's keepers would leave. Guarded by mu with
	// the rest of the live state, since changing it rebuilds the board.
	keeperScenario string

	// saved is the durable shortlist of sampled teams, persisted to savedPath.
	// Its own lock: it does not touch the board and changes on its own rhythm.
	savedMu   sync.Mutex
	saved     []SavedTeam
	savedPath string
}

func newServer(s *staticData, configDir string) (*server, error) {
	srv := &server{
		static: s, taken: map[string]gone{}, manual: map[string]gone{},
		scratch: newScratchpad(), leans: newLeanEdits(), configDir: configDir,
		savedPath: savedTeamsPath(configDir, s.ownerID),
	}
	saved, err := loadSavedTeams(srv.savedPath)
	if err != nil {
		return nil, err
	}
	srv.saved = saved
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
	snap, err := s.static.Build(combined, s.leans.snapshot(), s.keeperScenario)
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
// pollForever watches the draft, quickly while it runs and sparingly
// otherwise. The cadence is re-decided on every tick, so a draft opening
// while the board sits idle is picked up within the minute and everything
// after it lands in two seconds.
func (s *server) pollForever() {
	for {
		if s.static.Drafting() {
			s.poll()
			time.Sleep(pollInterval)
			continue
		}
		// Still poll once on the slow cadence: picks can be entered before
		// the status flips, and a stale board is worse than a slow one.
		s.poll()
		time.Sleep(idleInterval)
	}
}

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

// keeperScenarios are the research keeper views the board can show. "" is the
// live draft-night board; the rest take a projected keeper set off the pool.
var keeperScenarios = map[string]bool{"": true, "none": true, "locks": true, "expected": true}

// handleKeepers switches the board between the live view and a research keeper
// scenario. It is the whole of "research mode" on the server: the same rebuild
// path as recording a sale, just changing which keepers are assumed off.
func (s *server) handleKeepers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Scenario string `json:"scenario"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !keeperScenarios[body.Scenario] {
		http.Error(w, fmt.Sprintf("unknown keeper scenario %q", body.Scenario), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.keeperScenario = body.Scenario
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
func runServe(addr, leagueID, draftID, configDir, dataDir, ownerID string, baseline draft.Baseline, leanSets []string) error {
	log.Printf("loading draft history and sources...")
	static, err := loadStatic(leagueID, draftID, configDir, dataDir, ownerID, baseline, leanSets)
	if err != nil {
		return err
	}
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	srv, err := newServer(static, cfg)
	if err != nil {
		return err
	}
	for _, warning := range static.warnings {
		log.Printf("note: %s", warning)
	}

	go srv.pollForever()

	content, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(content))
	mux.Handle("/", files)
	// The leans page under its own name rather than leans.html, so the address
	// is one you would type. Deliberately no trailing slash: at /leans a
	// relative "style.css" resolves to /style.css, and behind a tailscale
	// serve --set-path mount /draftroom/leans resolves to /draftroom/style.css,
	// which is the file. A slash would send both looking one level too deep.
	mux.HandleFunc("/leans", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/leans.html"
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/board", srv.handleBoard)
	mux.HandleFunc("/api/sold", srv.handleSold)
	mux.HandleFunc("/api/undo", srv.handleUndo)
	mux.HandleFunc("/api/lean", srv.handleLean)
	mux.HandleFunc("/api/leans/reload", srv.handleLeanReload)
	mux.HandleFunc("/api/leans", srv.handleLeansPage)
	mux.HandleFunc("/api/scratch", srv.handleScratch)
	mux.HandleFunc("/api/scratch/view", srv.handleScratchView)
	mux.HandleFunc("/api/keepers", srv.handleKeepers)
	mux.HandleFunc("/api/teams", srv.handleTeams)
	mux.HandleFunc("/api/teams/saved", srv.handleTeamsSaved)
	mux.HandleFunc("/api/teams/save", srv.handleTeamSave)
	mux.HandleFunc("/api/teams/delete", srv.handleTeamDelete)
	mux.HandleFunc("/api/teams/scratch", srv.handleTeamScratch)

	snap := srv.snapshot()
	cadence := idleInterval
	if static.Drafting() {
		cadence = pollInterval
	}
	log.Printf("draft board on http://localhost%s  (%d available, $%d pool, polling every %s)",
		addr, len(snap.Players), snap.Dollars, cadence)
	return http.ListenAndServe(addr, mux)
}

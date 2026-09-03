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

// nearStartWindow and lateStartGrace bound the stretch either side of the
// scheduled start where an idle board stops idling.
//
// Once a minute is right for the eleven months a draft is not happening and
// wrong for the ten minutes before one: the opening nomination runs on a short
// clock, so a board that takes up to a minute to notice the draft began can
// miss the first player outright. Inside the window it watches at the live
// cadence instead.
//
// The window reaches past the start as well as before it, because the case
// this exists for is a commissioner starting late — the hour comes and goes,
// the status has not flipped, and the board must not have gone back to sleep.
// The grace period ends it so a draft that never happened does not poll at
// draft speed forever.
//
// The cost is bounded and small: ten minutes at three seconds is 400 requests
// against Sleeper's 1000 a minute, and outside the window an idle board's
// volume is exactly what it was.
const (
	nearStartWindow = 10 * time.Minute
	lateStartGrace  = 2 * time.Hour
)

// idleCadence is how long to wait before looking again at a draft that is not
// running: the live interval around its scheduled start, and the idle one
// otherwise. A draft with no start time set has nothing to tighten around.
func idleCadence(startsAt, now time.Time, live time.Duration) time.Duration {
	// Deliberately explicit, and no test can tell the difference. A zero time
	// is year 1, so the subtraction below overflows int64 nanoseconds and
	// saturates near -292 years, which lands outside the grace period and
	// returns the same answer by accident. Correct behaviour resting on
	// integer overflow is not behaviour worth relying on.
	if startsAt.IsZero() {
		return idleInterval
	}
	until := startsAt.Sub(now)
	if until <= nearStartWindow && until > -lateStartGrace {
		return live
	}
	return idleInterval
}

// defaultPollInterval is how often the live draft is read while it is running,
// and the default for -poll.
//
// Three seconds, and the reasoning behind that number changed completely once
// the cache was understood. Sleeper serves the draft object and its picks
// through Cloudflare at s-maxage=30, so a one-second poll re-read the same
// cached bytes thirty times and left the board up to half a minute behind the
// room — the freshness was never bounded by this interval at all, it was
// bounded by the edge. See Client.getFresh.
//
// Those two requests now reach origin every time, which makes the interval
// mean what it says and makes it the only thing keeping the volume sane. At
// three seconds each board spends 40 calls a minute across both endpoints;
// two boards on draft night is 80, eight percent of the 1000 a minute Sleeper
// asks callers to stay under. Faster buys little against a ten-second
// nomination timer and costs origin load on the one night it matters.
//
// The browser polls this server on its own cadence (POLL_MS in
// static/app.js), which is local and free.
const defaultPollInterval = 3 * time.Second

// minPollInterval is the floor -poll will accept. Below this the board buys
// no perceptible speed — the browser is still on its own cadence — and starts
// spending real budget for it, so a typo'd flag is refused rather than
// quietly pointed at Sleeper.
const minPollInterval = 250 * time.Millisecond

// The draft object used to be read only every ten seconds, because status —
// "has the commissioner started or ended the draft" — changes twice in an
// evening and staling it bought back the cost of polling picks twice as fast.
// That trade is off. The same object carries the current nomination, which
// lives inside a ten-second timer, so reading it a tick late means missing most
// nominations outright. It is now read every tick, and the status comes along
// for free rather than the other way round.

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

	// arbCache holds the arbitrage view per budget percentage, because that
	// page costs twenty times what the board does to build — a beam search
	// over every target — and the page polls on the board's cadence. The
	// board changes when a pick lands, perhaps twice a minute; without this
	// the search would run sixty times to produce the same answer.
	//
	// Its own lock, not the board's: the solve takes tens of milliseconds and
	// holding mu through it would stall every poll and sale behind it.
	// Invalidated wholesale in rebuildLocked, which is precisely "the board
	// changed" and so precisely when the answer can differ.
	arbMu    sync.Mutex
	arbCache map[int]ArbitrageView

	// nomination is the player currently up for auction, nil when none is.
	// Live state like taken and manual, written by the poll and guarded by mu.
	nomination *draft.Nomination

	// saved is the durable shortlist of sampled teams, persisted to savedPath.
	// Its own lock: it does not touch the board and changes on its own rhythm.
	savedMu   sync.Mutex
	saved     []SavedTeam
	savedPath string
}

// rehearsalTag is the draft id when the board is following a mock, and empty
// otherwise -- so a rehearsal keeps its own shortlist and the live board keeps
// the file it already has.
func rehearsalTag(s *staticData) string {
	if s.offLeagueDraft {
		return s.draftID
	}
	return ""
}

func newServer(s *staticData, configDir, keeperScenario string) (*server, error) {
	srv := &server{
		static: s, taken: map[string]gone{}, manual: map[string]gone{},
		scratch: newScratchpad(), leans: newLeanEdits(), configDir: configDir,
		savedPath:      savedTeamsPath(configDir, s.ownerID, rehearsalTag(s)),
		keeperScenario: keeperScenario,
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
	// Live state, layered on after the pure computation — and dropped if the
	// player has since been sold, so the banner cannot outlive the bidding by
	// the length of one poll.
	if s.nomination != nil {
		if _, sold := combined[s.nomination.PlayerID]; !sold {
			snap.Nomination = s.nomination
		}
	}
	s.cached = snap
	s.invalidateArbitrage()
	return nil
}

// invalidateArbitrage drops the cached views. Called from rebuildLocked, which
// runs on every change to the board the page is built from.
func (s *server) invalidateArbitrage() {
	s.arbMu.Lock()
	s.arbCache = nil
	s.arbMu.Unlock()
}

// poll reads the live draft and folds any new picks into the board.
// pollForever watches the draft, quickly while it runs and sparingly
// otherwise. The cadence is re-decided from the draft's status, so a draft
// opening while the board sits idle is picked up within the minute and
// everything after it lands within one interval.
//
// Two requests a tick, and exactly two: the draft object, which says both
// whether the draft is live and who is up for auction, and the picks. An idle
// board makes the same two calls a minute apart rather than a second apart.
func (s *server) pollForever(every time.Duration) {
	for {
		drafting, nom, startsAt := s.static.DraftState()
		s.setNomination(nom)
		// Poll on both cadences: picks can be entered before the status
		// flips, and a stale board is worse than a slow one.
		s.poll()
		if drafting {
			time.Sleep(every)
			continue
		}
		time.Sleep(idleCadence(startsAt, time.Now(), every))
	}
}

// setNomination records who is up, and repaints only when that changed.
//
// Guarded with the rest of the live state because the poll writes it while
// requests read it. Rebuilding unconditionally would re-solve the whole board
// every second for a field that changes every ten.
func (s *server) setNomination(nom *draft.Nomination) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if samePlayer(s.nomination, nom) {
		return
	}
	s.nomination = nom
	if err := s.rebuildLocked(); err != nil {
		log.Printf("draftroom: rebuilding after a nomination: %v", err)
	}
}

// samePlayer compares two nominations by who is up, which is all the board
// draws. Two nominations of the same player are the same banner.
func samePlayer(a, b *draft.Nomination) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.PlayerID == b.PlayerID
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
			mine:  s.static.isMine(p),
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

// validKeeperScenario reports whether name is one the board can open on. An
// unrecognised name reaching Build is silently draft night, so a typo would
// look like a working board with the wrong pool; this is what lets the caller
// refuse it where it was typed.
func validKeeperScenario(name string) bool { return keeperScenarios[name] }

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
func runServe(addr, leagueID, draftID, configDir, dataDir, ownerID string, baseline draft.Baseline, leanSets []string, keeperScenario string, pollEvery time.Duration) error {
	if !validKeeperScenario(keeperScenario) {
		return fmt.Errorf("unknown keeper scenario %q: use none, locks, expected, or leave it empty for draft night", keeperScenario)
	}
	if pollEvery < minPollInterval {
		return fmt.Errorf("poll interval %s is below the %s floor: the browser polls on its own cadence too, so a shorter one buys no speed and spends Sleeper's budget for it", pollEvery, minPollInterval)
	}
	log.Printf("loading draft history and sources...")
	static, err := loadStatic(leagueID, draftID, configDir, dataDir, ownerID, baseline, leanSets)
	if err != nil {
		return err
	}
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	srv, err := newServer(static, cfg, keeperScenario)
	if err != nil {
		return err
	}
	for _, warning := range static.warnings {
		log.Printf("note: %s", warning)
	}

	go srv.pollForever(pollEvery)

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
	// Same rewrite as /leans above, and for the same reason — no trailing
	// slash, so relative asset paths survive the tailscale --set-path mount.
	mux.HandleFunc("/arbitrage", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/arbitrage.html"
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/arbitrage", srv.handleArbitrage)
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
		cadence = pollEvery
	}
	log.Printf("draft board on http://localhost%s  (%d available, $%d pool, polling every %s)",
		addr, len(snap.Players), snap.Dollars, cadence)
	return http.ListenAndServe(addr, mux)
}

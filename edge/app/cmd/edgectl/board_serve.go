package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"edge/internal/board"
)

//go:embed static
var staticFS embed.FS

// boardServe puts the week files behind a phone-shaped web form.
//
// Typing a board by hand is the bottleneck the whole board package exists to
// remove, and it happens on a phone with the sportsbook one app-switch away.
// So the server is deliberately dumb: it holds no session, has no submit
// button, and writes each field the moment it loses focus. Losing ten minutes
// of typing to a backgrounded tab is the failure mode being designed against,
// and the only reliable defence is to never be holding unsaved work.
func boardServe(args []string) error {
	// Named flags rather than fs, which would shadow the io/fs import.
	flags := flag.NewFlagSet("board serve", flag.ExitOnError)
	addr := flags.String("addr", ":8085", "listen address")
	dir := flags.String("dir", defaultBoardDir, "directory holding the week files")
	if err := flags.Parse(args); err != nil {
		return err
	}

	srv, err := newBoardServer(*dir)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		return err
	}

	weeks := srv.weekNumbers()
	log.Printf("line board on http://localhost%s  (%d week files in %s)", *addr, len(weeks), *dir)
	return http.ListenAndServe(*addr, mux)
}

// boardServer owns the week files on disk.
//
// Documents are cached, but every read re-stats the file first. The board is
// also a text file the operator hand-edits, and serving a stale in-memory copy
// would mean the next field save silently reverted those edits -- the cache
// has to be a cache, not a second source of truth.
type boardServer struct {
	dir string
	// betlogPath is the prediction log. It sits beside the board rather than
	// inside it: a board cell holds the latest price, while a log entry holds
	// what was true when a wager was placed, and the two must never be the
	// same storage.
	betlogPath string
	// ledgerPath is the bankroll. Separate from the betlog because they answer
	// different questions -- what was predicted, and what is held -- and
	// joining them into one file would make either hard to read.
	ledgerPath string

	// beliefsDir holds the belief-probe packs and log: weekNN.input.json (and
	// its pasteable weekNN.prompt.md, produced by `make belief-pack`) and
	// log.jsonl. A sibling of the board dir, read by the beliefs view.
	beliefsDir string

	mu   sync.Mutex
	docs map[int]*weekFile
}

type weekFile struct {
	path string
	doc  *board.Doc
	// mod and size are the file's identity at the moment it was read. A
	// changed pair means someone else wrote it and the cache is void.
	mod  time.Time
	size int64
}

func newBoardServer(dir string) (*boardServer, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "week*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no week files in %s -- run `edgectl board scaffold` first", dir)
	}
	return &boardServer{
		dir: dir, betlogPath: defaultBetlog(), ledgerPath: defaultLedger(),
		beliefsDir: filepath.Join(filepath.Dir(dir), "beliefs"),
		docs:       map[int]*weekFile{},
	}, nil
}

func (s *boardServer) routes(mux *http.ServeMux) error {
	content, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(content)))
	mux.HandleFunc("/api/board", s.handleBoard)
	mux.HandleFunc("/api/report", s.handleReport)
	mux.HandleFunc("/api/log", s.handleLog)
	mux.HandleFunc("/api/place", s.handlePlace)
	mux.HandleFunc("/api/settle", s.handleSettle)
	mux.HandleFunc("/api/funds", s.handleFunds)
	mux.HandleFunc("/api/funds/adjust", s.handleAdjust)
	mux.HandleFunc("/api/boosts", s.handleBoosts)
	mux.HandleFunc("/api/price", s.handlePrice)
	mux.HandleFunc("/api/import/preview", s.handleImportPreview)
	mux.HandleFunc("/api/import/apply", s.handleImportApply)
	mux.HandleFunc("/api/beliefs/pack", s.handleBeliefsPack)
	mux.HandleFunc("/api/beliefs/ingest", s.handleBeliefsIngest)
	mux.HandleFunc("/api/beliefs/score", s.handleBeliefsScore)
	return nil
}

func (s *boardServer) path(week int) string {
	return filepath.Join(s.dir, fmt.Sprintf("week%02d.yaml", week))
}

// weekNumbers lists the weeks that have a file, newest scan each call so a
// freshly scaffolded week appears without a restart.
func (s *boardServer) weekNumbers() []int {
	paths, err := filepath.Glob(filepath.Join(s.dir, "week*.yaml"))
	if err != nil {
		return nil
	}
	var out []int
	for _, p := range paths {
		var w int
		if _, err := fmt.Sscanf(filepath.Base(p), "week%d.yaml", &w); err == nil && w > 0 {
			out = append(out, w)
		}
	}
	sort.Ints(out)
	return out
}

// load returns the week's document, re-reading it if the file changed on disk.
// Caller must hold s.mu.
func (s *boardServer) load(week int) (*weekFile, error) {
	path := s.path(week)
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("week %d: %w", week, err)
	}
	if wf, ok := s.docs[week]; ok && wf.mod.Equal(st.ModTime()) && wf.size == st.Size() {
		return wf, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := board.Parse(f)
	if err != nil {
		return nil, err
	}
	wf := &weekFile{path: path, doc: doc, mod: st.ModTime(), size: st.Size()}
	s.docs[week] = wf
	return wf, nil
}

// save writes the document and re-stamps the cache entry.
//
// The stat has to be taken after the write, not before: if the cache kept the
// pre-write identity, the very next read would see a changed file, discard the
// document it just wrote from, and re-parse for no reason.
func (s *boardServer) save(wf *weekFile) error {
	if err := writeDoc(wf.path, wf.doc); err != nil {
		return err
	}
	st, err := os.Stat(wf.path)
	if err != nil {
		return err
	}
	wf.mod, wf.size = st.ModTime(), st.Size()
	return nil
}

// ---- JSON shapes -------------------------------------------------------

type linesJSON struct {
	ML     string `json:"ml"`
	Spread string `json:"spread"`
	Total  string `json:"total"`
}

type gameJSON struct {
	ID      string               `json:"id"`
	Away    string               `json:"away"`
	Home    string               `json:"home"`
	Kickoff string               `json:"kickoff"`
	Books   map[string]linesJSON `json:"books"`
}

type boardJSON struct {
	Season int        `json:"season"`
	Week   int        `json:"week"`
	Weeks  []int      `json:"weeks"`
	Books  []string   `json:"books"`
	Games  []gameJSON `json:"games"`
}

func (s *boardServer) handleBoard(w http.ResponseWriter, r *http.Request) {
	week, err := strconv.Atoi(r.URL.Query().Get("week"))
	if err != nil || week <= 0 {
		httpError(w, http.StatusBadRequest, "week must be a positive number")
		return
	}

	// The whole response is built under the lock: handing the document out and
	// reading it afterwards would race a field save writing into the same maps.
	s.mu.Lock()
	defer s.mu.Unlock()

	wf, err := s.load(week)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}

	out := boardJSON{
		Season: wf.doc.Season, Week: wf.doc.Week,
		Weeks: s.weekNumbers(), Books: board.Books,
	}
	// Kickoff order, because that is the order the games are talked about and
	// the order a book's own page lists them in.
	for _, id := range wf.doc.GameIDs() {
		g := wf.doc.Games[id]
		gj := gameJSON{ID: id, Away: g.Away, Home: g.Home, Kickoff: g.Kickoff,
			Books: map[string]linesJSON{}}
		for bk, l := range g.Books {
			gj.Books[bk] = linesJSON{ML: l.ML, Spread: l.Spread, Total: l.Total}
		}
		out.Games = append(out.Games, gj)
	}
	writeJSON(w, out)
}

type priceRequest struct {
	Week   int    `json:"week"`
	GameID string `json:"game_id"`
	Book   string `json:"book"`
	Market string `json:"market"`
	Value  string `json:"value"`
}

// handlePrice stores one cell.
//
// The value is validated before anything is written, and a rejection comes
// back as a 400 naming what was wrong so the field can keep the text and show
// the reason. A rejected value that vanished from the box would be worse than
// no validation at all: the operator would not know what they had lost.
func (s *boardServer) handlePrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req priceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if err := wf.doc.SetPrice(req.GameID, req.Book, req.Market, req.Value); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.save(wf); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	l := wf.doc.Games[req.GameID].Books[req.Book]
	stored := map[string]string{"ml": l.ML, "spread": l.Spread, "total": l.Total}[req.Market]
	writeJSON(w, map[string]any{"ok": true, "value": stored})
}

type importRequest struct {
	Week   int    `json:"week"`
	Book   string `json:"book"`
	Market string `json:"market"`
	Blob   string `json:"blob"`
}

// handleImportPreview parses a pasted blob and returns the diff it would make.
// Nothing is written; see importer.go for why the confirmation step is not
// optional.
func (s *boardServer) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	req, changes, err := s.planImport(r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"changes": changes, "book": req.Book, "week": req.Week})
}

// handleImportApply re-parses and re-plans from the posted blob rather than
// trusting a diff sent back by the browser. The file may have changed between
// the preview and the confirmation, and a client-supplied change list would be
// a way to write anything anywhere.
func (s *boardServer) handleImportApply(w http.ResponseWriter, r *http.Request) {
	req, changes, err := s.planImport(r)
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
	if err := wf.doc.ApplyImport(changes); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.save(wf); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "applied": len(changes), "changes": changes})
}

func (s *boardServer) planImport(r *http.Request) (importRequest, []board.ImportChange, error) {
	var req importRequest
	if r.Method != http.MethodPost {
		return req, nil, fmt.Errorf("POST required")
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, nil, err
	}
	if req.Market == "" {
		req.Market = "ml"
	}
	pairs, err := board.ParsePaste(req.Blob)
	if err != nil {
		return req, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	wf, err := s.load(req.Week)
	if err != nil {
		return req, nil, err
	}
	changes, err := wf.doc.PlanImport(pairs, req.Book, req.Market)
	return req, changes, err
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("board serve: encoding response: %v", err)
	}
}

// httpError sends the reason as JSON, because every caller here is fetch() and
// a bare text body would have to be sniffed to tell an error from a result.
func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

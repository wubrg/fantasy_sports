package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"edge/internal/betlog"
)

// The beliefs view drives the belief-probe weekly loop from the browser: show
// the week's pasteable prompt, paste an LLM forecast and see the gates, and read
// the score once games settle. Pack GENERATION stays a CLI step (make
// belief-pack is Python and needs the nflverse cache); these handlers read what
// it produced and reuse the exact same score()/runIngest() the CLI does, so the
// browser and the terminal can never disagree.

func (s *boardServer) beliefsLog() string { return filepath.Join(s.beliefsDir, "log.jsonl") }

func (s *boardServer) packPaths(week int) (input, prompt string) {
	stem := fmt.Sprintf("week%02d", week)
	return filepath.Join(s.beliefsDir, stem+".input.json"),
		filepath.Join(s.beliefsDir, stem+".prompt.md")
}

// handleBeliefsPack returns the week's pasteable prompt and the slate, for the
// copy-and-forecast step. The pack must already exist on disk (make belief-pack).
func (s *boardServer) handleBeliefsPack(w http.ResponseWriter, r *http.Request) {
	week, err := strconv.Atoi(r.URL.Query().Get("week"))
	if err != nil || week < 1 {
		httpError(w, http.StatusBadRequest, "week must be a positive integer")
		return
	}
	inputPath, promptPath := s.packPaths(week)

	var pk inputPack
	if err := readJSON(inputPath, &pk, false); err != nil {
		httpError(w, http.StatusNotFound, fmt.Sprintf(
			"no pack for week %d — run `make belief-pack SEASON=<n> WEEK=%d` first (%v)", week, week, err))
		return
	}
	sum, err := fileSHA(inputPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		httpError(w, http.StatusNotFound, fmt.Sprintf("pasteable prompt missing: %v", err))
		return
	}

	type game struct {
		GameID  string   `json:"game_id"`
		Away    string   `json:"away"`
		Home    string   `json:"home"`
		Kickoff string   `json:"kickoff"`
		Total   *float64 `json:"total_line"`
		Spread  *float64 `json:"spread_line"`
	}
	games := make([]game, 0, len(pk.Games))
	for _, g := range pk.Games {
		games = append(games, game{
			GameID: g.GameID, Away: g.Away, Home: g.Home,
			Kickoff: g.Kickoff.Format(time.RFC3339), Total: g.TotalLine, Spread: g.SpreadLine,
		})
	}
	writeJSON(w, map[string]any{
		"season": pk.Season, "week": pk.Week, "sha": sum,
		"prompt": string(prompt), "games": games,
	})
}

// handleBeliefsIngest previews or applies a pasted forecast, the same gates the
// CLI runs. apply:false runs runIngest with a dry preview (nothing written);
// apply:true appends the ready records to the log. A whole-file refusal comes
// back as a 400 with the gate's message, exactly as the CLI prints it.
func (s *boardServer) handleBeliefsIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Week    int    `json:"week"`
		Blob    string `json:"blob"`
		Apply   bool   `json:"apply"`
		Partial bool   `json:"partial"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Week < 1 {
		httpError(w, http.StatusBadRequest, "week must be a positive integer")
		return
	}
	inputPath, _ := s.packPaths(req.Week)

	var pk inputPack
	if err := readJSON(inputPath, &pk, false); err != nil {
		httpError(w, http.StatusNotFound, fmt.Sprintf("no pack for week %d: %v", req.Week, err))
		return
	}
	if err := checkDefinitions(pk.Definitions); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Strict decode: an unknown field means the forecaster drifted off contract,
	// and silence would hide it — the same refusal the CLI makes on ingest.
	var ff forecastFile
	dec := json.NewDecoder(strings.NewReader(req.Blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ff); err != nil {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("the forecast is not valid JSON on contract: %v", err))
		return
	}
	sum, err := fileSHA(inputPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	res, err := runIngest(pk, ff, sum, inputPath, now, ingestOpts{partial: req.Partial})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Apply {
		for _, p := range res.ready {
			if _, err := betlog.Record(s.beliefsLog(), p, now); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	writeJSON(w, map[string]any{"applied": req.Apply, "result": res})
}

// handleBeliefsScore returns the ScoreResult the CLI's `beliefs score` computes,
// serialized. Read-only.
func (s *boardServer) handleBeliefsScore(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, _ := strconv.Atoi(q.Get("from"))
	to, _ := strconv.Atoi(q.Get("to"))
	vs := q.Get("vs")
	if vs == "" {
		vs = refAuto
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	preds, err := betlog.LoadPredictions(s.beliefsLog())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := score(preds, scoreOpts{
		fromWeek: from, toWeek: to, vs: vs, bar: 0.10, hold: 0.06,
	})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, res)
}

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

	"leaguehome/internal/draft"
)

// savedTeamsFile is where the durable shortlist lives, beside the lean sets.
// It holds your target rosters, so it is yours and gitignored.
const savedTeamsFile = "saved-teams.json"

// sampleDraws is how many auction outcomes each request draws; showTeams how
// many distinct teams it returns. A few hundred draws is instant and gives the
// dedup enough to surface a real spread; a dozen teams is a scannable shortlist.
const (
	sampleDraws = 300
	showTeams   = 12
)

// SavedPick is one player on a saved team and what the sample had him cost.
type SavedPick struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Price    int    `json:"price"`
}

// SavedTeam is a sampled roster pinned to the durable shortlist.
type SavedTeam struct {
	ID        string      `json:"id"`
	Objective string      `json:"objective"`
	Created   string      `json:"created"`
	Spend     int         `json:"spend"`
	POPR      float64     `json:"popr"`
	MyGuys    int         `json:"myGuys"`
	Picks     []SavedPick `json:"picks"`
}

// handleTeams samples the auction teams you could assemble under an objective,
// against the current pool (which reflects the active keeper scenario), your
// own keepers, and your real budget. A fresh seed each call, so re-sampling
// explores new draws rather than repeating.
func (s *server) handleTeams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Objective string `json:"objective"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	obj := draft.Objective(body.Objective)
	if obj != draft.ObjectiveStrategy && obj != draft.ObjectiveLeansMax {
		http.Error(w, fmt.Sprintf("unknown objective %q", body.Objective), http.StatusBadRequest)
		return
	}

	snap := s.snapshot()
	// Your own keepers are yours whatever you assume about rivals, so the team
	// is built on your Expected keepers and your real budget, drafting from the
	// pool the current scenario leaves.
	keepers := s.static.heldRoster(s.static.ownerID)
	budget, slots := s.static.budget, rosterSize
	for _, k := range keepers {
		budget -= k.Price
		slots--
	}

	teams := draft.SampleTeams(snap.Players, keepers, budget, slots, s.static.rosterShape(),
		s.scoringBaselines(), s.static.prefs, obj, sampleDraws, time.Now().UnixNano())
	if len(teams) > showTeams {
		teams = teams[:showTeams]
	}
	writeJSON(w, teams)
}

// handleTeamSave pins a sampled team to the durable shortlist.
func (s *server) handleTeamSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var team SavedTeam
	if err := json.NewDecoder(r.Body).Decode(&team); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(team.Picks) == 0 {
		http.Error(w, "a team needs at least one pick", http.StatusBadRequest)
		return
	}
	team.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	team.Created = time.Now().Format("Jan 2 15:04")

	s.savedMu.Lock()
	s.saved = append(s.saved, team)
	err := writeSavedTeams(s.savedPath, s.saved)
	list := append([]SavedTeam(nil), s.saved...)
	s.savedMu.Unlock()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// handleTeamDelete drops a saved team.
func (s *server) handleTeamDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.savedMu.Lock()
	kept := s.saved[:0]
	for _, t := range s.saved {
		if t.ID != body.ID {
			kept = append(kept, t)
		}
	}
	s.saved = kept
	err := writeSavedTeams(s.savedPath, s.saved)
	list := append([]SavedTeam(nil), s.saved...)
	s.savedMu.Unlock()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// handleTeamsSaved returns the durable shortlist, for the page to load on open.
func (s *server) handleTeamsSaved(w http.ResponseWriter, r *http.Request) {
	s.savedMu.Lock()
	list := append([]SavedTeam(nil), s.saved...)
	s.savedMu.Unlock()
	writeJSON(w, list)
}

// handleTeamScratch loads a saved team into the scratch roster, replacing what
// is there, so a target becomes the working hypothetical you can adjust by hand.
func (s *server) handleTeamScratch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.savedMu.Lock()
	var team *SavedTeam
	for i := range s.saved {
		if s.saved[i].ID == body.ID {
			team = &s.saved[i]
			break
		}
	}
	s.savedMu.Unlock()
	if team == nil {
		http.Error(w, "no saved team with that id", http.StatusNotFound)
		return
	}

	s.scratch.clear()
	for _, p := range team.Picks {
		s.scratch.add(p.PlayerID, p.Price)
	}
	snap := s.snapshot()
	writeJSON(w, scratchResponse{Board: snap, Scratch: s.scratchView(snap)})
}

// loadSavedTeams reads the shortlist. A missing file is an empty list, not an
// error — the same way the lean and preference files degrade.
func loadSavedTeams(path string) ([]SavedTeam, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading saved teams %s: %w", path, err)
	}
	var out []SavedTeam
	if len(b) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parsing saved teams %s: %w", path, err)
	}
	return out, nil
}

// writeSavedTeams persists the shortlist, written to a temp file and renamed so
// a crash mid-write cannot leave a half-file that fails to parse next startup.
func writeSavedTeams(path string, teams []SavedTeam) error {
	b, err := json.MarshalIndent(teams, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// savedTeamsPath scopes the shortlist to one owner, because boards for
// different owners share a config directory. A guest board runs the same
// binary against the same directory under its own owner id, and `-leans blank`
// withholds your reads while saying nothing about saved teams -- which the
// board fetches on load. One file would put your bid plan on a leaguemate's
// screen, and let him overwrite it. The owner id is already what makes a board
// someone's, so it is what separates the files.
//
// draftID scopes it a second time, and is set only for a draft outside this
// league -- a mock. A rehearsal board has to run under your real owner id or
// picks never register as yours and the budget it exists to exercise stays
// still, which would otherwise aim it at the same file as the live board and
// let a throwaway team overwrite the plan you drafted from. Empty for the
// league's own draft, so the live board keeps the path it has always had.
func savedTeamsPath(configDir, ownerID, draftID string) string {
	name := strings.TrimSuffix(savedTeamsFile, filepath.Ext(savedTeamsFile))
	for _, part := range []string{ownerID, draftID} {
		if tag := fileTag(part); tag != "" {
			name += "-" + tag
		}
	}
	return filepath.Join(configDir, name+filepath.Ext(savedTeamsFile))
}

// fileTag reduces an owner or draft id to what is safe in a filename. Sleeper
// ids are digits, but these arrive from the environment and the command line
// and are joined onto a path, so anything that could climb out of the config
// directory is dropped rather than trusted.
func fileTag(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

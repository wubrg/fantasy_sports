// Command leagueweb serves the League Home web UI: a static single-page
// app backed by a thin JSON API, both calling into the same internal/core
// operations as leaguectl and leaguebot. It owns no league-data logic of
// its own.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"

	"leaguehome/internal/core"
)

// defaultLeagueID is the "Hit or Miss" league's current Sleeper league ID
// (see leagues/hit_or_miss/readme.md). Override with the LEAGUE_ID environment
// variable for a different season or league.
const defaultLeagueID = "1368649414419189760"

//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	flag.Parse()

	leagueID := os.Getenv("LEAGUE_ID")
	if leagueID == "" {
		leagueID = defaultLeagueID
	}
	svc := core.New(leagueID)

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	// seasonService resolves the optional ?league= query param (one of the
	// IDs /api/seasons returns) to a Service for that season, falling back
	// to the server's configured league. Each Sleeper-backed-per-season
	// endpoint (standings/faab/matchups/scoring) takes this override so the
	// web UI's season selector can ask for a past year's data; State is
	// global NFL state, not league-specific, so it doesn't take one.
	seasonService := func(r *http.Request) *core.Service {
		if league := r.URL.Query().Get("league"); league != "" {
			return core.New(league)
		}
		return svc
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(staticContent)))

	mux.HandleFunc("/api/standings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, func() (interface{}, error) { return seasonService(r).Standings() })
	})
	mux.HandleFunc("/api/faab", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, func() (interface{}, error) { return seasonService(r).Faab() })
	})
	mux.HandleFunc("/api/matchups", func(w http.ResponseWriter, r *http.Request) {
		week, err := strconv.Atoi(r.URL.Query().Get("week"))
		if err != nil || week < 1 {
			http.Error(w, "missing or invalid week query parameter", http.StatusBadRequest)
			return
		}
		writeJSON(w, func() (interface{}, error) { return seasonService(r).Matchups(week) })
	})
	mux.HandleFunc("/api/history", jsonHandler(func() (interface{}, error) { return svc.History() }))
	mux.HandleFunc("/api/rules", jsonHandler(func() (interface{}, error) { return svc.Rules() }))
	mux.HandleFunc("/api/scoring", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, func() (interface{}, error) { return seasonService(r).Scoring() })
	})
	mux.HandleFunc("/api/managers", jsonHandler(func() (interface{}, error) { return svc.Managers() }))
	mux.HandleFunc("/api/announcements", jsonHandler(func() (interface{}, error) { return svc.Announcements() }))
	mux.HandleFunc("/api/schedule", jsonHandler(func() (interface{}, error) { return svc.Schedule() }))
	mux.HandleFunc("/api/rivalries", jsonHandler(func() (interface{}, error) { return svc.Rivalries() }))
	mux.HandleFunc("/api/state", jsonHandler(func() (interface{}, error) { return svc.State() }))
	mux.HandleFunc("/api/seasons", jsonHandler(func() (interface{}, error) { return svc.Seasons() }))

	log.Printf("league home web UI serving on %s (league %s)", *addr, leagueID)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// jsonHandler adapts a zero-argument core call into an http.HandlerFunc.
func jsonHandler(fn func() (interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fn)
	}
}

func writeJSON(w http.ResponseWriter, fn func() (interface{}, error)) {
	data, err := fn()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

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
	"strings"

	"leaguehome/internal/core"
)

// defaultLeagueID is the "Hit or Miss" league's current Sleeper league ID
// (see football/readme.md). Override with the LEAGUE_ID environment
// variable for a different season or league.
const defaultLeagueID = "1368649414419189760"

//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	prefix := flag.String("prefix", "", "URL path prefix to mount the app under (e.g. /leagueweb), for sharing one Tailscale hostname with other apps via `tailscale serve`")
	flag.Parse()

	*prefix = strings.TrimSuffix(*prefix, "/")
	if *prefix != "" && !strings.HasPrefix(*prefix, "/") {
		*prefix = "/" + *prefix
	}

	leagueID := os.Getenv("LEAGUE_ID")
	if leagueID == "" {
		leagueID = defaultLeagueID
	}
	svc := core.New(leagueID)

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle(*prefix+"/", http.StripPrefix(*prefix, http.FileServer(http.FS(staticContent))))

	mux.HandleFunc(*prefix+"/api/standings", jsonHandler(func() (interface{}, error) { return svc.Standings() }))
	mux.HandleFunc(*prefix+"/api/faab", jsonHandler(func() (interface{}, error) { return svc.Faab() }))
	mux.HandleFunc(*prefix+"/api/matchups", func(w http.ResponseWriter, r *http.Request) {
		week, err := strconv.Atoi(r.URL.Query().Get("week"))
		if err != nil || week < 1 {
			http.Error(w, "missing or invalid week query parameter", http.StatusBadRequest)
			return
		}
		writeJSON(w, func() (interface{}, error) { return svc.Matchups(week) })
	})
	mux.HandleFunc(*prefix+"/api/history", jsonHandler(func() (interface{}, error) { return svc.History() }))
	mux.HandleFunc(*prefix+"/api/rules", jsonHandler(func() (interface{}, error) { return svc.Rules() }))
	mux.HandleFunc(*prefix+"/api/scoring", jsonHandler(func() (interface{}, error) { return svc.Scoring() }))
	mux.HandleFunc(*prefix+"/api/managers", jsonHandler(func() (interface{}, error) { return svc.Managers() }))
	mux.HandleFunc(*prefix+"/api/announcements", jsonHandler(func() (interface{}, error) { return svc.Announcements() }))
	mux.HandleFunc(*prefix+"/api/schedule", jsonHandler(func() (interface{}, error) { return svc.Schedule() }))
	mux.HandleFunc(*prefix+"/api/rivalries", jsonHandler(func() (interface{}, error) { return svc.Rivalries() }))
	mux.HandleFunc(*prefix+"/api/state", jsonHandler(func() (interface{}, error) { return svc.State() }))

	log.Printf("league home web UI serving on %s (league %s, prefix: %q)", *addr, leagueID, *prefix)
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

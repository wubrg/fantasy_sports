// Command canton serves the NFL Awards Reference browser: a static
// filterable UI backed by the SQLite-persisted award dataset. Designed to
// run on a local desktop and be reached over a Tailscale network. Named
// after the Pro Football Hall of Fame's home, since the app is expected to
// broaden beyond awards lookups over time (see ../README.md Roadmap).
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"canton/internal/store"
)

//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "address to listen on (use :PORT to bind all interfaces, reachable over Tailscale)")
	dbPath := flag.String("db", "../data/canton.db", "path to the sqlite database")
	flag.Parse()

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening db %s: %v", *dbPath, err)
	}
	defer s.Close()

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(staticContent)))
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		ds, err := s.Dataset()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ds)
	})

	log.Printf("nfl awards reference serving on %s (db: %s)", *addr, *dbPath)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

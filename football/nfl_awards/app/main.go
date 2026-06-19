// Command nflawards serves the NFL Awards Reference browser: a static
// filterable UI backed by the nfl_awards_data.json dataset. Designed to run
// on a local desktop and be reached over a Tailscale network.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
)

//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "address to listen on (use :PORT to bind all interfaces, reachable over Tailscale)")
	dataPath := flag.String("data", "../data/nfl_awards_data.json", "path to nfl_awards_data.json")
	flag.Parse()

	data, err := os.ReadFile(*dataPath)
	if err != nil {
		log.Fatalf("reading data file %s: %v", *dataPath, err)
	}

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(staticContent)))
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	log.Printf("nfl awards reference serving on %s (data: %s)", *addr, *dataPath)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

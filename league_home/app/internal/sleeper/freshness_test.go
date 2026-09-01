package sleeper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recordingServer answers every request and remembers the raw URLs asked for.
func recordingServer(t *testing.T, body func(path string) string) (*Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body(r.URL.Path))
	}))
	t.Cleanup(srv.Close)

	return &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// TestTheLiveDraftEndpointsBypassTheEdgeCache is the whole point of getFresh.
//
// Sleeper serves the draft object and its picks with
// `cache-control: public, s-maxage=30`, so a plain request can return a
// response half a minute old — observed on a live draft as `cf-cache-status:
// HIT` with `age` climbing toward thirty. That is not a slow board, it is a
// board reading a stale document, and no amount of polling fixes it: every
// request inside the window returns identical bytes.
//
// Each request therefore has to be its own cache key.
func TestTheLiveDraftEndpointsBypassTheEdgeCache(t *testing.T) {
	c, seen := recordingServer(t, func(path string) string {
		if strings.HasSuffix(path, "/picks") {
			return `[]`
		}
		return `{"draft_id":"d1","status":"drafting"}`
	})

	if _, err := c.Draft("d1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DraftPicks("d1"); err != nil {
		t.Fatal(err)
	}

	got := seen()
	if len(got) != 2 {
		t.Fatalf("want two requests, got %v", got)
	}
	for _, u := range got {
		if !strings.Contains(u, "_=") {
			t.Errorf("%s carries no cache-busting parameter, so it can be served "+
				"from a thirty-second edge cache", u)
		}
	}

	// Two calls to the same endpoint must not collide, or the second is a
	// cache hit on the first and the whole exercise is pointless.
	if _, err := c.Draft("d1"); err != nil {
		t.Fatal(err)
	}
	got = seen()
	if got[0] == got[2] {
		t.Errorf("two draft reads produced the same URL %q: they share a cache key", got[0])
	}
}

// TestOnlyTheLiveEndpointsAreBusted.
//
// Everything else this client fetches is immutable or changes on the scale of
// days — the player dictionary alone is 14MB — and is far better served from
// the edge. Busting those would trade Sleeper's bandwidth and ours for
// freshness nothing needs.
func TestOnlyTheLiveEndpointsAreBusted(t *testing.T) {
	c, seen := recordingServer(t, func(path string) string {
		switch {
		case strings.HasSuffix(path, "/rosters"), strings.HasSuffix(path, "/users"),
			strings.HasSuffix(path, "/drafts"):
			return `[]`
		case strings.HasSuffix(path, "/players/nfl"):
			return `{}`
		default:
			return `{}`
		}
	})

	if _, err := c.League("L1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Rosters("L1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Users("L1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Drafts("L1"); err != nil {
		t.Fatal(err)
	}

	for _, u := range seen() {
		if strings.Contains(u, "_=") {
			t.Errorf("%s is cache-busted, but it does not change fast enough to "+
				"justify missing the edge every time", u)
		}
	}
}

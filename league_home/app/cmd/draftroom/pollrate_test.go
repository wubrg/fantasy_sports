package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"leaguehome/internal/sleeper"
)

// TestPollAsksForStatusFarLessOftenThanPicks is the test that protects the
// call budget.
//
// The loop used to ask Sleeper two questions on every tick — "is the draft
// running" and "what has been picked" — so halving the interval to make the
// board feel live would have doubled a cost that was already double what the
// comment claimed. Picks are what the board watches; status only decides how
// hard to watch them, and it changes twice in an evening. Holding a stale
// answer to the cheap question is what pays for asking the expensive one twice
// as often.
//
// Two boards run on draft night, so this ratio is the difference between 132
// calls a minute and 240.
func TestPollAsksForStatusFarLessOftenThanPicks(t *testing.T) {
	var mu sync.Mutex
	var status, picks int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if strings.HasSuffix(r.URL.Path, "/picks") {
			picks++
		} else {
			status++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/picks") {
			fmt.Fprint(w, `[]`)
			return
		}
		// "drafting" is what puts the loop on the fast cadence; anything
		// else and this test would measure the idle path instead.
		fmt.Fprint(w, `{"draft_id":"d1","status":"drafting","type":"auction"}`)
	}))
	defer srv.Close()

	static := testStatic()
	static.client = &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	static.draftID = "d1"

	board, err := newServer(static, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	// Fast enough to gather a real sample without making the suite slow. The
	// loop is unbounded by design — it is the board's lifetime — so it is left
	// running and the process reaps it.
	const tick = 20 * time.Millisecond
	go board.pollForever(tick)

	const window = 400 * time.Millisecond
	time.Sleep(window)

	mu.Lock()
	gotStatus, gotPicks := status, picks
	mu.Unlock()

	// The window is far shorter than statusInterval, so status should have
	// been asked once at startup and at most once more. Being generous about
	// the upper bound keeps this from flaking on a loaded machine; the
	// failure it exists to catch is status tracking picks one-for-one.
	if gotStatus > 3 {
		t.Errorf("status asked %d times in %s (statusInterval is %s): the check is back on every tick",
			gotStatus, window, statusInterval)
	}
	// Sanity: the loop really was polling. Without this a broken loop that
	// asked for nothing at all would pass the assertion above.
	if gotPicks < 5 {
		t.Fatalf("picks polled only %d times in %s at a %s tick — the loop is not running",
			gotPicks, window, tick)
	}
	if gotPicks <= gotStatus {
		t.Errorf("picks %d, status %d: the two are still coupled", gotPicks, gotStatus)
	}
}

// The floor exists so a typo cannot quietly point the board at Sleeper at full
// speed. It is checked before any network call, so an unusable value fails at
// startup rather than after a minute of loading.
func TestServeRejectsAPollIntervalBelowTheFloor(t *testing.T) {
	err := runServe(":0", "", "", t.TempDir(), t.TempDir(), "", "", nil, "", 10*time.Millisecond)
	if err == nil {
		t.Fatal("a 10ms poll interval was accepted")
	}
	if !strings.Contains(err.Error(), "below the") {
		t.Errorf("error does not explain the floor: %v", err)
	}
}

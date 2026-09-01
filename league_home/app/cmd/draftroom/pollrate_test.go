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

// TestPollMakesExactlyTwoCallsPerTick is what protects the call budget now.
//
// This test used to assert the opposite: that status was asked far less often
// than picks, because staling status to ten seconds is what paid for polling
// picks every second. That trade is off. The draft object also carries the
// current nomination, which lives inside a ten-second timer, so reading it a
// tick late means missing most nominations — it has to be read every tick, and
// the status now comes along for free rather than the reverse.
//
// What is still worth protecting is that the loop makes *two* calls a tick and
// not four. The obvious way to break it is to ask for the draft twice, once
// for the status and once for the nomination, which would take two boards on
// draft night from 240 calls a minute to 480.
func TestPollMakesExactlyTwoCallsPerTick(t *testing.T) {
	var mu sync.Mutex
	var draftCalls, pickCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if strings.HasSuffix(r.URL.Path, "/picks") {
			pickCalls++
		} else {
			draftCalls++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/picks") {
			fmt.Fprint(w, `[]`)
			return
		}
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

	// The loop is unbounded by design — it is the board's lifetime — so it is
	// left running and the process reaps it.
	const tick = 20 * time.Millisecond
	go board.pollForever(tick)

	const window = 400 * time.Millisecond
	time.Sleep(window)

	mu.Lock()
	gotDraft, gotPicks := draftCalls, pickCalls
	mu.Unlock()

	if gotPicks < 5 {
		t.Fatalf("picks polled only %d times in %s at a %s tick — the loop is not running",
			gotPicks, window, tick)
	}
	// One of each per tick. Generous bounds, because the assertion is about
	// the shape of the loop rather than the scheduler's precision; what fails
	// here is a second fetch per tick, which shows up as roughly double.
	if gotDraft > gotPicks*3/2 {
		t.Errorf("draft fetched %d times against %d picks: the loop is asking for the draft more than once a tick",
			gotDraft, gotPicks)
	}
	if gotDraft*3/2 < gotPicks {
		t.Errorf("draft fetched %d times against %d picks: the nomination is being read less often than every tick, so a ten-second nomination can be missed",
			gotDraft, gotPicks)
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

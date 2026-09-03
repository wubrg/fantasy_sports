package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"leaguehome/internal/sleeper"
)

// TestIdleCadenceTightensAroundTheStart.
//
// An idle board asks once a minute, which is right for the eleven months a
// draft is not happening and wrong for the ten minutes before one. The
// opening nomination runs on a short clock; a board that notices the draft
// began up to a minute late can miss it outright.
func TestIdleCadenceTightensAroundTheStart(t *testing.T) {
	const live = 3 * time.Second
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		startsA time.Time
		want    time.Duration
	}{
		{"a week out", now.Add(7 * 24 * time.Hour), idleInterval},
		{"an hour out", now.Add(time.Hour), idleInterval},
		{"just outside the window", now.Add(nearStartWindow + time.Minute), idleInterval},
		{"just inside the window", now.Add(nearStartWindow - time.Minute), live},
		{"a minute out", now.Add(time.Minute), live},
		{"exactly on time", now, live},
		// The commissioner running late is the case this exists for: the
		// hour is past, the status has not flipped, and the board must not
		// have gone back to sleep.
		{"half an hour late", now.Add(-30 * time.Minute), live},
		{"within the grace period", now.Add(-lateStartGrace + time.Minute), live},
		// A draft that never happened must not poll at draft speed forever.
		{"long abandoned", now.Add(-lateStartGrace - time.Minute), idleInterval},
		// No start time set at all: nothing to tighten around.
		{"no start time", time.Time{}, idleInterval},
	}

	for _, c := range cases {
		if got := idleCadence(c.startsA, now, live); got != c.want {
			t.Errorf("%s: cadence %v, want %v", c.name, got, c.want)
		}
	}
}

// The tight window is bounded on both sides, so an idle board's daily volume
// is unchanged outside it.
func TestIdleCadenceCostsNothingOutsideTheWindow(t *testing.T) {
	now := time.Now()
	far := idleCadence(now.Add(48*time.Hour), now, 3*time.Second)
	if far != idleInterval {
		t.Errorf("a draft two days out polls every %v, want %v", far, idleInterval)
	}
}

// scheduledBoard serves a draft in the given status with the given scheduled
// start, which is what an idle board needs in order to know when to wake up.
func scheduledBoard(t *testing.T, status string, startsAt time.Time) *staticData {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"draft_id":"d1","status":%q,"type":"auction","start_time":%d,"metadata":{}}`,
			status, startsAt.UnixMilli())
	}))
	t.Cleanup(srv.Close)

	s := testStatic()
	s.client = &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	s.draftID = "d1"
	return s
}

// TestAPreDraftBoardStillReportsItsStartTime.
//
// The wiring that matters: the cadence rule is only reachable if the start
// time survives the "not live" path, which is the one path an idle board ever
// takes. Returning it only for a running draft would leave the board asleep
// for exactly the case this was built for.
func TestAPreDraftBoardStillReportsItsStartTime(t *testing.T) {
	want := time.Now().Add(5 * time.Minute).Truncate(time.Millisecond)
	live, _, startsAt := scheduledBoard(t, "pre_draft", want).DraftState()

	if live {
		t.Error("a pre_draft board reported itself live")
	}
	if !startsAt.Equal(want) {
		t.Fatalf("start time %v, want %v", startsAt, want)
	}
	if got := idleCadence(startsAt, time.Now(), 3*time.Second); got != 3*time.Second {
		t.Errorf("a draft five minutes out polls every %v, want the live cadence", got)
	}
}

// And a draft nowhere near its start keeps the idle cadence, so the board's
// year-round volume is untouched.
func TestADistantDraftKeepsTheIdleCadence(t *testing.T) {
	starts := time.Now().Add(30 * 24 * time.Hour)
	_, _, startsAt := scheduledBoard(t, "pre_draft", starts).DraftState()

	if got := idleCadence(startsAt, time.Now(), 3*time.Second); got != idleInterval {
		t.Errorf("a draft a month out polls every %v, want %v", got, idleInterval)
	}
}

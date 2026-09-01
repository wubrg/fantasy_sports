package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"leaguehome/internal/sleeper"
)

// TestPausedCountsAsInSession.
//
// Sleeper reports a paused draft as "paused", and the poll loop reads this to
// decide between the one-second cadence and the one-minute one. A pause is a
// break in a draft you are sitting in, and it ends without warning — so
// treating it as idle meant the board could be up to a minute stale at the
// moment bidding resumed, which is the one moment it must not be. Found by
// pausing a mock and watching the board stop keeping up.
func TestPausedCountsAsInSession(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"drafting", true},
		{"paused", true},
		{"pre_draft", false},
		{"complete", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"draft_id":"d1","status":%q,"type":"auction"}`, tc.status)
		}))

		s := &staticData{
			client:  &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
			draftID: "d1",
		}
		if got := s.Drafting(); got != tc.want {
			t.Errorf("status %q: in session = %v, want %v", tc.status, got, tc.want)
		}
		srv.Close()
	}
}

// No draft to watch is not a draft in session, and must not put the board on
// the fast cadence for the eleven months it is mounted with nothing to follow.
func TestNoDraftIsNotInSession(t *testing.T) {
	if (&staticData{}).Drafting() {
		t.Error("a board with no draft reported one in session")
	}
}

// A failed lookup stays live on purpose: a blip must not stall the board on
// the one night it matters, and one minute of blindness is worse than one
// minute of wasted polling.
func TestAFailedStatusLookupStaysLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &staticData{
		client:  &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		draftID: "d1",
	}
	if !s.Drafting() {
		t.Error("a failed status lookup dropped the board to the idle cadence")
	}
}

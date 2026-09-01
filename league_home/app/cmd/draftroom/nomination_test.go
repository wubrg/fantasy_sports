package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"leaguehome/internal/draft"
	"leaguehome/internal/sleeper"
)

// nominatingBoard stands up a draft whose metadata says what the caller wants.
func nominatingBoard(t *testing.T, status, playerID, timerEnd string) *staticData {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"draft_id":"d1","status":%q,"type":"auction","metadata":{"nominated_player_id":%q,"timer_end_at":%q}}`,
			status, playerID, timerEnd)
	}))
	t.Cleanup(srv.Close)

	s := testStatic()
	s.client = &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	s.draftID = "d1"
	return s
}

func soon() string { return time.Now().Add(8 * time.Second).UTC().Format(time.RFC3339) }
func past() string { return time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339) }

// TestNominationNamesThePlayerBeingBidOn — the whole point.
func TestNominationNamesThePlayerBeingBidOn(t *testing.T) {
	s := nominatingBoard(t, "drafting", "1", soon())

	live, nom := s.DraftState()
	if !live {
		t.Fatal("a drafting board did not report itself in session")
	}
	if nom == nil {
		t.Fatal("no nomination reported while one is live")
	}
	if nom.PlayerID != "1" || nom.Name != "Jahmyr Gibbs" {
		t.Errorf("got %+v, want Jahmyr Gibbs (id 1)", nom)
	}
	if nom.Position != "RB" {
		t.Errorf("position = %q, want RB — the banner has to name him even when "+
			"he is not on the board", nom.Position)
	}
}

// TestAnExpiredNominationIsNotReported is the sharp edge.
//
// Sleeper never clears nominated_player_id: it holds the last nomination for
// as long as the draft object exists. A board that reads the id alone would
// pin a player the room finished bidding on to the top of the screen, and
// naming the wrong player mid-auction is worse than naming none.
func TestAnExpiredNominationIsNotReported(t *testing.T) {
	s := nominatingBoard(t, "drafting", "1", past())

	live, nom := s.DraftState()
	if !live {
		t.Fatal("an expired timer should not end the draft")
	}
	if nom != nil {
		t.Errorf("reported %q as up for auction after his timer ran out", nom.Name)
	}
}

// An unparseable timer is treated as expired: fall silent on a format we do
// not understand rather than pin a name up forever.
func TestAnUnreadableTimerIsTreatedAsExpired(t *testing.T) {
	if _, nom := nominatingBoard(t, "drafting", "1", "not-a-time").DraftState(); nom != nil {
		t.Errorf("reported a nomination on an unparseable timer: %+v", nom)
	}
	if _, nom := nominatingBoard(t, "drafting", "1", "").DraftState(); nom != nil {
		t.Errorf("reported a nomination with no timer at all: %+v", nom)
	}
}

// Nothing nominated is not a nomination.
func TestNoNominatedPlayerIsNoNomination(t *testing.T) {
	if _, nom := nominatingBoard(t, "drafting", "", soon()).DraftState(); nom != nil {
		t.Errorf("invented a nomination from an empty id: %+v", nom)
	}
}

// A draft nobody has started has no nomination, whatever its metadata says.
func TestADraftNotInSessionHasNoNomination(t *testing.T) {
	for _, status := range []string{"pre_draft", "complete"} {
		live, nom := nominatingBoard(t, status, "1", soon()).DraftState()
		if live {
			t.Errorf("status %q reported as in session", status)
		}
		if nom != nil {
			t.Errorf("status %q reported a nomination: %+v", status, nom)
		}
	}
}

// TestAPausedDraftKeepsItsNomination — and note the empty timer, which is the
// whole point.
//
// Sleeper drops timer_end_at while a draft is paused, because the clock is not
// running. An earlier version of this test passed a future timer and so proved
// nothing: it asserted a state that cannot occur. Against the real paused mock
// the banner went blank, because "no timer" was being read as "expired" — but
// a pause is exactly when the nominated player is still the nominated player.
func TestAPausedDraftKeepsItsNomination(t *testing.T) {
	live, nom := nominatingBoard(t, "paused", "1", "").DraftState()
	if !live {
		t.Fatal("a paused draft is still in session")
	}
	if nom == nil {
		t.Error("a paused draft dropped the player it is paused on")
	}
}

// A failed lookup stays live but reports no nomination: a stale name is a
// claim, where a missing banner is only silence.
func TestAFailedLookupReportsNoNomination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := testStatic()
	s.client = &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	s.draftID = "d1"

	live, nom := s.DraftState()
	if !live {
		t.Error("a blip dropped the board to the idle cadence")
	}
	if nom != nil {
		t.Errorf("a failed lookup produced a nomination: %+v", nom)
	}
}

// TestASoldPlayerLeavesTheBanner.
//
// The poll writes the nomination and the picks separately, so for up to one
// tick the board can hold a nomination for a player it has just seen sold.
// The banner must not outlive the bidding.
func TestASoldPlayerLeavesTheBanner(t *testing.T) {
	srv := scratchServer(t)
	srv.nomination = &draft.Nomination{PlayerID: "1", Name: "Jahmyr Gibbs", Position: "RB"}

	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	if srv.snapshot().Nomination == nil {
		t.Fatal("a live nomination did not reach the snapshot")
	}

	srv.taken["1"] = gone{price: 83, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	if nom := srv.snapshot().Nomination; nom != nil {
		t.Errorf("still bidding on %q after he was sold", nom.Name)
	}
}

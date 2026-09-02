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
// timerEnd is still written into the stub even though nothing reads it, so a
// test can prove that a long-expired timer does not hide a live nomination.
func nominatingBoard(t *testing.T, status, playerID, timerEnd string) *staticData {
	return biddingBoard(t, status, playerID, timerEnd, "", "", "")
}

// biddingBoard is nominatingBoard with the bid fields set. Sleeper sends all of
// them as strings, including the two that are plainly numbers.
func biddingBoard(t *testing.T, status, playerID, timerEnd, offer, slot, userID string) *staticData {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"draft_id":"d1","status":%q,"type":"auction","metadata":{"nominated_player_id":%q,"timer_end_at":%q,"highest_offer":%q,"offering_slot":%q,"offering_user_id":%q}}`,
			status, playerID, timerEnd, offer, slot, userID)
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

// TestAnExpiredTimerDoesNotHideALiveNomination is the regression.
//
// timer_end_at looks like the way to tell a live nomination from the stale
// field Sleeper leaves behind, and it is not: Sleeper stamps it once and never
// advances it. Bidding routinely outlasts it. Measured against the live mock,
// one player held the block for twenty seconds while his timer drifted from
// +0.1s to -18.7s — so an expiry check blanked the banner about a second into
// every nomination, which reads from the outside as a banner that will not
// update.
//
// Every timer below is long past, and every one of them must still name the
// player. What ends a nomination is the sale, not the clock.
func TestAnExpiredTimerDoesNotHideALiveNomination(t *testing.T) {
	for _, timer := range []string{past(), "", "not-a-time"} {
		live, nom := nominatingBoard(t, "drafting", "1", timer).DraftState()
		if !live {
			t.Fatalf("timer %q: draft not in session", timer)
		}
		if nom == nil {
			t.Errorf("timer %q: the banner went blank on a live nomination", timer)
		}
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

// A pause is a break in the bidding, not the end of it, so the player on the
// block stays on the block. Sleeper drops timer_end_at entirely while paused,
// which is one of the several shapes that field takes and one more reason
// nothing reads it.
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

// TestTheLeadingBidIsRead — the banner leads with whether to bid, and cannot
// without knowing what it is up against.
func TestTheLeadingBidIsRead(t *testing.T) {
	s := biddingBoard(t, "drafting", "1", soon(), "46", "2", "")
	_, nom := s.DraftState()
	if nom == nil {
		t.Fatal("no nomination")
	}
	if nom.HighestOffer != 46 {
		t.Errorf("HighestOffer = %d, want 46 parsed from the string Sleeper sends", nom.HighestOffer)
	}
	if nom.Leader != 2 {
		t.Errorf("Leader = %d, want seat 2", nom.Leader)
	}
}

// An unbid nomination is zero, not a guess. "no bid yet" and "$0" are
// different claims and the page renders them differently.
func TestAnUnbidNominationHasNoOffer(t *testing.T) {
	_, nom := biddingBoard(t, "drafting", "1", soon(), "", "", "").DraftState()
	if nom == nil {
		t.Fatal("no nomination")
	}
	if nom.HighestOffer != 0 || nom.Leader != 0 || nom.Mine {
		t.Errorf("want an empty bid, got %+v", nom)
	}
}

// TestWhoHoldsTheBid walks the same ladder isMine walks for a pick, and for
// the same reason: a real draft names the manager, a mock gives only the seat.
func TestWhoHoldsTheBid(t *testing.T) {
	const me = "243501760939814912"
	for _, tc := range []struct {
		name        string
		offer, slot string
		userID      string
		mySlot      int
		want        bool
	}{
		{"named as me", "20", "7", me, 7, true},
		{"named as someone else", "20", "7", "467790106363686912", 7, false},
		{"mock: my seat, no user id", "20", "7", "", 7, true},
		{"mock: another seat", "20", "3", "", 7, false},
		{"no seat known, nothing claimed", "20", "7", "", 0, false},
		{"no seat offered, nothing claimed", "20", "", "", 7, false},
		// The case that matters: an unset seat and an unknown seat are both
		// zero, so a bare equality claims every unbid nomination as yours.
		{"both unknown must not match", "20", "", "", 0, false},
	} {
		s := biddingBoard(t, "drafting", "1", soon(), tc.offer, tc.slot, tc.userID)
		s.ownerID, s.mySlot = me, tc.mySlot
		_, nom := s.DraftState()
		if nom == nil {
			t.Fatalf("%s: no nomination", tc.name)
		}
		if nom.Mine != tc.want {
			t.Errorf("%s: Mine = %v, want %v", tc.name, nom.Mine, tc.want)
		}
	}
}

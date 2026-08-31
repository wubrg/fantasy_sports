package main

import (
	"testing"

	"leaguehome/internal/sleeper"
)

// TestWatchedDraft — which draft the board follows.
//
// The explicit id is the whole point of the -draft flag: a Sleeper mock draft
// belongs to no league, so league discovery returns nothing for it and a
// rehearsal would silently watch the real league's draft instead.
func TestWatchedDraft(t *testing.T) {
	league := []sleeper.Draft{{DraftID: "newest"}, {DraftID: "older"}}

	for _, tc := range []struct {
		name     string
		explicit string
		drafts   []sleeper.Draft
		want     string
		wantOff  bool
	}{
		{"explicit id wins over the league's own", "mock-1", league, "mock-1", true},
		{"explicit id works when the league has none", "mock-1", nil, "mock-1", true},
		{"discovery takes the newest, which Sleeper returns first", "", league, "newest", false},
		{"no draft anywhere is not an error", "", nil, "", false},
		{"naming the league's own draft is not a rehearsal", "newest", league, "newest", false},
		{"naming an older draft of this league is not one either", "older", league, "older", false},
	} {
		got, off := watchedDraft(tc.explicit, tc.drafts)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if off != tc.wantOff {
			t.Errorf("%s: off-league %v, want %v", tc.name, off, tc.wantOff)
		}
	}
}

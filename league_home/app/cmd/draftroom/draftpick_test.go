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
	}{
		{"explicit id wins over the league's own", "mock-1", league, "mock-1"},
		{"explicit id works when the league has none", "mock-1", nil, "mock-1"},
		{"discovery takes the newest, which Sleeper returns first", "", league, "newest"},
		{"no draft anywhere is not an error", "", nil, ""},
	} {
		if got := watchedDraft(tc.explicit, tc.drafts); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

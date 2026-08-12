package draft

import "testing"

// pool is the projection source's spelling of every player, which is what a
// lean has to match — see Unmatched.
var pool = []string{
	"Isaiah Likely",
	"Jacory Croskey-Merritt",
	"Ja'Kobi Lane",
	"A.J. Brown",
	"Cyrus Allen",
	"Kenneth Walker",
}

// TestPunctuationAndCaseAreNotMismatches — the false positive that would
// matter most. Crying wolf over "a.j. brown" would train you to skim the
// report, and the report only earns its place if everything in it is real.
func TestPunctuationAndCaseAreNotMismatches(t *testing.T) {
	leans := leansFrom(t, "player,lean\n"+
		"a.j. brown,up\n"+
		"jakobi Lane,up\n"+
		"cyrus allen,up\n"+
		"KENNETH WALKER,must\n")
	if got := leans.Unmatched(pool); len(got) != 0 {
		t.Errorf("expected no unmatched reads, got %+v", got)
	}
}

// TestNearMissSuggestsTheRealPlayer — the whole point. A name typed on a
// phone is wrong by a character or two, and the fix is only cheap if the
// report says which player you meant.
func TestNearMissSuggestsTheRealPlayer(t *testing.T) {
	leans := leansFrom(t, "player,lean\n"+
		"isaiah lilely,up\n"+
		"jacorey croskey-merritt,up\n")
	got := leans.Unmatched(pool)
	if len(got) != 2 {
		t.Fatalf("expected 2 unmatched reads, got %d: %+v", len(got), got)
	}
	// Sorted by the name as written, so the output reads the same twice.
	want := []struct{ player, suggestion string }{
		{"isaiah lilely", "Isaiah Likely"},
		{"jacorey croskey-merritt", "Jacory Croskey-Merritt"},
	}
	for i, w := range want {
		if got[i].Lean.Player != w.player || got[i].Suggestion != w.suggestion {
			t.Errorf("got %q -> %q, want %q -> %q",
				got[i].Lean.Player, got[i].Suggestion, w.player, w.suggestion)
		}
	}
}

// TestNoSuggestionBeatsAWrongOne — with 450 names in the pool something is
// always "nearest". Naming it anyway would invent a read you never held.
func TestNoSuggestionBeatsAWrongOne(t *testing.T) {
	leans := leansFrom(t, "player,lean\nQuinshon Judkins,up\n")
	got := leans.Unmatched(pool)
	if len(got) != 1 {
		t.Fatalf("expected 1 unmatched read, got %d: %+v", len(got), got)
	}
	if got[0].Suggestion != "" {
		t.Errorf("nothing in the pool is close; got suggestion %q", got[0].Suggestion)
	}
}

// TestUnmatchedCarriesTheReadItself — so a caller can say whether the lost
// read was a must-have or a shrug, which decides how much it matters.
func TestUnmatchedCarriesTheReadItself(t *testing.T) {
	leans := leansFrom(t, "player,lean,note\nisaiah lilely,must,the TE room is his\n")
	got := leans.Unmatched(pool)
	if len(got) != 1 {
		t.Fatalf("expected 1 unmatched read, got %d", len(got))
	}
	if got[0].Lean.Lean != LeanMust || got[0].Lean.Note != "the TE room is his" {
		t.Errorf("the read did not survive: %+v", got[0].Lean)
	}
}

// TestAnEmptyPoolReportsNothing — no projection source loaded is not the
// same claim as "none of your reads are real", and the command has to be
// able to tell those apart.
func TestAnEmptyPoolReportsNothing(t *testing.T) {
	leans := leansFrom(t, "player,lean\nisaiah lilely,up\n")
	if got := leans.Unmatched(nil); got != nil {
		t.Errorf("an empty pool should assert nothing, got %+v", got)
	}
}

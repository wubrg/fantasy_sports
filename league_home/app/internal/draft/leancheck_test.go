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
	if got := leans.Unmatched(pool, nil); len(got) != 0 {
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
	got := leans.Unmatched(pool, nil)
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
	got := leans.Unmatched(pool, nil)
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
	got := leans.Unmatched(pool, nil)
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
	if got := leans.Unmatched(nil, nil); got != nil {
		t.Errorf("an empty pool should assert nothing, got %+v", got)
	}
}

// poolNames is the projection source's spelling of each player, which is
// what a lean is matched against.
var matcherPool = []string{
	"Kenneth Walker",
	"Chase Brown",
	"A.J. Brown",
	"Mitchell Tinsley",
	"Isaiah Likely",
}

// TestSuffixesAreNotAMismatch — Sleeper says "Kenneth Walker III" and the
// projection source says "Kenneth Walker". Typing the fuller name is the
// natural thing to do and used to silently record nothing.
func TestSuffixesAreNotAMismatch(t *testing.T) {
	m := NewPoolMatcher(matcherPool, nil)
	for _, written := range []string{"Kenneth Walker III", "Kenneth Walker", "kenneth walker iii"} {
		got, ok := m.Canonical(written)
		if !ok || got != "Kenneth Walker" {
			t.Errorf("%q resolved to %q (ok=%v), want Kenneth Walker", written, got, ok)
		}
	}
}

// TestSuffixInThePoolAlsoMatches — the drift runs both ways: the source may
// be the one carrying the suffix.
func TestSuffixInThePoolAlsoMatches(t *testing.T) {
	m := NewPoolMatcher([]string{"Marvin Harrison Jr."}, nil)
	got, ok := m.Canonical("Marvin Harrison")
	if !ok || got != "Marvin Harrison Jr." {
		t.Errorf("resolved to %q (ok=%v), want Marvin Harrison Jr.", got, ok)
	}
}

// TestAliasSynonymsMatchEachOther — aliases.csv maps names to Sleeper ids,
// so two names on the same id are two spellings of one player. That is the
// case aliases exist for, and leans never got the benefit of it.
func TestAliasSynonymsMatchEachOther(t *testing.T) {
	aliases := Aliases{
		normalizeName("Mitch Tinsley"):      "11068",
		normalizeName("Mitchell Tinsley"):   "11068",
		normalizeName("Dermarcus Robinson"): "3286",
	}
	m := NewPoolMatcher(matcherPool, aliases)
	got, ok := m.Canonical("Mitch Tinsley")
	if !ok || got != "Mitchell Tinsley" {
		t.Errorf("resolved to %q (ok=%v), want Mitchell Tinsley", got, ok)
	}
	// An alias whose id names nobody in the pool still resolves to nothing.
	if got, ok := m.Canonical("Dermarcus Robinson"); ok {
		t.Errorf("an alias for a player outside the pool resolved to %q", got)
	}
}

// TestAmbiguityResolvesToNothing — two pool players collapsing to the same
// suffix-stripped name is a question, not an answer. Picking one would put a
// read on a player you did not name.
func TestAmbiguityResolvesToNothing(t *testing.T) {
	m := NewPoolMatcher([]string{"Michael Pittman", "Michael Pittman Jr."}, nil)
	// The exact spelling still wins outright.
	if got, ok := m.Canonical("Michael Pittman Jr."); !ok || got != "Michael Pittman Jr." {
		t.Errorf("an exact name must still match itself: %q %v", got, ok)
	}
	if got, ok := m.Canonical("Michael Pittman Sr."); ok {
		t.Errorf("an ambiguous name resolved to %q; it should resolve to nothing", got)
	}
}

// TestMatcherStillRejectsRealTypos — the matcher must not get so forgiving
// that it undoes the reporting it feeds. These are the live typos from the
// phone-edited set.
func TestMatcherStillRejectsRealTypos(t *testing.T) {
	m := NewPoolMatcher(matcherPool, nil)
	for _, typo := range []string{"isaiah lilely", "jacorey croskey-merritt"} {
		if got, ok := m.Canonical(typo); ok {
			t.Errorf("%q resolved to %q; a typo must stay unmatched", typo, got)
		}
	}
	// Punctuation and case are still not differences.
	if got, ok := m.Canonical("aj brown"); !ok || got != "A.J. Brown" {
		t.Errorf("aj brown resolved to %q (ok=%v), want A.J. Brown", got, ok)
	}
}

// TestUnmatchedUsesTheMatcher — the report and the board have to agree, or
// the report cries wolf about reads that work and stops being read.
func TestUnmatchedUsesTheMatcher(t *testing.T) {
	leans := leansFrom(t, "player,lean\nKenneth Walker III,must\nisaiah lilely,up\n")
	m := NewPoolMatcher(matcherPool, nil)
	got := leans.Unmatched(matcherPool, m)
	if len(got) != 1 {
		t.Fatalf("expected only the real typo, got %d: %+v", len(got), got)
	}
	if got[0].Lean.Player != "isaiah lilely" {
		t.Errorf("wrong read reported: %+v", got[0])
	}
	if got[0].Suggestion != "Isaiah Likely" {
		t.Errorf("suggestion was %q, want Isaiah Likely", got[0].Suggestion)
	}
}

// TestMatchedLeanReachesTheBid — the point of the matcher. WalkAway looks a
// read up by the pool's spelling, so a lean written any other way used to
// leave the bid at the model's number with nothing said.
func TestMatchedLeanReachesTheBid(t *testing.T) {
	leans := leansFrom(t, "player,lean\nKenneth Walker III,must\n")

	// Before matching, the read cannot be found at all.
	if bid, _, rule := leans.WalkAway("Kenneth Walker", 24, 49); rule != RuleValue || bid != 24 {
		t.Fatalf("precondition failed: unmatched read already applied (%d, %s)", bid, rule)
	}

	matched := leans.Match(NewPoolMatcher(matcherPool, nil))
	bid, pl, rule := matched.WalkAway("Kenneth Walker", 24, 49)
	if rule != RuleMustHave || bid != 49 {
		t.Errorf("got $%d by %s, want $49 by must-have", bid, rule)
	}
	// The read keeps the spelling you wrote, so the board can show it back.
	if pl.Player != "Kenneth Walker III" {
		t.Errorf("the read lost its own wording: %+v", pl)
	}
}

// TestMatchLeavesUnmatchedReadsAlone — a typo must survive Match unchanged
// so the unmatched report can still find and explain it.
func TestMatchLeavesUnmatchedReadsAlone(t *testing.T) {
	leans := leansFrom(t, "player,lean\nisaiah lilely,up\n")
	matched := leans.Match(NewPoolMatcher(matcherPool, nil))
	if len(matched) != 1 {
		t.Fatalf("expected the read to survive, got %+v", matched)
	}
	if pl := matched[normalizeName("isaiah lilely")]; pl.Player != "isaiah lilely" {
		t.Errorf("the unmatched read moved or changed: %+v", matched)
	}
}

// TestMatchIsDeterministic — matching collapses two spellings onto one key,
// and Go randomises map iteration. Resolving that collision by iteration
// order gave a different board on maybe one restart in ten, silently, with
// the losing read's cap and note going with it.
func TestMatchIsDeterministic(t *testing.T) {
	pool := []string{"Kenneth Walker"}
	build := func() Leans {
		return Leans{
			normalizeName("Kenneth Walker"):     {Player: "Kenneth Walker", Lean: LeanMust, Cap: 20, Note: "hard cap"},
			normalizeName("Kenneth Walker III"): {Player: "Kenneth Walker III", Lean: LeanUp},
		}
	}
	m := NewPoolMatcher(pool, nil)
	first := build().Match(m)[normalizeName("Kenneth Walker")]
	for i := 0; i < 300; i++ {
		got := build().Match(m)[normalizeName("Kenneth Walker")]
		if got.Lean != first.Lean || got.Cap != first.Cap || got.Note != first.Note {
			t.Fatalf("run %d disagreed with the first: %+v vs %+v", i, got, first)
		}
	}
}

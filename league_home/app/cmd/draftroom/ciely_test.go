package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// TestCielyDivergenceSignFollowsDell.
//
// A better rank is a smaller number, so the gap has to be read the way
// DellDelta reads it: positive where Ciely rates a player above the field. Get
// the sign backwards and every flag on the board points the wrong way, which
// is the kind of defect that looks like a strong opinion rather than a bug.
func TestCielyDivergenceSignFollowsDell(t *testing.T) {
	primary := map[string]int{"up": 20, "down": 5, "same": 8}
	ciely := map[string]int{"up": 6, "down": 30, "same": 8}

	got := cielyDivergence(primary, ciely)

	if got["up"] != 14 {
		t.Errorf("Ciely ranks him 20th -> 6th: delta = %d, want +14", got["up"])
	}
	if got["down"] != -25 {
		t.Errorf("Ciely ranks him 5th -> 30th: delta = %d, want -25", got["down"])
	}
	if _, ok := got["same"]; ok {
		t.Errorf("agreement produced an entry: %d", got["same"])
	}
}

// A player either source does not cover is absent, not zero.
//
// Zero already means agreement. Spelling "no coverage" the same way would make
// every unranked player read as a consensus, which is a claim neither source
// made — and there are hundreds of them, since FantasyPros ranks roughly 735
// players against Ciely's 428.
func TestCielyDivergenceOmitsWhatNeitherSideRanked(t *testing.T) {
	primary := map[string]int{"both": 10, "primary-only": 4}
	ciely := map[string]int{"both": 2, "ciely-only": 7, "zero-rank": 0}

	got := cielyDivergence(primary, ciely)

	if len(got) != 1 {
		t.Fatalf("want only the player both ranked, got %v", got)
	}
	if got["both"] != 8 {
		t.Errorf("both = %d, want 8", got["both"])
	}
	for _, id := range []string{"primary-only", "ciely-only", "zero-rank"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s reached the map despite incomplete coverage", id)
		}
	}
}

// TestSecondOpinionsAreAssignedByNameNotOrder is the regression.
//
// The caller used to keep whichever second opinion came last in the registry,
// which is how Ciely came to be loaded and thrown away: he is registered
// first, and the two FantasyPros sharp subsets that follow overwrote him. The
// FP column landing on the right source was an accident of ordering that
// nothing documented and nothing tested.
//
// Reversed here, because if selection ever goes back to being positional this
// is the arrangement that catches it.
func TestSecondOpinionsAreAssignedByNameNotOrder(t *testing.T) {
	ciely := draft.SecondOpinion{Name: cielySource, Rank: map[string]int{"p": 3}}
	sharp := draft.SecondOpinion{
		Name:  fpSharpSource,
		Rank:  map[string]int{"p": 9},
		Sharp: map[string]int{"p": 4},
	}
	other := draft.SecondOpinion{Name: "fantasypros-top10", Rank: map[string]int{"p": 7}}

	for _, order := range [][]draft.SecondOpinion{
		{ciely, other, sharp},
		{sharp, other, ciely},
		{other, ciely, sharp},
	} {
		fp, cielyRank := assignSecondOpinions(order)
		if fp.Name != fpSharpSource {
			t.Errorf("FP column filled from %q, want %q", fp.Name, fpSharpSource)
		}
		if fp.Sharp["p"] != 4 {
			t.Errorf("FP sharp move = %d, want the sharp subset's 4", fp.Sharp["p"])
		}
		if cielyRank["p"] != 3 {
			t.Errorf("ciely rank = %d, want 3 — his ordering was dropped", cielyRank["p"])
		}
	}
}

// The names above are strings, so a rename in the registry would silently
// switch both signals off rather than fail to compile. This is the guard.
func TestTheSourceNamesExistInTheRegistry(t *testing.T) {
	have := map[string]bool{}
	for _, s := range draft.ProjectionSources {
		have[s.Name] = true
	}
	for _, want := range []string{fpSharpSource, cielySource} {
		if !have[want] {
			t.Errorf("%q is not a registered projection source, so the board reads nothing from it", want)
		}
	}
}

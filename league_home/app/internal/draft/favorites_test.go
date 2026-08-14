package draft

import (
	"strings"
	"testing"
)

// favLeans builds a Leans map from a parsed YAML body, failing the test on a
// parse error.
func favLeans(t *testing.T, body string) Leans {
	t.Helper()
	l, _, err := ParseLeansYAML(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseLeansYAML: %v", err)
	}
	return l
}

func TestParseFavoritesLayerOnReads(t *testing.T) {
	l := favLeans(t, "up:\n  - Chase Brown\nfavorites:\n  - Chase Brown\n  - Nick Singleton\n")

	brown := l[normalizeName("Chase Brown")]
	if brown.Lean != LeanUp || !brown.Favorite {
		t.Errorf("Chase Brown = {%q, fav %v}, want {up, fav true}", brown.Lean, brown.Favorite)
	}
	// A favorite with no read of his own gets a bare entry so the tag and the
	// name-check still apply.
	singleton := l[normalizeName("Nick Singleton")]
	if singleton.Lean != "" || !singleton.Favorite {
		t.Errorf("Nick Singleton = {%q, fav %v}, want {'', fav true}", singleton.Lean, singleton.Favorite)
	}
}

func TestFavoriteStretchesValue(t *testing.T) {
	l := favLeans(t, "favorites:\n  - Chase Brown\n")
	// value 20, swing 20 -> 20 + round(20*0.25) + 2 = 27.
	bid, _, rule := l.WalkAway("Chase Brown", 20, 50, 20)
	if bid != 27 || rule != RuleFavorite {
		t.Errorf("favorite bid = (%d, %s), want (27, favorite)", bid, rule)
	}
}

func TestFavoriteStacksOnConviction(t *testing.T) {
	l := favLeans(t, "up:\n  - Chase Brown\nfavorites:\n  - Chase Brown\n")
	// conviction first: round(20*1.15)=23, then +round(20*0.25)+2 = 30.
	bid, _, rule := l.WalkAway("Chase Brown", 20, 50, 20)
	if bid != 30 || rule != RuleFavorite {
		t.Errorf("favorite+up bid = (%d, %s), want (30, favorite)", bid, rule)
	}
}

func TestFavoriteCappedByRiskCeiling(t *testing.T) {
	l := favLeans(t, "favorites:\n  - Chase Brown\n")
	// The stretch would reach 27, but the risk ceiling is 24.
	bid, _, rule := l.WalkAway("Chase Brown", 20, 24, 20)
	if bid != 24 || rule != RuleFavorite {
		t.Errorf("capped favorite = (%d, %s), want (24, favorite)", bid, rule)
	}
}

func TestFavoriteBlockedWhenCeilingAtValue(t *testing.T) {
	l := favLeans(t, "favorites:\n  - Chase Brown\n")
	// Ceiling equals value, so the stretch cannot raise the number: no favorite.
	bid, _, rule := l.WalkAway("Chase Brown", 20, 20, 20)
	if bid != 20 || rule != RuleValue {
		t.Errorf("favorite at ceiling = (%d, %s), want (20, value)", bid, rule)
	}
}

func TestFavoriteDoesNotOverrideMustHave(t *testing.T) {
	l := favLeans(t, "must:\n  - Chase Brown\nfavorites:\n  - Chase Brown\n")
	_, _, rule := l.WalkAway("Chase Brown", 20, 50, 20)
	if rule != RuleMustHave {
		t.Errorf("must + favorite rule = %s, want must-have", rule)
	}
}

func TestMustHavesExcludesFavorites(t *testing.T) {
	l := favLeans(t, "must:\n  - Kept Guy\nfavorites:\n  - Stretch Guy\n")
	bids := map[string]int{normalizeName("Kept Guy"): 40, normalizeName("Stretch Guy"): 30}
	mh := l.MustHaves(200, 10, bids)
	if len(mh.Players) != 1 {
		t.Fatalf("MustHaves counted %d players, want 1 (favorites are not commitments)", len(mh.Players))
	}
	if mh.Players[0].Player != "Kept Guy" {
		t.Errorf("MustHaves player = %q, want Kept Guy", mh.Players[0].Player)
	}
	if mh.Committed != 40 {
		t.Errorf("committed = %d, want 40 (favorite not added)", mh.Committed)
	}
}

func TestFavoritesRoundTripYAML(t *testing.T) {
	src := "up:\n  - Chase Brown\nfavorites:\n  - Chase Brown\n  - Nick Singleton\n"
	l := favLeans(t, src)

	rows := make([]PlayerLean, 0, len(l))
	for _, pl := range l {
		rows = append(rows, pl)
	}
	doc, err := FormatLeansYAML(rows, nil)
	if err != nil {
		t.Fatalf("FormatLeansYAML: %v", err)
	}

	l2 := favLeans(t, string(doc))
	if !l2[normalizeName("Chase Brown")].Favorite || l2[normalizeName("Chase Brown")].Lean != LeanUp {
		t.Errorf("round trip lost Chase Brown's up+favorite: %+v", l2[normalizeName("Chase Brown")])
	}
	if !l2[normalizeName("Nick Singleton")].Favorite {
		t.Errorf("round trip lost Nick Singleton's favorite")
	}
	// A favorite-only player must not also be written under undecided.
	if strings.Contains(string(doc), "undecided") {
		t.Errorf("favorite-only player leaked into undecided:\n%s", doc)
	}
}

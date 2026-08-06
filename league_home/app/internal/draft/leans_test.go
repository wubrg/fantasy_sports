package draft

import (
	"path/filepath"
	"strings"
	"testing"
)

func leansFrom(t *testing.T, csv string) Leans {
	t.Helper()
	got, err := ParseLeans(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseLeans(t *testing.T) {
	got := leansFrom(t, "player,lean,cap,note\n"+
		"Ashton Jeanty,must,48,\"Kubiak scheme change\"\n"+
		"Chase Brown,up,,believe the OL fix\n"+
		"Kyle Pitts,dnd,,never again\n")
	if len(got) != 3 {
		t.Fatalf("expected 3 leans, got %d", len(got))
	}
	if pl := got[normalizeName("Ashton Jeanty")]; pl.Lean != LeanMust || pl.Cap != 48 {
		t.Errorf("must-have parsed wrong: %+v", pl)
	}
	// Apostrophes and punctuation must not defeat the lookup.
	got = leansFrom(t, "player,lean\nDe'Von Achane,down\n")
	if pl, ok := got[normalizeName("DeVon Achane")]; !ok || pl.Lean != LeanDown {
		t.Errorf("normalized lookup failed: %+v %v", pl, ok)
	}
}

// TestMustHaveNeedsNoCap — a hand-picked ceiling is a guess with a decimal
// point. Leaving it blank lets the risk math set the number instead.
func TestMustHaveNeedsNoCap(t *testing.T) {
	got := leansFrom(t, "player,lean,cap\nAshton Jeanty,must,\n")
	if pl := got[normalizeName("Ashton Jeanty")]; pl.Lean != LeanMust || pl.Cap != 0 {
		t.Errorf("expected an uncapped must-have, got %+v", pl)
	}
	for _, bad := range []string{"0", "-5", "lots"} {
		if _, err := ParseLeans(strings.NewReader("player,lean,cap\nAshton Jeanty,must," + bad + "\n")); err == nil {
			t.Errorf("expected an error for cap %q", bad)
		}
	}
}

func TestParseLeansRejectsUnknownLean(t *testing.T) {
	_, err := ParseLeans(strings.NewReader("player,lean\nCam Skattebo,maybe\n"))
	if err == nil {
		t.Fatal("expected an error for an unrecognized lean")
	}
	// The file is hand-edited, so the message has to list the options.
	for _, want := range []string{"must", "up", "down", "dnd"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestLoadLeansMissingFileIsNotAnError(t *testing.T) {
	got, err := LoadLeans(filepath.Join(t.TempDir(), "none.csv"))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty and no error", got, err)
	}
}

// TestMustHaveOverridesValueRatherThanScalingIt is the Jeanty case and the
// reason the old percentage design failed: the model says $24, the market
// pays $43, and no nudge to $24 ever wins him. A must-have states the
// number outright.
func TestMustHaveOverridesValueRatherThanScalingIt(t *testing.T) {
	leans := leansFrom(t, "player,lean,cap\nAshton Jeanty,must,48\n")
	bid, _, rule := leans.WalkAway("Ashton Jeanty", 24, 51)
	if bid != 48 {
		t.Errorf("walk-away = %d, want the declared cap of 48 tightening the $51 recommendation", bid)
	}
	if rule != RuleMustHave {
		t.Errorf("rule = %q, want must-have so the board can say why", rule)
	}
}

// TestCapOnlyBindsUpward — a cap is a ceiling on overpaying, not an
// instruction to underbid a player the model already rates higher.
func TestCapOnlyBindsUpward(t *testing.T) {
	leans := leansFrom(t, "player,lean,cap\nJahmyr Gibbs,must,40\n")
	bid, _, rule := leans.WalkAway("Jahmyr Gibbs", 63, 40)
	if bid != 63 {
		t.Errorf("walk-away = %d, want the model's 63", bid)
	}
	if rule != RuleValue {
		t.Errorf("rule = %q, want value when the cap does not bind", rule)
	}
}

// TestDNDIsAbsolute — do-not-draft is a refusal, not a discount. Quoting a
// number for a player you have sworn off is how you end up owning him.
func TestDNDIsAbsolute(t *testing.T) {
	leans := leansFrom(t, "player,lean\nKyle Pitts,dnd\n")
	for _, value := range []int{1, 25, 200} {
		bid, pl, rule := leans.WalkAway("Kyle Pitts", value, 50)
		if bid != 0 || rule != RuleRefused {
			t.Errorf("value %d: got bid %d rule %q, want 0/do-not-draft", value, bid, rule)
		}
		if pl.Marker() != "DND" {
			t.Errorf("marker = %q, want DND", pl.Marker())
		}
	}
}

func TestConvictionScalesValue(t *testing.T) {
	leans := leansFrom(t, "player,lean\nChase Brown,up\nSaquon Barkley,down\n")
	if bid, _, rule := leans.WalkAway("Chase Brown", 40, 60); bid != 46 || rule != RuleConviction {
		t.Errorf("up: got %d/%q, want 46/conviction", bid, rule)
	}
	if bid, _, _ := leans.WalkAway("Saquon Barkley", 40, 60); bid != 34 {
		t.Errorf("down: got %d, want 34", bid)
	}
	// Conviction must never price below the auction floor; dnd is the way
	// to say no.
	if bid, _, _ := leans.WalkAway("Saquon Barkley", 1, 60); bid != 1 {
		t.Errorf("floor: got %d, want 1", bid)
	}
}

func TestUnlistedPlayersAreUntouched(t *testing.T) {
	bid, _, rule := Leans{}.WalkAway("Nobody", 37, 50)
	if bid != 37 || rule != RuleValue {
		t.Errorf("got %d/%q, want 37/value", bid, rule)
	}
}

// TestMustHavesReportTheirTotalCost is the honesty mechanism: getting your
// guys has to show up as a budget decision.
func TestMustHavesReportTheirTotalCost(t *testing.T) {
	leans := leansFrom(t, "player,lean,cap\n"+
		"Ashton Jeanty,must,48\nKenneth Walker,must,40\nChase Brown,up,\n")

	got := leans.MustHaves(130, 12, map[string]int{
		normalizeName("Ashton Jeanty"): 48, normalizeName("Kenneth Walker"): 40,
	})
	if len(got.Players) != 2 {
		t.Fatalf("expected 2 must-haves, got %d", len(got.Players))
	}
	if got.Committed != 88 {
		t.Errorf("committed = %d, want 88", got.Committed)
	}
	if got.Remaining != 42 {
		t.Errorf("remaining = %d, want 42", got.Remaining)
	}
	if got.SlotsLeft != 10 {
		t.Errorf("slots left = %d, want 10", got.SlotsLeft)
	}
	if got.PerSlot() != 4.2 {
		t.Errorf("per slot = %v, want 4.2", got.PerSlot())
	}
	// Sorted by cap so the biggest commitment reads first.
	if got.Players[0].Cap != 48 {
		t.Errorf("expected the largest cap first, got %d", got.Players[0].Cap)
	}
}

// TestOvercommittedCatchesABudgetYouCannotFieldARosterWith
func TestOvercommittedCatchesABudgetYouCannotFieldARosterWith(t *testing.T) {
	sane := leansFrom(t, "player,lean,cap\nAshton Jeanty,must,48\n")
	if sane.MustHaves(130, 12, map[string]int{normalizeName("Ashton Jeanty"): 48}).Overcommitted() {
		t.Error("one $48 must-have out of $130 is not overcommitted")
	}

	greedy := leansFrom(t, "player,lean,cap\n"+
		"A,must,48\nB,must,40\nC,must,35\n")
	got := greedy.MustHaves(130, 12, map[string]int{
		normalizeName("A"): 48, normalizeName("B"): 40, normalizeName("C"): 35,
	})
	if !got.Overcommitted() {
		t.Errorf("$123 of $130 across 3 must-haves should flag: per slot $%.2f", got.PerSlot())
	}
}

// TestShippedMyGuysFileParses guards the file in the repo, since a typo
// there would surface at 8pm on draft night.
func TestShippedMyGuysFileParses(t *testing.T) {
	got, err := LoadLeans("data/my-guys.csv")
	if err != nil {
		t.Fatalf("the shipped my-guys.csv must always parse: %v", err)
	}
	for _, pl := range got {
		if pl.Cap < 0 {
			t.Errorf("%s has a negative cap", pl.Player)
		}
	}
}

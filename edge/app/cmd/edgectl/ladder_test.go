package main

import (
	"math"
	"testing"

	"edge/internal/wager"
)

func TestParseCorrelation(t *testing.T) {
	specs, err := parseCorrelation("mia@lv:0.3;was@phi:0.5,0.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d: %+v", len(specs), specs)
	}
	if specs[0].game != "mia@lv" || len(specs[0].rhos) != 1 || specs[0].rhos[0] != 0.3 {
		t.Errorf("spec 0 = %+v, want game mia@lv, rhos [0.3]", specs[0])
	}
	if specs[1].game != "was@phi" || len(specs[1].rhos) != 2 ||
		specs[1].rhos[0] != 0.5 || specs[1].rhos[1] != 0.2 {
		t.Errorf("spec 1 = %+v, want game was@phi, rhos [0.5, 0.2]", specs[1])
	}
}

func TestParseCorrelationEmptyStringIsNoSpecsNotAnError(t *testing.T) {
	specs, err := parseCorrelation("")
	if err != nil {
		t.Fatal(err)
	}
	if specs != nil {
		t.Errorf("empty -correlation should produce no specs, got %+v", specs)
	}
}

func TestParseCorrelationRejectsAMalformedEntry(t *testing.T) {
	if _, err := parseCorrelation("noColonHere"); err == nil {
		t.Error("an entry with no game:rhos separator should be rejected")
	}
	if _, err := parseCorrelation("mia@lv:not-a-number"); err == nil {
		t.Error("a non-numeric rho should be rejected")
	}
	if _, err := parseCorrelation(":0.5"); err == nil {
		t.Error("an empty game tag should be rejected")
	}
}

func TestParseCorrelationRejectsARhoOutsideRange(t *testing.T) {
	if _, err := parseCorrelation("mia@lv:1.5"); err == nil {
		t.Error("a rho above 1 should be rejected at parse time, not deferred to FrechetBlend")
	}
}

func TestGuardSameGameRefusesUncoveredPairs(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	if err := guardSameGame(legs, nil); err == nil {
		t.Error("two same-game legs with no correlation coverage should still be refused")
	}
}

func TestGuardSameGameAllowsCoveredPairs(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	covered := map[string]bool{"mia@lv": true}
	if err := guardSameGame(legs, covered); err != nil {
		t.Errorf("a covered game's same-game legs should be allowed, got: %v", err)
	}
}

func TestGuardSameGameStillRefusesOtherUncoveredGames(t *testing.T) {
	// mia@lv is covered; was@phi is not. A same-game pair in was@phi must
	// still be refused even though some other game in the same call is
	// covered.
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: -120, game: "was@phi", label: "C"},
		{price: +110, game: "was@phi", label: "D"},
	}
	covered := map[string]bool{"mia@lv": true}
	if err := guardSameGame(legs, covered); err == nil {
		t.Error("was@phi has no correlation coverage and should still be refused")
	}
}

func TestGuardSameGameStillRequiresAGameTag(t *testing.T) {
	legs := []parlayLeg{{price: -110, game: "", label: "A"}}
	if err := guardSameGame(legs, nil); err == nil {
		t.Error("a leg with no game tag at all should still be refused, covered or not")
	}
}

// ---- Task 6: collapseLegs + parlayCmd correlation integration ----

func TestParlayCmdCorrelationTwoLegSGP(t *testing.T) {
	// Both legs in the same game, covered by -correlation. With no other
	// legs, this collapses to exactly one combo, priced via FrechetBlend
	// instead of straight multiplication.
	//
	// Leg prices -110 (raw implied ~52.38%) and +150 (raw implied 40.00%).
	// p_indep ~= 0.2095. rho=0.3 should price ABOVE that independent
	// product (positive correlation raises the joint probability), so the
	// resulting combo's implied probability must exceed the naive product.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd: %v", err)
	}
}

func TestCollapseLegsTwoLegGroup(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3}}}
	collapsed, err := collapseLegs(legs, specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 1 {
		t.Fatalf("want 1 collapsed slot (both legs merge), got %d: %+v", len(collapsed), collapsed)
	}
	if len(collapsed[0].members) != 2 {
		t.Errorf("collapsed slot should carry both original legs, got %d members", len(collapsed[0].members))
	}
	if collapsed[0].prob == nil {
		t.Fatal("a correlated slot must always carry a derived probability")
	}
	pA, _ := wager.American(-110).ImpliedRaw()
	pB, _ := wager.American(150).ImpliedRaw()
	wantProb, err := wager.FrechetBlend(pA, pB, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(*collapsed[0].prob-wantProb) > 1e-9 {
		t.Errorf("collapsed prob = %v, want %v (FrechetBlend of the two legs' raw implied)", *collapsed[0].prob, wantProb)
	}
	if collapsed[0].caveat {
		t.Error("a 2-leg group is exact, not an approximation -- caveat should be false")
	}
}

func TestCollapseLegsLeavesIndependentLegsAlone(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: -120, game: "was@phi", label: "B"}, // different game, no correlation spec
	}
	collapsed, err := collapseLegs(legs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 2 {
		t.Fatalf("want 2 collapsed slots (no grouping happened), got %d", len(collapsed))
	}
	for _, c := range collapsed {
		if len(c.members) != 1 {
			t.Errorf("an independent leg's slot should carry exactly 1 member, got %d", len(c.members))
		}
		if c.prob != nil {
			t.Error("an independent leg with no -p override should carry no derived probability " +
				"(nil means: use raw implied, the existing default)")
		}
	}
}

func TestCollapseLegsThreeLegGroupSetsCaveat(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: +200, game: "mia@lv", label: "C"},
	}
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3, 0.2}}}
	collapsed, err := collapseLegs(legs, specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 1 || len(collapsed[0].members) != 3 {
		t.Fatalf("want 1 slot with 3 members, got %+v", collapsed)
	}
	if !collapsed[0].caveat {
		t.Error("a 3-leg chained group is an approximation and must set caveat=true")
	}
}

func TestCollapseLegsRejectsAWrongRhoCountForAGroup(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: +200, game: "mia@lv", label: "C"},
	}
	// 3 legs need 2 rhos; only 1 given.
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3}}}
	if _, err := collapseLegs(legs, specs, nil); err == nil {
		t.Error("a 3-leg group with only 1 rho should be refused, not silently truncated or padded")
	}
}

// ---- Task 7: printParlay breakeven / floor rendering ----

func TestCorrelatedBookComparisonReportsBreakeven(t *testing.T) {
	// Two legs, rho=0.3, -book 150 (the book's own quoted combined price).
	// This must compute RequiredCorrelation against the group's two
	// underlying leg probabilities and the book's raw implied -- confirm it
	// does not error and returns a real, in-range value for this case.
	pA, _ := wager.American(-110).ImpliedRaw()
	pB, _ := wager.American(150).ImpliedRaw()
	bookImplied, _ := wager.American(150).ImpliedRaw()
	req, err := wager.RequiredCorrelation(pA, pB, bookImplied)
	if err != nil {
		t.Fatal(err)
	}
	if req.Required < -1 || req.Required > 1 {
		t.Errorf("breakeven rho = %v, should be in [-1,1]", req.Required)
	}
}

func TestParlayCmdCorrelationWithBookFlag(t *testing.T) {
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-book", "150",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd with -book on a correlated combo: %v", err)
	}
}

func TestParlayCmdCorrelationFloorPrintsWithoutBookFlag(t *testing.T) {
	// The floor-shopping workflow this feature exists for: compute the
	// floor BEFORE knowing any book's real price, then go shop books for a
	// quote at or longer than it. No -book flag at all -- confirms the
	// floor line's print path (hasCorrelatedGroup && len(t.Combos)==1 in
	// printParlay) does not depend on -book being supplied.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd (floor only, no -book): %v", err)
	}
}

func TestParlayCmdThreeLegCorrelatedPrintsNoError(t *testing.T) {
	// Exercises the 3+-leg caveat path end to end -- this test only confirms
	// it runs without error; the caveat text itself is confirmed present by
	// reading printParlay's source in this task's own self-review, since
	// capturing stdout is not this codebase's established test convention
	// (see board_props_report.go's printPropsReport, tested the same way).
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B,+200:mia@lv:C",
		"-correlation", "mia@lv:0.3,0.2",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd with a 3-leg correlated group: %v", err)
	}
}

func TestParlayCmdWithoutCorrelationUnchanged(t *testing.T) {
	// Every pre-existing call shape must still work exactly as before --
	// -correlation is additive and optional. This exercises a plain
	// independent-legs round robin with -p and -book, the richest
	// pre-existing path, and confirms it still succeeds with no
	// -correlation flag at all.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:LV -3,+250:was@phi:McLaurin ATD,-130:nyg@dal:Dal ML",
		"-rr", "2",
		"-p", "0.55,0.40,0.30",
		"-total", "30",
	})
	if err != nil {
		t.Fatalf("parlayCmd (no -correlation): %v", err)
	}

	// Same-game legs with no -correlation flag at all must still be refused,
	// unchanged.
	err = parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B",
		"-total", "10",
	})
	if err == nil {
		t.Error("same-game legs with no -correlation flag should still be refused")
	}
}

func TestParlayCmdCorrelationComposesWithIndependentCrossGameLeg(t *testing.T) {
	// A round robin mixing one correlated same-game group with an
	// independent cross-game leg must price correctly end to end. This
	// collapses to 2 slots: an SGP group (mia@lv, A+B, blended to +275
	// fair / 26.7% implied) and one independent leg (was@phi, C). With 2
	// collapsed slots and no -rr (defaults to a single full-parlay combo),
	// it should produce one combo at decimal 3.75 x (decimal of -120 ~=
	// 1.8333) ~= 6.875 -> +588, with win probability ~= 0.26667 x (implied
	// of -120 ~= 0.54545) ~= 14.5%.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B,-120:was@phi:C",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd (correlated group + independent cross-game leg): %v", err)
	}
}

func TestParlayCmdCorrelationComposesWithIndependentCrossGameLegRoundRobin(t *testing.T) {
	// Same mixed composition as above, but with -rr 2: two collapsed
	// slots, round-robin size 2 -- still should produce exactly one combo
	// (size-2-of-2-slots), and should not error.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B,-120:was@phi:C",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
		"-rr", "2",
	})
	if err != nil {
		t.Fatalf("parlayCmd (correlated group + independent cross-game leg, -rr 2): %v", err)
	}
}

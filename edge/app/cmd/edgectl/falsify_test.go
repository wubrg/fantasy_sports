package main

import (
	"strings"
	"testing"
)

func testGame() packGame {
	pf := struct {
		SuccessRatePrior float64 `json:"success_rate_prior"`
		OffensePrior     float64 `json:"offense_prior"`
		PriorGames       int     `json:"prior_games"`
	}{SuccessRatePrior: 0.4512, OffensePrior: -1.2175, PriorGames: 4}
	return packGame{
		GameID: "2026_01_DEN_KC", Away: "DEN", Home: "KC",
		TotalLine: f64(47.5), SpreadLine: f64(3.5),
		Teams: map[string]packTeam{
			"KC":  {PriorForm: &pf},
			"DEN": {},
		},
	}
}

func check(t *testing.T, claims ...string) falsifyResult {
	t.Helper()
	g := testGame()
	return falsifyPrediction(forecast{Claims: claims}, g, g.Home, g.Away)
}

// TestFalsifierCatchesAContradictionOfTheFactsSupplied. This is the whole point:
// a forecaster restating a number it was handed, wrongly, is not a difference of
// opinion.
func TestFalsifierCatchesAContradictionOfTheFactsSupplied(t *testing.T) {
	for _, tc := range []struct{ name, claim, want string }{
		{"wrong total", "market: DEN@KC — the total is 51.5", "posted total"},
		{"wrong spread", "market: DEN@KC — KC laying 9.5", "posted spread"},
		{"wrong form", "form: KC — prior success rate .390", "prior success rate"},
		{"wrong prior games", "form: KC — 11 prior games", "prior games"},
		{"home and away swapped", "schedule: DEN — at home in prime time", "is at home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := check(t, tc.claim)
			if r.Reason == "" {
				t.Fatalf("claim %q was not falsified", tc.claim)
			}
			if !strings.Contains(r.Reason, tc.want) {
				t.Errorf("reason %q does not mention %q", r.Reason, tc.want)
			}
		})
	}
}

// TestFalsifierDoesNotAccuseWrongly is the more important half. A false
// accusation voids a real prediction and biases the survivor set, so the rule
// errs toward letting things through.
func TestFalsifierDoesNotAccuseWrongly(t *testing.T) {
	for _, tc := range []struct{ name, claim string }{
		{"exact total", "market: DEN@KC — the total is 47.5"},
		{"spread from the other side", "market: DEN@KC — DEN getting 3.5"},
		{"spread as stated", "market: DEN@KC — spread 3.5"},
		{"form rounded to three places", "form: KC — prior success rate .451"},
		{"form rounded to two places", "form: KC — success rate around .45"},
		{"negative proe restated", "form: KC — offence PROE -1.2175"},
		{"prior games", "form: KC — four games in, prior_games 4"},
		{"no number at all", "form: KC — the offence has been efficient lately"},
		{"correct home", "schedule: KC — at home"},
		{"correct away", "schedule: DEN — on the road"},
		{"unverifiable", "personnel: KC — new coordinator favours the quick game"},
		{"narrative", "narrative: KC — plays up to good opponents"},
		{"form for a team with no form in the pack", "form: DEN — success rate .48"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r := check(t, tc.claim); r.Reason != "" {
				t.Errorf("claim %q was wrongly falsified: %s", tc.claim, r.Reason)
			}
		})
	}
}

// A prediction resting only on claims nothing can adjudicate is not wrong. It
// is unauditable, which is a different thing and is counted separately.
func TestFalsifierSeparatesUnverifiableFromFalse(t *testing.T) {
	r := check(t,
		"personnel: KC — new OC",
		"narrative: KC — bounce-back spot",
	)
	if r.Reason != "" {
		t.Errorf("unverifiable claims were treated as false: %s", r.Reason)
	}
	if r.Unverifiable != 2 || r.Checked != 0 {
		t.Errorf("unverifiable=%d checked=%d, want 2 and 0", r.Unverifiable, r.Checked)
	}
	if !r.OnlyUnverifiable(2) {
		t.Error("a prediction resting only on unverifiable claims was not flagged as such")
	}

	mixed := check(t, "personnel: KC — new OC", "market: DEN@KC — total 47.5")
	if mixed.OnlyUnverifiable(2) {
		t.Error("a prediction with one checkable claim was flagged as unauditable")
	}
}

// usage and injury are checkable in principle and have no data for a season
// that has not started. They must be counted as deferred rather than silently
// passed — a checker that approves everything because its data is missing is
// worse than no checker.
func TestFalsifierDefersRatherThanSilentlyPassing(t *testing.T) {
	r := check(t,
		"injury: KC — WR1 ruled out Friday",
		"usage: KC — 41 attempts in week 3",
	)
	if r.Deferred != 2 {
		t.Errorf("deferred=%d, want 2", r.Deferred)
	}
	if r.Checked != 0 {
		t.Errorf("checked=%d — these must not count as checked", r.Checked)
	}
	if r.Reason != "" {
		t.Errorf("a deferred claim was falsified: %s", r.Reason)
	}
}

func TestFalsifierCountsUntypedClaims(t *testing.T) {
	r := check(t, "the Chiefs look good", "market: DEN@KC — total 47.5")
	if r.Untyped != 1 {
		t.Errorf("untyped=%d, want 1", r.Untyped)
	}
	if r.Checked != 1 {
		t.Errorf("checked=%d, want 1", r.Checked)
	}
}

// TestFalsifierRegressionsFromReview pins the exact cases the 2026-09-01 review's
// harness broke on, so they cannot silently come back.
func TestFalsifierRegressionsFromReview(t *testing.T) {
	// A true, concrete claim whose number belongs to no pack quantity is
	// unverifiable, not false. The old checker falsified any number matching
	// nothing in a three-number bag and rejected exactly these.
	survives := []string{
		"form: KC — averaged 27 points over its last three",
		"form: KC — 4 giveaways in three games",
		"market: DEN@KC — total 47.5, and it has moved 6 points since open",
		"schedule: DEN — away from home",
		"schedule: DEN — not at home",
	}
	for _, cl := range survives {
		if r := check(t, cl); r.Reason != "" {
			t.Errorf("true/unverifiable claim %q was falsified: %s", cl, r.Reason)
		}
	}

	// One true number no longer immunises an invented one beside it: the games
	// count is wrong even though the success rate is right.
	r := check(t, "form: KC — success rate .451 but only 11 games in")
	if r.Reason == "" {
		t.Error("a wrong named quantity beside a right one was not caught")
	}

	// An ASCII-hyphen claim is typed and adjudicated, not left untyped.
	h := check(t, "schedule: KC - at home")
	if h.Untyped != 0 {
		t.Errorf("an ASCII-hyphen claim was left untyped (untyped=%d)", h.Untyped)
	}
	if h.Checked != 1 {
		t.Errorf("an ASCII-hyphen schedule claim was not checked (checked=%d)", h.Checked)
	}

	// A genuinely wrong home/away claim still fires.
	if w := check(t, "schedule: DEN -- at home"); w.Reason == "" {
		t.Error("a false home claim on the away team was not caught")
	}
}

func TestParseClaimShape(t *testing.T) {
	c, ok := parseClaim("form: KC — prior success rate .451")
	if !ok {
		t.Fatal("a well-formed claim did not parse")
	}
	if c.kind != "form" || c.subject != "KC" || !strings.Contains(c.assertion, ".451") {
		t.Errorf("parsed as %+v", c)
	}
	// An en dash is accepted too; a model will produce either.
	if _, ok := parseClaim("market: DEN@KC – total 47.5"); !ok {
		t.Error("an en-dash separator did not parse")
	}
	if _, ok := parseClaim("no separator here"); ok {
		t.Error("an untyped string parsed as a claim")
	}
}

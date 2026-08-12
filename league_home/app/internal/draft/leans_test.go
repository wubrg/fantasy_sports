package draft

import (
	"os"
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
//
// Asserts it is non-empty as well as valid: a missing lean file is not an
// error by design, so a rename would otherwise leave this passing over
// nothing at all.
func TestShippedMyGuysFileParses(t *testing.T) {
	got, err := LoadLeans("data/leans/mine.csv")
	if err != nil {
		t.Fatalf("the shipped mine.csv must always parse: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("data/leans/mine.csv read as empty — has it moved?")
	}
	for _, pl := range got {
		if pl.Cap < 0 {
			t.Errorf("%s has a negative cap", pl.Player)
		}
	}
}

// TestWriteLeansRoundTrips — what is written has to be what ParseLeans
// reads, or a read set at the board comes back as something else on restart.
func TestWriteLeansRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.csv")
	in := Leans{
		normalizeName("Ashton Jeanty"): {Player: "Ashton Jeanty", Lean: LeanMust, Cap: 48, Note: "Kubiak"},
		normalizeName("Kyle Pitts"):    {Player: "Kyle Pitts", Lean: LeanDND},
		normalizeName("De'Von Achane"): {Player: "De'Von Achane", Lean: LeanUp},
	}
	if err := WriteLeans(path, in); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(in) {
		t.Fatalf("wrote %d reads, read back %d", len(in), len(back))
	}
	for key, want := range in {
		got, ok := back[key]
		if !ok {
			t.Fatalf("%s did not survive", want.Player)
		}
		if got.Lean != want.Lean || got.Cap != want.Cap || got.Note != want.Note {
			t.Errorf("%s came back as %+v, want %s/%d/%q",
				want.Player, got, want.Lean, want.Cap, want.Note)
		}
	}
}

// TestWriteLeansDoesNotMarkTheFileGenerated is the guard that protects
// hand-written reads.
//
// isGenerated decides whether `leans -generate` may overwrite a file. If a
// board-saved mine.csv carried the marker, the next generation would be
// entitled to destroy every read in it.
func TestWriteLeansDoesNotMarkTheFileGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.csv")
	if err := WriteLeans(path, Leans{normalizeName("X"): {Player: "X", Lean: LeanUp}}); err != nil {
		t.Fatal(err)
	}
	if isGenerated(path) {
		t.Error("a board-saved lean file reads as generated, so -generate could overwrite it")
	}
}

// TestWriteLeansLeavesNoTemporaryFiles — the write goes via a temp file in
// the same directory so a crash cannot truncate the real one. That temp must
// not survive, or the leans directory fills with debris that LoadLeanSet
// would eventually try to read.
func TestWriteLeansLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.csv")
	if err := WriteLeans(path, Leans{normalizeName("X"): {Player: "X", Lean: LeanUp}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "mine.csv" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only mine.csv", names)
	}
}

// TestWriteLeansIsStable — two saves of the same reads produce identical
// bytes, so the file does not churn in git every time the board touches it.
func TestWriteLeansIsStable(t *testing.T) {
	dir := t.TempDir()
	in := Leans{
		normalizeName("B Player"): {Player: "B Player", Lean: LeanUp},
		normalizeName("A Player"): {Player: "A Player", Lean: LeanDown},
	}
	first := filepath.Join(dir, "one.csv")
	second := filepath.Join(dir, "two.csv")
	if err := WriteLeans(first, in); err != nil {
		t.Fatal(err)
	}
	if err := WriteLeans(second, in); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) != string(b) {
		t.Errorf("two writes differ:\n%s\n---\n%s", a, b)
	}
}

// TestANoneReadSilencesLowerPrecedenceSets — an explicit absence has to beat
// a set that does have an opinion, and then get out of the way rather than
// showing up as a read of its own.
func TestANoneReadSilencesLowerPrecedenceSets(t *testing.T) {
	mine := LeanSet{Name: "mine", Leans: leansFrom(t,
		"player,lean,cap,note\nBreece Hall,none,,\n")}
	menton := LeanSet{Name: "menton", Leans: leansFrom(t,
		"player,lean,cap,note\nBreece Hall,down,,zero traits\n")}

	got := MergeLeans(mine, menton)
	if pl, still := got[normalizeName("Breece Hall")]; still {
		t.Errorf("a silenced player is still on the board as %q", pl.Lean)
	}

	// And with nothing to silence, none is simply absent rather than a read.
	alone := MergeLeans(mine)
	if _, still := alone[normalizeName("Breece Hall")]; still {
		t.Error("a lone none read should leave nothing behind")
	}
}

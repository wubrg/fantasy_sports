package draft

import (
	"strings"
	"testing"
)

func yamlLeansFrom(t *testing.T, doc string) (Leans, []string) {
	t.Helper()
	got, undecided, err := ParseLeansYAML(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return got, undecided
}

// TestYAMLGroupsPlayersByLean — the shape exists so adding a read is one
// line under the heading you already believe, which is what makes the file
// editable on a phone.
func TestYAMLGroupsPlayersByLean(t *testing.T) {
	got, _ := yamlLeansFrom(t, `
must:
  - Ashton Jeanty
up:
  - Chase Brown
  - A.J. Brown
down:
  - Kyle Pitts
dnd:
  - Cam Akers
`)
	if len(got) != 5 {
		t.Fatalf("expected 5 reads, got %d: %+v", len(got), got)
	}
	for name, want := range map[string]Lean{
		"Ashton Jeanty": LeanMust,
		"Chase Brown":   LeanUp,
		"A.J. Brown":    LeanUp,
		"Kyle Pitts":    LeanDown,
		"Cam Akers":     LeanDND,
	} {
		if pl := got[normalizeName(name)]; pl.Lean != want {
			t.Errorf("%s: got %q, want %q", name, pl.Lean, want)
		}
	}
}

// TestYAMLCapsAndNotesAreSeparateSections — both are optional and rare, so
// they live apart rather than padding every line with empty fields.
func TestYAMLCapsAndNotesAreSeparateSections(t *testing.T) {
	got, _ := yamlLeansFrom(t, `
must:
  - Ashton Jeanty
up:
  - Chase Brown
caps:
  Ashton Jeanty: 48
notes:
  Ashton Jeanty: Kubiak scheme + OL fix
  Chase Brown: 8.4 rec FPG two years running
`)
	jeanty := got[normalizeName("Ashton Jeanty")]
	if jeanty.Cap != 48 || jeanty.Note != "Kubiak scheme + OL fix" {
		t.Errorf("cap/note did not attach: %+v", jeanty)
	}
	brown := got[normalizeName("Chase Brown")]
	if brown.Cap != 0 || brown.Note != "8.4 rec FPG two years running" {
		t.Errorf("note-only player parsed wrong: %+v", brown)
	}
}

// TestYAMLUndecidedIsAGroup — the blank-lean state from the CSV format
// survives the move, as a heading rather than an empty column.
func TestYAMLUndecidedIsAGroup(t *testing.T) {
	got, undecided := yamlLeansFrom(t, `
up:
  - Chase Brown
undecided:
  - Colston Loveland
  - Tucker Kraft
`)
	if len(got) != 1 {
		t.Errorf("undecided players should record no read, got %+v", got)
	}
	if len(undecided) != 2 || undecided[0] != "Colston Loveland" || undecided[1] != "Tucker Kraft" {
		t.Errorf("undecided should be reported in file order: %v", undecided)
	}
}

// TestYAMLRejectsAnUnknownHeading — the whole point of headings is that a
// misspelled one is catchable. Ignoring "ups:" would silently drop every
// read underneath it.
func TestYAMLRejectsAnUnknownHeading(t *testing.T) {
	_, _, err := ParseLeansYAML(strings.NewReader("ups:\n  - Chase Brown\n"))
	if err == nil {
		t.Fatal("expected an error for an unknown heading")
	}
	for _, want := range []string{"must", "up", "down", "dnd"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list the headings, missing %q: %v", want, err)
		}
	}
}

// TestYAMLRejectsCapOrNoteForAnUnlistedPlayer — a note under a name that
// appears in no group is the same silent no-op the CSV format allowed, and
// it is a typo every time.
func TestYAMLRejectsCapOrNoteForAnUnlistedPlayer(t *testing.T) {
	for _, doc := range []string{
		"up:\n  - Chase Brown\nnotes:\n  Chse Brown: typo\n",
		"up:\n  - Chase Brown\ncaps:\n  Chse Brown: 20\n",
	} {
		if _, _, err := ParseLeansYAML(strings.NewReader(doc)); err == nil {
			t.Errorf("expected an error for an unlisted player in:\n%s", doc)
		}
	}
}

// TestYAMLRejectsAPlayerInTwoGroups — one file cannot hold both sides of a
// disagreement; that is what a second lean set is for.
func TestYAMLRejectsAPlayerInTwoGroups(t *testing.T) {
	_, _, err := ParseLeansYAML(strings.NewReader("up:\n  - Chase Brown\ndown:\n  - Chase Brown\n"))
	if err == nil {
		t.Fatal("expected an error for a player in two groups")
	}
}

// TestYAMLCapMustBeRealMoney — same rule the CSV enforced.
func TestYAMLCapMustBeRealMoney(t *testing.T) {
	for _, bad := range []string{"0", "-5"} {
		doc := "must:\n  - Ashton Jeanty\ncaps:\n  Ashton Jeanty: " + bad + "\n"
		if _, _, err := ParseLeansYAML(strings.NewReader(doc)); err == nil {
			t.Errorf("expected an error for cap %q", bad)
		}
	}
}

// TestYAMLEmptyFileIsNotAnError — recording no reads is a legitimate state,
// exactly as it was for the CSV.
func TestYAMLEmptyFileIsNotAnError(t *testing.T) {
	got, undecided := yamlLeansFrom(t, "")
	if len(got) != 0 || len(undecided) != 0 {
		t.Errorf("empty file should yield nothing: %+v %v", got, undecided)
	}
}

// TestYAMLRoundTrip — conversion has to be lossless, or the changeover
// quietly costs you reads. Notes carrying commas, colons and quotes are the
// case CSV needed escaping for and the one most likely to break.
func TestYAMLRoundTrip(t *testing.T) {
	in := []PlayerLean{
		{Player: "Ashton Jeanty", Lean: LeanMust, Cap: 48,
			Note: `Kubiak scheme: 83% inside the 5, "the risk ceiling sets the bid"`},
		{Player: "Chase Brown", Lean: LeanUp, Note: "8.4 rec FPG, RB5 two years running"},
		{Player: "Ja'Kobi Lane", Lean: LeanUp},
		{Player: "Kyle Pitts", Lean: LeanDND},
	}
	doc, err := FormatLeansYAML(in, []string{"Colston Loveland"})
	if err != nil {
		t.Fatal(err)
	}
	got, undecided, err := ParseLeansYAML(strings.NewReader(string(doc)))
	if err != nil {
		t.Fatalf("re-reading what we wrote failed: %v\n%s", err, doc)
	}
	if len(got) != len(in) {
		t.Fatalf("wrote %d reads, read back %d:\n%s", len(in), len(got), doc)
	}
	for _, want := range in {
		back := got[normalizeName(want.Player)]
		if back.Lean != want.Lean || back.Cap != want.Cap || back.Note != want.Note {
			t.Errorf("%s round-tripped wrong:\n got %+v\nwant %+v\n%s",
				want.Player, back, want, doc)
		}
	}
	if len(undecided) != 1 || undecided[0] != "Colston Loveland" {
		t.Errorf("undecided lost in the round trip: %v", undecided)
	}
}

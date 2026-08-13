package draft

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestParsePreferencesDefaults: an empty document is a deliberate "use the
// defaults" — both filters on, and the two quarterback stacks.
func TestParsePreferencesDefaults(t *testing.T) {
	p, err := ParsePreferences(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if !p.OnePerOffense || !p.NoHandcuffs {
		t.Errorf("defaults should have both filters on, got %+v", p)
	}
	if !p.Allows("QB", "WR") || !p.Allows("QB", "TE") {
		t.Errorf("defaults should allow QB+WR and QB+TE stacks, got %+v", p.Stacks)
	}
	if p.Allows("WR", "TE") {
		t.Errorf("defaults should not allow a WR+TE stack")
	}
}

// TestParsePreferencesOverrides: an explicit field wins over its default, and
// a field left out keeps the default rather than falling to Go's false.
func TestParsePreferencesOverrides(t *testing.T) {
	src := `one_per_offense: false
stacks:
  - [qb, rb]
`
	p, err := ParsePreferences(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.OnePerOffense {
		t.Errorf("one_per_offense should be off")
	}
	if !p.NoHandcuffs {
		t.Errorf("no_handcuffs was omitted and should keep its default (on)")
	}
	if !p.Allows("QB", "RB") {
		t.Errorf("declared QB+RB stack should be allowed (case-insensitive)")
	}
	if p.Allows("QB", "WR") {
		t.Errorf("declaring stacks replaces the defaults, so QB+WR should be gone")
	}
}

// TestParsePreferencesEmptyStacksMeansNone: an explicit empty list is not the
// same as an omitted key. Omitted keeps the defaults; empty means no stacks.
func TestParsePreferencesEmptyStacksMeansNone(t *testing.T) {
	p, err := ParsePreferences(strings.NewReader("stacks: []\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Allows("QB", "WR") {
		t.Errorf("an explicit empty stacks list should allow nothing, got %+v", p.Stacks)
	}
}

// TestParsePreferencesUnknownKey: a misspelled key is an error, not a silent
// no-op — the same reason ParseLeansYAML rejects unknown headings.
func TestParsePreferencesUnknownKey(t *testing.T) {
	_, err := ParsePreferences(strings.NewReader("one_per_ofense: true\n"))
	if err == nil {
		t.Fatalf("a misspelled key should error, so a filter you think is on is on")
	}
}

// TestParsePreferencesBadStack: a stack that is not exactly two positions is a
// malformed rule and should be rejected.
func TestParsePreferencesBadStack(t *testing.T) {
	_, err := ParsePreferences(strings.NewReader("stacks:\n  - [qb, wr, te]\n"))
	if err == nil {
		t.Fatalf("a three-position stack should error")
	}
}

// TestLoadPreferencesMissingFileDisabled: an absent file is not an error — it
// disables the feature, so a board built without one is the league's board.
func TestLoadPreferencesMissingFileDisabled(t *testing.T) {
	p, err := LoadPreferences(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file should load clean: %v", err)
	}
	if p.Active() {
		t.Errorf("missing file should disable filtering, got %+v", p)
	}
}

// TestExampleFileParses guards the shipped template: it must stay a valid,
// loadable preferences file so a reader who copies it gets a working board.
func TestExampleFileParses(t *testing.T) {
	p, err := LoadPreferences(filepath.Join("data", "preferences.example.yaml"))
	if err != nil {
		t.Fatalf("shipped example should parse: %v", err)
	}
	if !p.Active() || !p.Allows("QB", "WR") {
		t.Errorf("shipped example should be active with the QB stacks, got %+v", p)
	}
}

// TestAllowsSelfIsNeverAStack: two players of the same position are a handcuff,
// never a stack, whatever the stacks list says.
func TestAllowsSelfIsNeverAStack(t *testing.T) {
	p := Preferences{Stacks: []Stack{{A: "WR", B: "WR"}}}
	if p.Allows("WR", "WR") {
		t.Errorf("a position never stacks with itself")
	}
}

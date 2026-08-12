package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func big3Rows() []map[string]string {
	return []map[string]string{
		{"player": "De’Von Achane", "traits_hit": "2",
			"hits_receiving": "1", "hits_explosive": "1", "hits_goal_line": "0"},
		{"player": "Chase Brown", "traits_hit": "1",
			"hits_receiving": "1", "hits_explosive": "0", "hits_goal_line": "0"},
		{"player": "Kenneth Walker", "traits_hit": "0",
			"rec_fpg": "3.5", "explosive_fpg": "3.0", "inside10_fpg": "1.1"},
		{"player": "Cam Skattebo", "traits_hit": "3",
			"hits_receiving": "1", "hits_explosive": "1", "hits_goal_line": "1"},
		{"player": "", "traits_hit": "0"},
	}
}

// TestMentonThresholds — two of three leans up, none leans down, and one is
// the middle of the field with no read at all. A lean that fired on the
// common case would say nothing.
func TestMentonThresholds(t *testing.T) {
	got, err := mentonLeans(big3Rows())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Lean{
		"De’Von Achane":  LeanUp,
		"Cam Skattebo":   LeanUp,
		"Kenneth Walker": LeanDown,
	}
	if len(got) != len(want) {
		t.Fatalf("produced %d reads, want %d: %+v", len(got), len(want), got)
	}
	for _, pl := range got {
		if want[pl.Player] != pl.Lean {
			t.Errorf("%s leaned %s, want %s", pl.Player, pl.Lean, want[pl.Player])
		}
		if pl.Source != "menton" {
			t.Errorf("%s came from %q", pl.Player, pl.Source)
		}
		if pl.Note == "" {
			t.Errorf("%s has no reason attached", pl.Player)
		}
	}
}

// TestMentonNotesNameTheTraits — a verdict without a reason cannot be argued
// with, and arguing with it is the point of holding your own set.
func TestMentonNotesNameTheTraits(t *testing.T) {
	got, err := mentonLeans(big3Rows())
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{}
	for _, pl := range got {
		notes[pl.Player] = pl.Note
	}
	if !strings.Contains(notes["De’Von Achane"], "receiving + explosive") {
		t.Errorf("up note should list the traits hit: %q", notes["De’Von Achane"])
	}
	if !strings.Contains(notes["Kenneth Walker"], "3.5") {
		t.Errorf("down note should show the per-game numbers: %q", notes["Kenneth Walker"])
	}
}

func TestMentonRejectsBadTraitCounts(t *testing.T) {
	if _, err := mentonLeans([]map[string]string{{"player": "X", "traits_hit": "lots"}}); err == nil {
		t.Error("a non-numeric trait count should fail rather than read as zero")
	}
	if _, err := mentonLeans([]map[string]string{{"player": "X"}}); err == nil {
		t.Error("a missing trait count should fail rather than lean the player down")
	}
}

// TestGeneratedSetRoundTrips — what the generator writes must be what the
// loader reads, or the board silently runs on a different set from the one
// you inspected.
func TestGeneratedSetRoundTrips(t *testing.T) {
	cfg := t.TempDir()
	leans, err := mentonLeans(big3Rows())
	if err != nil {
		t.Fatal(err)
	}
	g, _ := GeneratorNamed("menton")
	path, err := WriteLeanSet(cfg, g, leans)
	if err != nil {
		t.Fatal(err)
	}

	set, err := LoadLeanSet(cfg, "menton")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Generated {
		t.Errorf("%s did not carry the generated marker", path)
	}
	if len(set.Leans) != len(leans) {
		t.Fatalf("wrote %d reads, read back %d", len(leans), len(set.Leans))
	}
	for _, pl := range leans {
		back, ok := set.Leans[normalizeName(pl.Player)]
		if !ok {
			t.Fatalf("%s did not survive the round trip", pl.Player)
		}
		if back.Lean != pl.Lean || back.Note != pl.Note {
			t.Errorf("%s came back as %+v, want %s / %q", pl.Player, back, pl.Lean, pl.Note)
		}
	}
}

// TestGenerateRefusesToEatHandWrittenReads is the load-bearing test here.
//
// A regeneration that overwrote mine.csv would destroy work with no warning
// and no copy, which is the exact failure the set split exists to prevent.
func TestGenerateRefusesToEatHandWrittenReads(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, leansDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "menton.csv")
	handWritten := "player,lean,cap,note\nKenneth Walker,must,,mine really\n"
	if err := os.WriteFile(path, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	g, _ := GeneratorNamed("menton")
	if _, err := WriteLeanSet(cfg, g, []PlayerLean{{Player: "X", Lean: LeanDown}}); err == nil {
		t.Fatal("overwrote a file this tool did not generate")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != handWritten {
		t.Errorf("the hand-written file was modified:\n%s", after)
	}
}

// TestRegeneratingItsOwnOutputIsFine — the guard must not block the normal
// case of refreshing a set the tool wrote last week.
func TestRegeneratingItsOwnOutputIsFine(t *testing.T) {
	cfg := t.TempDir()
	g, _ := GeneratorNamed("menton")
	if _, err := WriteLeanSet(cfg, g, []PlayerLean{{Player: "X", Lean: LeanDown, Note: "old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLeanSet(cfg, g, []PlayerLean{{Player: "Y", Lean: LeanUp, Note: "new"}}); err != nil {
		t.Fatalf("refusing to rewrite its own output: %v", err)
	}
	set, err := LoadLeanSet(cfg, "menton")
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := set.Leans[normalizeName("X")]; stale {
		t.Error("the previous generation was not replaced")
	}
	if _, fresh := set.Leans[normalizeName("Y")]; !fresh {
		t.Error("the new generation is missing")
	}
}

// TestGeneratedHeaderSaysWhereItCameFrom — a file you cannot trace is a file
// you have to remember the origin of.
func TestGeneratedHeaderSaysWhereItCameFrom(t *testing.T) {
	cfg := t.TempDir()
	g, _ := GeneratorNamed("menton")
	path, err := WriteLeanSet(cfg, g, []PlayerLean{{Player: "X", Lean: LeanUp}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{generatedMarker, g.Source, "mine.yaml"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the header does not mention %q:\n%s", want, body)
		}
	}
}

// TestEveryGeneratorIsNamed guards the registry against a set that cannot be
// asked for by name.
func TestEveryGeneratorIsNamed(t *testing.T) {
	for _, g := range Generators {
		if g.Name == "" || g.Source == "" || g.What == "" || g.Build == nil {
			t.Errorf("incomplete generator: %+v", g)
		}
		if _, ok := GeneratorNamed(g.Name); !ok {
			t.Errorf("%s is registered but cannot be looked up", g.Name)
		}
	}
	if _, ok := GeneratorNamed("mine"); ok {
		t.Error("your own reads must never be regenerable")
	}
}

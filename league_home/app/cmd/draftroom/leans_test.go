package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const someLeans = "player,lean,cap,note\nKenneth Walker,must,,situation\n"

// TestLegacyMyGuysIsStillRead is the whole reason the fallback exists.
//
// A missing lean file is not an error by design — recording no reads is a
// legitimate state — so a config directory that predates the split would
// otherwise load as empty and the board would look entirely correct while
// silently ignoring every conviction in it.
func TestLegacyMyGuysIsStillRead(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, myGuysFile), []byte(someLeans), 0o644); err != nil {
		t.Fatal(err)
	}

	leans, sets, err := loadLeanSets(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(leans) != 1 {
		t.Fatalf("read %d leans from the legacy file, want 1", len(leans))
	}
	if len(sets) != 1 || sets[0].Name != "mine" {
		t.Errorf("the legacy file should present as the set 'mine': %+v", sets)
	}
	if sets[0].Generated {
		t.Error("a hand-written legacy file must not read as generated")
	}
}

// TestNewLayoutWinsOverLegacy — once migrated, the old file is dead weight
// and must not shadow or merge with the new one.
func TestNewLayoutWinsOverLegacy(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, myGuysFile), []byte(someLeans), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	current := "player,lean,cap,note\nAshton Jeanty,must,,\nChase Brown,up,,\n"
	if err := os.WriteFile(filepath.Join(dir, "mine.csv"), []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}

	leans, sets, err := loadLeanSets(cfg, []string{"mine"})
	if err != nil {
		t.Fatal(err)
	}
	if len(leans) != 2 {
		t.Fatalf("read %d leans, want the 2 from leans/mine.csv", len(leans))
	}
	if sets[0].Path != filepath.Join(dir, "mine.csv") {
		t.Errorf("read from %s, want the new layout", sets[0].Path)
	}
}

// TestMissingLeansIsNotAnError — having no reads recorded at all is a
// legitimate way to run the board.
func TestMissingLeansIsNotAnError(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.csv"), []byte("player,lean,cap,note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leans, _, err := loadLeanSets(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(leans) != 0 {
		t.Errorf("expected no leans, got %d", len(leans))
	}
}

// TestNamedSetMissingIsAnErrorEvenWithLegacyPresent — the fallback covers
// an unmigrated "mine", never a set you asked for by name and typoed.
func TestNamedSetMissingIsAnErrorEvenWithLegacyPresent(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, myGuysFile), []byte(someLeans), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadLeanSets(cfg, []string{"mine", "menton"}); err == nil {
		t.Error("asking for a set that does not exist should fail")
	}
}

// TestNameCheckIsSkippedNotFatalWithoutSourceData — the private data repo is
// optional. Someone who cloned the public repo alone still gets their leans
// listed; only the spelling check goes missing, and it says so.
func TestNameCheckIsSkippedNotFatalWithoutSourceData(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.csv"), []byte(someLeans), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runLeans(cfg, filepath.Join(t.TempDir(), "absent"), []string{"mine"}, false); err != nil {
			t.Fatalf("a missing data dir must not fail the command: %v", err)
		}
	})
	if !strings.Contains(out, "skipped the name check") {
		t.Errorf("the skip should be stated, not silent:\n%s", out)
	}
	// The reads themselves still have to be listed.
	if !strings.Contains(out, "Kenneth Walker") {
		t.Errorf("leans should still be reported:\n%s", out)
	}
}

// captureStdout collects what fn prints, since these renderers write
// straight to stdout rather than to an injected writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	w.Close()
	os.Stdout = saved
	return <-done
}

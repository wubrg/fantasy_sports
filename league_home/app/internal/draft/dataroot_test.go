package draft

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDataRootPrefersFlag(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDataRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(dir)
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveDataRootFallsBackToEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DataDirEnv, dir)
	got, err := ResolveDataRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(dir)
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveDataRootErrorsOnExplicitMissingPath is the important one: a
// path the user actually asked for must fail loudly rather than silently
// falling through to a different directory, which would build a board from
// the wrong data without saying so.
func TestResolveDataRootErrorsOnExplicitMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := ResolveDataRoot(missing); err == nil {
		t.Fatal("expected an error for an explicitly requested missing path")
	}

	t.Setenv(DataDirEnv, missing)
	if _, err := ResolveDataRoot(""); err == nil {
		t.Fatal("expected an error for a missing path from the environment")
	}
}

// TestResolveDataRootErrorNamesTheFix — someone hitting this is mid-setup,
// so the error has to carry the commands, not just the complaint.
func TestResolveDataRootErrorNamesTheFix(t *testing.T) {
	t.Setenv(DataDirEnv, "")
	t.Chdir(t.TempDir())

	_, err := ResolveDataRoot("")
	if err == nil {
		t.Fatal("expected an error when no data dir exists anywhere")
	}
	for _, want := range []string{"git init", DataDirEnv, "fantasy_sports_data"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q:\n%s", want, err)
		}
	}
}

func TestDataRootPaths(t *testing.T) {
	d := DataRoot("/data")
	if got := d.Raw("ciely", "2026-08-05"); got != "/data/raw/ciely/2026-08-05" {
		t.Errorf("Raw() = %q", got)
	}
	if got := d.Normalized("ciely-2026.csv"); got != "/data/normalized/ciely-2026.csv" {
		t.Errorf("Normalized() = %q", got)
	}
	if got := d.Manifest(); got != "/data/manifest.json" {
		t.Errorf("Manifest() = %q", got)
	}
}

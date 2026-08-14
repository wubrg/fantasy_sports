package draft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOwners(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owners.csv")
	// Mixed case and stray whitespace, to prove both are normalised.
	body := "handle,owner\nsmallville07,Ben\n  Dakota41 , Dakota \nblankowner,\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	owners, err := LoadOwners(path)
	if err != nil {
		t.Fatalf("LoadOwners: %v", err)
	}
	if got := owners["smallville07"]; got != "Ben" {
		t.Errorf("smallville07 = %q, want Ben", got)
	}
	// Handle is stored lowercased so lookup is case-insensitive.
	if got := owners["dakota41"]; got != "Dakota" {
		t.Errorf("dakota41 = %q, want Dakota", got)
	}
	// A row with no owner is skipped rather than mapping to "".
	if _, ok := owners["blankowner"]; ok {
		t.Errorf("blankowner should have been skipped")
	}
}

func TestLoadOwnersMissingFileIsEmpty(t *testing.T) {
	owners, err := LoadOwners(filepath.Join(t.TempDir(), "nope.csv"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(owners) != 0 {
		t.Errorf("missing file should yield empty map, got %d entries", len(owners))
	}
}

func TestLoadOwnersRejectsMissingColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owners.csv")
	if err := os.WriteFile(path, []byte("name,team\nBen,smallville07\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOwners(path); err == nil {
		t.Fatal("expected an error for a file missing handle/owner columns")
	}
}

func TestOwnersLabel(t *testing.T) {
	owners := Owners{"smallville07": "Ben"}

	// Known owner, and the displayed handle keeps its original casing even
	// though the lookup is case-insensitive.
	if got := owners.Label("Smallville07"); got != "Ben (Smallville07)" {
		t.Errorf("Label(known) = %q, want %q", got, "Ben (Smallville07)")
	}
	// Unknown handle falls back to the bare handle, which still identifies
	// the team.
	if got := owners.Label("newguy99"); got != "newguy99" {
		t.Errorf("Label(unknown) = %q, want newguy99", got)
	}
}

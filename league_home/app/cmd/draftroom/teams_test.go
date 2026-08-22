package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSavedTeamsDoNotCrossOwners is the load-bearing test.
//
// A guest board runs the same binary against the same config directory as
// yours -- Sam's is `-leans blank` under his own owner id, nothing more. The
// blank lean set withholds your reads and says nothing about saved teams, and
// the board fetches them on load, so a shared file would put your bid plan on
// his screen without either of you doing anything.
func TestSavedTeamsDoNotCrossOwners(t *testing.T) {
	cfg := t.TempDir()

	mine := testStatic()
	mine.ownerID = "243501760939814912"
	host, err := newServer(mine, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	host.saved = []SavedTeam{{ID: "plan", Objective: "strategy", Spend: 199}}
	if err := writeSavedTeams(host.savedPath, host.saved); err != nil {
		t.Fatal(err)
	}

	theirs := testStatic()
	theirs.ownerID = "467790106363686912"
	guest, err := newServer(theirs, cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	if guest.savedPath == host.savedPath {
		t.Fatalf("both owners resolved to one file: %s", guest.savedPath)
	}
	if len(guest.saved) != 0 {
		t.Errorf("guest board loaded %d of the host's saved teams, want 0: %+v",
			len(guest.saved), guest.saved)
	}

	// The guest saving must not reach back over the host's file either.
	guest.saved = []SavedTeam{{ID: "his", Objective: "value", Spend: 40}}
	if err := writeSavedTeams(guest.savedPath, guest.saved); err != nil {
		t.Fatal(err)
	}
	reread, err := loadSavedTeams(host.savedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reread) != 1 || reread[0].ID != "plan" {
		t.Errorf("guest write reached the host's shortlist: %+v", reread)
	}
}

// An owner id becomes part of a filename, so it may not climb out of the
// config directory. Sleeper ids are digits; anything else is dropped rather
// than trusted.
func TestSavedTeamsPathStaysInsideConfigDir(t *testing.T) {
	for _, id := range []string{"../../etc/passwd", "a/b", ".."} {
		got := savedTeamsPath("/cfg", id)
		if dir := filepath.Dir(got); dir != "/cfg" {
			t.Errorf("owner %q escaped to %s", id, got)
		}
		if strings.Contains(filepath.Base(got), "/") {
			t.Errorf("owner %q left a separator in the name: %s", id, got)
		}
	}
}

// Without an owner id there is only one board, so it keeps the plain name the
// file has always had rather than stranding an existing shortlist.
func TestSavedTeamsPathWithoutOwnerKeepsLegacyName(t *testing.T) {
	if got, want := savedTeamsPath("/cfg", ""), filepath.Join("/cfg", savedTeamsFile); got != want {
		t.Errorf("savedTeamsPath(\"/cfg\", \"\") = %s, want %s", got, want)
	}
}

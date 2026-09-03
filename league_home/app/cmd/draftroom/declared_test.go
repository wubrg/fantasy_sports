package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leaguehome/internal/draft"
)

func writeLocks(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keeperLocksFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func priced() []draft.Entry {
	return []draft.Entry{
		{OwnerID: "darin", Name: "James Cook", Position: "RB", LeaguePrice: 39},
		{OwnerID: "darin", Name: "Jaylen Waddle", Position: "WR", LeaguePrice: 20},
		{OwnerID: "adam", Name: "Puka Nacua", Position: "WR", LeaguePrice: 35},
	}
}

// TestDeclaredResolvesASuffixedName is the regression.
//
// keeper-locks.csv is typed by hand and carries "James Cook III"; the priced
// pool is spelled the projection source's way and carries "James Cook". A
// plain normalize keeps the suffix and misses him, which showed up as Darin
// keeping one player for $20 instead of two for $59 — a wrong budget in a
// document about to be sent to eleven other managers.
func TestDeclaredResolvesASuffixedName(t *testing.T) {
	cfg := writeLocks(t, "owner,player\nDarin,James Cook III\nDarin,Jaylen Waddle\n")

	got, warn := declaredEntries(cfg, priced())

	if len(warn) != 0 {
		t.Errorf("unexpected warnings: %v", warn)
	}
	if len(got) != 2 {
		t.Fatalf("want both of Darin's keepers, got %d: %+v", len(got), got)
	}
	total := 0
	for _, e := range got {
		total += e.LeaguePrice
	}
	if total != 59 {
		t.Errorf("Darin's keepers cost $%d, want $59", total)
	}
}

// A name that reaches nobody is reported, never dropped quietly: it means a
// keeper this list cannot price, and silence would understate a team's spend.
func TestDeclaredWarnsOnAnUnmatchedName(t *testing.T) {
	cfg := writeLocks(t, "owner,player\nAdam,Nobody At All\n")

	got, warn := declaredEntries(cfg, priced())

	if len(got) != 0 {
		t.Errorf("matched a player who is not in the pool: %+v", got)
	}
	if len(warn) != 1 || !strings.Contains(warn[0], "Nobody At All") {
		t.Errorf("warnings = %v, want one naming the unmatched player", warn)
	}
}

// A "keeps nobody" row is a declaration, not a keeper.
func TestDeclaredSkipsTheKeepsNobodyRow(t *testing.T) {
	cfg := writeLocks(t, "owner,player\nAdam,none\nAdam,Puka Nacua\n")

	got, warn := declaredEntries(cfg, priced())

	if len(warn) != 0 {
		t.Errorf("a keeps-nobody row produced a warning: %v", warn)
	}
	if len(got) != 1 || got[0].Name != "Puka Nacua" {
		t.Errorf("got %+v, want only the real keeper", got)
	}
}

// The owner column is documentation. A player sits on exactly one roster, so a
// name filed against the wrong manager still has to land on the right team
// rather than corrupting two of them.
func TestDeclaredTakesTheOwnerFromTheRosterNotTheFile(t *testing.T) {
	cfg := writeLocks(t, "owner,player\nAdam,James Cook III\n")

	got, _ := declaredEntries(cfg, priced())

	if len(got) != 1 {
		t.Fatalf("want the one keeper, got %+v", got)
	}
	if got[0].OwnerID != "darin" {
		t.Errorf("owner = %q, want darin — the roster settles it, not the file", got[0].OwnerID)
	}
}

// The shared document has to add up: each team's cost, what it leaves, and the
// league-wide total that comes off the auction.
func TestWriteDeclaredKeepersAddsUp(t *testing.T) {
	var buf bytes.Buffer
	err := draft.WriteDeclaredKeepers(&buf, []draft.Entry{
		{OwnerID: "darin", Name: "James Cook", Position: "RB", LeaguePrice: 39},
		{OwnerID: "darin", Name: "Jaylen Waddle", Position: "WR", LeaguePrice: 20},
		{OwnerID: "adam", Name: "Puka Nacua", Position: "WR", LeaguePrice: 35},
	}, nil, "2026", 200)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"keeps for $59, leaving $141", // darin
		"keeps for $35, leaving $165", // adam
		"3 keepers",
		"$94 off the board",
		"2 teams",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}

	// Dearest first: the expensive keeper is the one that shapes a night.
	if strings.Index(out, "James Cook") > strings.Index(out, "Jaylen Waddle") {
		t.Error("keepers are not ordered dearest first")
	}
}

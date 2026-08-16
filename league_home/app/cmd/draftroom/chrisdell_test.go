package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadChrisDellSharp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chrisdell-2026.csv")
	body := "source,position,pos_rank,player,team,ecr,vs_ecr\n" +
		"chrisdell,WR,50,Known Guy,KC,80,30\n" +
		"chrisdell,TE,10,No Match,SF,12,-15\n" +
		"chrisdell,RB,5,Bad Delta,DAL,7,x\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Resolver that only knows one player.
	resolve := func(name string) string {
		if name == "Known Guy" {
			return "id1"
		}
		return ""
	}
	m, warn := loadChrisDellSharp(path, resolve)
	if m["id1"] != 30 {
		t.Errorf("Known Guy delta = %d, want 30", m["id1"])
	}
	if len(m) != 1 {
		t.Errorf("want one resolved player, got %d: %v", len(m), m)
	}
	// The unresolved row is surfaced, the non-numeric one silently skipped.
	if warn == "" {
		t.Error("expected an unmatched-rows warning")
	}
}

func TestLoadChrisDellSharpMissingFile(t *testing.T) {
	m, warn := loadChrisDellSharp(filepath.Join(t.TempDir(), "nope.csv"), func(string) string { return "" })
	if m != nil {
		t.Errorf("missing file should yield no map, got %v", m)
	}
	if warn == "" {
		t.Error("missing file should warn that dell flags are off")
	}
}

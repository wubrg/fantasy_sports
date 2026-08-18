package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBorisChenTiers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "borischen-2026.csv")
	body := "source,position,tier,player\n" +
		"borischen,WR,1,Known Guy\n" +
		"borischen,RB,4,No Match\n" +
		"borischen,TE,x,Bad Tier\n" +
		"borischen,FLEX,3,Known Guy\n" // same player again, weaker tier — must not override
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	resolve := func(name string) string {
		if name == "Known Guy" {
			return "id1"
		}
		return ""
	}
	m, warn := loadBorisChenTiers(path, resolve)
	if m["id1"] != 1 {
		t.Errorf("Known Guy tier = %d, want 1 (strongest kept)", m["id1"])
	}
	if len(m) != 1 {
		t.Errorf("want one resolved player, got %d: %v", len(m), m)
	}
	// The unresolved row is surfaced; the non-numeric tier is silently skipped.
	if warn == "" {
		t.Error("expected an unmatched-rows warning")
	}
}

func TestLoadBorisChenTiersMissingFile(t *testing.T) {
	m, warn := loadBorisChenTiers(filepath.Join(t.TempDir(), "nope.csv"), func(string) string { return "" })
	if m != nil {
		t.Errorf("missing file should yield no map, got %v", m)
	}
	if warn == "" {
		t.Error("missing file should warn that the tier column is off")
	}
}

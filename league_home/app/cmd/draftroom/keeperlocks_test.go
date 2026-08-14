package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeeperLocks(t *testing.T) {
	cfg := t.TempDir()
	body := "owner,player\nSam,Rome Odunze\n , Zay Flowers \nSam,\n"
	if err := os.WriteFile(filepath.Join(cfg, keeperLocksFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	locks, err := loadKeeperLocks(cfg)
	if err != nil {
		t.Fatalf("loadKeeperLocks: %v", err)
	}
	// Two players; the row with a blank player is skipped, and whitespace is
	// trimmed. The owner column is documentation only.
	if len(locks) != 2 {
		t.Fatalf("got %d locks, want 2: %+v", len(locks), locks)
	}
	if locks[0].Player != "Rome Odunze" || locks[0].Owner != "Sam" {
		t.Errorf("first lock = %+v", locks[0])
	}
	if locks[1].Player != "Zay Flowers" {
		t.Errorf("second lock player = %q, want Zay Flowers", locks[1].Player)
	}
}

func TestLoadKeeperLocksMissingFileIsEmpty(t *testing.T) {
	locks, err := loadKeeperLocks(t.TempDir())
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if locks != nil {
		t.Errorf("missing file should yield no locks, got %+v", locks)
	}
}

func TestLoadKeeperLocksNeedsPlayerColumn(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, keeperLocksFile), []byte("owner\nSam\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKeeperLocks(cfg); err == nil {
		t.Fatal("expected an error for a file with no player column")
	}
}

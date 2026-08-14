package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keeperLocksFile names the players their owners are known to be keeping,
// regardless of what the surplus heuristic would guess. The keeper research
// scenarios treat these as certain locks. It is optional; a missing file
// leaves keeper projection entirely to the value math.
const keeperLocksFile = "keeper-locks.csv"

// keeperLock is one declared keeper. Owner is documentation only — a player
// is on exactly one roster, so which owner keeps him is settled by the
// roster, not this column. Kept for a readable file and to catch a name
// entered against the wrong manager by eye.
type keeperLock struct{ Owner, Player string }

// loadKeeperLocks reads the declared keeper locks. A missing file is not an
// error: no locks is the normal state, and the value math still projects
// keepers on its own.
func loadKeeperLocks(cfg string) ([]keeperLock, error) {
	f, err := os.Open(filepath.Join(cfg, keeperLocksFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("draftroom: opening keeper locks: %w", err)
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("draftroom: reading keeper locks: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	playerCol, ok := cols["player"]
	if !ok {
		return nil, fmt.Errorf("draftroom: %s needs a player column", keeperLocksFile)
	}
	ownerCol, hasOwner := cols["owner"]

	var out []keeperLock
	for _, r := range records[1:] {
		if playerCol >= len(r) {
			continue
		}
		player := strings.TrimSpace(r[playerCol])
		if player == "" {
			continue
		}
		owner := ""
		if hasOwner && ownerCol < len(r) {
			owner = strings.TrimSpace(r[ownerCol])
		}
		out = append(out, keeperLock{Owner: owner, Player: player})
	}
	return out, nil
}

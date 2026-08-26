package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keeperLocksFile names the players their owners are known to be keeping,
// regardless of what the surplus heuristic would guess. It is optional; a
// missing file leaves keeper projection entirely to the value math.
//
// The file carries two things at once, which is what lets the board follow a
// league from "nobody has decided" to "keepers are final" without a switch to
// flip on the day:
//
//   - which players are kept, and
//   - which owners have DECLARED, by appearing in it at all.
//
// An owner in the file has finished deciding: his listed players are his whole
// keeper set and the surplus heuristic stops guessing for him. An owner absent
// has not entered yet, and is still projected. Absence is a gap, never a claim
// that he keeps nobody — a team really keeping nobody says so with a `none`
// row, because silence and "I decided nothing" have to stay distinguishable.
const keeperLocksFile = "keeper-locks.csv"

// noKeepers are the player values that mean "this owner declared, and is
// keeping nobody". Without a spelling for that, a team that keeps none is
// indistinguishable from a team that has not filed yet.
var noKeepers = map[string]bool{"none": true, "-": true, "nobody": true}

// keeperLock is one row of that file. A blank Player marks an owner who
// declared no keepers; Owner is required on such a row, since there is no
// player to identify the roster from.
//
// On a normal row Owner is documentation — a player is on exactly one roster,
// so which owner keeps him is settled by the roster, not this column — but it
// is still read, to catch a name entered against the wrong manager by eye.
type keeperLock struct{ Owner, Player string }

// Declared reports whether this row is the "keeping nobody" marker.
func (k keeperLock) Declared() bool { return k.Player == "" }

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
		owner := ""
		if hasOwner && ownerCol < len(r) {
			owner = strings.TrimSpace(r[ownerCol])
		}
		// A wholly blank line is nothing; a `none` row is a declaration and
		// has to name its owner, because no player identifies the roster.
		if noKeepers[strings.ToLower(player)] {
			if owner == "" {
				return nil, fmt.Errorf("draftroom: %s has a %q row with no owner — say whose team keeps nobody", keeperLocksFile, player)
			}
			out = append(out, keeperLock{Owner: owner})
			continue
		}
		if player == "" {
			continue
		}
		out = append(out, keeperLock{Owner: owner, Player: player})
	}
	return out, nil
}

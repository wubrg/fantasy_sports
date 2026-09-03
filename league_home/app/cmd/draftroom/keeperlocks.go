package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"leaguehome/internal/draft"
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

// declaredEntries joins the filed keeper locks to the priced candidate pool.
//
// Through the board's own PoolMatcher rather than a bare name comparison. The
// file is typed by hand and the pool is spelled the projection source's way,
// so "James Cook III" has to reach "James Cook" — and it does not on a plain
// normalize, which keeps the suffix. The matcher carries the stem index that
// closes that gap, and the aliases file besides; it also refuses a stem two
// players share, which is the behaviour worth having when the alternative is
// charging a keeper to the wrong roster.
//
// The owner comes from the entry, never from the file's owner column. That
// column is documentation — a player sits on exactly one roster and that
// settles whose keeper he is — so a name filed against the wrong manager still
// lands on the right team here rather than corrupting two of them.
//
// A lock that reaches no priced entry is returned as a warning rather than
// dropped in silence. It means a keeper nobody can price, and this list is
// about to be sent to eleven other people.
func declaredEntries(cfg string, priced []draft.Entry) ([]draft.Entry, []string) {
	locks, err := loadKeeperLocks(cfg)
	if err != nil {
		return nil, []string{fmt.Sprintf("keeper locks unreadable: %v", err)}
	}

	names := make([]string, 0, len(priced))
	byName := make(map[string]draft.Entry, len(priced))
	for _, e := range priced {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	// Aliases are optional; without them the matcher still has its name and
	// stem indexes, which is what this join actually leans on.
	aliases, _ := draft.LoadAliases(filepath.Join(cfg, aliasesFile))
	matcher := draft.NewPoolMatcher(names, aliases)

	var out []draft.Entry
	var warn []string
	for _, lk := range locks {
		if lk.Declared() {
			// A "keeps nobody" row is a declaration, not a keeper.
			continue
		}
		canonical, ok := matcher.Canonical(lk.Player)
		if !ok {
			warn = append(warn, fmt.Sprintf("%q is filed as a keeper but reaches no priced player — omitted", lk.Player))
			continue
		}
		e, ok := byName[canonical]
		if !ok {
			warn = append(warn, fmt.Sprintf("%q resolved to %q, which carries no price — omitted", lk.Player, canonical))
			continue
		}
		out = append(out, e)
	}
	return out, warn
}

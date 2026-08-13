package draft

import (
	"fmt"
	"strings"
)

// BlockedForMe returns the players who are off *your* board — not the room's —
// given who you already own and your declared preferences. The key is a player
// ID, the value a short reason naming the teammate who blocks him.
//
// This is the "filter, not a solver" of D8: it never removes a player or
// changes a price, it only marks the ones your own roster has already spent.
// Owning a piece of an offense takes the rest of that offense with it, and
// your starter's handcuff never joins it. A player you already own is not a
// candidate against himself, so he is skipped.
func BlockedForMe(owned, board []PlayerSignals, prefs Preferences) map[string]string {
	if !prefs.Active() {
		return nil
	}

	ownedByTeam := map[string][]PlayerSignals{}
	ownedIDs := map[string]bool{}
	for _, o := range owned {
		ownedIDs[o.PlayerID] = true
		if skipPos(o.Position) {
			continue
		}
		if t := normTeam(o.Team); t != "" {
			ownedByTeam[t] = append(ownedByTeam[t], o)
		}
	}
	if len(ownedByTeam) == 0 {
		return nil
	}

	out := map[string]string{}
	for _, c := range board {
		if ownedIDs[c.PlayerID] || skipPos(c.Position) {
			continue
		}
		team := normTeam(c.Team)
		if team == "" {
			continue
		}
		mates := ownedByTeam[team]
		if len(mates) == 0 {
			continue
		}
		if reason := blockReason(c, mates, prefs); reason != "" {
			out[c.PlayerID] = reason
		}
	}
	return out
}

// blockReason decides whether candidate c is blocked by the teammates you own,
// and why. The handcuff rule is checked first because it is the harder block:
// a same-position teammate rules c out even on an offense you are stacking.
func blockReason(c PlayerSignals, mates []PlayerSignals, prefs Preferences) string {
	team := normTeam(c.Team)
	if prefs.NoHandcuffs {
		for _, m := range mates {
			if strings.EqualFold(m.Position, c.Position) {
				return fmt.Sprintf("%s·%s (handcuff)", team, surnameLabel(m.Name))
			}
		}
	}
	if prefs.OnePerOffense {
		// Allowed onto the offense only if some teammate you own forms a stack
		// shape with him. A running back anchors no stack, so owning only the
		// back blocks the whole offense.
		for _, m := range mates {
			if prefs.Allows(m.Position, c.Position) {
				return ""
			}
		}
		return fmt.Sprintf("%s·%s", team, surnameLabel(mates[0].Name))
	}
	return ""
}

// EffectiveScarcity is league scarcity recomputed over your board — the pool
// with the blocked players removed — against the same pinned thresholds, so
// "yours" and "the league's" are measured at the same bar and only the count
// moves. Nil when nothing is blocked, in which case it equals league scarcity.
func EffectiveScarcity(board []PlayerSignals, blocked map[string]string, state PoolState, thresholds map[string]float64) map[string]PositionScarcity {
	if len(blocked) == 0 {
		return nil
	}
	filtered := make([]PlayerSignals, 0, len(board))
	for _, p := range board {
		if _, off := blocked[p.PlayerID]; off {
			continue
		}
		filtered = append(filtered, p)
	}
	return Scarcity(filtered, state, thresholds)
}

// skipPos is true for positions the offense filter should ignore — team
// defenses and kickers do not stack the way skill players do.
func skipPos(pos string) bool {
	switch strings.ToUpper(strings.TrimSpace(pos)) {
	case "DEF", "DST", "K":
		return true
	}
	return false
}

func normTeam(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// surnameLabel is the last word of a name, for a compact block reason. A
// single-word name (a team defense) is returned whole.
func surnameLabel(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return name
	}
	return fields[len(fields)-1]
}

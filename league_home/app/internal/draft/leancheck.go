package draft

import "sort"

// UnmatchedLean is a read naming a player the pool does not contain, which
// means it can never reach the board.
//
// This is the quiet failure in a lean file. A lean fires by name — WalkAway
// looks up normalizeName(player) against the projection source's spelling —
// so a name that is wrong by one character does not error, does not warn,
// and does not apply. It reads as a conviction you hold right up until the
// bidding, when it silently isn't there.
type UnmatchedLean struct {
	// Lean is the read as written, kept whole so a caller can say whether
	// what was lost was a must-have or a shrug.
	Lean PlayerLean
	// Suggestion is the closest name in the pool, or empty when nothing is
	// close enough to name without guessing.
	Suggestion string
}

// suggestionSlack is how far a name may be from a pool entry and still be
// offered as what you meant, as a fraction of its length: a longer name
// tolerates more drift than a short one, since one letter wrong in
// "croskeymerritt" is a typo and one letter wrong in "kelce" is a different
// player.
const suggestionSlack = 5

// Unmatched reports the reads that name nobody in the pool, each with the
// closest pool name when one is near enough to be worth offering.
//
// pool is the projection source's spelling of every player, because that is
// what a lean is matched against — see Projection.Name in the board's
// assembly. An empty pool returns nothing rather than declaring every read
// unmatched: "no source loaded" and "none of your reads are real" are very
// different claims, and only one of them should ever reach the screen.
func (l Leans) Unmatched(pool []string) []UnmatchedLean {
	if len(pool) == 0 {
		return nil
	}
	known := make(map[string]string, len(pool))
	for _, name := range pool {
		if key := normalizeName(name); key != "" {
			known[key] = name
		}
	}

	var out []UnmatchedLean
	for key, pl := range l {
		if _, ok := known[key]; ok {
			continue
		}
		out = append(out, UnmatchedLean{Lean: pl, Suggestion: nearest(key, known)})
	}
	// By the name as written, so the report reads the same way twice.
	sort.Slice(out, func(i, j int) bool { return out[i].Lean.Player < out[j].Lean.Player })
	return out
}

// nearest returns the closest name in known, or "" when the closest is far
// enough away that naming it would invent a read rather than recover one.
//
// Something is always nearest in a pool of hundreds. Offering it
// unconditionally would turn a typo report into a source of new mistakes,
// so a miss with no plausible fix says so instead.
func nearest(key string, known map[string]string) string {
	limit := len(key) / suggestionSlack
	if limit < 2 {
		limit = 2
	}
	best, bestName := limit+1, ""
	for candidate, name := range known {
		d := editDistance(key, candidate, best)
		if d < best {
			best, bestName = d, name
		}
	}
	return bestName
}

// editDistance is Levenshtein distance, giving up as soon as it exceeds
// cutoff since the caller only cares about near misses.
func editDistance(a, b string, cutoff int) int {
	ar, br := []rune(a), []rune(b)
	// A length gap alone already costs that many edits, so most of the pool
	// can be rejected without doing any work.
	if d := len(ar) - len(br); d >= cutoff || -d >= cutoff {
		return cutoff
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		row := curr[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
			if curr[j] < row {
				row = curr[j]
			}
		}
		// Every remaining row can only add to the best value on this one.
		if row >= cutoff {
			return cutoff
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

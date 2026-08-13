package draft

import (
	"sort"
	"strings"
)

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

// PoolMatcher resolves a name as written to the projection source's own
// spelling of that player.
//
// Vendor rows have always got this treatment — PlayerIndex.Resolve applies
// aliases and drops generational suffixes before deciding a row is
// unmatched. Leans got a plain map lookup, so a read had to be spelled the
// way the source spells it or it recorded nothing at all. "Kenneth Walker
// III" is what Sleeper calls him and what a person types; the source says
// "Kenneth Walker", and the difference was worth a silently missing
// must-have.
//
// Deliberately built from the pool and the alias file rather than from
// PlayerIndex, which needs Sleeper's 5MB dictionary. `draftroom leans` is
// offline by design, so an index-based matcher would make the report
// disagree with the board — flagging reads that in fact work, which is the
// fastest way to teach someone to ignore a report.
type PoolMatcher struct {
	// byName is the pool keyed by normalized name.
	byName map[string]string
	// byStem is the pool keyed by suffix-stripped name, holding the empty
	// string where two players collapse to the same stem.
	byStem map[string]string
	// synonym maps a normalized name to another name for the same player,
	// derived from alias entries that share a Sleeper id.
	synonym map[string][]string
}

// NewPoolMatcher indexes pool for matching. aliases may be nil.
func NewPoolMatcher(pool []string, aliases Aliases) *PoolMatcher {
	m := &PoolMatcher{
		byName:  make(map[string]string, len(pool)),
		byStem:  make(map[string]string, len(pool)),
		synonym: map[string][]string{},
	}
	for _, name := range pool {
		key := normalizeName(name)
		if key == "" {
			continue
		}
		m.byName[key] = name
		stem := stemName(name)
		if held, taken := m.byStem[stem]; taken && held != name {
			// Two players with one stem. Neither is the answer, and a
			// guess here would put a read on a player nobody named.
			m.byStem[stem] = ""
			continue
		}
		m.byStem[stem] = name
	}

	// Names sharing a Sleeper id are spellings of one player, which is the
	// only name-to-name fact the alias file holds without the dictionary.
	byID := map[string][]string{}
	for name, id := range aliases {
		byID[id] = append(byID[id], name)
	}
	for _, names := range byID {
		for _, name := range names {
			for _, other := range names {
				if other != name {
					m.synonym[name] = append(m.synonym[name], other)
				}
			}
		}
	}
	return m
}

// Canonical returns the pool's spelling of the named player.
//
// Tried in order of confidence, stopping at the first hit: the name as
// written, the name without a generational suffix, then any spelling the
// alias file says is the same player. Anything ambiguous resolves to
// nothing and is left for the unmatched report, which can at least show
// what it was close to.
func (m *PoolMatcher) Canonical(name string) (string, bool) {
	key := normalizeName(name)
	if key == "" {
		return "", false
	}
	if got, ok := m.byName[key]; ok {
		return got, true
	}
	if got, ok := m.byStem[stemName(name)]; ok && got != "" {
		return got, true
	}
	for _, other := range m.synonym[key] {
		if got, ok := m.byName[other]; ok {
			return got, true
		}
		if got, ok := m.byStem[stripSuffix(other)]; ok && got != "" {
			return got, true
		}
	}
	return "", false
}

// Match rewrites lean keys to the pool's spelling, so a read spelled a
// reasonable way still reaches the board. Reads that resolve to nothing are
// left exactly as they are, for the unmatched report to explain.
//
// Call this on each set *before* merging them, never on the merged result.
// Matching collapses two spellings of one player onto one key, and only the
// merge knows which set outranks which; run afterwards it would be picking
// between them itself, with nothing to pick on.
//
// Within a single set a collision means the file names one player twice
// under two spellings. The read that sorts first wins, so a given file
// always produces the same board rather than a different one per restart.
func (l Leans) Match(m *PoolMatcher) Leans {
	if m == nil {
		return l
	}
	keys := make([]string, 0, len(l))
	for key := range l {
		keys = append(keys, key)
	}
	// Go randomises map iteration, so an unsorted pass would resolve a
	// collision differently between runs of the same file.
	sort.Strings(keys)

	out := make(Leans, len(l))
	for _, key := range keys {
		pl := l[key]
		to := key
		if canonical, ok := m.Canonical(pl.Player); ok {
			to = normalizeName(canonical)
		}
		if _, taken := out[to]; !taken {
			out[to] = pl
		}
	}
	return out
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
func (l Leans) Unmatched(pool []string, m *PoolMatcher) []UnmatchedLean {
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
		// The report has to agree with the board, so it asks the same
		// matcher: a read the board will find is not unmatched.
		if m != nil {
			if _, ok := m.Canonical(pl.Player); ok {
				continue
			}
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

// ClosestPlayer names the player nearest to a written name, when one is
// near enough to be worth naming.
//
// Used to turn an unresolved source row into a fix you can paste rather
// than a name you have to go and look up. The threshold is the same one
// Unmatched suggests within, because a suggestion confident enough to print
// beside a read is confident enough to print beside a row — and one that is
// not is worse than none. A wrong alias binds every read on that player to
// somebody else, silently, which is the failure this whole area exists to
// stop.
// MatchKind says how a candidate was found, because the two ways carry very
// different confidence and the caller has to say so.
type MatchKind int

const (
	// MatchNone means nothing was near enough to name.
	MatchNone MatchKind = iota
	// MatchSpelling means the names are within a couple of edits — a typo,
	// and almost certainly the same player.
	MatchSpelling
	// MatchSurname means only the surname, position and team agree. That is
	// how a nickname is found ("Hollywood Brown" is "Marquise Brown"), and
	// it is also how two different men are confused: Brian Robinson and
	// Bijan Robinson are both running backs in Atlanta. It is a candidate
	// for a person to judge, never a fix to apply unread.
	MatchSurname
)

func ClosestPlayer(want, position, team string, players map[string]PlayerInfo) (id string, info PlayerInfo, kind MatchKind) {
	key := normalizeName(want)
	if key == "" || len(players) == 0 {
		return "", PlayerInfo{}, MatchNone
	}

	// Only players at the stated position are candidates. The source row
	// says what he plays, and searching the whole dictionary without it
	// offered a wide receiver a defensive lineman on the strength of a
	// surname — a fifth of held-out players drew a confidently wrong
	// suggestion that way. A quarterback is not a near miss for a guard
	// however close the letters run.
	type cand struct {
		id   string
		dist int
		same bool // team agrees with the row
	}
	limit := len(key) / suggestionSlack
	if limit < 2 {
		limit = 2
	}
	var best []cand
	for pid, p := range players {
		if position != "" && !strings.EqualFold(p.Position, position) {
			continue
		}
		n := normalizeName(p.Name)
		if n == "" {
			continue
		}
		if d := editDistance(key, n, limit+1); d <= limit && looksLikeATypo(want, p.Name) {
			best = append(best, cand{pid, d, team != "" && strings.EqualFold(p.Team, team)})
		}
	}
	if len(best) > 0 {
		// Closest first, then the team the source stated, then the id — so
		// the same file suggests the same player every run. Ranking by map
		// order meant three men named Chris Smith produced a different id
		// per invocation, and a suggestion you cannot reproduce is one you
		// cannot review before pasting it.
		sort.Slice(best, func(i, j int) bool {
			if best[i].dist != best[j].dist {
				return best[i].dist < best[j].dist
			}
			if best[i].same != best[j].same {
				return best[i].same
			}
			return best[i].id < best[j].id
		})
		return best[0].id, players[best[0].id], MatchSpelling
	}

	// Edit distance cannot reach a nickname: "Hollywood Brown" is nowhere
	// near "Marquise Brown" as a string, and they are the same man. That is
	// precisely the case aliases.csv exists for, so it deserves a second
	// try on different evidence — the surname with the position and team the
	// source already told us.
	//
	// Only when exactly one player fits. Two men sharing a surname on one
	// team at one position is rare, but naming either would be a guess, and
	// a wrong alias binds every read on that player to somebody else.
	if pid, found := onlyBySurname(want, position, team, players); found {
		return pid, players[pid], MatchSurname
	}
	return "", PlayerInfo{}, MatchNone
}

// onlyBySurname finds the single player with this surname at this position
// and team, if there is exactly one.
func onlyBySurname(want, position, team string, players map[string]PlayerInfo) (string, bool) {
	fields := strings.Fields(want)
	if len(fields) < 2 || position == "" || team == "" {
		return "", false
	}
	surname := normalizeName(fields[len(fields)-1])
	if surname == "" {
		return "", false
	}
	var hit string
	for pid, p := range players {
		pf := strings.Fields(p.Name)
		if len(pf) < 2 || normalizeName(pf[len(pf)-1]) != surname {
			continue
		}
		if !strings.EqualFold(p.Position, position) || !strings.EqualFold(p.Team, team) {
			continue
		}
		if hit != "" {
			return "", false
		}
		hit = pid
	}
	return hit, hit != ""
}

// looksLikeATypo reports whether two names differ the way one name
// mistyped differs, rather than the way two people differ.
//
// Whole-string distance cannot tell those apart. "Josh Williams" and "Joe
// Williams" are two edits apart and two different running backs; "isaiah
// lilely" and "Isaiah Likely" are two edits apart and one tight end. What
// separates them is where the edits fall. Two men very often share a
// surname exactly and differ in the first name; one man mistyped usually
// keeps his first name and loses a letter somewhere in the surname.
//
// So the surname may drift and the given name may not, beyond a single
// character. Applied to the paste-ready suggestion only, because that is
// the one offered as a fix rather than as a candidate to judge.
func looksLikeATypo(a, b string) bool {
	firstA, lastA := splitName(a)
	firstB, lastB := splitName(b)
	if lastA == "" || lastB == "" {
		// A single-token name has no parts to weigh; fall back to the
		// whole-string judgement the caller already made.
		return true
	}
	return editDistance(firstA, firstB, 2) <= 1
}

// splitName returns a name's normalized given name and surname.
func splitName(raw string) (first, last string) {
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return normalizeName(raw), ""
	}
	return normalizeName(fields[0]), normalizeName(fields[len(fields)-1])
}

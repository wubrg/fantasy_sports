package draft

import "strings"

// nflTeam is one franchise, enough of it to recognise a human's spelling.
type nflTeam struct{ abbr, city, nick string }

// nflTeams is the league, keyed for lookup by ResolveTeam. Abbreviations
// match Sleeper's dictionary, which is what player rows carry.
var nflTeams = []nflTeam{
	{"ARI", "Arizona", "Cardinals"},
	{"ATL", "Atlanta", "Falcons"},
	{"BAL", "Baltimore", "Ravens"},
	{"BUF", "Buffalo", "Bills"},
	{"CAR", "Carolina", "Panthers"},
	{"CHI", "Chicago", "Bears"},
	{"CIN", "Cincinnati", "Bengals"},
	{"CLE", "Cleveland", "Browns"},
	{"DAL", "Dallas", "Cowboys"},
	{"DEN", "Denver", "Broncos"},
	{"DET", "Detroit", "Lions"},
	{"GB", "Green Bay", "Packers"},
	{"HOU", "Houston", "Texans"},
	{"IND", "Indianapolis", "Colts"},
	{"JAX", "Jacksonville", "Jaguars"},
	{"KC", "Kansas City", "Chiefs"},
	{"LAC", "Los Angeles", "Chargers"},
	{"LAR", "Los Angeles", "Rams"},
	{"LV", "Las Vegas", "Raiders"},
	{"MIA", "Miami", "Dolphins"},
	{"MIN", "Minnesota", "Vikings"},
	{"NE", "New England", "Patriots"},
	{"NO", "New Orleans", "Saints"},
	{"NYG", "New York", "Giants"},
	{"NYJ", "New York", "Jets"},
	{"PHI", "Philadelphia", "Eagles"},
	{"PIT", "Pittsburgh", "Steelers"},
	{"SF", "San Francisco", "49ers"},
	{"SEA", "Seattle", "Seahawks"},
	{"TB", "Tampa Bay", "Buccaneers"},
	{"TEN", "Tennessee", "Titans"},
	{"WAS", "Washington", "Commanders"},
}

// teamKeyIndex maps a normalised identifier (abbr, nickname, city, or full
// name) to an abbreviation. A key two teams share -- "los angeles", "new
// york" -- maps to the empty string so it resolves nothing on its own; the
// nickname disambiguates those.
var teamKeyIndex = map[string]string{}

// nicknameIndex maps a normalised nickname to its abbreviation, for the
// word-level fallback that recovers a misspelled city ("Las Angeles
// Chargers" on "Chargers").
var nicknameIndex = map[string]string{}

func init() {
	add := func(key, abbr string) {
		key = teamNameKey(key)
		if key == "" {
			return
		}
		if held, taken := teamKeyIndex[key]; taken && held != abbr {
			teamKeyIndex[key] = "" // ambiguous; needs the nickname
			return
		}
		teamKeyIndex[key] = abbr
	}
	for _, t := range nflTeams {
		add(t.abbr, t.abbr)
		add(t.nick, t.abbr)
		add(t.city, t.abbr)
		add(t.city+t.nick, t.abbr)
		nicknameIndex[teamNameKey(t.nick)] = t.abbr
	}
}

// teamNameKey normalises a team identifier to lowercase alphanumerics, so
// spacing, case, and punctuation do not matter to a match. (teamKey is
// already taken by the roster sampler.)
func teamNameKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ResolveTeam maps a human team name to its abbreviation. It accepts an
// abbreviation, nickname, city, or full name, ignoring case, spacing, and
// punctuation, and falls back to a nickname appearing as a word so a
// misspelled city still resolves. It returns false when nothing matches, so
// a caller can fail loudly rather than silently drop a team.
func ResolveTeam(s string) (string, bool) {
	if abbr, ok := teamKeyIndex[teamNameKey(s)]; ok && abbr != "" {
		return abbr, true
	}
	for _, w := range strings.Fields(s) {
		if abbr, ok := nicknameIndex[teamNameKey(w)]; ok {
			return abbr, true
		}
	}
	return "", false
}

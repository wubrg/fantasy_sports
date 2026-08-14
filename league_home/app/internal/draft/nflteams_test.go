package draft

import "testing"

func TestResolveTeam(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Chicago Bears", "CHI", true},
		{"chicago bears", "CHI", true},
		{"  CHI ", "CHI", true},
		{"Bears", "CHI", true},
		{"New England", "NE", true},           // city only, unambiguous
		{"Las Angeles Chargers", "LAC", true}, // misspelled city, nickname saves it
		{"Cincinatti Bengals", "CIN", true},   // misspelled city, nickname saves it
		{"Los Angeles", "", false},            // two teams share the city, no nickname
		{"New York", "", false},               // same
		{"Gotham Rogues", "", false},          // not a team
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ResolveTeam(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ResolveTeam(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestResolveTeamEveryTeamRoundTrips(t *testing.T) {
	for _, tm := range nflTeams {
		full := tm.city + " " + tm.nick
		if got, ok := ResolveTeam(full); !ok || got != tm.abbr {
			t.Errorf("ResolveTeam(%q) = (%q, %v), want %q", full, got, ok, tm.abbr)
		}
		if got, ok := ResolveTeam(tm.abbr); !ok || got != tm.abbr {
			t.Errorf("ResolveTeam(%q) = (%q, %v), want %q", tm.abbr, got, ok, tm.abbr)
		}
	}
}

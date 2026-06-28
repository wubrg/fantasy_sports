// Package core normalizes raw Sleeper API data and locally-curated league
// data (history, rules, announcements) into the operations every front end
// (CLI, Discord bot, web UI) calls into. No front end owns league-data
// logic; they only format whatever this package returns.
package core

import "leaguehome/internal/sleeper"

// Service is the shared data layer. Construct with New.
type Service struct {
	sleeper  *sleeper.Client
	leagueID string
}

// New returns a Service for the given Sleeper league ID.
func New(leagueID string) *Service {
	return &Service{sleeper: sleeper.New(), leagueID: leagueID}
}

// NewWithClient returns a Service backed by a caller-supplied Sleeper
// client, so tests can point it at an httptest server instead of the real
// Sleeper API.
func NewWithClient(leagueID string, c *sleeper.Client) *Service {
	return &Service{sleeper: c, leagueID: leagueID}
}

// teamLookup builds a roster_id -> team name map by joining rosters to
// users on owner_id, since Sleeper doesn't return team names from a single
// endpoint.
func teamLookup(rosters []sleeper.Roster, users []sleeper.User) map[int]string {
	byOwner := make(map[string]string, len(users))
	for _, u := range users {
		byOwner[u.UserID] = u.TeamName()
	}

	names := make(map[int]string, len(rosters))
	for _, r := range rosters {
		name := byOwner[r.OwnerID]
		if name == "" {
			name = "Unknown"
		}
		names[r.RosterID] = name
	}
	return names
}

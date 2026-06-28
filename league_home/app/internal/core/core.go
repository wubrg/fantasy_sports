// Package core normalizes raw Sleeper API data and locally-curated league
// data (history, rules, announcements) into the operations every front end
// (CLI, Discord bot, web UI) calls into. No front end owns league-data
// logic; they only format whatever this package returns.
package core

import (
	"leaguehome/internal/espn"
	"leaguehome/internal/sleeper"
)

// Service is the shared data layer. Construct with New.
type Service struct {
	sleeper  *sleeper.Client
	leagueID string

	// espn and espnLeagueID are set by WithESPN/WithESPNClient, for leagues
	// that migrated from ESPN to Sleeper and want their pre-migration
	// history (see Historical* methods in historical.go). Both are nil/""
	// for a Service that doesn't have ESPN history to offer.
	espn         *espn.Client
	espnLeagueID string
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

// WithESPN attaches ESPN-era historical data access to a Service, for a
// league that migrated from ESPN to Sleeper. espnLeagueID is the league's
// ESPN league ID (a separate ID space from the Sleeper league ID Service is
// already bound to); s2/swid are the espn_s2/SWID auth cookies copied from
// a league member's browser session, required because ESPN's API has no
// keyless/public read access for league history the way Sleeper's does
// (pass "", "" if the league happens to be public). Returns s for
// chaining, e.g. core.New(leagueID).WithESPN(espnLeagueID, s2, swid).
func (s *Service) WithESPN(espnLeagueID, s2, swid string) *Service {
	return s.WithESPNClient(espnLeagueID, espn.New(s2, swid))
}

// WithESPNClient attaches a caller-supplied ESPN client, so tests can point
// it at an httptest server instead of the real ESPN API.
func (s *Service) WithESPNClient(espnLeagueID string, c *espn.Client) *Service {
	s.espn = c
	s.espnLeagueID = espnLeagueID
	return s
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

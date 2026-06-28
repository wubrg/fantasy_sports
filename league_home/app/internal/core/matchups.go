package core

import (
	"sort"

	"leaguehome/internal/sleeper"
)

// Matchup is one head-to-head pairing in a given week. Away is empty for a
// bye (an odd roster count, or a playoff-bracket gap).
type Matchup struct {
	Week       int
	Home       string
	HomePoints float64
	Away       string
	AwayPoints float64
}

// Matchups returns the week's schedule, pairing up rosters that share a
// MatchupID. Points are 0 for weeks that haven't been played yet.
func (s *Service) Matchups(week int) ([]Matchup, error) {
	raw, err := s.sleeper.Matchups(s.leagueID, week)
	if err != nil {
		return nil, err
	}
	rosters, err := s.sleeper.Rosters(s.leagueID)
	if err != nil {
		return nil, err
	}
	users, err := s.sleeper.Users(s.leagueID)
	if err != nil {
		return nil, err
	}
	names := teamLookup(rosters, users)

	byMatchupID := make(map[int][]sleeper.Matchup)
	for _, m := range raw {
		byMatchupID[m.MatchupID] = append(byMatchupID[m.MatchupID], m)
	}

	ids := make([]int, 0, len(byMatchupID))
	for id := range byMatchupID {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	matchups := make([]Matchup, 0, len(ids))
	for _, id := range ids {
		sides := byMatchupID[id]
		mu := Matchup{Week: week}
		if len(sides) > 0 {
			mu.Home = names[sides[0].RosterID]
			mu.HomePoints = sides[0].Points
		}
		if len(sides) > 1 {
			mu.Away = names[sides[1].RosterID]
			mu.AwayPoints = sides[1].Points
		}
		matchups = append(matchups, mu)
	}
	return matchups, nil
}

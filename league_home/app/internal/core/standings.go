package core

import "sort"

// StandingsRow is one team's record in the current standings.
type StandingsRow struct {
	Team          string
	Wins          int
	Losses        int
	Ties          int
	PointsFor     float64
	PointsAgainst float64
}

// Standings returns every team's current record, ranked by wins then
// points for (Sleeper's own tiebreaker for league standings).
func (s *Service) Standings() ([]StandingsRow, error) {
	rosters, err := s.sleeper.Rosters(s.leagueID)
	if err != nil {
		return nil, err
	}
	users, err := s.sleeper.Users(s.leagueID)
	if err != nil {
		return nil, err
	}
	names := teamLookup(rosters, users)

	rows := make([]StandingsRow, 0, len(rosters))
	for _, r := range rosters {
		rows = append(rows, StandingsRow{
			Team:          names[r.RosterID],
			Wins:          r.Settings.Wins,
			Losses:        r.Settings.Losses,
			Ties:          r.Settings.Ties,
			PointsFor:     r.PointsFor(),
			PointsAgainst: r.PointsAgainst(),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Wins != rows[j].Wins {
			return rows[i].Wins > rows[j].Wins
		}
		return rows[i].PointsFor > rows[j].PointsFor
	})
	return rows, nil
}

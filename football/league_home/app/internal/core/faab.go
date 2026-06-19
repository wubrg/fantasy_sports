package core

import "sort"

// FaabRow is one team's waiver budget usage.
type FaabRow struct {
	Team      string
	Budget    int
	Used      int
	Remaining int
}

// Faab returns every team's FAAB (waiver budget) balance.
func (s *Service) Faab() ([]FaabRow, error) {
	league, err := s.sleeper.League(s.leagueID)
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

	rows := make([]FaabRow, 0, len(rosters))
	for _, r := range rosters {
		used := r.Settings.WaiverBudgetUsed
		rows = append(rows, FaabRow{
			Team:      names[r.RosterID],
			Budget:    league.Settings.WaiverBudget,
			Used:      used,
			Remaining: league.Settings.WaiverBudget - used,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Remaining > rows[j].Remaining
	})
	return rows, nil
}

package core

// Season is one year of this league's history on Sleeper: its own league
// ID (Sleeper mints a new one every year a league rolls over) plus the
// year and that year's league status.
type Season struct {
	LeagueID string
	Season   string
	Status   string
}

// Seasons returns every season in this league's Sleeper history, most
// recent (the configured league ID) first, by walking the
// previous_league_id chain back until Sleeper stops returning one. Sleeper
// terminates the chain either with an empty/"0" previous_league_id or by
// omitting the field entirely, depending on how old the league is.
func (s *Service) Seasons() ([]Season, error) {
	var seasons []Season
	seen := make(map[string]bool)

	id := s.leagueID
	for id != "" && id != "0" && !seen[id] {
		seen[id] = true
		l, err := s.sleeper.League(id)
		if err != nil {
			return nil, err
		}
		seasons = append(seasons, Season{LeagueID: l.LeagueID, Season: l.Season, Status: l.Status})
		id = l.PreviousLeagueID
	}
	return seasons, nil
}

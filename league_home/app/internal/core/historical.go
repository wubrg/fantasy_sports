package core

import (
	"fmt"
	"sort"

	"leaguehome/internal/espn"
)

// HistoricalStanding is one team's final record for an ESPN-era season
// (before the league migrated to Sleeper).
type HistoricalStanding struct {
	Team          string
	Wins          int
	Losses        int
	Ties          int
	PointsFor     float64
	PointsAgainst float64
}

// HistoricalMatchup is one head-to-head pairing in an ESPN-era season's
// schedule.
type HistoricalMatchup struct {
	Week       int
	Home       string
	HomePoints float64
	Away       string
	AwayPoints float64
}

// HistoricalDraftPick is one selection in an ESPN-era season's draft.
// PlayerID is ESPN's raw numeric player ID; resolving it to a name needs a
// separate, unimplemented lookup against ESPN's player database.
type HistoricalDraftPick struct {
	Round    int
	Pick     int
	Overall  int
	Team     string
	PlayerID int
}

func (s *Service) espnRequired() error {
	if s.espn == nil {
		return fmt.Errorf("ESPN not configured for this service: call WithESPN/WithESPNClient first")
	}
	return nil
}

func espnTeamNames(l espn.League) map[int]string {
	names := make(map[int]string, len(l.Teams))
	for _, t := range l.Teams {
		names[t.ID] = t.Name()
	}
	return names
}

// HistoricalSeasons lists every ESPN season this league has, most recent
// first. It's ESPN's equivalent of Seasons(): ESPN keeps one league ID for
// the league's entire lifetime and indexes seasons by year underneath it,
// rather than minting a new league ID each year the way Sleeper does, so
// there's no chain to walk — just one history endpoint to ask.
func (s *Service) HistoricalSeasons() ([]int, error) {
	if err := s.espnRequired(); err != nil {
		return nil, err
	}
	leagues, err := s.espn.LeagueHistory(s.espnLeagueID)
	if err != nil {
		return nil, err
	}
	years := make([]int, len(leagues))
	for i, l := range leagues {
		years[i] = l.SeasonID
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years, nil
}

// HistoricalStandings returns every team's final record for the given
// ESPN-era season, ranked by wins then points for (same tiebreaker as
// Standings).
func (s *Service) HistoricalStandings(season int) ([]HistoricalStanding, error) {
	if err := s.espnRequired(); err != nil {
		return nil, err
	}
	l, err := s.espn.League(s.espnLeagueID, season)
	if err != nil {
		return nil, err
	}

	rows := make([]HistoricalStanding, 0, len(l.Teams))
	for _, t := range l.Teams {
		rows = append(rows, HistoricalStanding{
			Team:          t.Name(),
			Wins:          t.Record.Overall.Wins,
			Losses:        t.Record.Overall.Losses,
			Ties:          t.Record.Overall.Ties,
			PointsFor:     t.Record.Overall.PointsFor,
			PointsAgainst: t.Record.Overall.PointsAgainst,
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

// HistoricalMatchups returns the given ESPN-era season's schedule for the
// given week.
func (s *Service) HistoricalMatchups(season, week int) ([]HistoricalMatchup, error) {
	if err := s.espnRequired(); err != nil {
		return nil, err
	}
	l, err := s.espn.League(s.espnLeagueID, season)
	if err != nil {
		return nil, err
	}
	names := espnTeamNames(l)

	var matchups []HistoricalMatchup
	for _, m := range l.Schedule {
		if m.MatchupPeriodID != week {
			continue
		}
		matchups = append(matchups, HistoricalMatchup{
			Week:       week,
			Home:       names[m.Home.TeamID],
			HomePoints: m.Home.TotalPoints,
			Away:       names[m.Away.TeamID],
			AwayPoints: m.Away.TotalPoints,
		})
	}
	return matchups, nil
}

// HistoricalDraft returns the given ESPN-era season's draft picks in draft
// order.
func (s *Service) HistoricalDraft(season int) ([]HistoricalDraftPick, error) {
	if err := s.espnRequired(); err != nil {
		return nil, err
	}
	l, err := s.espn.League(s.espnLeagueID, season)
	if err != nil {
		return nil, err
	}
	names := espnTeamNames(l)

	picks := make([]HistoricalDraftPick, 0, len(l.DraftDetail.Picks))
	for _, p := range l.DraftDetail.Picks {
		picks = append(picks, HistoricalDraftPick{
			Round:    p.RoundID,
			Pick:     p.RoundPickNumber,
			Overall:  p.OverallPickNumber,
			Team:     names[p.TeamID],
			PlayerID: p.PlayerID,
		})
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].Overall < picks[j].Overall })
	return picks, nil
}

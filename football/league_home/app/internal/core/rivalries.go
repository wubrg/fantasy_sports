package core

import (
	"embed"
	"encoding/json"
)

//go:embed data/rivalries.json
var rivalriesFS embed.FS

// Rivalry is one pair of managers' all-time head-to-head record.
//
// data/rivalries.json ships empty: computing this for real requires
// walking each season's previous_league_id chain back through Sleeper and
// aggregating every head-to-head matchup, which needs live Sleeper API
// access this environment doesn't have. The schema and Rivalries() call
// exist now so the Discord bot/web UI can build against them; a sync job
// fills the data in once it can reach Sleeper.
type Rivalry struct {
	ManagerAID string  `json:"manager_a_id"`
	ManagerBID string  `json:"manager_b_id"`
	WinsA      int     `json:"wins_a"`
	WinsB      int     `json:"wins_b"`
	Ties       int     `json:"ties"`
	PointsForA float64 `json:"points_for_a"`
	PointsForB float64 `json:"points_for_b"`
}

// Rivalries returns every pair of managers' all-time head-to-head record.
func (s *Service) Rivalries() ([]Rivalry, error) {
	raw, err := rivalriesFS.ReadFile("data/rivalries.json")
	if err != nil {
		return nil, err
	}
	var r []Rivalry
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return r, nil
}

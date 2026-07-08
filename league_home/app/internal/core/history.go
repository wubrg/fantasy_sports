package core

import (
	"embed"
	"encoding/json"
)

//go:embed data/history.json
var historyFS embed.FS

// AwardSeason is one season's named-award winners, transcribed from
// league_fees_and_dues.md's Award History table.
type AwardSeason struct {
	Season             int    `json:"season"`
	GrandChampion      string `json:"grand_champion"`
	FirstOfTheWorst    string `json:"first_of_the_worst"`
	PointsFarmer       string `json:"points_farmer"`
	TheTaco            string `json:"the_taco"`
	TheScrubLife       string `json:"the_scrub_life"`
	LadyLuck           string `json:"lady_luck"`
	TheEli             string `json:"the_eli"`
	TheStrongMan       string `json:"the_strong_man"`
	ToiletBowlChampion string `json:"toilet_bowl_champion"`
	SackO              string `json:"sacko"`
	Note               string `json:"note,omitempty"`
}

// RoleYear is one year's league governance roster, transcribed from
// league_members.md's League Roles History table.
type RoleYear struct {
	Year             int    `json:"year"`
	LeagueManager    string `json:"league_manager"`
	MoneyCollector   string `json:"money_collector"`
	LeagueAmbassador string `json:"league_ambassador"`
	PollMaster       string `json:"poll_master"`
	Bookie           string `json:"bookie"`
	VP               string `json:"vp"`
}

// History is the full trophy case / governance history dataset.
type History struct {
	Awards []AwardSeason `json:"awards"`
	Roles  []RoleYear    `json:"roles"`
}

// History returns the locally-curated award and governance history. This
// data predates the league's Sleeper usage and isn't available from the
// API, so it's maintained by hand in data/history.json.
func (s *Service) History() (History, error) {
	raw, err := historyFS.ReadFile("data/history.json")
	if err != nil {
		return History{}, err
	}
	var h History
	if err := json.Unmarshal(raw, &h); err != nil {
		return History{}, err
	}
	return h, nil
}

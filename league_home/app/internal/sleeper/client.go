// Package sleeper is a minimal client for the subset of Sleeper's public,
// keyless REST API (https://docs.sleeper.com) the league home tool needs:
// league settings, rosters, users, matchups, and the current NFL state.
package sleeper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.sleeper.app/v1"

// Client talks to the Sleeper API. BaseURL is overridable so tests can point
// it at an httptest server instead of the real API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New returns a Client configured for the real Sleeper API.
func New() *Client {
	return &Client{
		BaseURL:    defaultBaseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// League describes the subset of league settings this tool needs.
type League struct {
	LeagueID         string             `json:"league_id"`
	Name             string             `json:"name"`
	Season           string             `json:"season"`
	Status           string             `json:"status"`
	PreviousLeagueID string             `json:"previous_league_id"`
	ScoringSettings  map[string]float64 `json:"scoring_settings"`
	Settings         struct {
		WaiverBudget     int `json:"waiver_budget"`
		PlayoffWeekStart int `json:"playoff_week_start"`
	} `json:"settings"`
}

// Roster is one team's roster, record, and waiver budget usage.
type Roster struct {
	RosterID int    `json:"roster_id"`
	OwnerID  string `json:"owner_id"`
	Settings struct {
		Wins             int `json:"wins"`
		Losses           int `json:"losses"`
		Ties             int `json:"ties"`
		FPts             int `json:"fpts"`
		FPtsDecimal      int `json:"fpts_decimal"`
		FPtsAgainst      int `json:"fpts_against"`
		FPtsAgainstDec   int `json:"fpts_against_decimal"`
		WaiverBudgetUsed int `json:"waiver_budget_used"`
	} `json:"settings"`
}

// PointsFor returns the roster's total points for as a float (fpts.fpts_decimal).
func (r Roster) PointsFor() float64 {
	return float64(r.Settings.FPts) + float64(r.Settings.FPtsDecimal)/100
}

// PointsAgainst returns the roster's total points against as a float.
func (r Roster) PointsAgainst() float64 {
	return float64(r.Settings.FPtsAgainst) + float64(r.Settings.FPtsAgainstDec)/100
}

// User is a league member account.
type User struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Metadata    struct {
		TeamName string `json:"team_name"`
	} `json:"metadata"`
}

// TeamName returns the user's custom team name, falling back to their
// display name if they haven't set one.
func (u User) TeamName() string {
	if u.Metadata.TeamName != "" {
		return u.Metadata.TeamName
	}
	return u.DisplayName
}

// Matchup is one roster's side of a head-to-head matchup in a given week.
// Two entries share the same MatchupID.
type Matchup struct {
	RosterID  int     `json:"roster_id"`
	MatchupID int     `json:"matchup_id"`
	Points    float64 `json:"points"`
}

// NFLState describes the current NFL season/week, used to default
// "what week is it" without the caller having to pass one in.
type NFLState struct {
	Season      string `json:"season"`
	SeasonType  string `json:"season_type"`
	Week        int    `json:"week"`
	DisplayWeek int    `json:"display_week"`
}

func (c *Client) get(path string, out interface{}) error {
	resp, err := c.HTTPClient.Get(c.BaseURL + path)
	if err != nil {
		return fmt.Errorf("sleeper: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sleeper: GET %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("sleeper: GET %s: decoding response: %w", path, err)
	}
	return nil
}

// League fetches league settings for leagueID.
func (c *Client) League(leagueID string) (League, error) {
	var l League
	err := c.get("/league/"+leagueID, &l)
	return l, err
}

// Rosters fetches every roster in leagueID.
func (c *Client) Rosters(leagueID string) ([]Roster, error) {
	var rosters []Roster
	err := c.get("/league/"+leagueID+"/rosters", &rosters)
	return rosters, err
}

// Users fetches every user (league member) in leagueID.
func (c *Client) Users(leagueID string) ([]User, error) {
	var users []User
	err := c.get("/league/"+leagueID+"/users", &users)
	return users, err
}

// Matchups fetches every roster's matchup entry for the given week.
func (c *Client) Matchups(leagueID string, week int) ([]Matchup, error) {
	var matchups []Matchup
	err := c.get(fmt.Sprintf("/league/%s/matchups/%d", leagueID, week), &matchups)
	return matchups, err
}

// State fetches the current NFL season/week.
func (c *Client) State() (NFLState, error) {
	var s NFLState
	err := c.get("/state/nfl", &s)
	return s, err
}

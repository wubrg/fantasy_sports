// Package espn is a minimal client for ESPN's fantasy football v3 API,
// used to pull a league's pre-Sleeper history (ESPN seasons before the
// league migrated, see league_home/README.md). Unlike Sleeper's public API,
// ESPN's requires auth for private leagues: the espn_s2 and SWID cookie
// values from a logged-in league member's browser session. There is no
// supported way to obtain those programmatically; a human has to copy them
// out of their browser's dev tools (Application > Cookies on
// fantasy.espn.com) and supply them via Client's S2/SWID fields.
package espn

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "https://fantasy.espn.com/apis/v3/games/ffl"

// Client talks to the ESPN fantasy football API. BaseURL is overridable so
// tests can point it at an httptest server instead of the real API. S2 and
// SWID are the espn_s2/SWID cookie values required to read a private
// league; leave both empty for a public league.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	S2         string
	SWID       string
}

// New returns a Client configured for the real ESPN API, authenticated with
// the given espn_s2/SWID cookie values (pass "", "" for a public league).
func New(s2, swid string) *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		// ESPN redirects (rather than 401s) an unauthenticated request for a
		// private league to a login page, which would otherwise come back
		// as a misleading "200 OK" full of HTML once http.Client follows
		// the redirect. Disabling redirects surfaces that as the 3xx it
		// actually is so get() can report it as an auth failure.
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		S2:   s2,
		SWID: swid,
	}
}

// League describes the subset of a season's league data this tool needs:
// final standings, the full matchup schedule, and the draft.
type League struct {
	ID          int         `json:"id"`
	SeasonID    int         `json:"seasonId"`
	Teams       []Team      `json:"teams"`
	Schedule    []Matchup   `json:"schedule"`
	DraftDetail DraftDetail `json:"draftDetail"`
}

// Team is one team's identity and season record.
type Team struct {
	ID       int      `json:"id"`
	Location string   `json:"location"`
	Nickname string   `json:"nickname"`
	Owners   []string `json:"owners"`
	Record   Record   `json:"record"`
}

// Name returns the team's display name (ESPN splits it into two fields).
func (t Team) Name() string {
	name := t.Location
	if t.Nickname != "" {
		if name != "" {
			name += " "
		}
		name += t.Nickname
	}
	return name
}

// Record holds a team's overall win-loss-tie record and point totals.
type Record struct {
	Overall RecordSplit `json:"overall"`
}

// RecordSplit is one split (overall/home/away) of a team's record.
type RecordSplit struct {
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	Ties          int     `json:"ties"`
	PointsFor     float64 `json:"pointsFor"`
	PointsAgainst float64 `json:"pointsAgainst"`
}

// Matchup is one head-to-head pairing in the season schedule.
// MatchupPeriodID is ESPN's week number.
type Matchup struct {
	MatchupPeriodID int       `json:"matchupPeriodId"`
	Home            TeamScore `json:"home"`
	Away            TeamScore `json:"away"`
}

// TeamScore is one side of a Matchup.
type TeamScore struct {
	TeamID      int     `json:"teamId"`
	TotalPoints float64 `json:"totalPoints"`
}

// DraftDetail holds the season's draft results, if it has happened.
type DraftDetail struct {
	Drafted bool   `json:"drafted"`
	Picks   []Pick `json:"picks"`
}

// Pick is one draft selection. ESPN identifies the player only by
// PlayerID; resolving it to a name requires a separate, unimplemented
// lookup against ESPN's player database.
type Pick struct {
	TeamID            int `json:"teamId"`
	PlayerID          int `json:"playerId"`
	RoundID           int `json:"roundId"`
	RoundPickNumber   int `json:"roundPickNumber"`
	OverallPickNumber int `json:"overallPickNumber"`
}

// views are the ESPN response "views" needed to populate everything League
// holds: mTeam for teams/records, mMatchup+mMatchupScore for the schedule,
// mDraftDetail for the draft.
var views = []string{"mTeam", "mMatchup", "mMatchupScore", "mDraftDetail"}

func (c *Client) get(path string, query string, out interface{}) error {
	url := c.BaseURL + path
	if query != "" {
		url += "?" + query
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("espn: GET %s: %w", path, err)
	}
	if c.S2 != "" && c.SWID != "" {
		req.AddCookie(&http.Cookie{Name: "espn_s2", Value: c.S2})
		req.AddCookie(&http.Cookie{Name: "SWID", Value: c.SWID})
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("espn: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("espn: GET %s: unauthorized (this league is private; set espn_s2/SWID)", path)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("espn: GET %s: redirected to %s (this league is private and needs valid espn_s2/SWID, or they've expired)", path, resp.Header.Get("Location"))
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("espn: GET %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("espn: GET %s: decoding response: %w", path, err)
	}
	return nil
}

func viewQuery() string {
	q := ""
	for i, v := range views {
		if i > 0 {
			q += "&"
		}
		q += "view=" + v
	}
	return q
}

// League fetches one season's league data.
func (c *Client) League(leagueID string, season int) (League, error) {
	var l League
	err := c.get(fmt.Sprintf("/seasons/%d/segments/0/leagues/%s", season, leagueID), viewQuery(), &l)
	return l, err
}

// LeagueHistory fetches every season this league ID has on ESPN, most
// recent first. This is ESPN's equivalent of walking Sleeper's
// previous_league_id chain: ESPN keeps one league ID for the league's
// entire lifetime and indexes seasons by year underneath it instead of
// minting a new ID each year.
func (c *Client) LeagueHistory(leagueID string) ([]League, error) {
	var leagues []League
	err := c.get("/leagueHistory/"+leagueID, viewQuery(), &leagues)
	return leagues, err
}

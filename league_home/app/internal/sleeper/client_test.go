package sleeper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, routes map[string]interface{}) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
}

func TestLeague(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/league/123": map[string]interface{}{
			"league_id": "123",
			"name":      "Hit or Miss",
			"season":    "2024",
			"settings": map[string]interface{}{
				"waiver_budget":      100,
				"playoff_week_start": 15,
			},
		},
	})

	l, err := c.League("123")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "Hit or Miss" || l.Settings.WaiverBudget != 100 {
		t.Errorf("unexpected league: %+v", l)
	}
}

func TestRostersAndUsers(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/league/123/rosters": []map[string]interface{}{
			{"roster_id": 1, "owner_id": "u1", "settings": map[string]interface{}{
				"wins": 5, "losses": 2, "ties": 0, "fpts": 543, "fpts_decimal": 24,
				"fpts_against": 500, "fpts_against_decimal": 10, "waiver_budget_used": 23,
			}},
		},
		"/league/123/users": []map[string]interface{}{
			{"user_id": "u1", "display_name": "adam", "metadata": map[string]interface{}{"team_name": "Team Adam"}},
		},
	})

	rosters, err := c.Rosters("123")
	if err != nil {
		t.Fatal(err)
	}
	if len(rosters) != 1 || rosters[0].PointsFor() != 543.24 {
		t.Errorf("unexpected rosters: %+v", rosters)
	}

	users, err := c.Users("123")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].TeamName() != "Team Adam" {
		t.Errorf("unexpected users: %+v", users)
	}
}

func TestUserTeamNameFallsBackToDisplayName(t *testing.T) {
	u := User{DisplayName: "adam"}
	if u.TeamName() != "adam" {
		t.Errorf("expected fallback to display name, got %q", u.TeamName())
	}
}

func TestMatchups(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/league/123/matchups/1": []map[string]interface{}{
			{"roster_id": 1, "matchup_id": 1, "points": 102.5},
			{"roster_id": 2, "matchup_id": 1, "points": 98.2},
		},
	})

	matchups, err := c.Matchups("123", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchups) != 2 {
		t.Errorf("expected 2 matchup entries, got %d", len(matchups))
	}
}

func TestNotFoundReturnsError(t *testing.T) {
	c := testClient(t, map[string]interface{}{})
	if _, err := c.League("missing"); err == nil {
		t.Error("expected error for unknown league, got nil")
	}
}

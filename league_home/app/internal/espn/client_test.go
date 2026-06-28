package espn

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
		"/seasons/2020/segments/0/leagues/56226": map[string]interface{}{
			"id":       56226,
			"seasonId": 2020,
			"teams": []map[string]interface{}{
				{
					"id": 1, "location": "Hit or", "nickname": "Miss",
					"owners": []string{"{abc-123}"},
					"record": map[string]interface{}{
						"overall": map[string]interface{}{
							"wins": 10, "losses": 4, "ties": 0,
							"pointsFor": 1500.5, "pointsAgainst": 1300.25,
						},
					},
				},
			},
			"schedule": []map[string]interface{}{
				{
					"matchupPeriodId": 1,
					"home":            map[string]interface{}{"teamId": 1, "totalPoints": 110.5},
					"away":            map[string]interface{}{"teamId": 2, "totalPoints": 98.25},
				},
			},
			"draftDetail": map[string]interface{}{
				"drafted": true,
				"picks": []map[string]interface{}{
					{"teamId": 1, "playerId": 4040, "roundId": 1, "roundPickNumber": 1, "overallPickNumber": 1},
				},
			},
		},
	})

	l, err := c.League("56226", 2020)
	if err != nil {
		t.Fatal(err)
	}
	if l.SeasonID != 2020 || len(l.Teams) != 1 {
		t.Fatalf("unexpected league: %+v", l)
	}
	if l.Teams[0].Name() != "Hit or Miss" {
		t.Errorf("expected team name %q, got %q", "Hit or Miss", l.Teams[0].Name())
	}
	if l.Teams[0].Record.Overall.Wins != 10 {
		t.Errorf("unexpected record: %+v", l.Teams[0].Record)
	}
	if len(l.Schedule) != 1 || l.Schedule[0].Home.TotalPoints != 110.5 {
		t.Errorf("unexpected schedule: %+v", l.Schedule)
	}
	if !l.DraftDetail.Drafted || len(l.DraftDetail.Picks) != 1 {
		t.Errorf("unexpected draft detail: %+v", l.DraftDetail)
	}
}

func TestLeagueHistory(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/leagueHistory/56226": []map[string]interface{}{
			{"id": 56226, "seasonId": 2020},
			{"id": 56226, "seasonId": 2019},
		},
	})

	leagues, err := c.LeagueHistory("56226")
	if err != nil {
		t.Fatal(err)
	}
	if len(leagues) != 2 || leagues[0].SeasonID != 2020 || leagues[1].SeasonID != 2019 {
		t.Errorf("unexpected league history: %+v", leagues)
	}
}

func TestUnauthorizedReturnsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	_, err := c.League("56226", 2020)
	if err == nil {
		t.Fatal("expected an error for an unauthorized request, got nil")
	}
}

func TestRedirectReturnsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://www.espn.com/fantasy/")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	c := New("", "")
	c.BaseURL = srv.URL

	_, err := c.League("56226", 2020)
	if err == nil {
		t.Fatal("expected an error for a redirected (unauthenticated) request, got nil")
	}
}

func TestAuthCookiesAreSent(t *testing.T) {
	var gotS2, gotSWID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("espn_s2"); err == nil {
			gotS2 = c.Value
		}
		if c, err := r.Cookie("SWID"); err == nil {
			gotSWID = c.Value
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": 56226, "seasonId": 2020})
	}))
	defer srv.Close()

	c := New("s2-value", "{swid-value}")
	c.BaseURL = srv.URL
	if _, err := c.League("56226", 2020); err != nil {
		t.Fatal(err)
	}
	if gotS2 != "s2-value" || gotSWID != "{swid-value}" {
		t.Errorf("expected auth cookies to be sent, got espn_s2=%q SWID=%q", gotS2, gotSWID)
	}
}

package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"leaguehome/internal/sleeper"
)

func testService(t *testing.T, routes map[string]interface{}) *Service {
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
	return NewWithClient("123", &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()})
}

func rosterRow(id int, owner string, wins, losses int, fpts float64) map[string]interface{} {
	return map[string]interface{}{
		"roster_id": id, "owner_id": owner,
		"settings": map[string]interface{}{
			"wins": wins, "losses": losses, "ties": 0,
			"fpts": int(fpts), "fpts_decimal": 0,
			"fpts_against": 0, "fpts_against_decimal": 0,
			"waiver_budget_used": 10 * id,
		},
	}
}

func userRow(id, team string) map[string]interface{} {
	return map[string]interface{}{
		"user_id": id, "display_name": id,
		"metadata": map[string]interface{}{"team_name": team},
	}
}

func TestStandingsRanksByWinsThenPoints(t *testing.T) {
	s := testService(t, map[string]interface{}{
		"/league/123/rosters": []map[string]interface{}{
			rosterRow(1, "u1", 5, 2, 500),
			rosterRow(2, "u2", 6, 1, 480),
		},
		"/league/123/users": []map[string]interface{}{
			userRow("u1", "Team One"),
			userRow("u2", "Team Two"),
		},
	})

	rows, err := s.Standings()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Team != "Team Two" {
		t.Errorf("expected Team Two (6 wins) ranked first, got %+v", rows)
	}
}

func TestFaabRemainingBudget(t *testing.T) {
	s := testService(t, map[string]interface{}{
		"/league/123": map[string]interface{}{
			"league_id": "123",
			"settings":  map[string]interface{}{"waiver_budget": 100},
		},
		"/league/123/rosters": []map[string]interface{}{
			rosterRow(1, "u1", 0, 0, 0),
		},
		"/league/123/users": []map[string]interface{}{
			userRow("u1", "Team One"),
		},
	})

	rows, err := s.Faab()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Remaining != 90 {
		t.Errorf("expected 90 remaining (100 budget - 10 used), got %+v", rows)
	}
}

func TestMatchupsPairsRostersByMatchupID(t *testing.T) {
	s := testService(t, map[string]interface{}{
		"/league/123/matchups/3": []map[string]interface{}{
			{"roster_id": 1, "matchup_id": 1, "points": 100.0},
			{"roster_id": 2, "matchup_id": 1, "points": 90.0},
		},
		"/league/123/rosters": []map[string]interface{}{
			rosterRow(1, "u1", 0, 0, 0),
			rosterRow(2, "u2", 0, 0, 0),
		},
		"/league/123/users": []map[string]interface{}{
			userRow("u1", "Team One"),
			userRow("u2", "Team Two"),
		},
	})

	rows, err := s.Matchups(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Home != "Team One" || rows[0].Away != "Team Two" {
		t.Errorf("unexpected matchup: %+v", rows)
	}
}

func TestRulesLoadsEmbeddedData(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	r, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if r.Roster.BenchSlots != 5 || r.Keepers.MaxKeepers != 2 {
		t.Errorf("unexpected rules: %+v", r)
	}
}

// TestKeeperPricingFormulaMatchesDraftDocExamples checks the formula
// reproduces every row of draft.md's worked-example pricing table.
func TestKeeperPricingFormulaMatchesDraftDocExamples(t *testing.T) {
	k := KeeperRules{MinimumValue: 10, IncrementPerKeepCount: 5}

	cases := []struct {
		previousValue, keepCount, want int
	}{
		{1, 1, 10},  // first time, under minimum
		{7, 1, 12},  // first time, under minimum
		{10, 1, 15}, // first time
		{15, 2, 25}, // second time
		{25, 3, 40}, // third time
	}
	for _, c := range cases {
		got := k.NewValue(c.previousValue, c.keepCount)
		if got != c.want {
			t.Errorf("NewValue(%d, %d) = %d, want %d", c.previousValue, c.keepCount, got, c.want)
		}
	}
}

func TestHistoryLoadsEmbeddedData(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	h, err := s.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Awards) == 0 || len(h.Roles) == 0 {
		t.Errorf("expected non-empty history, got %+v", h)
	}
	if h.Awards[0].Season != 2014 {
		t.Errorf("expected first award season 2014, got %d", h.Awards[0].Season)
	}
}

func TestManagersLoadsEmbeddedData(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	managers, err := s.Managers()
	if err != nil {
		t.Fatal(err)
	}
	if len(managers) == 0 {
		t.Fatal("expected non-empty managers")
	}
}

func TestResolveManagerMatchesAlias(t *testing.T) {
	managers := []Manager{
		{ID: "chris-buschjost", Name: "Chris Buschjost", Aliases: []string{"Chris Bushjost"}},
	}

	m, ok := ResolveManager(managers, "chris bushjost")
	if !ok || m.ID != "chris-buschjost" {
		t.Errorf("expected alias match to resolve to chris-buschjost, got %+v, ok=%v", m, ok)
	}

	if _, ok := ResolveManager(managers, "nobody"); ok {
		t.Error("expected no match for unknown name")
	}
}

func TestAnnouncementsLoadsEmbeddedDataMostRecentFirst(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	rows, err := s.Announcements()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatal("expected at least 2 announcements")
	}
	if rows[0].PostedAt < rows[1].PostedAt {
		t.Errorf("expected most recent first, got %+v", rows)
	}
}

func TestScheduleLoadsEmbeddedData(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	rows, err := s.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected non-empty schedule")
	}
}

func TestScoringGroupsByCategoryAndRoundsFloatArtifacts(t *testing.T) {
	s := testService(t, map[string]interface{}{
		"/league/123": map[string]interface{}{
			"league_id": "123",
			"scoring_settings": map[string]interface{}{
				"pass_yd":             0.03999999910593033, // Sleeper's float32 -> float64 artifact for 0.04
				"pass_td":             4,
				"rec":                 0.5,
				"made_up_future_stat": 3, // unlabeled key should still surface, under "Other"
			},
		},
	})

	categories, err := s.Scoring()
	if err != nil {
		t.Fatal(err)
	}

	byName := make(map[string]ScoringCategory)
	for _, c := range categories {
		byName[c.Name] = c
	}

	passing, ok := byName["Passing"]
	if !ok || len(passing.Entries) != 2 {
		t.Fatalf("expected 2 Passing entries, got %+v", passing)
	}
	for _, e := range passing.Entries {
		if e.Key == "pass_yd" && e.Points != 0.04 {
			t.Errorf("expected pass_yd rounded to 0.04, got %v", e.Points)
		}
	}

	if _, ok := byName["Receiving"]; !ok {
		t.Error("expected a Receiving category for the rec key")
	}

	other, ok := byName["Other"]
	if !ok || len(other.Entries) != 1 || other.Entries[0].Label != "made_up_future_stat" {
		t.Errorf("expected unlabeled key to fall into Other with its raw key as label, got %+v", other)
	}
}

func TestRivalriesLoadsEmbeddedData(t *testing.T) {
	s := NewWithClient("123", sleeper.New())

	rows, err := s.Rivalries()
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil {
		t.Error("expected non-nil (possibly empty) rivalries slice")
	}
}

func TestSeasonsWalksPreviousLeagueIDChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leagues := map[string]map[string]interface{}{
			"/league/3": {"league_id": "3", "season": "2026", "status": "pre_draft", "previous_league_id": "2"},
			"/league/2": {"league_id": "2", "season": "2025", "status": "complete", "previous_league_id": "1"},
			"/league/1": {"league_id": "1", "season": "2024", "status": "complete", "previous_league_id": "0"},
		}
		body, ok := leagues[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	s := NewWithClient("3", &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()})

	seasons, err := s.Seasons()
	if err != nil {
		t.Fatal(err)
	}
	want := []Season{
		{LeagueID: "3", Season: "2026", Status: "pre_draft"},
		{LeagueID: "2", Season: "2025", Status: "complete"},
		{LeagueID: "1", Season: "2024", Status: "complete"},
	}
	if len(seasons) != len(want) {
		t.Fatalf("expected %d seasons, got %+v", len(want), seasons)
	}
	for i, s := range seasons {
		if s != want[i] {
			t.Errorf("season %d: got %+v, want %+v", i, s, want[i])
		}
	}
}

func TestSeasonsStopsWhenPreviousLeagueIDMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No previous_league_id field at all, the terminator for the
		// oldest leagues Sleeper hosts (predates the field's existence).
		json.NewEncoder(w).Encode(map[string]interface{}{
			"league_id": "1", "season": "2019", "status": "complete",
		})
	}))
	t.Cleanup(srv.Close)
	s := NewWithClient("1", &sleeper.Client{BaseURL: srv.URL, HTTPClient: srv.Client()})

	seasons, err := s.Seasons()
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 1 {
		t.Fatalf("expected chain to stop after 1 season, got %+v", seasons)
	}
}

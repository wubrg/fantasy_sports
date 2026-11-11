package board

import (
	"reflect"
	"testing"

	"edge/internal/oddspull"
)

// weekWithAmbiguousNames is a week that has BOTH the Rams and the Chargers on
// the schedule, and both New York clubs: the case where a first word alone
// ("NY", "LA") cannot say which team is meant.
func weekWithAmbiguousNames() *Doc {
	g := func(a, h string) *Game { return &Game{Away: a, Home: h, Books: map[string]Lines{"draftkings": {}}} }
	return &Doc{Season: 2026, Week: 10, Games: map[string]*Game{
		"2026_10_WAS_NYG": g("WAS", "NYG"),
		"2026_10_BUF_NYJ": g("BUF", "NYJ"),
		"2026_10_LAC_BAL": g("LAC", "BAL"),
		"2026_10_LA_ARI":  g("LA", "ARI"),
	}}
}

func TestMatchEventResolvesTheAmbiguousFullNames(t *testing.T) {
	d := weekWithAmbiguousNames()
	cases := []struct{ event, id string }{
		{"WAS Commanders @ NY Giants", "2026_10_WAS_NYG"},
		{"BUF Bills @ NY Jets", "2026_10_BUF_NYJ"},
		{"LA Chargers @ BAL Ravens", "2026_10_LAC_BAL"},
		{"LA Rams @ ARI Cardinals", "2026_10_LA_ARI"},
		{"ny giants @ was commanders", "2026_10_WAS_NYG"}, // case, and the reversed order
	}
	for _, c := range cases {
		id, _, _, _, err := d.matchEvent(c.event)
		if err != nil || id != c.id {
			t.Errorf("matchEvent(%q) = %q, %v; want %q", c.event, id, err, c.id)
		}
		if !d.HasMatchup(c.event) {
			t.Errorf("HasMatchup(%q) = false", c.event)
		}
	}
	if d.HasMatchup("DAL Cowboys @ NY Giants") {
		t.Error("DAL @ NYG is not on this week's schedule")
	}
}

func TestTeamsMentionedDoesNotReadLAChargersAsTheRams(t *testing.T) {
	d := weekWithAmbiguousNames()
	cases := []struct {
		in   string
		want []string
	}{
		{"LA Chargers +4.5", []string{"LAC"}}, // not [LA, LAC]
		{"LA Rams -9.5", []string{"LA"}},
		{"NY Giants", []string{"NYG"}},
		{"NY Jets +7.5", []string{"NYJ"}},
	}
	for _, c := range cases {
		if got := d.TeamsMentioned(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("TeamsMentioned(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// The sync path needs both halves: the event resolves AND each side's label
// names exactly one team, so a "NY Giants"/"LA Chargers" game is not skipped.
func TestOddsPairsFromOutcomesKeepsNewYorkAndLAChargersGames(t *testing.T) {
	d := weekWithAmbiguousNames()
	outs := []oddspull.Outcome{
		{Event: "LA Chargers @ BAL Ravens", Market: "Moneyline", Selection: "LA Chargers", Price: 185, Category: "Game", MarketID: "a"},
		{Event: "LA Chargers @ BAL Ravens", Market: "Moneyline", Selection: "BAL Ravens", Price: -230, Category: "Game", MarketID: "a"},
		{Event: "WAS Commanders @ NY Giants", Market: "Moneyline", Selection: "WAS Commanders", Price: 120, Category: "Game", MarketID: "b"},
		{Event: "WAS Commanders @ NY Giants", Market: "Moneyline", Selection: "NY Giants", Price: -140, Category: "Game", MarketID: "b"},
	}
	pairs := d.OddsPairsFromOutcomes(outs)
	if len(pairs) != 2 {
		t.Fatalf("want a pair for each of the two games, got %d: %+v", len(pairs), pairs)
	}
	for _, p := range pairs {
		switch p.GameID {
		case "2026_10_LAC_BAL":
			if p.AwayPrice != 185 || p.HomePrice != -230 {
				t.Errorf("LAC @ BAL prices wrong: %+v", p)
			}
		case "2026_10_WAS_NYG":
			if p.AwayPrice != 120 || p.HomePrice != -140 {
				t.Errorf("WAS @ NYG prices wrong: %+v", p)
			}
		default:
			t.Errorf("unexpected game %q", p.GameID)
		}
	}
}

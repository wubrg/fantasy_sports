package draft

import (
	"testing"

	"leaguehome/internal/sleeper"
)

func priced(pos string, costs ...int) []PlayerSignals {
	var out []PlayerSignals
	for i, c := range costs {
		out = append(out, PlayerSignals{
			PlayerID: pos + string(rune('a'+i)), Position: pos, Cost: c,
		})
	}
	return out
}

// TestLinesReadTheWholePositionNotWhatIsLeft is the property that makes this
// worth computing live.
//
// A back already gone still cost what he cost. Reading the ladder off the
// remaining board alone would have the top-three line describing whoever is
// left over after the top three sold, which is a different question and the
// wrong answer to this one.
func TestLinesReadTheWholePositionNotWhatIsLeft(t *testing.T) {
	remaining := priced("RB", 30, 25, 20, 15, 10)
	gone := map[string][]int{"RB": {90, 80, 70}}

	got := PriceLines(remaining, gone, nil)["RB"]
	// Whole position descending: 90 80 70 30 25 20 15 10
	if got.Live[0] != 70 {
		t.Errorf("top-3 line = $%d, want $70 — the third dearest back overall", got.Live[0])
	}
	if got.Live[1] != 25 {
		t.Errorf("top-5 line = $%d, want $25", got.Live[1])
	}

	// And without the sold backs the same board reads completely differently,
	// which is the mistake being avoided.
	bare := PriceLines(remaining, nil, nil)["RB"]
	if bare.Live[0] == got.Live[0] {
		t.Error("the sold backs made no difference, so they are not being counted")
	}
}

// TestSoldPriceBeatsTheBoardsGuess — a player who went for $95 against a $68
// cost sets the line at what the room actually paid.
func TestSoldPriceBeatsTheBoardsGuess(t *testing.T) {
	got := PriceLines(priced("RB", 68, 60, 55), map[string][]int{"RB": {95}}, nil)["RB"]
	if got.Live[0] != 60 {
		t.Errorf("top-3 = $%d, want $60 from the ladder 95/68/60", got.Live[0])
	}
}

// TestATierDeeperThanThePositionIsBlank — "the top-twelve line is $1" would
// be true of a four-man position and would mean nothing.
func TestATierDeeperThanThePositionIsBlank(t *testing.T) {
	got := PriceLines(priced("TE", 40, 20, 10, 5), nil, nil)["TE"]
	if got.Live[0] != 10 {
		t.Errorf("top-3 = $%d, want $10", got.Live[0])
	}
	if got.Live[2] != 0 || got.Live[3] != 0 {
		t.Errorf("tiers past the position report %v, want zeroes", got.Live)
	}
}

// TestPositionsAreReadSeparately — the allocation that produces Cost sorts
// every player together regardless of position, so the only thing keeping a
// cheap position from being drowned by an expensive one is that the ladder
// is read within each.
func TestPositionsAreReadSeparately(t *testing.T) {
	players := append(priced("RB", 70, 65, 60, 55, 50), priced("TE", 30, 8, 6, 4, 2)...)
	got := PriceLines(players, nil, nil)

	if got["TE"].Live[0] != 6 {
		t.Errorf("TE top-3 = $%d, want $6 — not a rank from the combined board", got["TE"].Live[0])
	}
	if got["RB"].Live[0] != 60 {
		t.Errorf("RB top-3 = $%d, want $60", got["RB"].Live[0])
	}
}

func linePick(id string, amount string) sleeper.DraftPick {
	p := sleeper.DraftPick{PlayerID: id}
	p.Metadata.Amount = amount
	p.PickedBy = "owner-" + id
	// Keepers, because the history guard skips the inaugural draft by
	// counting them and a fixture of pure auction picks looks like 2021.
	p.IsKeeper = true
	return p
}

// TestHistoryTakesTheMedianAcrossSeasons — one season where the room went mad
// at a position should not drag the line it is compared against.
func TestHistoryTakesTheMedianAcrossSeasons(t *testing.T) {
	posOf := func(string) string { return "RB" }
	mk := func(year string, amounts ...string) SeasonData {
		s := SeasonData{Year: year}
		for i, a := range amounts {
			s.Picks = append(s.Picks, linePick(year+string(rune('a'+i)), a))
		}
		return s
	}
	seasons := []SeasonData{
		mk("2023", "60", "50", "40", "30", "20"),
		mk("2024", "62", "52", "42", "32", "22"),
		mk("2025", "200", "190", "180", "170", "160"), // the mad one
	}
	got := HistoricalPriceLines(seasons, posOf, 0)["RB"]
	if got[0] != 42 {
		t.Errorf("top-3 line = $%d, want the median 42 rather than a mean dragged by 2025", got[0])
	}
}

// TestHistorySkipsSeasonsWhoseMoneyNeverReachedTheTable.
//
// 2022 settled at $157 of a $200 budget while the league was still working
// out how keeper money came off the auction. Prices from a draft where a
// third of the money never reached the table cannot be compared with prices
// from one where it did.
func TestHistorySkipsSeasonsWhoseMoneyNeverReachedTheTable(t *testing.T) {
	posOf := func(string) string { return "RB" }
	rich := SeasonData{Year: "2024"}
	poor := SeasonData{Year: "2022"}
	for i := 0; i < 4; i++ {
		p := linePick("r"+string(rune('a'+i)), "100")
		p.PickedBy = "owner"
		rich.Picks = append(rich.Picks, p)
		q := linePick("p"+string(rune('a'+i)), "5")
		q.PickedBy = "owner"
		poor.Picks = append(poor.Picks, q)
	}
	got := HistoricalPriceLines([]SeasonData{rich, poor}, posOf, 100)["RB"]
	if got[0] != 100 {
		t.Errorf("top-3 line = $%d, want $100 — the cheap season should be excluded", got[0])
	}
}

// TestAPositionNobodyBidsOnIsNotCorrelated.
//
// Defenses go for a dollar or four in this league — eleven of them across a
// $3 spread. Their "price rank" is mostly the order the picks happened in,
// so correlating it against a finish produces a confident number about
// nothing. Sleeper does score defenses, so the guard cannot be about missing
// data; it has to be about whether a price ladder exists.
func TestAPositionNobodyBidsOnIsNotCorrelated(t *testing.T) {
	var picks []DraftedPlayer
	for i := 0; i < 12; i++ {
		picks = append(picks, DraftedPlayer{
			Name: "def" + string(rune('a'+i)), Position: "DEF",
			Price: 1 + i%4, Points: float64(100 + i*7),
		})
	}
	got := FitPriceRanks([]TeamSeason{{Season: "2024", Picks: picks}})
	for _, f := range got {
		if f.Position == "DEF" {
			t.Errorf("reported a correlation for a position whose dearest player cost $%d", 4)
		}
	}
}

// TestPriceRankFitFindsARealRelationship, and reports the hit rate beside it.
func TestPriceRankFitFindsARealRelationship(t *testing.T) {
	var picks []DraftedPlayer
	for i := 0; i < 12; i++ {
		// Price and production move together, perfectly.
		picks = append(picks, DraftedPlayer{
			Name: "rb" + string(rune('a'+i)), Position: "RB",
			Price: 60 - i*5, Points: float64(300 - i*20),
		})
	}
	got := FitPriceRanks([]TeamSeason{{Season: "2024", Picks: picks}})
	if len(got) != 1 {
		t.Fatalf("expected one fit, got %d", len(got))
	}
	if got[0].Rho < 0.99 {
		t.Errorf("rho = %.2f on a perfect relationship", got[0].Rho)
	}
	if got[0].TopFive != 5 {
		t.Errorf("top-five hits = %d, want 5", got[0].TopFive)
	}
}

// TestTiedFinishesDoNotInventAnOrder is the bug this had.
//
// Ranking the finishes here rather than handing raw points to Spearman gave
// every player who never scored a distinct made-up finish, in whatever order
// the sort happened to leave them. In a league where a dozen picks score
// nothing that is a lot of invented ordering, and it moved the correlation.
func TestTiedFinishesDoNotInventAnOrder(t *testing.T) {
	var picks []DraftedPlayer
	for i := 0; i < 12; i++ {
		pts := 0.0
		if i < 2 {
			pts = float64(200 - i)
		}
		picks = append(picks, DraftedPlayer{
			Name: "rb" + string(rune('a'+i)), Position: "RB",
			Price: 50 - i*4, Points: pts,
		})
	}
	got := FitPriceRanks([]TeamSeason{{Season: "2024", Picks: picks}})
	if len(got) != 1 {
		t.Fatalf("expected one fit, got %d", len(got))
	}
	// Ten players tied on zero carry no ordering information, so the
	// correlation comes from the two who scored and must stay well short of
	// the perfect relationship a fabricated order would produce.
	if got[0].Rho > 0.85 {
		t.Errorf("rho = %.2f — ten tied zeroes appear to be carrying an order", got[0].Rho)
	}
}

// TestHistorySkipsTheInauguralDraft — 2021 ran with one keeper against
// twenty-odd since, so every elite player was in the pool and the top of
// each ladder priced accordingly. It passes the spend guard comfortably and
// was quietly sitting in the reference line while the correlation printed
// beside it covered 2023-2025 only.
func TestHistorySkipsTheInauguralDraft(t *testing.T) {
	posOf := func(string) string { return "RB" }
	mk := func(year string, keepers bool, amounts ...string) SeasonData {
		s := SeasonData{Year: year}
		for i, a := range amounts {
			p := linePick(year+string(rune('a'+i)), a)
			p.IsKeeper = keepers
			s.Picks = append(s.Picks, p)
		}
		return s
	}
	seasons := []SeasonData{
		mk("2021", false, "200", "190", "180", "170", "160"), // no keepers
		mk("2024", true, "60", "50", "40", "30", "20"),
	}
	got := HistoricalPriceLines(seasons, posOf, 0)["RB"]
	if got[0] != 40 {
		t.Errorf("top-3 line = $%d, want $40 from the keeper-era season alone", got[0])
	}
}

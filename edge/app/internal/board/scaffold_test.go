package board

import (
	"bytes"
	"strings"
	"testing"
)

const csvHead = "game_id,season,game_type,week,gameday,gametime,away_team,home_team," +
	"away_moneyline,home_moneyline,spread_line,away_spread_odds,home_spread_odds," +
	"total_line,over_odds,under_odds\n"

func TestReadSchedule(t *testing.T) {
	src := csvHead +
		"2026_01_NYJ_TEN,2026,REG,1,2026-09-13,13:00,NYJ,TEN,130,-155,2.5,-110,-110,44.5,-110,-110\n" +
		"2026_01_ARI_LAC,2026,REG,1,2026-09-13,16:25,ARI,LAC,455,-625,10.5,-110,-110,46.5,-110,-110\n" +
		"2026_02_GB_MIN,2026,REG,2,2026-09-20,13:00,GB,MIN,,,,,,,,\n" +
		"2025_01_OLD_GAME,2025,REG,1,2025-09-07,13:00,AAA,BBB,100,-120,1.0,-110,-110,40.0,-110,-110\n" +
		"2026_99_PLAYOFF,2026,POST,19,2027-01-10,13:00,CCC,DDD,100,-120,1.0,-110,-110,40.0,-110,-110\n"

	games, err := ReadSchedule(strings.NewReader(src), 2026)
	if err != nil {
		t.Fatalf("ReadSchedule: %v", err)
	}
	// The 2025 row and the POST row must both be dropped.
	if len(games) != 3 {
		t.Fatalf("got %d games, want 3: %+v", len(games), games)
	}
	g := games[0]
	if g.ID != "2026_01_NYJ_TEN" || g.Away != "NYJ" || g.Home != "TEN" {
		t.Fatalf("first game wrong: %+v", g)
	}
	if g.Kickoff != "2026-09-13T13:00" {
		t.Fatalf("kickoff = %q", g.Kickoff)
	}
	if !g.SpreadOK || g.Spread != "2.5 -110/-110" {
		t.Fatalf("spread = %q ok=%v", g.Spread, g.SpreadOK)
	}

	// A row with no published lines is normal early in a season and must not
	// invent prices.
	if games[2].AwayML != "" || games[2].SpreadOK || games[2].TotalOK {
		t.Fatalf("unpriced game gained prices: %+v", games[2])
	}
}

func TestReadScheduleRejectsMissingColumn(t *testing.T) {
	// Columns are addressed by name because nflverse adds fields between
	// releases; a positional read would silently pull the wrong column.
	_, err := ReadSchedule(strings.NewReader("season,week\n2026,1\n"), 2026)
	if err == nil {
		t.Fatal("accepted a header with no game_id, want an error")
	}
}

func TestScaffoldNeverOverwritesAPrice(t *testing.T) {
	games, err := ReadSchedule(strings.NewReader(csvHead+
		"2026_01_NYJ_TEN,2026,REG,1,2026-09-13,13:00,NYJ,TEN,130,-155,2.5,-110,-110,44.5,-110,-110\n"), 2026)
	if err != nil {
		t.Fatalf("ReadSchedule: %v", err)
	}

	doc := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	if added := doc.Scaffold(games); added != 1 {
		t.Fatalf("first scaffold added %d, want 1", added)
	}

	// Type a price in by hand, and correct the consensus column too -- a
	// correction you made after spotting a bad line must survive as surely as
	// an original entry does.
	l := doc.Games["2026_01_NYJ_TEN"].Books["fanatics"]
	l.ML = "+125/-150"
	doc.Games["2026_01_NYJ_TEN"].Books["fanatics"] = l
	c := doc.Games["2026_01_NYJ_TEN"].Books[Consensus]
	c.ML = "+131/-156"
	doc.Games["2026_01_NYJ_TEN"].Books[Consensus] = c

	if added := doc.Scaffold(games); added != 0 {
		t.Fatalf("re-scaffold added %d games, want 0", added)
	}
	if got := doc.Games["2026_01_NYJ_TEN"].Books["fanatics"].ML; got != "+125/-150" {
		t.Fatalf("hand-entered price was clobbered: %q", got)
	}
	if got := doc.Games["2026_01_NYJ_TEN"].Books[Consensus].ML; got != "+131/-156" {
		t.Fatalf("hand-corrected consensus was clobbered: %q", got)
	}
}

func TestScaffoldRefreshesScheduleFacts(t *testing.T) {
	// A flexed game moves. Schedule facts are authoritative and refresh;
	// prices do not.
	early := csvHead + "2026_10_X_Y,2026,REG,10,2026-11-08,13:00,X,Y,,,,,,,,\n"
	late := csvHead + "2026_10_X_Y,2026,REG,10,2026-11-08,20:20,X,Y,,,,,,,,\n"

	g1, _ := ReadSchedule(strings.NewReader(early), 2026)
	g2, _ := ReadSchedule(strings.NewReader(late), 2026)

	doc := &Doc{Season: 2026, Week: 10, Games: map[string]*Game{}}
	doc.Scaffold(g1)
	l := doc.Games["2026_10_X_Y"].Books["fanatics"]
	l.ML = "+200/-240"
	doc.Games["2026_10_X_Y"].Books["fanatics"] = l

	doc.Scaffold(g2)
	if got := doc.Games["2026_10_X_Y"].Kickoff; got != "2026-11-08T20:20" {
		t.Fatalf("kickoff did not follow the flex: %q", got)
	}
	if got := doc.Games["2026_10_X_Y"].Books["fanatics"].ML; got != "+200/-240" {
		t.Fatalf("flex lost the price: %q", got)
	}
}

func TestScaffoldGivesEveryBookASlot(t *testing.T) {
	games, _ := ReadSchedule(strings.NewReader(csvHead+
		"2026_01_NYJ_TEN,2026,REG,1,2026-09-13,13:00,NYJ,TEN,130,-155,2.5,-110,-110,44.5,-110,-110\n"), 2026)
	doc := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	doc.Scaffold(games)

	books := doc.Games["2026_01_NYJ_TEN"].Books
	for _, want := range Books {
		if _, ok := books[want]; !ok {
			t.Fatalf("no slot for %q", want)
		}
	}
	if got := books[Consensus].ML; got != "+130/-155" {
		t.Fatalf("consensus ML = %q, want +130/-155", got)
	}
	if !books["fanatics"].Empty() {
		t.Fatalf("fanatics should start empty, got %+v", books["fanatics"])
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	games, _ := ReadSchedule(strings.NewReader(csvHead+
		"2026_01_NYJ_TEN,2026,REG,1,2026-09-13,13:00,NYJ,TEN,130,-155,2.5,-110,-110,44.5,-110,-110\n"), 2026)
	doc := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	doc.Scaffold(games)

	var buf bytes.Buffer
	if err := doc.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "# Week 01, 2026.") {
		t.Fatalf("header missing; yaml.v3 drops comments so Encode must re-emit it")
	}
	back, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Parse of our own output: %v", err)
	}
	if len(back.Games) != 1 || back.Week != 1 || back.Season != 2026 {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if got := back.Games["2026_01_NYJ_TEN"].Books[Consensus].Spread; got != "2.5 -110/-110" {
		t.Fatalf("round trip lost the spread: %q", got)
	}
	if probs := back.Validate(); len(probs) != 0 {
		t.Fatalf("our own scaffold output does not validate: %v", probs)
	}
}

func TestGameIDsSortByKickoff(t *testing.T) {
	// Alphabetical order would put ARI first; the board shows games in the
	// order they are played.
	games, _ := ReadSchedule(strings.NewReader(csvHead+
		"2026_01_ARI_LAC,2026,REG,1,2026-09-13,16:25,ARI,LAC,455,-625,10.5,-110,-110,46.5,-110,-110\n"+
		"2026_01_NYJ_TEN,2026,REG,1,2026-09-13,13:00,NYJ,TEN,130,-155,2.5,-110,-110,44.5,-110,-110\n"), 2026)
	doc := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	doc.Scaffold(games)

	ids := doc.GameIDs()
	if ids[0] != "2026_01_NYJ_TEN" {
		t.Fatalf("first game = %q, want the 13:00 kickoff", ids[0])
	}
}

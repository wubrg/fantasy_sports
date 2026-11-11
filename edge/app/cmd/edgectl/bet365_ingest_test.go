package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edge/internal/board"
)

// A bet365 page in its wire format (see internal/oddspull/bet365.go): the lines
// tab for two games and a receiving-yards ladder for one. SF @ LA is on the
// week's board; DAL @ NYG is a game from another week that the page also lists.
const b365Head = "MA;NA=Lines;PD=#AC#B12#C1#D48#E1441#F36#;|" +
	"MA;NA=Game Lines;PD=#AC#B12#C1#D48#E1441#F36#N0#;|" +
	"MA;NA=Receiving Yds;PD=#AC#B12#C1#D47#E121158#F47#N0#;"

const b365LinesTab = "F|CL;ID=12;|" + b365Head + "|MG;ID=1441;SY=cmx;OI=10;|MA;ID=M1441;NA= ;|" +
	"PA;ID=PC1;NA=SF 49ers;N2=LA Rams;FD=SF 49ers @ LA Rams;FI=10;|PA;PZ=0;|" +
	"PA;ID=PC2;NA=DAL Cowboys;N2=NY Giants;FD=DAL Cowboys @ NY Giants;FI=20;|PA;PZ=1;|" +
	"MA;ID=M1441;NA=Spread;FI=10;|" +
	"PA;ID=1;HD=-3.5;OD=10/11;PZ=0;|PA;ID=2;HD=+3.5;OD=10/11;PZ=0;|" +
	"PA;ID=3;HD=-1.5;OD=10/11;PZ=1;|PA;ID=4;HD=+1.5;OD=10/11;PZ=1;|" +
	"MA;ID=M1442;NA=Total;FI=10;|" +
	"PA;ID=5;HD=O 44.5;OD=10/11;PZ=0;|PA;ID=6;HD=U 44.5;OD=10/11;PZ=0;|" +
	"PA;ID=7;HD=O 40.5;OD=10/11;PZ=1;|PA;ID=8;HD=U 40.5;OD=10/11;PZ=1;|" +
	"MA;ID=M936;NA=Money;FI=10;|" +
	"PA;ID=9;OD=5/6;PZ=0;|PA;ID=10;OD=11/10;PZ=0;|" +
	"PA;ID=11;OD=5/6;PZ=1;|PA;ID=12;OD=11/10;PZ=1;|"

const b365RecTab = "F|CL;ID=12;|" + b365Head + "|MG;ID=121158;SY=cmx;OI=10;|" +
	"MG;ID=121158;FI=11;SY=sgh;NA=SF 49ers @ LA Rams;|" +
	"PA;ID=PC1;NA=Puka Nacua;N2=LA Rams;|" +
	"CO;ID=C1;NA=60;|PA;ID=1;OD=1/2;|" +
	"CO;ID=C1;NA=80;|PA;ID=2;OD=11/10;|" +
	"MG;ID=121158;FI=21;SY=sgh;NA=DAL Cowboys @ NY Giants;|" +
	"PA;ID=PC3;NA=CeeDee Lamb;N2=DAL Cowboys;|" +
	"CO;ID=C1;NA=60;|PA;ID=3;OD=1/2;|"

func writeBet365Capture(t *testing.T, dir, name string) {
	t.Helper()
	var entries []map[string]any
	for _, b := range []string{b365LinesTab, b365RecTab} {
		entries = append(entries, map[string]any{"response": map[string]any{"content": map[string]any{"text": b}}})
	}
	data, _ := json.Marshal(map[string]any{"log": map[string]any{"entries": entries}})
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeWeekBoard writes a week-1 board holding only SF @ LA, with both books'
// columns present and empty.
func writeWeekBoard(t *testing.T, dir string) string {
	t.Helper()
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{"draftkings": {}, "bet365": {}}},
	}}
	path := filepath.Join(dir, "week01.yaml")
	if err := writeDoc(path, doc); err != nil {
		t.Fatal(err)
	}
	return path
}

// serveFor starts the board server over an existing board dir and ingest folder.
func serveFor(t *testing.T, dir, ingest string) *httptest.Server {
	t.Helper()
	srv, err := newBoardServer(dir, ingest)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestPropsRowsReadsBet365AndScopesToTheWeek(t *testing.T) {
	ingest, boards := t.TempDir(), t.TempDir()
	writeBet365Capture(t, ingest, "bet365_week1.har")
	writeWeekBoard(t, boards)

	rows, _, err := propsRows(ingest, 1, "", "bet365", boards)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no bet365 rows")
	}
	var ladder, ml int
	for _, r := range rows {
		if r.Book != "bet365" {
			t.Errorf("row attributed to %q, want bet365: %+v", r.Book, r)
		}
		if strings.Contains(r.Game, "DAL") {
			t.Errorf("DAL @ NYG is not on the week's board and must be scoped out: %+v", r)
		}
		if r.category == "Receiving" {
			ladder++
		}
		if r.Market == "Moneyline" {
			ml++
			if r.FairDevig == nil {
				t.Errorf("a two-sided moneyline should be de-vigged: %+v", r)
			}
		}
	}
	if ladder != 2 || ml != 2 {
		t.Errorf("want Nacua's 2 rungs and SF @ LA's 2 moneyline sides, got ladder=%d ml=%d", ladder, ml)
	}

	// With no board there is nothing to scope by, so every game the page lists shows.
	all, _, _ := propsRows(ingest, 1, "", "bet365", "")
	var sawDAL bool
	for _, r := range all {
		if strings.Contains(r.Game, "DAL") {
			sawDAL = true
		}
	}
	if !sawDAL {
		t.Error("with no board directory the page's other games should not be dropped")
	}
}

func TestPropsRowsScopesDraftKingsToTheWeekToo(t *testing.T) {
	// A DraftKings capture can hold games from other weeks (a week-10 TNF capture
	// held 27 events, 12 of them not week 10). Only the week's board games stay.
	ingest, boards := t.TempDir(), t.TempDir()
	writeWeekBoard(t, boards) // SF @ LA only
	body := `{"events":[{"id":"E1","name":"SF @ LA"},{"id":"E2","name":"DAL @ NYG"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Moneyline"},{"id":"M2","eventId":"E2","name":"Moneyline"}],` +
		`"selections":[` +
		`{"marketId":"M1","label":"SF 49ers","displayOdds":{"american":"-150"}},` +
		`{"marketId":"M1","label":"LA Rams","displayOdds":{"american":"+130"}},` +
		`{"marketId":"M2","label":"DAL Cowboys","displayOdds":{"american":"-120"}},` +
		`{"marketId":"M2","label":"NYG Giants","displayOdds":{"american":"+100"}}]}`
	if err := os.WriteFile(filepath.Join(ingest, "dk_week1.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, _, _ := propsRows(ingest, 1, "", "draftkings", boards)
	if len(rows) != 2 {
		t.Fatalf("want only SF @ LA's two moneyline rows, got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if strings.Contains(r.Game, "DAL") {
			t.Errorf("DAL @ NYG is not on the week's board: %+v", r)
		}
	}
	// With no board there is nothing to scope by.
	if all, _, _ := propsRows(ingest, 1, "", "draftkings", ""); len(all) != 4 {
		t.Errorf("with no board directory all 4 rows should show, got %d", len(all))
	}
}

func TestPropsRowsBookFilterKeepsDraftKingsDefaultSafe(t *testing.T) {
	ingest, boards := t.TempDir(), t.TempDir()
	writeBet365Capture(t, ingest, "bet365_week1.har")
	writeWeekBoard(t, boards)

	// A folder holding only a bet365 capture has no DraftKings prices: the default
	// report must be empty, not quietly full of another book's numbers.
	dk, _, _ := propsRows(ingest, 1, "", "draftkings", boards)
	if len(dk) != 0 {
		t.Errorf("draftkings rows from a bet365-only folder: %d", len(dk))
	}
	if got := booksInFolder(ingest, 1, boards); len(got) != 1 || got[0] != "bet365" {
		t.Errorf("booksInFolder = %v, want [bet365]", got)
	}
	// "" keeps every book.
	if all, _, _ := propsRows(ingest, 1, "", "", boards); len(all) == 0 {
		t.Error("an empty book filter should keep every book")
	}
}

func TestTwoBooksAtTheSamePriceStayTwoRows(t *testing.T) {
	ingest := t.TempDir()
	writeBet365Capture(t, ingest, "bet365_week1.har")
	writeDKCapture(t, ingest, "dk_week1.json")

	rows, _, _ := propsRows(ingest, 1, "SF LA", "", "")
	books := map[string]int{}
	for _, r := range rows {
		if r.Market == "Moneyline" && r.Selection == "SF 49ers" {
			books[r.Book]++
		}
	}
	if books["draftkings"] != 1 || books["bet365"] != 1 {
		t.Errorf("want one SF moneyline row per book, got %v (a newer capture of one book must not overwrite the other)", books)
	}
}

func TestServePropsSyncKeepsEachBooksPricesInItsOwnColumn(t *testing.T) {
	dir, ingest := t.TempDir(), t.TempDir()
	path := writeWeekBoard(t, dir)
	writeDKCapture(t, ingest, "dk_week1.json") // SF -150 / LA +130
	writeBet365Capture(t, ingest, "bet365_week1.har")
	ts := serveFor(t, dir, ingest)

	// Syncing the DraftKings column must not pick up bet365's -120/+110.
	if code, body := post(t, ts, "/api/props/sync/apply", propsSyncRequest{Week: 1, Book: "draftkings"}); code != 200 {
		t.Fatalf("apply dk: %d %v", code, body)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "-150/+130") {
		t.Errorf("draftkings column should hold the DK price:\n%s", raw)
	}
	if strings.Contains(string(raw), "-120/+110") {
		t.Errorf("a bet365 price leaked into a sync of the draftkings column:\n%s", raw)
	}

	// Syncing bet365 fills its own column from the bet365 page, scoped to this
	// week (DAL @ NYG is not on the board).
	if code, body := post(t, ts, "/api/props/sync/apply", propsSyncRequest{Week: 1, Book: "bet365"}); code != 200 {
		t.Fatalf("apply bet365: %d %v", code, body)
	}
	doc, err := loadWeekDoc(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	g := doc.Games["2026_01_SF_LA"]
	if got := g.Books["bet365"].ML; got != "-120/+110" {
		t.Errorf("bet365 ml = %q, want -120/+110", got)
	}
	if got := g.Books["bet365"].Spread; got != "-3.5 -110/-110" {
		t.Errorf("bet365 spread = %q", got)
	}
	if got := g.Books["bet365"].Total; got != "44.5 -110/-110" {
		t.Errorf("bet365 total = %q", got)
	}
	if got := g.Books["draftkings"].ML; got != "-150/+130" {
		t.Errorf("draftkings ml changed by the bet365 sync: %q", got)
	}
}

func TestSnapshotNameSaysWhichBook(t *testing.T) {
	root := t.TempDir()
	bet := []marketRow{{Game: "SF 49ers @ LA Rams", Book: "bet365"}}
	dk := []marketRow{{Game: "SF 49ers @ LA Rams", Book: "draftkings"}}
	mixed := []marketRow{{Game: "SF 49ers @ LA Rams", Book: "bet365"}, {Game: "SF 49ers @ LA Rams", Book: "draftkings"}}

	pBet, err := saveSnapshot(root, 2026, 1, "", bet, ingestResult{})
	if err != nil {
		t.Fatal(err)
	}
	pDK, _ := saveSnapshot(root, 2026, 1, "", dk, ingestResult{})
	pMix, _ := saveSnapshot(root, 2026, 1, "", mixed, ingestResult{})
	if !strings.Contains(filepath.Base(pBet), "-bet365-") {
		t.Errorf("a bet365 snapshot should say so in its name: %s", pBet)
	}
	if strings.Contains(filepath.Base(pDK), "bet365") || strings.Contains(filepath.Base(pMix), "bet365") {
		t.Errorf("DraftKings and mixed snapshots keep their usual name: %s, %s", pDK, pMix)
	}
}

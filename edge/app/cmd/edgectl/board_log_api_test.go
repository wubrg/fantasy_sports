package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"edge/internal/betlog"
	"edge/internal/wager"
)

// TestHandleLogSplitsAtRiskByBankroll pins that the log reports real-money and
// bonus stakes as separate at-risk figures — a lost bonus bet costs no cash, so
// $50 of bonus at risk is not $50 of real money at risk.
func TestHandleLogSplitsAtRiskByBankroll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if _, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "cash bet", Price: -110, Bankroll: "real money", Stake: 30, Predicted: 0.55}); err != nil {
		t.Fatal(err)
	}
	if _, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "bonus bet", Price: 300, Bankroll: "bonus bet", Stake: 50, Predicted: 0.25}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	(&boardServer{betlogPath: path}).handleLog(rr, httptest.NewRequest("GET", "/api/log", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r struct {
		OpenStaked      float64 `json:"open_staked"`
		OpenStakedCash  float64 `json:"open_staked_cash"`
		OpenStakedBonus float64 `json:"open_staked_bonus"`
		OpenPayout      float64 `json:"open_payout"`
		Entries         []struct {
			Selection string  `json:"selection"`
			Payout    float64 `json:"payout"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.OpenStakedCash != 30 || r.OpenStakedBonus != 50 || r.OpenStaked != 80 {
		t.Errorf("cash=%v bonus=%v total=%v, want 30/50/80", r.OpenStakedCash, r.OpenStakedBonus, r.OpenStaked)
	}
	// To-win: -110 on 30 pays 30*(100/110)=27.27; +300 on 50 pays 150. The
	// aggregate is the sum of the two, independent of any belief.
	if math.Abs(r.OpenPayout-177.2727) > 1e-3 {
		t.Errorf("open_payout=%.4f, want ~177.27", r.OpenPayout)
	}
	want := map[string]float64{"cash bet": 27.2727, "bonus bet": 150}
	for _, e := range r.Entries {
		if math.Abs(e.Payout-want[e.Selection]) > 1e-3 {
			t.Errorf("%q payout=%.4f, want %.4f", e.Selection, e.Payout, want[e.Selection])
		}
	}
}

// TestHandleLogFiltersByBookAndQuery pins the log tab's two filters: a
// multi-select of books, and a free-text needle matched against the selection
// and the narrative (the only place a team or a player name is ever written).
//
// It also pins the trap in available_books: derived from the FILTERED entries,
// picking one book would erase every other book from the chip row, leaving the
// filter unclearable from the UI that set it. The option set is week-scoped and
// filter-independent by construction.
func TestHandleLogFiltersByBookAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	for _, b := range []betlog.Bet{
		{Selection: "Bijan Robinson 70+ rush yards", Price: -110, Bankroll: "real money", Stake: 20, Predicted: 0.55, Book: "fanduel", Week: 4},
		{Selection: "Falcons team total over", Price: 100, Bankroll: "real money", Stake: 10, Predicted: 0.52, Book: "fanduel", Week: 4,
			Narrative: "the same Bijan volume, priced off the team side"},
		{Selection: "Drake London 67+ rec yards", Price: -120, Bankroll: "real money", Stake: 15, Predicted: 0.56, Book: "fanduel", Week: 4},
		{Selection: "Bijan Robinson anytime TD", Price: 130, Bankroll: "real money", Stake: 12, Predicted: 0.45, Book: "draftkings", Week: 4},
		{Selection: "Bijan Robinson anytime TD", Price: 145, Bankroll: "real money", Stake: 12, Predicted: 0.45, Book: "fanduel", Week: 5},
	} {
		if _, err := betlog.PlaceBet(path, b); err != nil {
			t.Fatal(err)
		}
	}

	type logResp struct {
		Count     int      `json:"count"`
		Books     []string `json:"books"`
		Avail     []string `json:"available_books"`
		Q         string   `json:"q"`
		OpenStake float64  `json:"open_staked"`
		Entries   []struct {
			Selection string `json:"selection"`
			Book      string `json:"book"`
		} `json:"entries"`
	}
	get := func(query string) logResp {
		t.Helper()
		rr := httptest.NewRecorder()
		(&boardServer{betlogPath: path}).handleLog(rr, httptest.NewRequest("GET", "/api/log?"+query, nil))
		if rr.Code != 200 {
			t.Fatalf("%s: status %d: %s", query, rr.Code, rr.Body.String())
		}
		var r logResp
		if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return r
	}

	// No filter: the whole week, and both filter fields echo back empty.
	if r := get("week=4"); r.Count != 4 || len(r.Books) != 0 || r.Q != "" {
		t.Errorf("unfiltered week 4: count=%d books=%v q=%q, want 4//\"\"", r.Count, r.Books, r.Q)
	}

	// Book + search together AND: only the FanDuel week-4 bets naming Bijan --
	// one by selection, one only in its narrative.
	r := get("week=4&books=fanduel&q=bijan")
	if r.Count != 2 {
		t.Fatalf("books=fanduel&q=bijan: count=%d, want 2 (%+v)", r.Count, r.Entries)
	}
	for _, e := range r.Entries {
		if e.Book != "fanduel" {
			t.Errorf("entry %q came from %q, want fanduel only", e.Selection, e.Book)
		}
	}
	// The stats accumulate over the FILTERED set, not the week: 20 + 10.
	if math.Abs(r.OpenStake-30) > 1e-9 {
		t.Errorf("open_staked=%v, want 30 (the filtered stakes only)", r.OpenStake)
	}
	if r.Q != "bijan" || len(r.Books) != 1 || r.Books[0] != "fanduel" {
		t.Errorf("filter not echoed: books=%v q=%q", r.Books, r.Q)
	}
	// The bug this pins: draftkings is filtered OUT of the entries and must still
	// be offered as a chip, or the filter cannot be widened again.
	if len(r.Avail) != 2 || r.Avail[0] != "draftkings" || r.Avail[1] != "fanduel" {
		t.Errorf("available_books=%v, want the week's full sorted set [draftkings fanduel]", r.Avail)
	}

	// Several books OR together.
	if r := get("week=4&books=fanduel,draftkings&q=bijan"); r.Count != 3 {
		t.Errorf("two books: count=%d, want 3", r.Count)
	}
	// Search alone, case-insensitively, across every book in the week.
	if r := get("week=4&q=BIJAN"); r.Count != 3 {
		t.Errorf("q=BIJAN: count=%d, want 3", r.Count)
	}
	// available_books re-scopes with the week: week 5 has only the one bet.
	if r := get("week=5&books=fanduel"); r.Count != 1 || len(r.Avail) != 1 || r.Avail[0] != "fanduel" {
		t.Errorf("week 5: count=%d avail=%v, want 1/[fanduel]", r.Count, r.Avail)
	}
	// Over-narrow: zero entries, but the chip row still offers the whole week.
	if r := get("week=4&books=fanduel&q=mahomes"); r.Count != 0 || len(r.Avail) != 2 {
		t.Errorf("no matches: count=%d avail=%v, want 0 and the full option set", r.Count, r.Avail)
	}
}

// TestHandleLogFiltersByStatus pins the open/settled toggle: exclusive (a bet
// is one or the other, never both, unlike the multi-select book chips), and
// it gates the accumulated stats the same way book/q already do -- an "open"
// view has zero realized, a "settled" view has zero open exposure.
func TestHandleLogFiltersByStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	openID, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "still live", Price: -110, Bankroll: "real money", Stake: 20, Week: 6})
	if err != nil {
		t.Fatal(err)
	}
	wonID, err := betlog.PlaceBet(path, betlog.Bet{
		Selection: "already graded", Price: 150, Bankroll: "real money", Stake: 10, Week: 6})
	if err != nil {
		t.Fatal(err)
	}
	if err := betlog.Settle(path, wonID, betlog.Won, nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	type logResp struct {
		Count      int     `json:"count"`
		Status     string  `json:"status"`
		OpenStaked float64 `json:"open_staked"`
		Realized   float64 `json:"realized"`
		Entries    []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	get := func(query string) logResp {
		t.Helper()
		rr := httptest.NewRecorder()
		(&boardServer{betlogPath: path}).handleLog(rr, httptest.NewRequest("GET", "/api/log?"+query, nil))
		if rr.Code != 200 {
			t.Fatalf("%s: status %d: %s", query, rr.Code, rr.Body.String())
		}
		var r logResp
		if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return r
	}

	if r := get("week=6"); r.Count != 2 || r.Status != "" {
		t.Errorf("unfiltered: count=%d status=%q, want 2/\"\"", r.Count, r.Status)
	}
	if r := get("week=6&status=open"); r.Count != 1 || r.Entries[0].ID != openID || r.Realized != 0 {
		t.Errorf("status=open: count=%d id=%q realized=%v, want 1/%q/0", r.Count, r.Entries[0].ID, r.Realized, openID)
	}
	if r := get("week=6&status=settled"); r.Count != 1 || r.Entries[0].ID != wonID || r.OpenStaked != 0 {
		t.Errorf("status=settled: count=%d id=%q open_staked=%v, want 1/%q/0", r.Count, r.Entries[0].ID, r.OpenStaked, wonID)
	}
}

// TestRealizedPnL pins the settled-P&L rules the log's "realized" figure sums:
// a win books its profit regardless of bankroll; only a real-money loss costs
// cash; a bonus loss, a push and a void are all zero.
func TestRealizedPnL(t *testing.T) {
	cases := []struct {
		name     string
		bankroll string
		result   betlog.Result
		price    wager.American
		stake    float64
		want     float64
	}{
		{"won real money", "real money", betlog.Won, 150, 50, 75},   // 50 * 1.5
		{"won bonus", "bonus bet", betlog.Won, 150, 50, 75},         // profit is cash either way
		{"won favorite", "real money", betlog.Won, -200, 50, 25},    // 50 * 0.5
		{"lost real money", "real money", betlog.Lost, 150, 50, -50}, // stake gone
		{"lost bonus", "bonus bet", betlog.Lost, 150, 50, 0},        // no cash was risked
		{"push", "real money", betlog.Pushed, 150, 50, 0},
		{"void", "real money", betlog.Void, 150, 50, 0},
		{"open counts as nothing", "real money", betlog.Open, 150, 50, 0},
	}
	for _, c := range cases {
		got := realizedPnL(c.bankroll, c.result, c.price, c.stake)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: realizedPnL = %+.2f, want %+.2f", c.name, got, c.want)
		}
	}
}

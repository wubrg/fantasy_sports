package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type propsResp struct {
	Groups []struct {
		Category string
		Rows     []struct {
			Selection string
			Implied   float64
			BoostBE   *float64 `json:"boost_be"`
			Fair      *float64
			Hold      *float64
		}
	}
	Sources []map[string]string
	Note    string
}

func getProps(t *testing.T, srv *boardServer) propsResp {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.handleProps(rr, httptest.NewRequest("GET", "/api/props", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r propsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rr.Body.String())
	}
	return r
}

func TestHandlePropsPricesCapture(t *testing.T) {
	dir := t.TempDir()
	// A DK sportscontent body: a two-sided moneyline (de-vig) + a one-sided prop
	// ladder rung (boost breakeven).
	body := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Moneyline"},` +
		`{"id":"M2","eventId":"E1","name":"JSN Receiving Yards"}],` +
		`"selections":[{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"+140"}},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-166"}},` +
		`{"marketId":"M2","label":"JSN 100+","displayOdds":{"american":"+135"},"points":100}]}`
	esc, _ := json.Marshal(body) // embed as a HAR response text string
	har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "dk.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}

	r := getProps(t, &boardServer{ingestDir: dir})
	if len(r.Sources) != 1 {
		t.Fatalf("want 1 source, got %d", len(r.Sources))
	}

	var game, recv *struct {
		Selection string
		Implied   float64
		BoostBE   *float64 `json:"boost_be"`
		Fair      *float64
		Hold      *float64
	}
	for gi := range r.Groups {
		for ri := range r.Groups[gi].Rows {
			row := &r.Groups[gi].Rows[ri]
			if r.Groups[gi].Category == "Game" && row.Selection == "NE Patriots" {
				game = row
			}
			if r.Groups[gi].Category == "Receiving" {
				recv = row
			}
		}
	}
	if game == nil || game.Fair == nil {
		t.Fatalf("moneyline should be de-vigged with a fair prob: %+v", game)
	}
	if math.Abs(*game.Fair-0.40) > 0.02 { // NE +140 vs SEA -166 de-vig ~40%
		t.Errorf("NE fair de-vig = %.3f, want ~0.40", *game.Fair)
	}
	// A Game-category row keeps its Fair-only shape: the tab never carried a
	// hold column for game lines and must not start now.
	if game.Hold != nil {
		t.Errorf("Game line should not carry a hold, got %.4f", *game.Hold)
	}
	if recv == nil || recv.BoostBE == nil || recv.Fair != nil {
		t.Fatalf("one-sided prop should have a boost breakeven and no fair: %+v", recv)
	}
}

// TestHandlePropsTwoSidedPropDevigged pins the HTTP endpoint's behaviour: a
// two-sided PROP market (an Over/Under yardage line) is now de-vigged the same
// way a two-sided game line is -- both sides pair by marketId and each gets a
// fair prob plus the market's hold. (Before this it was left on the
// boosted-breakeven path; the props tab now matches `props-report`.) A
// de-vigged prop carries Fair and Hold and no boost breakeven.
func TestHandlePropsTwoSidedPropDevigged(t *testing.T) {
	dir := t.TempDir()
	body := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"JSN Receiving Yards O/U"}],` +
		`"selections":[{"marketId":"M1","label":"JSN Over","displayOdds":{"american":"-115"},"points":49.5},` +
		`{"marketId":"M1","label":"JSN Under","displayOdds":{"american":"-105"},"points":49.5}]}`
	esc, _ := json.Marshal(body)
	har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "dk.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}
	r := getProps(t, &boardServer{ingestDir: dir})
	seen := 0
	for _, g := range r.Groups {
		if g.Category != "Receiving" {
			continue
		}
		for _, row := range g.Rows {
			seen++
			if row.Fair == nil {
				t.Errorf("%q: a two-sided prop should now be de-vigged with a fair prob", row.Selection)
				continue
			}
			if row.Hold == nil {
				t.Errorf("%q: a de-vigged prop should carry the market's hold", row.Selection)
			}
			if row.BoostBE != nil {
				t.Errorf("%q: a de-vigged prop should not also carry a boost breakeven; got %.3f", row.Selection, *row.BoostBE)
			}
			// -115/-105 is a modest two-way hold, a few percent.
			if row.Hold != nil && (*row.Hold < 0 || *row.Hold > 0.10) {
				t.Errorf("%q: hold %.4f out of the plausible band for -115/-105", row.Selection, *row.Hold)
			}
			// Both sides share the same market hold.
			if row.Fair != nil && (*row.Fair <= 0 || *row.Fair >= 1) {
				t.Errorf("%q: fair prob %.4f not in (0,1)", row.Selection, *row.Fair)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("want 2 receiving prop rows (Over/Under), saw %d", seen)
	}
}

// TestHandlePropsOneSidedPropStaysBoostOnly pins that a one-sided prop (a lone
// alt-line rung with no opposing price) is still priced on the boosted-
// breakeven path and never gets a fabricated fair/hold -- the refuse-rather-
// than-guess rule the whole tool runs on.
func TestHandlePropsOneSidedPropStaysBoostOnly(t *testing.T) {
	dir := t.TempDir()
	body := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"JSN Receiving Yards"}],` +
		`"selections":[{"marketId":"M1","label":"JSN 100+","displayOdds":{"american":"+135"},"points":100}]}`
	esc, _ := json.Marshal(body)
	har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "dk.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}
	r := getProps(t, &boardServer{ingestDir: dir})
	seen := 0
	for _, g := range r.Groups {
		if g.Category != "Receiving" {
			continue
		}
		for _, row := range g.Rows {
			seen++
			if row.Fair != nil {
				t.Errorf("%q: a one-sided prop must not get a fabricated fair; got %.3f", row.Selection, *row.Fair)
			}
			if row.Hold != nil {
				t.Errorf("%q: a one-sided prop must not get a fabricated hold; got %.3f", row.Selection, *row.Hold)
			}
			if row.BoostBE == nil {
				t.Errorf("%q: a one-sided prop should carry a boosted breakeven", row.Selection)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("want 1 receiving prop row, saw %d", seen)
	}
}

func TestHandlePropsEmptyAndUnconfigured(t *testing.T) {
	// Unconfigured ingest dir: a note, no crash.
	if r := getProps(t, &boardServer{ingestDir: ""}); r.Note == "" {
		t.Error("an unconfigured ingest dir should return a note")
	}
	// Empty folder: no groups, no note (nothing wrong, just nothing there).
	if r := getProps(t, &boardServer{ingestDir: t.TempDir()}); len(r.Groups) != 0 {
		t.Errorf("empty folder should yield no groups, got %d", len(r.Groups))
	}
}

func TestCaptureWeek(t *testing.T) {
	cases := map[string]int{
		"dk_week2_tnf.har":        2,
		"dk_wk3_sunday.har":       3,
		"dk_w1_early.json":        1,
		"dk_week10_mnf.har":       10, // full number, not week-1
		"showdown_week2.har":      2,  // the w's in "showdown" have no trailing digit
		"draftkings_2026_det.har": 0,  // no week/wk/w token before the digits
		"props.har":               0,
	}
	for name, want := range cases {
		if got := captureWeek(name); got != want {
			t.Errorf("captureWeek(%q) = %d, want %d", name, got, want)
		}
	}
}

// TestReadIngestMergesMovedGameLineAcrossCaptures guards the bug this file's
// mergeKey fixes: two captures of the same week, taken hours apart, with a
// spread that moved in between. Before the fix, outcomeKey's Line component
// meant the old and new entries never collided -- both survived the merge,
// so a market that should have had two sides had four, and
// OddsPairsFromOutcomes's two-sided check silently dropped it instead of
// syncing the newer line.
func TestReadIngestMergesMovedGameLineAcrossCaptures(t *testing.T) {
	dir := t.TempDir()

	older := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Spread"}],` +
		`"selections":[{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"-110"},"points":3},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-110"},"points":-3}]}`
	newer := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Spread"}],` +
		`"selections":[{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"-120"},"points":2.5},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"+100"},"points":-2.5}]}`

	writeHAR := func(name, body string) {
		esc, _ := json.Marshal(body)
		har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(har), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeHAR("dk_week4_old.har", older)
	writeHAR("dk_week4_new.har", newer)
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "dk_week4_old.har"), now, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "dk_week4_new.har"), now, now); err != nil {
		t.Fatal(err)
	}

	res := readIngest(dir, 4)
	var spreads []string
	for _, o := range res.outcomes {
		if o.Market == "Spread" {
			spreads = append(spreads, fmt.Sprintf("%s@%v=%v", o.Selection, *o.Line, o.Price))
		}
	}
	if len(spreads) != 2 {
		t.Fatalf("want exactly 2 merged Spread outcomes (the newer capture's), got %d: %v", len(spreads), spreads)
	}
	for _, s := range spreads {
		if !strings.Contains(s, "2.5=") {
			t.Errorf("spread outcome %q still reflects the stale 3-point line, not the moved 2.5", s)
		}
	}
}

// TestReadIngestDoesNotCollapseAlternateLines is the regression guard for the
// fix's first (too-broad) attempt: dropping Line from every merge key, not
// just the three literal game-line markets, collapsed a whole "Spread
// Alternate" ladder (every rung selecting the same two team names) down to
// one arbitrary rung, because marketKind's board-sync matcher reads market
// names by substring and would have treated it as a real "Spread" market.
// mergeKey must leave alternates on the original Line-inclusive key.
func TestReadIngestDoesNotCollapseAlternateLines(t *testing.T) {
	dir := t.TempDir()
	body := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Spread Alternate"}],` +
		`"selections":[` +
		`{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"-110"},"points":1.5},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-110"},"points":-1.5},` +
		`{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"+150"},"points":6.5},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-200"},"points":-6.5}]}`
	esc, _ := json.Marshal(body)
	har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "dk_week4.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}

	res := readIngest(dir, 4)
	var alts int
	for _, o := range res.outcomes {
		if o.Market == "Spread Alternate" {
			alts++
		}
	}
	if alts != 4 {
		t.Errorf("want all 4 alternate-line rungs to survive the merge distinctly, got %d", alts)
	}
}

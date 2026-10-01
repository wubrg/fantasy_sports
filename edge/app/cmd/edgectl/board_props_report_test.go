package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"edge/internal/oddspull"
	"edge/internal/wager"
)

// TestDevigPairsTwoSidedAndLadder is the shared helper's unit test: a market
// with exactly two sides de-vigs, and anything else (a one-sided price, a
// many-rung ladder sharing one marketId) is left for the caller to price
// one-sided.
func TestDevigPairsTwoSidedAndLadder(t *testing.T) {
	over := oddspull.Outcome{Event: "A @ B", Market: "Rush Yds O/U", Selection: "Over", Line: f64(49.5), Price: -110, MarketID: "M1"}
	under := oddspull.Outcome{Event: "A @ B", Market: "Rush Yds O/U", Selection: "Under", Line: f64(49.5), Price: -110, MarketID: "M1"}
	rung1 := oddspull.Outcome{Event: "A @ B", Market: "Rush Yds", Selection: "25+", Price: -200, MarketID: "M2"}
	rung2 := oddspull.Outcome{Event: "A @ B", Market: "Rush Yds", Selection: "50+", Price: +150, MarketID: "M2"}
	rung3 := oddspull.Outcome{Event: "A @ B", Market: "Rush Yds", Selection: "75+", Price: +400, MarketID: "M2"}
	lone := oddspull.Outcome{Event: "A @ B", Market: "Anytime TD", Selection: "Player X", Price: +120, MarketID: "M3"}

	fair := devigPairs([]oddspull.Outcome{over, under, rung1, rung2, rung3, lone})

	// The two-sided O/U pairs and de-vigs to 50/50 at -110/-110.
	do, ok := fair[outcomeKey(over)]
	if !ok {
		t.Fatalf("the two-sided O/U should be paired")
	}
	if math.Abs(do.Fair-0.5) > 1e-9 {
		t.Errorf("Over fair = %.4f, want 0.5", do.Fair)
	}
	if math.Abs(do.Hold-1.0/22.0) > 1e-9 {
		t.Errorf("hold = %.6f, want 1/22 at -110/-110", do.Hold)
	}
	// The three-rung ladder has three sides, not two, so no rung is paired.
	for _, r := range []oddspull.Outcome{rung1, rung2, rung3} {
		if _, ok := fair[outcomeKey(r)]; ok {
			t.Errorf("ladder rung %q must not be de-vigged (not a two-sided market)", r.Selection)
		}
	}
	// The lone outcome has no partner.
	if _, ok := fair[outcomeKey(lone)]; ok {
		t.Errorf("a one-sided outcome must not be de-vigged")
	}
}

// writeIngestFixture drops a DraftKings sportscontent body into dir as a .json
// capture Parse accepts. One event carrying: a two-sided receiving O/U (de-vig),
// a one-sided alt-line rung (boost breakeven), and a two-sided moneyline.
func writeIngestFixture(t *testing.T, dir string) {
	t.Helper()
	body := map[string]any{
		"events": []any{map[string]any{"id": "E1", "name": "PIT @ CLE"}},
		"markets": []any{
			map[string]any{"id": "M1", "eventId": "E1", "name": "Moneyline"},
			map[string]any{"id": "M2", "eventId": "E1", "name": "George Pickens Receiving Yards O/U"},
			map[string]any{"id": "M3", "eventId": "E1", "name": "George Pickens Receiving Yards"},
		},
		"selections": []any{
			map[string]any{"marketId": "M1", "label": "PIT Steelers", "displayOdds": map[string]any{"american": "+140"}},
			map[string]any{"marketId": "M1", "label": "CLE Browns", "displayOdds": map[string]any{"american": "-166"}},
			map[string]any{"marketId": "M2", "label": "George Pickens Over", "points": 54.5, "displayOdds": map[string]any{"american": "-110"}},
			map[string]any{"marketId": "M2", "label": "George Pickens Under", "points": 54.5, "displayOdds": map[string]any{"american": "-110"}},
			map[string]any{"marketId": "M3", "label": "George Pickens 100+", "points": 100, "displayOdds": map[string]any{"american": "+450"}},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dk_week4_test.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPropsRowsJSONShape(t *testing.T) {
	dir := t.TempDir()
	writeIngestFixture(t, dir)

	rows, ing, err := propsRows(dir, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.sources) != 1 {
		t.Fatalf("want 1 capture source, got %d", len(ing.sources))
	}
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(rows))
	}

	byID := map[string]marketRow{}
	for _, r := range rows {
		byID[r.Selection] = r
	}

	// Two-sided receiving O/U: fair_devig and hold present, fair ~0.5.
	over := byID["George Pickens Over"]
	if over.FairDevig == nil || over.Hold == nil {
		t.Fatalf("a two-sided prop must carry fair_devig and hold: %+v", over)
	}
	if math.Abs(*over.FairDevig-0.5) > 1e-9 {
		t.Errorf("Over fair = %.4f, want 0.5", *over.FairDevig)
	}
	if over.Breakeven != over.ImpliedRaw {
		t.Errorf("breakeven (%.4f) must equal implied_raw (%.4f)", over.Breakeven, over.ImpliedRaw)
	}

	// One-sided alt rung: fair_devig and hold null; boosted breakeven is set
	// internally for the text report.
	rung := byID["George Pickens 100+"]
	if rung.FairDevig != nil || rung.Hold != nil {
		t.Errorf("a one-sided prop must leave fair_devig and hold null: %+v", rung)
	}
	if rung.boostBE == nil {
		t.Errorf("a one-sided prop should carry a boosted breakeven for the report")
	}
	wantBE, _ := wager.BoostedBreakeven(450, defaultBoostPct)
	if math.Abs(*rung.boostBE-wantBE) > 1e-9 {
		t.Errorf("boost breakeven = %.4f, want %.4f", *rung.boostBE, wantBE)
	}

	// The serialised shape must be exactly the ten MARKET-block fields, with
	// null (not zero) for a one-sided prop's fair_devig and hold.
	blob, err := json.Marshal(rung)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(blob, &generic); err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "game", "market", "selection", "book", "american", "implied_raw", "fair_devig", "breakeven", "hold"}
	if len(generic) != len(want) {
		t.Errorf("row JSON has %d fields, want %d: %s", len(generic), len(want), blob)
	}
	for _, k := range want {
		if _, ok := generic[k]; !ok {
			t.Errorf("row JSON missing field %q: %s", k, blob)
		}
	}
	if string(generic["fair_devig"]) != "null" {
		t.Errorf("one-sided fair_devig should serialise as null, got %s", generic["fair_devig"])
	}
	if string(generic["hold"]) != "null" {
		t.Errorf("one-sided hold should serialise as null, got %s", generic["hold"])
	}

	// The id is stable across re-pricing the same capture.
	rows2, _, _ := propsRows(dir, 4, "")
	if rows2[0].ID != rows[0].ID {
		t.Errorf("row id is not stable: %q vs %q", rows2[0].ID, rows[0].ID)
	}
}

func TestPropsRowsGameFilter(t *testing.T) {
	dir := t.TempDir()
	writeIngestFixture(t, dir)
	// Tokens are matched case-insensitively against the event; a non-match
	// yields no rows.
	if rows, _, _ := propsRows(dir, 4, "PIT CLE"); len(rows) != 5 {
		t.Errorf("'PIT CLE' should match the only event, got %d rows", len(rows))
	}
	if rows, _, _ := propsRows(dir, 4, "DAL HOU"); len(rows) != 0 {
		t.Errorf("'DAL HOU' should match nothing, got %d rows", len(rows))
	}
}

func TestSaveSnapshotPathAndContent(t *testing.T) {
	dir := t.TempDir()
	writeIngestFixture(t, dir)
	rows, ing, err := propsRows(dir, 4, "PIT CLE")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path, err := saveSnapshot(root, 2026, 4, "PIT CLE", rows, ing)
	if err != nil {
		t.Fatal(err)
	}
	// The season nests the file, the week and game slug name it.
	if got := filepath.Dir(path); got != filepath.Join(root, "2026") {
		t.Errorf("snapshot dir = %q, want .../2026", got)
	}
	base := filepath.Base(path)
	if want := "week04-pit-cle-"; base[:len(want)] != want {
		t.Errorf("snapshot name = %q, want prefix %q", base, want)
	}
	// The content round-trips to the same rows.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back []marketRow
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("snapshot is not valid JSON rows: %v", err)
	}
	if len(back) != len(rows) {
		t.Errorf("snapshot has %d rows, priced %d", len(back), len(rows))
	}
}

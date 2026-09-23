package board

import (
	"testing"

	"edge/internal/oddspull"
)

func fptr(f float64) *float64 { return &f }

// dkOutcomes mirrors what oddspull.Parse actually returns from a real
// DraftKings sportscontent capture (see oddspull_test.go's fixture): the
// event string is "AWAY @ HOME" and a team-side selection leads with the
// team's own abbreviation ("SF 49ers", "LA Rams").
func dkOutcomes() []oddspull.Outcome {
	return []oddspull.Outcome{
		{Event: "SF @ LA", Market: "Moneyline", Selection: "SF 49ers", Price: -150, Category: "Game", MarketID: "M1"},
		{Event: "SF @ LA", Market: "Moneyline", Selection: "LA Rams", Price: 130, Category: "Game", MarketID: "M1"},
		{Event: "SF @ LA", Market: "Point Spread", Selection: "SF 49ers -3.5", Price: -110, Category: "Game", MarketID: "M2", Line: fptr(-3.5)},
		{Event: "SF @ LA", Market: "Point Spread", Selection: "LA Rams +3.5", Price: -110, Category: "Game", MarketID: "M2", Line: fptr(3.5)},
		{Event: "SF @ LA", Market: "Total", Selection: "Over", Price: -110, Category: "Game", MarketID: "M3", Line: fptr(44.5)},
		{Event: "SF @ LA", Market: "Total", Selection: "Under", Price: -105, Category: "Game", MarketID: "M3", Line: fptr(44.5)},
		// A non-Game outcome, and a prop, must never leak into game lines.
		{Event: "SF @ LA", Market: "Drake Maye Passing Yards", Selection: "Drake Maye 250+", Price: 141, Category: "Passing", MarketID: "M4", Line: fptr(250)},
	}
}

func TestOddsPairsFromOutcomesML(t *testing.T) {
	d := testWeek()
	pairs := d.OddsPairsFromOutcomes(dkOutcomes())

	var ml *OddsPair
	for i := range pairs {
		if pairs[i].GameID == "2026_01_SF_LA" && pairs[i].Market == "ml" {
			ml = &pairs[i]
		}
	}
	if ml == nil {
		t.Fatalf("no ml pair resolved, got %+v", pairs)
	}
	if ml.AwayPrice != -150 || ml.HomePrice != 130 {
		t.Errorf("ml prices = %v/%v, want -150 (SF, away) / 130 (LA, home)", ml.AwayPrice, ml.HomePrice)
	}
}

func TestOddsPairsFromOutcomesSpread(t *testing.T) {
	d := testWeek()
	pairs := d.OddsPairsFromOutcomes(dkOutcomes())

	var sp *OddsPair
	for i := range pairs {
		if pairs[i].GameID == "2026_01_SF_LA" && pairs[i].Market == "spread" {
			sp = &pairs[i]
		}
	}
	if sp == nil {
		t.Fatalf("no spread pair resolved, got %+v", pairs)
	}
	if sp.Line == nil || *sp.Line != -3.5 {
		t.Errorf("spread line = %v, want -3.5 (SF, the away team's number)", sp.Line)
	}
	if sp.AwayPrice != -110 || sp.HomePrice != -110 {
		t.Errorf("spread prices = %v/%v, want -110/-110", sp.AwayPrice, sp.HomePrice)
	}
}

func TestOddsPairsFromOutcomesTotal(t *testing.T) {
	d := testWeek()
	pairs := d.OddsPairsFromOutcomes(dkOutcomes())

	var tot *OddsPair
	for i := range pairs {
		if pairs[i].GameID == "2026_01_SF_LA" && pairs[i].Market == "total" {
			tot = &pairs[i]
		}
	}
	if tot == nil {
		t.Fatalf("no total pair resolved, got %+v", pairs)
	}
	if tot.Line == nil || *tot.Line != 44.5 {
		t.Errorf("total line = %v, want 44.5", tot.Line)
	}
	if tot.AwayPrice != -110 || tot.HomePrice != -105 {
		t.Errorf("total prices = %v/%v, want -110 (over)/-105 (under)", tot.AwayPrice, tot.HomePrice)
	}
}

func TestOddsPairsFromOutcomesReversedEvent(t *testing.T) {
	// A capture that lists the home team first is still a recognisable shape,
	// exactly like PlanImport tolerates a reversed paste.
	d := testWeek()
	outs := []oddspull.Outcome{
		{Event: "LA @ SF", Market: "Moneyline", Selection: "SF 49ers", Price: -150, Category: "Game", MarketID: "M1"},
		{Event: "LA @ SF", Market: "Moneyline", Selection: "LA Rams", Price: 130, Category: "Game", MarketID: "M1"},
	}
	pairs := d.OddsPairsFromOutcomes(outs)
	if len(pairs) != 1 || pairs[0].GameID != "2026_01_SF_LA" {
		t.Fatalf("got %+v, want one pair resolved to 2026_01_SF_LA", pairs)
	}
	if pairs[0].AwayPrice != -150 || pairs[0].HomePrice != 130 {
		t.Errorf("prices = %v/%v, want SF(-150)/LA(130) regardless of listing order",
			pairs[0].AwayPrice, pairs[0].HomePrice)
	}
}

func TestOddsPairsFromOutcomesUnscheduledEventSkipped(t *testing.T) {
	d := testWeek()
	outs := []oddspull.Outcome{
		{Event: "KC @ BUF", Market: "Moneyline", Selection: "KC Chiefs", Price: -150, Category: "Game", MarketID: "M1"},
		{Event: "KC @ BUF", Market: "Moneyline", Selection: "BUF Bills", Price: 130, Category: "Game", MarketID: "M1"},
	}
	if pairs := d.OddsPairsFromOutcomes(outs); len(pairs) != 0 {
		t.Errorf("a matchup not on this week's schedule must be skipped, got %+v", pairs)
	}
}

func TestPlanOddsSyncAndApply(t *testing.T) {
	d := testWeek()
	pairs := d.OddsPairsFromOutcomes(dkOutcomes())
	changes, err := d.PlanOddsSync(pairs, "fanatics")
	if err != nil {
		t.Fatalf("PlanOddsSync: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want 3 (ml, spread, total for SF_LA): %+v", len(changes), changes)
	}

	want := map[string]string{
		"ml":     "-150/+130",
		"spread": "-3.5 -110/-110",
		"total":  "44.5 -110/-105",
	}
	for _, c := range changes {
		if c.GameID != "2026_01_SF_LA" {
			t.Errorf("unexpected game in changes: %+v", c)
			continue
		}
		if c.New != want[c.Market] {
			t.Errorf("%s: got %q, want %q", c.Market, c.New, want[c.Market])
		}
	}

	// Nothing written until ApplyOddsSync runs.
	if got := d.Games["2026_01_SF_LA"].Books["fanatics"].ML; got != "" {
		t.Errorf("PlanOddsSync wrote %q; it must not touch the doc", got)
	}
	if err := d.ApplyOddsSync(changes); err != nil {
		t.Fatalf("ApplyOddsSync: %v", err)
	}
	l := d.Games["2026_01_SF_LA"].Books["fanatics"]
	if l.ML != "-150/+130" || l.Spread != "-3.5 -110/-110" || l.Total != "44.5 -110/-105" {
		t.Errorf("after apply: %+v", l)
	}
}

func TestPlanOddsSyncSkipsNoOps(t *testing.T) {
	d := testWeek() // GB/MIN fanatics ML already +100/-120
	pairs := []OddsPair{{GameID: "2026_01_GB_MIN", Market: "ml", AwayPrice: 100, HomePrice: -120}}
	changes, err := d.PlanOddsSync(pairs, "fanatics")
	if err != nil {
		t.Fatalf("PlanOddsSync: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("re-syncing the same price should change nothing, got %+v", changes)
	}
}

func TestPlanOddsSyncRejectsConsensusAndUnknownBook(t *testing.T) {
	d := testWeek()
	pairs := []OddsPair{{GameID: "2026_01_SF_LA", Market: "ml", AwayPrice: -150, HomePrice: 130}}
	if _, err := d.PlanOddsSync(pairs, Consensus); err == nil {
		t.Error("consensus must not be syncable")
	}
	if _, err := d.PlanOddsSync(pairs, "nosuchbook"); err == nil {
		t.Error("an unknown book must be rejected")
	}
}

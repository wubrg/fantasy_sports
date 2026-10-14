package betlog

import (
	"math"
	"strings"
	"testing"
	"time"
)

func validProjection() Projection {
	return Projection{
		Season: 2026, Week: 6, Player: "Cooper Kupp", Outcome: "receiving_yards",
		Scenario: "shootout", OurMedian: 21.0, MarketLine: 21.0, Baseline: 25.8, Trend: 0.0007,
		GeneratedAt: time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC),
		Kickoff:     time.Date(2026, 10, 16, 0, 15, 0, 0, time.UTC),
	}
}

func TestProjectionKey(t *testing.T) {
	p := validProjection()
	want := "2026/6/Cooper Kupp/receiving_yards/shootout"
	if got := p.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestProjectionValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Projection)
	}{
		{"no season", func(p *Projection) { p.Season = 0 }},
		{"no week", func(p *Projection) { p.Week = 0 }},
		{"no player", func(p *Projection) { p.Player = "" }},
		{"no outcome", func(p *Projection) { p.Outcome = "" }},
		{"no scenario", func(p *Projection) { p.Scenario = "" }},
		{"no kickoff", func(p *Projection) { p.Kickoff = time.Time{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validProjection()
			c.mut(&p)
			if err := p.Validate(); err == nil {
				t.Errorf("expected Validate to reject %s", c.name)
			}
		})
	}
}

func TestProjectionValidateAcceptsAValidOne(t *testing.T) {
	if err := validProjection().Validate(); err != nil {
		t.Errorf("expected a valid projection to pass, got %v", err)
	}
}

func TestLoadProjectionsRefusesABetLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	if _, err := PlaceBet(path, Bet{Selection: "x", Price: -110, Bankroll: "real money", Stake: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjections(path); err == nil {
		t.Error("expected LoadProjections to refuse a file holding wagers")
	}
}

func TestLoadProjectionsRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, err := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != id {
		t.Fatalf("expected 1 projection with id %q, got %+v", id, loaded)
	}
	if loaded[0].Result != Open {
		t.Errorf("expected an unsettled projection to be Open, got %v", loaded[0].Result)
	}
	if loaded[0].Actual != nil {
		t.Errorf("expected Actual to be nil before settlement, got %v", *loaded[0].Actual)
	}
}

func TestRecordRefusesAfterKickoff(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection() // Kickoff is in the future relative to time.Now() in the fixture
	p.Kickoff = time.Now().Add(-time.Minute) // already kicked off
	if _, err := RecordProjection(path, p, time.Now()); err == nil {
		t.Error("expected RecordProjection to refuse a projection recorded after its own kickoff")
	}
}

func TestSettleProjection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))

	if err := SettleProjection(path, id, 41.0, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].Result != Measured {
		t.Errorf("expected Measured, got %v", loaded[0].Result)
	}
	if loaded[0].Actual == nil || *loaded[0].Actual != 41.0 {
		t.Fatalf("expected Actual 41.0, got %v", loaded[0].Actual)
	}
}

func TestVoidProjectionRequiresAReason(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))

	if err := VoidProjection(path, id, ""); err == nil {
		t.Error("expected VoidProjection to require a reason")
	}
	if err := VoidProjection(path, id, "player inactive"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadProjections(path)
	if loaded[0].Result != Void {
		t.Errorf("expected Void, got %v", loaded[0].Result)
	}
	if loaded[0].Actual != nil {
		t.Errorf("expected nil Actual on a void, got %v", *loaded[0].Actual)
	}
}

func TestSettleProjectionCannotBeResettled(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))
	if err := SettleProjection(path, id, 41.0, "first"); err != nil {
		t.Fatal(err)
	}
	// Appending a second settlement is possible -- the file is append-only --
	// but the log must then refuse to LOAD rather than take the last write.
	// This mirrors TestBeliefCannotBeResettled (belief_test.go) exactly: the
	// second Settle/SettleBelief call itself succeeds, because Append has no
	// read-before-write check; only scan()'s replay (exercised via Load) is
	// where the double-settle is caught. A test asserting the second
	// SettleProjection call's own return value would be testing behavior
	// this package does not have anywhere else, and no new code should be
	// added to manufacture it for projections alone.
	if err := SettleProjection(path, id, 50.0, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjections(path); err == nil {
		t.Fatal("a second settlement was accepted; hindsight is what this log exists to prevent")
	} else if !strings.Contains(err.Error(), "already settled") {
		t.Errorf("error should name the double settlement, got: %v", err)
	}
}

func TestScoreProjectionsMatchesAHandWorkedExample(t *testing.T) {
	mk := func(median, line, actual float64) SettledProjection {
		a := actual
		return SettledProjection{
			Projection: Projection{Outcome: "receiving_yards", OurMedian: median, MarketLine: line},
			Result:     Measured, Actual: &a,
		}
	}
	rows := []SettledProjection{mk(50, 60, 55), mk(50, 60, 48), mk(50, 60, 65), mk(50, 60, 52)}

	s, err := ScoreProjections(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.N != 4 {
		t.Errorf("N = %d, want 4", s.N)
	}
	if math.Abs(s.OurMAE-6.0) > 1e-9 {
		t.Errorf("OurMAE = %v, want 6.0", s.OurMAE)
	}
	if math.Abs(s.MarketMAE-7.5) > 1e-9 {
		t.Errorf("MarketMAE = %v, want 7.5", s.MarketMAE)
	}
	if math.Abs(s.WinRate-0.5) > 1e-9 {
		t.Errorf("WinRate = %v, want 0.5", s.WinRate)
	}
	if math.Abs(s.OurBias-(-5.0)) > 1e-9 {
		t.Errorf("OurBias = %v, want -5.0", s.OurBias)
	}
	if math.Abs(s.MarketBias-5.0) > 1e-9 {
		t.Errorf("MarketBias = %v, want 5.0", s.MarketBias)
	}
}

func TestScoreProjectionsExcludesUnsettledAndVoid(t *testing.T) {
	a := 55.0
	rows := []SettledProjection{
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Measured, Actual: &a},
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Open}, // not yet settled
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Void}, // unmeasurable
	}
	s, err := ScoreProjections(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.N != 1 {
		t.Errorf("N = %d, want 1 (only the Measured row counts)", s.N)
	}
}

func TestByOutcomeGroupsSeparately(t *testing.T) {
	a := 55.0
	rows := []SettledProjection{
		{Projection: Projection{Outcome: "receiving_yards", OurMedian: 50, MarketLine: 60}, Result: Measured, Actual: &a},
		{Projection: Projection{Outcome: "rushing_yards", OurMedian: 40, MarketLine: 45}, Result: Measured, Actual: &a},
	}
	byOutcome, err := ByOutcome(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(byOutcome) != 2 {
		t.Fatalf("expected 2 outcome groups, got %d: %+v", len(byOutcome), byOutcome)
	}
	if byOutcome["receiving_yards"].N != 1 || byOutcome["rushing_yards"].N != 1 {
		t.Errorf("expected 1 row per outcome, got %+v", byOutcome)
	}
}

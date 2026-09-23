package betlog

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edge/internal/wager"
)

func tmpLog(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "bets.jsonl")
}

func mustPlace(t *testing.T, path string, b Bet) string {
	t.Helper()
	id, err := PlaceBet(path, b)
	if err != nil {
		t.Fatalf("placing %q: %v", b.Selection, err)
	}
	return id
}

// TestRoundTrip covers the basic write-then-read path.
func TestRoundTrip(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "Player A over 52.5 rec yds",
		Price:     -110,
		Book:      wager.DraftKings,
		Bankroll:  "real money",
		Stake:     5,
		Scenario:  "shootout",
		Predicted: 0.58,
	})
	if err := Settle(path, id, Won, nil, nil, "cleared by 12"); err != nil {
		t.Fatal(err)
	}

	bets, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bets) != 1 {
		t.Fatalf("got %d bets, want 1", len(bets))
	}
	if bets[0].Result != Won {
		t.Errorf("result = %v, want won", bets[0].Result)
	}
	if bets[0].Bet.Predicted != 0.58 {
		t.Errorf("predicted = %v, want 0.58", bets[0].Bet.Predicted)
	}
}

// TestSettlementIsAppendedNotRewritten is the integrity property. The original
// prediction must still be on disk verbatim after settling, so a bet cannot be
// re-predicted once the outcome is known.
func TestSettlementIsAppendedNotRewritten(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "Player B over 40.5",
		Price:     150,
		Bankroll:  "real money",
		Stake:     2,
		Predicted: 0.44,
	})

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Settle(path, id, Lost, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("settling must append; the original prediction was modified")
	}
	if lines := strings.Count(strings.TrimSpace(string(after)), "\n") + 1; lines != 2 {
		t.Errorf("got %d lines, want 2 (bet + settlement)", lines)
	}
	if !strings.Contains(string(after), `"predicted":0.44`) {
		t.Error("the original predicted probability must survive settlement")
	}
}

// TestCalibrationHeadline is the number the log exists to produce: predicted
// versus realised hit rate.
func TestCalibrationHeadline(t *testing.T) {
	path := tmpLog(t)
	// Ten bets each predicted at 50%; only three win.
	for i := 0; i < 10; i++ {
		id := mustPlace(t, path, Bet{
			Selection: "bet",
			Price:     100,
			Bankroll:  "real money",
			Stake:     1,
			Predicted: 0.5,
		})
		res := Lost
		if i < 3 {
			res = Won
		}
		if err := Settle(path, id, res, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}

	bets, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Score(bets, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Scored != 10 || c.Wins != 3 {
		t.Fatalf("got %d scored / %d wins, want 10/3", c.Scored, c.Wins)
	}
	if !closeTo(c.Expected, 0.5, 1e-9) {
		t.Errorf("expected = %.4f, want 0.5", c.Expected)
	}
	if !closeTo(c.Realised, 0.3, 1e-9) {
		t.Errorf("realised = %.4f, want 0.3", c.Realised)
	}
	// Even money, 3 of 10: +3 -7 = -4 on 10 staked.
	if !closeTo(c.StakedROI, -0.4, 1e-9) {
		t.Errorf("ROI = %.4f, want -0.4", c.StakedROI)
	}
}

// TestBonusBetLossCostsNothing checks that the bankroll distinction survives
// into the scoring, since a losing bonus bet is not a cash loss.
func TestBonusBetLossCostsNothing(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "longshot", Price: 900, Bankroll: "bonus bet", Stake: 10, Predicted: 0.12,
	})
	if err := Settle(path, id, Lost, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	bets := mustLoad(t, path)
	c, err := Score(bets, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profit != 0 {
		t.Errorf("a losing bonus bet should cost nothing, got profit %.4f", c.Profit)
	}

	// The same wager in cash does cost the stake.
	path2 := tmpLog(t)
	id2 := mustPlace(t, path2, Bet{
		Selection: "longshot", Price: 900, Bankroll: "real money", Stake: 10, Predicted: 0.12,
	})
	if err := Settle(path2, id2, Lost, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	bets2 := mustLoad(t, path2)
	c2, err := Score(bets2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(c2.Profit, -10, 1e-9) {
		t.Errorf("a losing cash bet should cost the stake, got %.4f", c2.Profit)
	}
}

// TestPushesAreExcluded: a push tested no prediction.
func TestPushesAreExcluded(t *testing.T) {
	path := tmpLog(t)
	for _, r := range []Result{Won, Pushed, Void, Lost} {
		id := mustPlace(t, path, Bet{
			Selection: "b", Price: -110, Bankroll: "real money", Stake: 1, Predicted: 0.5,
		})
		if err := Settle(path, id, r, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	// One still open.
	mustPlace(t, path, Bet{Selection: "b", Price: -110, Bankroll: "real money", Stake: 1, Predicted: 0.5})

	bets := mustLoad(t, path)
	c, err := Score(bets, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Scored != 2 {
		t.Errorf("scored = %d, want 2 (won + lost only)", c.Scored)
	}
	if c.Excluded != 2 {
		t.Errorf("excluded = %d, want 2 (push + void)", c.Excluded)
	}
	if c.Open != 1 {
		t.Errorf("open = %d, want 1", c.Open)
	}
}

// TestBySourceSplitsCalibration is the reason Source is recorded: it answers
// whether your own reads or the line-derived ones were better.
func TestBySourceSplitsCalibration(t *testing.T) {
	path := tmpLog(t)
	place := func(src string, predicted float64, res Result) {
		id := mustPlace(t, path, Bet{
			Selection: "b", Price: 100, Bankroll: "real money", Stake: 1,
			ScenarioSource: src, Predicted: predicted,
		})
		if err := Settle(path, id, res, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Stated reads: predicted 60%, went 1 for 4.
	for i := 0; i < 4; i++ {
		r := Lost
		if i == 0 {
			r = Won
		}
		place("stated", 0.6, r)
	}
	// Derived: predicted 40%, went 2 for 4.
	for i := 0; i < 4; i++ {
		r := Lost
		if i < 2 {
			r = Won
		}
		place("derived-from-line", 0.4, r)
	}

	bets := mustLoad(t, path)
	by, err := BySource(bets)
	if err != nil {
		t.Fatal(err)
	}
	if len(by) != 2 {
		t.Fatalf("got %d sources, want 2", len(by))
	}
	stated, derived := by["stated"], by["derived-from-line"]
	if !closeTo(stated.Realised, 0.25, 1e-9) || !closeTo(stated.Expected, 0.6, 1e-9) {
		t.Errorf("stated: expected %.2f realised %.2f, want 0.60/0.25", stated.Expected, stated.Realised)
	}
	if !closeTo(derived.Realised, 0.5, 1e-9) || !closeTo(derived.Expected, 0.4, 1e-9) {
		t.Errorf("derived: expected %.2f realised %.2f, want 0.40/0.50", derived.Expected, derived.Realised)
	}
	if got := SortedSources(by); got[0] != "derived-from-line" || got[1] != "stated" {
		t.Errorf("sources not sorted: %v", got)
	}
}

// TestCorruptLogFailsLoudly: a settlement with no bet means the file was edited
// or an id was mistyped, and hiding it would corrupt the calibration silently.
func TestCorruptLogFailsLoudly(t *testing.T) {
	path := tmpLog(t)
	if err := Settle(path, "no-such-bet", Won, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("a settlement for an unknown bet must fail")
	}

	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(bad, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("malformed JSON must fail")
	}

	// A missing file is not an error -- it is an empty log.
	empty, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Errorf("a missing log should read as empty, got %v", err)
	}
	if len(empty) != 0 {
		t.Error("a missing log should be empty")
	}
}

// TestPlaceBetValidates keeps the fail-loudly contract at the entry point.
func TestPlaceBetValidates(t *testing.T) {
	path := tmpLog(t)
	if _, err := PlaceBet(path, Bet{Price: -110, Predicted: 0.5}); err == nil {
		t.Error("a bet with no selection must be rejected")
	}
	if _, err := PlaceBet(path, Bet{Selection: "x", Price: -110, Predicted: 1.5}); err == nil {
		t.Error("an out-of-range prediction must be rejected")
	}
	if _, err := PlaceBet(path, Bet{Selection: "x", Price: 0, Predicted: 0.5}); err == nil {
		t.Error("an invalid price must be rejected")
	}
}

// mustLoad fails the test on a load error rather than swallowing it. Swallowing
// it once already turned a duplicate-id bug into three unrelated-looking
// assertion failures.
func mustLoad(t *testing.T, path string) []Settled {
	t.Helper()
	bets, err := Load(path)
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	return bets
}

func closeTo(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// TestCannotResettle is the integrity property the package doc claims and did
// not have.
//
// Append-only bytes are not append-only meaning. Load used to take the last
// write, so a second settlement silently rewrote the outcome: one bet's ROI
// swung from -100% to +90.9% with three valid lines on disk and no error.
// Hindsight is exactly what a calibration log exists to prevent.
func TestCannotResettle(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "x", Price: -110, Bankroll: "real money", Stake: 100, Predicted: 0.5,
	})
	if err := Settle(path, id, Lost, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	before := mustLoad(t, path)
	if before[0].Result != Lost {
		t.Fatalf("setup: result = %v", before[0].Result)
	}

	// The append itself may succeed -- the file is a raw event stream -- but the
	// log must not READ as though the outcome changed.
	_ = Settle(path, id, Won, nil, nil, "changed my mind")
	if _, err := Load(path); err == nil {
		t.Error("a second settlement must make the log fail loudly, not silently rewrite the outcome")
	}
}

// TestUnknownResultIsRejected: a bogus result used to load without error and be
// filed as "excluded", quietly dropping the bet from calibration.
func TestUnknownResultIsRejected(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "x", Price: -110, Bankroll: "real money", Stake: 1, Predicted: 0.5,
	})
	if err := Append(path, Entry{Kind: KindSettle, ID: id, Result: Result("WON")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("an unrecognised result must be rejected, not silently excluded")
	}
}

// TestBankrollMatchingIsExact closes an accounting hole. Matching used to be a
// substring test for "bonus", so anything else silently got cash accounting --
// including "snr", which the CLI accepts as a bonus-bet alias. A losing bonus
// bet costs nothing; a losing cash bet costs the stake. The two differ by the
// whole stake on every loss.
func TestBankrollMatchingIsExact(t *testing.T) {
	for _, good := range []string{"real money", "bonus bet", "Bonus Bet", "  real money  "} {
		if _, err := ParseBankroll(good); err != nil {
			t.Errorf("%q should parse: %v", good, err)
		}
	}
	for _, bad := range []string{"snr", "promo", "", "bonus", "cash", "free bet"} {
		if _, err := ParseBankroll(bad); err == nil {
			t.Errorf("%q must be rejected rather than defaulted to cash", bad)
		}
	}

	// It must be rejected at the boundary too, not just parsed strictly later.
	path := tmpLog(t)
	if _, err := PlaceBet(path, Bet{
		Selection: "x", Price: 900, Bankroll: "snr", Stake: 10, Predicted: 0.12,
	}); err == nil {
		t.Error("PlaceBet must reject an unrecognised bankroll")
	}
}

// TestStakeMustBePositive: a non-positive stake used to be accepted, then
// counted toward calibration while contributing nothing to ROI, because Score
// incremented Scored before skipping it.
func TestStakeMustBePositive(t *testing.T) {
	path := tmpLog(t)
	for _, bad := range []float64{0, -500, math.NaN(), math.Inf(1)} {
		if _, err := PlaceBet(path, Bet{
			Selection: "x", Price: -110, Bankroll: "real money", Stake: bad, Predicted: 0.5,
		}); err == nil {
			t.Errorf("stake %v must be rejected", bad)
		}
	}
}

// TestMultiLegSettledPriceScoresRealizedPayout is the actual bug fix: a
// same-game parlay that loses a leg to a void gets repriced by the book, and
// Score must use that real payout, not the price originally quoted.
func TestMultiLegSettledPriceScoresRealizedPayout(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "SGP: A anytime TD + B over 5.5 recs + C over 250 pass yds",
		Price:     450, // combined price as originally struck
		Bankroll:  "real money",
		Stake:     10,
		Predicted: 0.3,
		Legs: []Leg{
			{Selection: "A anytime TD", Price: -150},
			{Selection: "B over 5.5 recs", Price: -110},
			{Selection: "C over 250 pass yds", Price: 120},
		},
	})
	// C's leg voids (player ruled out pregame); the book reprices the
	// remaining two-leg parlay down to +180 and pays that out.
	settledPrice := wager.American(180)
	legResults := []Leg{
		{Selection: "A anytime TD", Result: Won},
		{Selection: "B over 5.5 recs", Result: Won},
		{Selection: "C over 250 pass yds", Result: Void},
	}
	if err := Settle(path, id, Won, &settledPrice, legResults, "C voided, repriced to +180"); err != nil {
		t.Fatal(err)
	}

	bets := mustLoad(t, path)
	if len(bets) != 1 {
		t.Fatalf("got %d bets, want 1", len(bets))
	}
	if bets[0].SettledPrice == nil || *bets[0].SettledPrice != settledPrice {
		t.Fatalf("SettledPrice = %v, want %v", bets[0].SettledPrice, settledPrice)
	}
	if len(bets[0].LegResults) != 3 {
		t.Fatalf("got %d leg results, want 3", len(bets[0].LegResults))
	}

	c, err := Score(bets, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantMultiple, err := settledPrice.ProfitMultiple()
	if err != nil {
		t.Fatal(err)
	}
	wantProfit := 10 * wantMultiple
	if !closeTo(c.Profit, wantProfit, 1e-9) {
		t.Errorf("profit = %.4f, want %.4f (scored against SettledPrice +180, not the original +450 quote)", c.Profit, wantProfit)
	}

	// Sanity: scoring against the ORIGINAL quote would give a different
	// (much larger) profit, confirming the settled price actually took effect.
	originalMultiple, err := wager.American(450).ProfitMultiple()
	if err != nil {
		t.Fatal(err)
	}
	if closeTo(c.Profit, 10*originalMultiple, 1e-9) {
		t.Error("profit matches the original +450 quote; SettledPrice was not used")
	}
}

// TestSingleLegBetUnaffectedByMultiLegFields is the regression check: a plain
// single-leg bet with no Legs and no SettledPrice on settle must score exactly
// as it always has.
func TestSingleLegBetUnaffectedByMultiLegFields(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "Player A over 52.5 rec yds",
		Price:     -110,
		Bankroll:  "real money",
		Stake:     5,
		Predicted: 0.58,
	})
	if err := Settle(path, id, Won, nil, nil, "cleared by 12"); err != nil {
		t.Fatal(err)
	}
	bets := mustLoad(t, path)
	if bets[0].SettledPrice != nil {
		t.Errorf("SettledPrice = %v, want nil for a plain single-leg bet", bets[0].SettledPrice)
	}
	if bets[0].LegResults != nil {
		t.Errorf("LegResults = %v, want nil for a plain single-leg bet", bets[0].LegResults)
	}
	c, err := Score(bets, nil)
	if err != nil {
		t.Fatal(err)
	}
	pm, err := wager.American(-110).ProfitMultiple()
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(c.Profit, 5*pm, 1e-9) {
		t.Errorf("profit = %.4f, want %.4f (scored against Bet.Price)", c.Profit, 5*pm)
	}
}

// TestEntryRoundTripsLegsAndSettledPrice confirms the new fields survive a
// marshal-to-JSON-and-scan round trip losslessly.
func TestEntryRoundTripsLegsAndSettledPrice(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "SGP round trip", Price: 300, Bankroll: "real money", Stake: 20, Predicted: 0.25,
		Legs: []Leg{
			{Selection: "leg one", Price: -200},
			{Selection: "leg two", Price: 150},
		},
	})
	settledPrice := wager.American(-105)
	legResults := []Leg{
		{Selection: "leg one", Result: Won},
		{Selection: "leg two", Result: Void},
	}
	if err := Settle(path, id, Pushed, &settledPrice, legResults, "leg two voided"); err != nil {
		t.Fatal(err)
	}

	bets := mustLoad(t, path)
	if len(bets) != 1 {
		t.Fatalf("got %d bets, want 1", len(bets))
	}
	b := bets[0]
	if len(b.Bet.Legs) != 2 || b.Bet.Legs[0].Selection != "leg one" || b.Bet.Legs[0].Price != -200 ||
		b.Bet.Legs[1].Selection != "leg two" || b.Bet.Legs[1].Price != 150 {
		t.Errorf("Bet.Legs did not round-trip: %+v", b.Bet.Legs)
	}
	if b.SettledPrice == nil || *b.SettledPrice != settledPrice {
		t.Errorf("SettledPrice did not round-trip: %v", b.SettledPrice)
	}
	if len(b.LegResults) != 2 || b.LegResults[0].Result != Won || b.LegResults[1].Result != Void {
		t.Errorf("LegResults did not round-trip: %+v", b.LegResults)
	}
}

// TestMultiLegDuplicateAndUnknownSettlementStillRejected confirms the core
// integrity properties (no re-settling, no settling an unknown id) hold
// unchanged with the extended Settle signature.
func TestMultiLegDuplicateAndUnknownSettlementStillRejected(t *testing.T) {
	path := tmpLog(t)
	id := mustPlace(t, path, Bet{
		Selection: "SGP", Price: 300, Bankroll: "real money", Stake: 10, Predicted: 0.3,
		Legs: []Leg{{Selection: "leg one", Price: -150}},
	})
	settledPrice := wager.American(200)
	if err := Settle(path, id, Won, &settledPrice, nil, "first settle"); err != nil {
		t.Fatal(err)
	}
	// A second settlement, even with different multi-leg detail, must make the
	// log fail loudly rather than silently overwrite the outcome.
	other := wager.American(-500)
	_ = Settle(path, id, Lost, &other, nil, "changed my mind")
	if _, err := Load(path); err == nil {
		t.Error("a second settlement must make the log fail loudly, not silently rewrite the outcome")
	}

	path2 := tmpLog(t)
	if err := Settle(path2, "no-such-bet", Won, &settledPrice, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path2); err == nil {
		t.Error("a settlement for an unknown bet must fail even when it carries multi-leg detail")
	}
}

// TestUnknownBookRejected: bonus-bet rules differ by book, and an unrecognised
// one used to inherit the permissive default.
func TestUnknownBookRejected(t *testing.T) {
	path := tmpLog(t)
	if _, err := PlaceBet(path, Bet{
		Selection: "x", Price: -110, Bankroll: "real money", Stake: 1, Predicted: 0.5,
		Book: wager.Book("Bovada"),
	}); err == nil {
		t.Error("an unknown book must be rejected")
	}
	// A known book, however cased, is fine.
	if _, err := PlaceBet(path, Bet{
		Selection: "x", Price: -110, Bankroll: "real money", Stake: 1, Predicted: 0.5,
		Book: wager.Book("FanDuel"),
	}); err != nil {
		t.Errorf("a known book should be accepted regardless of case: %v", err)
	}
}

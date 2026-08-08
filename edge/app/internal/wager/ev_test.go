package wager

import (
	"math"
	"testing"
)

// TestProfitabilityMatrix pins all 14 cells of the "Profitability Matrix"
// (edge-of-vigor.md p.8) as literal expected values. Every cell was verified
// correct; this test keeps the library honest against the source document.
//
// Values are ROI per unit staked, i.e. EV on a stake of 1.
func TestProfitabilityMatrix(t *testing.T) {
	cases := []struct {
		odds       American
		at55, at60 float64
	}{
		{-200, -0.175, -0.100},
		{-150, -0.083, 0.000},
		{-130, -0.027, 0.062},
		{-110, 0.050, 0.145},
		{100, 0.100, 0.200},
		{120, 0.210, 0.320},
		{150, 0.375, 0.500},
	}
	for _, c := range cases {
		for _, tc := range []struct {
			p    float64
			want float64
		}{{0.55, c.at55}, {0.60, c.at60}} {
			got, err := EVRealMoney(tc.p, c.odds, 1)
			if err != nil {
				t.Fatalf("%d @ %.0f%%: %v", c.odds, tc.p*100, err)
			}
			if !closeTo(got, tc.want, tol) {
				t.Errorf("%d @ p=%.2f: EV = %+.4f, source matrix says %+.3f",
					c.odds, tc.p, got, tc.want)
			}
		}
	}
}

// TestKeyTakeaways pins the three headline claims of edge-of-vigor.md p.9.
func TestKeyTakeaways(t *testing.T) {
	// "Heavy Favorites Kill ROI: even if you are 65% sure, -200 still loses
	// money long-term (-2.5% EV)."
	ev, err := EVRealMoney(0.65, -200, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(ev, -0.025, tol) {
		t.Errorf("-200 @ 65%%: EV = %+.4f, source says -2.5%%", ev)
	}
	if ev >= 0 {
		t.Error("the -200 trap: 65% confidence must still be negative EV")
	}

	// "Underdogs Scale Fast: a +120 dog you think is a coin flip is +10% EV."
	ev, err = EVRealMoney(0.50, 120, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(ev, 0.100, tol) {
		t.Errorf("+120 @ 50%%: EV = %+.4f, source says +10%%", ev)
	}

	// "The -110 Trap": at exactly the breakeven rate, EV is zero. Being
	// "kind of sure" (50%) is a loss.
	be, err := American(-110).Breakeven()
	if err != nil {
		t.Fatal(err)
	}
	ev, err = EVRealMoney(be, -110, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(ev, 0, 1e-9) {
		t.Errorf("EV at the breakeven rate must be exactly 0, got %+.9f", ev)
	}
	ev, _ = EVRealMoney(0.50, -110, 1)
	if ev >= 0 {
		t.Error("a coin flip at -110 must be negative EV")
	}
}

// TestSafetyTax pins the -200 table from edge-of-vigor.md p.13 ($10 stake).
func TestSafetyTax(t *testing.T) {
	cases := []struct{ p, want float64 }{
		{0.667, 0.00}, {0.70, 0.50}, {0.75, 1.25}, {0.80, 2.00},
	}
	for _, c := range cases {
		got, err := EVRealMoney(c.p, -200, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(got, c.want, 0.01) {
			t.Errorf("-200 @ %.1f%% on $10: EV = %+.4f, source says %+.2f", c.p*100, got, c.want)
		}
	}
}

// TestBonusBetLongshotImperative pins the three worked examples from
// analytical-hobbyist.md p.6 — a $100 SNR bonus bet at three prices — and the
// monotonicity claim they exist to demonstrate.
func TestBonusBetLongshotImperative(t *testing.T) {
	cases := []struct {
		odds American
		want float64
	}{
		{-1000, 9.09},
		{100, 50.00},
		{1000, 90.90},
	}
	var prev float64
	for i, c := range cases {
		p, err := c.odds.ImpliedRaw()
		if err != nil {
			t.Fatal(err)
		}
		got, err := EVBonusBet(p, c.odds, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(got, c.want, 0.01) {
			t.Errorf("$100 bonus bet @ %d: EV = $%.2f, source says $%.2f", c.odds, got, c.want)
		}
		if i > 0 && got <= prev {
			t.Errorf("bonus bet EV must increase with odds length: %d gave $%.2f after $%.2f",
				c.odds, got, prev)
		}
		prev = got
	}
}

// TestBonusConversionClosedForm pins the identity none of the source documents
// state: at fair odds a bonus bet is worth stake × (1 − p). It must reproduce
// the three worked examples above exactly.
func TestBonusConversionClosedForm(t *testing.T) {
	for _, odds := range []American{-1000, -250, -110, 100, 300, 1000, 4000} {
		p, err := odds.ImpliedRaw()
		if err != nil {
			t.Fatal(err)
		}
		viaFormula, err := EVBonusBet(p, odds, 100)
		if err != nil {
			t.Fatal(err)
		}
		viaClosedForm, err := BonusConversionAtFairOdds(odds, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(viaFormula, viaClosedForm, 1e-9) {
			t.Errorf("%d: p·profit = %.6f but stake·(1−p) = %.6f", odds, viaFormula, viaClosedForm)
		}
	}
}

// TestBonusVsRealMoneyAsymmetry pins the exact size of the gap between the two
// EV formulas. This is the highest-value piece of math in the corpus: it is
// why bonus bets belong on longshots and real money does not.
func TestBonusVsRealMoneyAsymmetry(t *testing.T) {
	const stake = 100
	for _, odds := range []American{-500, -110, 100, 300, 1000} {
		for _, p := range []float64{0.1, 0.35, 0.5, 0.8} {
			real, err := EVRealMoney(p, odds, stake)
			if err != nil {
				t.Fatal(err)
			}
			bonus, err := EVBonusBet(p, odds, stake)
			if err != nil {
				t.Fatal(err)
			}
			// The bonus bet omits exactly the (1−p)·stake downside term.
			wantGap := (1 - p) * stake
			if !closeTo(bonus-real, wantGap, 1e-9) {
				t.Errorf("%d @ p=%.2f: gap = %.6f, want (1−p)·stake = %.6f",
					odds, p, bonus-real, wantGap)
			}
			if bonus <= real {
				t.Errorf("%d @ p=%.2f: bonus EV must exceed real-money EV", odds, p)
			}
		}
	}
}

// TestSourceErrorMinus250BonusBet pins the correct value for a claim that is
// wrong in analytical-hobbyist.md p.6, which states $30.08.
func TestSourceErrorMinus250BonusBet(t *testing.T) {
	p, err := American(-250).ImpliedRaw()
	if err != nil {
		t.Fatal(err)
	}
	got, err := EVBonusBet(p, -250, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(got, 28.57, 0.01) {
		t.Errorf("$100 bonus bet @ -250: EV = $%.2f, want $28.57", got)
	}
	if closeTo(got, 30.08, 0.01) {
		t.Error("$30.08 is the source's incorrect figure and must not be reproduced")
	}
}

// TestSourceErrorBonusTableFirstRows pins the correct values for the three
// rows that are wrong in edge-of-vigor.md Tables D and E, where the -200
// profit multiplier (0.50x) was used for -500, -400 and -300.
//
// Tier 1 means p_true is 5 percentage points above the implied probability.
func TestSourceErrorBonusTableFirstRows(t *testing.T) {
	cases := []struct {
		odds         American
		stake        float64
		correct      float64
		sourceStated float64
	}{
		{-500, 5, 0.88, 2.21},
		{-400, 5, 1.06, 2.13},
		{-300, 5, 1.33, 2.00},
		{-500, 10, 1.77, 4.42},
		{-400, 10, 2.13, 4.25},
		{-300, 10, 2.67, 4.00},
	}
	for _, c := range cases {
		implied, err := c.odds.ImpliedRaw()
		if err != nil {
			t.Fatal(err)
		}
		got, err := EVBonusBet(implied+0.05, c.odds, c.stake)
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(got, c.correct, 0.01) {
			t.Errorf("$%.0f bonus bet @ %d tier 1: EV = $%.2f, want $%.2f",
				c.stake, c.odds, got, c.correct)
		}
		if closeTo(got, c.sourceStated, 0.01) {
			t.Errorf("$%.0f @ %d: reproduced the source's incorrect $%.2f",
				c.stake, c.odds, c.sourceStated)
		}
	}

	// The error inverted the document's own thesis: as printed, a -500 bonus
	// bet appears to convert better than a -200 one. Correct math is
	// monotonic in odds length.
	var prev float64
	for i, odds := range []American{-500, -400, -300, -200, -110, 100, 1000} {
		implied, _ := odds.ImpliedRaw()
		got, err := EVBonusBet(implied+0.05, odds, 5)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && got <= prev {
			t.Errorf("bonus EV must rise with odds length: %d gave $%.2f after $%.2f",
				odds, got, prev)
		}
		prev = got
	}
}

// TestReportSeparatesHurdleFromMarket is the de-vigging correction expressed
// as behaviour: an estimate can clear the breakeven hurdle while failing to
// beat the market's actual view, and the report must distinguish the two.
func TestReportSeparatesHurdleFromMarket(t *testing.T) {
	m := Market{A: -110, B: -110} // hurdle 52.38%, market view 50.0%

	// 53% clears the hurdle AND beats the de-vigged market.
	r, err := Report(m, 0.53, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ClearsHurdle || !r.BeatsMarket {
		t.Errorf("p=0.53 should clear hurdle (%.4f) and beat market (%.4f)", r.Breakeven, r.FairDevig)
	}

	// 51% beats the market's 50% view but does NOT clear the 52.38% hurdle:
	// a real disagreement that is still not worth betting at this price.
	r, err = Report(m, 0.51, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.ClearsHurdle {
		t.Error("p=0.51 must not clear a 52.38% hurdle")
	}
	if !r.BeatsMarket {
		t.Error("p=0.51 does disagree with a de-vigged 50% market")
	}
	if r.EVReal >= 0 {
		t.Errorf("p=0.51 at -110 must be negative EV, got %+.6f", r.EVReal)
	}
	if r.FairDevig >= r.Breakeven {
		t.Error("de-vigged fair value must sit below the raw hurdle rate")
	}
}

// TestNoSilentZeros is the "fail loudly" contract for the EV layer. A missing
// or nonsensical input must never produce a zero that reads as "no edge".
func TestNoSilentZeros(t *testing.T) {
	for _, p := range []float64{-0.01, 1.01, 2, -1} {
		if _, err := EVRealMoney(p, -110, 1); err == nil {
			t.Errorf("probability %v must be rejected", p)
		}
		if _, err := EVBonusBet(p, -110, 1); err == nil {
			t.Errorf("probability %v must be rejected for bonus bets", p)
		}
	}
	for _, stake := range []float64{0, -1} {
		if _, err := EVRealMoney(0.5, -110, stake); err == nil {
			t.Errorf("stake %v must be rejected", stake)
		}
	}
	if _, err := EVRealMoney(0.5, 0, 1); err == nil {
		t.Error("an invalid price must be rejected")
	}
	// Report needs both sides of the market; it must not invent the other.
	if _, err := Report(Market{A: -110, B: 0}, 0.55, 1); err == nil {
		t.Error("Report must fail without a valid opposing price")
	}
}

// TestNonFiniteInputsAreRejected closes the gap every other "rejects nonsense"
// test in this package shared: they all used finite out-of-range values
// (-0.1, 1.1, 2, -1) and none used NaN or Inf.
//
// NaN defeats an ordinary range check, because `p < 0 || p > 1` is FALSE for
// NaN. An audit rode that through every validator here and out the far end as
// a printed price of -9223372036854775808, reachable from the command line
// since strconv.ParseFloat accepts "NaN".
func TestNonFiniteInputsAreRejected(t *testing.T) {
	nasty := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}

	for _, v := range nasty {
		if _, err := EVRealMoney(v, -110, 1); err == nil {
			t.Errorf("EVRealMoney accepted probability %v", v)
		}
		if _, err := EVBonusBet(v, -110, 1); err == nil {
			t.Errorf("EVBonusBet accepted probability %v", v)
		}
		if _, err := EVRealMoney(0.5, -110, v); err == nil {
			t.Errorf("EVRealMoney accepted stake %v", v)
		}
		if _, err := MinPrice(RealMoney, v, 0); err == nil {
			t.Errorf("MinPrice accepted probability %v", v)
		}
		if _, err := MinPrice(RealMoney, 0.5, v); err == nil {
			t.Errorf("MinPrice accepted target %v", v)
		}
		if _, err := MinPriceForConversion(v); err == nil {
			t.Errorf("MinPriceForConversion accepted target %v", v)
		}
		if _, err := BlendedProb(v, 0.5, 0.3); err == nil {
			t.Errorf("BlendedProb accepted s = %v", v)
		}
		if _, err := RequiredScenarioProb(-110, v, 0.3); err == nil {
			t.Errorf("RequiredScenarioProb accepted q = %v", v)
		}
		if _, err := ComputeHitRate([]float64{1, v, 3}, 2, Over, 0.95); err == nil {
			t.Errorf("ComputeHitRate accepted a game-log value of %v", v)
		}
		if _, err := ComputeHitRate([]float64{1, 2, 3}, 2, Over, v); err == nil {
			t.Errorf("ComputeHitRate accepted confidence %v", v)
		}
	}

	// Whatever a caller does, no price may come back that is not a real market
	// price. MinInt64 used to, via American(math.Ceil(NaN)).
	if err := American(math.MinInt64).validate(); err == nil {
		t.Error("MinInt64 is not a price; -a overflows to itself and Decimal() returned 1.0")
	}
	if err := American(maxAmerican + 1).validate(); err == nil {
		t.Error("an absurd price should be rejected")
	}
	if _, err := American(math.MinInt64).Decimal(); err == nil {
		t.Error("Decimal must reject an unrepresentable price rather than return 1.0")
	}
}

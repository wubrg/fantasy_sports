package wager

import "testing"

// TestMinPriceInvertsTheMatrix is the key correctness check: the threshold
// function must reproduce the source's Profitability Matrix from the other
// direction. If p=0.55 at -110 yields +5% EV, then asking for +5% EV at
// p=0.55 must return exactly -110.
func TestMinPriceInvertsTheMatrix(t *testing.T) {
	cases := []struct {
		p      float64
		target float64
		want   American
		note   string
	}{
		{0.55, 0.05, -110, "matrix: -110 @ 55% = +5.0%"},
		{0.60, 0.00, -150, "matrix: -150 @ 60% = breakeven"},
		{0.50, 0.10, 120, "matrix: +120 @ 50% = +10.0%"},
		{0.55, 0.00, -122, "breakeven price at 55% confidence"},
		{0.70, 0.00, -233, "breakeven price at 70% confidence"},
	}
	for _, c := range cases {
		got, err := MinPrice(RealMoney, c.p, c.target)
		if err != nil {
			t.Fatalf("p=%.2f target=%.2f: %v", c.p, c.target, err)
		}
		if got != c.want {
			t.Errorf("MinPrice(real, %.2f, %.2f) = %+d, want %+d (%s)",
				c.p, c.target, got, c.want, c.note)
		}
	}
}

// TestMinPriceRoundsConservatively is the property that makes these numbers
// safe to act on: the returned price must actually meet the target, and the
// next worse price must not. A threshold that rounds the wrong way would tell
// you to accept a -EV price.
func TestMinPriceRoundsConservatively(t *testing.T) {
	for _, p := range []float64{0.35, 0.42, 0.5, 0.53, 0.55, 0.61, 0.66, 0.7, 0.78, 0.9} {
		for _, target := range []float64{0, 0.02, 0.05, 0.1} {
			got, err := MinPrice(RealMoney, p, target)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := EVRealMoney(p, got, 1)
			if err != nil {
				t.Fatal(err)
			}
			if ev < target-1e-9 {
				t.Errorf("p=%.2f target=%.3f: returned %+d which only yields %.5f",
					p, target, got, ev)
			}

			// One step worse must fail, or the threshold is not minimal.
			// "Worse" means shorter odds, which is one lower in both signs:
			// -122 -> -123 for a favourite, +151 -> +150 for a dog.
			worse := got - 1
			if err := worse.validate(); err != nil {
				continue // stepped across the +-100 boundary
			}
			evWorse, err := EVRealMoney(p, worse, 1)
			if err != nil {
				continue
			}
			if evWorse >= target-1e-9 {
				t.Errorf("p=%.2f target=%.3f: returned %+d but %+d also clears (not minimal)",
					p, target, got, worse)
			}
		}
	}
}

// TestMinPriceForConversion pins the static bonus-bet card, and shows why the
// rounding rule matters: 70% conversion needs +234, not the +233 you get from
// rounding 233.33 to nearest.
func TestMinPriceForConversion(t *testing.T) {
	cases := []struct {
		target float64
		want   American
	}{
		{0.50, 100},
		{0.60, 150},
		{0.70, 234},
		{0.75, 300},
		{0.80, 400},
		{0.90, 900},
	}
	for _, c := range cases {
		got, err := MinPriceForConversion(c.target)
		if err != nil {
			t.Fatalf("target %.2f: %v", c.target, err)
		}
		if got != c.want {
			t.Errorf("MinPriceForConversion(%.2f) = %+d, want %+d", c.target, got, c.want)
		}
		// The returned price must actually deliver the target conversion.
		conv, err := got.ConversionCeiling()
		if err != nil {
			t.Fatal(err)
		}
		if conv < c.target-1e-9 {
			t.Errorf("target %.2f: %+d only converts %.4f", c.target, got, conv)
		}
	}

	// The framework's "+300 or higher" bonus-bet rule is not a heuristic --
	// it is exactly the 75% conversion threshold.
	got, err := MinPriceForConversion(0.75)
	if err != nil {
		t.Fatal(err)
	}
	if got != 300 {
		t.Errorf("75%% conversion should derive exactly +300, got %+d", got)
	}
}

// TestBonusMinPriceAgreesWithConversion checks the two bonus-bet entry points
// against each other. With no edge (p = implied), asking MinPrice for a
// conversion target must give the same answer as MinPriceForConversion.
func TestBonusMinPriceAgreesWithConversion(t *testing.T) {
	for _, target := range []float64{0.5, 0.6, 0.7, 0.75, 0.8} {
		viaConversion, err := MinPriceForConversion(target)
		if err != nil {
			t.Fatal(err)
		}
		p, err := viaConversion.ImpliedRaw()
		if err != nil {
			t.Fatal(err)
		}
		viaMinPrice, err := MinPrice(BonusBet, p, target)
		if err != nil {
			t.Fatal(err)
		}
		if viaMinPrice != viaConversion {
			t.Errorf("target %.2f: MinPriceForConversion says %+d, MinPrice(bonus) says %+d",
				target, viaConversion, viaMinPrice)
		}
	}
}

// TestBankrollsWantOppositePrices is the structural claim behind splitting the
// reference card in two: for the same belief, real money and a bonus bet do
// not agree about what price is acceptable.
func TestBankrollsWantOppositePrices(t *testing.T) {
	const p = 0.30

	// Real money at p=0.30 needs a substantial underdog price just to break
	// even.
	real, err := MinPrice(RealMoney, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if real <= 0 {
		t.Errorf("real money at p=0.30 should require plus money, got %+d", real)
	}

	// A bonus bet at the same probability is already profitable at any
	// price, because there is no downside term.
	ev, err := EV(BonusBet, p, real, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ev <= 0 {
		t.Error("a bonus bet must be positive EV at any valid price")
	}
	// -500 implies 83.3%, so p=0.80 is a genuine disadvantage: the same
	// wager is a loser in cash and a winner on a bonus bet. That divergence
	// is the whole reason the reference card splits in two.
	const shortPrice, belowImplied = American(-500), 0.80

	evBonus, err := EV(BonusBet, belowImplied, shortPrice, 1)
	if err != nil {
		t.Fatal(err)
	}
	if evBonus <= 0 {
		t.Error("a bonus bet is positive EV even on a heavy favourite you are fading")
	}

	evReal, err := EV(RealMoney, belowImplied, shortPrice, 1)
	if err != nil {
		t.Fatal(err)
	}
	if evReal >= 0 {
		t.Errorf("-500 at p=0.80 must be negative EV in cash, got %+.4f", evReal)
	}
}

// TestTrueConversionIsBelowCeiling pins the de-vigging correction for bonus
// bets: the naive ceiling overstates, and overstates most on longshots, which
// is exactly where the card points.
func TestTrueConversionIsBelowCeiling(t *testing.T) {
	// A +1000 longshot in a market whose other side is heavily juiced.
	m := Market{A: 1000, B: -1400}

	ceiling, err := m.A.ConversionCeiling()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := TrueConversion(m)
	if err != nil {
		t.Fatal(err)
	}
	if actual >= ceiling {
		t.Errorf("de-vigged conversion %.4f should sit below the ceiling %.4f", actual, ceiling)
	}

	// The gap must widen as the market gets more juiced.
	tight := Market{A: 1000, B: -1100}
	tightActual, err := TrueConversion(tight)
	if err != nil {
		t.Fatal(err)
	}
	if tightActual <= actual {
		t.Error("a tighter market must convert closer to the ceiling than a juiced one")
	}
}

// TestFanDuelPushRule pins the highest-cost operational rule in the corpus.
func TestFanDuelPushRule(t *testing.T) {
	if err := CheckBonusMarket(FanDuel, true); err == nil {
		t.Error("a FanDuel bonus bet on a pushable market must be rejected")
	}
	if err := CheckBonusMarket(FanDuel, false); err != nil {
		t.Errorf("a FanDuel bonus bet on a non-pushable market is fine: %v", err)
	}
	if err := CheckBonusMarket(DraftKings, true); err != nil {
		t.Errorf("DraftKings returns the bonus on a push, so this is allowed: %v", err)
	}
	if !FanDuel.BonusLostOnPush() {
		t.Error("FanDuel forfeits the bonus bet on a push")
	}
	if DraftKings.BonusLostOnPush() {
		t.Error("DraftKings returns the bonus bet on a push")
	}
	if !FanDuel.BonusSplittable() || DraftKings.BonusSplittable() {
		t.Error("FanDuel bonus bets split; DraftKings tokens must be used whole")
	}
}

// TestMinPriceRejectsNonsense keeps the fail-loudly contract.
func TestMinPriceRejectsNonsense(t *testing.T) {
	if _, err := MinPrice(RealMoney, 0, 0); err == nil {
		t.Error("p=0 has no profitable price")
	}
	if _, err := MinPrice(RealMoney, 1.5, 0); err == nil {
		t.Error("p>1 must be rejected")
	}
	if _, err := MinPrice(RealMoney, 0.5, -0.1); err == nil {
		t.Error("negative target must be rejected")
	}
	if _, err := MinPrice(Bankroll(99), 0.5, 0); err == nil {
		t.Error("unknown bankroll must be rejected")
	}
	for _, bad := range []float64{0, 1, -0.5, 1.5} {
		if _, err := MinPriceForConversion(bad); err == nil {
			t.Errorf("conversion target %v must be rejected", bad)
		}
	}
}

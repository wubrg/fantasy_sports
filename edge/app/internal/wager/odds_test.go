package wager

import (
	"math"
	"testing"
)

// tolerance for values the source documents quote to one decimal place
// (0.1 percentage points).
const tol = 0.0006

func closeTo(got, want, epsilon float64) bool {
	return math.Abs(got-want) <= epsilon
}

// TestImpliedRaw pins the "Breakeven Reference Table" (edge-of-vigor.md p.7)
// and the "Expanded Implied Probability Ruler" (p.10). These are the hurdle
// rates the whole framework is built on.
func TestImpliedRaw(t *testing.T) {
	cases := []struct {
		odds American
		want float64 // as quoted in the source, to 0.1%
		src  string
	}{
		{-500, 0.833, "p.10 ruler"},
		{-400, 0.800, "p.10 ruler"},
		{-300, 0.750, "p.7 breakeven"},
		{-250, 0.714, "p.7 breakeven"},
		{-200, 0.667, "p.7 breakeven"},
		{-175, 0.636, "p.7 breakeven"},
		{-150, 0.600, "p.7 breakeven"},
		{-130, 0.565, "p.7 breakeven"},
		{-110, 0.524, "p.7 breakeven — the standard price"},
		{100, 0.500, "p.7 breakeven"},
		{110, 0.476, "p.7 breakeven"},
		{130, 0.435, "p.7 breakeven"},
		{150, 0.400, "p.7 breakeven"},
		{200, 0.333, "p.7 breakeven"},
		{300, 0.250, "p.7 breakeven"},
		{500, 0.167, "p.10 ruler"},
		{1000, 0.091, "p.10 ruler"},
	}
	for _, c := range cases {
		got, err := c.odds.ImpliedRaw()
		if err != nil {
			t.Fatalf("%d: unexpected error: %v", c.odds, err)
		}
		if !closeTo(got, c.want, tol) {
			t.Errorf("%d implied = %.4f, source says %.3f (%s)", c.odds, got, c.want, c.src)
		}
		// Breakeven is the same number under the name that describes what it
		// is actually used for.
		be, err := c.odds.Breakeven()
		if err != nil || be != got {
			t.Errorf("%d: Breakeven and ImpliedRaw must agree", c.odds)
		}
	}
}

// TestProfitMultiple pins the "Win Multiplier (Profit)" column
// (edge-of-vigor.md p.10). Getting this column wrong for three rows is the
// single arithmetic error in that document's bonus-bet tables.
func TestProfitMultiple(t *testing.T) {
	cases := []struct {
		odds American
		want float64
	}{
		{-500, 0.20}, {-400, 0.25}, {-300, 0.33}, {-200, 0.50},
		{-150, 0.67}, {-110, 0.91}, {100, 1.00}, {150, 1.50},
		{200, 2.00}, {300, 3.00}, {500, 5.00}, {1000, 10.00},
	}
	for _, c := range cases {
		got, err := c.odds.ProfitMultiple()
		if err != nil {
			t.Fatalf("%d: unexpected error: %v", c.odds, err)
		}
		if !closeTo(got, c.want, 0.005) {
			t.Errorf("%d profit multiple = %.4f, source says %.2fx", c.odds, got, c.want)
		}
	}
}

// TestStandardMarketVig is the correction of edge-of-vigor.md's conflation of
// overround with hold. The document says "that extra 4.8% is the Vig" and uses
// one number for both quantities. They are different, and neither is 4.8%.
func TestStandardMarketVig(t *testing.T) {
	m := Market{A: -110, B: -110}

	over, err := m.Overround()
	if err != nil {
		t.Fatal(err)
	}
	// Two sides at 52.38% sum to 104.76%, so 4.76 points of overround.
	// The document rounds each side to 52.4% and reports 4.8.
	if !closeTo(over, 0.047619, 1e-6) {
		t.Errorf("overround = %.6f, want 0.047619", over)
	}

	hold, err := m.Hold()
	if err != nil {
		t.Fatal(err)
	}
	// Exactly 1/22. Note this is NOT 4.8%, and it is also not the 4.58% you
	// get by dividing the document's rounded 4.8 by its rounded 104.8.
	if !closeTo(hold, 1.0/22.0, 1e-9) {
		t.Errorf("hold = %.6f, want %.6f (1/22)", hold, 1.0/22.0)
	}
	if closeTo(hold, over, 1e-4) {
		t.Error("hold and overround must not be treated as the same number")
	}

	// The market's actual view of a -110/-110 line is a coin flip, not the
	// 52.4% each side is quoted at. This is the distinction the Final
	// Directive ("if True > Implied, you have found the Edge of Vigor")
	// silently skips.
	pa, pb, err := m.FairDevig()
	if err != nil {
		t.Fatal(err)
	}
	if !closeTo(pa, 0.5, 1e-9) || !closeTo(pb, 0.5, 1e-9) {
		t.Errorf("de-vigged = %.6f/%.6f, want 0.5/0.5", pa, pb)
	}
	if !closeTo(pa+pb, 1.0, 1e-9) {
		t.Errorf("de-vigged probabilities must sum to 1, got %.9f", pa+pb)
	}
}

// TestMarketWidthImpliesMoreJuice encodes the one uncontested half of the
// "Market Width" discussion: a wider market carries more vig. (The document's
// advice about what to DO with that is self-contradictory between Part 0 and
// Part II, so only the measurable claim is pinned here.)
func TestMarketWidthImpliesMoreJuice(t *testing.T) {
	tight := Market{A: -110, B: -110} // 20-cent market
	wide := Market{A: -135, B: 105}   // 40-cent market

	tightHold, err := tight.Hold()
	if err != nil {
		t.Fatal(err)
	}
	wideHold, err := wide.Hold()
	if err != nil {
		t.Fatal(err)
	}
	if wideHold <= tightHold {
		t.Errorf("wide market hold %.4f should exceed tight market hold %.4f", wideHold, tightHold)
	}
}

// TestInvalidPricesFail is the "fail loudly" contract. A price that cannot be
// interpreted must not silently become a zero probability.
func TestInvalidPricesFail(t *testing.T) {
	for _, bad := range []American{0, 1, 50, 99, -1, -50, -99} {
		if _, err := bad.ImpliedRaw(); err == nil {
			t.Errorf("American(%d) should be rejected as a price", bad)
		}
		if _, err := bad.Decimal(); err == nil {
			t.Errorf("American(%d).Decimal should be rejected", bad)
		}
	}
	// A market is only as valid as both its sides.
	if _, err := (Market{A: -110, B: 0}).Hold(); err == nil {
		t.Error("a market with an invalid side must fail, not return zero hold")
	}
	if _, _, err := (Market{A: 50, B: -110}).FairDevig(); err == nil {
		t.Error("a market with an invalid side must fail, not return zero probabilities")
	}
}

// TestDecimalRoundTrip guards the conversion itself.
func TestDecimalRoundTrip(t *testing.T) {
	cases := []struct {
		odds American
		want float64
	}{
		{-110, 1.909091}, {100, 2.0}, {150, 2.5}, {-200, 1.5}, {1000, 11.0},
	}
	for _, c := range cases {
		got, err := c.odds.Decimal()
		if err != nil {
			t.Fatal(err)
		}
		if !closeTo(got, c.want, 1e-5) {
			t.Errorf("%d decimal = %.6f, want %.6f", c.odds, got, c.want)
		}
		// implied is 1/decimal by definition
		p, _ := c.odds.ImpliedRaw()
		if !closeTo(p*got, 1.0, 1e-9) {
			t.Errorf("%d: implied × decimal should be 1", c.odds)
		}
	}
}

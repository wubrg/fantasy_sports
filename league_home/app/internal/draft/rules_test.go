package draft

import (
	"slices"
	"testing"
)

// TestPriceMatchesLeagueDocTable walks the worked example table in
// leagues/hit_or_miss/draft.md row for row. If this test ever fails, either
// the rules changed or the implementation drifted from them.
func TestPriceMatchesLeagueDocTable(t *testing.T) {
	r := DefaultRules()
	cases := []struct {
		name      string
		prior     int
		keepCount int
		want      int
		wantFlag  string
	}{
		{"first time under minimum, $1", 1, 1, 10, FlagFloored},
		{"first time under minimum, $7", 7, 1, 12, ""},
		{"first time, $10", 10, 1, 15, ""},
		{"second time, $15", 15, 2, 25, ""},
		{"third time, $25", 25, 3, 40, ""},
		{"fourth time, $40", 40, 4, 55, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, flags, err := r.Price(tc.prior, tc.keepCount)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Price(%d, %d) = %d, want %d", tc.prior, tc.keepCount, got, tc.want)
			}
			if tc.wantFlag != "" && !slices.Contains(flags, tc.wantFlag) {
				t.Errorf("expected flag %q, got %v", tc.wantFlag, flags)
			}
			if tc.wantFlag == "" && len(flags) != 0 {
				t.Errorf("expected no flags, got %v", flags)
			}
		})
	}
}

// TestPriceFreeAgentPickup covers draft.md's rule that undrafted players —
// free agent adds with no FAAB cost — are kept at the $10 minimum.
func TestPriceFreeAgentPickup(t *testing.T) {
	got, flags, err := DefaultRules().Price(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Errorf("free agent first keep = %d, want 10", got)
	}
	if !slices.Contains(flags, FlagFloored) {
		t.Errorf("expected %q flag, got %v", FlagFloored, flags)
	}
}

// TestPriceThreeYearChain follows one player through three consecutive
// keeps, feeding each year's price in as the next year's prior value. This
// is the path the ledger actually walks, and it is where an off-by-one in
// keep counting would show up.
func TestPriceThreeYearChain(t *testing.T) {
	r := DefaultRules()
	value := 10 // drafted for $10
	want := []int{15, 25, 40}
	for i, expected := range want {
		keepCount := i + 1
		got, _, err := r.Price(value, keepCount)
		if err != nil {
			t.Fatal(err)
		}
		if got != expected {
			t.Fatalf("keep #%d: got %d, want %d", keepCount, got, expected)
		}
		value = got
	}
}

// TestPriceFourthKeepUsesRuledIncrement covers the LM ruling that a fourth
// consecutive keep costs +$15 rather than the +$20 the ladder's own step
// would imply. Kenneth Walker is the only player who reaches it in 2026.
func TestPriceFourthKeepUsesRuledIncrement(t *testing.T) {
	got, flags, err := DefaultRules().Price(31, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != 31+FourthKeepIncrement {
		t.Errorf("4th keep of a $31 player = %d, want %d", got, 31+FourthKeepIncrement)
	}
	// The fourth rung is a decided rule now, so it must not be reported
	// as a guess.
	if slices.Contains(flags, FlagIncrementExtrapolated) {
		t.Errorf("the 4th keep is ruled, not extrapolated: %v", flags)
	}
}

// TestPriceBeyondDocumentedLadder guards the remaining rule gap: nothing
// defines a fifth consecutive keep. It must still produce a number, and
// must say that it guessed.
func TestPriceBeyondDocumentedLadder(t *testing.T) {
	got, flags, err := DefaultRules().Price(46, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(flags, FlagIncrementExtrapolated) {
		t.Errorf("expected %q flag for a 5th keep, got %v", FlagIncrementExtrapolated, flags)
	}
	// Continuing the ladder's own +$5 step past the ruled 4th rung.
	if got != 66 {
		t.Errorf("5th keep of a $46 player = %d, want 66", got)
	}
}

// TestWalker2026 pins the concrete outcome of the two LM rulings that
// interact: the 2025 price stands as charged at $31, and the fourth keep
// costs +$15 instead of +$20.
func TestWalker2026(t *testing.T) {
	got, _, err := DefaultRules().Price(31, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != 46 {
		t.Errorf("Walker's 2026 keeper price = %d, want 46", got)
	}
}

func TestPriceRejectsNonsenseKeepCount(t *testing.T) {
	for _, keepCount := range []int{0, -1} {
		if _, _, err := DefaultRules().Price(10, keepCount); err == nil {
			t.Errorf("expected error for keep count %d, got nil", keepCount)
		}
	}
}

func TestPriceTreatsNegativePriorAsZero(t *testing.T) {
	got, _, err := DefaultRules().Price(-5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Errorf("negative prior value = %d, want the $10 floor", got)
	}
}

// TestPriceIsFlooredNotClamped confirms the minimum only ever raises a
// price. A cheap keeper floors to $10, but an expensive one is untouched.
func TestPriceIsFlooredNotClamped(t *testing.T) {
	got, flags, err := DefaultRules().Price(78, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 83 {
		t.Errorf("Price(78, 1) = %d, want 83", got)
	}
	if slices.Contains(flags, FlagFloored) {
		t.Error("an $83 keeper should not be flagged as floored")
	}
}

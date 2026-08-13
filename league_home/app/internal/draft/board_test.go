package draft

import (
	"strings"
	"testing"
)

func boardFixture() []PlayerSignals {
	return []PlayerSignals{
		{Name: "Jahmyr Gibbs", Position: "RB", Value: 76, Cost: 72, MyMaxBid: 85,
			AAV: 68, ScarcityPct: 93, ECR: ECRConsensus,
			Lean: PlayerLean{Lean: LeanUp}, VBD: map[Baseline]float64{"beer": 73, "vols": 91}},
		{Name: "Justin Jefferson", Position: "WR", Value: 50, Cost: 47, MyMaxBid: 43,
			AAV: 47, ScarcityPct: 67, ECR: ECRContested, Lean: PlayerLean{Lean: LeanDown}},
		{Name: "George Kittle", Position: "TE", Value: 14, Cost: 4, MyMaxBid: 0,
			AAV: 3, ScarcityPct: 6, ECR: ECRDownside, Availability: "PUP",
			Lean: PlayerLean{Lean: LeanDND}, BidRule: RuleRefused},
		{Name: "Nobody Known", Position: "WR", Value: 3, MyMaxBid: 3},
	}
}

func TestWriteBoardShowsTheEssentials(t *testing.T) {
	var sb strings.Builder
	me := MyState{Budget: 120, OpenSlots: 8, StartersNeeded: map[string]int{"RB": 1, "TE": 1}}
	err := WriteBoard(&sb, boardFixture(), me, MustHaveCost{}, Pivot{}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"$120 left", "max bid $113", "1 RB, 1 TE", "Jahmyr Gibbs", "$76", "$85"} {
		if !strings.Contains(out, want) {
			t.Errorf("board missing %q:\n%s", want, out)
		}
	}
}

// TestWriteBoardNeverQuotesABidYouCannotAfford — showing $85 when you can
// only spend $40 invites exactly the mistake the board exists to prevent.
func TestWriteBoardNeverQuotesABidYouCannotAfford(t *testing.T) {
	var sb strings.Builder
	me := MyState{Budget: 45, OpenSlots: 6, StartersNeeded: map[string]int{"RB": 1}}
	if err := WriteBoard(&sb, boardFixture(), me, MustHaveCost{}, Pivot{}, false, 0); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if strings.Contains(out, "$85") {
		t.Errorf("quoted an unaffordable bid:\n%s", out)
	}
	if !strings.Contains(out, "$40!") {
		t.Errorf("expected the affordable ceiling flagged:\n%s", out)
	}
}

func TestWriteBoardRendersAvoidAsADash(t *testing.T) {
	var sb strings.Builder
	me := MyState{Budget: 120, OpenSlots: 8}
	if err := WriteBoard(&sb, boardFixture(), me, MustHaveCost{}, Pivot{}, false, 0); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(sb.String(), "\n") {
		if strings.Contains(line, "George Kittle") {
			if !strings.Contains(line, "—") || !strings.Contains(line, "DND") {
				t.Errorf("do-not-draft should show a dash and DND: %q", line)
			}
			if !strings.Contains(line, "pup") {
				t.Errorf("availability should surface: %q", line)
			}
		}
	}
}

func TestWriteBoardShowsPivotWhenPresent(t *testing.T) {
	var sb strings.Builder
	me := MyState{Budget: 120, OpenSlots: 8}
	p := Pivot{Name: "SCARCITY BREAK", Reason: "only 8% of TE value left"}
	if err := WriteBoard(&sb, boardFixture(), me, MustHaveCost{}, p, true, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "SCARCITY BREAK") {
		t.Errorf("pivot banner missing:\n%s", sb.String())
	}
}

func TestFlagsForOnlyShowsWhatMatters(t *testing.T) {
	// A player with nothing notable gets no flags at all.
	if got := flagsFor(PlayerSignals{Name: "Plain", ECR: ECRUnknown}); len(got) != 0 {
		t.Errorf("expected no flags, got %v", got)
	}
	// A small baseline spread is noise and must stay off the board.
	quiet := PlayerSignals{VBD: map[Baseline]float64{"beer": 20, "vols": 24}}
	for _, f := range flagsFor(quiet) {
		if strings.HasPrefix(f, "swing") {
			t.Errorf("a $4 spread should not be flagged: %v", f)
		}
	}
	loud := PlayerSignals{VBD: map[Baseline]float64{"beer": 73, "vols": 91}}
	found := false
	for _, f := range flagsFor(loud) {
		if f == "swing$18" {
			found = true
		}
	}
	if !found {
		t.Errorf("an $18 spread should be flagged, got %v", flagsFor(loud))
	}
}

func TestFlagsForSharpDivergence(t *testing.T) {
	has := func(p PlayerSignals, flag string) bool {
		for _, f := range flagsFor(p) {
			if f == flag {
				return true
			}
		}
		return false
	}
	if !has(PlayerSignals{SharpRankDelta: 6}, "sharp+") {
		t.Errorf("sharps ranking him higher should flag sharp+, got %v", flagsFor(PlayerSignals{SharpRankDelta: 6}))
	}
	if !has(PlayerSignals{SharpRankDelta: -7}, "sharp-") {
		t.Errorf("sharps ranking him lower should flag sharp-, got %v", flagsFor(PlayerSignals{SharpRankDelta: -7}))
	}
	// A move under the threshold is noise and must stay off the board.
	for _, f := range flagsFor(PlayerSignals{SharpRankDelta: 3}) {
		if strings.HasPrefix(f, "sharp") {
			t.Errorf("a 3-rank move should not flag: %v", f)
		}
	}
}

func TestWriteBoardLimit(t *testing.T) {
	var sb strings.Builder
	me := MyState{Budget: 120, OpenSlots: 8}
	if err := WriteBoard(&sb, boardFixture(), me, MustHaveCost{}, Pivot{}, false, 2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "George Kittle") {
		t.Error("limit not respected")
	}
}

func TestWriteScarcityAndGaps(t *testing.T) {
	var sb strings.Builder
	if err := WriteScarcity(&sb, map[string]PositionScarcity{
		"RB": {Position: "RB", Startable: 40, StartersLeft: 20, Cover: 2, TopScarcityPct: 85, Cliff: 22},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "RB") || !strings.Contains(sb.String(), "22 pts") {
		t.Errorf("scarcity table wrong:\n%s", sb.String())
	}
	if strings.Contains(sb.String(), "YOURS") {
		t.Errorf("no effective scarcity given, so the YOURS column should be hidden:\n%s", sb.String())
	}

	sb.Reset()
	if err := WriteGaps(&sb, Gaps(boardFixture(), "aav", 1), 5); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "George Kittle") {
		t.Errorf("expected Kittle's cost-vs-AAV gap:\n%s", sb.String())
	}
}

// TestWriteBoardStatesWhatMustHavesCost — wanting specific players is
// legitimate; not knowing what it costs is not.
func TestWriteBoardStatesWhatMustHavesCost(t *testing.T) {
	leans := Leans{}
	for _, pl := range []PlayerLean{
		{Player: "Ashton Jeanty", Lean: LeanMust, Cap: 48},
		{Player: "Kenneth Walker", Lean: LeanMust, Cap: 40},
	} {
		leans[normalizeName(pl.Player)] = pl
	}
	me := MyState{Budget: 130, OpenSlots: 12}
	bids := map[string]int{normalizeName("Ashton Jeanty"): 48, normalizeName("Kenneth Walker"): 40}

	var sb strings.Builder
	if err := WriteBoard(&sb, boardFixture(), me, leans.MustHaves(me.Budget, me.OpenSlots, bids), Pivot{}, false, 0); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"must-haves", "$88", "$42", "10 slots"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the budget line:\n%s", want, out)
		}
	}
}

func TestWriteBoardWarnsWhenMustHavesEatTheBudget(t *testing.T) {
	leans := Leans{}
	for i, cap := range []int{48, 40, 35} {
		p := PlayerLean{Player: string(rune('A' + i)), Lean: LeanMust, Cap: cap}
		leans[normalizeName(p.Player)] = p
	}
	me := MyState{Budget: 130, OpenSlots: 12}
	bids := map[string]int{normalizeName("A"): 48, normalizeName("B"): 40, normalizeName("C"): 35}

	var sb strings.Builder
	if err := WriteBoard(&sb, boardFixture(), me, leans.MustHaves(me.Budget, me.OpenSlots, bids), Pivot{}, false, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "too thin") {
		t.Errorf("expected an overcommitment warning:\n%s", sb.String())
	}
}

package draft

import "testing"

// leaguePool is a mid-draft room: money and slots left across all twelve
// teams.
func leaguePool(dollars, slots int, filled map[string]int) PoolState {
	p := HitOrMissPool()
	p.Dollars, p.Slots, p.Filled = dollars, slots, filled
	return p
}

func TestLeaguePerStarterReservesBenchDollars(t *testing.T) {
	// 12 teams x (1+2+3+1 starters + 1 flex) = 96 starting spots, and
	// 168 total slots, so 72 bench spots each holding back a dollar.
	p := leaguePool(2400, 168, nil)
	if got := p.StartingSlotsLeft(); got != 96 {
		t.Fatalf("starting slots = %d, want 96", got)
	}
	want := float64(2400-72) / 96
	if got := p.LeaguePerStarter(); got != want {
		t.Errorf("league per starter = %.2f, want %.2f", got, want)
	}
}

func TestStartingSlotsLeftDropsAsKeepersFill(t *testing.T) {
	full := leaguePool(2400, 168, nil).StartingSlotsLeft()
	after := leaguePool(1977, 151, map[string]int{"RB": 7, "WR": 6, "TE": 3, "QB": 1}).StartingSlotsLeft()
	if after >= full {
		t.Errorf("keepers should reduce starting demand: %d -> %d", full, after)
	}
}

// TestRiskOfMeasuresPositionNotSize — $113 is not risky because it is a big
// number; it is risky because of what it leaves.
func TestRiskOfMeasuresPositionNotSize(t *testing.T) {
	me := MyState{Budget: 130, OpenSlots: 12, StartersNeeded: map[string]int{"QB": 1, "RB": 1, "WR": 2, "TE": 1}}
	league := 20.0

	cheap := me.RiskOf(20, league)
	if cheap.Band != RiskComfortable {
		t.Errorf("a $20 bid should be comfortable, got %s (%.2f)", cheap.Band, cheap.Ratio)
	}
	broke := me.RiskOf(113, league)
	if broke.Band != RiskDangerous {
		t.Errorf("spending nearly everything should be dangerous, got %s (%.2f)", broke.Band, broke.Ratio)
	}
	// The same bid against a poorer room is safer, because the yardstick
	// is what everyone else can still spend.
	if me.RiskOf(60, 20).Ratio >= me.RiskOf(60, 8).Ratio {
		t.Error("a cheaper room should improve the same bid's standing")
	}
}

func TestRiskBandsOrderCorrectly(t *testing.T) {
	me := MyState{Budget: 200, OpenSlots: 10, StartersNeeded: map[string]int{"RB": 2, "WR": 3}}
	league := 20.0
	var prev float64 = 1e9
	for _, bid := range []int{10, 50, 100, 150, 190} {
		r := me.RiskOf(bid, league)
		if r.Ratio > prev {
			t.Errorf("bidding more should never improve the ratio: $%d gave %.2f after %.2f", bid, r.Ratio, prev)
		}
		prev = r.Ratio
	}
}

// TestMaxRecommendedBidHoldsTheFloor is the whole point: the ceiling is
// computed from what the rest of the roster still needs, not picked.
func TestMaxRecommendedBidHoldsTheFloor(t *testing.T) {
	me := MyState{Budget: 130, OpenSlots: 12, StartersNeeded: map[string]int{"QB": 1, "RB": 1, "WR": 2, "TE": 1}}
	league := 20.0

	got := me.MaxRecommendedBid(league, DefaultRiskFloor)
	if got <= 0 || got > me.MaxBid() {
		t.Fatalf("recommendation %d out of range (hard max %d)", got, me.MaxBid())
	}
	// At the recommendation you are inside the floor...
	if r := me.RiskOf(got, league); r.Ratio < DefaultRiskFloor {
		t.Errorf("recommended $%d sits at %.2f, below the %.2f floor", got, r.Ratio, DefaultRiskFloor)
	}
	// ...and one dollar more breaks it.
	if r := me.RiskOf(got+1, league); r.Ratio >= DefaultRiskFloor {
		t.Errorf("$%d should have been recommended instead of $%d", got+1, got)
	}
}

// TestMaxRecommendedBidRisesAsTheRoomSpends — the ceiling moves with the
// draft, which a hand-picked cap cannot do.
func TestMaxRecommendedBidRisesAsTheRoomSpends(t *testing.T) {
	me := MyState{Budget: 130, OpenSlots: 12, StartersNeeded: map[string]int{"RB": 2, "WR": 3}}
	rich := me.MaxRecommendedBid(25, DefaultRiskFloor)
	poor := me.MaxRecommendedBid(8, DefaultRiskFloor)
	if poor <= rich {
		t.Errorf("as the room runs out of money you can afford to pay more: rich=%d poor=%d", rich, poor)
	}
}

func TestMaxRecommendedBidNeverExceedsWhatYouCanPay(t *testing.T) {
	me := MyState{Budget: 20, OpenSlots: 9, StartersNeeded: map[string]int{"RB": 2, "WR": 3}}
	got := me.MaxRecommendedBid(30, DefaultRiskFloor)
	if got > me.MaxBid() {
		t.Errorf("recommendation %d exceeds the hard max %d", got, me.MaxBid())
	}
	if got < 1 {
		t.Errorf("recommendation should never drop below the auction floor, got %d", got)
	}
}

// TestLastSlotIgnoresTheYardstick — with only one spot left there is no
// future roster to protect, so the physical limit is the only constraint.
func TestLastSlotIgnoresTheYardstick(t *testing.T) {
	me := MyState{Budget: 40, OpenSlots: 1, StartersNeeded: map[string]int{"TE": 1}}
	if got := me.MaxRecommendedBid(25, DefaultRiskFloor); got != 40 {
		t.Errorf("recommendation = %d, want the full $40", got)
	}
}

func TestRiskDescribeIsReadable(t *testing.T) {
	me := MyState{Budget: 130, OpenSlots: 12, StartersNeeded: map[string]int{"RB": 2, "WR": 3}}
	got := me.RiskOf(48, 20).Describe()
	if got == "" {
		t.Fatal("expected a description")
	}
	broke := MyState{Budget: 5, OpenSlots: 9, StartersNeeded: map[string]int{"RB": 2}}
	if r := broke.RiskOf(5, 20); r.Affordable() {
		t.Errorf("spending the last $5 with 8 slots left is not affordable: %+v", r)
	}
}

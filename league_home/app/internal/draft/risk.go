package draft

import "fmt"

// RiskBand describes how far a bid would push you below the rest of the
// room on the roster spots you still have to fill.
//
// The measure is deliberately relative. Spending $113 of $130 on one player
// is not risky because $113 is a big number — it is risky because it leaves
// you buying starters at a dollar while everyone else buys them at fifteen.
// An auction is positional, so the only meaningful yardstick is what your
// remaining money buys compared with what everyone else's does.
type RiskBand string

const (
	RiskComfortable RiskBand = "comfortable"
	RiskStretched   RiskBand = "stretched"
	RiskRisky       RiskBand = "risky"
	RiskDangerous   RiskBand = "dangerous"
)

// Risk band thresholds, as a ratio of your per-starter budget to the
// league's. At 1.0 you can buy the same quality of starter as the average
// team; at 0.5 you are shopping at half their price.
const (
	stretchedBelow = 1.00
	riskyBelow     = 0.75
	dangerousBelow = 0.50
	// DefaultRiskFloor is the band edge a recommended maximum aims to hold.
	// Stretched is a deliberate choice: paying up for a player you want
	// should cost you something, and refusing to dip below comfortable
	// would make must-haves impossible at the top of the board.
	DefaultRiskFloor = riskyBelow
)

// BidRisk is what a given bid does to your ability to fill the rest of the
// roster.
type BidRisk struct {
	Bid int
	// PerStarter is what each unfilled starting spot could cost after this
	// bid, once a dollar is set aside for every bench spot.
	PerStarter float64
	// LeaguePerStarter is the same figure for the room as a whole.
	LeaguePerStarter float64
	// Ratio is PerStarter / LeaguePerStarter. Below 1 means your remaining
	// starters will be cheaper than everyone else's.
	Ratio float64
	Band  RiskBand
}

// Affordable reports whether the bid leaves enough to fill every roster
// spot at the auction floor. This is a hard constraint, unlike the bands.
func (r BidRisk) Affordable() bool { return r.PerStarter >= 0 }

// bandFor classifies a ratio.
func bandFor(ratio float64) RiskBand {
	switch {
	case ratio >= stretchedBelow:
		return RiskComfortable
	case ratio >= riskyBelow:
		return RiskStretched
	case ratio >= dangerousBelow:
		return RiskRisky
	}
	return RiskDangerous
}

// StartingSlotsLeft is how many starting spots the league still has to
// fill, across every team.
func (p PoolState) StartingSlotsLeft() int {
	total := 0
	for pos, perTeam := range p.Starters {
		n := perTeam*p.Teams - p.Filled[pos]
		if n > 0 {
			total += n
		}
	}
	total += p.FlexSlots * p.Teams
	if total > p.Slots {
		total = p.Slots
	}
	return total
}

// LeaguePerStarter is what the room can spend on each remaining starting
// spot, after reserving a dollar for every bench spot.
//
// This is the yardstick every bid is measured against, and it moves as the
// draft moves: if the room overspends early, this falls and your own
// position improves without you doing anything.
func (p PoolState) LeaguePerStarter() float64 {
	starting := p.StartingSlotsLeft()
	if starting <= 0 {
		return 0
	}
	bench := p.Slots - starting
	if bench < 0 {
		bench = 0
	}
	available := float64(p.Dollars - bench)
	if available < 0 {
		available = 0
	}
	return available / float64(starting)
}

// startersNeeded counts your unfilled starting spots.
func (m MyState) startersNeeded() int {
	n := 0
	for _, v := range m.StartersNeeded {
		if v > 0 {
			n += v
		}
	}
	return n
}

// RiskOf reports what spending bid dollars would do to your position.
func (m MyState) RiskOf(bid int, leaguePerStarter float64) BidRisk {
	r := BidRisk{Bid: bid, LeaguePerStarter: leaguePerStarter}

	starters := m.startersNeeded()
	// Spending on this player uses one slot; the rest still need filling.
	remainingSlots := m.OpenSlots - 1
	if remainingSlots < 0 {
		remainingSlots = 0
	}
	// The player being bought may himself fill a starting spot, so the
	// starters left afterwards is one fewer when one is needed.
	remainingStarters := starters
	if remainingStarters > 0 {
		remainingStarters--
	}
	bench := remainingSlots - remainingStarters
	if bench < 0 {
		bench = 0
	}

	left := float64(m.Budget-bid) - float64(bench)
	if remainingStarters <= 0 {
		// Nothing left to start; the only question is whether the bid is
		// payable at all.
		r.PerStarter = float64(m.Budget-bid) - float64(remainingSlots)
		r.Ratio = 1
		r.Band = RiskComfortable
		if r.PerStarter < 0 {
			r.Band = RiskDangerous
		}
		return r
	}

	r.PerStarter = left / float64(remainingStarters)
	if leaguePerStarter > 0 {
		r.Ratio = r.PerStarter / leaguePerStarter
	} else {
		r.Ratio = 1
	}
	r.Band = bandFor(r.Ratio)
	return r
}

// MaxRecommendedBid is the most you can pay for one player while keeping
// your remaining starting spots at or above floor times the league's
// per-starter budget.
//
// This replaces a hand-picked ceiling. Choosing a number yourself is not
// pragmatism — it is a guess with a decimal point. This one moves with the
// draft: as the room spends, the yardstick drops and the recommendation
// rises on its own.
func (m MyState) MaxRecommendedBid(leaguePerStarter, floor float64) int {
	hard := m.MaxBid()
	if hard <= 0 {
		return 0
	}
	if leaguePerStarter <= 0 || m.startersNeeded() <= 1 {
		// No starters left to protect, so the physical limit is the only
		// one that applies.
		return hard
	}
	// Walk down from the hard limit to the first bid inside the floor.
	// Bounded by hard, which is at most a team budget, so this is cheap
	// and avoids sign and rounding traps in the closed form.
	for bid := hard; bid >= 1; bid-- {
		if m.RiskOf(bid, leaguePerStarter).Ratio >= floor {
			return bid
		}
	}
	return 1
}

// Describe renders a risk band as a short phrase for the board.
func (r BidRisk) Describe() string {
	if !r.Affordable() {
		return "cannot afford the rest of the roster"
	}
	return fmt.Sprintf("%s — $%.0f per remaining starter vs the league's $%.0f (%.0f%%)",
		r.Band, r.PerStarter, r.LeaguePerStarter, r.Ratio*100)
}

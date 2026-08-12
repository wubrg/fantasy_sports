package draft

import "sort"

// RankTiers are the positional finishes the price lines are read at.
//
// Four of them, and no more. The point is to answer "is this top-five money"
// in the second before you bid, and a table of twenty ranks answers nothing
// at that speed.
var RankTiers = []int{3, 5, 8, 12}

// PositionPriceLine is what a dollar figure buys at one position.
//
// A statement about price, not about players. Paying the top-five line means
// you are spending what top-five players cost — measured against this
// league's own drafts, price rank and end-of-season finish rank correlate at
// about +0.46, which is real and nowhere near a promise. Five of these lines
// were tested against the seasons that followed and the top-five one landed
// 28 times in 60.
type PositionPriceLine struct {
	Position string `json:"position"`
	// Tiers are the ranks each figure belongs to, mirroring RankTiers.
	Tiers []int `json:"tiers"`
	// Live is the dollar standing at each tier right now, zero where the
	// position holds fewer players than the tier is deep.
	Live []int `json:"live"`
	// History is the same read from past drafts, nil when unknown.
	History []int `json:"history,omitempty"`
}

// PriceLines reads the current dollar standing at each rank tier.
//
// The curve covers the whole position, not the part still for sale. A back
// already gone contributes the price he actually went for, because that is
// what a back of his standing cost in this room today — pricing him at the
// board's guess would leave the top-five line describing a market that has
// moved on. Once four backs go the line moves with them, which is the whole
// reason to compute this live rather than read it off a table.
//
// gone maps a position to the prices paid for players who have left the
// board. history is optional and passes straight through.
func PriceLines(remaining []PlayerSignals, gone map[string][]int, history map[string][]int) map[string]PositionPriceLine {
	byPos := map[string][]int{}
	for _, p := range remaining {
		if p.Cost > 0 {
			byPos[p.Position] = append(byPos[p.Position], p.Cost)
		}
	}
	for pos, prices := range gone {
		for _, price := range prices {
			if price > 0 {
				byPos[pos] = append(byPos[pos], price)
			}
		}
	}

	out := make(map[string]PositionPriceLine, len(byPos))
	for pos, prices := range byPos {
		sort.Sort(sort.Reverse(sort.IntSlice(prices)))
		line := PositionPriceLine{
			Position: pos,
			Tiers:    RankTiers,
			Live:     atTiers(prices),
		}
		if h, ok := history[pos]; ok {
			line.History = h
		}
		out[pos] = line
	}
	return out
}

// atTiers reads a descending price list at each rank tier.
//
// A tier deeper than the position reports zero rather than the cheapest
// player available. "The top-twelve line is $1" would be true of a position
// holding four players and would say nothing except that the question did
// not apply.
func atTiers(sorted []int) []int {
	out := make([]int, len(RankTiers))
	for i, rank := range RankTiers {
		if rank <= len(sorted) {
			out[i] = sorted[rank-1]
		}
	}
	return out
}

// HistoricalPriceLines reads the same tiers from completed drafts, taking
// the median across seasons.
//
// Median rather than mean because one season where the room went mad at a
// position should not drag the reference line it is compared against.
//
// seasons whose median team spend falls short of minSpend are skipped: 2022
// settled at $157 of a $200 budget while the league was still working out
// how keeper money came off the auction, and prices from a draft where a
// third of the money never reached the table cannot be compared with prices
// from one where it did.
func HistoricalPriceLines(seasons []SeasonData, positionOf func(playerID string) string, minSpend int) map[string][]int {
	perSeason := map[string][][]int{}

	for _, s := range seasons {
		// The inaugural draft is not comparable either. 2021 ran with one
		// keeper against twenty-odd since, so every elite player was in the
		// pool and the top of each ladder priced accordingly. It passes the
		// spend guard comfortably and was quietly sitting in the reference
		// line while the correlation beside it covered 2023-2025 only.
		if keptShare(s) < minKeeperShare {
			continue
		}
		byPos := map[string][]int{}
		total := 0
		teams := map[string]int{}
		for _, p := range s.Picks {
			price := p.Metadata.Dollars()
			if price <= 0 {
				continue
			}
			total += price
			teams[p.PickedBy] += price
			if pos := positionOf(p.PlayerID); pos != "" {
				byPos[pos] = append(byPos[pos], price)
			}
		}
		if medianSpendOf(teams) < minSpend {
			continue
		}
		for pos, prices := range byPos {
			sort.Sort(sort.Reverse(sort.IntSlice(prices)))
			perSeason[pos] = append(perSeason[pos], atTiers(prices))
		}
	}

	out := make(map[string][]int, len(perSeason))
	for pos, seasonLines := range perSeason {
		line := make([]int, len(RankTiers))
		for i := range RankTiers {
			var vals []float64
			for _, sl := range seasonLines {
				if sl[i] > 0 {
					vals = append(vals, float64(sl[i]))
				}
			}
			if len(vals) > 0 {
				line[i] = int(median(vals) + 0.5)
			}
		}
		out[pos] = line
	}
	return out
}

// minKeeperShare separates the keeper era from the inaugural draft.
//
// A share rather than a count, so it does not quietly depend on how many
// teams the league has. One keeper among 2021's picks is well under a
// percent; every season since runs above ten.
const minKeeperShare = 0.05

// keptShare is the fraction of a season's picks that were keepers.
func keptShare(s SeasonData) float64 {
	if len(s.Picks) == 0 {
		return 0
	}
	n := 0
	for _, p := range s.Picks {
		if p.IsKeeper {
			n++
		}
	}
	return float64(n) / float64(len(s.Picks))
}

// medianSpendOf is the median of what each team laid out.
func medianSpendOf(teams map[string]int) int {
	if len(teams) == 0 {
		return 0
	}
	vals := make([]float64, 0, len(teams))
	for _, v := range teams {
		vals = append(vals, float64(v))
	}
	return int(median(vals))
}

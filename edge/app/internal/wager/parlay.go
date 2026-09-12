package wager

import (
	"fmt"
	"sort"
)

// A parlay multiplies INDEPENDENT legs: the combined decimal is the product of
// the legs' decimals, and (treating them as independent) the combined win
// probability is the product of the legs'. That independence is the whole
// contract of this file. It holds ONLY across different games — two legs in the
// same game share a game state and are correlated, so multiplying them
// overstates the true joint probability. Same-game legs are an SGP and must be
// priced at the book; the CLI enforces the split with per-leg game tags, since
// this layer sees only prices.
//
// maxParlayLegs bounds the round-robin machinery: the outcome distribution
// enumerates all 2^L win/lose combinations of the legs, so L is kept small. Ten
// legs is 1024 outcomes — plenty for any real round robin and still instant.
const maxParlayLegs = 10

// Combo is one parlay within a round robin: the legs it comprises (by index into
// the leg list) and its combined price.
type Combo struct {
	Legs    []int
	Decimal float64
	Price   American
	Implied float64 // product of the legs' raw implied — carries each leg's vig
	Stake   float64 // assigned when a stake is distributed
	EV      float64 // per-combo EV, set only when beliefs supplied
	WinProb float64 // product of supplied true probs, set only when beliefs supplied
}

// CombineParlay multiplies a set of legs into one parlay price. It is the
// single-combo case and the building block for round robins.
func CombineParlay(legs []American) (Combo, error) {
	if len(legs) < 2 {
		return Combo{}, fmt.Errorf("wager: a parlay needs at least 2 legs, got %d", len(legs))
	}
	idx := make([]int, len(legs))
	for i := range legs {
		idx[i] = i
	}
	return combineIdx(legs, idx)
}

func combineIdx(legs []American, idx []int) (Combo, error) {
	dec := 1.0
	implied := 1.0
	for _, i := range idx {
		d, err := legs[i].Decimal()
		if err != nil {
			return Combo{}, fmt.Errorf("leg %d: %w", i+1, err)
		}
		r, _ := legs[i].ImpliedRaw()
		dec *= d
		implied *= r
	}
	price, err := AmericanFromDecimal(dec)
	if err != nil {
		return Combo{}, err
	}
	c := Combo{Legs: append([]int(nil), idx...), Decimal: dec, Price: price, Implied: implied}
	return c, nil
}

// combinations returns all k-length index combinations of 0..n-1.
func combinations(n, k int) [][]int {
	var out [][]int
	if k <= 0 || k > n {
		return out
	}
	idx := make([]int, k)
	for i := range idx {
		idx[i] = i
	}
	for {
		out = append(out, append([]int(nil), idx...))
		i := k - 1
		for i >= 0 && idx[i] == n-k+i {
			i--
		}
		if i < 0 {
			break
		}
		idx[i]++
		for j := i + 1; j < k; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
	return out
}

// RoundRobin builds every combo of each requested size from the legs. Sizes are
// de-duplicated and sorted; a size of len(legs) is the straight parlay of all
// legs. Independence across legs is the caller's contract (see file comment).
func RoundRobin(legs []American, sizes []int) ([]Combo, error) {
	if len(legs) < 2 {
		return nil, fmt.Errorf("wager: a round robin needs at least 2 legs, got %d", len(legs))
	}
	if len(legs) > maxParlayLegs {
		return nil, fmt.Errorf("wager: %d legs exceeds the max of %d (the outcome distribution is 2^legs)", len(legs), maxParlayLegs)
	}
	seen := map[int]bool{}
	var uniq []int
	for _, s := range sizes {
		if s < 2 || s > len(legs) {
			return nil, fmt.Errorf("wager: combo size %d out of range 2..%d", s, len(legs))
		}
		if !seen[s] {
			seen[s] = true
			uniq = append(uniq, s)
		}
	}
	sort.Ints(uniq)
	var out []Combo
	for _, k := range uniq {
		for _, idx := range combinations(len(legs), k) {
			c, err := combineIdx(legs, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// Ticket distributes a stake across combos and, when per-leg probabilities are
// supplied, computes the exact profit distribution and EV by enumerating all
// 2^L leg outcomes. perCombo stakes each combo equally; the caller may instead
// set Combo.Stake beforehand and pass perCombo<=0 to keep them.
type Ticket struct {
	Combos    []Combo
	TotalRisk float64

	HasEV   bool
	EV      float64
	PProfit float64 // P(net > 0)
	PAny    float64 // P(at least one combo cashes)
	// Dist is the profit distribution keyed by number of legs that won.
	Dist map[int]struct {
		Prob   float64
		NetSum float64 // probability-weighted net contribution
	}
}

// BuildTicket stakes each combo equally from total and, if probs is non-nil
// (one true probability per leg), computes the exact EV and profit distribution
// under independence.
func BuildTicket(legs []American, combos []Combo, total float64, probs []float64) (*Ticket, error) {
	if err := validStake(total); err != nil {
		return nil, err
	}
	if len(combos) == 0 {
		return nil, fmt.Errorf("wager: no combos to stake")
	}
	per := total / float64(len(combos))
	cs := make([]Combo, len(combos))
	copy(cs, combos)
	for i := range cs {
		cs[i].Stake = per
	}
	t := &Ticket{Combos: cs, TotalRisk: total}

	if probs == nil {
		return t, nil
	}
	if len(probs) != len(legs) {
		return nil, fmt.Errorf("wager: got %d probabilities for %d legs", len(probs), len(legs))
	}
	for i, p := range probs {
		if err := validProb(p); err != nil {
			return nil, fmt.Errorf("leg %d: %w", i+1, err)
		}
	}
	// Per-combo EV and win prob (independence: product of leg probs).
	for i := range cs {
		wp := 1.0
		for _, li := range cs[i].Legs {
			wp *= probs[li]
		}
		cs[i].WinProb = wp
		cs[i].EV = cs[i].Stake * (wp*cs[i].Decimal - 1)
	}
	// Exact distribution by enumerating all 2^L leg outcomes.
	L := len(legs)
	dist := map[int]struct {
		Prob   float64
		NetSum float64
	}{}
	var ev, pProfit, pAny float64
	for mask := 0; mask < (1 << L); mask++ {
		prob := 1.0
		wins := 0
		for i := 0; i < L; i++ {
			if mask&(1<<i) != 0 {
				prob *= probs[i]
				wins++
			} else {
				prob *= 1 - probs[i]
			}
		}
		if prob == 0 {
			continue
		}
		var ret float64
		anyCash := false
		for i := range cs {
			won := true
			for _, li := range cs[i].Legs {
				if mask&(1<<li) == 0 {
					won = false
					break
				}
			}
			if won {
				ret += cs[i].Stake * cs[i].Decimal
				anyCash = true
			}
		}
		net := ret - total
		ev += prob * net
		if net > 1e-9 {
			pProfit += prob
		}
		if anyCash {
			pAny += prob
		}
		d := dist[wins]
		d.Prob += prob
		d.NetSum += prob * net
		dist[wins] = d
	}
	t.HasEV, t.EV, t.PProfit, t.PAny, t.Dist = true, ev, pProfit, pAny, dist
	return t, nil
}

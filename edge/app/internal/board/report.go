package board

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"edge/internal/wager"
)

// This file is the analysis the board exists for: de-vig every two-way
// moneyline, rank the dogs by what a bonus bet actually converts, pair them
// into disjoint parlays, and show where one book is out of line with the rest.
//
// Every number here was previously produced by pasting a board into a chat and
// asking a model to do the arithmetic. The arithmetic itself was never the
// hard part -- the bookkeeping was: which team is already used, which price is
// a transcription error, which of two dogs converts better despite the shorter
// price. All of that is mechanical, so it lives here rather than in a prompt.

// Overround bounds outside which a two-way price is treated as a probable
// transcription error rather than a real market.
//
// A real book prices a two-way market somewhere between about 2% and 10% of
// overround; 4-5% is typical. Both bounds catch a specific failure:
//
//   - Below 0.5%: the two sides were not read off the same board. A board once
//     listed "SF +150 / LAR -150" -- exactly 0.00% overround, which is not a
//     price any book posts, because it is the book working for free. The true
//     home price was -180. De-vigging that pair silently returns a fair 50/50
//     and every downstream conversion is wrong by four points.
//   - Above 15%: a digit was dropped or a sign was lost.
//
// The bounds are deliberately wide. This flags what cannot be real, not what
// is merely unusual; a flagged game is still analysed, because the operator
// may know the price is right.
const (
	MinPlausibleOverround = 0.005
	MaxPlausibleOverround = 0.15
)

// DefaultTarget is the bonus-bet conversion floor from `edgectl card bonus`.
// At 0.70 the minimum acceptable price is +234.
const DefaultTarget = 0.70

// Side is one team's moneyline in one game, de-vigged: the unit a bonus bet or
// a parlay leg is placed on.
type Side struct {
	GameID string
	Team   string
	Home   bool
	// Market is "ml", "spread" or "total". A leg has to say which it is: a
	// parlay of NE and a spread is not a parlay of NE twice, and the ticket
	// has to be readable back at the book.
	Market string
	// Label is what to write on the ticket -- "ATL +3.5" or "over 44.5".
	// Empty for a moneyline, where the team name is the whole selection.
	Label string
	Price wager.American
	// Fair is the de-vigged probability this side wins -- the market's actual
	// estimate. It is NOT Price.ImpliedRaw(), which is inflated by the vig.
	Fair float64
	// Conversion is the share of a bonus bet's face value this price returns,
	// using Fair rather than the raw implied probability. See
	// wager.TrueConversion for why the difference matters most on longshots.
	Conversion float64
}

// Clears reports whether this side meets a bonus-bet conversion floor.
func (s Side) Clears(target float64) bool { return s.Conversion >= target }

// GameLine is one game's two-way moneyline at one book, fully de-vigged.
type GameLine struct {
	GameID  string
	Kickoff string
	Book    string
	Market  wager.Market // A is the away side, B is the home side
	Away    Side
	Home    Side
	// Overround is how far the two raw implied probabilities exceed 1.
	Overround float64
	// Suspect is set when Overround falls outside the plausible band. The line
	// is still fully populated: the flag is advice to re-check the board, not
	// a refusal to compute.
	Suspect bool
	Why     string
}

// Dog returns the side with the longer price -- the one worth a bonus bet.
//
// Chosen by decimal odds rather than by sign, because both sides of a close
// game can be negative (-118/-102) and the dog is then the less negative one.
func (l GameLine) Dog() Side {
	if l.Away.Fair <= l.Home.Fair {
		return l.Away
	}
	return l.Home
}

// Fav returns the shorter-priced side.
func (l GameLine) Fav() Side {
	if l.Away.Fair <= l.Home.Fair {
		return l.Home
	}
	return l.Away
}

// Devig reads one book's moneyline for one game and removes the vig.
//
// ok is false for an empty cell. That is the normal state of almost every cell
// on the board and must never become a zero-priced wager: a Side with a zero
// price would de-vig into a confident nonsense number rather than being
// skipped.
func Devig(gameID string, g *Game, book string) (GameLine, bool, error) {
	if g == nil {
		return GameLine{}, false, fmt.Errorf("%s: no such game", gameID)
	}
	m, ok, err := ParseMarket(g.Books[book].ML)
	if err != nil {
		return GameLine{}, false, fmt.Errorf("%s %s ml: %w", gameID, book, err)
	}
	if !ok {
		return GameLine{}, false, nil
	}

	over, err := m.Overround()
	if err != nil {
		return GameLine{}, false, fmt.Errorf("%s %s ml: %w", gameID, book, err)
	}
	fairAway, fairHome, err := m.FairDevig()
	if err != nil {
		return GameLine{}, false, fmt.Errorf("%s %s ml: %w", gameID, book, err)
	}

	away, err := makeSide(gameID, g.Away, false, m.A, fairAway)
	if err != nil {
		return GameLine{}, false, err
	}
	home, err := makeSide(gameID, g.Home, true, m.B, fairHome)
	if err != nil {
		return GameLine{}, false, err
	}

	l := GameLine{
		GameID:    gameID,
		Kickoff:   g.Kickoff,
		Book:      book,
		Market:    m,
		Away:      away,
		Home:      home,
		Overround: over,
	}
	switch {
	case over < MinPlausibleOverround:
		l.Suspect = true
		l.Why = fmt.Sprintf("%.2f%% overround — no book posts a market this thin; "+
			"the two sides were probably not read off one board", over*100)
	case over > MaxPlausibleOverround:
		l.Suspect = true
		l.Why = fmt.Sprintf("%.2f%% overround — far above any real two-way market; "+
			"check for a dropped digit or a lost sign", over*100)
	}
	return l, true, nil
}

// makeSide computes one side's conversion. The conversion is taken from the
// de-vigged probability, so it is the value that actually shows up rather than
// the fair-odds ceiling the bonus card prints.
func makeSide(gameID, team string, home bool, price wager.American, fair float64) (Side, error) {
	profit, err := price.ProfitMultiple()
	if err != nil {
		return Side{}, fmt.Errorf("%s %s: %w", gameID, team, err)
	}
	return Side{
		GameID:     gameID,
		Team:       team,
		Home:       home,
		Market:     "ml",
		Price:      price,
		Fair:       fair,
		Conversion: fair * profit,
	}, nil
}

// Parlay is a straight multi-leg wager built from de-vigged legs.
type Parlay struct {
	Legs []Side
	// Decimal is the product of the legs' decimal odds: the exact total return
	// per unit staked. Price is this value rounded to a quotable American
	// number, and is what the ticket will read.
	Decimal float64
	Price   wager.American
	// TrueProb is the product of the legs' DE-VIGGED probabilities.
	//
	// It is emphatically not the de-vigged implied probability of Price. A
	// parlay price compounds every leg's vig, so de-vigging the parlay as if
	// it were a two-way market bakes that compounded vig back in and
	// overstates the wager's accuracy by roughly a point. The vig must come
	// off each leg first, and only then be multiplied.
	TrueProb float64
	// Conversion is TrueProb times the exact decimal profit, not the profit of
	// the rounded Price: rounding is a display concern and must not move the
	// arithmetic.
	Conversion float64
}

// Teams lists the legs' teams in order, for display and for the disjointness
// check that matters most to a human reading the ticket.
func (p Parlay) Teams() []string {
	out := make([]string, 0, len(p.Legs))
	for _, l := range p.Legs {
		out = append(out, l.Team)
	}
	return out
}

// MakeParlay prices a straight parlay from legs that have already been
// de-vigged.
//
// It rejects a repeated team or a repeated game outright rather than pricing
// them. Two legs from one game are correlated -- often perfectly, as with both
// sides of the same moneyline -- and multiplying their probabilities would
// produce a number that is not just imprecise but structurally wrong.
func MakeParlay(legs ...Side) (Parlay, error) {
	if len(legs) < 2 {
		return Parlay{}, fmt.Errorf("a parlay needs at least 2 legs, got %d", len(legs))
	}
	seenTeam := make(map[string]bool, len(legs))
	seenGame := make(map[string]bool, len(legs))
	dec, prob := 1.0, 1.0
	for _, l := range legs {
		if seenTeam[l.Team] {
			return Parlay{}, fmt.Errorf("%s appears twice in one parlay", l.Team)
		}
		if seenGame[l.GameID] {
			return Parlay{}, fmt.Errorf("two legs from %s in one parlay", l.GameID)
		}
		seenTeam[l.Team], seenGame[l.GameID] = true, true

		d, err := l.Price.Decimal()
		if err != nil {
			return Parlay{}, fmt.Errorf("%s: %w", l.Team, err)
		}
		if l.Fair <= 0 || l.Fair >= 1 {
			return Parlay{}, fmt.Errorf("%s: de-vigged probability %v is not usable", l.Team, l.Fair)
		}
		dec *= d
		prob *= l.Fair
	}
	price, err := parlayPrice(dec)
	if err != nil {
		return Parlay{}, err
	}
	return Parlay{
		Legs:       append([]Side(nil), legs...),
		Decimal:    dec,
		Price:      price,
		TrueProb:   prob,
		Conversion: prob * (dec - 1),
	}, nil
}

// parlayPrice converts a parlay's exact decimal odds to a quotable American
// price.
//
// The exact value is almost always fractional (2.45 x 2.75 = 6.7375, or
// +573.75) and a book quotes integers, so it has to be rounded. The rule is
// FLOOR, not round-to-nearest: always toward the price that is worse for the
// bettor. On the American scale "down" is unfavourable on both sides of zero
// (+574 -> +573 pays less, -120 -> -121 risks more), so one rule
// covers both. A quoted price you cannot actually get is the worse error, and
// only the display is affected: Parlay.Conversion uses the exact decimal.
//
// This deliberately differs from wager's internal threshold conversion, which
// rounds the other way. That function answers "what is the worst price I would
// still accept", where rounding must never hand back a price that misses the
// target. This one reports a price that already exists.
// Two real Fanatics prices settle the rule empirically. IND +145 x NE +175 is
// 2.45 x 2.75 = 6.7375 (+573.75) and was quoted +573; SF +150 x MIA +175 is
// 2.50 x 2.75 = 6.875 (+587.50) and was quoted +587. Floor reproduces both;
// round-to-nearest reproduces neither.
func parlayPrice(d float64) (wager.American, error) {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return 0, fmt.Errorf("board: parlay decimal odds %v is not a real number", d)
	}
	if d <= 1 {
		return 0, fmt.Errorf("board: parlay decimal odds %v imply no profit", d)
	}
	var exact float64
	if d >= 2 {
		exact = (d - 1) * 100
	} else {
		exact = -100 / (d - 1)
	}
	// Snap to an exact integer first: a product like 2.5 x 2.2 lands on
	// +449.99999999999994 in binary floating point, and rounding that as-is
	// quotes +450 as +449.
	if nearest := math.Round(exact); math.Abs(exact-nearest) < 1e-9*math.Max(1, math.Abs(exact)) {
		exact = nearest
	}
	a := wager.American(math.Floor(exact))
	if _, err := a.Decimal(); err != nil {
		return 0, fmt.Errorf("board: parlay price is not quotable: %w", err)
	}
	return a, nil
}

// ParlaySet is a group of parlays that share no team and no game, so they can
// all be live at once without one hedging another.
type ParlaySet struct {
	Parlays []Parlay
	// AvgConversion is the mean conversion per shot -- the number to compare
	// against the bonus-bet floor, since each parlay is one token.
	AvgConversion float64
	// AnyHit is P(at least one parlay in the set wins). Because the set is
	// disjoint across games, the legs are independent and this is
	// 1 - prod(1 - p_i). It is the honest answer to "will I see anything back
	// from this week", which the per-shot conversion does not give.
	AnyHit float64
	// Unfilled is how many requested shots could not be built, because the
	// candidate pool ran out of legs from distinct games.
	Unfilled int
}

// maxPoolGames caps the exhaustive pairing search.
//
// The search is exact over the pool: it considers every way of choosing
// disjoint pairs of GAMES, via a bitmask. 16 is the largest NFL week, so the
// cap does not bind on a single week's board. A larger pool is trimmed to the
// games whose best leg converts highest, which is the only heuristic in this
// file and worth naming: a game outside the top 16 could in principle belong
// to an optimal pair, though it would have to be rescued by a partner far
// better than any of the 16 it lost to.
const maxPoolGames = 16

// BuildSet chooses `shots` two-leg parlays that share no team and no game,
// maximising total conversion across the set.
//
// Total conversion, not average, is the objective: each shot is one bonus-bet
// token of the same face value, so the set is worth the sum of what its
// tickets convert. Greedily taking the best available pair repeatedly does not
// maximise that -- pairing the two longest dogs together burns both on one
// ticket -- so the search is exhaustive over disjoint pairings rather than
// greedy.
func BuildSet(legs []Side, shots int, o Objective, floor float64) (ParlaySet, error) {
	if shots < 0 {
		return ParlaySet{}, fmt.Errorf("shots %d must not be negative", shots)
	}
	pool := gamePool(legs)
	// Each shot consumes two games, so a pool of n games supports at most n/2
	// shots however they are arranged.
	want := shots
	if most := len(pool) / 2; want > most {
		want = most
	}
	if want == 0 {
		return ParlaySet{Unfilled: shots}, nil
	}

	pairs, picks, err := pairWeights(pool, o, floor)
	if err != nil {
		return ParlaySet{}, err
	}
	chosen := bestMatching(len(pool), want, pairs)

	set := ParlaySet{Unfilled: shots - len(chosen)}
	if len(chosen) == 0 {
		// Every candidate pair was disallowed -- a pool whose games all fail
		// the floor together, say. An empty set is the right answer; an
		// average over no parlays is not.
		return set, nil
	}
	total, miss := 0.0, 1.0
	for _, pr := range chosen {
		lp := picks[pr[0]][pr[1]]
		p, err := MakeParlay(pool[pr[0]].legs[lp[0]], pool[pr[1]].legs[lp[1]])
		if err != nil {
			return ParlaySet{}, err
		}
		set.Parlays = append(set.Parlays, p)
		total += p.Conversion
		miss *= 1 - p.TrueProb
	}
	// Highest conversion first: the set is read top-down when tokens are
	// placed, and a partially placed set should be the best part of it.
	sort.SliceStable(set.Parlays, func(i, j int) bool {
		return set.Parlays[i].Conversion > set.Parlays[j].Conversion
	})
	set.AvgConversion = total / float64(len(set.Parlays))
	set.AnyHit = 1 - miss
	return set, nil
}

// gamePool groups usable legs by game and trims to maxPoolGames.
//
// Grouping by GAME rather than by leg is a correctness requirement, not a
// tidying. A set is only independent if no game appears twice across it: two
// tickets both riding on ARI @ LAC are not two chances, they are one chance
// counted twice, and every hit-rate figure in the report would be inflated.
// While a game could offer only one leg -- the moneyline dog -- keeping legs
// flat was equivalent. Admitting spread and total sides makes it wrong, since
// two parlays could otherwise each take a different leg from the same game.
//
// So the matching runs over games, and the choice of WHICH leg a game
// contributes is made per candidate pair, where the objective can actually
// judge it. A leg with an unusable price is dropped rather than rejected: the
// pool comes from a board where most cells are empty, and one bad cell must
// not cost the whole set.
type gameLegs struct {
	gameID string
	legs   []Side
}

func gamePool(legs []Side) []gameLegs {
	byGame := map[string][]Side{}
	var order []string
	seen := make(map[string]bool, len(legs))
	for _, l := range legs {
		if _, err := l.Price.Decimal(); err != nil {
			continue
		}
		if l.Fair <= 0 || l.Fair >= 1 || math.IsNaN(l.Conversion) {
			continue
		}
		// One entry per distinct selection. Without this the same side
		// arriving from two books could be paired with itself.
		key := l.GameID + "|" + l.Market + "|" + l.Team
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, ok := byGame[l.GameID]; !ok {
			order = append(order, l.GameID)
		}
		byGame[l.GameID] = append(byGame[l.GameID], l)
	}

	out := make([]gameLegs, 0, len(order))
	for _, id := range order {
		ls := byGame[id]
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].Conversion > ls[j].Conversion })
		out = append(out, gameLegs{gameID: id, legs: ls})
	}
	// Trim by each game's best leg. The bitmask search is exponential in the
	// number of games, so the cap is a hard ceiling rather than a preference.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].legs[0].Conversion > out[j].legs[0].Conversion
	})
	if len(out) > maxPoolGames {
		out = out[:maxPoolGames]
	}
	return out
}

// pairWeights precomputes the conversion of every legal pair. A pair sharing a
// game is left at forbidden, which is what keeps the set disjoint by
// construction rather than by a check after the fact.
var forbidden = math.Inf(-1)

// Objective is what a set of parlays is chosen to maximise. The two pull in
// opposite directions and there is no set that is best at both.
type Objective int

const (
	// MaxHitRate maximises P(at least one ticket hits). This is the default
	// because it is why a stake is split at all: four shots exist to stop the
	// median outcome being zero, and a set that lifts conversion while
	// dropping the hit rate defeats its own purpose. Optimising conversion
	// alone on a real Week 1 board produced 82.9% conversion at a 31.9% hit
	// rate -- worse, on the metric that motivated splitting, than a hand-built
	// set at 75.1% and 51.1%.
	MaxHitRate Objective = iota

	// MaxConversion maximises total conversion: the most expected value per
	// dollar of face, variance disregarded. Correct when the stake is large
	// enough, or repeated often enough, for the median to stop mattering.
	MaxConversion
)

func (o Objective) String() string {
	if o == MaxConversion {
		return "conversion"
	}
	return "hitrate"
}

// ParseObjective reads the flag value.
func ParseObjective(s string) (Objective, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hitrate", "hit-rate", "":
		return MaxHitRate, nil
	case "conversion", "conv":
		return MaxConversion, nil
	}
	return 0, fmt.Errorf("unknown objective %q (want 'hitrate' or 'conversion')", s)
}

// pairWeight turns one candidate parlay into an additive DP weight.
//
// Conversion is already additive across a set, so it is its own weight. Hit
// rate is not: P(at least one hits) is 1 - product(1-p), and a product cannot
// be accumulated by a sum. Maximising it is the same as minimising the
// product, which is the same as maximising the sum of -log(1-p) -- so the
// existing matching DP carries over untouched, with only the weight changed.
func pairWeight(p Parlay, o Objective) float64 {
	if o == MaxConversion {
		return p.Conversion
	}
	// Log1p(-x) is log(1-x) computed without the cancellation that 1-x
	// suffers for small x, which is exactly the range longshot legs live in.
	return -math.Log1p(-p.TrueProb)
}

// pairWeights precomputes the weight of every legal game pair, and remembers
// which legs achieved it.
//
// A pair of games can be combined several ways once spreads and totals are in
// play -- ARI's moneyline with SEA's total, or ARI's spread with SEA's
// moneyline. The best combination depends on the objective, so it is chosen
// here rather than guessed at when the pool is built: picking one leg per game
// up front would have to decide without knowing the partner, and the longest
// leg in a game is the right choice under MaxConversion and the wrong one
// under MaxHitRate.
func pairWeights(pool []gameLegs, o Objective, floor float64) ([][]float64, [][][2]int, error) {
	n := len(pool)
	w := make([][]float64, n)
	picks := make([][][2]int, n)
	for i := range w {
		w[i] = make([]float64, n)
		picks[i] = make([][2]int, n)
		for j := range w[i] {
			w[i][j] = forbidden
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			best, bi, bj := forbidden, -1, -1
			for ii, a := range pool[i].legs {
				for jj, b := range pool[j].legs {
					// Games differ by construction here, but a selection can
					// still collide across books, and a leg must never face
					// itself.
					if a.Team == b.Team {
						continue
					}
					p, err := MakeParlay(a, b)
					if err != nil {
						return nil, nil, err
					}
					// Under MaxHitRate the objective pulls towards SHORT
					// prices, which is the opposite of what MaxConversion
					// does, so nothing stops it running down to near-even-money
					// pairs that convert terribly. The floor is what bounds it.
					// MaxConversion needs no such guard: it is already climbing
					// away from the floor by construction, and applying one
					// there would change long-standing behaviour.
					if o == MaxHitRate && p.Conversion < floor {
						continue
					}
					if ww := pairWeight(p, o); ww > best {
						best, bi, bj = ww, ii, jj
					}
				}
			}
			if bi < 0 {
				continue
			}
			w[i][j], w[j][i] = best, best
			picks[i][j] = [2]int{bi, bj}
			picks[j][i] = [2]int{bj, bi}
		}
	}
	return w, picks, nil
}

// bestMatching returns the `shots` disjoint pairs with the greatest total
// weight, by dynamic programming over a bitmask of still-available legs.
//
// The recursion always disposes of the lowest available leg -- either skip it
// or pair it with someone -- which visits each subset once instead of once per
// ordering. At 16 legs that is 65536 states per remaining shot.
func bestMatching(n, shots int, w [][]float64) [][2]int {
	states := 1 << n
	stride := shots + 1
	val := make([]float64, states*stride)
	// choice records how the lowest available leg was disposed of: -1 unvisited,
	// 0 skipped, j+1 paired with j.
	choice := make([]int32, states*stride)
	for i := range choice {
		choice[i] = -1
	}

	var solve func(mask, k int) float64
	solve = func(mask, k int) float64 {
		if k == 0 || mask == 0 {
			return 0
		}
		idx := mask*stride + k
		if choice[idx] >= 0 {
			return val[idx]
		}
		i := lowestBit(mask)
		rest := mask &^ (1 << i)
		best, how := solve(rest, k), int32(0) // skip i
		for j := i + 1; j < n; j++ {
			if mask&(1<<j) == 0 || w[i][j] == forbidden {
				continue
			}
			if v := w[i][j] + solve(rest&^(1<<j), k-1); v > best {
				best, how = v, int32(j+1)
			}
		}
		val[idx], choice[idx] = best, how
		return best
	}
	solve(states-1, shots)

	var out [][2]int
	for mask, k := states-1, shots; k > 0 && mask != 0; {
		how := choice[mask*stride+k]
		i := lowestBit(mask)
		mask &^= 1 << i
		if how == 0 {
			continue
		}
		j := int(how) - 1
		mask &^= 1 << j
		out = append(out, [2]int{i, j})
		k--
	}
	return out
}

func lowestBit(mask int) int {
	for i := 0; ; i++ {
		if mask&(1<<i) != 0 {
			return i
		}
	}
}

// Quote is one book's price for one side, for line shopping.
type Quote struct {
	Book  string
	Price wager.American
}

// ShopRow is the best available price for one side of one game, against the
// consensus reference.
//
// Consensus is not a book you can bet, which is exactly what makes it the
// right benchmark: it says what the side is worth, so the gap says what taking
// the best bettable price costs. A board once showed consensus ARI +455 while
// Fanatics had +390 -- 65 points, eaten unknowingly, because nothing put the
// two numbers next to each other.
type ShopRow struct {
	GameID    string
	Kickoff   string
	Team      string
	Best      Quote
	Others    []Quote
	Consensus wager.American
	HasCons   bool
	// Points is Best minus Consensus in American points, valid only when the
	// two prices share a sign. Across the sign boundary American points are
	// not a scale (+100 and -100 are the same price, and there is nothing in
	// between), so subtracting them there produces a number that looks like a
	// 200-point gap and means nothing.
	Points      int
	PointsValid bool
	// PayoutGap is the fractional change in total return per unit staked from
	// taking Best instead of Consensus. It is defined for every pair of prices
	// and is the number to trust when Points is not valid.
	PayoutGap float64
}

// Analysis is one week's board, read for one book.
type Analysis struct {
	Season int
	Week   int
	Book   string
	Target float64
	// Floor is the shortest price that meets Target at fair odds (+234 at
	// 0.70). It is a ceiling-based bound, so a price above it can still fail
	// on conversion once the vig is removed -- which is the whole reason Dogs
	// is ranked by conversion and not by price.
	Floor   wager.American
	Lines   []GameLine // kickoff order, only games this book has priced
	Missing []string   // game ids with no price at this book
	Suspect []GameLine // implausible overround; a subset of Lines
	Dogs    []Side     // one per priced game, best conversion first
	Set     ParlaySet
	Shop    []ShopRow
	// Problems are cells that failed to parse. Reported rather than returned
	// as an error so one bad cell does not hide the rest of the board.
	Problems []string

	// Provisional is true when any game in the week has no price at this book.
	//
	// It is not a warning about data quality; the prices that are there are
	// fine. It is a statement about what the parlay set means. The set is the
	// best pairing of what is entered, and the best pairing of six games is
	// not the best pairing of sixteen -- entering the rest will usually change
	// it. A board is partly filled nearly all of the time, so this is the
	// normal case and has to be said out loud rather than inferred from a
	// count the reader has to notice.
	Provisional bool

	// PricedBooks lists the books other than consensus with at least one price
	// this week. Below two of them there is nothing to shop -- a "best
	// available price" with one candidate is a tautology, not a choice.
	//
	// Named apart from the Bettable() method below, which asks a different
	// question: whether the ONE book being reported on has any prices.
	PricedBooks []string
}

// Options configures Analyze.
type Options struct {
	Book      string    // which book's prices to read
	Target    float64   // bonus-bet conversion floor
	Shots     int       // how many disjoint parlays to build
	Objective Objective // what the parlay set is chosen to maximise
	// Lined admits spread and total sides as parlay legs alongside moneyline
	// dogs. Only safe at a book that returns the stake on a push -- see
	// LinedLegs -- so it is opt-in rather than assumed.
	Lined bool
	// Exclude drops every leg belonging to these teams, by nflverse code.
	//
	// A set is only independent if nothing in it is already committed
	// elsewhere. Five tokens placed on this week's board tie up seven teams,
	// and a report that does not know about them will happily propose a
	// ticket riding a team you are already on -- which is not a fifth chance,
	// it is more of the fourth. Until placements are recorded and this can be
	// derived, it is passed in.
	Exclude []string
}

// Analyze reads the whole week for one book.
//
// It never fails on a mostly-empty board. Almost every cell in a scaffolded
// season is empty and only consensus is filled today, so "this book has no
// prices" is a normal answer that the caller reports, not an error.
func Analyze(d *Doc, opt Options) (*Analysis, error) {
	if d == nil {
		return nil, fmt.Errorf("board: no document")
	}
	book := opt.Book
	if book == "" {
		book = Consensus
	}
	target := opt.Target
	if target <= 0 {
		target = DefaultTarget
	}
	floor, err := wager.MinPriceForConversion(target)
	if err != nil {
		return nil, err
	}

	a := &Analysis{Season: d.Season, Week: d.Week, Book: book, Target: target, Floor: floor}
	a.PricedBooks = d.BettableBooks()
	for _, id := range d.GameIDs() {
		g := d.Games[id]
		l, ok, err := Devig(id, g, book)
		if err != nil {
			a.Problems = append(a.Problems, err.Error())
			continue
		}
		if !ok {
			a.Missing = append(a.Missing, id)
			continue
		}
		a.Lines = append(a.Lines, l)
		if l.Suspect {
			a.Suspect = append(a.Suspect, l)
		}
		a.Dogs = append(a.Dogs, l.Dog())
	}
	sort.SliceStable(a.Dogs, func(i, j int) bool { return a.Dogs[i].Conversion > a.Dogs[j].Conversion })

	// A cell that failed to parse is a game this book has not usefully priced,
	// so it counts towards provisional exactly as a blank one does. Otherwise
	// a board full of typos would report itself complete.
	a.Provisional = len(a.Missing) > 0 || len(a.Problems) > 0

	// Every dog is a candidate leg, including the ones that fail the floor on
	// their own. That is the point of pairing them: two dogs converting 0.53
	// and 0.55 singly make a ticket that converts 0.74, because the price
	// lengthens faster than the probability falls. Filtering the pool by the
	// single-bet floor would throw away exactly the legs the set exists for.
	//
	// A game with an implausible overround is held back, because its de-vig is
	// the number in doubt and a parlay would propagate it into three more
	// figures. The caller reports the exclusion rather than hiding it.
	var legs []Side
	for _, l := range a.Lines {
		if l.Suspect {
			continue
		}
		legs = append(legs, l.Dog())
	}
	if opt.Lined {
		// Lined legs come from every game with a spread or total, including
		// games whose moneyline is missing or suspect: a spread stands on its
		// own and does not inherit a doubt about a different market.
		for _, id := range d.GameIDs() {
			extra, err := LinedLegs(id, d.Games[id], book)
			if err != nil {
				a.Problems = append(a.Problems, err.Error())
				continue
			}
			legs = append(legs, extra...)
		}
	}
	// Excluded teams are dropped before pairing rather than filtered after, so
	// the search optimises over what is actually available instead of
	// returning its best answer and having it struck out. This runs AFTER the
	// lined legs are added: a spread on a committed team is just as correlated
	// as its moneyline.
	if len(opt.Exclude) > 0 {
		drop := make(map[string]bool, len(opt.Exclude))
		for _, t := range opt.Exclude {
			if t = strings.ToUpper(strings.TrimSpace(t)); t != "" {
				drop[t] = true
			}
		}
		kept := legs[:0]
		for _, l := range legs {
			// A lined leg's Team carries a label -- "ATL +3.5", or "over 44.5"
			// with no team in it at all -- so match the leading word and let
			// anything wordless through.
			f := strings.Fields(l.Team)
			if len(f) > 0 && drop[strings.ToUpper(f[0])] {
				continue
			}
			kept = append(kept, l)
		}
		legs = kept
	}
	sort.SliceStable(legs, func(i, j int) bool { return legs[i].Conversion > legs[j].Conversion })
	if a.Set, err = BuildSet(legs, opt.Shots, opt.Objective, opt.Target); err != nil {
		return nil, err
	}

	a.Shop = shopRows(d)
	return a, nil
}

// Bettable reports whether the requested book has any prices at all.
func (a *Analysis) Bettable() bool { return len(a.Lines) > 0 }

// Parlays is the built set, for callers that only want the tickets.
func (a *Analysis) Parlays() []Parlay { return a.Set.Parlays }

// shopRows compares every bettable book against consensus, per side.
//
// Consensus is excluded from "best available" because it cannot be bet; a
// board where only consensus is filled therefore produces no rows at all,
// which is the honest report of a board with nothing to shop.
func shopRows(d *Doc) []ShopRow {
	var out []ShopRow
	for _, id := range d.GameIDs() {
		g := d.Games[id]
		cons, hasCons, err := ParseMarket(g.Books[Consensus].ML)
		if err != nil {
			hasCons = false
		}
		for side := 0; side < 2; side++ {
			team := g.Away
			if side == 1 {
				team = g.Home
			}
			var quotes []Quote
			for _, bk := range Books {
				if bk == Consensus {
					continue
				}
				m, ok, err := ParseMarket(g.Books[bk].ML)
				if err != nil || !ok {
					continue
				}
				p := m.A
				if side == 1 {
					p = m.B
				}
				quotes = append(quotes, Quote{Book: bk, Price: p})
			}
			if len(quotes) == 0 {
				continue
			}
			// Best is the longest price: the one paying most per unit staked.
			// Decimal odds order correctly across the sign boundary, where the
			// raw integers do not.
			sort.SliceStable(quotes, func(i, j int) bool {
				di, _ := quotes[i].Price.Decimal()
				dj, _ := quotes[j].Price.Decimal()
				return di > dj
			})
			row := ShopRow{
				GameID: id, Kickoff: g.Kickoff, Team: team,
				Best: quotes[0], Others: quotes[1:],
			}
			if hasCons {
				c := cons.A
				if side == 1 {
					c = cons.B
				}
				row.Consensus, row.HasCons = c, true
				if (c > 0) == (row.Best.Price > 0) {
					row.Points, row.PointsValid = int(row.Best.Price)-int(c), true
				}
				db, errB := row.Best.Price.Decimal()
				dc, errC := c.Decimal()
				if errB == nil && errC == nil && dc > 0 {
					row.PayoutGap = db/dc - 1
				}
			}
			out = append(out, row)
		}
	}
	return out
}

// LinedLegs returns the spread and total sides of one game as parlay legs.
//
// These exist as legs because Fanatics returns the stake on a push (House
// Rules, recorded 2026-08-21), so a whole-number line no longer risks the
// asset. Under a forfeit-on-push book they would have to stay out: there the
// downside is losing the bonus outright, not converting less.
//
// A standard -110/-110 line de-vigs to exactly 50% a side, which makes it the
// least biased leg on a board -- a moneyline carries favourite-longshot bias,
// and a longshot carries the most vig of anything quoted. That does NOT make a
// spread leg a better bet than a long dog: at a matched parlay price the two
// convert within a point of each other.
//
// What it buys is REACH ALONG THE FRONTIER, and it is not free. Measured on a
// full 16-game board at 8 shots: under MaxConversion it changes nothing at all,
// because moneyline dogs already maximise conversion and a 50% leg cannot
// improve on them. Under MaxHitRate it moves 77.9%/70.5% to 71.4%/83.5% --
// thirteen points of hit rate bought with thirteen dollars of expected value.
// A spread leg is a way to reach a SHORTER parlay that still clears the floor,
// which is exactly what a hit-rate objective wants and a conversion objective
// does not.
//
// It also relieves the pool when few games have a dog past the floor: every
// game has a spread, so tickets can be built where moneylines alone would run
// out. That case is a genuine gain rather than a trade.
//
// Both sides of a lined market are returned. They are mutually exclusive, but
// the pairing already refuses two legs from one game, so nothing has to be
// decided here.
func LinedLegs(gameID string, g *Game, book string) ([]Side, error) {
	if g == nil {
		return nil, fmt.Errorf("%s: no such game", gameID)
	}
	var out []Side
	l := g.Books[book]

	for _, m := range []struct {
		name, raw string
		awayLabel func(line float64) string
		homeLabel func(line float64) string
	}{
		{"spread", l.Spread,
			func(v float64) string { return fmt.Sprintf("%s %+g", g.Away, v) },
			func(v float64) string { return fmt.Sprintf("%s %+g", g.Home, -v) }},
		{"total", l.Total,
			func(v float64) string { return fmt.Sprintf("over %g", v) },
			func(v float64) string { return fmt.Sprintf("under %g", v) }},
	} {
		line, mkt, ok, err := ParseHandicap(m.raw)
		if err != nil {
			return nil, fmt.Errorf("%s %s %s: %w", gameID, book, m.name, err)
		}
		if !ok {
			continue
		}
		fairA, fairB, err := mkt.FairDevig()
		if err != nil {
			return nil, fmt.Errorf("%s %s %s: %w", gameID, book, m.name, err)
		}
		// The team field carries the label rather than a bare team name: two
		// legs from one game must never look interchangeable in a set, and
		// "over 44.5" has no team at all.
		a, err := makeSide(gameID, m.awayLabel(line), false, mkt.A, fairA)
		if err != nil {
			return nil, err
		}
		b, err := makeSide(gameID, m.homeLabel(line), true, mkt.B, fairB)
		if err != nil {
			return nil, err
		}
		a.Market, a.Label = m.name, m.awayLabel(line)
		b.Market, b.Label = m.name, m.homeLabel(line)
		out = append(out, a, b)
	}
	return out, nil
}

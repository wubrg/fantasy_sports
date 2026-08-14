package draft

import (
	"math/rand"
	"sort"
)

// Objective is the lens a team sample is drawn through.
type Objective string

const (
	// ObjectiveStrategy fills a legal roster that respects your one-per-offense
	// and no-handcuff preferences and secures your must-haves — a team you would
	// actually be content to end up with.
	ObjectiveStrategy Objective = "strategy"
	// ObjectiveLeansMax lands as many of your must/up players as the budget
	// allows, preferences relaxed. How many of your guys can you actually get.
	ObjectiveLeansMax Objective = "leans-max"
)

// priceSwing is the half-width of the auction-price noise. A player goes for
// his board cost give or take this fraction, drawn from a triangular
// distribution so the projected cost is the mode and the tails are thin — the
// auction is noisy, not wild. Tunable; 0.30 spans roughly what a contested
// nomination really swings.
const priceSwing = 0.30

// defReserve is the money and the slot held back for a team defense. Ciely's
// projections omit DST and defenses go for a dollar, so the sample fills the
// skill lineup and leaves the defense as a reserved dollar rather than pricing
// one.
const defReserve = 1

// positionCap limits how many of a position a sampled roster will carry. Three
// quarterbacks or three tight ends is a wasted draft: you start one, maybe
// bench one for a bye, and a third never plays. Backs and receivers fill the
// flex and the bench, so they are left uncapped.
var positionCap = map[string]int{"QB": 2, "TE": 2}

// TeamSample is one sampled roster and how it reads, scored the same way the
// live board and the scratch panel score a lineup.
type TeamSample struct {
	Objective Objective    `json:"objective"`
	Starters  []RosterSpot `json:"starters"`
	Bench     []RosterSpot `json:"bench"`
	// Spend is what the skill roster cost; the reserved defense dollar is not
	// counted here.
	Spend int     `json:"spend"`
	POPR  float64 `json:"popr"`
	// MyGuys is must + up players landed; MustLanded the must-haves alone.
	MyGuys     int `json:"myGuys"`
	MustLanded int `json:"mustLanded"`
	// Unfilled names any starting slot the draw could not cover.
	Unfilled []string `json:"unfilled,omitempty"`
}

// SampleTeams draws n plausible auction outcomes against a pool and returns the
// distinct teams, best POPR first.
//
// Each draw prices every player at his board cost with a random swing, then
// fills a roster under budget: must-haves first, then the required starting
// slots, then the bench. It is a sample of what the auction could hand you, not
// a claim about the best team or whether any particular one is reachable — the
// noise across draws is the whole point.
func SampleTeams(pool []PlayerSignals, keepers []RosterSpot, budget, openSlots int,
	shape PoolState, baselines map[string]float64, prefs Preferences,
	objective Objective, n int, seed int64) []TeamSample {

	// Skill-only shape: reserve one slot and a dollar for the defense the pool
	// cannot price, so a complete skill roster reads as filled.
	skill := shape
	skill.Starters = map[string]int{}
	for pos, k := range shape.Starters {
		if pos == "DEF" || pos == "DST" {
			continue
		}
		skill.Starters[pos] = k
	}
	sampleBudget := budget - defReserve
	sampleSlots := openSlots - defReserve

	seen := map[string]bool{}
	var out []TeamSample
	for i := 0; i < n; i++ {
		rng := rand.New(rand.NewSource(seed + int64(i)))
		r := fillOne(pool, keepers, sampleBudget, sampleSlots, skill, prefs, objective, rng)

		key := teamKey(r)
		if seen[key] {
			continue
		}
		seen[key] = true

		m := Score(r, baselines, skill)
		s := TeamSample{
			Objective:  objective,
			Starters:   r.Starters(),
			Bench:      r.Bench(),
			Spend:      m.Spend,
			POPR:       m.POPR,
			MyGuys:     m.MyGuys,
			MustLanded: mustLanded(r),
			Unfilled:   m.Unfilled,
		}
		out = append(out, s)
	}

	sort.SliceStable(out, func(a, b int) bool {
		if out[a].MyGuys != out[b].MyGuys && objective == ObjectiveLeansMax {
			return out[a].MyGuys > out[b].MyGuys
		}
		return out[a].POPR > out[b].POPR
	})
	return out
}

// fillOne builds one roster from one price draw.
func fillOne(pool []PlayerSignals, keepers []RosterSpot, budget, slots int,
	skill PoolState, prefs Preferences, objective Objective, rng *rand.Rand) *Roster {

	r := &Roster{}
	for _, k := range keepers {
		k.Held = true
		r.Players = append(r.Players, k)
	}
	owned := map[string]bool{}
	for _, k := range keepers {
		owned[k.Player.PlayerID] = true
	}

	// One market price per player for this draw.
	price := make(map[string]int, len(pool))
	for _, p := range pool {
		price[p.PlayerID] = drawPrice(p.Cost, rng)
	}

	budgetLeft, slotsLeft := budget, slots
	// A buy is only allowed if a dollar is still left for every other empty
	// slot, so a draw can never strand you unable to complete the lineup.
	affordable := func(pid string) bool {
		return slotsLeft > 0 && price[pid] <= budgetLeft-(slotsLeft-1)
	}
	buy := func(p PlayerSignals) {
		r.Add(p, price[p.PlayerID])
		owned[p.PlayerID] = true
		budgetLeft -= price[p.PlayerID]
		slotsLeft--
	}
	// eligible reports whether a candidate can be taken: on the board, not
	// owned, never a do-not-draft, and — under the strategy objective — not
	// blocked by your own preferences given who you already hold.
	eligible := func(p PlayerSignals) bool {
		if owned[p.PlayerID] || p.Lean.Lean == LeanDND {
			return false
		}
		// No stacking a third of a one-slot position — a wasted roster spot.
		if lim := positionCap[p.Position]; lim > 0 && have(r, p.Position) >= lim {
			return false
		}
		// Your ceiling gates your must-haves: if this draw sends one past your
		// walk-away price you were outbid and do not get him, which is why the
		// same guy lands on some sampled teams and not others.
		if p.Lean.Lean == LeanMust && p.MyMaxBid > 0 && price[p.PlayerID] > p.MyMaxBid {
			return false
		}
		if objective == ObjectiveStrategy && prefs.Active() {
			if mates := ownedMates(r, p.Team); len(mates) > 0 && blockReason(p, mates, prefs) != "" {
				return false
			}
		}
		return true
	}

	// Phase 1 — must-haves, best (most you would pay) first.
	musts := filterLean(pool, LeanMust)
	sort.SliceStable(musts, func(a, b int) bool { return musts[a].MyMaxBid > musts[b].MyMaxBid })
	for _, p := range musts {
		if eligible(p) && affordable(p.PlayerID) {
			buy(p)
		}
	}

	// Phase 2 — the required starting slots, in a fixed order, then the flex.
	for _, pos := range []string{"QB", "RB", "WR", "TE"} {
		for have(r, pos) < skill.Starters[pos] {
			p, ok := choose(pool, []string{pos}, objective, eligible, affordable, rng)
			if !ok {
				break
			}
			buy(p)
		}
	}
	for k := 0; k < skill.FlexSlots; k++ {
		p, ok := choose(pool, skill.FlexPositions, objective, eligible, affordable, rng)
		if !ok {
			break
		}
		buy(p)
	}

	// Phase 3 — bench: spend what is left on more of your guys (leans-max) or a
	// good affordable player (strategy), until the roster or the budget is full.
	// As the money tightens this naturally lands the dollar fliers a real
	// auction ends on.
	for slotsLeft > 0 {
		p, ok := choose(pool, []string{"QB", "RB", "WR", "TE"}, objective, eligible, affordable, rng)
		if !ok {
			break
		}
		buy(p)
	}
	return r
}

// drawPrice is a board cost with a triangular auction swing, floored at $1.
func drawPrice(cost int, rng *rand.Rand) int {
	base := cost
	if base < 1 {
		base = 1
	}
	// Two uniforms summed is triangular on [-1,1] with the mode at 0.
	eps := (rng.Float64() + rng.Float64() - 1) * priceSwing
	p := int(float64(base)*(1+eps) + 0.5)
	if p < 1 {
		return 1
	}
	return p
}

// topK is how many of the best affordable candidates a pick chooses randomly
// among. One would be greedy and collapse every draw to the same team; a wide
// field would buy anyone. A few keeps the sample both good and varied — this is
// the selection noise that, with the price noise, makes it a real sample.
const topK = 4

// pick chooses one eligible, affordable player from the given positions:
// ranked by the objective (leans-max floats your up-tagged guys, then
// projection; strategy by projection), then drawn at random from the top few so
// the sample spreads across the teams you could plausibly assemble.
func choose(pool []PlayerSignals, positions []string, objective Objective,
	eligible func(PlayerSignals) bool, affordable func(string) bool, rng *rand.Rand) (PlayerSignals, bool) {

	ok := map[string]bool{}
	for _, pos := range positions {
		ok[pos] = true
	}
	var cands []PlayerSignals
	for _, p := range pool {
		if ok[p.Position] && eligible(p) && affordable(p.PlayerID) {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		return PlayerSignals{}, false
	}
	sort.SliceStable(cands, func(a, b int) bool { return betterPick(cands[a], cands[b], objective) })
	k := topK
	if k > len(cands) {
		k = len(cands)
	}
	return cands[rng.Intn(k)], true
}

// betterPick ranks two candidates for the objective. Leans-max lifts your
// up-tagged guys above a slightly better stranger; both then go by projection.
func betterPick(a, b PlayerSignals, objective Objective) bool {
	if objective == ObjectiveLeansMax {
		if mine(a) != mine(b) {
			return mine(a)
		}
	}
	return a.CielyPoints > b.CielyPoints
}

func mine(p PlayerSignals) bool { return p.Lean.Lean == LeanMust || p.Lean.Lean == LeanUp }

func have(r *Roster, pos string) int {
	n := 0
	for _, s := range r.Players {
		if s.Player.Position == pos {
			n++
		}
	}
	return n
}

// ownedMates is the players you already hold on one NFL team, for the
// preference check during a strategy fill.
func ownedMates(r *Roster, team string) []PlayerSignals {
	var out []PlayerSignals
	for _, s := range r.Players {
		if s.Player.Team == team {
			out = append(out, s.Player)
		}
	}
	return out
}

func filterLean(pool []PlayerSignals, lean Lean) []PlayerSignals {
	var out []PlayerSignals
	for _, p := range pool {
		if p.Lean.Lean == lean {
			out = append(out, p)
		}
	}
	return out
}

func mustLanded(r *Roster) int {
	n := 0
	for _, s := range r.Players {
		if s.Player.Lean.Lean == LeanMust {
			n++
		}
	}
	return n
}

// teamKey identifies a roster by its set of players, so two draws that buy the
// same men at different prices count as one team.
func teamKey(r *Roster) string {
	ids := make([]string, 0, len(r.Players))
	for _, s := range r.Players {
		ids = append(ids, s.Player.PlayerID)
	}
	sort.Strings(ids)
	key := ""
	for _, id := range ids {
		key += id + ","
	}
	return key
}

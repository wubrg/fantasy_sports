package draft

import "sort"

// Roster is a set of players held together with what each cost.
type Roster struct {
	Players []RosterSpot
}

// BoardPrice is what the cost board says a player will go for, floored at a
// dollar. The only pricing mode there is; kept as a function because a
// hypothetical roster has to price players the same way the live board does.
func BoardPrice(p PlayerSignals) int {
	if p.Cost > 0 {
		return p.Cost
	}
	return 1
}

// RosterSpot is one player and the price paid for him.
type RosterSpot struct {
	Player PlayerSignals
	Price  int
	// Starting is filled in by Score: true if he makes the lineup.
	Starting bool
	// Slot is the lineup position he fills, "FLEX" for the flex spot and
	// "BN" for a bench player.
	Slot string
	// Held marks a player already owned — a keeper. He cannot be sold to
	// make room, so no fill may swap him out.
	Held bool
	// Anchored marks a player bought specifically to satisfy an anchor.
	//
	// Also immovable, for a different reason: he is the shape. The upgrade
	// pass maximizes points per dollar and will happily trade four red-zone
	// aces for four better players who score nothing at the goal line,
	// dismantling the shape it was asked to build and then reporting it
	// unreachable.
	Anchored bool
}

// Add puts a player on the roster at a price.
func (r *Roster) Add(p PlayerSignals, price int) {
	r.Players = append(r.Players, RosterSpot{Player: p, Price: price})
}

// Remove drops the first spot holding a player, reporting whether it found
// one.
func (r *Roster) Remove(playerID string) bool {
	for i, s := range r.Players {
		if s.Player.PlayerID == playerID {
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
			return true
		}
	}
	return false
}

// Spend is the total paid.
func (r Roster) Spend() int {
	total := 0
	for _, s := range r.Players {
		total += s.Price
	}
	return total
}

// Has reports whether a player is already on the roster.
func (r Roster) Has(playerID string) bool {
	for _, s := range r.Players {
		if s.Player.PlayerID == playerID {
			return true
		}
	}
	return false
}

// Counts returns how many players are held at each position.
func (r Roster) Counts() map[string]int {
	out := map[string]int{}
	for _, s := range r.Players {
		out[s.Player.Position]++
	}
	return out
}

// ScoringBaselines returns the replacement points every roster is scored
// against.
//
// Deliberately pinned to VOLS and to the full pre-draft pool, for two
// separate reasons:
//
// Pinned to VOLS because points above positional replacement and value over
// last starter are the same measure when replacement sits at the last
// starter, which is what BaselineVOLS means. The board's curve is a live
// switch defaulting to BEER+, and scoring lineups against whatever happens
// to be selected would silently rescore every shape you had looked at when
// you flipped it. Measuring a starting lineup against the worst startable
// alternative is also simply the right frame.
//
// Pinned to the pre-draft pool because replacement level moves as players
// leave the board. Scoring each hypothetical against its own baseline would
// make two rosters incomparable — the same error that made the raw edge list
// read as nine tight ends.
func ScoringBaselines(allPlayers []Projection, shape PoolState) map[string]float64 {
	state := shape
	state.Baseline = BaselineVOLS
	state.Filled = nil
	return ReplacementLevels(allPlayers, state)
}

// RosterMetrics is how a roster shape reads.
type RosterMetrics struct {
	// POPR is the starting lineup's points above positional replacement.
	// This is the headline number.
	POPR float64
	// StartingPoints is the same lineup's raw projected points, shown
	// alongside because raw points flatter whichever position the
	// projection source is generous about.
	StartingPoints float64
	// BenchPoints is what the bench projects, reported separately rather
	// than folded in: VOLS values the last starter at zero, so a bench
	// scores as free, which is right for comparing lineups and wrong for
	// judging depth.
	BenchPoints float64

	Spend         int
	SpendPosition map[string]int
	Counts        map[string]int

	// Unfilled lists starting slots the roster cannot cover.
	Unfilled []string

	// Soft counts. Structured so more tags can be added without changing
	// the shape of this type.
	MyGuys        int
	DNDViolations int
	Contested     int
	Injured       int
}

// Filled reports whether every starting slot is covered.
func (m RosterMetrics) Filled() bool { return len(m.Unfilled) == 0 }

// Score works out the best legal lineup and measures it.
//
// Lineup filling is greedy by position and then flex, which is optimal here:
// the position slots are disjoint and flex eligibility is a superset of
// them, so holding a better player back for the flex can never help.
func Score(r *Roster, baselines map[string]float64, shape PoolState) RosterMetrics {
	m := RosterMetrics{
		SpendPosition: map[string]int{},
		Counts:        map[string]int{},
	}

	// Best first within each position.
	byPos := map[string][]int{}
	for i := range r.Players {
		r.Players[i].Starting, r.Players[i].Slot = false, "BN"
		p := r.Players[i].Player
		byPos[p.Position] = append(byPos[p.Position], i)
		m.Spend += r.Players[i].Price
		m.SpendPosition[p.Position] += r.Players[i].Price
		m.Counts[p.Position]++

		switch p.Lean.Lean {
		case LeanMust, LeanUp:
			m.MyGuys++
		case LeanDND:
			// A shape should never contain one of these; counting them
			// makes a violation visible rather than silent.
			m.DNDViolations++
		}
		if p.ECR == ECRContested {
			m.Contested++
		}
		if p.Availability != "" {
			m.Injured++
		}
	}
	for pos := range byPos {
		idx := byPos[pos]
		sort.SliceStable(idx, func(a, b int) bool {
			return r.Players[idx[a]].Player.CielyPoints > r.Players[idx[b]].Player.CielyPoints
		})
	}

	used := map[int]bool{}
	take := func(pos, slot string) bool {
		for _, i := range byPos[pos] {
			if used[i] {
				continue
			}
			used[i] = true
			r.Players[i].Starting, r.Players[i].Slot = true, slot
			return true
		}
		return false
	}

	for pos, n := range shape.Starters {
		for k := 0; k < n; k++ {
			if !take(pos, pos) {
				m.Unfilled = append(m.Unfilled, pos)
			}
		}
	}

	// The flex takes the best player left among the eligible positions.
	for k := 0; k < shape.FlexSlots; k++ {
		best, bestPts := -1, 0.0
		for _, pos := range shape.FlexPositions {
			for _, i := range byPos[pos] {
				if used[i] {
					continue
				}
				if pts := r.Players[i].Player.CielyPoints; best < 0 || pts > bestPts {
					best, bestPts = i, pts
				}
				break // the position list is sorted, so only its head matters
			}
		}
		if best < 0 {
			m.Unfilled = append(m.Unfilled, "FLEX")
			continue
		}
		used[best] = true
		r.Players[best].Starting, r.Players[best].Slot = true, "FLEX"
	}

	for i, s := range r.Players {
		pts := s.Player.CielyPoints
		if !r.Players[i].Starting {
			m.BenchPoints += pts
			continue
		}
		m.StartingPoints += pts
		if above := pts - baselines[s.Player.Position]; above > 0 {
			m.POPR += above
		}
	}
	sort.Strings(m.Unfilled)
	return m
}

// Starters returns the spots making the lineup, in slot order. Score must
// have run first.
func (r Roster) Starters() []RosterSpot {
	order := map[string]int{"QB": 0, "RB": 1, "WR": 2, "TE": 3, "FLEX": 4}
	var out []RosterSpot
	for _, s := range r.Players {
		if s.Starting {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].Slot] != order[out[j].Slot] {
			return order[out[i].Slot] < order[out[j].Slot]
		}
		return out[i].Price > out[j].Price
	})
	return out
}

// Bench returns the spots not in the lineup, most expensive first.
func (r Roster) Bench() []RosterSpot {
	var out []RosterSpot
	for _, s := range r.Players {
		if !s.Starting {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Price > out[j].Price })
	return out
}

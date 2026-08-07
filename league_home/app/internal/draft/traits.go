package draft

import "sort"

// Trait is a kind of player rather than a price.
//
// The money shapes ask how to divide a budget. Once keepers hand you a
// measurable surplus that question is largely settled — you are not choosing
// between Stars & Scrubs and Balanced, you are choosing what to do with what
// is left. These say what kind of players that money buys.
type Trait string

const (
	// TraitFloor is a player whose points come from usage that repeats:
	// receptions and yardage rather than touchdowns. He rarely wins you a
	// week and rarely loses one.
	TraitFloor Trait = "floor"
	// TraitRedZone is a player whose points are concentrated in scoring
	// plays. Touchdown equity is the least predictable thing in football,
	// which cuts both ways — cornering it is a strategy, not an accident.
	TraitRedZone Trait = "redzone"
	// TraitUpside is a player the median understates: either the industry
	// disagrees with the consensus about him, or he is projected well past
	// anything he has actually done.
	TraitUpside Trait = "upside"
	// TraitTargetHog is raw projected target volume. The stickiest thing a
	// pass catcher has — targets survive bad games, touchdowns do not.
	TraitTargetHog Trait = "targets"
	// TraitBellCow is a back carrying both the ground work and the passing
	// down work, rather than a share of a committee.
	TraitBellCow Trait = "bellcow"
	// TraitDiscounted is a player whose price is suppressed by an injury
	// designation. You are buying the discount and the risk together.
	TraitDiscounted Trait = "discounted"
)

// TraitNames is every trait in report order.
var TraitNames = []Trait{
	TraitFloor, TraitRedZone, TraitUpside,
	TraitTargetHog, TraitBellCow, TraitDiscounted,
}

// Label is the trait in words, for a report.
func (t Trait) Label() string {
	switch t {
	case TraitFloor:
		return "high floor"
	case TraitRedZone:
		return "red-zone ace"
	case TraitUpside:
		return "upside"
	case TraitTargetHog:
		return "target hog"
	case TraitBellCow:
		return "bell cow"
	case TraitDiscounted:
		return "injury discount"
	}
	return string(t)
}

// TraitSet is what a player is, as a sorted list so it renders the same way
// twice.
type TraitSet []Trait

// Has reports whether the set contains a trait.
func (s TraitSet) Has(t Trait) bool {
	for _, x := range s {
		if x == t {
			return true
		}
	}
	return false
}

// TraitInput is everything the classifier needs about one player.
type TraitInput struct {
	PlayerID string
	Position string
	Points   float64
	Parts    Components
	// ECRUpside is true when the industry flags him as beating consensus.
	ECRUpside bool
	// Injury is a Sleeper designation, empty when healthy.
	Injury string
	// PriorPoints and PriorGames are last season's half-PPR production.
	// PriorGames of zero means no sample, which is a rookie or a player
	// who missed the year — either way, nothing to check the projection
	// against.
	PriorPoints float64
	PriorGames  int
}

// Trait thresholds. Explicit and adjustable in the same spirit as the
// baseline depths: the players they select should be checked against the
// board rather than trusted.
const (
	// traitWindow is how many players deep at each position the
	// percentiles are taken over, as a multiple of starting demand.
	// Past that everyone is waiver fodder and their ratios are noise.
	traitWindow = 3
	// unprovenLeap is the per-game multiple over last season that counts
	// as projected past your record. The league's own distribution puts
	// the 90th percentile at 1.32, so this is past where projections
	// normally sit rather than a round number.
	unprovenLeap = 1.4
	// provenGames is the sample below which last season says nothing.
	provenGames = 6
	// bellCowTargets is the per-season target count that separates an
	// every-down back from a committee member on passing downs.
	bellCowTargets = 45
)

// ClassifyTraits labels every player, position by position.
//
// Position-relative throughout. A 27% touchdown share is ordinary for a
// running back and high for a receiver, so an absolute cut would label whole
// positions rather than players. The comparison set is the startable range
// at each position, for the same reason the scarcity count ignores waiver
// fodder: hundreds of players projected for nothing would drag every
// percentile down to meaninglessness.
func ClassifyTraits(in []TraitInput, shape PoolState) map[string]TraitSet {
	state := shape
	state.Baseline = BaselineVOLS
	state.Filled = nil
	depth := replacementDepth(state)

	byPos := map[string][]TraitInput{}
	for _, p := range in {
		byPos[p.Position] = append(byPos[p.Position], p)
	}

	out := map[string]TraitSet{}
	for pos, list := range byPos {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Points > list[j].Points })
		window := depth[pos] * traitWindow
		if window > len(list) || window == 0 {
			window = len(list)
		}
		ranked := list[:window]

		tdHi := quantileOf(ranked, 0.75, tdShare)
		tdLo := quantileOf(ranked, 0.40, tdShare)
		recHi := quantileOf(ranked, 0.60, recShare)
		tgtHi := quantileOf(ranked, 0.75, func(p TraitInput) float64 { return p.Parts.Targets })

		for _, p := range list {
			var set TraitSet
			// Only the contended range gets labelled. A trait on a player
			// nobody will roster is a label with nothing behind it.
			if !within(ranked, p.PlayerID) {
				out[p.PlayerID] = nil
				continue
			}

			if p.Parts.HasParts {
				// Low touchdown dependence is the floor, at every position:
				// the points come from yardage and catches, which repeat.
				// Pass catchers must also carry real reception volume — a
				// receiver who neither scores nor catches is not a floor, he
				// is just bad. Quarterbacks have no receptions to weigh, and
				// requiring them made every quarterback qualify.
				catches := pos == "RB" || pos == "WR" || pos == "TE"
				if tdShare(p) <= tdLo && (!catches || recShare(p) >= recHi) {
					set = append(set, TraitFloor)
				}
				if tdShare(p) >= tdHi {
					set = append(set, TraitRedZone)
				}
				if tgtHi > 0 && p.Parts.Targets >= tgtHi {
					set = append(set, TraitTargetHog)
				}
				if pos == "RB" && p.Parts.RushYards > 0 && p.Parts.Targets >= bellCowTargets {
					set = append(set, TraitBellCow)
				}
			}
			// Industry disagreement or a projection past his own record.
			//
			// The spread of his value across the valuation curves looked
			// like a third signal and was not: it scales with price, so it
			// flagged every player over $45 on the board and nothing under
			// $10. That is an expensiveness detector wearing the name of an
			// upside one. The curve spread is still shown per player as
			// swing$N, where it means what it says.
			if p.ECRUpside || p.unproven() {
				set = append(set, TraitUpside)
			}
			if p.Injury != "" {
				set = append(set, TraitDiscounted)
			}
			out[p.PlayerID] = set
		}
	}
	return out
}

// unproven reports whether the projection outruns the player's own record.
//
// Two ways to qualify, matching the two ways a player can lack support: no
// real sample at all, or a rate well past the one he has posted.
//
// A thin season is the first kind. Four good games is not evidence a full
// season will follow, so it counts as unproven however good those games
// were — that is what "without a solid history" means.
//
// For anyone with a real sample the comparison is per game rather than per
// season, because a season total punishes injury rather than measuring
// ambition: a back who missed half a year looks like a huge projected leap
// on totals and no leap at all on rate. Rate against rate asks the question
// that was meant.
func (p TraitInput) unproven() bool {
	if p.PriorGames < provenGames {
		return true
	}
	prior := p.PriorPoints / float64(p.PriorGames)
	if prior <= 0 {
		return true
	}
	return (p.Points/17)/prior >= unprovenLeap
}

func tdShare(p TraitInput) float64 {
	if p.Points <= 0 {
		return 0
	}
	return p.Parts.TouchdownPoints() / p.Points
}

func recShare(p TraitInput) float64 {
	if p.Points <= 0 {
		return 0
	}
	return p.Parts.ReceptionPoints() / p.Points
}

// quantileOf is the qth quantile of a measure over the ranked players.
func quantileOf(ranked []TraitInput, q float64, of func(TraitInput) float64) float64 {
	vals := make([]float64, 0, len(ranked))
	for _, p := range ranked {
		if p.Parts.HasParts || of(p) > 0 {
			vals = append(vals, of(p))
		}
	}
	if len(vals) == 0 {
		return 0
	}
	sort.Float64s(vals)
	i := int(q * float64(len(vals)-1))
	return vals[i]
}

func within(ranked []TraitInput, playerID string) bool {
	for _, p := range ranked {
		if p.PlayerID == playerID {
			return true
		}
	}
	return false
}

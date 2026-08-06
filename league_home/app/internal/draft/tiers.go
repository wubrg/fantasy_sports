package draft

import "sort"

// tierBreakFactor is how much larger than the typical gap a drop has to be
// before it separates two tiers.
//
// Explicit and adjustable rather than tuned, in the same spirit as the
// baseline depths: the shape it produces should be checked against the
// board rather than trusted. At 2 the breaks land where a human reading the
// projection list would put them — after Josh Allen, after the top three
// tight ends, at the edge of the flat receiver band in the forties.
const tierBreakFactor = 2.0

// tierWindowMultiple bounds the region tiers are read from, as a multiple
// of the starting depth.
//
// Beyond roughly twice the starting requirement every gap is noise between
// players nobody will start, and including that noise drags the typical gap
// down until ordinary drops start reading as tier breaks.
const tierWindowMultiple = 2

// ScarcityThresholds returns the projection a player must meet to count as
// startable at his position.
//
// The threshold is the median of the tier the last starting slot falls in,
// not the projection of the player sitting in that slot. Those differ, and
// the difference is the point.
//
// A single player is a knife edge: whoever happens to land at rank 42 sets
// the line, and he is one projection revision away from being someone else.
// A tier is the cluster he belongs to, and asking what the typical member of
// that cluster projects for is asking the real question — how good is the
// player you end up with in the last starting slot, and how many players
// are that good?
//
// Where the slot falls inside its tier is what makes the answer move. The
// last starting quarterback sits at the bottom of his tier, so the tier's
// median is well above him and only eight quarterbacks clear it against
// twelve slots. The last starting receiver sits in the middle of a long
// flat band, so forty-five receivers clear that band's median against
// forty-two slots. Same league, opposite conclusions, and a point estimate
// at the slot itself reports neither.
//
// Separate from ScoringBaselines on purpose. That one answers "how much
// better than a replacement starter is this lineup", where replacement is
// the last starter by definition. This one answers "how many players worth
// starting are left", which is a question about the shape of the position
// rather than about any one roster.
func ScarcityThresholds(allPlayers []Projection, shape PoolState) map[string]float64 {
	state := shape
	state.Baseline = BaselineVOLS
	state.Filled = nil
	depth := replacementDepth(state)

	byPos := map[string][]float64{}
	for _, p := range allPlayers {
		byPos[p.Position] = append(byPos[p.Position], p.Points)
	}

	out := map[string]float64{}
	for pos, points := range byPos {
		sort.Sort(sort.Reverse(sort.Float64Slice(points)))
		out[pos] = tierMedianAt(points, depth[pos])
	}
	return out
}

// tierMedianAt finds the tier containing the nth-ranked player and returns
// that tier's median projection. points must be sorted descending.
func tierMedianAt(points []float64, n int) float64 {
	if len(points) == 0 {
		return 0
	}
	if n <= 0 {
		return points[0]
	}
	if n > len(points) {
		// Demand outruns supply. Everyone available is startable by this
		// measure, and the count against it will say so.
		n = len(points)
	}
	idx := n - 1

	window := n * tierWindowMultiple
	if window > len(points) {
		window = len(points)
	}
	typical := medianGap(points[:window])
	if typical <= 0 {
		// A position with no separation anywhere: the slot itself is the
		// only line available.
		return points[idx]
	}
	breakAt := typical * tierBreakFactor

	// Walk outward from the slot until a drop big enough to separate tiers
	// is found in each direction. The tier is not clipped at the slot — a
	// cluster straddling the last starting spot is one cluster, and half of
	// it being on the bench does not make it two.
	lo := idx
	for lo > 0 && points[lo-1]-points[lo] < breakAt {
		lo--
	}
	hi := idx
	for hi+1 < window && points[hi]-points[hi+1] < breakAt {
		hi++
	}
	return median(points[lo : hi+1])
}

// medianGap is the typical drop between consecutive players, which sets the
// scale a tier break has to beat.
func medianGap(points []float64) float64 {
	if len(points) < 2 {
		return 0
	}
	gaps := make([]float64, 0, len(points)-1)
	for i := 0; i+1 < len(points); i++ {
		gaps = append(gaps, points[i]-points[i+1])
	}
	return median(gaps)
}

// median of a slice, which it may reorder.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

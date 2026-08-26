// Package calib scores probability forecasts against what happened.
//
// It exists because "is this forecast any good" decomposes into two questions
// that a single number cannot answer, and conflating them is how a worthless
// forecaster looks excellent:
//
//   - RELIABILITY. When it says 40%, does it happen 40% of the time? This is
//     what most people mean by calibration, and it is necessary.
//   - RESOLUTION. Does it say different things about different games, and are
//     the differences real? This is where an edge lives.
//
// A forecaster that always returns the base rate has perfect reliability and
// zero resolution. It is not worthless — it establishes the source is not
// misrepresenting its own uncertainty — but it cannot beat a market, and it is
// cheap to build: it requires knowing the base rate and nothing else. So both
// are reported, always, and a good Brier score is never allowed to stand in for
// the second.
//
// # What the wager actually needs
//
// Calibration statistics do not answer the question this project asks. A prop
// is +EV when
//
//	s_you − s_book  >  P_book × hold / (q − r)
//
// which is a claim about the MAGNITUDE of disagreement, in the right direction.
// MeanAbsDisagreement and InformedFraction answer it directly; nothing else
// here does.
//
// # Dependence
//
// A week's forecasts are not independent: both teams' outcomes come from the
// same game. Measured over 2,608 games, efficient_offense correlates +0.109
// within a game and blowout_loss −0.353. A naive interval overstates
// significance for the first and understates it for the second, so intervals
// come from a bootstrap that resamples whole GAMES.
package calib

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// Point is one scored forecast.
type Point struct {
	// P is the forecast probability; Y is whether the event occurred.
	P float64
	Y bool

	// Ref is the forecast this one is being measured against — the market's
	// number, the base rate, or the incumbent model. HasRef is false where no
	// reference exists, which is not the same as a reference of zero.
	Ref    float64
	HasRef bool

	// Abstained marks a forecast that declined to take a position. It is
	// DECLARED by the forecaster, not inferred from the number, because
	// whether it had a read is a property of the forecast rather than of how
	// close it landed to the reference.
	//
	// Abstentions must be scored apart. A source required to forecast every
	// game — so its scored set cannot be cherry-picked — will abstain on most
	// of them, and pooling those drags MeanAbsDisagreement toward zero and
	// hides a real edge on the rows where it committed.
	Abstained bool

	// Cluster is the unit of dependence: the game. Bootstrap resamples these,
	// not rows.
	Cluster string
}

func (p Point) y() float64 {
	if p.Y {
		return 1
	}
	return 0
}

// Report is everything worth knowing about a set of forecasts.
type Report struct {
	N         int
	Positions int // forecasts that took a position
	Abstained int
	Mean      float64 // mean forecast
	Base      float64 // realised rate
	Bias      float64 // Mean − Base

	// Brier and Murphy's decomposition: Brier = Reliability − Resolution +
	// Uncertainty. Reported together because either alone misleads.
	Brier       float64
	Reliability float64
	Resolution  float64
	Uncertainty float64
	Bins        int

	// Slope and Intercept from y ~ logit(p). Slope 1 with intercept 0 is
	// perfect; Slope below 1 means the forecasts are too extreme. Continuous,
	// because bucketing throws away the ordering and costs real power.
	Slope     float64
	Intercept float64
	SlopeSE   float64
	Converged bool

	// Discrimination, rank-based, no bucketing.
	AUC        float64
	Separation float64 // mean(P | occurred) − mean(P | not)

	// Against the reference.
	HasRef   bool
	RefN     int // rows where BOTH produce a number
	RefBrier float64
	Skill    float64 // 1 − Brier/RefBrier, on the common rows

	// The two the +EV requirement actually needs.
	MeanAbsDisagreement float64
	Bar                 float64
	OverBar             int
	InformedFraction    float64
}

var errNoPoints = errors.New("calib: no points to score")

// Score computes everything over the points that took a position.
//
// Abstentions are counted and excluded. Scoring them together would answer a
// different question than the one asked.
func Score(pts []Point, bar float64) (Report, error) {
	var r Report
	r.N = len(pts)
	if r.N == 0 {
		return r, errNoPoints
	}
	var live []Point
	for _, p := range pts {
		if err := validate(p); err != nil {
			return Report{}, err
		}
		if p.Abstained {
			r.Abstained++
			continue
		}
		live = append(live, p)
	}
	r.Positions = len(live)
	if r.Positions == 0 {
		return r, fmt.Errorf("calib: all %d forecasts abstained; there is nothing to score", r.N)
	}

	r.Mean = meanOf(live, func(p Point) float64 { return p.P })
	r.Base = meanOf(live, func(p Point) float64 { return p.y() })
	r.Bias = r.Mean - r.Base
	r.Brier = Brier(live)
	r.Bins = binCount(len(live))
	r.Reliability, r.Resolution, r.Uncertainty = Decompose(live, r.Bins)
	r.Slope, r.Intercept, r.SlopeSE, r.Converged = CalibrationSlope(live)
	r.AUC, _ = AUC(live)
	r.Separation = separation(live)
	r.Bar = bar

	ref := withRef(live)
	r.RefN = len(ref)
	if r.RefN > 0 {
		r.HasRef = true
		r.RefBrier = brierRef(ref)
		if r.RefBrier > 0 {
			r.Skill = 1 - Brier(ref)/r.RefBrier
		}
		var sum float64
		var over, informed int
		for _, p := range ref {
			d := p.P - p.Ref
			sum += math.Abs(d)
			if math.Abs(d) > bar {
				over++
				// "Informed" means it moved away from the reference in the
				// direction the outcome went. A big disagreement in the wrong
				// direction is worse than none.
				if (d > 0) == p.Y {
					informed++
				}
			}
		}
		r.MeanAbsDisagreement = sum / float64(len(ref))
		r.OverBar = over
		if over > 0 {
			r.InformedFraction = float64(informed) / float64(over)
		}
	}
	return r, nil
}

func validate(p Point) error {
	if math.IsNaN(p.P) || math.IsInf(p.P, 0) || p.P < 0 || p.P > 1 {
		return fmt.Errorf("calib: forecast %v is not a probability", p.P)
	}
	if p.HasRef && (math.IsNaN(p.Ref) || p.Ref < 0 || p.Ref > 1) {
		return fmt.Errorf("calib: reference %v is not a probability", p.Ref)
	}
	return nil
}

func meanOf(pts []Point, f func(Point) float64) float64 {
	var s float64
	for _, p := range pts {
		s += f(p)
	}
	return s / float64(len(pts))
}

// Positions drops abstentions.
//
// Exported because every statistic and every interval must be computed over the
// SAME population, and the population is positions. Mixing them produced a
// bootstrap interval that did not contain its own point estimate -- the
// estimate over 20 positions, the interval over 60 rows including 40
// abstentions sitting on the reference.
func Positions(pts []Point) []Point {
	out := make([]Point, 0, len(pts))
	for _, p := range pts {
		if !p.Abstained {
			out = append(out, p)
		}
	}
	return out
}

func withRef(pts []Point) []Point {
	var out []Point
	for _, p := range pts {
		if p.HasRef {
			out = append(out, p)
		}
	}
	return out
}

// Brier is the mean squared error of the forecast.
func Brier(pts []Point) float64 {
	if len(pts) == 0 {
		return math.NaN()
	}
	var s float64
	for _, p := range pts {
		d := p.P - p.y()
		s += d * d
	}
	return s / float64(len(pts))
}

func brierRef(pts []Point) float64 {
	var s float64
	for _, p := range pts {
		d := p.Ref - p.y()
		s += d * d
	}
	return s / float64(len(pts))
}

// binCount picks a bin count for the decomposition from the sample size.
//
// The decomposition is the ONLY thing here that needs bins, and its terms move
// with the choice — more bins flatter reliability. Derived from n rather than
// hardcoded so it cannot be tuned after seeing a result.
func binCount(n int) int {
	b := int(math.Round(math.Sqrt(float64(n)) / 2))
	if b < 3 {
		return 3
	}
	if b > 10 {
		return 10
	}
	return b
}

// Decompose splits the Brier score into Murphy's three terms:
//
//	Reliability − Resolution + Uncertainty
//
// Reliability is the penalty for saying 40% when it happens 60% of the time,
// and lower is better. Resolution is the credit for saying different things
// about different games, and HIGHER is better. Uncertainty is the base rate's
// own variance and belongs to the problem, not the forecaster.
//
// The identity is EXACT for the binned forecast, not the raw one. Murphy's
// decomposition assumes a forecast taking finitely many values; binning a
// continuous one introduces a residual equal to the within-bin scatter. So
// this returns the three terms and BinnedBrier reports what they sum to —
// comparing that against the raw Brier shows what the discretisation cost,
// rather than leaving a small unexplained gap for a reader to trip over.
func Decompose(pts []Point, bins int) (reliability, resolution, uncertainty float64) {
	n := len(pts)
	if n == 0 || bins < 1 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	ybar := meanOf(pts, func(p Point) float64 { return p.y() })
	uncertainty = ybar * (1 - ybar)

	type bin struct {
		n    int
		sumP float64
		sumY float64
	}
	bs := make([]bin, bins)
	for _, p := range pts {
		i := int(p.P * float64(bins))
		if i >= bins {
			i = bins - 1
		}
		bs[i].n++
		bs[i].sumP += p.P
		bs[i].sumY += p.y()
	}
	for _, b := range bs {
		if b.n == 0 {
			continue
		}
		pbar := b.sumP / float64(b.n)
		obar := b.sumY / float64(b.n)
		w := float64(b.n) / float64(n)
		reliability += w * (pbar - obar) * (pbar - obar)
		resolution += w * (obar - ybar) * (obar - ybar)
	}
	return reliability, resolution, uncertainty
}

// BinnedBrier is the Brier score of the binned forecast — the quantity
// Decompose's three terms sum to exactly.
//
// Its gap from the raw Brier is the discretisation residual. A large gap means
// the bins are too coarse to describe what the forecaster is doing.
func BinnedBrier(pts []Point, bins int) float64 {
	rel, res, unc := Decompose(pts, bins)
	return rel - res + unc
}

const (
	logitClamp    = 1e-6
	maxNewtonStep = 0.5
)

func logit(p float64) float64 {
	p = math.Min(math.Max(p, logitClamp), 1-logitClamp)
	return math.Log(p / (1 - p))
}

// CalibrationSlope fits y ~ sigmoid(a + b·logit(p)) by Newton-Raphson.
//
// b = 1 and a = 0 is a perfectly calibrated forecaster. b < 1 means it is too
// extreme — its confident calls are not as right as it thinks. b near 0 means
// its variation carries no information at all, which is the interesting failure:
// a forecaster can be well calibrated ON AVERAGE and still be ordering nothing.
//
// Continuous rather than bucketed. A median split discards the ordering and
// costs 5–12 points of power at the sample sizes available here, which is the
// difference between deciding in six weeks and deciding in eight.
func CalibrationSlope(pts []Point) (a, b, se float64, converged bool) {
	if len(pts) < 3 {
		return math.NaN(), math.NaN(), math.NaN(), false
	}
	x := make([]float64, len(pts))
	y := make([]float64, len(pts))
	for i, p := range pts {
		x[i] = logit(p.P)
		y[i] = p.y()
	}
	a, b = 0, 1
	var cov11 float64
	for iter := 0; iter < 200; iter++ {
		var g0, g1, h00, h01, h11 float64
		for i := range x {
			eta := a + b*x[i]
			mu := 1 / (1 + math.Exp(-eta))
			w := mu * (1 - mu)
			r := y[i] - mu
			g0 += r
			g1 += r * x[i]
			h00 += w
			h01 += w * x[i]
			h11 += w * x[i] * x[i]
		}
		det := h00*h11 - h01*h01
		if math.Abs(det) < 1e-12 {
			return a, b, math.NaN(), false
		}
		da := (h11*g0 - h01*g1) / det
		db := (h00*g1 - h01*g0) / det
		// Damped. Undamped Newton diverges when many forecasts pile against the
		// logit clamp -- which is exactly what an over-confident source does,
		// and therefore exactly the case this statistic exists to measure.
		if m := math.Max(math.Abs(da), math.Abs(db)); m > maxNewtonStep {
			da *= maxNewtonStep / m
			db *= maxNewtonStep / m
		}
		a += da
		b += db
		cov11 = h00 / det
		if math.Abs(da) < 1e-9 && math.Abs(db) < 1e-9 {
			if cov11 >= 0 {
				se = math.Sqrt(cov11)
			}
			return a, b, se, true
		}
	}
	if cov11 >= 0 {
		se = math.Sqrt(cov11)
	}
	return a, b, se, false
}

// AUC is the probability a randomly chosen occurrence was forecast higher than
// a randomly chosen non-occurrence. 0.5 is no discrimination.
//
// Rank-based (Mann-Whitney), with ties given half credit, so it needs no bins.
func AUC(pts []Point) (float64, error) {
	type pair struct {
		p float64
		y bool
	}
	all := make([]pair, 0, len(pts))
	var pos, neg int
	for _, p := range pts {
		all = append(all, pair{p.P, p.Y})
		if p.Y {
			pos++
		} else {
			neg++
		}
	}
	if pos == 0 || neg == 0 {
		return math.NaN(), fmt.Errorf(
			"calib: AUC needs both outcomes, got %d occurrences and %d non-occurrences", pos, neg)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].p < all[j].p })

	// Midranks, so ties do not bias the statistic.
	ranks := make([]float64, len(all))
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].p == all[i].p {
			j++
		}
		mid := (float64(i+1) + float64(j)) / 2
		for k := i; k < j; k++ {
			ranks[k] = mid
		}
		i = j
	}
	var sumPos float64
	for i, p := range all {
		if p.y {
			sumPos += ranks[i]
		}
	}
	u := sumPos - float64(pos)*float64(pos+1)/2
	return u / (float64(pos) * float64(neg)), nil
}

func separation(pts []Point) float64 {
	var sp, sn float64
	var np, nn int
	for _, p := range pts {
		if p.Y {
			sp += p.P
			np++
		} else {
			sn += p.P
			nn++
		}
	}
	if np == 0 || nn == 0 {
		return math.NaN()
	}
	return sp/float64(np) - sn/float64(nn)
}

// BootstrapCI resamples whole CLUSTERS with replacement.
//
// Resampling rows would treat two teams in the same game as independent draws.
// They are not, and the error runs both ways: for positively correlated
// scenarios a row bootstrap is too narrow, and for blowout_loss — where both
// teams cannot be blown out, correlation −0.353 — it is too wide.
//
// Points with an empty Cluster are each their own cluster.
func BootstrapCI(pts []Point, stat func([]Point) float64, iters int, seed int64, alpha float64) (lo, hi float64) {
	if len(pts) == 0 || iters < 2 {
		return math.NaN(), math.NaN()
	}
	groups := map[string][]Point{}
	var keys []string
	for i, p := range pts {
		k := p.Cluster
		if k == "" {
			k = fmt.Sprintf("\x00row-%d", i)
		}
		if _, seen := groups[k]; !seen {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], p)
	}
	rng := rand.New(rand.NewSource(seed))
	vals := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		sample := make([]Point, 0, len(pts))
		for range keys {
			sample = append(sample, groups[keys[rng.Intn(len(keys))]]...)
		}
		v := stat(sample)
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			vals = append(vals, v)
		}
	}
	if len(vals) < 2 {
		return math.NaN(), math.NaN()
	}
	sort.Float64s(vals)
	loIdx := int(alpha / 2 * float64(len(vals)))
	hiIdx := int((1 - alpha/2) * float64(len(vals)))
	if hiIdx >= len(vals) {
		hiIdx = len(vals) - 1
	}
	return vals[loIdx], vals[hiIdx]
}

// RealisedEdge is what the wagers this forecast would actually place returned,
// per unit staked, against the price the reference implies.
//
// This is the money question, and no calibration statistic answers it. A
// forecaster can be more accurate than the reference everywhere by a hair --
// a healthy paired Brier gain -- and never disagree by enough to place a single
// bet. The two endpoints are different claims and both have to hold.
//
// Only rows over the bar count, because those are the only ones that become
// wagers. The side taken follows the direction of the disagreement:
//
//	p > ref   bet the scenario happens;     breakeven ref*(1+hold),      realised y
//	p < ref   bet it does not;              breakeven (1-ref)*(1+hold),  realised 1-y
//
// Positive means the wagers won more than the price required.
func RealisedEdge(pts []Point, bar, hold float64) float64 {
	var sum float64
	var n int
	for _, p := range Positions(pts) {
		if !p.HasRef {
			continue
		}
		d := p.P - p.Ref
		if math.Abs(d) <= bar {
			continue
		}
		var breakeven, realised float64
		if d > 0 {
			breakeven, realised = p.Ref*(1+hold), p.y()
		} else {
			breakeven, realised = (1-p.Ref)*(1+hold), 1-p.y()
		}
		sum += realised - breakeven
		n++
	}
	if n == 0 {
		return math.NaN()
	}
	return sum / float64(n)
}

// OverBarCount is how many rows the realised-edge statistic actually rests on.
//
// Reported beside it because it is a small fraction of the sample: the bar is
// what makes a row a wager, and a forecaster that abstains freely -- as the
// contract asks it to -- will put few rows over it.
func OverBarCount(pts []Point, bar float64) int {
	var n int
	for _, p := range Positions(pts) {
		if p.HasRef && math.Abs(p.P-p.Ref) > bar {
			n++
		}
	}
	return n
}

// PairedBrierGain is how much better the forecast is than its reference, per
// row. Positive means the forecast beat the reference.
//
// This is the pre-registered primary endpoint: pooled, one-sided, at a declared
// week. Everything else in Report is descriptive, and saying so in advance is
// what stops four scenarios times several statistics times eighteen weekly
// looks from manufacturing a winner.
func PairedBrierGain(pts []Point) float64 {
	ref := withRef(Positions(pts))
	if len(ref) == 0 {
		return math.NaN()
	}
	var s float64
	for _, p := range ref {
		dp := p.P - p.y()
		dr := p.Ref - p.y()
		s += dr*dr - dp*dp
	}
	return s / float64(len(ref))
}

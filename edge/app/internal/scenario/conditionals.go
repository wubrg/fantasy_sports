package scenario

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"edge/internal/wager"
)

// conditionalsJSON is fitted by edge/model/analysis/fit_conditionals.py.
//
//go:embed artifacts/conditionals.json
var conditionalsJSON []byte

// Cell is one pooled estimate: the distribution of receiving yards for
// player-games matching an opportunity band, a role-trend band, and whether a
// game script occurred.
//
// Quantiles are stored rather than a single probability so P(yards > L) can be
// answered at any line, instead of only the one the fit happened to pick.
type Cell struct {
	Outcome  string `json:"outcome"`
	Scenario string `json:"scenario"`
	Occurred bool   `json:"occurred"`
	// The opportunity axis, whose meaning depends on the outcome: projected
	// targets for a pass-catcher, projected attempts for a quarterback. Was
	// targets_min/max, which named one outcome's axis as though it were the
	// only one.
	OpportunityMin float64     `json:"opportunity_min"`
	OpportunityMax float64     `json:"opportunity_max"`
	TrendMin       float64     `json:"trend_min"`
	TrendMax       float64     `json:"trend_max"`
	N              int         `json:"n"`
	Median         float64     `json:"median"`
	Quantiles      [][]float64 `json:"quantiles"` // [[probability, yards], ...]

	// NEff is N discounted for repeat players. A cell pools many games from the
	// same player and those rows are not independent, so the raw count claims
	// more precision than the data supports. Fitting measures the design effect
	// per cell (ANOVA ICC over players) and records the result here; intervals
	// are built on NEff, not N.
	NEff    float64 `json:"n_eff"`
	Players int     `json:"players"`
	ICC     float64 `json:"icc"`
}

// effectiveN is the sample size intervals should be built on.
//
// Falls back to N for artifacts predating the n_eff field rather than treating
// a missing value as zero, which would make every interval maximally wide.
func (c Cell) effectiveN() int {
	if c.NEff <= 0 {
		return c.N
	}
	if n := int(c.NEff + 0.5); n >= 1 {
		return n
	}
	return 1
}

// ScenarioStatus says whether a fitted scenario is fit to bet on.
//
// Cells are emitted for unvalidated scenarios too -- the fit stays reproducible
// and the data remains available for the work that would validate them -- but
// pricing a wager from one is refused. A scenario that is measurable is not the
// same as a scenario that is usable.
type ScenarioStatus struct {
	Validated bool   `json:"validated"`
	Note      string `json:"note"`
}

// Definition is what a scenario name MEANS: the quantity it tests, the
// direction, and the threshold.
//
// It is recorded because the two halves of the decomposition get their
// definition from different places. q and r are fitted once, against a fixed
// condition; s is derived per query from whatever -threshold the caller passed.
// Nothing tied them together, so `-name shootout -threshold 65` produced
// s = P(total > 65) blended against a q measured on total > 50 -- a
// well-formed, confident number that is not a probability of anything, printed
// under a header reading "shootout (total > 65.0)" directly above cells meaning
// something else.
type Definition struct {
	Basis     string  `json:"basis"` // "total" or "margin"
	Op        string  `json:"op"`    // ">" or "<"
	Threshold float64 `json:"threshold"`
}

func (d Definition) String() string {
	return fmt.Sprintf("%s %s %.1f", d.Basis, d.Op, d.Threshold)
}

// OutcomeDef is what an outcome predicts and the opportunity axis it is
// conditioned on. The axis is not interchangeable: a pass-catcher's
// opportunity is a share of a fixed team pool, a quarterback's is his own
// attempt volume, and reading one through the other's bands is meaningless.
type OutcomeDef struct {
	YardsField  string   `json:"yards_field"`
	Opportunity string   `json:"opportunity"`
	ShareBased  bool     `json:"share_based"`
	Positions   []string `json:"positions"`
	MinBaseline float64  `json:"min_baseline"`
}

// Conditionals is the whole fitted grid.
type Conditionals struct {
	GeneratedAt string                `json:"generated_at"`
	GeneratedBy string                `json:"generated_by"`
	Outcomes    map[string]OutcomeDef `json:"outcomes"`
	Seasons     []int                 `json:"seasons"`
	MinCell     int                   `json:"min_cell"`
	Definitions map[string]Definition `json:"scenario_definitions"`
	// Keyed by outcome, then scenario. A scenario that separates receiving
	// yards need not separate passing yards, so each pairing carries its own
	// verdict.
	ScenarioStatus map[string]map[string]ScenarioStatus `json:"scenario_status"`
	Cells          []Cell                               `json:"cells"`
}

// ErrDefinitionMismatch marks a query whose scenario threshold disagrees with
// the one the grid was fitted against.
var ErrDefinitionMismatch = errors.New("scenario definition mismatch")

// CheckDefinition refuses a query that would blend q and r against a different
// event than the one they measure.
//
// An artifact with no recorded definitions fails closed. It predates this check
// and cannot be verified, and the failure mode it guards against is silent.
func (c *Conditionals) CheckDefinition(outcome, scenario, basis string, threshold float64) error {
	// Validation first. An unvalidated scenario cannot be priced on any basis,
	// so reporting a threshold mismatch would name the wrong problem and send
	// the caller off to fix a flag that was never the obstacle.
	if err := c.checkValidated(outcome, scenario); err != nil {
		return err
	}
	def, ok := c.Definitions[scenario]
	if !ok {
		return fmt.Errorf(
			"scenario: this grid records no definition for %q, so the threshold you asked "+
				"for cannot be checked against the one q and r were fitted on. Refit with a "+
				"current fit_conditionals.py: %w", scenario, ErrDefinitionMismatch)
	}
	if def.Basis != basis {
		return fmt.Errorf(
			"scenario: %q is fitted on the game %s, but you asked for it on the %s. "+
				"q and r would describe a different event from s: %w",
			scenario, def.Basis, basis, ErrDefinitionMismatch)
	}
	if def.Threshold != threshold {
		return fmt.Errorf(
			"scenario: %q is fitted as %s, but you asked for a threshold of %.1f.\n"+
				"  s would be P(%s %s %.1f) while q and r measure %s -- blending them is not "+
				"a probability of anything.\n"+
				"  Use -threshold %.1f, or supply -q and -r for the line you actually mean: %w",
			scenario, def, threshold, basis, def.Op, threshold, def,
			def.Threshold, ErrDefinitionMismatch)
	}
	return nil
}

// ErrScenarioNotPriceable marks the errors that mean "this SCENARIO cannot be
// priced" -- unknown, or fitted but not validated.
//
// It exists so callers can tell that case apart from the others Lookup
// returns. Listing the priceable scenarios is helpful when the scenario is the
// problem and actively misleading when it is not: a line outside the observed
// range fails on a scenario that IS priceable, and answering it with "scenarios
// you can price: shootout" points at the one thing that was already correct.
var ErrScenarioNotPriceable = errors.New("scenario cannot be priced")

// checkValidated refuses to price a wager from a scenario that failed
// validation.
//
// An unknown scenario is also refused rather than assumed good: a typo must not
// silently inherit the benefit of the doubt.
func (c *Conditionals) checkValidated(outcome, scenario string) error {
	byScenario, ok := c.ScenarioStatus[outcome]
	if !ok {
		return fmt.Errorf(
			"scenario: this grid does not fit %q (fitted outcomes: %s): %w",
			outcome, strings.Join(c.OutcomeNames(), ", "), ErrScenarioNotPriceable)
	}
	st, ok := byScenario[scenario]
	if !ok {
		return fmt.Errorf(
			"scenario: %q has no recorded validation status for %s, so it cannot be "+
				"priced (fitted scenarios: %s): %w",
			scenario, outcome, strings.Join(c.ScenarioNames(), ", "), ErrScenarioNotPriceable)
	}
	if !st.Validated {
		return fmt.Errorf(
			"scenario: %q is fitted but NOT validated for %s, so it cannot be priced.\n  %s: %w",
			scenario, outcome, st.Note, ErrScenarioNotPriceable)
	}
	return nil
}

// OutcomeNames lists the outcomes this grid fits.
func (c *Conditionals) OutcomeNames() []string {
	out := make([]string, 0, len(c.Outcomes))
	for k := range c.Outcomes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidatedScenarioNames lists only the scenarios that may be priced.
func (c *Conditionals) ValidatedScenarioNames(outcome string) []string {
	var out []string
	for _, name := range c.ScenarioNames() {
		if st, ok := c.ScenarioStatus[outcome][name]; ok && st.Validated {
			out = append(out, name)
		}
	}
	return out
}

var (
	condOnce sync.Once
	cond     *Conditionals
	condErr  error
)

// LoadConditionals parses the embedded grid once.
func LoadConditionals() (*Conditionals, error) {
	condOnce.Do(func() {
		var c Conditionals
		if err := json.Unmarshal(conditionalsJSON, &c); err != nil {
			condErr = fmt.Errorf("scenario: parsing embedded conditionals: %w", err)
			return
		}
		if len(c.Cells) == 0 {
			condErr = fmt.Errorf("scenario: conditionals artifact has no cells")
			return
		}
		for i, cell := range c.Cells {
			if len(cell.Quantiles) < 2 {
				condErr = fmt.Errorf("scenario: cell %d has %d quantile points",
					i, len(cell.Quantiles))
				return
			}
			if cell.N <= 0 {
				condErr = fmt.Errorf("scenario: cell %d reports n=%d", i, cell.N)
				return
			}
			for j, q := range cell.Quantiles {
				if len(q) != 2 {
					condErr = fmt.Errorf("scenario: cell %d quantile %d has %d fields, want 2",
						i, j, len(q))
					return
				}
			}
		}
		cond = &c
	})
	return cond, condErr
}

// Conditional is a looked-up probability with the uncertainty its cell
// supports.
//
// The interval is not decoration. A cell of 120 observations and a cell of
// 3,000 produce very different confidence in the same point estimate, and the
// whole reason for pooling was to stop pretending a thin sample is a
// probability.
type Conditional struct {
	Prob  float64
	Lower float64
	Upper float64

	// N is the raw cell count; NEff is what the interval was actually built on
	// after discounting repeat players. Both are reported so the discount is
	// visible rather than buried.
	N          int
	NEff       int
	CellMedian float64
	Cell       Cell

	// TailN is the effective observations behind the SPARSER side of the
	// estimate: min(p, 1-p) x NEff.
	//
	// It exists because the interval cannot carry this. At a deep line p is
	// small, and Wilson on a small p is ABSOLUTELY narrow while being
	// relatively enormous -- so a q of 2.3% resting on seven observations
	// prints a TIGHTER interval than a q of 25.2% resting on ninety-four, and
	// reads as the more precise of the two. It is the opposite.
	//
	// The sparser side is the one that matters regardless of direction: an
	// estimate near 1 is as thinly evidenced as one near 0, just mirrored.
	TailN float64
}

// MinTailN is the effective observations below which an estimate is reported
// as thin.
//
// Ten rather than the conventional five: five is the floor for a distributional
// test to be valid at all, and this number is being used to price a wager.
const MinTailN = 10

// Thin reports whether too little of the sample sits on the side of the line
// being bet for the estimate to carry its printed precision.
func (c Conditional) Thin() bool { return c.TailN < MinTailN }

// probAbove returns P(yards > line) from a cell's quantile table.
//
// The table maps probability to yards, so this inverts it: find where the
// yards curve crosses the line and read off the probability. Ties are common
// at the low end -- plenty of player-games produce zero yards -- so the search
// takes the LAST quantile at or below the line, which keeps a run of equal
// values from collapsing the estimate.
func probAbove(quantiles [][]float64, line float64) float64 {
	n := len(quantiles)
	if line < quantiles[0][1] {
		return 1
	}
	if line >= quantiles[n-1][1] {
		return 0
	}
	last := 0
	for i := 0; i < n; i++ {
		if quantiles[i][1] <= line {
			last = i
		}
	}
	if last == n-1 {
		return 0
	}
	p0, y0 := quantiles[last][0], quantiles[last][1]
	p1, y1 := quantiles[last+1][0], quantiles[last+1][1]
	var cdf float64
	if y1 == y0 {
		cdf = p1
	} else {
		cdf = p0 + (p1-p0)*(line-y0)/(y1-y0)
	}
	return 1 - cdf
}

// outsideSupport reports whether a line falls beyond anything the cell ever
// observed -- the case where probAbove returns exactly 0 or 1 because the
// quantile table has run out, rather than because the sample says so.
//
// This is not a thin estimate. It is not an estimate: the answer is "no
// player-game in this cell ever reached that line", which clampToSupport then
// converts into a small non-zero probability so the interval stays sane. That
// clamp is right for arithmetic and wrong to report, because everything
// downstream treats the result as measured -- s* divides by (q - r), and two
// clamped endpoints produce a confident verdict with a sensitivity in the
// thousands of points per point.
func outsideSupport(quantiles [][]float64, line float64) bool {
	return line < quantiles[0][1] || line >= quantiles[len(quantiles)-1][1]
}

// findCell locates the grid cell matching an opportunity level and role trend.
func (c *Conditionals) findCell(outcome, scenario string, occurred bool, opportunity, trend float64) (Cell, error) {
	for _, cell := range c.Cells {
		if cell.Outcome != outcome || cell.Scenario != scenario || cell.Occurred != occurred {
			continue
		}
		if opportunity < cell.OpportunityMin || opportunity >= cell.OpportunityMax {
			continue
		}
		if trend < cell.TrendMin || trend >= cell.TrendMax {
			continue
		}
		return cell, nil
	}
	axis := "projected opportunity"
	if def, ok := c.Outcomes[outcome]; ok {
		axis = "projected " + def.Opportunity
	}
	return Cell{}, fmt.Errorf(
		"scenario: no fitted cell for %s/%s occurred=%v at %.1f %s and %+.3f trend "+
			"(cells thinner than %d observations are not published)",
		outcome, scenario, occurred, opportunity, axis, trend, c.MinCell)
}

// Lookup returns P(yards > line) for one side of a scenario.
func (c *Conditionals) Lookup(outcome, scenario string, occurred bool, opportunity, trend, line, confidence float64) (Conditional, error) {
	if err := c.checkValidated(outcome, scenario); err != nil {
		return Conditional{}, err
	}
	for name, v := range map[string]float64{"opportunity": opportunity, "trend": trend, "line": line, "confidence": confidence} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Conditional{}, fmt.Errorf("scenario: %s = %v is not a real number", name, v)
		}
	}
	cell, err := c.findCell(outcome, scenario, occurred, opportunity, trend)
	if err != nil {
		return Conditional{}, err
	}

	n := cell.effectiveN()

	// Refuse before clamping. A line past the observed range is not a small
	// probability, it is an absence of evidence, and clampToSupport is about to
	// make the two indistinguishable.
	if outsideSupport(cell.Quantiles, line) {
		lo, hi := cell.Quantiles[0][1], cell.Quantiles[len(cell.Quantiles)-1][1]
		return Conditional{}, fmt.Errorf(
			"scenario: a line of %.1f is outside what %s/%s occurred=%v at %.1f opportunity "+
				"ever produced (%.0f to %.0f over %d games); the grid cannot price it.\n"+
				"  Supply -q and -r yourself if you have a read on a line this far out",
			line, outcome, scenario, occurred, opportunity, lo, hi, cell.N)
	}

	p := clampToSupport(probAbove(cell.Quantiles, line), n)

	// Reuse the interval the hit-rate layer already uses, so a pooled estimate
	// and an empirical one report uncertainty the same way. Built on the
	// effective sample size, so repeat players do not buy false precision.
	hits := clampHits(p, n)
	lower, upper, err := wager.WilsonInterval(hits, n, confidence)
	if err != nil {
		return Conditional{}, err
	}
	return Conditional{
		Prob: p, Lower: lower, Upper: upper,
		N: cell.N, NEff: n, CellMedian: cell.Median, Cell: cell,
		TailN: math.Min(p, 1-p) * float64(n),
	}, nil
}

// clampToSupport stops a finite sample from claiming certainty.
//
// The quantile table's endpoints are the smallest and largest values actually
// observed, so a line outside that range makes probAbove return exactly 0 or 1.
// That reads as impossibility or certainty, which no sample of n can establish
// -- and it corrupts the interval too, since hits then equals n and Wilson
// reports something like [0.987, 1.000] on a cell where 2% of observations
// disagree.
//
// Half an observation is the most precision a sample of n supports, so the
// estimate is bounded to [1/(2n), 1 - 1/(2n)]. Mid-range values are untouched;
// only the endpoints move.
// clampHits converts a probability to a success count for the interval, keeping
// the strictness clampToSupport just established.
//
// Rounding to nearest silently undoes the clamp. At the upper bound
// p = 1 - 1/(2n), so p*n + 0.5 is EXACTLY n and int() returns n -- handing back
// the half observation the clamp bought and putting Wilson right back at an
// upper bound of 1.0000. That defect shipped once already, described in the
// commit message as fixed, because the test checked the probability and never
// looked at the interval.
//
// The count therefore carries the same invariant as the probability: strictly
// inside (0, n) whenever n allows it.
func clampHits(p float64, n int) int {
	hits := int(p*float64(n) + 0.5)
	if n < 2 {
		return hits
	}
	if hits >= n {
		hits = n - 1
	}
	if hits < 1 {
		hits = 1
	}
	return hits
}

func clampToSupport(p float64, n int) float64 {
	if n < 1 {
		return p
	}
	lo := 1 / (2 * float64(n))
	if p < lo {
		return lo
	}
	if p > 1-lo {
		return 1 - lo
	}
	return p
}

// QR returns the conditional probabilities the belief decomposition needs:
// q with the scenario, r without it.
//
// Both come from the same grid at the same opportunity and trend, so the only
// thing that differs between them is the game script -- which is precisely what
// the decomposition assumes.
func (c *Conditionals) QR(outcome, scenario string, opportunity, trend, line, confidence float64) (q, r Conditional, err error) {
	q, err = c.Lookup(outcome, scenario, true, opportunity, trend, line, confidence)
	if err != nil {
		return Conditional{}, Conditional{}, err
	}
	r, err = c.Lookup(outcome, scenario, false, opportunity, trend, line, confidence)
	if err != nil {
		return Conditional{}, Conditional{}, err
	}
	return q, r, nil
}

// ScenarioNames lists the scenarios the fitted grid covers.
func (c *Conditionals) ScenarioNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, cell := range c.Cells {
		if !seen[cell.Scenario] {
			seen[cell.Scenario] = true
			out = append(out, cell.Scenario)
		}
	}
	return out
}

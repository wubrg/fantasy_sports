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
	// The three conditioning axes. Projected opportunity used to be the first
	// of them; it was dropped when the fitted value became a ratio to the
	// player's own baseline, which carries most of what it stood in for at a
	// third of the density cost.
	//
	// PostedMin/Max is the total the MARKET posted, which `s` is derived from.
	// Pooling q and r across it while deriving s from it was finding C1.
	// An outcome whose population cannot fill the split publishes one band
	// covering everything, which is the same grid with the axis switched off.
	PostedMin   float64 `json:"posted_min"`
	PostedMax   float64 `json:"posted_max"`
	BaselineMin float64 `json:"baseline_min"`
	BaselineMax float64 `json:"baseline_max"`
	TrendMin    float64 `json:"trend_min"`
	TrendMax    float64 `json:"trend_max"`
	N           int     `json:"n"`

	// Median is a RATIO to the player's own baseline: 1.0 is a typical game
	// for him. MedianOutput is the same cell in the units a person reads.
	Median       float64     `json:"median"`
	MedianOutput float64     `json:"median_output"`
	Quantiles    [][]float64 `json:"quantiles"` // [[probability, ratio], ...]

	// NEff is N discounted for repeat players. A cell pools many games from the
	// same player and those rows are not independent, so the raw count claims
	// more precision than the data supports. Fitting measures the design effect
	// per cell (ANOVA ICC over players) and records the result here; intervals
	// are built on NEff, not N.
	NEff    float64 `json:"n_eff"`
	Players int     `json:"players"`
	ICC     float64 `json:"icc"`

	// Validated is THE gate, and it is per cell because a price is per cell.
	//
	// It used to live on the scenario: one verdict covering every cell the
	// scenario had, decided by whether the effect's direction held in ALL of
	// them. That rule passes with probability (1-e)^k for k cells, so it grew
	// stricter the more finely the same data was cut -- and cell count is a
	// design choice, not evidence. Judging each site on its own evidence
	// removes k from the expression.
	//
	// Both halves of a site (occurred and not) carry the same flag. q and r
	// only mean something together, so half a validated pair is not usable.
	Validated bool     `json:"validated"`
	Why       []string `json:"why"`

	// Override is set when an operator accepted THIS site's specific failure.
	Override *AcceptedFailure `json:"override,omitempty"`

	// Stability is the share of MIN_CELL x OOS_SPLIT settings under which this
	// site reaches the same verdict -- 1.0 for a verdict about the data, less
	// for one the two constants can move.
	//
	// It exists because those constants were chosen after the rule they feed,
	// and nothing said so. A verdict that holds at MIN_CELL 100 and fails at
	// 150 is a fact about 100. Reported at the point of pricing rather than in
	// a sweep nobody runs.
	Stability *float64 `json:"stability"`
}

// Firm reports whether every swept knob setting agrees with this cell's
// verdict. Anything less is priceable but worth saying out loud.
func (c Cell) Firm() bool {
	return c.Stability != nil && *c.Stability >= 1.0
}

// StabilityNote describes the verdict's dependence on the two constants, or ""
// when it does not depend on them at all.
func (c Cell) StabilityNote() string {
	if c.Stability == nil {
		return "verdict stability was not measured for this cell"
	}
	if *c.Stability >= 1.0 {
		return ""
	}
	return fmt.Sprintf(
		"this verdict holds at %.0f%% of the swept MIN_CELL x OOS_SPLIT settings, not all "+
			"of them — it depends partly on where those two constants were set",
		*c.Stability*100)
}

// SiteLabel names the cell's coordinates the way a person reads them, for use
// in a refusal. A reason without a location sends the reader hunting.
func (c Cell) SiteLabel() string {
	return fmt.Sprintf("posted %g-%g, baseline %g-%g, trend %+.2f..%+.2f",
		c.PostedMin, c.PostedMax, c.BaselineMin, c.BaselineMax,
		c.TrendMin, c.TrendMax)
}

// Site is where a wager sits in the grid: the three coordinates a cell is
// keyed on. Passed as one value rather than three bare floats, because three
// adjacent float64 arguments is an invitation to transpose two of them.
type Site struct {
	// Posted is the total the market posted for the game.
	Posted float64
	// Baseline is what this player normally does -- his own prior mean output.
	// It is what the book sets its line near, and the line is read against it
	// as a ratio.
	Baseline float64
	// Trend is his role trend, in the outcome's own units.
	Trend float64
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
	// Validated now means "at least one site of this pairing may be priced".
	// The per-cell flags say which. A pairing with no priceable site is as
	// unbettable as a scenario that failed the old whole-scenario gate, so the
	// name still carries its original meaning at this granularity.
	Validated bool   `json:"validated"`
	Note      string `json:"note"`

	Sites          int `json:"sites"`
	SitesPriceable int `json:"sites_priceable"`

	// RuleSays is what the old whole-scenario rule computed. Kept because
	// FINDINGS.md argues from it and the two verdicts should stay comparable.
	RuleSays bool `json:"rule_says"`

	// SignP is the two-sided binomial p-value that the scenario's dominant
	// direction beats a coin flip. A site is judged by agreement with that
	// direction, so when it is not established no site can be priced.
	SignP float64 `json:"sign_p"`

	// Vetoed is an operator removing the pairing for a reason no test can
	// see -- the effect is real and still not something to bet on. It only
	// ever subtracts.
	Vetoed bool   `json:"vetoed,omitempty"`
	Why    string `json:"why,omitempty"`
}

// AcceptedFailure is an operator overriding the gate for one named cell.
//
// It exists so that the alternative -- softening the rule -- stays off the
// table. A rule that bends to admit a scenario stops discriminating for every
// scenario after it; a recorded exception costs only the scenario it names, and
// travels with the artifact so it can be argued with later.
//
// The cost is that it must never be quiet. Anything priced on an accepted
// failure says so at the point of use, not only in the fit log.
type AcceptedFailure struct {
	Cell       string `json:"cell"`
	Measured   string `json:"measured"`
	Why        string `json:"why"`
	AcceptedBy string `json:"accepted_by"`

	// The failing cell's bounds, so a caller can be told whether its own
	// wager sits inside it. A warning that has to be cross-referenced by hand
	// is a warning that gets skipped.
	PostedMin   float64 `json:"posted_min"`
	PostedMax   float64 `json:"posted_max"`
	BaselineMin float64 `json:"baseline_min"`
	BaselineMax float64 `json:"baseline_max"`
	TrendMin    float64 `json:"trend_min"`
	TrendMax    float64 `json:"trend_max"`
}

// Covers reports whether a wager falls inside the failing cell.
func (a AcceptedFailure) Covers(at Site) bool {
	return at.Posted >= a.PostedMin && at.Posted < a.PostedMax &&
		at.Baseline >= a.BaselineMin && at.Baseline < a.BaselineMax &&
		at.Trend >= a.TrendMin && at.Trend < a.TrendMax
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
	// %g, not %.1f. A success-rate threshold of 0.46 printed as "0.5", so the
	// mismatch error below said "fitted as success_rate > 0.5, but you asked
	// for 0.5" and told the caller to use 0.5 -- which can never match. The
	// scenario was unreachable through the CLI by way of its own diagnostic
	// rounding away the one number it exists to communicate.
	return fmt.Sprintf("%s %s %g", d.Basis, d.Op, d.Threshold)
}

// OutcomeDef is what an outcome predicts and the opportunity axis it is
// conditioned on. The axis is not interchangeable: a pass-catcher's
// opportunity is a share of a fixed team pool, a quarterback's is his own
// attempt volume, and reading one through the other's bands is meaningless.
type OutcomeDef struct {
	YardsField  string `json:"yards_field"`
	Opportunity string `json:"opportunity"`
	ShareBased  bool   `json:"share_based"`
	// Discrete says the OUTCOME is a count. It no longer changes how a cell is
	// read: the grid stores ratios to the player's own baseline, which are
	// continuous even for counts. It survives because the unit and the way a
	// median is summarised still depend on it.
	Discrete    bool     `json:"discrete"`
	Unit        string   `json:"unit"`
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
func (c *Conditionals) CheckDefinition(outcome, scenario, basis string, threshold float64, below bool) error {
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
	// The operator is checked too, not just the quantity and the level. Its
	// absence here is what let blowout_loss price on the complement of its own
	// probability: basis matched, threshold matched, and "<" against ">" was
	// never compared.
	if (def.Op == "<") != below {
		want := "above"
		if def.Op == "<" {
			want = "below"
		}
		return fmt.Errorf(
			"scenario: %q occurs %s its threshold, but you asked for the other "+
				"direction. s would be the complement of what q and r measure: %w",
			scenario, want, ErrDefinitionMismatch)
	}
	if def.Threshold != threshold {
		return fmt.Errorf(
			"scenario: %q is fitted as %s, but you asked for a threshold of %g.\n"+
				"  s would be P(%s %s %g) while q and r measure %s -- blending them is not "+
				"a probability of anything.\n"+
				"  Use -threshold %g, or supply -q and -r for the line you actually mean: %w",
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
		if st.Vetoed {
			return fmt.Errorf(
				"scenario: %q is fitted for %s and vetoed by the operator, so it cannot be "+
					"priced at any cell.\n  %s\n  %w",
				scenario, outcome, st.Why, ErrScenarioNotPriceable)
		}
		return fmt.Errorf(
			"scenario: %q is fitted for %s but no cell of it survives validation, so it "+
				"cannot be priced.\n  %s\n  0 of %d sites priceable; the scenario's direction "+
				"beats a coin flip with p=%.3f: %w",
			scenario, outcome, st.Note, st.Sites, st.SignP, ErrScenarioNotPriceable)
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

// OccursBelow reports whether a scenario happens below its threshold, so a
// caller can derive its probability in the right direction.
func (c *Conditionals) OccursBelow(scenario string) bool {
	return c.Definitions[scenario].Op == "<"
}

// AcceptedFailureFor returns the override a scenario is being priced under, if
// any. Callers must surface it; a wager placed on an overridden gate should
// never look like one placed on a clean pass.
func (c *Conditionals) AcceptedFailureFor(outcome, scenario string, at Site) *AcceptedFailure {
	for i := range c.Cells {
		cell := &c.Cells[i]
		if cell.Outcome == outcome && cell.Scenario == scenario && cell.Override != nil &&
			cell.Override.Covers(at) {
			return cell.Override
		}
	}
	return nil
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

// SparseTailN is where "thinly evidenced" stops and "measured" begins.
//
// A single threshold read as a promise it could not keep: 13 effective
// observations printed MEASURED alongside 400, and the relative standard error
// on a tail count k is about 1/sqrt(k) -- 28% at 13, and 5% at 400. Those are
// not the same claim and should not carry the same word. At 30 the relative
// error is ~18%, which is the point at which the interval printed beside it
// starts to be the binding constraint rather than the count.
const SparseTailN = 30

// Thin reports whether too little of the sample sits on the side of the line
// being bet for the estimate to carry its printed precision.
func (c Conditional) Thin() bool { return c.TailN < MinTailN }

// Sparse reports an estimate that clears the thin floor but still rests on few
// enough observations that its relative error is above ~18%.
func (c Conditional) Sparse() bool {
	return c.TailN >= MinTailN && c.TailN < SparseTailN
}

// RelativeError is the approximate relative standard error of the estimate,
// 1/sqrt(k) on the sparser side's effective count. Reported rather than left
// for the reader to infer from a label.
func (c Conditional) RelativeError() float64 {
	if c.TailN <= 0 {
		return math.Inf(1)
	}
	return 1 / math.Sqrt(c.TailN)
}

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
func (c *Conditionals) findCell(outcome, scenario string, occurred bool, at Site) (Cell, error) {
	for _, cell := range c.Cells {
		if cell.Outcome != outcome || cell.Scenario != scenario || cell.Occurred != occurred {
			continue
		}
		if at.Posted < cell.PostedMin || at.Posted >= cell.PostedMax {
			continue
		}
		if at.Baseline < cell.BaselineMin || at.Baseline >= cell.BaselineMax {
			continue
		}
		if at.Trend < cell.TrendMin || at.Trend >= cell.TrendMax {
			continue
		}
		return cell, nil
	}
	unit := "yds"
	if def, ok := c.Outcomes[outcome]; ok && def.Unit != "" {
		unit = def.Unit
	}
	return Cell{}, fmt.Errorf(
		"scenario: no fitted cell for %s/%s occurred=%v at a posted total of %.1f, "+
			"a %.1f %s baseline and %+.3f trend "+
			"(cells thinner than %d observations are not published)",
		outcome, scenario, occurred, at.Posted, at.Baseline, unit, at.Trend, c.MinCell)
}

// Lookup returns P(output > line) for one side of a scenario.
//
// The grid stores ratios to the player's own baseline, so the line is divided
// by that baseline before it is read off. This is the whole point of the
// change: a book sets its line near THIS player's median, and a grid holding
// raw yards answered "what does the cohort do at this line" when the question
// was "what does he do". Measured, that mismatch reached 8pp at the top tier
// against a 2.38pp vig cushion.
func (c *Conditionals) Lookup(outcome, scenario string, occurred bool, at Site, line, confidence float64) (Conditional, error) {
	if err := c.checkValidated(outcome, scenario); err != nil {
		return Conditional{}, err
	}
	for name, v := range map[string]float64{"posted": at.Posted, "baseline": at.Baseline,
		"trend": at.Trend, "line": line, "confidence": confidence} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Conditional{}, fmt.Errorf("scenario: %s = %v is not a real number", name, v)
		}
	}
	// A ratio to a non-positive baseline is not a quantity. Refused rather
	// than clamped: a player with no prior production has no distribution here
	// to price against, and pretending otherwise is how a divide-by-zero
	// becomes a probability.
	if at.Baseline <= 0 {
		return Conditional{}, fmt.Errorf(
			"scenario: a baseline of %g is not usable -- the grid prices a line as a ratio "+
				"to what this player normally does, so it needs his own prior mean", at.Baseline)
	}
	cell, err := c.findCell(outcome, scenario, occurred, at)
	if err != nil {
		return Conditional{}, err
	}
	// The gate, at the granularity the price is formed at. checkValidated above
	// only established that SOME site of this pairing is priceable; this is the
	// one being asked for.
	if !cell.Validated {
		return Conditional{}, fmt.Errorf(
			"scenario: %s/%s is priceable, but not at %s.\n  %s.\n"+
				"  Supply -q and -r yourself if you have your own read on this cell: %w",
			outcome, scenario, cell.SiteLabel(),
			strings.Join(cell.Why, "; "), ErrScenarioNotPriceable)
	}

	n := cell.effectiveN()

	// Refuse before clamping. A line past the observed range is not a small
	// probability, it is an absence of evidence, and clampToSupport is about to
	// make the two indistinguishable.
	// The line, expressed as a multiple of what this player normally does.
	ratio := line / at.Baseline

	if outsideSupport(cell.Quantiles, ratio) {
		lo, hi := cell.Quantiles[0][1], cell.Quantiles[len(cell.Quantiles)-1][1]
		return Conditional{}, fmt.Errorf(
			"scenario: a line of %.1f is %.2fx this player's %.1f baseline, and %s/%s "+
				"occurred=%v never produced a game between %.2fx and %.2fx over %d "+
				"games; the grid cannot price it.\n"+
				"  Supply -q and -r yourself if you have a read on a line this far out",
			line, ratio, at.Baseline, outcome, scenario, occurred, lo, hi, cell.N)
	}

	// Always the continuous reader. A ratio is continuous even when the
	// outcome is a count: 3 receptions against a 4.2 baseline is 0.714, and
	// there is no integer lattice left for the exact-CDF path to exploit.
	raw := probAbove(cell.Quantiles, ratio)
	p := clampToSupport(raw, n)

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
		// CellMedian is reported in the OUTCOME's units, not as the ratio the
		// grid stores. A median of "1.02" beside a line of 52.5 tells a reader
		// nothing they can check.
		N: cell.N, NEff: n, CellMedian: cell.MedianOutput, Cell: cell,
		TailN: math.Min(p, 1-p) * float64(n),
	}, nil
}

// Complement turns P(output > line) into P(output < line), for pricing an
// under off the same cell.
//
// The grid only ever fits one direction, because it only needs to: the two are
// the same distribution read from opposite ends. What has to move with the
// probability is the INTERVAL, and it has to be mirrored rather than
// recomputed -- [lo, hi] on the over is [1-hi, 1-lo] on the under, and
// forgetting to swap the ends would report an interval that does not contain
// its own estimate.
//
// Ties are measure zero: the stored quantiles are ratios to a player's own
// baseline, and a line divided by a baseline lands exactly on a stored point
// essentially never. This would not be safe on the old count grid, where
// P(X > 3) and P(X < 4) differ by the mass sitting exactly on 3.
func (c Conditional) Complement() Conditional {
	c.Prob = 1 - c.Prob
	c.Lower, c.Upper = 1-c.Upper, 1-c.Lower
	// TailN is min(p, 1-p) x NEff and so is already symmetric; CellMedian, N
	// and NEff describe the cell rather than the direction.
	return c
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
func (c *Conditionals) QR(outcome, scenario string, at Site, line, confidence float64) (q, r Conditional, err error) {
	q, err = c.Lookup(outcome, scenario, true, at, line, confidence)
	if err != nil {
		return Conditional{}, Conditional{}, err
	}
	r, err = c.Lookup(outcome, scenario, false, at, line, confidence)
	if err != nil {
		return Conditional{}, Conditional{}, err
	}
	return q, r, nil
}

// BestWagerableSite returns the (q, r) the belief probe's realised-edge endpoint
// wagers against for a scenario: the validated site where the scenario moves the
// prop MOST, priced at the player's own median line (ratio 1.0).
//
// The belief probe names no player, line or site, so E2 needs a representative
// one. The choice is deliberate on two counts. The site is the best available --
// "on the sharpest cell you could actually deploy this, would the belief have
// paid?" -- because a site where the scenario barely moves the distribution can
// never pay however good the read. The line is the median, ratio 1.0, which is
// the most conservative: separation is smaller there than at a deep line, so E2
// understates rather than overstates the edge. Both are frozen at ingest.
//
// ok=false when the scenario has no validated site -- blowout_loss, and
// pass_heavy on the volume outcomes -- so those never produce a wager, which is
// correct rather than a gap.
//
// Caveat: taking the MAX separation over validated cells carries a winner's
// curse -- the selected q-r is biased high by the selection -- which inflates the
// implied wager count and narrows E2's interval. E2 is a diagnostic, not the
// decision, so this is noted rather than corrected; a shrunk or held-out
// separation would be the fix if E2 were ever promoted.
func (c *Conditionals) BestWagerableSite(scenario string) (q, r float64, site string, ok bool) {
	for i := range c.Cells {
		cell := c.Cells[i]
		if cell.Scenario != scenario || !cell.Occurred || !cell.Validated {
			continue
		}
		at := Site{
			Posted:   interiorPoint(cell.PostedMin, cell.PostedMax),
			Baseline: interiorPoint(cell.BaselineMin, cell.BaselineMax),
			Trend:    interiorPoint(cell.TrendMin, cell.TrendMax),
		}
		if at.Baseline <= 0 {
			continue
		}
		qc, rc, err := c.QR(cell.Outcome, scenario, at, at.Baseline, 0.95)
		if err != nil {
			continue // this site's other half is unvalidated or ratio 1.0 is off its support
		}
		if sep := qc.Prob - rc.Prob; sep > 0 && sep > q-r {
			q, r, ok = qc.Prob, rc.Prob, true
			site = cell.Outcome + " @ " + cell.SiteLabel()
		}
	}
	return q, r, site, ok
}

// interiorPoint is a representative value inside a possibly open-ended band.
func interiorPoint(min, max float64) float64 {
	switch {
	case math.IsInf(min, -1) && math.IsInf(max, 1):
		return 0
	case math.IsInf(min, -1):
		return max
	case math.IsInf(max, 1):
		return min
	default:
		return (min + max) / 2
	}
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

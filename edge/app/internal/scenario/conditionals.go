package scenario

import (
	_ "embed"
	"encoding/json"
	"fmt"
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
	Scenario   string      `json:"scenario"`
	Occurred   bool        `json:"occurred"`
	TargetsMin float64     `json:"targets_min"`
	TargetsMax float64     `json:"targets_max"`
	TrendMin   float64     `json:"trend_min"`
	TrendMax   float64     `json:"trend_max"`
	N          int         `json:"n"`
	Median     float64     `json:"median"`
	Quantiles  [][]float64 `json:"quantiles"` // [[probability, yards], ...]
}

// Conditionals is the whole fitted grid.
type Conditionals struct {
	GeneratedAt string `json:"generated_at"`
	GeneratedBy string `json:"generated_by"`
	Outcome     string `json:"outcome"`
	Seasons     []int  `json:"seasons"`
	MinCell     int    `json:"min_cell"`
	Cells       []Cell `json:"cells"`
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
	Prob       float64
	Lower      float64
	Upper      float64
	N          int
	CellMedian float64
	Cell       Cell
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

// findCell locates the grid cell matching an opportunity level and role trend.
func (c *Conditionals) findCell(scenario string, occurred bool, projTargets, trend float64) (Cell, error) {
	for _, cell := range c.Cells {
		if cell.Scenario != scenario || cell.Occurred != occurred {
			continue
		}
		if projTargets < cell.TargetsMin || projTargets >= cell.TargetsMax {
			continue
		}
		if trend < cell.TrendMin || trend >= cell.TrendMax {
			continue
		}
		return cell, nil
	}
	return Cell{}, fmt.Errorf(
		"scenario: no fitted cell for %s occurred=%v at %.1f projected targets and %+.3f trend "+
			"(cells thinner than %d observations are not published)",
		scenario, occurred, projTargets, trend, c.MinCell)
}

// Lookup returns P(yards > line) for one side of a scenario.
func (c *Conditionals) Lookup(scenario string, occurred bool, projTargets, trend, line, confidence float64) (Conditional, error) {
	cell, err := c.findCell(scenario, occurred, projTargets, trend)
	if err != nil {
		return Conditional{}, err
	}
	p := probAbove(cell.Quantiles, line)

	// Reuse the interval the hit-rate layer already uses, so a pooled estimate
	// and an empirical one report uncertainty the same way.
	hits := int(p*float64(cell.N) + 0.5)
	lower, upper, err := wager.WilsonInterval(hits, cell.N, confidence)
	if err != nil {
		return Conditional{}, err
	}
	return Conditional{
		Prob: p, Lower: lower, Upper: upper,
		N: cell.N, CellMedian: cell.Median, Cell: cell,
	}, nil
}

// QR returns the conditional probabilities the belief decomposition needs:
// q with the scenario, r without it.
//
// Both come from the same grid at the same opportunity and trend, so the only
// thing that differs between them is the game script -- which is precisely what
// the decomposition assumes.
func (c *Conditionals) QR(scenario string, projTargets, trend, line, confidence float64) (q, r Conditional, err error) {
	q, err = c.Lookup(scenario, true, projTargets, trend, line, confidence)
	if err != nil {
		return Conditional{}, Conditional{}, err
	}
	r, err = c.Lookup(scenario, false, projTargets, trend, line, confidence)
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

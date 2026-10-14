package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"

	"edge/internal/betlog"
)

func projectionsCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: edgectl projections settle|score")
	}
	switch args[0] {
	case "settle":
		return projectionsSettle(args[1:])
	case "score":
		return projectionsScore(args[1:])
	case "robust":
		return projectionsRobust(args[1:])
	}
	return fmt.Errorf("edgectl projections: unknown subcommand %q (want settle, score or robust)", args[0])
}

type outcomesFile struct {
	Season int               `json:"season"`
	Week   int               `json:"week"`
	Rows   []outcomesFileRow `json:"rows"`
}

type outcomesFileRow struct {
	Player  string   `json:"player"`
	Outcome string   `json:"outcome"`
	Status  string   `json:"status"` // settled | unavailable
	Actual  *float64 `json:"actual"`
	Reason  string   `json:"reason"`
}

func projectionsSettle(args []string) error {
	fs := flag.NewFlagSet("projections settle", flag.ExitOnError)
	outcomesPath := fs.String("outcomes", "", "the realized-outcomes file for this week (required)")
	logPath := fs.String("log", "beliefs/projections.jsonl", "the projection log")
	dry := fs.Bool("n", false, "print the plan and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outcomesPath == "" {
		return fmt.Errorf("projections settle: -outcomes is required")
	}

	f, err := os.Open(*outcomesPath)
	if err != nil {
		return fmt.Errorf("projections settle: opening %s: %w", *outcomesPath, err)
	}
	defer f.Close()
	var of outcomesFile
	if err := json.NewDecoder(f).Decode(&of); err != nil {
		return fmt.Errorf("projections settle: %s: %w", *outcomesPath, err)
	}

	type key struct{ player, outcome string }
	result := map[key]outcomesFileRow{}
	for _, r := range of.Rows {
		result[key{r.Player, r.Outcome}] = r
	}

	projs, err := betlog.LoadProjections(*logPath)
	if err != nil {
		return err
	}

	var settle, void []betlog.SettledProjection
	var pending, already int
	for _, sp := range projs {
		p := sp.Projection
		if p.Season != of.Season || p.Week != of.Week {
			continue
		}
		if sp.Result != betlog.Open {
			already++
			continue
		}
		r, ok := result[key{p.Player, p.Outcome}]
		if !ok {
			pending++
			continue
		}
		switch r.Status {
		case "settled":
			if r.Actual == nil {
				return fmt.Errorf(
					"projections settle: %s is marked settled but carries no actual value", p.Key())
			}
			settle = append(settle, sp)
		case "unavailable":
			void = append(void, sp)
		default:
			return fmt.Errorf("projections settle: %s: unknown status %q", p.Key(), r.Status)
		}
	}

	fmt.Printf("SETTLE PROJECTIONS  %d week %d\n", of.Season, of.Week)
	fmt.Printf("  settle   %d\n  void     %d\n  pending  %d\n  already  %d\n",
		len(settle), len(void), pending, already)
	if *dry {
		fmt.Printf("\n-n: nothing written\n")
		return nil
	}
	for _, sp := range settle {
		r := result[key{sp.Projection.Player, sp.Projection.Outcome}]
		if err := betlog.SettleProjection(*logPath, sp.ID, *r.Actual, ""); err != nil {
			return err
		}
	}
	for _, sp := range void {
		// Looked up fresh from the result map, by (player, outcome) -- not from
		// a parallel slice indexed by position. A shared reasons slice appended
		// to in mixed settle/void iteration order, then read back via
		// reasons[len(settle)+i], only lines up when every settle match happens
		// to precede every void match in iteration order -- which log order
		// does not guarantee. (The sibling edgectl beliefs settle has exactly
		// this bug today, at app/cmd/edgectl/beliefs.go:391 -- out of scope to
		// fix here since it is existing, already-shipped code, but this task's
		// own new code must not reproduce it.)
		r := result[key{sp.Projection.Player, sp.Projection.Outcome}]
		reason := r.Reason
		if reason == "" {
			reason = "unmeasurable"
		}
		if err := betlog.VoidProjection(*logPath, sp.ID, reason); err != nil {
			return err
		}
	}
	return nil
}

// robustScenario is one scenario's priced view of a player/outcome.
type robustScenario struct {
	name       string
	ourMedian  float64
	marketLine float64
}

// robustGroup is a (player, outcome) the grid priced across multiple game
// scripts this week, with the distinct scenarios it cleared.
type robustGroup struct {
	player    string
	outcome   string
	count     int              // distinct scenarios
	maxGap    float64          // largest |OurMedian - MarketLine| across the group
	scenarios []robustScenario // one per distinct scenario, sorted by name
}

// robustGroups loads the projection log and returns the (player, outcome)
// groups for the given season/week that the grid priced under at least
// minScenarios distinct game-script scenarios. Settlement status is ignored --
// an Open, Measured or Void row all count as "the grid found this priceable" --
// because this runs before a week is settled. Results are sorted by distinct
// scenario count descending, ties broken by the largest market gap descending.
func robustGroups(logPath string, season, week, minScenarios int) ([]robustGroup, error) {
	projs, err := betlog.LoadProjections(logPath)
	if err != nil {
		return nil, err
	}

	type key struct{ player, outcome string }
	// Dedupe scenarios per group: a report that re-priced a player/outcome
	// under the same scenario must count once, not twice.
	byGroup := map[key]map[string]robustScenario{}
	order := []key{} // first-seen order, for a stable base before sorting
	for _, sp := range projs {
		p := sp.Projection
		if p.Season != season || p.Week != week {
			continue
		}
		k := key{p.Player, p.Outcome}
		if byGroup[k] == nil {
			byGroup[k] = map[string]robustScenario{}
			order = append(order, k)
		}
		byGroup[k][p.Scenario] = robustScenario{
			name: p.Scenario, ourMedian: p.OurMedian, marketLine: p.MarketLine}
	}

	var groups []robustGroup
	for _, k := range order {
		scenarios := byGroup[k]
		if len(scenarios) < minScenarios {
			continue
		}
		g := robustGroup{player: k.player, outcome: k.outcome, count: len(scenarios)}
		for _, s := range scenarios {
			g.scenarios = append(g.scenarios, s)
			if gap := math.Abs(s.ourMedian - s.marketLine); gap > g.maxGap {
				g.maxGap = gap
			}
		}
		sort.Slice(g.scenarios, func(i, j int) bool {
			return g.scenarios[i].name < g.scenarios[j].name
		})
		groups = append(groups, g)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].maxGap > groups[j].maxGap
	})
	return groups, nil
}

// projectionsRobust surfaces the player/outcomes the fitted grid corroborates
// across multiple game scripts this week -- the "which bets does the grid
// believe in regardless of how the game breaks" query that was previously only
// answerable by re-reading report prose.
func projectionsRobust(args []string) error {
	fs := flag.NewFlagSet("projections robust", flag.ExitOnError)
	logPath := fs.String("log", "beliefs/projections.jsonl", "the projection log")
	season := fs.Int("season", 0, "the season to scan (required)")
	week := fs.Int("week", 0, "the week to scan (required)")
	minScenarios := fs.Int("min-scenarios", 3, "keep player/outcomes priced under at least this many distinct scenarios")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *season <= 0 {
		return fmt.Errorf("projections robust: -season is required")
	}
	if *week <= 0 {
		return fmt.Errorf("projections robust: -week is required")
	}

	groups, err := robustGroups(*logPath, *season, *week, *minScenarios)
	if err != nil {
		return err
	}

	fmt.Printf("ROBUST PROJECTIONS  %d week %d  (>= %d scenarios)\n\n", *season, *week, *minScenarios)
	if len(groups) == 0 {
		fmt.Printf("  nothing cleared %d scenarios this week\n", *minScenarios)
		return nil
	}
	for _, g := range groups {
		fmt.Printf("  %s  %s  (%d of 4 scenarios)\n", g.player, g.outcome, g.count)
		for _, s := range g.scenarios {
			fmt.Printf("    %-18s  our %.1f  market %.1f\n", s.name, s.ourMedian, s.marketLine)
		}
		fmt.Println()
	}
	return nil
}

func projectionsScore(args []string) error {
	fs := flag.NewFlagSet("projections score", flag.ExitOnError)
	logPath := fs.String("log", "beliefs/projections.jsonl", "the projection log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projs, err := betlog.LoadProjections(*logPath)
	if err != nil {
		return err
	}
	byOutcome, err := betlog.ByOutcome(projs)
	if err != nil {
		return err
	}

	outcomes := make([]string, 0, len(byOutcome))
	for o := range byOutcome {
		outcomes = append(outcomes, o)
	}
	sort.Strings(outcomes)

	fmt.Printf("PROJECTIONS SCORE  %s\n\n", *logPath)
	if len(outcomes) == 0 {
		fmt.Printf("  no settled projections yet\n")
		return nil
	}
	for _, o := range outcomes {
		s := byOutcome[o]
		fmt.Printf("  %s  (n=%d)\n", o, s.N)
		if s.N == 0 {
			fmt.Printf("    nothing settled yet\n\n")
			continue
		}
		fmt.Printf("    our MAE      %.1f\n", s.OurMAE)
		fmt.Printf("    market MAE   %.1f\n", s.MarketMAE)
		fmt.Printf("    win rate     %.1f%%  (closer to actual than the market's line)\n", s.WinRate*100)
		fmt.Printf("    our bias     %+.1f  (positive = we project high)\n", s.OurBias)
		fmt.Printf("    market bias  %+.1f\n", s.MarketBias)
		if s.N < 20 {
			fmt.Printf("    SMALL SAMPLE: n=%d is not enough to trust the win rate above\n", s.N)
		}
		fmt.Println()
	}
	return nil
}

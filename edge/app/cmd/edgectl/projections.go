package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
	}
	return fmt.Errorf("edgectl projections: unknown subcommand %q (want settle or score)", args[0])
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

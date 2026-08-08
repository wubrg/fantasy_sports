package main

import (
	"flag"
	"fmt"
	"strings"

	"edge/internal/betlog"
)

func logCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("log needs a mode: list, settle or score")
	}
	switch args[0] {
	case "list":
		return logList(args[1:])
	case "settle":
		return logSettle(args[1:])
	case "score":
		return logScore(args[1:])
	default:
		return fmt.Errorf("unknown log mode %q (want list, settle or score)", args[0])
	}
}

func logList(args []string) error {
	fs := flag.NewFlagSet("log list", flag.ExitOnError)
	path := fs.String("file", "", "path to the bet log (required)")
	openOnly := fs.Bool("open", false, "show only unsettled wagers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-file is required")
	}
	bets, err := betlog.Load(*path)
	if err != nil {
		return err
	}
	if len(bets) == 0 {
		fmt.Println("no wagers logged")
		return nil
	}

	fmt.Printf("%-48s %-24s %7s %8s %8s\n", "id", "selection", "price", "pred", "result")
	fmt.Println(strings.Repeat("-", 100))
	for _, b := range bets {
		if *openOnly && b.Result != betlog.Open {
			continue
		}
		sel := b.Bet.Selection
		if len(sel) > 24 {
			sel = sel[:23] + "…"
		}
		fmt.Printf("%-48s %-24s %7s %7.1f%% %8s\n",
			b.ID, sel, fmt.Sprintf("%+d", b.Bet.Price), b.Bet.Predicted*100, b.Result)
	}
	return nil
}

func logSettle(args []string) error {
	fs := flag.NewFlagSet("log settle", flag.ExitOnError)
	path := fs.String("file", "", "path to the bet log (required)")
	id := fs.String("id", "", "wager id (required)")
	result := fs.String("result", "", "won, lost, push or void (required)")
	note := fs.String("note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" || *id == "" || *result == "" {
		return fmt.Errorf("-file, -id and -result are all required")
	}
	r := betlog.Result(strings.ToLower(*result))
	switch r {
	case betlog.Won, betlog.Lost, betlog.Pushed, betlog.Void:
	default:
		return fmt.Errorf("-result must be won, lost, push or void, got %q", *result)
	}

	// Confirm the wager exists before appending, so a typo does not leave an
	// orphaned settlement that makes the whole log unreadable.
	bets, err := betlog.Load(*path)
	if err != nil {
		return err
	}
	found := false
	for _, b := range bets {
		if b.ID == *id {
			found = true
			if b.Result != betlog.Open {
				return fmt.Errorf("wager %s is already settled as %s", *id, b.Result)
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("no wager with id %q in %s", *id, *path)
	}

	if err := betlog.Settle(*path, *id, r, *note); err != nil {
		return err
	}
	fmt.Printf("%s settled as %s\n", *id, r)
	return nil
}

func logScore(args []string) error {
	fs := flag.NewFlagSet("log score", flag.ExitOnError)
	path := fs.String("file", "", "path to the bet log (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-file is required")
	}
	bets, err := betlog.Load(*path)
	if err != nil {
		return err
	}
	overall, err := betlog.Score(bets, nil)
	if err != nil {
		return err
	}

	fmt.Println("CALIBRATION")
	fmt.Println()
	if overall.Scored == 0 {
		fmt.Printf("  nothing settled yet (%d open, %d excluded)\n", overall.Open, overall.Excluded)
		return nil
	}

	fmt.Printf("  settled %d   open %d   excluded %d (pushes and voids test no prediction)\n",
		overall.Scored, overall.Open, overall.Excluded)
	fmt.Printf("  predicted %.1f%%   realised %.1f%%   (%+.1f points)\n",
		overall.Expected*100, overall.Realised*100,
		(overall.Realised-overall.Expected)*100)
	fmt.Printf("  staked %.2f   profit %+.2f   ROI %+.1f%%\n",
		overall.Staked, overall.Profit, overall.StakedROI*100)

	if overall.Scored < 30 {
		fmt.Printf("\n  note: %d settled wagers is far too few to read as calibration.\n", overall.Scored)
		fmt.Printf("  the gap above is mostly noise until this number is in the hundreds.\n")
	}

	groups := []struct {
		title string
		fn    func([]betlog.Settled) (map[string]betlog.Calibration, error)
	}{
		{"BY SCENARIO SOURCE (who judged the game script)", betlog.BySource},
		{"BY CONDITIONAL SOURCE (who supplied q and r)", betlog.ByConditionalSource},
	}
	for _, g := range groups {
		by, err := g.fn(bets)
		if err != nil {
			return err
		}
		if len(by) < 2 {
			continue
		}
		fmt.Printf("\n  %s\n", g.title)
		fmt.Printf("  %-24s %8s %10s %10s\n", "source", "settled", "predicted", "realised")
		for _, k := range betlog.SortedSources(by) {
			c := by[k]
			if c.Scored == 0 {
				continue
			}
			fmt.Printf("  %-24s %8d %9.1f%% %9.1f%%\n", k, c.Scored, c.Expected*100, c.Realised*100)
		}
	}
	return nil
}

package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"edge/internal/wager"
)

func hitrateCmd(args []string) error {
	fs := flag.NewFlagSet("hitrate", flag.ExitOnError)
	line := fs.Float64("line", 0, "the prop line (required)")
	sideFlag := fs.String("side", "over", "over or under")
	values := fs.String("values", "", "comma-separated game-log results, most recent first (required)")
	confidence := fs.Float64("confidence", 0.95, "confidence level for the interval")
	price := fs.Int("price", 0, "offered American price, to get a verdict")
	label := fs.String("label", "", "what this wager is")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *values == "" {
		return fmt.Errorf("-values is required: a hit rate needs a game log")
	}
	// -line defaults to 0, which is a real number a caller could mean, so
	// absence has to be detected rather than inferred. Omitting it used to
	// score every game against a line of zero and report a 100% hit rate.
	lineSupplied := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "line" {
			lineSupplied = true
		}
	})
	if !lineSupplied {
		return fmt.Errorf("-line is required: without it every game is scored against 0")
	}

	var side wager.Side
	switch strings.ToLower(*sideFlag) {
	case "over", "o":
		side = wager.Over
	case "under", "u":
		side = wager.Under
	default:
		return fmt.Errorf("-side must be 'over' or 'under', got %q", *sideFlag)
	}

	parsed, err := parseValues(*values)
	if err != nil {
		return err
	}

	h, err := wager.ComputeHitRate(parsed, *line, side, *confidence)
	if err != nil {
		return err
	}

	if *label != "" {
		fmt.Printf("%s\n", *label)
	}
	fmt.Printf("%s %.1f — %d of %d games", side, *line, h.Hits, h.N)
	if h.Pushes > 0 {
		fmt.Printf("  (%d push%s excluded)", h.Pushes, plural(h.Pushes))
	}
	fmt.Println()
	fmt.Println()
	fmt.Printf("  point estimate   %.1f%%\n", h.Rate*100)
	fmt.Printf("  %.0f%% interval     %.1f%% – %.1f%%   (width %.1f pts)\n",
		h.Confidence*100, h.Lower*100, h.Upper*100, h.Width()*100)
	fmt.Println()

	pointPrice, err := wager.MinPrice(wager.RealMoney, h.Rate, 0)
	if err != nil {
		return err
	}
	fmt.Printf("  minimum price, point estimate ... %+d\n", pointPrice)
	if h.Lower > 0 {
		boundPrice, err := wager.MinPrice(wager.RealMoney, h.Lower, 0)
		if err != nil {
			return err
		}
		fmt.Printf("  minimum price, lower bound ...... %+d\n", boundPrice)
	} else {
		fmt.Printf("  minimum price, lower bound ...... none (bound is 0%%)\n")
	}

	if *price != 0 {
		verdict, err := h.Assess(wager.American(*price))
		if err != nil {
			return err
		}
		breakeven, err := wager.American(*price).Breakeven()
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Printf("  offered %+d — hurdle %.1f%% — %s\n",
			*price, breakeven*100, strings.ToUpper(verdict.String()))
		switch verdict {
		case wager.Supported:
			fmt.Printf("  the whole interval clears the price\n")
		case wager.Unproven:
			fmt.Printf("  the point estimate clears but the lower bound does not:\n")
			fmt.Printf("  this is not distinguishable from sampling noise\n")
		case wager.Reject:
			fmt.Printf("  the point estimate itself fails to clear the price\n")
		}
	}

	if h.N < 20 {
		fmt.Println()
		fmt.Printf("  note: %d games is a small sample. Widen the window before\n", h.N)
		fmt.Printf("  treating the point estimate as a probability.\n")
	}
	return nil
}

func parseValues(s string) ([]float64, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(fields) == 0 {
		return nil, fmt.Errorf("no values parsed from %q", s)
	}
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil {
			return nil, fmt.Errorf("could not parse %q as a number", f)
		}
		out = append(out, v)
	}
	return out, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

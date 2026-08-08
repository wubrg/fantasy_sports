package main

import (
	"flag"
	"fmt"
	"math"

	"edge/internal/wager"
)

func cardCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("card needs a mode: 'bonus' or 'real'")
	}
	switch args[0] {
	case "bonus":
		return bonusCard(args[1:])
	case "real":
		return realCard(args[1:])
	default:
		return fmt.Errorf("unknown card mode %q (want 'bonus' or 'real')", args[0])
	}
}

// bonusCard renders the static bonus-bet reference. It takes no market data
// and no projections: a bonus bet is positive EV at any price, so the only
// question is how much face value converts, and that depends on the price
// alone. This card never needs regenerating.
func bonusCard(args []string) error {
	fs := flag.NewFlagSet("card bonus", flag.ExitOnError)
	target := fs.Float64("target", 0.70, "minimum acceptable conversion rate")
	if err := fs.Parse(args); err != nil {
		return err
	}

	floor, err := wager.MinPriceForConversion(*target)
	if err != nil {
		return err
	}

	fmt.Println("BONUS BET CARD  (static — no data pull required)")
	fmt.Println()
	fmt.Printf("  A bonus bet is +EV at ANY price: the stake is not yours and is not\n")
	fmt.Printf("  returned, so there is no downside term. You are not hunting an edge,\n")
	fmt.Printf("  only the longest price you are willing to wait on.\n")
	fmt.Println()
	fmt.Printf("  YOUR FLOOR: %+d  (for %.0f%% conversion)\n", floor, *target*100)
	fmt.Println()
	fmt.Printf("  %-8s %8s %12s   %s\n", "price", "wins", "conversion", "note")
	fmt.Printf("  %s\n", "------------------------------------------------------------")

	for _, a := range []wager.American{100, 150, 200, 234, 300, 400, 500, 600, 1000, 2000} {
		p, err := a.ImpliedRaw()
		if err != nil {
			return err
		}
		conv, err := a.ConversionCeiling()
		if err != nil {
			return err
		}
		note := ""
		switch {
		case a >= 300 && a <= 600:
			note = "sweet spot"
		case a > 600:
			note = "high variance"
		}
		mark := "  "
		if a >= floor {
			mark = "> "
		}
		fmt.Printf("%s%-8s %7.1f%% %11.1f%%   %s\n",
			mark, fmt.Sprintf("%+d", a), p*100, conv*100, note)
	}

	fmt.Println()
	fmt.Println("  RULES")
	fmt.Printf("   1. FanDuel: NEVER on a market that can push (whole-number spreads\n")
	fmt.Printf("      and totals). A push forfeits the bonus bet outright — a 100%%\n")
	fmt.Printf("      loss, not a reduced conversion. DraftKings returns it.\n")
	fmt.Printf("   2. Prefer +300 to +600. EV keeps climbing past it, but at 3 bonus\n")
	fmt.Printf("      bets a month:\n")
	for _, a := range []wager.American{300, 600, 1000, 2000} {
		p, err := a.ImpliedRaw()
		if err != nil {
			return err
		}
		fmt.Printf("        %+6d — %.0f%% chance all three lose (median month: $0)\n",
			a, math.Pow(1-p, 3)*100)
	}
	fmt.Printf("   3. Conversion above is a CEILING at fair odds. Vig is heaviest on\n")
	fmt.Printf("      longshots, so the overstatement is worst exactly where this card\n")
	fmt.Printf("      points. Realised conversion is typically 60–80%%.\n")
	fmt.Printf("   4. DraftKings tokens cannot be split; FanDuel bonuses can.\n")
	return nil
}

// realCard renders the real-money thresholds for one belief. Unlike the bonus
// card this is per-candidate and must be regenerated whenever the projection
// changes.
func realCard(args []string) error {
	fs := flag.NewFlagSet("card real", flag.ExitOnError)
	p := fs.Float64("p", -1, "point estimate of true probability (required)")
	lo := fs.Float64("lo", -1, "lower confidence bound on p, if known")
	label := fs.String("label", "", "what this wager is")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *p < 0 {
		return fmt.Errorf("-p is required: a real-money threshold needs a belief")
	}

	fmt.Println("REAL MONEY CARD")
	if *label != "" {
		fmt.Printf("  %s\n", *label)
	}
	fmt.Println()
	fmt.Printf("  Bet only at the listed price OR BETTER (longer odds).\n")
	fmt.Println()
	fmt.Printf("  %-12s %14s", "target", "point est.")
	if *lo >= 0 {
		fmt.Printf(" %14s", "lower bound")
	}
	fmt.Println()
	fmt.Printf("  %-12s %14s", "", fmt.Sprintf("%.1f%%", *p*100))
	if *lo >= 0 {
		fmt.Printf(" %14s", fmt.Sprintf("%.1f%%", *lo*100))
	}
	fmt.Println()
	fmt.Printf("  %s\n", "------------------------------------------------------")

	for _, target := range []float64{0, 0.02, 0.05} {
		point, err := wager.MinPrice(wager.RealMoney, *p, target)
		if err != nil {
			return err
		}
		fmt.Printf("  EV >= %-6s %14s", fmt.Sprintf("%.0f%%", target*100), fmt.Sprintf("%+d", point))
		if *lo >= 0 {
			bound, err := wager.MinPrice(wager.RealMoney, *lo, target)
			if err != nil {
				return err
			}
			fmt.Printf(" %14s", fmt.Sprintf("%+d", bound))
		}
		fmt.Println()
	}

	if *lo >= 0 {
		fmt.Println()
		fmt.Printf("  The two columns are the honest state of your estimate. If the board\n")
		fmt.Printf("  sits between them, the point estimate says bet and your uncertainty\n")
		fmt.Printf("  says it is indistinguishable from noise.\n")
	} else {
		fmt.Println()
		fmt.Printf("  No -lo supplied: this card assumes the estimate is exact. It is not.\n")
		fmt.Printf("  At +150 a 5-point error in p moves EV by 12.5 points.\n")
	}
	return nil
}

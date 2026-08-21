package main

import (
	"flag"
	"fmt"

	"edge/internal/wager"
)

// hedgeCmd converts a bonus bet into guaranteed cash.
//
// This is Phase 1 of the analytical-hobbyist framework: use risk-free
// conversion to build a cash bankroll, THEN deploy that capital into
// high-variance +EV betting. Running Phase 2 without Phase 1 is not wrong, but
// it is the strategy without its foundation -- every asset rides on outcomes
// rather than being banked first.
//
// The hedge needs cash at a DIFFERENT book. That requirement is why this is
// unavailable to anyone holding only promotional money at one sportsbook, and
// it is worth stating plainly rather than discovering at the betslip.
func hedgeCmd(args []string) error {
	fs := flag.NewFlagSet("hedge", flag.ExitOnError)
	face := fs.Float64("face", 0, "bonus bet face value (required)")
	back := fs.Int("back", 0, "price of the bonus bet you hold (required)")
	against := fs.Int("against", 0, "price on the OPPOSING side at a different book (required)")
	target := fs.Float64("target", 0.70, "conversion rate to judge against")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *face <= 0 || *back == 0 || *against == 0 {
		return fmt.Errorf("-face, -back and -against are all required")
	}

	h, err := wager.ConvertBonus(*face, wager.American(*back), wager.American(*against))
	if err != nil {
		return err
	}

	fmt.Printf("\n  CONVERTING %s of bonus at %s\n", money(*face), price(wager.American(*back)))
	fmt.Printf("  hedging %s on the other side, at another book\n\n", price(wager.American(*against)))
	fmt.Printf("    cash to stake       %10s\n", money(h.Stake))
	fmt.Printf("    guaranteed profit   %10s\n", money(h.Profit))
	fmt.Printf("    conversion          %9.1f%%\n", h.Conversion*100)
	fmt.Printf("    combined hold       %9.2f%%\n", h.Hold*100)
	fmt.Println()

	switch {
	case h.Hold < 0:
		fmt.Printf("  Negative hold: the two books disagree enough that this is a true\n")
		fmt.Printf("  arbitrage, not merely a good conversion.\n")
	case h.Conversion >= *target:
		fmt.Printf("  Clears the %.0f%% goal.\n", *target*100)
	case h.Conversion >= 0.60:
		fmt.Printf("  Below the %.0f%% goal but above 60%%, which the framework calls\n", *target*100)
		fmt.Printf("  acceptable for speed or simplicity.\n")
	case h.Conversion >= 0.50:
		fmt.Printf("  Weak. Under the %.0f%% goal and close to the 50%% line below which\n", *target*100)
		fmt.Printf("  conversion is considered highly inefficient. Look for a lower-hold market.\n")
	default:
		fmt.Printf("  Highly inefficient: under 50%%. Conversion is decided almost entirely\n")
		fmt.Printf("  by the combined hold (%.2f%% here), so a different market will help far\n", h.Hold*100)
		fmt.Printf("  more than resizing the hedge.\n")
	}

	fmt.Printf("\n  The hedge MUST be at a different sportsbook. Backing both sides at one\n")
	fmt.Printf("  is the clearest signal of an arber and is what gets an account\n")
	fmt.Printf("  promo-banned -- which ends the strategy, not just the wager.\n")
	fmt.Printf("  Round the stake to %s rather than %s: exact cents are a red flag,\n",
		money(roundTo(h.Stake, 1)), money(h.Stake))
	fmt.Printf("  and the lost precision is the price of camouflage.\n")
	return nil
}

// roundTo rounds a stake to the nearest step, for camouflage. Part 5 of the
// framework treats this as a cost of doing business rather than sloppiness: a
// $47.81 wager is a hedge calculation showing through.
func roundTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return float64(int((v/step)+0.5)) * step
}

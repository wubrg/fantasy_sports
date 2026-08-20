package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"edge/internal/board"
	"edge/internal/wager"
)

// boardReport prints one week's board, read for one book.
//
// Everything printed here is computed in internal/board. This file only reads
// flags, opens the file and lays the numbers out: the arithmetic must stay
// testable, and formatting code is the one place it cannot be tested honestly.
func boardReport(args []string) error {
	fs := flag.NewFlagSet("board report", flag.ExitOnError)
	dir := fs.String("dir", defaultBoardDir, "directory holding the week files")
	week := fs.Int("week", 0, "week to report on (required)")
	book := fs.String("book", board.Consensus, "which book's prices to read")
	stake := fs.Float64("stake", 25, "bonus bet face value, for the dollar columns")
	shots := fs.Int("shots", 4, "how many disjoint parlays to build")
	target := fs.Float64("target", board.DefaultTarget, "bonus-bet conversion floor")
	objective := fs.String("objective", "hitrate",
		"what the parlay set maximises: 'hitrate' (P at least one hits) or 'conversion' (EV per dollar)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}
	if *stake <= 0 {
		return fmt.Errorf("-stake %v must be positive", *stake)
	}
	// A book the board has no column for would otherwise report as "no prices
	// anywhere", which is what a typo and an untyped book look like alike.
	if !slices.Contains(board.Books, *book) {
		return fmt.Errorf("no column for book %q (have: %s)",
			*book, strings.Join(board.Books, ", "))
	}

	path := filepath.Join(*dir, fmt.Sprintf("week%02d.yaml", *week))
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read the board: %w\n"+
			"run `edgectl board scaffold -week %d` to create it", err, *week)
	}
	defer f.Close()
	doc, err := board.Parse(f)
	if err != nil {
		return err
	}

	obj, err := board.ParseObjective(*objective)
	if err != nil {
		return err
	}
	a, err := board.Analyze(doc, board.Options{
		Book: *book, Target: *target, Shots: *shots, Objective: obj,
	})
	if err != nil {
		return err
	}

	fmt.Printf("BOARD REPORT  week %02d, %d — %s\n", a.Week, a.Season, a.Book)
	fmt.Printf("  %s\n", path)
	fmt.Println()

	if !a.Bettable() {
		return reportEmpty(a, path)
	}
	fmt.Printf("  %d of %d games priced at %s.\n",
		len(a.Lines), len(a.Lines)+len(a.Missing), a.Book)
	if a.Book == board.Consensus {
		fmt.Printf("  consensus is a reference column, not a book you can bet. Prices\n")
		fmt.Printf("  below are what the side is WORTH, not what you can get.\n")
	}
	printProblems(a)
	printSuspect(a)
	printDevig(a)
	printDogs(a, *stake)
	if err := printSet(a, *stake); err != nil {
		return err
	}
	printShop(a)
	return nil
}

// reportEmpty is the normal state of every book but consensus. It is not an
// error and must not read like one: the board is typed by hand, one book at a
// time, and an unpriced book means "not typed yet".
func reportEmpty(a *board.Analysis, path string) error {
	fmt.Printf("  No prices for %q anywhere in this week — %d games, every cell empty.\n",
		a.Book, len(a.Missing))
	fmt.Println()
	if a.Book != board.Consensus {
		fmt.Printf("  The consensus column is prefilled from the schedule, so:\n")
		fmt.Printf("    edgectl board report -week %d -book consensus\n", a.Week)
		fmt.Println()
		fmt.Printf("  To report on %s, fill its ml: cells in %s\n", a.Book, path)
		fmt.Printf("  (away/home, e.g. +390/-525) and run this again.\n")
	} else {
		fmt.Printf("  Re-run `edgectl board scaffold -week %d` to prefill it from games.csv.\n", a.Week)
	}
	printProblems(a)
	return nil
}

func printProblems(a *board.Analysis) {
	if len(a.Problems) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("  UNREADABLE CELLS (skipped)\n")
	for _, p := range a.Problems {
		fmt.Printf("    %s\n", p)
	}
}

func printSuspect(a *board.Analysis) {
	if len(a.Suspect) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("  SUSPECT PRICES — probable transcription errors\n")
	for _, l := range a.Suspect {
		fmt.Printf("    %-12s %s/%s\n", matchup(l), price(l.Away.Price), price(l.Home.Price))
		// The explanation is one sentence with a semicolon in it; breaking
		// there keeps every line inside 80 columns without hard-wrapping text
		// that the board package owns.
		for i, part := range strings.SplitAfter(l.Why, "; ") {
			fmt.Printf("      %s%s\n", strings.Repeat("  ", i), strings.TrimSpace(part))
		}
	}
	fmt.Printf("    Re-check these against the board. They are held out of the parlay\n")
	fmt.Printf("    set below, because a de-vig you do not trust propagates.\n")
}

func printDevig(a *board.Analysis) {
	fmt.Println()
	fmt.Printf("  DE-VIGGED LINES  (fair = the market's own estimate, vig removed)\n")
	fmt.Printf("  %-12s %-10s %10s %8s %8s %9s\n",
		"game", "dog", "overround", "raw", "fair", "conv")
	fmt.Printf("  %s\n", strings.Repeat("-", 62))
	for _, l := range a.Lines {
		d := l.Dog()
		raw, err := d.Price.ImpliedRaw()
		if err != nil {
			continue
		}
		flag := " "
		if l.Suspect {
			flag = "!"
		}
		fmt.Printf(" %s%-12s %-10s %9.2f%% %7.1f%% %7.1f%% %8.1f%%\n",
			flag, matchup(l), fmt.Sprintf("%s %s", d.Team, price(d.Price)),
			l.Overround*100, raw*100, d.Fair*100, d.Conversion*100)
	}
	fmt.Printf("  raw is the hurdle rate the price implies; fair is what the market\n")
	fmt.Printf("  actually thinks. Conversion uses fair, which is why a long price can\n")
	fmt.Printf("  still convert badly.\n")
}

func printDogs(a *board.Analysis, stake float64) {
	fmt.Println()
	fmt.Printf("  DOGS BY CONVERSION  (floor %+d = %.0f%%, from `edgectl card bonus`)\n",
		a.Floor, a.Target*100)
	fmt.Printf("  %-6s %8s %9s %10s   %s\n", "team", "price", "conv", "on "+money(stake), "note")
	fmt.Printf("  %s\n", strings.Repeat("-", 62))
	suspect := map[string]bool{}
	for _, l := range a.Suspect {
		suspect[l.GameID] = true
	}
	for _, d := range a.Dogs {
		ev, err := wager.EVBonusBet(d.Fair, d.Price, stake)
		if err != nil {
			continue
		}
		note := ""
		if suspect[d.GameID] {
			note = "suspect price — see above"
		} else if !d.Clears(a.Target) {
			// The failure this ranking exists to catch: a price above the
			// floor that still converts below it once the vig is off.
			note = "below floor"
			if d.Price >= a.Floor {
				note = fmt.Sprintf("below floor — %s clears %+d on price alone", d.Team, a.Floor)
			}
		}
		fmt.Println(strings.TrimRight(fmt.Sprintf("  %-6s %8s %8.1f%% %10s   %s",
			d.Team, price(d.Price), d.Conversion*100, money(ev), note), " "))
	}
}

func printSet(a *board.Analysis, stake float64) error {
	set := a.Set
	fmt.Println()
	if len(set.Parlays) == 0 {
		fmt.Printf("  PARLAY SET: nothing to build (needs two dogs from two games).\n")
		return nil
	}
	fmt.Printf("  DISJOINT PARLAY SET — %d shot(s) at %s\n", len(set.Parlays), money(stake))
	fmt.Printf("  No team appears twice and no game is used twice, so every ticket can\n")
	fmt.Printf("  be live at once without one hedging another.\n")
	fmt.Println()
	fmt.Printf("  %-14s %9s %9s %9s %10s\n", "legs", "price", "true p", "conv", "on "+money(stake))
	fmt.Printf("  %s\n", strings.Repeat("-", 62))
	converted := 0.0
	for _, p := range set.Parlays {
		// Conversion comes from the exact decimal, not the rounded ticket
		// price, so the dollar column is not moved by a display decision.
		fmt.Printf("  %-14s %9s %8.1f%% %8.1f%% %10s\n",
			strings.Join(p.Teams(), "+"), price(p.Price),
			p.TrueProb*100, p.Conversion*100, money(p.Conversion*stake))
		converted += p.Conversion * stake
	}
	fmt.Println()
	fmt.Printf("  average conversion %.1f%%   P(at least one hits) %.1f%%   total %s\n",
		set.AvgConversion*100, set.AnyHit*100, money(converted))
	if set.Unfilled > 0 {
		fmt.Printf("  %d shot(s) could not be filled: the board ran out of dogs from\n", set.Unfilled)
		fmt.Printf("  games not already used.\n")
	}
	fmt.Printf("  True p is the product of the DE-VIGGED legs. De-vigging the parlay\n")
	fmt.Printf("  price instead would leave every leg's vig compounded inside it and\n")
	fmt.Printf("  overstate accuracy by about a point.\n")
	return nil
}

func printShop(a *board.Analysis) {
	fmt.Println()
	if len(a.Shop) == 0 {
		fmt.Printf("  LINE SHOPPING: nothing to compare — only the consensus column is\n")
		fmt.Printf("  filled, and consensus is not a book you can bet.\n")
		return
	}
	fmt.Printf("  LINE SHOPPING — best bettable price vs consensus\n")
	fmt.Printf("  %-6s %10s %-11s %10s %8s %9s\n",
		"side", "best", "at", "consensus", "gap", "payout")
	fmt.Printf("  %s\n", strings.Repeat("-", 62))
	for _, r := range a.Shop {
		gap := "n/a"
		if r.PointsValid {
			gap = fmt.Sprintf("%+d", r.Points)
		}
		cons := "—"
		payout := ""
		if r.HasCons {
			cons = price(r.Consensus)
			payout = fmt.Sprintf("%+.1f%%", r.PayoutGap*100)
		}
		fmt.Printf("  %-6s %10s %-11s %10s %8s %9s\n",
			r.Team, price(r.Best.Price), r.Best.Book, cons, gap, payout)
	}
	fmt.Printf("  gap is in American points and is blank where the two prices sit on\n")
	fmt.Printf("  opposite sides of even money, where points are not a scale; payout is\n")
	fmt.Printf("  the change in total return per unit staked and is always defined.\n")
}

func matchup(l board.GameLine) string {
	return fmt.Sprintf("%s @ %s", l.Away.Team, l.Home.Team)
}

func price(a wager.American) string { return fmt.Sprintf("%+d", a) }

func money(v float64) string { return fmt.Sprintf("$%.2f", v) }

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	book := fs.String("book", "", "which book's prices to read (required)")
	stake := fs.Float64("stake", 25, "bonus bet face value, when not deploying a bankroll")
	funds := fs.Float64("funds", 0, "bankroll to deploy; with this, stake is derived rather than given")
	minBet := fs.Float64("min-bet", 1, "smallest wager the book accepts; bounds how far funds can split")
	expiry := fs.String("expiry", "", "when the funds die (2026-08-26); decides whether waiting is an option")
	shots := fs.Int("shots", 4, "how many disjoint parlays to build")
	target := fs.Float64("target", board.DefaultTarget, "bonus-bet conversion floor")
	exclude := fs.String("exclude", "",
		"extra teams to leave out, on top of those the log shows as committed")
	logPath := fs.String("log", defaultBetlog(), "prediction log, read for teams already committed")
	ignoreLog := fs.Bool("ignore-log", false,
		"do not exclude teams carrying open wagers (they will be proposed again)")
	lined := fs.Bool("lined", false,
		"admit spread and total sides as parlay legs (only safe where a push returns the stake)")
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
	if *book != "" && !slices.Contains(board.Books, *book) {
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

	// -book is required rather than defaulting.
	//
	// It used to default to consensus, which is prefilled from the schedule and
	// so is always complete -- meaning the tool produced a confident-looking
	// report even when not one real price had been entered. Defaulting to the
	// one column that is never empty is the most misleading possible default.
	if *book == "" {
		return fmt.Errorf("-book is required\n%s", coverageTable(doc, *week))
	}

	obj, err := board.ParseObjective(*objective)
	if err != nil {
		return err
	}
	// Teams already carrying an open wager are excluded by default. Deriving
	// this from the log is the point of keeping one: retyping the list every
	// run only had to be forgotten once to put two tickets on one game.
	excl := splitCSV(*exclude)
	var committed []Commitment
	if !*ignoreLog {
		c, teams, err := PlacedCommitments(*logPath, doc)
		if err != nil {
			return fmt.Errorf("reading %s: %w\n"+
				"pass -ignore-log to report without it", *logPath, err)
		}
		committed = c
		excl = append(excl, teams...)
	}

	a, err := board.Analyze(doc, board.Options{
		Book: *book, Target: *target, Shots: *shots, Objective: obj, Lined: *lined,
		Exclude: excl,
		Funds:   map[string]float64{*book: *funds},
		MinBet:  *minBet,
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
	// With a bankroll, the stake is a consequence of how far it is split rather
	// than a number supplied alongside it.
	useStake := *stake
	if *funds > 0 {
		exp, err := parseExpiry(*expiry)
		if err != nil {
			return err
		}
		chosen, err := printDeployment(a, doc, *funds, *minBet, *shots, obj, exp)
		if err != nil {
			return err
		}
		if chosen > 0 {
			useStake = chosen
		}
	}
	printCommitments(committed, splitCSV(*exclude), *ignoreLog)
	if err := printSet(a, useStake); err != nil {
		return err
	}
	printShop(a)
	return nil
}

// parseExpiry reads a plain date in local time. Local because a promo's expiry
// is read off a book's page in the operator's own timezone, and shifting it to
// UTC would move a Tuesday deadline into Monday for anyone west of Greenwich.
func parseExpiry(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("-expiry %q: want a date like 2026-08-26", s)
	}
	return t, nil
}

// printDeployment prints the frontier and the advice, returning the stake of
// the row the operator should use.
func printDeployment(a *board.Analysis, doc *board.Doc, funds, minBet float64,
	want int, obj board.Objective, expiry time.Time) (float64, error) {

	f, err := board.Frontier(a.Legs(), funds, minBet, obj, a.Target)
	if err != nil {
		return 0, err
	}
	fmt.Println()
	if len(f) == 0 {
		// The empty case needs the advice MORE than a full one, not less: this
		// is precisely when "so should I wait?" is the next question, and
		// whether the money survives to wait with is the answer.
		fmt.Printf("  DEPLOYING %s: nothing in this week clears the %.0f%% floor.\n",
			money(funds), a.Target*100)
		adv := board.Advise(nil, want, funds, a.Target, expiry, doc.LastKickoff(), time.Now())
		for _, r := range adv.Reasons {
			fmt.Printf("  - %s\n", r)
		}
		return 0, nil
	}

	fmt.Printf("  DEPLOYING %s  (minimum wager %s)\n", money(funds), money(minBet))
	free := board.FreeToSplit(f)
	if free {
		// Saying this plainly matters more than the table. Asking someone to
		// weigh a trade that does not exist wastes the one judgement the tool
		// is asking them for.
		fmt.Printf("  Splitting further costs nothing here: every row below is beaten by the\n")
		fmt.Printf("  last on BOTH conversion and hit rate, so take the most shots available.\n")
	}
	fmt.Println()
	fmt.Printf("  %6s %9s %10s %9s %9s\n", "shots", "stake", "avg conv", "P(>=1)", "EV")
	fmt.Printf("  %s\n", strings.Repeat("-", 48))
	for _, d := range f {
		mark := " "
		if d.Dominated {
			mark = "\u00b7"
		}
		fmt.Printf(" %s%5d %9s %9.1f%% %8.1f%% %9s\n",
			mark, d.Shots, money(d.Stake), d.Set.AvgConversion*100,
			d.Set.AnyHit*100, money(d.EV))
	}
	if !free {
		fmt.Printf("  \u00b7 marks a row beaten by a later one on both metrics.\n")
	}

	adv := board.Advise(f, want, funds, a.Target, expiry, doc.LastKickoff(), time.Now())
	if len(adv.Reasons) > 0 {
		fmt.Println()
		for _, r := range adv.Reasons {
			fmt.Printf("  - %s\n", r)
		}
	}

	chosen := board.Prefer(f, obj)
	for _, d := range f {
		if d.Shots == want {
			chosen = d
		}
	}
	fmt.Println()
	fmt.Printf("  Showing %d shot(s) at %s.\n", chosen.Shots, money(chosen.Stake))
	return chosen.Stake, nil
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
	if a.Provisional {
		// Said before the tickets, not after them. A caveat under a table is
		// read once the numbers have already been believed.
		fmt.Printf("  DISJOINT PARLAY SET — PROVISIONAL (%d of %d games priced at %s)\n",
			len(a.Lines), len(a.Lines)+len(a.Missing), a.Book)
		fmt.Printf("  %d game(s) have no %s price. This is the best pairing of the %d that\n",
			len(a.Missing), a.Book, len(a.Lines))
		fmt.Printf("  do, which is not the same as the best pairing of the week — expect it\n")
		fmt.Printf("  to change as you enter more.\n")
	} else {
		fmt.Printf("  DISJOINT PARLAY SET — %d shot(s) at %s\n", len(set.Parlays), money(stake))
	}
	fmt.Printf("  No team appears twice and no game is used twice, so every ticket can\n")
	fmt.Printf("  be live at once without one hedging another.\n")
	fmt.Println()
	fmt.Printf("  %-38s %7s %7s %7s %9s\n", "legs", "price", "true p", "conv", "on "+money(stake))
	fmt.Printf("  %s\n", strings.Repeat("-", 74))
	converted := 0.0
	for _, p := range set.Parlays {
		// Conversion comes from the exact decimal, not the rounded ticket
		// price, so the dollar column is not moved by a display decision.
		fmt.Printf("  %-38s %7s %6.1f%% %6.1f%% %9s\n",
			strings.Join(p.Tickets(), " + "), price(p.Price),
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
	// With one bettable book the "best at" column has exactly one possible
	// value, so calling it shopping dresses a tautology as a choice. The
	// comparison against consensus is still worth having -- it is what caught
	// a fanatics +390 against a +455 consensus -- so it stays, renamed.
	if len(a.PricedBooks) < 2 {
		fmt.Printf("  %s vs CONSENSUS\n", strings.ToUpper(a.PricedBooks[0]))
		fmt.Printf("  Only %s has prices this week, so there is nothing to shop yet —\n",
			a.PricedBooks[0])
		fmt.Printf("  this is that book against the schedule's own number.\n")
		fmt.Printf("  %-6s %10s %10s %8s %9s\n", "side", "price", "consensus", "gap", "payout")
	} else {
		fmt.Printf("  LINE SHOPPING — best bettable price vs consensus (comparing %d books)\n",
			len(a.PricedBooks))
		fmt.Printf("  %-6s %10s %-11s %10s %8s %9s\n",
			"side", "best", "at", "consensus", "gap", "payout")
	}
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
		if len(a.PricedBooks) < 2 {
			fmt.Printf("  %-6s %10s %10s %8s %9s\n", r.Team, price(r.Best.Price), cons, gap, payout)
		} else {
			fmt.Printf("  %-6s %10s %-11s %10s %8s %9s\n",
				r.Team, price(r.Best.Price), r.Best.Book, cons, gap, payout)
		}
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

// coverageTable lists what each book actually has for a week, so the answer to
// "which book?" is on screen instead of being guessed at.
func coverageTable(doc *board.Doc, week int) string {
	cov := doc.Coverage()
	total := len(doc.Games)
	var b strings.Builder
	fmt.Fprintf(&b, "\n  week %02d has %d games. Prices on hand:\n\n", week, total)
	for _, bk := range board.Books {
		note := ""
		switch {
		case bk == board.Consensus:
			note = "  (reference only -- not a book you can bet)"
		case cov[bk] == 0:
			note = "  (nothing entered)"
		case cov[bk] < total:
			note = "  (partial)"
		}
		fmt.Fprintf(&b, "    %-12s %2d/%d%s\n", bk, cov[bk], total, note)
	}
	fmt.Fprintf(&b, "\n  e.g. edgectl board report -week %d -book fanatics\n", week)
	return b.String()
}

// splitCSV turns a comma list into trimmed, non-empty entries.
func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// printCommitments says which teams were removed and why.
//
// A derived exclusion has to be visible. Silently dropping half the board
// leaves the operator wondering why a team they can see priced never appears
// in a set, and the honest answers -- "you already bet it" and "the tool is
// broken" -- look identical from outside.
func printCommitments(c []Commitment, manual []string, ignored bool) {
	fmt.Println()
	if ignored {
		fmt.Printf("  COMMITMENTS: ignored (-ignore-log). Teams already carrying an open\n")
		fmt.Printf("  wager may be proposed again, which would double an existing position.\n")
		return
	}
	if len(c) == 0 && len(manual) == 0 {
		return
	}
	fmt.Printf("  EXCLUDED — already committed, or asked for\n")
	for _, x := range c {
		fmt.Printf("    %-14s %s\n", strings.Join(x.Teams, ","), x.Selection)
	}
	if len(manual) > 0 {
		fmt.Printf("    %-14s (-exclude)\n", strings.Join(manual, ","))
	}
	fmt.Printf("  Two tickets on one game are one chance counted twice, so a game with an\n")
	fmt.Printf("  open wager is off the board until it settles.\n")
}

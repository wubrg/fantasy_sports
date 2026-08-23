// Command edgectl computes wager math from operator-supplied prices.
//
// It does not fetch odds. There is no network code in this program and there
// is not meant to be: prices come from the operator, who is responsible for
// having read them off a book. The point of the tool is that the arithmetic
// happens here, in tested code, rather than in a language model that will
// produce a confident-looking number either way.
//
//	edgectl market -a -110 -b -110 -p 0.55
//	edgectl bonus  -odds +1000 -stake 25
package main

import (
	"flag"
	"fmt"
	"os"

	"edge/internal/wager"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "market":
		err = marketCmd(os.Args[2:])
	case "bonus":
		err = bonusCmd(os.Args[2:])
	case "card":
		err = cardCmd(os.Args[2:])
	case "hitrate":
		err = hitrateCmd(os.Args[2:])
	case "scenario":
		err = scenarioCmd(os.Args[2:])
	case "log":
		err = logCmd(os.Args[2:])
	case "hedge":
		err = hedgeCmd(os.Args[2:])
	case "board":
		err = boardCmd(os.Args[2:])
	case "ledger":
		err = ledgerCmd(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "edgectl: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "edgectl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `edgectl — wager math for operator-supplied prices

  edgectl market -a <american> -b <american> [-p <true prob>] [-stake <amount>]
        Both sides of a market. Reports the hurdle rate, the de-vigged fair
        value, overround and hold, and (with -p) EV for real money and bonus.

  edgectl bonus -odds <american> [-stake <amount>] [-p <true prob>]
        Bonus-bet (stake not returned) value. Without -p, reports the fair-odds
        conversion ceiling.

  edgectl card bonus [-target <rate>]
        The static bonus-bet reference card. Needs no data: a bonus bet is +EV
        at any price, so the floor depends only on your conversion target.

  edgectl hitrate -line <n> -side over|under -values <csv> [-price <american>]
        Empirical win probability from a game log, with a Wilson interval. With
        -price, returns a verdict: supported, unproven, or reject.

  edgectl card real -p <prob> [-lo <lower bound>] [-label <text>]
        Minimum acceptable price for a cash wager. With -lo, shows the
        threshold your uncertainty can actually support alongside the one your
        point estimate claims.

  edgectl scenario [-outcome receiving_yards|passing_yards] -total <n> -threshold <n>
                   -belief <p> -q <p> -r <p> -price <american>
        What you would have to believe for a wager to be +EV, against what the
        game line already implies. Add -rungs line:price:q:r,... for a ladder,
        or -log <path> to record the prediction. -side over|under picks the
        direction. -baseline is what the player
        NORMALLY does in the outcome's own units; the grid prices the line as
        a ratio to it, because that is what a book sets it near.

  edgectl hedge -face <amount> -back <american> -against <american>
        Convert a bonus bet to guaranteed cash by backing the other side at a
        DIFFERENT book. Reports the hedge stake, locked-in profit and the
        conversion rate. Phase 1 of the analytical-hobbyist framework: bank the
        asset first, then deploy cash into high-variance betting.

  edgectl board scaffold|validate [-dir <path>] [-season <year>] [-week <n>]
        The per-week line board: every game on the schedule, with a slot for
        each sportsbook. scaffold generates the week files from games.csv and
        never overwrites a price you have already entered; validate parses
        every cell and reports the malformed ones.

  edgectl board report -week <n> [-book <name>] [-stake <amount>] [-shots <n>]
        Read one week's board: de-vig every moneyline and flag implausible
        overrounds, rank the dogs by what a bonus bet actually converts, build
        disjoint two-leg parlays that share no team, and show where the best
        bettable price sits against consensus.

  edgectl board serve [-addr :8085] [-dir <path>]
        A phone-shaped form for typing prices into the board, scoped to one
        week, book and market at a time. Every field saves as it loses focus;
        there is no submit button and nothing to lose to a reload.

  edgectl board import -week <n> -book <name> [-file <path>] [-n]
        Backfill one week's moneylines from a blob copied off a book
        ("SF +150, LAR -150, ..."), read from stdin unless -file is given.
        Prints the diff; -n stops before writing.

  edgectl ledger add|balances|expiring [-file ~/bankroll.jsonl]
        The bankroll: what you hold, per book, and when each piece of it dies.
        Balances are replayed from the log rather than stored, so -as-of answers
        what you held on a past date. expiring is the one that earns its keep —
        every meaningful loss last campaign was a deadline, not a bad price.

  edgectl log list|settle|score -file <path>
        The calibration log. Predictions are recorded before the outcome and
        settled by appending, never by rewriting, so nothing can be re-predicted
        after the fact.

Every command documents its own flags -- "edgectl scenario -h", "edgectl
ledger add -h", "edgectl board report -h". This page is the map; those are
the manual.

edgectl never fetches prices and never guesses one. Bad input is an error, not
a zero.
`)
}

func marketCmd(args []string) error {
	fs := flag.NewFlagSet("market", flag.ExitOnError)
	a := fs.Int("a", 0, "American price for side A (required)")
	b := fs.Int("b", 0, "American price for side B (required, needed to measure vig)")
	p := fs.Float64("p", -1, "your true-probability estimate for side A")
	stake := fs.Float64("stake", 1, "stake")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *a == 0 || *b == 0 {
		return fmt.Errorf("both -a and -b are required: vig cannot be measured from one side")
	}

	m := wager.Market{A: wager.American(*a), B: wager.American(*b)}
	fairA, fairB, err := m.FairDevig()
	if err != nil {
		return err
	}
	over, err := m.Overround()
	if err != nil {
		return err
	}
	hold, err := m.Hold()
	if err != nil {
		return err
	}
	rawA, err := m.A.ImpliedRaw()
	if err != nil {
		return err
	}
	rawB, err := m.B.ImpliedRaw()
	if err != nil {
		return err
	}

	fmt.Printf("side       price   implied(raw)   fair(de-vig)\n")
	fmt.Printf("A        %+6d        %6.2f%%         %6.2f%%\n", *a, rawA*100, fairA*100)
	fmt.Printf("B        %+6d        %6.2f%%         %6.2f%%\n", *b, rawB*100, fairB*100)
	fmt.Printf("\novrround %6.2f pts   hold %6.2f%%\n", over*100, hold*100)
	fmt.Printf("hurdle   %6.2f%% on side A (raw implied — this includes the vig)\n", rawA*100)

	if *p < 0 {
		fmt.Printf("\nno -p supplied: EV not computed (a missing estimate is not zero)\n")
		return nil
	}

	r, err := wager.Report(m, *p, *stake)
	if err != nil {
		return err
	}
	fmt.Printf("\np_true   %6.2f%%   stake %.2f\n", r.PTrue*100, *stake)
	fmt.Printf("EV real  %+7.4f   EV bonus %+7.4f\n", r.EVReal, r.EVBonus)
	fmt.Printf("clears hurdle: %-5v   beats market: %v\n", r.ClearsHurdle, r.BeatsMarket)
	if r.ClearsHurdle && !r.BeatsMarket {
		fmt.Printf("note: clears the price but does not disagree with the market's own view\n")
	}
	if !r.ClearsHurdle && r.BeatsMarket {
		fmt.Printf("note: disagrees with the market but not by enough to beat the price\n")
	}
	fmt.Printf("\ncalibration: this EV is only as good as p_true. At +150 a 5-point\n")
	fmt.Printf("error in p_true moves EV by 12.5 points.\n")
	return nil
}

func bonusCmd(args []string) error {
	fs := flag.NewFlagSet("bonus", flag.ExitOnError)
	odds := fs.Int("odds", 0, "American price (required)")
	stake := fs.Float64("stake", 1, "bonus bet face value")
	p := fs.Float64("p", -1, "your true-probability estimate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *odds == 0 {
		return fmt.Errorf("-odds is required")
	}
	a := wager.American(*odds)

	implied, err := a.ImpliedRaw()
	if err != nil {
		return err
	}
	ceiling, err := wager.BonusConversionAtFairOdds(a, *stake)
	if err != nil {
		return err
	}

	fmt.Printf("price          %+d\n", *odds)
	fmt.Printf("face value     %.2f\n", *stake)
	fmt.Printf("win prob       %6.2f%%  (raw implied)\n", implied*100)
	fmt.Printf("fair ceiling   %.2f   (%.1f%% of face, = stake × (1−p))\n",
		ceiling, ceiling / *stake * 100)
	fmt.Printf("\nthis is a CEILING at fair odds. Real longshot markets are juiced;\n")
	fmt.Printf("realised conversion is typically 60-80%% of face value.\n")

	if *p >= 0 {
		ev, err := wager.EVBonusBet(*p, a, *stake)
		if err != nil {
			return err
		}
		real, err := wager.EVRealMoney(*p, a, *stake)
		if err != nil {
			return err
		}
		fmt.Printf("\np_true         %6.2f%%\n", *p*100)
		fmt.Printf("EV bonus       %+.4f\n", ev)
		fmt.Printf("EV if cash     %+.4f   (difference is the un-risked stake, (1−p)×stake)\n", real)
	}
	fmt.Printf("\nvariance: this wins %.1f%% of the time. Over a handful of bets per\n", implied*100)
	fmt.Printf("season the median outcome is zero regardless of EV.\n")
	return nil
}

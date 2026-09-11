package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"edge/internal/board"
	"edge/internal/ledger"
	"edge/internal/wager"
)

// defaultLedgerPath is where the bankroll log lives unless -file says otherwise.
// It sits in the home directory rather than in the repo because it is a private
// financial record, and the repo is a place things get committed by accident.
func defaultLedgerPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "bankroll.jsonl"
	}
	return filepath.Join(home, "bankroll.jsonl")
}

// shortLot fits a lot id into the column it is printed in. Ids are 41
// characters and the column was 20, so `expiring` -- the command whose whole
// job is being readable on a phone at a glance -- wrapped into soup.
func shortLot(id string) string {
	const w = 24
	if len(id) <= w {
		return id
	}
	return id[:w-1] + "\u2026"
}

func ledgerCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("ledger needs a mode: add, balances, expiring or period")
	}
	switch args[0] {
	case "add":
		return ledgerAdd(args[1:])
	case "balances":
		return ledgerBalances(args[1:])
	case "expiring":
		return ledgerExpiring(args[1:])
	case "period":
		return ledgerPeriod(args[1:])
	default:
		return fmt.Errorf("unknown ledger mode %q (want add, balances, expiring or period)", args[0])
	}
}

// parseWindow accepts "7d", "36h", "90m" and anything time.ParseDuration takes.
//
// The day suffix is here because every promo clock is quoted in days and Go's
// parser stops at hours. Writing -within 168h for "a week" is the kind of
// friction that stops a tool being used, and this tool is only worth anything if
// it gets run.
func parseWindow(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty window")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration (try 7d, 36h or 90m)", s)
	}
	return d, nil
}

// parseWhen accepts a date, a datetime, or an offset like "+3d".
//
// Expiries are read off a book's promo page in local time, so a bare date is
// interpreted locally rather than as UTC: recording "expires 2026-08-27" and
// having it mean 8pm the previous evening would defeat the point.
func parseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if strings.HasPrefix(s, "+") {
		d, err := parseWindow(strings.TrimPrefix(s, "+"))
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date I recognise (want 2026-08-27, 2026-08-27 20:00, an RFC3339 timestamp, or an offset like +3d)", s)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func ledgerAdd(args []string) error {
	fs := flag.NewFlagSet("ledger add", flag.ExitOnError)
	path := fs.String("file", defaultLedgerPath(), "path to the bankroll log")
	kind := fs.String("kind", "", "deposit, withdraw, grant, convert, place, settle or expire (required)")
	book := fs.String("book", "", "sportsbook holding the asset")
	asset := fs.String("asset", "", "asset type: cash, bonus, boost — or any other name you need")
	amount := fs.Float64("amount", 0, "amount of the asset")
	expiry := fs.String("expiry", "", "when the asset dies (2026-08-27, 2026-08-27 20:00, or +3d)")
	lot := fs.String("lot", "", "id of an existing lot to draw from (withdraw, convert, place, expire)")
	id := fs.String("id", "", "id for the lot this event creates (default: the event id)")
	wagerID := fs.String("wager", "", "wager id tying a place to its settle; use the betlog id")
	result := fs.String("result", "", "settle only: won, lost, push or void")
	returns := fs.Float64("returns", 0, "settle only: amount handed back by the book")
	returnsAsset := fs.String("returns-asset", ledger.Cash, "settle only: asset the returns arrive as")
	note := fs.String("note", "", "free-text note")

	boostPct := fs.Float64("boost-pct", 0, "grant a profit boost with this profit multiplier (0.5 = 50%)")
	boostMax := fs.Float64("boost-max", 0, "profit boost: maximum stake it applies to")
	boostMinOdds := fs.Int("boost-min-odds", 0, "profit boost: minimum American price it may be used on")
	boostCash := fs.Bool("boost-needs-cash", false, "profit boost: requires a real-money stake (it will not attach to a bonus bet)")
	nosweatMax := fs.Float64("nosweat-max", 0, "grant a no-sweat token: the stake it refunds (as a bonus) on a loss")
	nosweatMarket := fs.String("nosweat-market", "", "no-sweat token: the market it applies to (atd, ftd, any)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *kind == "" {
		return fmt.Errorf("-kind is required (deposit, withdraw, grant, convert, place, settle or expire)")
	}

	now := time.Now()
	k := ledger.Kind(strings.ToLower(strings.TrimSpace(*kind)))
	// The id carries a human label so a log read months later is skimmable. Book
	// first, then whatever else this kind actually names — a place with no -book
	// would otherwise mint "place-place", which tells a reader nothing.
	label := firstNonEmpty(*book, *wagerID, *lot, string(k))
	e := ledger.Event{
		Kind: k,
		ID:   ledger.NewID(now, string(k)+"-"+label),
		Time: now,
		Lot:  *lot,
		Note: *note,
	}

	// A lot descriptor, shared by the kinds that create one. Building it once
	// keeps deposit, grant and convert from drifting apart in what they accept.
	newLot := func() (*ledger.Lot, error) {
		l := ledger.Lot{ID: *id, Book: *book, Asset: *asset, Amount: *amount}
		if *expiry != "" {
			t, err := parseWhen(*expiry, now)
			if err != nil {
				return nil, err
			}
			l.Expires = &t
		}
		if *boostPct != 0 {
			l.Boost = &ledger.BoostSpec{
				Percent:           *boostPct,
				MaxStake:          *boostMax,
				MinOdds:           wager.American(*boostMinOdds),
				RequiresCashStake: *boostCash,
			}
			if l.Asset == "" {
				l.Asset = ledger.Boost
			}
		}
		if *nosweatMax > 0 || *nosweatMarket != "" {
			label := strings.TrimSpace("no-sweat " + *nosweatMarket)
			l.NoSweat = &ledger.NoSweatSpec{
				MaxStake: *nosweatMax,
				Market:   *nosweatMarket,
				Label:    label,
			}
			if l.Asset == "" {
				l.Asset = ledger.NoSweat
			}
		}
		return &l, nil
	}

	switch k {
	case ledger.KindDeposit, ledger.KindGrant, ledger.KindConvert:
		l, err := newLot()
		if err != nil {
			return err
		}
		e.Creates = l
		if k == ledger.KindConvert {
			// -amount describes the destination for a create, but a convert also
			// has to say how much of the source it takes. They are equal by the
			// conservation rule the package enforces, so one flag serves both and
			// there is no way to type a mismatch by accident.
			e.Amount = *amount
		}
	case ledger.KindWithdraw:
		e.Amount = *amount
	case ledger.KindPlace:
		e.Wager, e.Amount = *wagerID, *amount
	case ledger.KindExpire:
		// nothing further: an expiry takes whatever is left of the lot
	case ledger.KindSettle:
		e.Wager = *wagerID
		e.Result = ledger.Result(strings.ToLower(strings.TrimSpace(*result)))
		if *returns > 0 {
			if *book == "" {
				return fmt.Errorf("-book is required with -returns: proceeds land at a specific book")
			}
			e.Returns = &ledger.Lot{ID: *id, Book: *book, Asset: *returnsAsset, Amount: *returns}
		}
	}

	if err := e.Validate(); err != nil {
		return err
	}

	// Replay the log with the new event on the end before writing it. An
	// append-only file cannot take a line back, so the only safe moment to
	// discover that this event is impossible is before it is on disk.
	existing, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	if _, err := ledger.Balances(append(append([]ledger.Event{}, existing...), e), time.Time{}); err != nil {
		return fmt.Errorf("%w\n\nnothing was written. fix the event, or record the missing history first", err)
	}
	if err := ledger.AppendFile(*path, e); err != nil {
		return err
	}

	fmt.Printf("recorded %s %s\n", e.Kind, e.ID)
	if e.Creates != nil {
		lotID := e.Creates.ID
		if lotID == "" {
			lotID = e.ID
		}
		fmt.Printf("  lot %s — %s %s", lotID, e.Creates.Book, e.Creates.Asset)
		if e.Creates.Boost != nil {
			fmt.Printf(" %.0f%% boost", e.Creates.Boost.Percent*100)
		} else if ns := e.Creates.NoSweat; ns != nil {
			fmt.Printf(" no-sweat token")
			if ns.Market != "" {
				fmt.Printf(" (%s)", ns.Market)
			}
			if ns.MaxStake > 0 {
				fmt.Printf(", max %.0f", ns.MaxStake)
			}
		} else {
			fmt.Printf(" %.2f", e.Creates.Amount)
		}
		if e.Creates.Expires != nil {
			fmt.Printf(", expires %s", e.Creates.Expires.Local().Format("2006-01-02 15:04"))
		}
		fmt.Println()
		fmt.Printf("  use that lot id with -lot when you place, convert or expire it\n")
	}
	return nil
}

func ledgerBalances(args []string) error {
	fs := flag.NewFlagSet("ledger balances", flag.ExitOnError)
	path := fs.String("file", defaultLedgerPath(), "path to the bankroll log")
	asOf := fs.String("as-of", "", "derive the position as it stood at this date (default: now)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	events, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Printf("no events in %s\n", *path)
		fmt.Printf("record one with: edgectl ledger add -kind deposit -book fanatics -asset cash -amount 200\n")
		return nil
	}

	when := time.Time{}
	if *asOf != "" {
		when, err = parseWhen(*asOf, time.Now())
		if err != nil {
			return err
		}
	}
	pos, err := ledger.Balances(events, when)
	if err != nil {
		return err
	}

	if when.IsZero() {
		fmt.Printf("POSITION (all %d events)\n\n", len(events))
	} else {
		fmt.Printf("POSITION as of %s\n\n", when.Local().Format("2006-01-02 15:04"))
	}

	bal := pos.Balances()
	if len(bal) == 0 {
		fmt.Println("  nothing held")
	} else {
		fmt.Printf("  %-12s %-18s %12s %8s\n", "book", "asset", "amount", "units")
		fmt.Println("  " + strings.Repeat("-", 52))
		for _, b := range bal {
			amount := "—"
			if b.Amount != 0 {
				amount = fmt.Sprintf("%.2f", b.Amount)
			}
			units := "—"
			if b.Units != 0 {
				units = strconv.Itoa(b.Units)
			}
			fmt.Printf("  %-12s %-18s %12s %8s\n", b.Book, b.Asset, amount, units)
		}
		// Said out loud because a reader will otherwise try to add the columns.
		fmt.Printf("\n  amount and units are not addable. a boost is a right to a better price,\n")
		fmt.Printf("  not a sum of money, and pricing one on this table is how four of them\n")
		fmt.Printf("  got carried as $19.03 apiece and expired unused.\n")
	}

	if len(pos.Committed) > 0 {
		var total float64
		fmt.Printf("\nAT RISK (placed, not yet settled)\n\n")
		fmt.Printf("  %-24s %-12s %-14s %10s\n", "wager", "book", "asset", "amount")
		fmt.Println("  " + strings.Repeat("-", 64))
		for _, c := range pos.Committed {
			amount := fmt.Sprintf("%.2f", c.Amount)
			if c.Unit {
				amount = "1 unit"
			}
			fmt.Printf("  %-24s %-12s %-14s %10s\n", shortLot(c.Wager), c.Book, c.Asset, amount)
			total += c.Amount
		}
		fmt.Printf("\n  %.2f staked and unresolved. it is neither held nor lost, so it is not in\n", total)
		fmt.Printf("  the table above; settle these to move it one way or the other.\n")
	}
	return nil
}

func ledgerExpiring(args []string) error {
	fs := flag.NewFlagSet("ledger expiring", flag.ExitOnError)
	path := fs.String("file", defaultLedgerPath(), "path to the bankroll log")
	within := fs.String("within", "7d", "how far ahead to look (7d, 36h, 90m)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	window, err := parseWindow(*within)
	if err != nil {
		return err
	}

	events, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	now := time.Now()
	exp, err := ledger.Expiring(events, window, now)
	if err != nil {
		return err
	}

	fmt.Printf("EXPIRING WITHIN %s\n\n", *within)
	if len(exp) == 0 {
		fmt.Printf("  nothing held expires in the next %s.\n", *within)
		fmt.Printf("  assets with no recorded expiry never appear here, so an empty list is only\n")
		fmt.Printf("  as trustworthy as the -expiry values you entered.\n")
		return nil
	}

	fmt.Printf("  %-24s %-12s %-14s %10s  %s\n", "lot", "book", "asset", "value", "deadline")
	fmt.Println("  " + strings.Repeat("-", 76))
	overdue := 0
	for _, e := range exp {
		value := fmt.Sprintf("%.2f", e.Lot.Amount)
		if e.Lot.Boost != nil {
			value = fmt.Sprintf("%.0f%% boost", e.Lot.Boost.Percent*100)
		} else if e.Lot.NoSweat != nil {
			value = "no-sweat"
		}
		deadline := fmt.Sprintf("%s (%s)", e.At.Local().Format("Mon 2006-01-02 15:04"), humanLeft(e.In))
		if e.Expired() {
			overdue++
			deadline = fmt.Sprintf("%s (PAST — %s ago)", e.At.Local().Format("Mon 2006-01-02 15:04"), humanLeft(-e.In))
		}
		fmt.Printf("  %-24s %-12s %-14s %10s  %s\n", shortLot(e.Lot.ID), e.Lot.Book, e.Lot.Asset, value, deadline)
	}

	// The constraints that make a boost unusable are invisible in a value
	// column, and that invisibility is what cost real money.
	for _, e := range exp {
		b := e.Lot.Boost
		if b == nil {
			continue
		}
		fmt.Printf("\n  %s: %.0f%% boost", e.Lot.ID, b.Percent*100)
		if b.MaxStake > 0 {
			fmt.Printf(", max stake %.2f", b.MaxStake)
		}
		if b.MinOdds != 0 {
			fmt.Printf(", price %+d or longer", int(b.MinOdds))
		}
		fmt.Println()
		if b.RequiresCashStake {
			fmt.Printf("    requires a REAL-MONEY stake. it will not attach to a bonus bet, so it is\n")
			fmt.Printf("    worth nothing unless there is cash at %s to put behind it.\n", e.Lot.Book)
		}
	}

	if overdue > 0 {
		fmt.Printf("\n  %d lot(s) are past their deadline and still open in this log. either they\n", overdue)
		fmt.Printf("  were used and the placement was never recorded, or they died unused:\n")
		fmt.Printf("    edgectl ledger add -kind expire -lot <id>\n")
	}
	fmt.Printf("\n  every meaningful loss in the last campaign was a deadline, not a bad price.\n")
	return nil
}

// ledgerPeriod reports the bankroll's flows over a window: what a week cost or
// made, apart from what happens to be sitting in the book. It exists because the
// operator zeroes out to the bank weekly, so a point-in-time balance says nothing
// about how the week went -- only a sum of the week's flows can.
func ledgerPeriod(args []string) error {
	fs := flag.NewFlagSet("ledger period", flag.ExitOnError)
	path := fs.String("file", defaultLedgerPath(), "path to the bankroll log")
	week := fs.Int("week", 0, "NFL week to report; its window is read from the schedule")
	fromS := fs.String("from", "", "window start (date/datetime/RFC3339); overrides -week")
	toS := fs.String("to", "", "window end; use with -from")
	dir := fs.String("dir", defaultBoardDir, "directory of weekNN.yaml files, for -week")
	if err := fs.Parse(args); err != nil {
		return err
	}

	now := time.Now()
	var start, end time.Time
	var label string
	switch {
	case *fromS != "" || *toS != "":
		if *fromS == "" || *toS == "" {
			return fmt.Errorf("-from and -to must be given together")
		}
		var err error
		if start, err = parseWhen(*fromS, now); err != nil {
			return err
		}
		if end, err = parseWhen(*toS, now); err != nil {
			return err
		}
		label = "custom window"
	case *week > 0:
		var err error
		if start, end, err = weekWindow(*dir, *week); err != nil {
			return err
		}
		label = fmt.Sprintf("Week %d", *week)
	default:
		return fmt.Errorf("give -week N, or -from and -to together")
	}

	events, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	rep, err := ledger.Period(events, start, end)
	if err != nil {
		return err
	}
	printPeriod(rep, label)
	return nil
}

// weekWindow derives a reporting window for an NFL week from the schedule. It
// runs from the Tuesday on or before the week's first kickoff to the Tuesday on
// or before the next week's first kickoff: NFL weeks roll over on Tuesday, so a
// Wednesday deposit made to fund the slate is counted with the slate it funds.
// When the next week's file is missing (the final week loaded), the window is
// one week long.
func weekWindow(dir string, week int) (start, end time.Time, err error) {
	doc, err := loadWeekDoc(dir, week)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	first := doc.FirstKickoff()
	if first.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("week %d has no parseable kickoff in %s, so its window cannot be bounded", week, dir)
	}
	start = tuesdayOnOrBefore(first)

	if next, nerr := loadWeekDoc(dir, week+1); nerr == nil {
		if nf := next.FirstKickoff(); !nf.IsZero() {
			end = tuesdayOnOrBefore(nf)
		}
	}
	if end.IsZero() {
		end = start.AddDate(0, 0, 7)
	}
	return start, end, nil
}

func loadWeekDoc(dir string, week int) (*board.Doc, error) {
	f, err := os.Open(filepath.Join(dir, fmt.Sprintf("week%02d.yaml", week)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return board.Parse(f)
}

// tuesdayOnOrBefore returns local midnight on the Tuesday on or before t.
func tuesdayOnOrBefore(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	back := (int(day.Weekday()) - int(time.Tuesday) + 7) % 7
	return day.AddDate(0, 0, -back)
}

func printPeriod(r ledger.Report, label string) {
	fmt.Printf("PERIOD  %s  (%s → %s)\n\n",
		label,
		r.Start.Local().Format("Mon 2006-01-02"),
		r.End.Local().Format("Mon 2006-01-02"))

	fmt.Printf("  CAPITAL — real money moved with your bank\n")
	fmt.Printf("    deposited     %10.2f\n", r.Deposits)
	fmt.Printf("    withdrawn     %10.2f\n", r.Withdrawals)
	fmt.Printf("    net to bank   %10.2f\n\n", r.ExternalNet())

	fmt.Printf("  BETTING — realized on wagers settled this window\n")
	fmt.Printf("    cash          %10.2f\n", r.RealizedCash)
	fmt.Printf("    bonus won     %10.2f\n", r.RealizedBonus)
	fmt.Printf("    realized net  %10.2f\n\n", r.RealizedNet())

	fmt.Printf("  STAKED — committed on wagers placed this window\n")
	fmt.Printf("    cash          %10.2f\n", r.StakedCash)
	fmt.Printf("    bonus         %10.2f\n\n", r.StakedBonus)

	fmt.Printf("  OPEN AT PERIOD END — carried forward, not in the P&L above\n")
	fmt.Printf("    cash          %10.2f\n", r.OpenStakedCash)
	fmt.Printf("    bonus         %10.2f\n\n", r.OpenStakedBonus)

	// Reconciliation. On a book zeroed to the bank at both ends of the window,
	// net-to-bank equals realized net once open stake and parked bonus are
	// accounted for. A larger gap than those explain is a mis-logged event --
	// and because the log replays, it is findable rather than merely suspected.
	gap := r.ExternalNet() - r.RealizedNet()
	fmt.Printf("  RECONCILE\n")
	fmt.Printf("    net to bank %.2f  vs  realized net %.2f   (gap %.2f)\n", r.ExternalNet(), r.RealizedNet(), gap)
	fmt.Printf("    open stake (%.2f cash) and parked bonus explain a gap; anything past that\n", r.OpenStakedCash)
	fmt.Printf("    is a mis-logged event worth finding.\n")
}

// humanLeft renders a duration the way a deadline is actually read: hours when
// the answer is today, days when it is not. "142h13m" is technically the same
// information and useless at a glance.
func humanLeft(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

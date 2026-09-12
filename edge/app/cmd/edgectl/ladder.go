package main

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"edge/internal/wager"
)

// parseAmerican reads a "+150" / "-110" / "150" token into a price.
func parseAmerican(tok string) (wager.American, error) {
	tok = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tok), "+"))
	n, err := strconv.Atoi(tok)
	if err != nil {
		return 0, fmt.Errorf("%q is not an American price", tok)
	}
	return wager.American(n), nil
}

// parseFloatList reads "0.6,0.5,0.31" into floats. Empty string → nil.
func parseFloatList(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]float64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p)
		}
		out[i] = v
	}
	return out, nil
}

// ladderCmd stakes a nested milestone ladder and prints the payoff band-by-band.
func ladderCmd(args []string) error {
	fs := flag.NewFlagSet("ladder", flag.ExitOnError)
	rungsFlag := fs.String("rungs", "", `the rungs' prices, most-probable first: "-153,-101,+217,+444" (labels optional as price:label)`)
	stake := fs.Float64("stake", 0, "total stake to allocate (required)")
	mode := fs.String("mode", "free-roll", "free-roll, flat, weighted, or max-ev")
	weights := fs.String("weights", "", `weighted mode: one weight per rung, e.g. "4:3:2:1"`)
	pFlag := fs.String("p", "", `optional true probabilities per rung, most-probable first: "0.60,0.50,0.31,0.18"`)
	floor := fs.Int("floor", 1, "free-roll: which rung (1-based) recovers the stake")
	rollOn := fs.Int("roll-on", 0, "free-roll: which rung (1-based) the remainder rides (default: the top rung)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stake <= 0 {
		return fmt.Errorf("-stake is required and must be positive")
	}
	rungs, labels, err := parseLadderRungs(*rungsFlag)
	if err != nil {
		return err
	}
	probs, err := parseFloatList(*pFlag)
	if err != nil {
		return fmt.Errorf("-p: %w", err)
	}

	var plan *wager.LadderPlan
	switch *mode {
	case "free-roll":
		rollIdx := *rollOn - 1
		if *rollOn == 0 {
			rollIdx = len(rungs) - 1
		}
		plan, err = wager.FreeRoll(rungs, *stake, *floor-1, rollIdx)
	case "flat":
		plan, err = wager.Flat(rungs, *stake)
	case "weighted":
		var ws []float64
		ws, err = parseWeights(*weights, len(rungs))
		if err != nil {
			return err
		}
		plan, err = wager.Weighted(rungs, *stake, ws)
	case "max-ev":
		if probs == nil {
			return fmt.Errorf("max-ev needs -p (true probabilities); without a belief there is no EV to maximise")
		}
		plan, err = wager.MaxEV(rungs, *stake, probs)
	default:
		return fmt.Errorf("unknown -mode %q (want free-roll, flat, weighted or max-ev)", *mode)
	}
	if err != nil {
		return err
	}
	if probs != nil && !plan.HasEV {
		if plan, err = wager.WithProbs(plan, probs); err != nil {
			return err
		}
	}

	printLadder(plan, labels, *mode)
	return nil
}

func parseWeights(s string, n int) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("weighted mode needs -weights")
	}
	parts := strings.Split(s, ":")
	if len(parts) != n {
		return nil, fmt.Errorf("got %d weights for %d rungs", len(parts), n)
	}
	out := make([]float64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("weight %q is not a number", p)
		}
		out[i] = v
	}
	return out, nil
}

// parseRungs reads "price[:label],..." into rungs and their labels.
func parseLadderRungs(s string) ([]wager.LadderRung, []string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil, fmt.Errorf("-rungs is required")
	}
	parts := strings.Split(s, ",")
	rungs := make([]wager.LadderRung, 0, len(parts))
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.SplitN(strings.TrimSpace(part), ":", 2)
		price, err := parseAmerican(fields[0])
		if err != nil {
			return nil, nil, err
		}
		label := ""
		if len(fields) == 2 {
			label = strings.TrimSpace(fields[1])
		}
		rungs = append(rungs, wager.LadderRung{Price: price, Label: label})
		labels = append(labels, label)
	}
	return rungs, labels, nil
}

func printLadder(p *wager.LadderPlan, labels []string, mode string) {
	fmt.Printf("LADDER  %s   (%d rungs, $%.2f total)\n\n", mode, len(p.Rungs), p.Total)
	fmt.Printf("  %-4s %-8s %-10s %8s\n", "rung", "price", "stake", "decimal")
	fmt.Println("  " + strings.Repeat("-", 34))
	for i, r := range p.Rungs {
		d, _ := r.Price.Decimal()
		lbl := ""
		if labels[i] != "" {
			lbl = "  " + labels[i]
		}
		fmt.Printf("  %-4d %-8s %10.2f %8.3f%s\n", i+1, fmt.Sprintf("%+d", int(r.Price)), p.Stakes[i], d, lbl)
	}
	if p.FloorRecovers > 0 {
		fmt.Printf("\n  free-roll: %.2f on rung %d returns the full %.2f stake; %.2f rides the rung(s) above it.\n",
			p.FloorRecovers, floorIndex(p), p.Total, p.Total-p.FloorRecovers)
	}

	fmt.Printf("\n  PAYOFF BY OUTCOME  (the rungs nest — clearing one clears all below)\n")
	hasProb := len(p.Bands) > 0 && p.Bands[0].HasProb
	if hasProb {
		fmt.Printf("  %-14s %10s %10s %8s\n", "cleared", "return", "net", "prob")
	} else {
		fmt.Printf("  %-14s %10s %10s\n", "cleared", "return", "net")
	}
	fmt.Println("  " + strings.Repeat("-", 46))
	for _, b := range p.Bands {
		var name string
		switch b.ClearedThrough {
		case 0:
			name = "none"
		case len(p.Rungs):
			name = "all"
		default:
			name = fmt.Sprintf("through %d", b.ClearedThrough)
		}
		if hasProb {
			fmt.Printf("  %-14s %10.2f %+10.2f %7.1f%%\n", name, b.Return, b.Net, b.Prob*100)
		} else {
			fmt.Printf("  %-14s %10.2f %+10.2f\n", name, b.Return, b.Net)
		}
	}

	if p.HasEV {
		fmt.Printf("\n  EV %+.2f    P(net ≥ 0) %.1f%%    P(profit) %.1f%%\n", p.EV, p.PNonNeg*100, p.PProfit*100)
	}
	fmt.Printf("\n  NOTE: splitting a ladder does not change EV — it shapes the payoff. A\n")
	fmt.Printf("  free roll is only free if the floor hits; the miss-all band loses it all.\n")
}

func floorIndex(p *wager.LadderPlan) int {
	for i, s := range p.Stakes {
		if s > 0 {
			return i + 1
		}
	}
	return 1
}

// ---- parlay / round robin ------------------------------------------------

type parlayLeg struct {
	price wager.American
	game  string
	label string
}

// parlayCmd prices a parlay or round robin of INDEPENDENT (cross-game) legs.
func parlayCmd(args []string) error {
	fs := flag.NewFlagSet("parlay", flag.ExitOnError)
	legsFlag := fs.String("legs", "", `legs as price:game[:label], comma-separated: "-110:mia@lv:LV -3,+250:was@phi:McLaurin ATD"`)
	rr := fs.String("rr", "", `round-robin combo size(s), e.g. "2" or "2,3" (default: one parlay of all legs)`)
	total := fs.Float64("total", 0, "total stake, split equally across combos (required)")
	pFlag := fs.String("p", "", `optional true probabilities per leg: "0.55,0.40,0.30"`)
	book := fs.Int("book", 0, "optional: the book's offered combined price, to compare (single-combo only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *total <= 0 {
		return fmt.Errorf("-total is required and must be positive")
	}
	legs, err := parseLegs(*legsFlag)
	if err != nil {
		return err
	}
	if err := guardSameGame(legs); err != nil {
		return err
	}
	prices := make([]wager.American, len(legs))
	for i, l := range legs {
		prices[i] = l.price
	}
	probs, err := parseFloatList(*pFlag)
	if err != nil {
		return fmt.Errorf("-p: %w", err)
	}

	sizes, err := parseSizes(*rr, len(legs))
	if err != nil {
		return err
	}
	combos, err := wager.RoundRobin(prices, sizes)
	if err != nil {
		return err
	}
	ticket, err := wager.BuildTicket(prices, combos, *total, probs)
	if err != nil {
		return err
	}

	printParlay(legs, ticket, *book)
	return nil
}

func parseSizes(s string, nLegs int) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []int{nLegs}, nil // one parlay of all legs
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("-rr %q is not a number", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func parseLegs(s string) ([]parlayLeg, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("-legs is required")
	}
	parts := strings.Split(s, ",")
	legs := make([]parlayLeg, 0, len(parts))
	for _, part := range parts {
		fields := strings.SplitN(strings.TrimSpace(part), ":", 3)
		price, err := parseAmerican(fields[0])
		if err != nil {
			return nil, err
		}
		l := parlayLeg{price: price}
		if len(fields) >= 2 {
			l.game = strings.TrimSpace(fields[1])
		}
		if len(fields) == 3 {
			l.label = strings.TrimSpace(fields[2])
		}
		legs = append(legs, l)
	}
	return legs, nil
}

// guardSameGame is the correlation firewall: parlay math assumes independent
// legs, which holds only across different games. Two legs sharing a game tag are
// correlated and must be built as an SGP at the book, so this refuses them.
func guardSameGame(legs []parlayLeg) error {
	seen := map[string]int{}
	for i, l := range legs {
		if l.game == "" {
			return fmt.Errorf("leg %d has no game tag; every leg needs price:game[:label] so same-game (correlated) legs can be rejected", i+1)
		}
		if j, ok := seen[l.game]; ok {
			return fmt.Errorf(
				"legs %d and %d are both in game %q — they are correlated (one game state), not independent. Build that as an SGP at the book and price the ONE combined number; this tool multiplies independent legs only",
				j+1, i+1, l.game)
		}
		seen[l.game] = i
	}
	return nil
}

func printParlay(legs []parlayLeg, t *wager.Ticket, book int) {
	fmt.Printf("PARLAY / ROUND ROBIN  (%d legs, %d combo(s), $%.2f total)\n\n", len(legs), len(t.Combos), t.TotalRisk)
	fmt.Printf("  legs:\n")
	for i, l := range legs {
		lbl := l.label
		if lbl != "" {
			lbl = "  " + lbl
		}
		fmt.Printf("    [%d] %+d  %s%s\n", i+1, int(l.price), l.game, lbl)
	}
	fmt.Printf("\n  %-14s %-8s %8s %8s %12s", "combo", "price", "decimal", "stake", "payout(win)")
	if t.HasEV {
		fmt.Printf(" %8s %9s", "winP", "EV")
	}
	fmt.Println()
	fmt.Println("  " + strings.Repeat("-", 70))
	for _, c := range t.Combos {
		name := comboName(c.Legs)
		payout := c.Stake * c.Decimal
		fmt.Printf("  %-14s %-8s %8.2f %8.2f %12.2f", name, fmt.Sprintf("%+d", int(c.Price)), c.Decimal, c.Stake, payout)
		if t.HasEV {
			fmt.Printf(" %7.1f%% %+9.4f", c.WinProb*100, c.EV)
		}
		fmt.Println()
	}

	if t.HasEV {
		fmt.Printf("\n  TICKET  EV %+.2f    P(any cashes) %.1f%%    P(profit) %.1f%%\n", t.EV, t.PAny*100, t.PProfit*100)
		fmt.Printf("\n  outcome by legs won:\n")
		var keys []int
		for k := range t.Dist {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			d := t.Dist[k]
			if d.Prob < 1e-9 {
				continue
			}
			fmt.Printf("    %d of %d win   P %5.1f%%   E[net] %+8.2f\n", k, len(legs), d.Prob*100, d.NetSum)
		}
	}

	if book != 0 && len(t.Combos) == 1 {
		bookImpl, _ := wager.American(book).ImpliedRaw()
		fmt.Printf("\n  book offers %+d on the combined parlay (implied %.1f%%); the independence-fair\n", book, bookImpl*100)
		fmt.Printf("  price is %+d (implied %.1f%%). ", int(t.Combos[0].Price), t.Combos[0].Implied*100)
		if book > int(t.Combos[0].Price) {
			fmt.Printf("The book pays MORE than fair — a positive gap.\n")
		} else {
			fmt.Printf("The book pays no better than fair.\n")
		}
	}

	fmt.Printf("\n  NOTE: legs are treated as INDEPENDENT (probabilities multiply). Same-game\n")
	fmt.Printf("  legs are correlated — build those as an SGP at the book, not here.\n")
}

func comboName(idx []int) string {
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = strconv.Itoa(v + 1)
	}
	return strings.Join(parts, "+")
}

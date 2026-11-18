package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"edge/internal/wager"
)

// sgp-scenario prices a scenario-tuned same-game parlay: 5-6 "scenario floor"
// legs that are near-locks IF one game script happens, combined toward ~+1000.
//
// `parlay -p` multiplies leg probabilities as if they were independent, which
// is exactly wrong for legs chosen because one scenario lifts all of them. Here
// the legs are treated as conditionally independent GIVEN the scenario, so
//
//	P(all) = s * prod(q_i) + (1 - s) * prod(r_i)
//
// where q_i / r_i are each leg's fitted P(hit | scenario) / P(hit | not), read
// off `edgectl scenario`, and s is the week's belief-forecast row. The
// correlation the book charges for lives in the gap between that mixture and
// the naive product of leg prices.

type sgpLeg struct {
	q, r  float64
	price wager.American
	label string
}

type sgpResult struct {
	pIn, pOut, pAll float64
	naiveDec        float64 // product of the legs' own decimal prices
	fair            wager.American
}

func parseSGPLegs(s string) ([]sgpLeg, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("-legs is required")
	}
	var legs []sgpLeg
	for _, raw := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(raw), ":", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("leg %q: want q:r:price[:label]", raw)
		}
		q, err1 := strconv.ParseFloat(parts[0], 64)
		r, err2 := strconv.ParseFloat(parts[1], 64)
		p, err3 := strconv.Atoi(strings.TrimPrefix(parts[2], "+"))
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("leg %q: q and r must be probabilities, price an american integer", raw)
		}
		if q <= 0 || q >= 1 || r <= 0 || r >= 1 {
			return nil, fmt.Errorf("leg %q: q and r must be strictly between 0 and 1", raw)
		}
		leg := sgpLeg{q: q, r: r, price: wager.American(p)}
		if _, err := leg.price.Decimal(); err != nil {
			return nil, fmt.Errorf("leg %q: %w", raw, err)
		}
		if len(parts) == 4 {
			leg.label = parts[3]
		} else {
			leg.label = fmt.Sprintf("leg %d", len(legs)+1)
		}
		legs = append(legs, leg)
	}
	return legs, nil
}

func priceSGPScenario(s float64, legs []sgpLeg) (sgpResult, error) {
	res := sgpResult{pIn: 1, pOut: 1, naiveDec: 1}
	for _, l := range legs {
		res.pIn *= l.q
		res.pOut *= l.r
		d, err := l.price.Decimal()
		if err != nil {
			return res, err
		}
		res.naiveDec *= d
	}
	res.pAll = s*res.pIn + (1-s)*res.pOut
	fair, err := wager.AmericanFromDecimal(1 / res.pAll)
	if err != nil {
		return res, err
	}
	res.fair = fair
	return res, nil
}

func sgpScenarioCmd(args []string) error {
	fs := flag.NewFlagSet("sgp-scenario", flag.ExitOnError)
	belief := fs.Float64("belief", -1, "P(scenario): the matching row of the week's belief forecast (required)")
	name := fs.String("name", "", "the scenario, for the header (e.g. blowout_loss IND)")
	legsFlag := fs.String("legs", "", `legs as q:r:price[:label], comma-separated, q/r from edgectl scenario for THAT leg and side: "0.82:0.55:-200:Mixon rush 50+,..."`)
	book := fs.Int("book", 0, "optional: the book's combined SGP price, read at the book")
	stake := fs.Float64("stake", 1, "stake, for EV")
	target := fs.Int("target", 1000, "the price band the parlay is tuned toward")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *belief <= 0 || *belief >= 1 {
		return fmt.Errorf("-belief is required: the forecast row's P(scenario), strictly between 0 and 1")
	}
	legs, err := parseSGPLegs(*legsFlag)
	if err != nil {
		return err
	}
	if len(legs) < 3 || len(legs) > 8 {
		return fmt.Errorf("got %d legs: a scenario SGP takes 3 to 8 (5-6 is the design)", len(legs))
	}
	res, err := priceSGPScenario(*belief, legs)
	if err != nil {
		return err
	}

	fmt.Printf("SCENARIO SGP  %s   s = %.1f%%   %d legs\n\n", *name, 100**belief, len(legs))
	fmt.Printf("  %-34s %7s  %6s  %6s  %6s\n", "leg", "price", "q", "r", "lift")
	var flat []string
	for _, l := range legs {
		lift := l.q - l.r
		mark := ""
		if lift <= 0 {
			mark = "  <- scenario does NOT lift this leg"
			flat = append(flat, l.label)
		}
		fmt.Printf("  %-34s %+7d  %5.1f%%  %5.1f%%  %+5.1f%s\n", l.label, int(l.price), 100*l.q, 100*l.r, 100*lift, mark)
	}
	naive, err := wager.AmericanFromDecimal(res.naiveDec)
	if err != nil {
		return err
	}
	fmt.Printf("\n  P(all | scenario)      %6.2f%%\n", 100*res.pIn)
	fmt.Printf("  P(all | not scenario)  %6.2f%%\n", 100*res.pOut)
	fmt.Printf("  P(all)  = s*in + (1-s)*out   %6.2f%%\n", 100*res.pAll)
	fmt.Printf("  fair price (min to bet)      %+d\n", int(res.fair))
	fmt.Printf("  legs' prices multiplied      %+d   (independent-legs reference; the book's SGP will be shorter)\n", int(naive))
	if int(naive) < *target/2 || int(naive) > *target*2 {
		fmt.Printf("  NOTE  legs multiply to %+d, outside the ~%+d design band: retune rungs\n", int(naive), *target)
	}
	if len(legs) < 5 || len(legs) > 6 {
		fmt.Printf("  NOTE  %d legs; the design is 5-6\n", len(legs))
	}
	if len(flat) > 0 {
		fmt.Printf("  WARNING  %d leg(s) not lifted by the scenario: %s -- swap them out\n", len(flat), strings.Join(flat, "; "))
	}

	if *book != 0 {
		bp := wager.American(*book)
		dec, err := bp.Decimal()
		if err != nil {
			return fmt.Errorf("-book: %w", err)
		}
		be, _ := bp.ImpliedRaw()
		ev := *stake * (res.pAll*dec - 1)
		fmt.Printf("\n  book SGP %+d   breakeven %.2f%%   our P(all) %.2f%%\n", *book, 100*be, 100*res.pAll)
		fmt.Printf("  EV (stake %.2f) ... %+.4f\n", *stake, ev)
		verdict := "PASS  the book's price is shorter than our fair price"
		if res.pAll > be {
			verdict = "PLAYABLE  the book's price is longer than our fair price"
		}
		fmt.Printf("  VERDICT  %s\n", verdict)
	} else {
		fmt.Printf("\n  No -book price: read the combined SGP at the book; bet only at %+d or longer.\n", int(res.fair))
	}
	fmt.Printf("\n  RELIANCE  if the scenario fails this hits %.2f%%; the whole edge is the scenario call.\n", 100*res.pOut)
	return nil
}

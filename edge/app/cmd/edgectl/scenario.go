package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"edge/internal/betlog"
	"edge/internal/scenario"
	"edge/internal/wager"
)

func scenarioCmd(args []string) error {
	fs := flag.NewFlagSet("scenario", flag.ExitOnError)
	total := fs.Float64("total", 0, "posted game total, to derive the market's scenario probability")
	spread := fs.Float64("spread", 0, "favourite's spread, e.g. -3 (use with -basis margin)")
	basis := fs.String("basis", "total", "what the threshold measures: total or margin")
	threshold := fs.Float64("threshold", 0, "scenario threshold (combined points, or final margin)")
	sigma := fs.Float64("sigma", 0, "override the dispersion; 0 uses the documented default")
	sMarket := fs.Float64("smarket", -1, "market scenario probability, overriding the derived one")
	belief := fs.Float64("belief", -1, "your scenario probability (required)")
	q := fs.Float64("q", -1, "P(hit | scenario) (required)")
	r := fs.Float64("r", -1, "P(hit | no scenario) (required)")
	price := fs.Int("price", 0, "offered American price (required)")
	stake := fs.Float64("stake", 1, "stake")
	bankroll := fs.String("bankroll", "real", "real or bonus")
	label := fs.String("label", "", "what this wager is")
	name := fs.String("name", "scenario", "name of the scenario")
	logPath := fs.String("log", "", "append this wager to a bet log at this path")
	rungs := fs.String("rungs", "", "ladder as line:price:q:r,... (replaces -price/-q/-r)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *belief < 0 {
		return fmt.Errorf("-belief is required: this tool prices your read, it does not supply one")
	}

	b, err := parseBankroll(*bankroll)
	if err != nil {
		return err
	}
	basisVal, err := parseBasis(*basis)
	if err != nil {
		return err
	}

	// Establish the market's view of the scenario.
	sc, err := marketScenario(*name, basisVal, *total, *spread, *threshold, *sigma, *sMarket)
	if err != nil {
		return err
	}

	fmt.Printf("SCENARIO  %s\n", sc)
	if sc.Source == scenario.DerivedFromLine {
		switch basisVal {
		case scenario.Total:
			fmt.Printf("  from a posted total of %.1f (sigma %.1f)\n",
				*total, orDefault(*sigma, scenario.DefaultSigmaTotal))
		case scenario.Margin:
			fmt.Printf("  from a spread of %+.1f (sigma %.1f)\n",
				*spread, orDefault(*sigma, scenario.DefaultSigmaMargin))
		}
	}
	fmt.Printf("  market says %.1f%%   you say %.1f%%   (you are %+.1f points apart)\n\n",
		sc.Prob*100, *belief*100, (*belief-sc.Prob)*100)

	if *rungs != "" {
		return ladderReport(*rungs, sc.Prob, *belief, b, *stake)
	}

	if *price == 0 || *q < 0 || *r < 0 {
		return fmt.Errorf("-price, -q and -r are all required (or use -rungs)")
	}

	req, err := wager.RequiredScenarioProb(wager.American(*price), *q, *r)
	if err != nil {
		return err
	}
	class, err := wager.Classify(req, sc.Prob, *belief)
	if err != nil {
		return err
	}
	predicted, err := wager.BlendedProb(*belief, *q, *r)
	if err != nil {
		return err
	}
	ev, err := wager.BlendedEV(b, *belief, *q, *r, wager.American(*price), *stake)
	if err != nil {
		return err
	}

	if *label != "" {
		fmt.Printf("%s\n", *label)
	}
	fmt.Printf("  price %+d   hurdle %.1f%%   q %.1f%%   r %.1f%%\n",
		*price, req.Breakeven*100, *q*100, *r*100)
	fmt.Println()

	printRequirement(req)
	fmt.Printf("  your blended P(hit) ... %.1f%%\n", predicted*100)
	fmt.Printf("  EV (%s, stake %.2f) ... %+.4f\n", b, *stake, ev)
	fmt.Println()
	printClassification(class)

	s := req.Sensitivity()
	fmt.Println()
	fmt.Printf("  SENSITIVITY  s* moves %+.2f pts per point of q, %+.2f per point of r\n",
		s.PerPointOfQ, s.PerPointOfR)
	fmt.Printf("  the conclusion is driven most by %s\n", s.Dominant())

	if *logPath != "" {
		id, err := betlog.PlaceBet(*logPath, betlog.Bet{
			Selection: pick(*label, "unlabelled"), Price: wager.American(*price),
			Bankroll: b.String(), Stake: *stake,
			Scenario: sc.Name, ScenarioSource: sc.Source.String(),
			SMarket: sc.Prob, SYours: *belief, Q: *q, R: *r,
			SStar: req.Required, EdgeSource: class.Source.String(), Predicted: predicted,
		})
		if err != nil {
			return err
		}
		fmt.Printf("\n  logged as %s\n", id)
	}
	return nil
}

func printRequirement(req wager.BeliefRequirement) {
	switch {
	case req.AlwaysClears:
		fmt.Printf("  REQUIRES  nothing — the baseline alone beats this price.\n")
		fmt.Printf("            the narrative is not carrying this bet; check r.\n")
	case req.Unreachable:
		fmt.Printf("  REQUIRES  more than certainty — even s=100%% fails to clear %+.1f%%.\n",
			req.Breakeven*100)
	case req.AtLeast:
		fmt.Printf("  REQUIRES  believing the scenario is at least %.1f%% likely\n", req.Required*100)
	default:
		fmt.Printf("  REQUIRES  believing the scenario is at most %.1f%% likely\n", req.Required*100)
		fmt.Printf("            (the scenario hurts this wager, so less is better)\n")
	}
}

func printClassification(c wager.Classification) {
	fmt.Printf("  VERDICT   %s\n", strings.ToUpper(c.Source.String()))
	switch c.Source {
	case wager.MarketAlone:
		fmt.Printf("  the game line already implies enough. This is +EV on the market's\n")
		fmt.Printf("  own view, which usually means the prop and the game line disagree —\n")
		fmt.Printf("  a real edge, but also the likeliest place to have mis-set q or r.\n")
	case wager.DisagreementRequired:
		fmt.Printf("  your read is what carries this. Margin over the requirement: %+.1f pts\n",
			c.Margin()*100)
	case wager.BeyondYourRead:
		fmt.Printf("  not even your own belief justifies this price (short by %.1f pts)\n",
			-c.Margin()*100)
	}
}

func ladderReport(spec string, sMarket, belief float64, b wager.Bankroll, stake float64) error {
	rungs, err := parseRungs(spec)
	if err != nil {
		return err
	}
	steps, err := wager.AnalyzeLadder(rungs, sMarket, belief)
	if err != nil {
		return err
	}

	fmt.Printf("  %-8s %8s %9s %9s %10s   %s\n", "line", "price", "requires", "margin", "EV", "verdict")
	fmt.Printf("  %s\n", strings.Repeat("-", 68))
	for _, s := range steps {
		if s.Err != nil {
			fmt.Printf("  %-8.1f %8s  %s\n", s.Line, fmt.Sprintf("%+d", s.Price), s.Err)
			continue
		}
		ev, err := wager.BlendedEV(b, belief, s.Q, s.R, s.Price, stake)
		if err != nil {
			return err
		}
		req := "n/a"
		if !s.Requirement.AlwaysClears && !s.Requirement.Unreachable {
			req = fmt.Sprintf("%.1f%%", s.Requirement.Required*100)
		}
		fmt.Printf("  %-8.1f %8s %9s %+8.1f%% %+10.4f   %s\n",
			s.Line, fmt.Sprintf("%+d", s.Price), req,
			s.Classification.Margin()*100, ev, s.Classification.Source)
	}

	if idx, ok := wager.BestRung(steps); ok {
		best := steps[idx]
		fmt.Printf("\n  most room: %.1f at %+d (%.1f points of margin)\n",
			best.Line, best.Price, best.Classification.Margin()*100)
		fmt.Printf("  ranked by margin, not EV: at these sample sizes the ordering of\n")
		fmt.Printf("  requirements is far more trustworthy than the probability levels.\n")
	} else {
		fmt.Printf("\n  no rung clears your own belief.\n")
	}
	return nil
}

func marketScenario(name string, basis scenario.Basis, total, spread, threshold, sigma, override float64) (scenario.Scenario, error) {
	if override >= 0 {
		return scenario.StateProb(name, basis, threshold, override)
	}
	switch basis {
	case scenario.Total:
		if total <= 0 {
			return scenario.Scenario{}, fmt.Errorf(
				"need -total to derive a total-based scenario (or pass -smarket)")
		}
		return scenario.FromTotal(name, total, threshold, sigma)
	default:
		return scenario.FromSpread(name, spread, threshold, sigma)
	}
}

func parseBasis(s string) (scenario.Basis, error) {
	switch strings.ToLower(s) {
	case "total", "t":
		return scenario.Total, nil
	case "margin", "m":
		return scenario.Margin, nil
	}
	return scenario.Total, fmt.Errorf("-basis must be total or margin, got %q", s)
}

func parseBankroll(s string) (wager.Bankroll, error) {
	switch strings.ToLower(s) {
	case "real", "cash", "real money":
		return wager.RealMoney, nil
	case "bonus", "bonus bet", "snr":
		return wager.BonusBet, nil
	}
	return wager.RealMoney, fmt.Errorf("-bankroll must be real or bonus, got %q", s)
}

// parseRungs reads "line:price:q:r,line:price:q:r".
func parseRungs(spec string) ([]wager.Rung, error) {
	var out []wager.Rung
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := strings.Split(part, ":")
		if len(f) != 4 {
			return nil, fmt.Errorf("rung %q: want line:price:q:r", part)
		}
		nums := make([]float64, 4)
		for i, s := range f {
			v, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(s), "+"), 64)
			if err != nil {
				return nil, fmt.Errorf("rung %q: %q is not a number", part, s)
			}
			nums[i] = v
		}
		out = append(out, wager.Rung{
			Line: nums[0], Price: wager.American(int(nums[1])), Q: nums[2], R: nums[3],
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no rungs parsed from %q", spec)
	}
	return out, nil
}

func orDefault(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

func pick(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

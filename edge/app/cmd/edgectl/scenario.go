package main

import (
	"errors"
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
	basis := fs.String("basis", "total",
		"what the scenario's threshold measures: "+scenario.BasisNames())
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
	outcome := fs.String("outcome", "receiving_yards",
		"what the prop measures: "+outcomeNames())
	baseline := fs.Float64("baseline", 0,
		"what this player NORMALLY does: his prior mean in the outcome's own units "+
			"(e.g. 52 for a 52-yards-a-game receiver). The grid prices the line as a "+
			"ratio to this, because that is what a book sets it near")
	trend := fs.Float64("trend", 0, "two-game target-share trend, e.g. 0.06 for +6 points")
	line := fs.Float64("line", 0, "the prop line in yards, required when looking up q and r")
	side := fs.String("side", "over",
		"over or under. The grid fits one direction and reads the other off the same "+
			"cell; -q/-r, if you state them, are taken as already describing the side "+
			"you are pricing")
	confidence := fs.Float64("confidence", 0.95, "confidence level for looked-up intervals")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// A flag default of 0 is indistinguishable from "-spread 0", which is a
	// legitimate pick'em. Without this, omitting -spread entirely produced a
	// fabricated pick'em, printed "market says 19.1%", labelled it
	// derived-from-line and wrote it to the bet log as s_market. Track what was
	// actually supplied instead of inferring it from the value.
	supplied := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { supplied[f.Name] = true })

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
	if basisVal == scenario.Margin && !supplied["spread"] && *sMarket < 0 {
		return fmt.Errorf(
			"-basis margin needs an explicit -spread (or -smarket): without one the market's " +
				"view would be a fabricated pick'em, reported as if it came from a real line")
	}
	if basisVal == scenario.Total && !supplied["total"] && *sMarket < 0 {
		return fmt.Errorf("-basis total needs an explicit -total (or -smarket)")
	}
	if basisVal.NeedsStatedProbability() && *sMarket < 0 {
		return fmt.Errorf(
			"-basis %s has no fitted residual distribution, so its probability cannot be "+
				"derived from a line -- books do not price a team's pass rate over expected.\n"+
				"  Supply -smarket with your own estimate of how likely the scenario is",
			basisVal)
	}
	// Check the grid's definition before printing anything. The failure this
	// guards against is a header reading "shootout (total > 65.0)" sitting
	// directly above q and r that mean total > 50 -- so emitting the header
	// first and erroring after would print the contradiction on the way past.
	//
	// Only when the grid will actually be consulted: stated -q/-r and -rungs
	// carry the operator's own conditionals, and they own what those mean.
	// The direction the scenario occurs in comes from the grid, not from a
	// flag: it is a property of the fit, and asking the operator to restate it
	// would be one more place for the two to disagree.
	below := false
	if c, err := scenario.LoadConditionals(); err == nil {
		below = c.OccursBelow(*name)
	}

	if *rungs == "" && *q < 0 && *r < 0 {
		c, err := scenario.LoadConditionals()
		if err != nil {
			return err
		}
		if err := c.CheckDefinition(*outcome, *name, basisVal.String(), *threshold, below); err != nil {
			return err
		}
	}

	sc, err := marketScenario(*name, basisVal, *total, *spread, *threshold, *sigma, *sMarket, below)
	if err != nil {
		return err
	}

	fmt.Printf("SCENARIO  %s\n", sc)
	if sc.Source == scenario.DerivedFromLine {
		method := "empirical residuals"
		if *sigma > 0 {
			method = fmt.Sprintf("normal, sigma %.1f", *sigma)
		}
		switch basisVal {
		case scenario.Total:
			fmt.Printf("  from a posted total of %.1f (%s)\n", *total, method)
		case scenario.Margin:
			fmt.Printf("  from a spread of %+.1f (%s)\n", *spread, method)
		}
	}
	fmt.Printf("  market says %.1f%%   you say %.1f%%   (you are %+.1f points apart)\n\n",
		sc.Prob*100, *belief*100, (*belief-sc.Prob)*100)

	if *rungs != "" {
		return ladderReport(*rungs, sc.Prob, *belief, b, *stake)
	}

	if *price == 0 {
		return fmt.Errorf("-price is required (or use -rungs)")
	}

	qv, rv, condSource, err := resolveConditionals(
		*outcome, *name, *q, *r,
		scenario.Site{Posted: *total, Baseline: *baseline, Trend: *trend},
		*line, *confidence, basisVal.String(), *threshold, *side)
	if err != nil {
		return err
	}

	req, err := wager.RequiredScenarioProb(wager.American(*price), qv, rv)
	if err != nil {
		return err
	}
	class, err := wager.Classify(req, sc.Prob, *belief)
	if err != nil {
		return err
	}
	predicted, err := wager.BlendedProb(*belief, qv, rv)
	if err != nil {
		return err
	}
	ev, err := wager.BlendedEV(b, *belief, qv, rv, wager.American(*price), *stake)
	if err != nil {
		return err
	}

	if *label != "" {
		fmt.Printf("%s\n", *label)
	}
	fmt.Printf("  price %+d   hurdle %.1f%%   q %.1f%%   r %.1f%%\n",
		*price, req.Breakeven*100, qv*100, rv*100)
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
			SMarket: sc.Prob, SYours: *belief, Q: qv, R: rv,
			ConditionalSource: condSource,
			SStar:             req.Required, EdgeSource: class.Source.String(), Predicted: predicted,
		})
		if err != nil {
			return err
		}
		fmt.Printf("\n  logged as %s\n", id)
	}
	return nil
}

// resolveConditionals supplies q and r, either from the operator or from the
// fitted pooled grid.
//
// Operator values take precedence, always. The grid is pooled across thousands
// of player-games with similar opportunity, which is what makes it estimable at
// all, but it cannot see the things that make a specific week unusual -- an
// injury to the man covering him, a coordinator change, a cast on the left
// tackle. When you know something the grid does not, you should be able to say
// so and have it win.
//
// The two sources are reported separately into the bet log so they can be
// scored separately later.
func resolveConditionals(
	outcome, name string, q, r float64, at scenario.Site, line, confidence float64,
	basis string, threshold float64, side string,
) (float64, float64, string, error) {
	if side != "over" && side != "under" {
		return 0, 0, "", fmt.Errorf("-side must be over or under, got %q", side)
	}
	if q >= 0 && r >= 0 {
		return q, r, "stated", nil
	}
	if q >= 0 || r >= 0 {
		return 0, 0, "", fmt.Errorf(
			"-q and -r must be supplied together: one alone would be silently dropped in " +
				"favour of the fitted grid, and you would never be told")
	}
	if at.Baseline <= 0 || line <= 0 {
		return 0, 0, "", fmt.Errorf(
			"supply both -q and -r, or -baseline and -line to look them up from the fitted grid")
	}
	// The grid is conditioned on the POSTED total as well, so a lookup without
	// one would silently land in whichever band contains zero. Refused: the
	// number is on the board next to the price.
	if at.Posted <= 0 {
		return 0, 0, "", fmt.Errorf(
			"-total is required for a grid lookup: q and r are fitted per posted-total band, " +
				"so without it the lookup lands in whichever band contains zero")
	}

	c, err := scenario.LoadConditionals()
	if err != nil {
		return 0, 0, "", err
	}
	// Before anything is looked up: the grid's q and r answer a fixed question,
	// and s answers whatever -threshold was passed. If those differ, no amount
	// of correct arithmetic downstream produces a meaningful number.
	if err := c.CheckDefinition(outcome, name, basis, threshold, c.OccursBelow(name)); err != nil {
		return 0, 0, "", err
	}
	qc, rc, err := c.QR(outcome, name, at, line, confidence)
	if err != nil {
		// Only list the alternatives when the SCENARIO is what failed. A line
		// outside the observed range is a different problem, and answering it
		// with the list points at the part that was already right.
		if errors.Is(err, scenario.ErrScenarioNotPriceable) {
			return 0, 0, "", fmt.Errorf("%w\n  scenarios you can price for %s: %s",
				err, outcome, strings.Join(c.ValidatedScenarioNames(outcome), ", "))
		}
		return 0, 0, "", err
	}
	// The grid fits P(output > line) only. An under is the same cell read from
	// the other end -- and it is where this grid's value tends to sit, because
	// a line set near a player's own median is one he clears rather less often
	// than half the time.
	if side == "under" {
		qc, rc = qc.Complement(), rc.Complement()
	}

	unit := "yds"
	trendScale := 1.0
	trendUnit := ""
	if def, ok := c.Outcomes[outcome]; ok {
		if def.Unit != "" {
			unit = def.Unit
		}
		// A share trend is reported in percentage points; a volume trend is
		// already in the units of the axis and must not be multiplied by 100.
		if def.ShareBased {
			trendScale, trendUnit = 100.0, " pt"
		}
	}
	// An override now names one SITE, and this lookup is keyed by the site the
	// wager falls in -- so a non-nil result already means "this wager is the
	// overridden one". The old call took no coordinates and fired on every
	// wager anywhere in the scenario, which made the warning something to read
	// past rather than something to act on.
	if af := c.AcceptedFailureFor(outcome, name, at); af != nil {
		fmt.Printf("  OVERRIDDEN GATE — THIS CELL does not pass validation and is being\n")
		fmt.Printf("  priced on a failure the operator accepted by name.\n")
		fmt.Printf("    failing cell   %s\n", af.Cell)
		fmt.Printf("    measured       %s\n", af.Measured)
		fmt.Printf("    accepted by    %s\n", af.AcceptedBy)
		fmt.Printf("  You are betting the part that failed, not the sites that held.\n\n")
	}
	fmt.Printf("  CONDITIONALS from the fitted grid (%s, %d-%d)\n",
		outcome, c.Seasons[0], c.Seasons[1])
	// The line is stated as a multiple of the player's own baseline as well as
	// in its own units, because the multiple is what the grid actually reads
	// and a reader should be able to see the number being looked up.
	fmt.Printf("    %.1f %s baseline, posted total %.1f, %+.1f%s trend\n",
		at.Baseline, unit, at.Posted, at.Trend*trendScale, trendUnit)
	fmt.Printf("    line %.1f = %.2fx his baseline, pricing the %s\n",
		line, line/at.Baseline, side)
	// The gate's own dependence on its two constants, stated where the number
	// is used. A verdict that moves when MIN_CELL moves is still a verdict,
	// but the operator should not have to run a sweep to find that out.
	if note := qc.Cell.StabilityNote(); note != "" {
		fmt.Printf("    CAVEAT: %s\n", note)
	}
	// n_eff is shown beside n, not instead of it. The interval is built on the
	// smaller number, and printing only the raw count would overstate the
	// evidence behind the interval printed next to it.
	fmt.Printf("    q = %.1f%%  [%.1f-%.1f]  n=%d (eff %d)  median %.0f %s  (scenario occurred)\n",
		qc.Prob*100, qc.Lower*100, qc.Upper*100, qc.N, qc.NEff, qc.CellMedian, unit)
	fmt.Printf("      %s\n", support(qc))
	fmt.Printf("    r = %.1f%%  [%.1f-%.1f]  n=%d (eff %d)  median %.0f %s  (it did not)\n",
		rc.Prob*100, rc.Lower*100, rc.Upper*100, rc.N, rc.NEff, rc.CellMedian, unit)
	fmt.Printf("      %s\n", support(rc))
	if qc.NEff < qc.N || rc.NEff < rc.N {
		fmt.Printf("    intervals use the effective count: cells pool repeat players,\n")
		fmt.Printf("    so the raw n overstates how much independent evidence there is\n")
	}

	// A wide interval on either side means the requirement below is softer
	// than its two decimal places suggest.
	if qc.Upper-qc.Lower > 0.15 || rc.Upper-rc.Lower > 0.15 {
		fmt.Printf("    note: these cells are thin; treat s* as indicative\n")
	}

	// Interval width alone never catches the deep-line case: at a small p,
	// Wilson is narrow in absolute terms however little evidence there is. A
	// q of 2.3% on seven observations prints a tighter interval than a q of
	// 25.2% on ninety-four, and the two used to be indistinguishable here.
	if qc.Thin() || rc.Thin() {
		fmt.Printf("    THIN: fewer than %d effective observations sit past this line on\n",
			scenario.MinTailN)
		fmt.Printf("    %s. The interval above is narrow because the probability is\n", thinSide(qc, rc))
		fmt.Printf("    small, not because the estimate is settled. s* is a direction here,\n")
		fmt.Printf("    not a threshold — move the line in, or supply -q and -r yourself.\n")
	}
	fmt.Println()
	return qc.Prob, rc.Prob, "pooled", nil
}

// support says how much evidence sits on the side of the line being bet.
//
// It goes on its own line rather than at the end of the estimate because it is
// the number that decides whether the estimate means anything, and appending it
// to an already-long row buries it exactly where it would be skimmed past.
func support(c scenario.Conditional) string {
	switch {
	case c.Thin():
		return fmt.Sprintf("THIN — only ~%.0f effective observations past the line (±%.0f%%)",
			c.TailN, c.RelativeError()*100)
	case c.Sparse():
		// The middle band exists because one threshold made 13 observations and
		// 400 print the same word, at ±28% and ±5% respectively.
		return fmt.Sprintf("SPARSE — ~%.0f effective observations past the line (±%.0f%%)",
			c.TailN, c.RelativeError()*100)
	}
	return fmt.Sprintf("MEASURED — ~%.0f effective observations past the line (±%.0f%%)",
		c.TailN, c.RelativeError()*100)
}

// thinSide names which of q and r is short, since the remedy differs: a thin q
// means the scenario rarely produces the line, a thin r means the baseline
// almost never does.
func thinSide(qc, rc scenario.Conditional) string {
	switch {
	case qc.Thin() && rc.Thin():
		return "either side"
	case qc.Thin():
		return "the scenario side (q)"
	default:
		return "the baseline side (r)"
	}
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

func marketScenario(name string, basis scenario.Basis, total, spread, threshold, sigma, override float64, below bool) (scenario.Scenario, error) {
	if override >= 0 {
		return scenario.StateProb(name, basis, threshold, override, below)
	}
	switch basis {
	case scenario.Total:
		if total <= 0 {
			return scenario.Scenario{}, fmt.Errorf(
				"need -total to derive a total-based scenario (or pass -smarket)")
		}
		return scenario.FromTotal(name, total, threshold, sigma, below)
	default:
		return scenario.FromSpread(name, spread, threshold, sigma, below)
	}
}

func parseBasis(s string) (scenario.Basis, error) {
	b, err := scenario.ParseBasis(s)
	if err != nil {
		return scenario.Total, fmt.Errorf("-basis: %w", err)
	}
	return b, nil
}

// outcomeNames lists what the artifact actually fits, for help text.
//
// Read from the grid rather than written beside it. The hardcoded version said
// "receiving_yards or passing_yards" for as long as it took to add two more.
func outcomeNames() string {
	c, err := scenario.LoadConditionals()
	if err != nil {
		return "receiving_yards"
	}
	return strings.Join(c.OutcomeNames(), ", ")
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

package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"

	"edge/internal/betlog"
	"edge/internal/calib"
)

// referenceMode picks what the forecast is measured against.
//
// Which reference is available depends on the scenario and the week, and the
// difference matters: beating a base rate is a weak claim, beating the market's
// own number is the sharp one. shootout and blowout_loss have a market line
// from week 1; the other two have the incumbent model only from week 4, when
// prior form first exists.
const (
	refAuto      = "auto"
	refBaseRate  = "base-rate"
	refIncumbent = "belief-json"
	refMarket    = "market"
	refLine      = "line"
)

// scenariosNotWagerable are the scenarios a belief cannot be spent on at all.
//
// Only blowout_loss: it fails validation on every outcome it is fitted for, so
// no site prices it. pass_heavy IS wagerable (it validates on passing_yards),
// but its line barely moves, which is why it is excluded from the DECISION by
// decisionScenarios rather than from wagering here. Scoring an unwagerable
// scenario is free and informs how honest the forecaster is; the footer says so
// rather than leaving a reader to find it in the fit script.
var scenariosNotWagerable = map[string]string{
	"blowout_loss": "fails validation on every outcome it is fitted for",
}

// decisionScenarios are the ones the pre-registered verdict is computed on. The
// other two are scored and reported but do not decide: blowout_loss cannot be
// wagered at all, and pass_heavy's line barely moves, so pooling either into the
// verdict measures something the decision does not rest on. They still appear in
// BY REFERENCE and BY SCENARIO.
var decisionScenarios = map[string]bool{
	"shootout":          true,
	"efficient_offense": true,
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// forDecision keeps only the predictions the verdict is allowed to rest on: the
// decision scenarios, further narrowed to `only` when the user asked for one.
func forDecision(preds []betlog.SettledPrediction, only string) []betlog.SettledPrediction {
	var out []betlog.SettledPrediction
	for _, sp := range preds {
		s := sp.Prediction.Scenario
		if !decisionScenarios[s] {
			continue
		}
		if only != "" && s != only {
			continue
		}
		out = append(out, sp)
	}
	return out
}

func beliefsScore(args []string) error {
	fs := flag.NewFlagSet("beliefs score", flag.ExitOnError)
	logPath := fs.String("log", "beliefs/log.jsonl", "the belief log")
	only := fs.String("scenario", "", "score one scenario only")
	fromWeek := fs.Int("from-week", 0, "ignore weeks before this")
	toWeek := fs.Int("to-week", 0,
		"ignore weeks after this. With -from-week this expresses the pre-registered "+
			"window exactly, e.g. -from-week 1 -to-week 8")
	vs := fs.String("vs", refAuto,
		"what to measure against: auto, base-rate, belief-json, line or market")
	bar := fs.Float64("bar", 0.10,
		"the s-edge a disagreement must clear to count; FINDINGS 16 puts the "+
			"requirement at +0.03 to +0.16 depending on the site")
	hold := fs.Float64("hold", 0.06,
		"the book's proportional hold, for the realised-edge endpoint. A flat number of "+
			"points would tax a 12%% line at 20%%, which no book does")
	includeRejected := fs.Bool("include-rejected", false,
		"score predictions whose claims were falsified, alongside the survivors")
	if err := fs.Parse(args); err != nil {
		return err
	}

	preds, err := betlog.LoadPredictions(*logPath)
	if err != nil {
		return err
	}
	if len(preds) == 0 {
		fmt.Printf("no beliefs recorded in %s\n", *logPath)
		return nil
	}

	all := points(preds, *only, *fromWeek, *toWeek, *vs, true)
	survivors := points(preds, *only, *fromWeek, *toWeek, *vs, false)
	use := survivors
	label := "survivors"
	if *includeRejected {
		use = all
		label = "all, including falsified"
	}
	if len(use) == 0 {
		fmt.Printf("nothing settled yet to score (%d predictions recorded)\n", len(preds))
		return nil
	}

	pts := onlyPoints(use)
	r, err := calib.Score(pts, *bar)
	if err != nil {
		return err
	}

	fmt.Printf("BELIEF SCORE  (%s)\n\n", label)
	fmt.Printf("  settled           %d  (%d took a position, %d abstained)\n",
		r.N, r.Positions, r.Abstained)
	fmt.Printf("  predicted         %.3f\n  happened          %.3f\n  bias              %+.2fpp\n",
		r.Mean, r.Base, r.Bias*100)

	// The interval must cover the same population as the estimate: positions,
	// not positions plus abstentions.
	scored := calib.Positions(pts)
	lo, hi := calib.BootstrapCI(scored, calib.Brier, 800, 20260824, 0.05)
	fmt.Printf("\n  RELIABILITY and RESOLUTION are different things, and one good number\n")
	fmt.Printf("  for the first proves nothing about the second.\n")
	fmt.Printf("    Brier           %.4f  [%.4f, %.4f]  (clustered by game)\n", r.Brier, lo, hi)
	fmt.Printf("    binned Brier    %.4f   what the three terms below sum to; gap %+.4f is\n",
		r.BinnedBrier, r.Brier-r.BinnedBrier)
	fmt.Printf("                    discretisation, not miscalibration\n")
	fmt.Printf("    reliability     %.4f   lower is better — is 40%% really 40%%?\n", r.Reliability)
	fmt.Printf("    resolution      %.4f   HIGHER is better — does it vary, usefully?\n", r.Resolution)
	fmt.Printf("    uncertainty     %.4f   the base rate's own variance; not the forecaster's\n",
		r.Uncertainty)
	if r.Resolution < 0.002 {
		fmt.Printf("    NOTE resolution near zero: this is saying much the same thing about\n")
		fmt.Printf("         every game. That can be perfectly calibrated and still worthless.\n")
	}

	if r.Converged {
		fmt.Printf("\n  CALIBRATION SLOPE  %.3f  (se %.3f, intercept %+.3f)\n",
			r.Slope, r.SlopeSE, r.Intercept)
		switch {
		case r.Slope < 0:
			fmt.Printf("    BELOW ZERO: higher belief went with the scenario happening LESS — its\n")
			fmt.Printf("    confident calls point the wrong way, which is worse than saying nothing.\n")
		default:
			fmt.Printf("    1.0 is honest; below 1 means its confident calls are over-confident.\n")
		}
	} else {
		// No number, rather than a number to be misread -- and the RIGHT reason,
		// because the two causes mean opposite things about the forecaster.
		fmt.Printf("\n  CALIBRATION SLOPE  unmeasurable — %s\n", slopeFailure(r))
	}
	fmt.Printf("  DISCRIMINATION     AUC %.3f, separation %+.3f\n", r.AUC, r.Separation)

	if r.HasRef {
		fmt.Printf("\n  AGAINST THE REFERENCE  (%d of %d rows had one)\n", r.RefN, r.Positions)
		fmt.Printf("    reference Brier %.4f   skill %+.3f\n", r.RefBrier, r.Skill)
		gain := calib.PairedBrierGain(scored)
		glo, ghi := calib.BootstrapCI(scored, calib.PairedBrierGain, 800, 20260824, 0.05)
		fmt.Printf("    paired gain     %+.5f  [%+.5f, %+.5f]\n", gain, glo, ghi)
		fmt.Printf("\n    Calibration is not an edge. These two are what the wager needs:\n")
		fmt.Printf("    mean |p−ref|    %.4f   does it disagree by enough to matter?\n",
			r.MeanAbsDisagreement)
		fmt.Printf("    over the bar    %d of %d at ±%.2f\n", r.OverBar, r.RefN, r.Bar)
		if r.OverBar > 0 {
			fmt.Printf("    informed        %.1f%%   of those, the share that moved the RIGHT way\n",
				r.InformedFraction*100)
			if r.InformedFraction < 0.5 {
				fmt.Printf("    NOTE below half: its big disagreements point the wrong way, which\n")
				fmt.Printf("         is worse than not disagreeing at all.\n")
			}
		}
	} else {
		fmt.Printf("\n  No reference on any row — nothing to beat, so no edge can be claimed.\n")
	}

	// Survivors against everything, on identical outcomes. This is what the
	// falsifier is worth; a bare rejection count cannot say.
	if !*includeRejected && len(all) > len(survivors) {
		if ra, err := calib.Score(onlyPoints(all), *bar); err == nil {
			fmt.Printf("\n  THE FALSIFIER  %d of %d predictions had a claim falsified\n",
				len(all)-len(survivors), len(all))
			fmt.Printf("    survivors Brier %.4f   all %.4f   (lower is better)\n", r.Brier, ra.Brier)
			fmt.Printf("    Discarding them is part of the strategy, not a filter on the score:\n")
			fmt.Printf("    a forecast caught inventing evidence is never wagered on.\n")
		}
	}

	jointVerdict(preds, *only, *fromWeek, *toWeek, *includeRejected, *bar, *hold)
	referenceBreakdown(referenceSets(preds, *only, *fromWeek, *toWeek, *includeRejected), *bar)
	groupReport("BY SCENARIO", use, *bar, func(r scoredRow) string { return r.scenario })
	groupReport("BY STATED CONFIDENCE", use, *bar, confidenceBand)
	flaggedReport(use, *bar)
	powerNote(r.Positions)
	return nil
}

// jointVerdict reports the pre-registered endpoint.
//
// The DECISION is E1: is this more accurate than every opponent that exists —
// the market, the incumbent, and the line — each scored on its own rows, with
// the hardest binding? Beating a single auto-picked reference was the flaw the
// 2026-09-04 review found; beating one that the tool's own line model already
// beats is no achievement.
//
// E2 — would the implied wagers have profited? — was registered as a co-equal
// second claim, but the belief probe collects no prop: a wager settles on the
// same game-script outcome Y that drives E1, so E2 is a rescaling of E1, not
// independent evidence. It is kept as a DIAGNOSTIC with an honest interval
// (Bernoulli-settled, not the plug-in mean) rather than a gate. See the
// pre-registration amendment in docs/frameworks/belief-probe.md.
//
// Intervals are clustered by game, because two teams in one game are not two
// independent draws.
// refSet is one opponent's rows, ready to score. Built once and shared by the
// verdict and the breakdown so the number the verdict decides on is the same
// number the table prints.
type refSet struct {
	name, mode string
	binding    bool // a real opponent the verdict must beat; false for the base-rate floor
	pts        []calib.Point
}

// referenceSets scores each opponent on its OWN rows. base-rate is included but
// marked non-binding: it is the universal fallback, the easiest to beat, and
// pooling it across scenarios would reintroduce the very pooling this avoids, so
// it informs but does not decide.
func referenceSets(preds []betlog.SettledPrediction, only string,
	fromWeek, toWeek int, includeRejected bool) []refSet {
	defs := []struct {
		name, mode string
		binding    bool
	}{
		{"market", refMarket, true},
		{"belief-json (incumbent)", refIncumbent, true},
		{"line", refLine, true},
		{"base-rate", refBaseRate, false},
	}
	var out []refSet
	for _, d := range defs {
		rows := points(preds, only, fromWeek, toWeek, d.mode, includeRejected)
		var withRef []calib.Point
		for _, p := range calib.Positions(onlyPoints(rows)) {
			if p.HasRef {
				withRef = append(withRef, p)
			}
		}
		if len(withRef) > 0 {
			out = append(out, refSet{d.name, d.mode, d.binding, withRef})
		}
	}
	return out
}

func jointVerdict(preds []betlog.SettledPrediction, only string, fromWeek, toWeek int,
	includeRejected bool, bar, hold float64) {
	fmt.Printf("\n  PRE-REGISTERED ENDPOINT  E1 accuracy is the decision; E2 is a diagnostic\n")
	fmt.Printf("                 on %s only — the other scenarios are scored but do not decide\n",
		strings.Join(sortedKeys(decisionScenarios), " and "))

	// The verdict rests ONLY on the decision scenarios. Building from a filtered
	// prediction set here — rather than the report-wide points — keeps blowout_loss
	// (unwagerable, and negatively correlated, which would inflate effective n) and
	// pass_heavy (a line that barely moves) out of the decision.
	dpreds := forDecision(preds, only)

	// E1 must beat EVERY real opponent, each scored on its own rows -- the
	// HARDEST binds. The old verdict scored one auto-picked reference, so it
	// scored the two PROE scenarios against the beatable incumbent and never
	// against the line the C1 fix added; a forecaster replaying the line beat the
	// incumbent and passed. Now the line is one of the opponents that must be
	// beaten, and losing to any of them fails E1.
	sets := referenceSets(dpreds, "", fromWeek, toWeek, includeRejected)
	autoPts := calib.Positions(onlyPoints(points(dpreds, "", fromWeek, toWeek, refAuto, includeRejected)))
	ev := evaluateE1(sets)
	e1, e1Decidable := ev.pass, ev.decidable
	if !e1Decidable {
		fmt.Printf("    E1 accuracy     no opponent is evaluable yet\n")
	} else {
		fmt.Printf("    E1 accuracy     vs the hardest of %d opponents (%s)\n", bindingCount(sets), ev.name)
		fmt.Printf("                    paired Brier gain %+.5f  [%+.5f, %+.5f]   %s\n",
			ev.gain, ev.lo, ev.hi, verdictWord(e1, false))
	}

	// E2 is a DIAGNOSTIC, not a second gate. With game-script-only data the
	// wager's outcome is the same Y that drives E1, so it is a rescaling of E1,
	// not independent evidence -- registering it as a co-equal claim was a promise
	// the data cannot keep. The point estimate is the EXPECTED ROI assuming the
	// frozen site is right; the interval is bootstrapped over Bernoulli-settled
	// wagers so it reflects real prop variance rather than the ~5x-too-tight
	// plug-in spread.
	n := calib.OverBarCount(autoPts, bar, hold)
	edge := calib.RealisedEdge(autoPts, bar, hold)
	rng := rand.New(rand.NewSource(20260904))
	stat := func(s []calib.Point) float64 { return calib.RealisedEdgeSampled(s, bar, hold, rng) }
	elo, ehi := calib.BootstrapCI(autoPts, stat, 800, 20260824, 0.05)
	if n == 0 {
		fmt.Printf("    E2 diagnostic   no wagers implied yet\n")
	} else {
		fmt.Printf("    E2 diagnostic   expected ROI %+.4f  [%+.4f, %+.4f]  on %d implied wagers\n",
			edge, elo, ehi, n)
		fmt.Printf("                    per unit staked at a %.0f%% hold, assuming the frozen site is\n", hold*100)
		fmt.Printf("                    exact; a robustness check on E1, not independent of it\n")
	}

	// The decision is E1 alone.
	switch {
	case !e1Decidable:
		fmt.Printf("    VERDICT         not yet decidable — no opponent is evaluable\n")
	case e1:
		fmt.Printf("    VERDICT         PASS — more accurate than every opponent that exists\n")
	default:
		fmt.Printf("    VERDICT         FAIL — it loses to at least one opponent (%s)\n", ev.name)
	}
}

func verdictWord(pass, undecided bool) string {
	switch {
	case undecided:
		return "—"
	case pass:
		return "PASS"
	}
	return "fail"
}

// confidenceBand buckets a forecaster's own confidence.
//
// Reported rather than used as a weight. The question is whether it KNOWS when
// it is guessing -- if the high-confidence calls are not more accurate, the
// confidence is noise, and weighting by noise would hide that rather than show
// it.
func confidenceBand(r scoredRow) string {
	switch {
	case r.confidence <= 0:
		return "unstated"
	case r.confidence < 0.34:
		return "low"
	case r.confidence < 0.67:
		return "medium"
	}
	return "high"
}

// referenceBreakdown scores each opponent on its OWN rows, never pooled.
//
// The default auto number mixes reference types across rows -- some measured
// against the market, some the incumbent, some the line, some the base rate --
// and the 2026-09-01 review showed why that hides the result: a forecaster can
// beat a stale base rate on weeks 1-3 and lose to the market later, and the
// pooled gain averages the two into something that means neither. Beating the
// line is the claim that matters, so it gets its own row here.
// e1Eval is the outcome of the E1 accuracy test: the binding (hardest) opponent
// and whether the forecaster beat every one.
type e1Eval struct {
	pass, decidable bool
	name            string
	gain, lo, hi    float64
}

// evaluateE1 requires the forecaster to beat EVERY real opponent, each scored on
// its own rows. The hardest (lowest lower-bound) binds and is reported. An
// opponent whose interval is unmeasurable is skipped rather than counted as a
// pass or a fail; if none is measurable the test is not yet decidable.
func evaluateE1(sets []refSet) e1Eval {
	out := e1Eval{pass: true}
	first := true
	for _, s := range sets {
		if !s.binding {
			continue
		}
		gain := calib.PairedBrierGain(s.pts)
		lo, hi := calib.BootstrapCI(s.pts, calib.PairedBrierGain, 800, 20260824, 0.05)
		if math.IsNaN(lo) {
			continue
		}
		out.decidable = true
		if lo <= 0 {
			out.pass = false
		}
		if first || lo < out.lo {
			out.name, out.gain, out.lo, out.hi, first = s.name, gain, lo, hi, false
		}
	}
	out.pass = out.pass && out.decidable
	return out
}

// bindingCount is how many real opponents the verdict must beat.
func bindingCount(sets []refSet) int {
	n := 0
	for _, s := range sets {
		if s.binding {
			n++
		}
	}
	return n
}

func referenceBreakdown(sets []refSet, bar float64) {
	fmt.Printf("\n  BY REFERENCE  (each opponent scored on its own rows — never pooled)\n")
	// "over ±bar" counts rows disagreeing by more than the bar — NOT the E2 wager
	// count, which additionally requires clearing the vig.
	fmt.Printf("    %-24s %6s %10s %11s\n", "", "n", "gain", "over ±bar")
	if len(sets) == 0 {
		fmt.Printf("    (no row carried any reference yet)\n")
		return
	}
	for _, s := range sets {
		g, err := calib.Score(s.pts, bar)
		gain := calib.PairedBrierGain(s.pts)
		glo, ghi := calib.BootstrapCI(s.pts, calib.PairedBrierGain, 800, 20260824, 0.05)
		gs := "—"
		if !math.IsNaN(gain) {
			gs = fmt.Sprintf("%+.5f", gain)
		}
		over := "—"
		if err == nil {
			over = fmt.Sprintf("%d of %d", g.OverBar, g.RefN)
		}
		name := s.name
		if !s.binding {
			name += " (floor)"
		}
		fmt.Printf("    %-24s %6d %10s %11s  [%+.5f, %+.5f]\n",
			name, len(s.pts), gs, over, glo, ghi)
	}
}

func groupReport(title string, rows []scoredRow, bar float64, key func(scoredRow) string) {
	groups := map[string][]scoredRow{}
	var order []string
	for _, r := range rows {
		k := key(r)
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	if len(order) < 2 {
		return // a split into one group says nothing
	}
	sort.Strings(order)
	fmt.Printf("\n  %s\n", title)
	fmt.Printf("    %-20s %6s %8s %9s %9s %11s\n",
		"", "n", "Brier", "resol.", "gain", "informed")
	for _, k := range order {
		pts := onlyPoints(groups[k])
		g, err := calib.Score(pts, bar)
		if err != nil {
			fmt.Printf("    %-20s %6d   %s\n", k, len(pts), "all abstentions")
			continue
		}
		gain := calib.PairedBrierGain(calib.Positions(pts))
		inf := "—"
		if g.OverBar > 0 {
			inf = fmt.Sprintf("%.0f%% of %d", g.InformedFraction*100, g.OverBar)
		}
		gs := "—"
		if !math.IsNaN(gain) {
			gs = fmt.Sprintf("%+.5f", gain)
		}
		fmt.Printf("    %-20s %6d %8.4f %9.4f %9s %11s\n",
			k, g.Positions, g.Brier, g.Resolution, gs, inf)
	}
}

// flaggedReport compares the candidates the forecaster would actually have bet
// against everything else it said.
//
// It does not filter the score -- flagging is the forecaster's own choice, and
// letting it select its own scored set is the cherry-picking the contract
// exists to stop. It is reported because a source whose FLAGGED calls are no
// better than its others has no candidate-selection skill, which is a different
// finding from having no forecasting skill.
func flaggedReport(rows []scoredRow, bar float64) {
	var yes, no []calib.Point
	for _, r := range rows {
		if r.flagged {
			yes = append(yes, r.pt)
		} else {
			no = append(no, r.pt)
		}
	}
	if len(yes) == 0 {
		return
	}
	fmt.Printf("\n  FLAGGED CANDIDATES  %d of %d\n", len(yes), len(rows))
	for _, tc := range []struct {
		name string
		pts  []calib.Point
	}{{"flagged", yes}, {"the rest", no}} {
		g, err := calib.Score(tc.pts, bar)
		if err != nil {
			continue
		}
		// A one-row group has no paired gain to speak of; print an em dash like
		// every other section rather than a bare +NaN. A single flagged
		// candidate is entirely plausible early season.
		gain := calib.PairedBrierGain(calib.Positions(tc.pts))
		gs := "—"
		if !math.IsNaN(gain) {
			gs = fmt.Sprintf("%+.5f", gain)
		}
		fmt.Printf("    %-10s n=%-4d Brier %.4f   gain %s\n",
			tc.name, g.Positions, g.Brier, gs)
	}
	fmt.Printf("    Flagging does not filter the score. If these are no better than the\n")
	fmt.Printf("    rest, the forecaster cannot pick its own spots — a different finding\n")
	fmt.Printf("    from being unable to forecast.\n")
}

// slopeFailure names why the logistic fit did not converge.
//
// The two causes are opposite findings and must not share a message. A
// forecaster whose calls never vary has nothing to fit; one whose calls were
// ALL correct has a maximum likelihood at infinity. Reporting the first when
// the second happened would tell someone their forecaster is flat when it was
// in fact perfect on the sample.
func slopeFailure(r calib.Report) string {
	switch {
	case r.AUC >= 0.999 || r.AUC <= 0.001:
		return fmt.Sprintf(
			"every call separated the outcome perfectly (AUC %.3f), so the fit runs to "+
				"infinity. That is a small-sample artefact, not a slope of zero", r.AUC)
	case r.Resolution < 1e-6:
		return "the forecasts barely vary, so there is no slope to fit"
	}
	return "the fit did not converge; treat it as unmeasured rather than as a number"
}

// scoredRow is a point with enough of its prediction to group by.
//
// Grouping is not decoration: the decision weights shootout and
// efficient_offense, and "does it know when it is guessing" cannot be answered
// from a pooled number.
type scoredRow struct {
	pt         calib.Point
	scenario   string
	week       int
	confidence float64
	flagged    bool
}

func onlyPoints(rows []scoredRow) []calib.Point {
	out := make([]calib.Point, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.pt)
	}
	return out
}

// points turns settled predictions into scored points.
func points(preds []betlog.SettledPrediction, only string, fromWeek, toWeek int,
	mode string, includeRejected bool) []scoredRow {
	var out []scoredRow
	for _, sp := range preds {
		p := sp.Prediction
		if only != "" && p.Scenario != only {
			continue
		}
		if p.Week < fromWeek {
			continue
		}
		if toWeek > 0 && p.Week > toWeek {
			continue
		}
		if p.Rejected && !includeRejected {
			continue
		}
		happened, known := sp.Occurred()
		if !known {
			continue // open, or voided as unmeasurable
		}
		pt := calib.Point{
			P:         p.Belief,
			Y:         happened,
			Cluster:   p.GameID,
			Abstained: p.Abstained,
		}
		if ref, ok := reference(p, mode); ok {
			pt.Ref, pt.HasRef = ref, true
		}
		// The frozen wagerable site, if this scenario has one. Both halves or
		// neither: q and r only mean something together.
		if p.Q != nil && p.R != nil {
			pt.Q, pt.R, pt.HasQR = *p.Q, *p.R, true
		}
		out = append(out, scoredRow{pt: pt, scenario: p.Scenario, week: p.Week,
			confidence: p.Confidence, flagged: p.Flagged})
	}
	return out
}

// reference resolves what this prediction is measured against.
//
// Auto prefers the market where one exists, because beating a real opponent is
// the informative result; it falls back to the incumbent model, then to the
// base rate.
func reference(p betlog.Prediction, mode string) (float64, bool) {
	pick := func(v *float64) (float64, bool) {
		if v == nil {
			return 0, false
		}
		return *v, true
	}
	switch mode {
	case refMarket:
		return pick(p.SMarket)
	case refIncumbent:
		return pick(p.SIncumbent)
	case refLine:
		return pick(p.SLine)
	case refBaseRate:
		return pick(p.SBaseRate)
	}
	// Auto prefers the sharpest opponent available. The line model sits above
	// the base rate: it is available from week 1 on the two PROE scenarios --
	// exactly where the base rate was stale and the exploit lived -- so where
	// there is no market or incumbent yet, the default opponent is the line,
	// not a constant.
	if v, ok := pick(p.SMarket); ok {
		return v, true
	}
	if v, ok := pick(p.SIncumbent); ok {
		return v, true
	}
	if v, ok := pick(p.SLine); ok {
		return v, true
	}
	return pick(p.SBaseRate)
}

// powerNote exists because 112 rows in week one looks decisive and is not.
func powerNote(n int) {
	fmt.Printf("\n  POWER  %d scored positions.\n", n)
	weeks := float64(n) / 60.0
	fmt.Printf("    A week is about 60 EFFECTIVE predictions once within-game correlation\n")
	fmt.Printf("    is discounted, so this is roughly %.1f weeks.\n", weeks)
	switch {
	case n < 120:
		fmt.Printf("    At this size only a very large edge (+0.20) is detectable, and even\n")
		fmt.Printf("    that at about 40%% power. READ NOTHING INTO THE SIGN YET.\n")
	case n < 360:
		fmt.Printf("    Enough for a +0.15 edge (~85%%). A +0.10 edge is still a coin flip\n")
		fmt.Printf("    to detect at ~52%%.\n")
	case n < 480:
		fmt.Printf("    Enough for +0.10 at ~80%% power. This is the first point worth\n")
		fmt.Printf("    reading as a verdict.\n")
	default:
		fmt.Printf("    Enough for +0.10 at 90%%+. A +0.05 edge remains undetectable in a\n")
		fmt.Printf("    season, and would still clear the bar on the best sites.\n")
	}
	if len(scenariosNotWagerable) > 0 {
		var names []string
		for k := range scenariosNotWagerable {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Printf("\n  NOT WAGERABLE  %s — score it, but do not decide on it.\n",
			strings.Join(names, ", "))
	}
}

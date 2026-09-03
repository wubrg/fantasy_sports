package main

import (
	"flag"
	"fmt"
	"math"
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

// scenariosNotWagerable are the pairings a good belief cannot be spent on.
//
// blowout_loss fails validation everywhere it is fitted, and pass_heavy is
// vetoed for receiving yards as a volume identity. Scoring them is free and
// informs how honest the forecaster is; DECIDING on them would be deciding on a
// wager that cannot be placed, so the score says so rather than leaving a
// reader to find it in the fit script.
var scenariosNotWagerable = map[string]string{
	"blowout_loss": "fails validation on every outcome it is fitted for",
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
		fmt.Printf("    1.0 is honest; below 1 means its confident calls are over-confident.\n")
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

	jointVerdict(scored, *bar, *hold)
	referenceBreakdown(preds, *only, *fromWeek, *toWeek, *includeRejected, *bar)
	groupReport("BY SCENARIO", use, *bar, func(r scoredRow) string { return r.scenario })
	groupReport("BY STATED CONFIDENCE", use, *bar, confidenceBand)
	flaggedReport(use, *bar)
	powerNote(r.Positions)
	return nil
}

// jointVerdict reports the pre-registered endpoint, which is two claims.
//
// Both must hold, and they are genuinely different claims:
//
//	E1  is this MORE ACCURATE than the reference?
//	E2  would the wagers it actually produces have WON?
//
// A forecaster better than the reference by a hair on every row passes E1 with
// a healthy gain and never disagrees by enough to place a bet, so E2 rests on
// nothing. One right about eight big calls and mediocre elsewhere can fail E1
// and be the profitable one. Registering only the first would let "it works" be
// declared about a forecaster that produces no wagers.
//
// Both intervals are clustered by game, because two teams in one game are not
// two independent draws.
func jointVerdict(pts []calib.Point, bar, hold float64) {
	fmt.Printf("\n  PRE-REGISTERED ENDPOINT  (both must pass)\n")

	gain := calib.PairedBrierGain(pts)
	glo, ghi := calib.BootstrapCI(pts, calib.PairedBrierGain, 800, 20260824, 0.05)
	e1 := !math.IsNaN(glo) && glo > 0
	fmt.Printf("    E1 accuracy     paired Brier gain %+.5f  [%+.5f, %+.5f]   %s\n",
		gain, glo, ghi, verdictWord(e1, math.IsNaN(glo)))

	n := calib.OverBarCount(pts, bar)
	edge := calib.RealisedEdge(pts, bar, hold)
	stat := func(s []calib.Point) float64 { return calib.RealisedEdge(s, bar, hold) }
	elo, ehi := calib.BootstrapCI(pts, stat, 800, 20260824, 0.05)
	e2 := !math.IsNaN(elo) && elo > 0
	fmt.Printf("    E2 profit       realised edge %+.4f  [%+.4f, %+.4f]  on %d wagers   %s\n",
		edge, elo, ehi, n, verdictWord(e2, math.IsNaN(elo) || n == 0))
	fmt.Printf("                    per unit staked, at a %.0f%% hold, on rows over ±%.2f\n",
		hold*100, bar)

	switch {
	case math.IsNaN(glo) || math.IsNaN(elo) || n == 0:
		fmt.Printf("    VERDICT         not yet decidable\n")
	case e1 && e2:
		fmt.Printf("    VERDICT         BOTH PASS — accurate, and the wagers it implies won\n")
	case e1:
		fmt.Printf("    VERDICT         accuracy only. It beats the reference and does not\n")
		fmt.Printf("                    disagree by enough, often enough, to be worth betting\n")
	case e2:
		fmt.Printf("    VERDICT         profit only. Its big calls paid without it being\n")
		fmt.Printf("                    measurably more accurate — treat as unproven at this n\n")
	default:
		fmt.Printf("    VERDICT         NEITHER\n")
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
func referenceBreakdown(preds []betlog.SettledPrediction, only string,
	fromWeek, toWeek int, includeRejected bool, bar float64) {
	type ref struct {
		name, mode string
	}
	refs := []ref{
		{"market", refMarket},
		{"belief-json (incumbent)", refIncumbent},
		{"line", refLine},
		{"base-rate", refBaseRate},
	}
	fmt.Printf("\n  BY REFERENCE  (each opponent scored on its own rows — never pooled)\n")
	fmt.Printf("    %-24s %6s %10s %11s\n", "", "n", "gain", "over bar")
	any := false
	for _, rf := range refs {
		rows := points(preds, only, fromWeek, toWeek, rf.mode, includeRejected)
		pts := calib.Positions(onlyPoints(rows))
		var withRef []calib.Point
		for _, p := range pts {
			if p.HasRef {
				withRef = append(withRef, p)
			}
		}
		if len(withRef) == 0 {
			continue
		}
		any = true
		g, err := calib.Score(withRef, bar)
		gain := calib.PairedBrierGain(withRef)
		glo, ghi := calib.BootstrapCI(withRef, calib.PairedBrierGain, 800, 20260824, 0.05)
		gs := "—"
		if !math.IsNaN(gain) {
			gs = fmt.Sprintf("%+.5f", gain)
		}
		over := "—"
		if err == nil {
			over = fmt.Sprintf("%d of %d", g.OverBar, g.RefN)
		}
		fmt.Printf("    %-24s %6d %10s %11s  [%+.5f, %+.5f]\n",
			rf.name, len(withRef), gs, over, glo, ghi)
	}
	if !any {
		fmt.Printf("    (no row carried any reference yet)\n")
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
		fmt.Printf("    %-10s n=%-4d Brier %.4f   gain %+.5f\n",
			tc.name, g.Positions, g.Brier, calib.PairedBrierGain(calib.Positions(tc.pts)))
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

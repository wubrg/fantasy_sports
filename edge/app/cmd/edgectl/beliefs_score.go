package main

import (
	"flag"
	"fmt"
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
	vs := fs.String("vs", refAuto,
		"what to measure against: auto, base-rate, belief-json or market")
	bar := fs.Float64("bar", 0.10,
		"the s-edge a disagreement must clear to count; FINDINGS 16 puts the "+
			"requirement at +0.03 to +0.16 depending on the site")
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

	all := points(preds, *only, *fromWeek, *vs, true)
	survivors := points(preds, *only, *fromWeek, *vs, false)
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

	r, err := calib.Score(use, *bar)
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
	scored := calib.Positions(use)
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
		fmt.Printf("    paired gain     %+.5f  [%+.5f, %+.5f]   <- the pre-registered endpoint\n",
			gain, glo, ghi)
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
		if ra, err := calib.Score(all, *bar); err == nil {
			fmt.Printf("\n  THE FALSIFIER  %d of %d predictions had a claim falsified\n",
				len(all)-len(survivors), len(all))
			fmt.Printf("    survivors Brier %.4f   all %.4f   (lower is better)\n", r.Brier, ra.Brier)
			fmt.Printf("    Discarding them is part of the strategy, not a filter on the score:\n")
			fmt.Printf("    a forecast caught inventing evidence is never wagered on.\n")
		}
	}

	powerNote(r.Positions)
	return nil
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

// points turns settled predictions into scored points.
func points(preds []betlog.SettledPrediction, only string, fromWeek int,
	mode string, includeRejected bool) []calib.Point {
	var out []calib.Point
	for _, sp := range preds {
		p := sp.Prediction
		if only != "" && p.Scenario != only {
			continue
		}
		if p.Week < fromWeek {
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
			Abstained: abstained(p),
		}
		if ref, ok := reference(p, mode); ok {
			pt.Ref, pt.HasRef = ref, true
		}
		out = append(out, pt)
	}
	return out
}

func abstained(p betlog.Prediction) bool {
	for _, c := range p.Claims {
		if strings.HasPrefix(c, "abstained:") {
			return true
		}
	}
	return false
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
	case refBaseRate:
		return pick(p.SBaseRate)
	}
	if v, ok := pick(p.SMarket); ok {
		return v, true
	}
	if v, ok := pick(p.SIncumbent); ok {
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

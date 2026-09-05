package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"

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
	// The empty-log notice names the path, which is a CLI-only nicety score()
	// cannot know; everything else the object carries.
	if len(preds) == 0 {
		fmt.Printf("no beliefs recorded in %s\n", *logPath)
		return nil
	}

	res, err := score(preds, scoreOpts{
		only: *only, fromWeek: *fromWeek, toWeek: *toWeek,
		vs: *vs, bar: *bar, hold: *hold, includeRejected: *includeRejected,
	})
	if err != nil {
		return err
	}
	res.WriteTerminal(os.Stdout)
	return nil
}

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
		// alpha=0.10 so the lower bound is a genuine one-sided 5% bound, matching
		// the pre-registration and the power table. Passing 0.05 here would be a
		// one-sided 2.5% test — the α defect the 2026-09-04 review found.
		lo, hi := calib.BootstrapCI(s.pts, calib.PairedBrierGain, 800, 20260824, 0.10)
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

// flagging is the forecaster's own choice, and letting it select its own scored
// set is the cherry-picking the contract exists to stop. computeFlagged reports
// it because a source whose FLAGGED calls are no better than its others has no
// candidate-selection skill, a different finding from having no forecasting skill.

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

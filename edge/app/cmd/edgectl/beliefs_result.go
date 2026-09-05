package main

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"edge/internal/betlog"
	"edge/internal/board"
	"edge/internal/calib"
	"edge/internal/scenario"
)

// clean replaces a non-finite float with 0 so the result serializes as JSON,
// which rejects NaN and ±Inf. Every field passed through clean is one the
// terminal renderer only shows behind a guard bool (Converged, GainNaN,
// E1Decidable, E2Wagers>0) or is a CI bound that is finite whenever the section
// renders — so zeroing the non-finite case never changes the printed output,
// and the GUI gets 0 with the guard telling it the value is undefined.
func clean(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func sortedMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ScoreResult is the whole belief score as data. `score` builds it; the terminal
// pretty-prints it with WriteTerminal and the web handler serializes the same
// object. Nothing is computed in a renderer, so the CLI and the GUI cannot show
// different numbers.
type ScoreResult struct {
	// Notice is a terminal-only message for the states that have no score to
	// render ("no beliefs recorded", "nothing settled yet"); when set, every
	// other field is zero and WriteTerminal prints only this.
	Notice string `json:"notice,omitempty"`

	Label     string `json:"label"`
	N         int    `json:"n"`
	Positions int    `json:"positions"`
	Abstained int    `json:"abstained"`

	Mean float64 `json:"mean"`
	Base float64 `json:"base"`
	Bias float64 `json:"bias"`

	Calib   calib.Report `json:"calib"`
	BrierCI [2]float64   `json:"brier_ci"`

	Reference    *referenceSummary `json:"reference,omitempty"`
	Falsifier    *falsifierSummary `json:"falsifier,omitempty"`
	Verdict      verdictResult     `json:"verdict"`
	ByReference  []referenceRow    `json:"by_reference"`
	ByScenario   []groupRow        `json:"by_scenario"`
	ByConfidence []groupRow        `json:"by_confidence"`
	Flagged      *flaggedSummary   `json:"flagged,omitempty"`
	Power        powerSummary      `json:"power"`
}

// referenceSummary is the AGAINST THE REFERENCE section, computed against the
// pooled auto reference. HasRef false means no row carried one.
type referenceSummary struct {
	HasRef              bool    `json:"has_ref"`
	RefN                int     `json:"ref_n"`
	Positions           int     `json:"positions"`
	RefBrier            float64 `json:"reference_brier"`
	Skill               float64 `json:"skill"`
	Gain                float64 `json:"paired_gain"`
	GainLo              float64 `json:"paired_gain_lo"`
	GainHi              float64 `json:"paired_gain_hi"`
	MeanAbsDisagreement float64 `json:"mean_abs_disagreement"`
	OverBar             int     `json:"over_bar"`
	Bar                 float64 `json:"bar"`
	InformedFraction    float64 `json:"informed_fraction"`
}

type falsifierSummary struct {
	Falsified      int     `json:"falsified"`
	Total          int     `json:"total"`
	SurvivorsBrier float64 `json:"survivors_brier"`
	AllBrier       float64 `json:"all_brier"`
}

// verdictResult is the pre-registered endpoint: E1 (the decision) and E2 (the
// diagnostic).
type verdictResult struct {
	DecisionScenarios []string `json:"decision_scenarios"`

	E1Decidable  bool    `json:"e1_decidable"`
	E1Pass       bool    `json:"e1_pass"`
	BindingName  string  `json:"binding_opponent"`
	BindingCount int     `json:"binding_count"`
	Gain         float64 `json:"e1_gain"`
	Lo           float64 `json:"e1_lo"`
	Hi           float64 `json:"e1_hi"`

	E2Wagers int     `json:"e2_wagers"`
	E2Edge   float64 `json:"e2_edge"`
	E2Lo     float64 `json:"e2_lo"`
	E2Hi     float64 `json:"e2_hi"`

	Hold float64 `json:"hold"`
}

type referenceRow struct {
	Name    string  `json:"name"`
	Binding bool    `json:"binding"`
	N       int     `json:"n"`
	Gain    float64 `json:"gain"`
	GainNaN bool    `json:"gain_undefined"`
	OverBar int     `json:"over_bar"`
	RefN    int     `json:"ref_n"`
	ScoreOK bool    `json:"scored"`
	Lo      float64 `json:"lo"`
	Hi      float64 `json:"hi"`
}

type groupRow struct {
	Key              string  `json:"key"`
	N                int     `json:"n"`
	AllAbstentions   bool    `json:"all_abstentions"`
	Brier            float64 `json:"brier"`
	Resolution       float64 `json:"resolution"`
	Gain             float64 `json:"gain"`
	GainNaN          bool    `json:"gain_undefined"`
	OverBar          int     `json:"over_bar"`
	InformedFraction float64 `json:"informed_fraction"`
}

type flaggedRow struct {
	Name    string  `json:"name"`
	N       int     `json:"n"`
	Brier   float64 `json:"brier"`
	Gain    float64 `json:"gain"`
	GainNaN bool    `json:"gain_undefined"`
}

type flaggedSummary struct {
	YesCount int          `json:"flagged_count"`
	Total    int          `json:"total"`
	Rows     []flaggedRow `json:"rows"`
}

type powerSummary struct {
	N            int      `json:"n"`
	NotWagerable []string `json:"not_wagerable"`
}

// scoreOpts is the fully-resolved score request; the CLI fills it from flags,
// the web handler from query params.
type scoreOpts struct {
	only             string
	fromWeek, toWeek int
	vs               string
	bar, hold        float64
	includeRejected  bool
}

// score builds the whole ScoreResult from settled predictions. It computes; it
// does not print. The empty and nothing-settled states come back as a Notice.
func score(preds []betlog.SettledPrediction, o scoreOpts) (ScoreResult, error) {
	if len(preds) == 0 {
		return ScoreResult{Notice: "no beliefs recorded"}, nil
	}

	all := points(preds, o.only, o.fromWeek, o.toWeek, o.vs, true)
	survivors := points(preds, o.only, o.fromWeek, o.toWeek, o.vs, false)
	use := survivors
	label := "survivors"
	if o.includeRejected {
		use = all
		label = "all, including falsified"
	}
	if len(use) == 0 {
		return ScoreResult{Notice: fmt.Sprintf(
			"nothing settled yet to score (%d predictions recorded)", len(preds))}, nil
	}

	pts := onlyPoints(use)
	r, err := calib.Score(pts, o.bar)
	if err != nil {
		return ScoreResult{}, err
	}
	// The slope trio is NaN when the fit does not converge (perfect separation),
	// and it is never rendered in that case (WriteTerminal prints slopeFailure
	// instead). Zero it so res.Calib serializes; the Converged flag carries the
	// truth to the GUI.
	if !r.Converged {
		r.Slope, r.SlopeSE, r.Intercept = clean(r.Slope), clean(r.SlopeSE), clean(r.Intercept)
	}

	res := ScoreResult{
		Label: label, N: r.N, Positions: r.Positions, Abstained: r.Abstained,
		Mean: r.Mean, Base: r.Base, Bias: r.Bias, Calib: r,
	}

	scored := calib.Positions(pts)
	lo, hi := calib.BootstrapCI(scored, calib.Brier, 800, 20260824, 0.05)
	res.BrierCI = [2]float64{lo, hi}

	if r.HasRef {
		gain := calib.PairedBrierGain(scored)
		glo, ghi := calib.BootstrapCI(scored, calib.PairedBrierGain, 800, 20260824, 0.05)
		res.Reference = &referenceSummary{
			HasRef: true, RefN: r.RefN, Positions: r.Positions,
			RefBrier: r.RefBrier, Skill: r.Skill,
			Gain: clean(gain), GainLo: clean(glo), GainHi: clean(ghi),
			MeanAbsDisagreement: r.MeanAbsDisagreement,
			OverBar:             r.OverBar, Bar: r.Bar, InformedFraction: r.InformedFraction,
		}
	} else {
		res.Reference = &referenceSummary{HasRef: false}
	}

	// Survivors against everything, on identical outcomes — what the falsifier
	// is worth. Only when some prediction was actually falsified.
	if !o.includeRejected && len(all) > len(survivors) {
		if ra, err := calib.Score(onlyPoints(all), o.bar); err == nil {
			res.Falsifier = &falsifierSummary{
				Falsified: len(all) - len(survivors), Total: len(all),
				SurvivorsBrier: r.Brier, AllBrier: ra.Brier,
			}
		}
	}

	res.Verdict = computeVerdict(preds, o)
	res.ByReference = computeReferenceRows(referenceSets(preds, o.only, o.fromWeek, o.toWeek, o.includeRejected), o.bar)
	res.ByScenario = computeGroups(use, o.bar, func(r scoredRow) string { return r.scenario })
	res.ByConfidence = computeGroups(use, o.bar, confidenceBand)
	res.Flagged = computeFlagged(use, o.bar)
	res.Power = powerSummary{N: r.Positions, NotWagerable: sortedMapKeys(scenariosNotWagerable)}
	return res, nil
}

// computeVerdict evaluates E1 (beat every opponent, hardest binds) and the E2
// diagnostic on the decision scenarios only.
func computeVerdict(preds []betlog.SettledPrediction, o scoreOpts) verdictResult {
	v := verdictResult{DecisionScenarios: sortedKeys(decisionScenarios), Hold: o.hold}

	dpreds := forDecision(preds, o.only)
	sets := referenceSets(dpreds, "", o.fromWeek, o.toWeek, o.includeRejected)
	autoPts := calib.Positions(onlyPoints(points(dpreds, "", o.fromWeek, o.toWeek, refAuto, o.includeRejected)))

	ev := evaluateE1(sets)
	v.E1Decidable, v.E1Pass = ev.decidable, ev.pass
	v.BindingName, v.BindingCount = ev.name, bindingCount(sets)
	v.Gain, v.Lo, v.Hi = clean(ev.gain), clean(ev.lo), clean(ev.hi)

	v.E2Wagers = calib.OverBarCount(autoPts, o.bar, o.hold)
	v.E2Edge = clean(calib.RealisedEdge(autoPts, o.bar, o.hold))
	rng := rand.New(rand.NewSource(20260904))
	stat := func(s []calib.Point) float64 { return calib.RealisedEdgeSampled(s, o.bar, o.hold, rng) }
	elo, ehi := calib.BootstrapCI(autoPts, stat, 800, 20260824, 0.05)
	v.E2Lo, v.E2Hi = clean(elo), clean(ehi)
	return v
}

func computeReferenceRows(sets []refSet, bar float64) []referenceRow {
	rows := make([]referenceRow, 0, len(sets))
	for _, s := range sets {
		g, err := calib.Score(s.pts, bar)
		gain := calib.PairedBrierGain(s.pts)
		glo, ghi := calib.BootstrapCI(s.pts, calib.PairedBrierGain, 800, 20260824, 0.10)
		row := referenceRow{
			Name: s.name, Binding: s.binding, N: len(s.pts),
			Gain: clean(gain), GainNaN: math.IsNaN(gain), Lo: clean(glo), Hi: clean(ghi),
		}
		if err == nil {
			row.ScoreOK, row.OverBar, row.RefN = true, g.OverBar, g.RefN
		}
		rows = append(rows, row)
	}
	return rows
}

func computeGroups(rows []scoredRow, bar float64, key func(scoredRow) string) []groupRow {
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
		return nil // a split into one group says nothing
	}
	sort.Strings(order)
	out := make([]groupRow, 0, len(order))
	for _, k := range order {
		pts := onlyPoints(groups[k])
		g, err := calib.Score(pts, bar)
		if err != nil {
			out = append(out, groupRow{Key: k, N: len(pts), AllAbstentions: true})
			continue
		}
		gain := calib.PairedBrierGain(calib.Positions(pts))
		out = append(out, groupRow{
			Key: k, N: g.Positions, Brier: g.Brier, Resolution: g.Resolution,
			Gain: clean(gain), GainNaN: math.IsNaN(gain),
			OverBar: g.OverBar, InformedFraction: g.InformedFraction,
		})
	}
	return out
}

func computeFlagged(rows []scoredRow, bar float64) *flaggedSummary {
	var yes, no []calib.Point
	for _, r := range rows {
		if r.flagged {
			yes = append(yes, r.pt)
		} else {
			no = append(no, r.pt)
		}
	}
	if len(yes) == 0 {
		return nil
	}
	fs := &flaggedSummary{YesCount: len(yes), Total: len(rows)}
	for _, tc := range []struct {
		name string
		pts  []calib.Point
	}{{"flagged", yes}, {"the rest", no}} {
		g, err := calib.Score(tc.pts, bar)
		if err != nil {
			continue
		}
		gain := calib.PairedBrierGain(calib.Positions(tc.pts))
		fs.Rows = append(fs.Rows, flaggedRow{
			Name: tc.name, N: g.Positions, Brier: g.Brier,
			Gain: clean(gain), GainNaN: math.IsNaN(gain),
		})
	}
	return fs
}

// WriteTerminal renders the score exactly as the CLI always has. Every string
// here is the same one score() used to print inline; only the source of the
// numbers changed, from locals to struct fields.
func (res ScoreResult) WriteTerminal(w io.Writer) {
	if res.Notice != "" {
		fmt.Fprintf(w, "%s\n", res.Notice)
		return
	}

	fmt.Fprintf(w, "BELIEF SCORE  (%s)\n\n", res.Label)
	fmt.Fprintf(w, "  settled           %d  (%d took a position, %d abstained)\n",
		res.N, res.Positions, res.Abstained)
	fmt.Fprintf(w, "  predicted         %.3f\n  happened          %.3f\n  bias              %+.2fpp\n",
		res.Mean, res.Base, res.Bias*100)

	r := res.Calib
	fmt.Fprintf(w, "\n  RELIABILITY and RESOLUTION are different things, and one good number\n")
	fmt.Fprintf(w, "  for the first proves nothing about the second.\n")
	fmt.Fprintf(w, "    Brier           %.4f  [%.4f, %.4f]  (clustered by game)\n", r.Brier, res.BrierCI[0], res.BrierCI[1])
	fmt.Fprintf(w, "    binned Brier    %.4f   what the three terms below sum to; gap %+.4f is\n",
		r.BinnedBrier, r.Brier-r.BinnedBrier)
	fmt.Fprintf(w, "                    discretisation, not miscalibration\n")
	fmt.Fprintf(w, "    reliability     %.4f   lower is better — is 40%% really 40%%?\n", r.Reliability)
	fmt.Fprintf(w, "    resolution      %.4f   HIGHER is better — does it vary, usefully?\n", r.Resolution)
	fmt.Fprintf(w, "    uncertainty     %.4f   the base rate's own variance; not the forecaster's\n",
		r.Uncertainty)
	if r.Resolution < 0.002 {
		fmt.Fprintf(w, "    NOTE resolution near zero: this is saying much the same thing about\n")
		fmt.Fprintf(w, "         every game. That can be perfectly calibrated and still worthless.\n")
	}

	if r.Converged {
		fmt.Fprintf(w, "\n  CALIBRATION SLOPE  %.3f  (se %.3f, intercept %+.3f)\n",
			r.Slope, r.SlopeSE, r.Intercept)
		switch {
		case r.Slope < 0:
			fmt.Fprintf(w, "    BELOW ZERO: higher belief went with the scenario happening LESS — its\n")
			fmt.Fprintf(w, "    confident calls point the wrong way, which is worse than saying nothing.\n")
		default:
			fmt.Fprintf(w, "    1.0 is honest; below 1 means its confident calls are over-confident.\n")
		}
	} else {
		fmt.Fprintf(w, "\n  CALIBRATION SLOPE  unmeasurable — %s\n", slopeFailure(r))
	}
	fmt.Fprintf(w, "  DISCRIMINATION     AUC %.3f, separation %+.3f\n", r.AUC, r.Separation)

	if ref := res.Reference; ref != nil && ref.HasRef {
		fmt.Fprintf(w, "\n  AGAINST THE REFERENCE  (%d of %d rows had one)\n", ref.RefN, ref.Positions)
		fmt.Fprintf(w, "    reference Brier %.4f   skill %+.3f\n", ref.RefBrier, ref.Skill)
		fmt.Fprintf(w, "    paired gain     %+.5f  [%+.5f, %+.5f]\n", ref.Gain, ref.GainLo, ref.GainHi)
		fmt.Fprintf(w, "\n    Calibration is not an edge. These two are what the wager needs:\n")
		fmt.Fprintf(w, "    mean |p−ref|    %.4f   does it disagree by enough to matter?\n",
			ref.MeanAbsDisagreement)
		fmt.Fprintf(w, "    over the bar    %d of %d at ±%.2f\n", ref.OverBar, ref.RefN, ref.Bar)
		if ref.OverBar > 0 {
			fmt.Fprintf(w, "    informed        %.1f%%   of those, the share that moved the RIGHT way\n",
				ref.InformedFraction*100)
			if ref.InformedFraction < 0.5 {
				fmt.Fprintf(w, "    NOTE below half: its big disagreements point the wrong way, which\n")
				fmt.Fprintf(w, "         is worse than not disagreeing at all.\n")
			}
		}
	} else {
		fmt.Fprintf(w, "\n  No reference on any row — nothing to beat, so no edge can be claimed.\n")
	}

	if f := res.Falsifier; f != nil {
		fmt.Fprintf(w, "\n  THE FALSIFIER  %d of %d predictions had a claim falsified\n", f.Falsified, f.Total)
		fmt.Fprintf(w, "    survivors Brier %.4f   all %.4f   (lower is better)\n", f.SurvivorsBrier, f.AllBrier)
		fmt.Fprintf(w, "    Discarding them is part of the strategy, not a filter on the score:\n")
		fmt.Fprintf(w, "    a forecast caught inventing evidence is never wagered on.\n")
	}

	res.Verdict.writeTerminal(w)
	writeReferenceRows(w, res.ByReference)
	writeGroups(w, "BY SCENARIO", res.ByScenario)
	writeGroups(w, "BY STATED CONFIDENCE", res.ByConfidence)
	if res.Flagged != nil {
		res.Flagged.writeTerminal(w)
	}
	res.Power.writeTerminal(w)
}

func (v verdictResult) writeTerminal(w io.Writer) {
	fmt.Fprintf(w, "\n  PRE-REGISTERED ENDPOINT  E1 accuracy is the decision; E2 is a diagnostic\n")
	fmt.Fprintf(w, "                 on %s only — the other scenarios are scored but do not decide\n",
		strings.Join(v.DecisionScenarios, " and "))

	if !v.E1Decidable {
		fmt.Fprintf(w, "    E1 accuracy     no opponent is evaluable yet\n")
	} else {
		fmt.Fprintf(w, "    E1 accuracy     vs the hardest of %d opponents (%s)\n", v.BindingCount, v.BindingName)
		fmt.Fprintf(w, "                    paired Brier gain %+.5f  [%+.5f, %+.5f]   %s\n",
			v.Gain, v.Lo, v.Hi, verdictWord(v.E1Pass, false))
	}

	if v.E2Wagers == 0 {
		fmt.Fprintf(w, "    E2 diagnostic   no wagers implied yet\n")
	} else {
		fmt.Fprintf(w, "    E2 diagnostic   expected ROI %+.4f  [%+.4f, %+.4f]  on %d implied wagers\n",
			v.E2Edge, v.E2Lo, v.E2Hi, v.E2Wagers)
		fmt.Fprintf(w, "                    per unit staked at a %.0f%% hold, assuming the frozen site is\n", v.Hold*100)
		fmt.Fprintf(w, "                    exact; a robustness check on E1, not independent of it\n")
	}

	switch {
	case !v.E1Decidable:
		fmt.Fprintf(w, "    VERDICT         not yet decidable — no opponent is evaluable\n")
	case v.E1Pass:
		fmt.Fprintf(w, "    VERDICT         PASS — more accurate than every opponent that exists\n")
	default:
		fmt.Fprintf(w, "    VERDICT         FAIL — it loses to at least one opponent (%s)\n", v.BindingName)
	}
}

func writeReferenceRows(w io.Writer, rows []referenceRow) {
	fmt.Fprintf(w, "\n  BY REFERENCE  (each opponent scored on its own rows — never pooled)\n")
	fmt.Fprintf(w, "    %-24s %6s %10s %11s\n", "", "n", "gain", "over ±bar")
	if len(rows) == 0 {
		fmt.Fprintf(w, "    (no row carried any reference yet)\n")
		return
	}
	for _, row := range rows {
		gs := "—"
		if !row.GainNaN {
			gs = fmt.Sprintf("%+.5f", row.Gain)
		}
		over := "—"
		if row.ScoreOK {
			over = fmt.Sprintf("%d of %d", row.OverBar, row.RefN)
		}
		name := row.Name
		if !row.Binding {
			name += " (floor)"
		}
		fmt.Fprintf(w, "    %-24s %6d %10s %11s  [%+.5f, %+.5f]\n",
			name, row.N, gs, over, row.Lo, row.Hi)
	}
}

func writeGroups(w io.Writer, title string, rows []groupRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  %s\n", title)
	fmt.Fprintf(w, "    %-20s %6s %8s %9s %9s %11s\n",
		"", "n", "Brier", "resol.", "gain", "informed")
	for _, row := range rows {
		if row.AllAbstentions {
			fmt.Fprintf(w, "    %-20s %6d   %s\n", row.Key, row.N, "all abstentions")
			continue
		}
		inf := "—"
		if row.OverBar > 0 {
			inf = fmt.Sprintf("%.0f%% of %d", row.InformedFraction*100, row.OverBar)
		}
		gs := "—"
		if !row.GainNaN {
			gs = fmt.Sprintf("%+.5f", row.Gain)
		}
		fmt.Fprintf(w, "    %-20s %6d %8.4f %9.4f %9s %11s\n",
			row.Key, row.N, row.Brier, row.Resolution, gs, inf)
	}
}

func (f flaggedSummary) writeTerminal(w io.Writer) {
	fmt.Fprintf(w, "\n  FLAGGED CANDIDATES  %d of %d\n", f.YesCount, f.Total)
	for _, row := range f.Rows {
		gs := "—"
		if !row.GainNaN {
			gs = fmt.Sprintf("%+.5f", row.Gain)
		}
		fmt.Fprintf(w, "    %-10s n=%-4d Brier %.4f   gain %s\n", row.Name, row.N, row.Brier, gs)
	}
	fmt.Fprintf(w, "    Flagging does not filter the score. If these are no better than the\n")
	fmt.Fprintf(w, "    rest, the forecaster cannot pick its own spots — a different finding\n")
	fmt.Fprintf(w, "    from being unable to forecast.\n")
}

func (p powerSummary) writeTerminal(w io.Writer) {
	fmt.Fprintf(w, "\n  POWER  %d committed positions (decision scenarios).\n", p.N)
	switch {
	case p.N < 120:
		fmt.Fprintf(w, "    Even a large edge (+0.15) is under 50%% detectable here. READ NOTHING\n")
		fmt.Fprintf(w, "    INTO THE SIGN YET.\n")
	case p.N < 200:
		fmt.Fprintf(w, "    A +0.15 edge is ~55-60%% detectable; +0.10 is ~30%%, a coin flip or worse.\n")
	case p.N < 340:
		fmt.Fprintf(w, "    A +0.15 edge approaches ~70%%; +0.10 is ~42%% and still not decidable.\n")
	default:
		fmt.Fprintf(w, "    +0.15 reaches ~80%% (the week-18 decision point); +0.10 tops out near\n")
		fmt.Fprintf(w, "    50%% in a full season and +0.05 is undetectable — both still clear the bar\n")
		fmt.Fprintf(w, "    on the best sites, a real profitable-but-untestable regime.\n")
	}
	if len(p.NotWagerable) > 0 {
		fmt.Fprintf(w, "\n  NOT WAGERABLE  %s — score it, but do not decide on it.\n",
			strings.Join(p.NotWagerable, ", "))
	}
}

// ingestOpts is the resolved ingest request beyond the files themselves.
type ingestOpts struct {
	dropLate bool
	partial  bool
}

// IngestResult is one ingest attempt as data: what would be written and what the
// gates found. The CLI prints it; the web handler serializes it. A fatal,
// whole-file refusal (bad hash, wrong week, an unknown game, a late file without
// -drop-late, a partial file without -partial) comes back as an error instead,
// the same in both callers.
type IngestResult struct {
	Season   int    `json:"season"`
	Week     int    `json:"week"`
	Model    string `json:"model"`
	PackPath string `json:"pack_path"`
	SHA      string `json:"pack_sha256"`

	Ready         int          `json:"ready"`
	Tally         falsifyTally `json:"falsifier"`
	OnlyNarrative int          `json:"only_narrative"`
	Late          []string     `json:"late,omitempty"`
	Missing       []string     `json:"missing,omitempty"`

	ready []betlog.Prediction // the records to append on apply; not serialized
}

// runIngest validates a forecast against its pack and returns what would be
// written. It reads and writes nothing; the caller records res.ready on apply.
func runIngest(pk inputPack, ff forecastFile, sum, packPath string, now time.Time, opts ingestOpts) (IngestResult, error) {
	if ff.InputPackSHA != sum {
		return IngestResult{}, fmt.Errorf(
			"beliefs: the forecast claims pack sha %s but %s hashes to %s — it was written "+
				"against different facts than the ones on disk",
			short(ff.InputPackSHA), packPath, short(sum))
	}
	if ff.Season != pk.Season || ff.Week != pk.Week {
		return IngestResult{}, fmt.Errorf("beliefs: the forecast is for %d week %d, the pack for %d week %d",
			ff.Season, ff.Week, pk.Season, pk.Week)
	}

	byGame := map[string]packGame{}
	for _, g := range pk.Games {
		byGame[g.GameID] = g
	}
	c, err := scenario.LoadConditionals()
	if err != nil {
		return IngestResult{}, err
	}
	bel, err := scenario.LoadBelief()
	if err != nil {
		return IngestResult{}, err
	}

	flagged := map[string]bool{}
	for _, f := range ff.Flagged {
		flagged[f] = true
	}

	res := IngestResult{Season: pk.Season, Week: pk.Week, Model: ff.Model, PackPath: packPath, SHA: sum}
	seen := map[string]bool{}

	for _, p := range ff.Predictions {
		g, ok := byGame[p.GameID]
		if !ok {
			return IngestResult{}, fmt.Errorf("beliefs: %s is not a game in this pack", p.GameID)
		}
		def, ok := pk.Definitions[p.Scenario]
		if !ok {
			return IngestResult{}, fmt.Errorf("beliefs: %q is not a scenario in this pack", p.Scenario)
		}
		team := board.CanonicalTeam(p.Team)
		unit := "team"
		if def.Basis == "total" {
			unit = "game"
		}
		if unit == "game" && team != "" {
			return IngestResult{}, fmt.Errorf(
				"beliefs: %s is a property of the game, but %s carries team %q",
				p.Scenario, p.GameID, p.Team)
		}
		if unit == "team" {
			if team != board.CanonicalTeam(g.Away) && team != board.CanonicalTeam(g.Home) {
				return IngestResult{}, fmt.Errorf("beliefs: %q is not a side of %s (%s at %s)",
					p.Team, p.GameID, g.Away, g.Home)
			}
		}
		key := p.GameID + "/" + team + "/" + p.Scenario
		if seen[key] {
			return IngestResult{}, fmt.Errorf(
				"beliefs: %s appears twice in this file — a second opinion on the same claim "+
					"is exactly the hindsight this log exists to prevent", key)
		}
		seen[key] = true

		pred := betlog.Prediction{
			Season: pk.Season, Week: pk.Week,
			GameID: p.GameID, Team: team, Scenario: p.Scenario,
			Belief: p.Belief, Confidence: p.Confidence,
			Source: "prompt", Model: ff.Model, Prompt: ff.Prompt,
			InputPack: packPath, InputPackSHA: sum,
			Kickoff: g.Kickoff, GeneratedAt: ff.GeneratedAt,
			Abstained: p.Abstained, Flagged: flagged[key],
			Claims: p.Claims,
		}
		// Rejection is decided HERE, by checking the reasons against the facts the
		// forecaster was handed -- never taken from the file.
		fr := falsifyPrediction(p, g, g.Home, g.Away)
		res.Tally.add(fr, len(p.Claims))
		if fr.Reason != "" {
			pred.Rejected, pred.RejectedReason = true, fr.Reason
		}
		if fr.OnlyUnverifiable(len(p.Claims)) {
			res.OnlyNarrative++
		}
		freezeReferences(&pred, pk, g, c, bel)

		if err := pred.Validate(); err != nil {
			return IngestResult{}, err
		}
		if !ff.GeneratedAt.Before(g.Kickoff) || !now.Before(g.Kickoff) {
			res.Late = append(res.Late, key)
			continue
		}
		res.ready = append(res.ready, pred)
	}

	// Completeness. A forecast for EVERY row in the pack -- abstain where there is
	// no read, never omit -- so a file missing rows is refused rather than scored
	// on a self-selected subset.
	for _, g := range pk.Games {
		for name, def := range pk.Definitions {
			if def.Basis == "total" {
				if k := g.GameID + "//" + name; !seen[k] {
					res.Missing = append(res.Missing, k)
				}
				continue
			}
			for _, t := range []string{board.CanonicalTeam(g.Away), board.CanonicalTeam(g.Home)} {
				if k := g.GameID + "/" + t + "/" + name; !seen[k] {
					res.Missing = append(res.Missing, k)
				}
			}
		}
	}
	if len(res.Missing) > 0 && !opts.partial {
		return IngestResult{}, fmt.Errorf(
			"beliefs: the forecast is missing %d of the pack's rows (e.g. %s). The contract "+
				"is a forecast for every row — abstain, do not omit — so a partial file is "+
				"refused rather than scored on a self-selected subset. Pass -partial to accept "+
				"it anyway (rehearsal only; the pre-registered run does not)",
			len(res.Missing), strings.Join(firstN(res.Missing, 3), ", "))
	}
	if len(res.Late) > 0 && !opts.dropLate {
		return IngestResult{}, fmt.Errorf(
			"beliefs: %d of %d predictions were made or ingested after kickoff (%s). "+
				"A systematically late file is a broken process, not a data point — pass "+
				"-drop-late if you mean to keep the rest, and the dropped count is recorded",
			len(res.Late), len(ff.Predictions), strings.Join(firstN(res.Late, 3), ", "))
	}
	res.Ready = len(res.ready)
	return res, nil
}

// writeTerminal renders an ingest result exactly as the CLI always has, to
// stdout (the web handler serializes the struct instead of calling this).
func (res IngestResult) writeTerminal(dry bool) {
	fmt.Printf("BELIEFS  %d week %d  from %s\n", res.Season, res.Week, res.Model)
	fmt.Printf("  pack     %s  (sha %s)\n", res.PackPath, short(res.SHA))
	fmt.Printf("  ready    %d\n", res.Ready)
	res.Tally.report(res.Ready+len(res.Late), res.OnlyNarrative)
	if len(res.Late) > 0 {
		fmt.Printf("  DROPPED  %d made after kickoff\n", len(res.Late))
	}
	if len(res.Missing) > 0 {
		fmt.Printf("  PARTIAL  %d of the pack's rows are missing — accepted under -partial; "+
			"the verdict rests on a self-selected subset\n", len(res.Missing))
	}
	if dry {
		fmt.Printf("\n-n: nothing written\n")
	}
}

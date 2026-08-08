// Package scenario turns a game line into the probability of a game script.
//
// A prop bet made on a narrative is a conditional claim: not "this player beats
// 52.5 yards" but "if this turns into a shootout, he beats it, and I think a
// shootout is likely." The two halves are estimated very differently, and
// keeping them apart is what makes the claim arguable instead of a vibe.
//
// This package supplies the second half. The game total and spread are a free,
// public forecast of the game script, so deriving a scenario probability from
// them gives a baseline to disagree with -- and the disagreement, not the level,
// is where an edge would live.
//
// # The assumption
//
// Final totals and margins are modelled as normal. That is the single largest
// modelling assumption here and it is wrong in the tails: NFL margins cluster on
// 3 and 7 and have fatter tails than a normal. Calibrating sigma against real
// moneylines bears this out -- a 3-point spread implies about 14.0 and a 7-point
// spread about 11.6, which a single normal cannot reconcile.
//
// Read scenario probabilities near the middle of the distribution as reasonable
// and extreme thresholds as indicative only.
package scenario

import (
	"fmt"
	"math"

	"edge/internal/wager"
)

// Default dispersions, in points. These are conventional empirical values for
// NFL final scores, not fitted constants; both are overridable.
const (
	DefaultSigmaTotal  = 10.0
	DefaultSigmaMargin = 13.5
)

// Basis is the quantity a scenario's threshold is measured against.
type Basis int

const (
	// Total measures combined final points. "Shootout" is a Total scenario.
	Total Basis = iota
	// Margin measures the favourite's final margin (favourite minus
	// underdog), so it may be negative when the favourite loses. Used as the
	// proxy for blowout and garbage-time scripts.
	Margin
)

func (b Basis) String() string {
	if b == Margin {
		return "margin"
	}
	return "total"
}

// Source records where a scenario's probability came from.
//
// This is the extension seam. Downstream code -- decomposition, verdict,
// ladder, log -- takes a Scenario and never asks how it was produced, so a new
// source can be added by writing a constructor rather than reworking callers.
// Recording it also lets the bet log score stated scenarios separately from
// derived ones, and find out which is better calibrated.
type Source int

const (
	// Stated: the operator supplied the probability directly.
	Stated Source = iota
	// DerivedFromLine: computed from the game total or spread.
	DerivedFromLine
	// DerivedFromSignals is reserved. Signals (target funnel meeting a funnel
	// defense, a snap-share trend implying a role change) can both estimate
	// the probability of a named scenario and identify which scenario is in
	// play at all. Not implemented; declared so the type does not change when
	// it is.
	DerivedFromSignals
)

func (s Source) String() string {
	switch s {
	case DerivedFromLine:
		return "derived-from-line"
	case DerivedFromSignals:
		return "derived-from-signals"
	default:
		return "stated"
	}
}

// Scenario is a named game script with a probability and a provenance.
type Scenario struct {
	Name      string
	Basis     Basis
	Threshold float64 // combined points, or favourite's final margin
	Prob      float64
	Source    Source
}

func (s Scenario) String() string {
	return fmt.Sprintf("%s (%s > %.1f) p=%.3f [%s]",
		s.Name, s.Basis, s.Threshold, s.Prob, s.Source)
}

// Validate reports whether the scenario is usable.
func (s Scenario) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("scenario: unnamed scenario")
	}
	if s.Prob < 0 || s.Prob > 1 {
		return fmt.Errorf("scenario %q: probability %v out of range [0,1]", s.Name, s.Prob)
	}
	return nil
}

// normalCDF is the standard normal distribution function.
func normalCDF(z float64) float64 {
	return 0.5 * (1 + math.Erf(z/math.Sqrt2))
}

// normalQuantile inverts normalCDF.
func normalQuantile(p float64) (float64, error) {
	if p <= 0 || p >= 1 {
		return 0, fmt.Errorf("scenario: quantile undefined at p=%v", p)
	}
	return math.Sqrt2 * math.Erfinv(2*p-1), nil
}

// StateProb builds a scenario from a probability the operator supplies.
func StateProb(name string, basis Basis, threshold, prob float64) (Scenario, error) {
	s := Scenario{Name: name, Basis: basis, Threshold: threshold, Prob: prob, Source: Stated}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

// FromTotal derives P(combined final points > threshold) from a posted game
// total. Pass 0 for sigma to use DefaultSigmaTotal.
//
// This is the "shootout" construction: a 52.5 total against a 50-point
// threshold gives about 60%, a 41 total gives about 18%.
func FromTotal(name string, total, threshold, sigma float64) (Scenario, error) {
	if total <= 0 {
		return Scenario{}, fmt.Errorf("scenario: game total %v must be positive", total)
	}
	if sigma == 0 {
		sigma = DefaultSigmaTotal
	}
	if sigma <= 0 {
		return Scenario{}, fmt.Errorf("scenario: sigma %v must be positive", sigma)
	}
	return Scenario{
		Name:      name,
		Basis:     Total,
		Threshold: threshold,
		Prob:      1 - normalCDF((threshold-total)/sigma),
		Source:    DerivedFromLine,
	}, nil
}

// FromSpread derives P(favourite's final margin > threshold) from a posted
// spread. Pass 0 for sigma to use DefaultSigmaMargin.
//
// spread is the favourite's handicap as posted, so -3 means favoured by three.
// threshold is a margin in the same direction (favourite minus underdog) and
// may be negative: threshold 0 asks how often the favourite simply wins,
// threshold 10 asks how often it becomes a blowout.
func FromSpread(name string, spread, threshold, sigma float64) (Scenario, error) {
	if sigma == 0 {
		sigma = DefaultSigmaMargin
	}
	if sigma <= 0 {
		return Scenario{}, fmt.Errorf("scenario: sigma %v must be positive", sigma)
	}
	expected := -spread // a -3 favourite is expected to win by 3
	return Scenario{
		Name:      name,
		Basis:     Margin,
		Threshold: threshold,
		Prob:      1 - normalCDF((threshold-expected)/sigma),
		Source:    DerivedFromLine,
	}, nil
}

// minCalibrationSpread is the shortest spread that can calibrate sigma.
//
// Below it the normal quantile approaches zero and sigma explodes: a 1.5-point
// spread with a fair 52.2% favourite implies sigma = 27.5, which is nonsense.
// The estimate is a ratio with a near-zero denominator, so it is refused rather
// than returned.
const minCalibrationSpread = 3.0

// CalibrateSigma solves the margin dispersion implied by a spread and its
// moneyline, rather than assuming DefaultSigmaMargin.
//
// fairProbFav must be DE-VIGGED -- use wager.Market.FairDevig on the two
// moneylines first. A raw implied probability is inflated and will understate
// sigma.
//
// This is a single-game estimate and it is noisy: real spreads disagree with
// each other (3 points implies about 14.0, 7 points about 11.6) because margins
// are not normal. Treat the result as a sanity check on the default, not a
// replacement for it.
func CalibrateSigma(spread, fairProbFav float64) (float64, error) {
	expected := math.Abs(spread)
	if expected < minCalibrationSpread {
		return 0, fmt.Errorf(
			"scenario: cannot calibrate sigma from a %.1f-point spread "+
				"(below %.1f the quantile approaches zero and sigma explodes)",
			expected, minCalibrationSpread)
	}
	if fairProbFav <= 0.5 || fairProbFav >= 1 {
		return 0, fmt.Errorf(
			"scenario: favourite's fair probability %v must be in (0.5, 1) — "+
				"did you pass a de-vigged probability for the favourite?", fairProbFav)
	}
	z, err := normalQuantile(fairProbFav)
	if err != nil {
		return 0, err
	}
	return expected / z, nil
}

// CalibrateSigmaFromMoneyline is CalibrateSigma with the de-vigging done for
// you, reusing the two-sided market handling in package wager.
func CalibrateSigmaFromMoneyline(spread float64, fav, dog wager.American) (float64, error) {
	fairFav, _, err := wager.Market{A: fav, B: dog}.FairDevig()
	if err != nil {
		return 0, err
	}
	return CalibrateSigma(spread, fairFav)
}

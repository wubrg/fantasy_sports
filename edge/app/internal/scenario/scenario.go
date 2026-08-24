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
// # How the probabilities are produced
//
// By default this uses an EMPIRICAL residual distribution fitted from ~1,600
// real games, embedded as artifacts/residuals.json. It assumes no shape at all.
//
// It did originally assume normality, with sigma 10.0 for totals and 13.5 for
// margins, and that turned out to be badly wrong for totals -- the real residual
// sd is 13.3, not 10.0. Measured on games the fit never saw, the normal model's
// mean absolute calibration gap was 4.30 points for totals and 3.80 for margins;
// the empirical model gets 0.78 and 1.89. The old curve claimed 7.5% where
// reality was 14.7%, and 73.6% where reality was 57.4%.
//
// The normal path is still reachable by passing an explicit sigma, so the two
// can be compared rather than swapped on faith. Passing sigma = 0 selects the
// empirical model, which is what callers should normally do.
//
// # What is still assumed
//
// One pooled residual distribution serves every line level and every game. That
// is supported by measurement -- dispersion is flat across line level, 12.8 to
// 13.9 in every bucket -- but it is still an assumption, and it means a game
// with unusual conditions is priced like an average one.
package scenario

import (
	"fmt"
	"math"
	"strings"

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
	// OffensePROE measures a team's pass rate over expected: how much more it
	// threw than down, distance, score and time called for.
	//
	// Unlike Total and Margin there is no residual distribution fitted for it,
	// so its probability cannot be derived from a posted line -- the market
	// does not price a team's PROE. A scenario on this basis therefore requires
	// an operator-supplied -smarket, and says so rather than inventing one.
	OffensePROE
	// SuccessRate measures the fraction of a team's scrimmage plays that
	// gained positive expected points -- the efficiency staple, and the
	// substitute for DVOA, which is proprietary.
	//
	// Like OffensePROE it has no fitted residual distribution: books price
	// points and margins, not a team's success rate, so there is no line to
	// derive its probability from.
	SuccessRate
)

func (b Basis) String() string {
	for _, e := range basisTable {
		if e.Basis == b {
			return e.Name
		}
	}
	// Naming an unknown value rather than defaulting to "total": a Basis that
	// fell out of the table should be visible, not disguised as a valid one.
	return fmt.Sprintf("Basis(%d)", int(b))
}

// basisTable is the ONE list. Names, order, parsing and help text all read
// from it, so adding a basis is a single row and nothing can be left behind.
//
// This was three separate lists -- a switch in the parser, a switch in String,
// and a hardcoded help string -- and they drifted exactly as you would expect:
// the help advertised "total or margin" long after two more existed. A test
// was written to catch that and could not, because a parser case absent from
// the advertised list is invisible to a test that iterates the advertised list.
// Structure beats assertion here: with one table the failure cannot occur.
var basisTable = []struct {
	Name  string
	Basis Basis
}{
	{"total", Total},
	{"margin", Margin},
	{"offense_proe", OffensePROE},
	{"success_rate", SuccessRate},
}

// basisAliases are shorthands accepted on input and never advertised.
var basisAliases = map[string]string{
	"t":       "total",
	"m":       "margin",
	"proe":    "offense_proe",
	"success": "success_rate",
}

// Bases is every basis a scenario may be defined on, in a stable order.
func Bases() []Basis {
	out := make([]Basis, 0, len(basisTable))
	for _, e := range basisTable {
		out = append(out, e.Basis)
	}
	return out
}

// ParseBasis maps a name to a Basis, accepting shorthands.
func ParseBasis(s string) (Basis, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	if full, ok := basisAliases[name]; ok {
		name = full
	}
	for _, e := range basisTable {
		if e.Name == name {
			return e.Basis, nil
		}
	}
	return Total, fmt.Errorf("scenario: %q is not a basis (want one of %s)", s, BasisNames())
}

// BasisNames is the canonical names, comma-separated, for help text and errors.
func BasisNames() string {
	out := make([]string, 0, len(basisTable))
	for _, e := range basisTable {
		out = append(out, e.Name)
	}
	return strings.Join(out, ", ")
}

// NeedsStatedProbability reports whether this basis has no fitted residual
// distribution, so its scenario probability must be supplied rather than
// derived from a line.
func (b Basis) NeedsStatedProbability() bool {
	return b == OffensePROE || b == SuccessRate
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
	// DerivedFromSignals: read off the fitted belief model from a team's own
	// prior form. This was the reserved seam and is now in use for the two
	// scenarios with no market line -- the probability that used to be an
	// operator's invention. Recorded separately from Stated precisely so the
	// bet log can score the two apart and find out which is better calibrated.
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

	// Below is true when the scenario occurs BELOW the threshold, as
	// blowout_loss does (margin < -7).
	//
	// It exists because its absence was a live bug. The constructors all
	// derive P(X > threshold), so a below-threshold scenario received the
	// complement of its own probability: blowout_loss reported s = 77.5%
	// for an event the market put at 22.5%, and the blend q*s + r*(1-s) then
	// weighted the wrong side. CheckDefinition compared basis and threshold
	// and never the operator, so the one mismatch it could not see is the one
	// that shipped.
	Below bool
}

// applyOp turns P(X > threshold) into the probability the scenario OCCURS,
// which for a below-threshold scenario is the complement.
func (s Scenario) applyOp() Scenario {
	if s.Below {
		s.Prob = 1 - s.Prob
	}
	return s
}

func (s Scenario) String() string {
	// %g for the threshold: a success rate of 0.46 printed as "0.5" here for
	// the same reason it did in Definition.String, and a header that disagrees
	// with the flag the caller just typed is its own small lie.
	op := ">"
	if s.Below {
		op = "<"
	}
	return fmt.Sprintf("%s (%s %s %g) p=%.3f [%s]",
		s.Name, s.Basis, op, s.Threshold, s.Prob, s.Source)
}

// Validate reports whether the scenario is usable.
func (s Scenario) Validate() error {
	if !isFinite(s.Prob) || !isFinite(s.Threshold) {
		return fmt.Errorf("scenario %q: probability %v / threshold %v must be real numbers",
			s.Name, s.Prob, s.Threshold)
	}
	if s.Name == "" {
		return fmt.Errorf("scenario: unnamed scenario")
	}
	if s.Prob < 0 || s.Prob > 1 {
		return fmt.Errorf("scenario %q: probability %v out of range [0,1]", s.Name, s.Prob)
	}
	return nil
}

// isFinite reports whether f is a real number. NaN defeats ordinary range
// checks -- `p < 0 || p > 1` is false for NaN -- so finiteness is tested first.
func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

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

// FromSignals builds a scenario whose probability came from the fitted belief
// model rather than from a person or a game line.
func FromSignals(name string, basis Basis, threshold, prob float64, below bool) (Scenario, error) {
	s := Scenario{Name: name, Basis: basis, Threshold: threshold, Prob: prob,
		Source: DerivedFromSignals, Below: below}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

// StateProb builds a scenario from a probability the operator supplies.
func StateProb(name string, basis Basis, threshold, prob float64, below bool) (Scenario, error) {
	// No complement here: an operator supplying -smarket is stating the
	// probability the scenario OCCURS, not P(X > threshold).
	s := Scenario{Name: name, Basis: basis, Threshold: threshold, Prob: prob,
		Source: Stated, Below: below}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

// FromTotal derives P(combined final points > threshold) from a posted game
// total.
//
// Pass sigma = 0 for the fitted empirical distribution, which is the calibrated
// path and what callers should normally use. A positive sigma selects the older
// normal approximation with that dispersion, kept so the two can be compared.
//
// This is the "shootout" construction: a 52.5 total against a 50-point
// threshold gives about 57%, a 41 total about 22%.
func FromTotal(name string, total, threshold, sigma float64, below bool) (Scenario, error) {
	if !isFinite(total) || !isFinite(threshold) || !isFinite(sigma) {
		return Scenario{}, fmt.Errorf(
			"scenario: total %v / threshold %v / sigma %v must be real numbers", total, threshold, sigma)
	}
	if total <= 0 {
		return Scenario{}, fmt.Errorf("scenario: game total %v must be positive", total)
	}
	if sigma < 0 {
		return Scenario{}, fmt.Errorf("scenario: sigma %v must not be negative", sigma)
	}
	// The residual is actual minus expected; expected is the posted total.
	residual := threshold - total

	var prob float64
	if sigma > 0 {
		prob = 1 - normalCDF(residual/sigma)
	} else {
		m, err := Model()
		if err != nil {
			return Scenario{}, err
		}
		prob = m.Total.SurvivalAt(residual)
	}
	return Scenario{
		Name:      name,
		Basis:     Total,
		Threshold: threshold,
		Prob:      prob,
		Source:    DerivedFromLine,
		Below:     below,
	}.applyOp(), nil
}

// FromSpread derives P(favourite's final margin > threshold) from a posted
// spread.
//
// spread is the favourite's handicap as posted, so -3 means favoured by three.
// threshold is a margin in the same direction (favourite minus underdog) and
// may be negative: threshold 0 asks how often the favourite simply wins,
// threshold 10 asks how often it becomes a blowout.
//
// Pass sigma = 0 for the fitted empirical distribution; a positive sigma
// selects the normal approximation, as with FromTotal.
func FromSpread(name string, spread, threshold, sigma float64, below bool) (Scenario, error) {
	if !isFinite(spread) || !isFinite(threshold) || !isFinite(sigma) {
		return Scenario{}, fmt.Errorf(
			"scenario: spread %v / threshold %v / sigma %v must be real numbers", spread, threshold, sigma)
	}
	if sigma < 0 {
		return Scenario{}, fmt.Errorf("scenario: sigma %v must not be negative", sigma)
	}
	// This package posts a favourite as a negative number; the fitted residuals
	// are indexed by actual-minus-expected and carry no convention of their own,
	// so the conversion happens here and only here.
	expected := -spread
	residual := threshold - expected

	var prob float64
	if sigma > 0 {
		prob = 1 - normalCDF(residual/sigma)
	} else {
		m, err := Model()
		if err != nil {
			return Scenario{}, err
		}
		prob = m.Margin.SurvivalAt(residual)
	}
	return Scenario{
		Name:      name,
		Basis:     Margin,
		Threshold: threshold,
		Prob:      prob,
		Source:    DerivedFromLine,
		Below:     below,
	}.applyOp(), nil
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

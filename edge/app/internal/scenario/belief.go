package scenario

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// beliefJSON is fitted by edge/model/analysis/fit_belief.py.
//
//go:embed artifacts/belief.json
var beliefJSON []byte

// Belief answers the half of the decomposition the grid does not: how likely
// the scenario is at all.
//
// P(hit) = q*s + r*(1-s). q and r are fitted. Where s comes from depends on the
// basis: shootout reads it off the posted total and blowout_loss off the
// spread, both via the residual fit. The other two had no market to read and so
// were STATED BY THE OPERATOR -- a project built to reduce unfalsifiable
// judgement, relocating it into a number someone invents.
//
// A team's own prior form predicts it, and that is measurable. This is the
// same shape the grid uses: band the predictor, read the empirical rate off
// each band, check the bands hold out of sample.
type Belief struct {
	GeneratedAt string                    `json:"generated_at"`
	Seasons     []int                     `json:"seasons"`
	Note        string                    `json:"note"`
	Scenarios   map[string]BeliefScenario `json:"scenarios"`
}

// BeliefScenario is one scenario's occurrence model.
type BeliefScenario struct {
	Field      string       `json:"field"`
	PriorField string       `json:"prior_field"`
	Threshold  float64      `json:"threshold"`
	Bands      []BeliefBand `json:"bands"`

	// Spread is the gap between the loosest and tightest band. A model whose
	// bands all say the same thing is not predicting anything.
	Spread float64 `json:"spread"`
	// Monotone: does P(occurs) rise with the prior? If not, the bands are
	// describing noise rather than ordering anything.
	Monotone bool `json:"monotone"`

	BaseRate        float64 `json:"base_rate"`
	BaseRateHeldOut float64 `json:"base_rate_held_out"`
	// WorstBandShift is the largest gap between a band's fitted rate and its
	// held-out rate. The ORDERING survives out of sample; the LEVEL drifts,
	// and a caller pricing off this should know by how much.
	WorstBandShift float64 `json:"worst_band_shift"`
	Split          int     `json:"split"`
}

// BeliefBand is one range of the prior-form predictor.
type BeliefBand struct {
	// Min and Max are nil at the ends: the outer bands are open, because a
	// team's prior form has no bound the fit can promise.
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	P     float64  `json:"p"`
	N     int      `json:"n"`
	HeldP *float64 `json:"held_p"`
	HeldN int      `json:"held_n"`
}

// Contains reports whether a prior-form value falls in this band.
func (b BeliefBand) Contains(v float64) bool {
	if b.Min != nil && v < *b.Min {
		return false
	}
	if b.Max != nil && v >= *b.Max {
		return false
	}
	return true
}

// Range renders the band's bounds the way a person reads them.
func (b BeliefBand) Range() string {
	lo, hi := "-inf", "+inf"
	if b.Min != nil {
		lo = fmt.Sprintf("%.3f", *b.Min)
	}
	if b.Max != nil {
		hi = fmt.Sprintf("%.3f", *b.Max)
	}
	return lo + ".." + hi
}

var (
	beliefOnce sync.Once
	beliefVal  *Belief
	beliefErr  error
)

// LoadBelief parses the embedded belief artifact once.
func LoadBelief() (*Belief, error) {
	beliefOnce.Do(func() {
		var b Belief
		if err := json.Unmarshal(beliefJSON, &b); err != nil {
			beliefErr = fmt.Errorf("scenario: parsing belief artifact: %w", err)
			return
		}
		beliefVal = &b
	})
	return beliefVal, beliefErr
}

// ErrNoBeliefModel means this scenario's probability is not modelled here --
// either because it has a market line to read instead, or because it is not
// fitted at all.
var ErrNoBeliefModel = fmt.Errorf("no belief model for this scenario")

// Lookup returns P(scenario occurs) for a team whose prior form is `prior`.
func (b *Belief) Lookup(scenario string, prior float64) (BeliefBand, BeliefScenario, error) {
	m, ok := b.Scenarios[scenario]
	if !ok {
		return BeliefBand{}, BeliefScenario{}, fmt.Errorf(
			"scenario: %q has no fitted belief model (modelled: %s).\n"+
				"  shootout and blowout_loss are not modelled here on purpose -- their "+
				"probability comes off the posted total and the spread, which is the "+
				"market's own estimate and beats a team-form model at its own game: %w",
			scenario, strings.Join(b.Names(), ", "), ErrNoBeliefModel)
	}
	for _, band := range m.Bands {
		if band.Contains(prior) {
			return band, m, nil
		}
	}
	return BeliefBand{}, m, fmt.Errorf(
		"scenario: a prior of %g falls outside every fitted band for %q", prior, scenario)
}

// Names lists the scenarios with a fitted belief model.
func (b *Belief) Names() []string {
	out := make([]string, 0, len(b.Scenarios))
	for k := range b.Scenarios {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

package scenario

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

// lineModelJSON is fitted by edge/model/analysis/fit_line_model.py.
//
//go:embed artifacts/line_model.json
var lineModelJSON []byte

// LineModel is the honest null for "does an outside read beat the numbers
// already in the pack".
//
// The 2026-09-01 review showed a four-parameter logistic on the posted total
// and spread beating the base rate by +0.029 -- three times the target edge --
// with no football knowledge at all. So that logistic becomes a mandatory third
// reference: a forecaster earns its keep only by beating not just a constant but
// the market's own line, mechanically converted.
//
// Only efficient_offense and pass_heavy are modelled. shootout and blowout_loss
// already carry an s_market derived from the line, so a line model for them
// would duplicate a reference they already have.
type LineModel struct {
	GeneratedAt string                  `json:"generated_at"`
	Seasons     []int                   `json:"seasons"`
	Features    []string                `json:"features"`
	Transform   LineTransform           `json:"transform"`
	Models      map[string]LineScenario `json:"models"`
}

// LineTransform is the centring/scaling applied to the raw total and margin
// before the coefficients. It MUST match fit_line_model.py exactly, so it is
// read from the artifact rather than hardcoded here.
type LineTransform struct {
	TotalCenter float64 `json:"total_center"`
	TotalScale  float64 `json:"total_scale"`
	MarginScale float64 `json:"margin_scale"`
}

// LineScenario is one fitted logistic: the four coefficients and the provenance
// a reader needs to trust them.
type LineScenario struct {
	Coefficients   []float64 `json:"coefficients"`
	N              int       `json:"n"`
	BaseRate       float64   `json:"base_rate"`
	HeldOutLogLoss float64   `json:"held_out_logloss"`
	Converged      bool      `json:"converged"`
}

var (
	lineModelOnce sync.Once
	lineModelVal  *LineModel
	lineModelErr  error
)

// LoadLineModel parses the embedded line-model artifact once.
func LoadLineModel() (*LineModel, error) {
	lineModelOnce.Do(func() {
		var m LineModel
		if err := json.Unmarshal(lineModelJSON, &m); err != nil {
			lineModelErr = fmt.Errorf("scenario: parsing line-model artifact: %w", err)
			return
		}
		lineModelVal = &m
	})
	return lineModelVal, lineModelErr
}

// Predict returns P(scenario) from the posted total and this team's expected
// margin, or (0, false) if the scenario is not modelled.
//
// expectedMargin is the team's OWN expected margin: positive when favoured. The
// caller flips the home-positive spread_line to the team's frame before calling,
// the same sign discipline freezeReferences uses for s_market.
func (m *LineModel) Predict(scenario string, total, expectedMargin float64) (float64, bool) {
	s, ok := m.Models[scenario]
	if !ok || len(s.Coefficients) != 4 {
		return 0, false
	}
	x := []float64{
		1.0,
		(total - m.Transform.TotalCenter) / m.Transform.TotalScale,
		expectedMargin / m.Transform.MarginScale,
		math.Abs(expectedMargin) / m.Transform.MarginScale,
	}
	var z float64
	for i, c := range s.Coefficients {
		z += c * x[i]
	}
	return sigmoid(z), true
}

func sigmoid(z float64) float64 {
	if z >= 0 {
		return 1 / (1 + math.Exp(-z))
	}
	e := math.Exp(z)
	return e / (1 + e)
}

package betlog

import (
	"fmt"
	"math"
	"time"
)

// Projection is what the grid said about a player/outcome, with nothing
// staked and no recommendation implied -- present whenever the grid prices a
// cell, whether or not the report ends up recommending a wager on it.
//
// It exists to answer a different question than Bet or Prediction do: not
// "did we recommend the right thing" but "was our own median a better
// point-estimate of what the player would do than the market's posted line."
// Recommendation count is small and selection-biased; every priced
// player/outcome is a much larger, much less biased sample of that question.
type Projection struct {
	Season   int    `json:"season"`
	Week     int    `json:"week"`
	Player   string `json:"player"`
	Outcome  string `json:"outcome"`  // receiving_yards, passing_yards, ...
	Scenario string `json:"scenario"` // shootout, pass_heavy, ...

	// OurMedian is the fitted grid's cell median for this player/outcome under
	// this scenario, in the outcome's own units -- the same number printed as
	// "median NN yds" in edgectl scenario's CONDITIONALS block.
	OurMedian float64 `json:"our_median"`
	// MarketLine is the operator-supplied -line: the book's posted O/U number,
	// same units as OurMedian.
	MarketLine float64 `json:"market_line"`

	Baseline float64 `json:"baseline"` // the player's own prior mean, for context
	Trend    float64 `json:"trend"`

	InputPack    string `json:"input_pack,omitempty"`
	InputPackSHA string `json:"input_pack_sha256,omitempty"`

	GeneratedAt time.Time `json:"generated_at"`
	Kickoff     time.Time `json:"kickoff"`
}

// Key is the join key settlement uses: one outcome, for one player, in one
// week, under one scenario. Mirrors Prediction.Key's shape.
func (p Projection) Key() string {
	return fmt.Sprintf("%d/%d/%s/%s/%s", p.Season, p.Week, p.Player, p.Outcome, p.Scenario)
}

// Validate rejects a projection that cannot be scored or settled.
func (p Projection) Validate() error {
	if p.Season <= 0 || p.Week <= 0 {
		return fmt.Errorf("betlog: projection needs a season and week, got %d/%d", p.Season, p.Week)
	}
	if p.Player == "" {
		return fmt.Errorf("betlog: projection has no player")
	}
	if p.Outcome == "" {
		return fmt.Errorf("betlog: projection has no outcome")
	}
	if p.Scenario == "" {
		return fmt.Errorf("betlog: projection has no scenario")
	}
	if math.IsNaN(p.OurMedian) || math.IsInf(p.OurMedian, 0) {
		return fmt.Errorf("betlog: our_median %v is not a real number", p.OurMedian)
	}
	if math.IsNaN(p.MarketLine) || math.IsInf(p.MarketLine, 0) {
		return fmt.Errorf("betlog: market_line %v is not a real number", p.MarketLine)
	}
	if p.Kickoff.IsZero() {
		return fmt.Errorf("betlog: projection has no kickoff, so it cannot be shown to predate one")
	}
	return nil
}

// RecordProjection appends a projection to the log, refusing one made after
// its own kickoff for the same reason Prediction.Record does -- see belief.go
// and ADR-002. `now` is passed rather than read so the gate is testable.
func RecordProjection(path string, p Projection, now time.Time) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if !p.GeneratedAt.IsZero() && !p.GeneratedAt.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s was generated at %s, which is not before kickoff at %s — a projection "+
				"made after the game started is not a projection",
			p.Key(), p.GeneratedAt.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	if !now.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s cannot be recorded at %s, after its kickoff at %s",
			p.Key(), now.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	id := NewID(now, p.Outcome+"-"+p.Player+"-"+p.Scenario)
	if err := Append(path, Entry{Kind: KindProjection, ID: id, Time: now, Projection: &p}); err != nil {
		return "", err
	}
	return id, nil
}

// SettledProjection is a projection joined to what the player actually did.
// Actual is nil until settled, and a *float64 rather than a bare float so
// "0 yards, really" and "not yet known" are distinguishable.
type SettledProjection struct {
	ID         string
	Recorded   time.Time
	Projection Projection
	Result     Result // Open, Measured, or Void
	Actual     *float64
	SettleAt   time.Time
	Note       string
}

// LoadProjections reads the stream and folds settlements onto their
// projections. Mirrors LoadPredictions: a betlog or belief file is refused
// rather than silently scored over the wrong population.
func LoadProjections(path string) ([]SettledProjection, error) {
	recs, _, skipped, err := scan(path)
	if err != nil {
		return nil, err
	}
	warnSkipped(path, skipped)

	out := make([]SettledProjection, 0, len(recs))
	for _, r := range recs {
		if r.Kind == KindBet {
			return nil, fmt.Errorf(
				"betlog: %s holds wagers, not projections — read it with `edgectl log score`", path)
		}
		if r.Kind == KindBelief {
			return nil, fmt.Errorf(
				"betlog: %s holds belief predictions, not projections — read it with "+
					"`edgectl beliefs score`", path)
		}
		out = append(out, SettledProjection{ID: r.ID, Recorded: r.Time, Projection: *r.Proj,
			Result: r.Result, Actual: r.Actual, SettleAt: r.SettleAt, Note: r.Note})
	}
	return out, nil
}

// SettleProjection appends a realized value for a previously recorded
// projection.
func SettleProjection(path, id string, actual float64, note string) error {
	if math.IsNaN(actual) || math.IsInf(actual, 0) {
		return fmt.Errorf("betlog: actual %v is not a real number", actual)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(),
		Result: Measured, Actual: &actual, Note: note})
}

// VoidProjection records that a projection could not be settled -- the
// player did not play, a name could not be matched, whatever the reason. Void
// is excluded from scoring (Result.Counts()), the same convention VoidBelief
// uses and for the same reason: an unmeasurable case is not the same claim as
// "the player did worse than projected."
func VoidProjection(path, id, reason string) error {
	if reason == "" {
		return fmt.Errorf("betlog: voiding %s needs a reason", id)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: Void, Note: reason})
}

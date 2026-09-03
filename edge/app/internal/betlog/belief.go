package betlog

import (
	"fmt"
	"math"
	"time"
)

// Occurred and NotOccurred are the settlement results for a belief.
//
// They are the same wire values as Won and Lost — one stream format, one
// scan — but a scenario does not win or lose, it happens or it does not, and
// code that reads "b.Result == Won" about a game script reads as if money were
// involved. There is none.
const (
	Occurred    = Won
	NotOccurred = Lost
)

// Prediction is a claim that a game script will occur, with nothing staked on
// it.
//
// It exists because the expensive half of a prop wager is `s` — how likely the
// script is — and that half can be tested without a price, a player or a line.
// The grid supplies q and r; s was an operator's invention for two of the four
// scenarios, and this is how a better source for it gets measured rather than
// asserted.
type Prediction struct {
	Season   int    `json:"season"`
	Week     int    `json:"week"`
	GameID   string `json:"game_id"`
	Team     string `json:"team,omitempty"` // empty for a game-level scenario
	Scenario string `json:"scenario"`

	// Belief is P(the scenario occurs). It is the only number being scored.
	Belief float64 `json:"belief"`
	// Confidence is the forecaster's own, in [0,1]. Scored separately, because
	// a source that knows when it is guessing is worth more than one that does
	// not.
	Confidence float64 `json:"confidence,omitempty"`

	Source string `json:"source"` // "prompt" | "base-rate" | "belief-json" | "market"
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt,omitempty"`

	// InputPackSHA binds this prediction to the exact bytes it was shown.
	// Paths are mutable; content is not.
	InputPack    string `json:"input_pack,omitempty"`
	InputPackSHA string `json:"input_pack_sha256,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`

	// The references this prediction is measured against, FROZEN at ingest.
	//
	// This is a correctness requirement rather than a convenience. `make fit`
	// rewrites belief.json, so a reference re-derived at scoring time would let
	// a mid-season refit retroactively change the opponent a forecast was
	// judged against, with nothing to show it had happened.
	//
	// Pointers, not sentinels: "no reference exists" must be distinguishable
	// from a probability of zero. SIncumbent is nil before week 4, when prior
	// form does not yet exist.
	SMarket    *float64 `json:"s_market,omitempty"`
	SBaseRate  *float64 `json:"s_base_rate,omitempty"`
	SIncumbent *float64 `json:"s_incumbent,omitempty"`
	// SLine is the line-only logistic: P(scenario) from the posted total and
	// spread alone. It is the honest null for "does an outside read beat the
	// numbers already in the pack", and exists only for efficient_offense and
	// pass_heavy -- the two scenarios with no market line of their own. Nil
	// where the scenario is not modelled or the game had no posted total/spread.
	SLine     *float64 `json:"s_line,omitempty"`
	PriorForm *float64 `json:"prior_form,omitempty"`

	Kickoff     time.Time `json:"kickoff"`
	GeneratedAt time.Time `json:"generated_at"`

	// Abstained is the forecaster declining to take a position.
	//
	// A real field rather than a marker inside Claims. It was briefly encoded as
	// the string "abstained: no read" appended to the claims and prefix-matched
	// back, which is fragile in both directions: a forecaster writing a genuine
	// claim beginning that way would be misread, and the JSON already carried a
	// boolean that was being thrown away.
	//
	// It matters because a source required to forecast every game -- so its
	// scored set cannot be cherry-picked -- will abstain on most of them, and
	// pooling those hides an edge on the rows where it committed.
	Abstained bool `json:"abstained,omitempty"`

	// Flagged marks a prediction the forecaster would actually bet.
	//
	// It has no effect on scoring, deliberately: the point is to keep the
	// candidate list and the scored set connected without letting the first
	// filter the second.
	Flagged bool `json:"flagged,omitempty"`

	// Rejected marks a prediction whose supporting claims were falsified.
	//
	// It is recorded and settled ANYWAY. Scoring survivors against the whole
	// set on identical outcomes measures what the falsifier is worth; a bare
	// rejection count cannot. Dropping them at ingest would throw that away and
	// leave the log unable to answer it later.
	Rejected       bool     `json:"rejected,omitempty"`
	RejectedReason string   `json:"rejected_reason,omitempty"`
	Claims         []string `json:"claims,omitempty"`
}

// Key is the join key settlement uses: one scenario, for one side, in one game.
func (p Prediction) Key() string {
	if p.Team == "" {
		return fmt.Sprintf("%d/%d/%s/%s", p.Season, p.Week, p.GameID, p.Scenario)
	}
	return fmt.Sprintf("%d/%d/%s/%s/%s", p.Season, p.Week, p.GameID, p.Team, p.Scenario)
}

// Validate rejects a prediction that cannot be scored.
func (p Prediction) Validate() error {
	if p.Season <= 0 || p.Week <= 0 {
		return fmt.Errorf("betlog: prediction needs a season and week, got %d/%d", p.Season, p.Week)
	}
	if p.GameID == "" {
		return fmt.Errorf("betlog: prediction has no game id")
	}
	if p.Scenario == "" {
		return fmt.Errorf("betlog: prediction has no scenario")
	}
	if math.IsNaN(p.Belief) || math.IsInf(p.Belief, 0) || p.Belief < 0 || p.Belief > 1 {
		return fmt.Errorf("betlog: belief %v is not a probability", p.Belief)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("betlog: confidence %v is out of [0,1]", p.Confidence)
	}
	if p.Kickoff.IsZero() {
		return fmt.Errorf("betlog: prediction has no kickoff, so it cannot be shown to predate one")
	}
	return nil
}

// SettledPrediction is a belief joined to what actually happened.
type SettledPrediction struct {
	ID         string
	Recorded   time.Time
	Prediction Prediction
	Result     Result
	SettleAt   time.Time
	Note       string
}

// Occurred reports whether the scenario happened, and whether that is known.
func (s SettledPrediction) Occurred() (happened, known bool) {
	switch s.Result {
	case Occurred:
		return true, true
	case NotOccurred:
		return false, true
	}
	return false, false
}

// Record appends a belief to the log.
//
// `now` is passed rather than read so the kickoff gate is testable. Both clocks
// are checked: the forecaster's own timestamp catches an honestly late file,
// the wall clock catches a late ingest of an honestly-timestamped one, and
// neither alone is sufficient.
//
// Neither is proof. Nothing here can stop a backdated GeneratedAt; the only
// external evidence is the commit that carried the predictions file, pushed
// before kickoff. See ADR-002 — this is attestation, and saying so is the
// point.
func Record(path string, p Prediction, now time.Time) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if !p.GeneratedAt.IsZero() && !p.GeneratedAt.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s was generated at %s, which is not before kickoff at %s — a forecast "+
				"made after the game started is not a forecast",
			p.Key(), p.GeneratedAt.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	if !now.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s cannot be recorded at %s, after its kickoff at %s",
			p.Key(), now.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	id := NewID(now, p.Scenario+"-"+p.Team+"-"+p.GameID)
	if err := Append(path, Entry{Kind: KindBelief, ID: id, Time: now, Prediction: &p}); err != nil {
		return "", err
	}
	return id, nil
}

// LoadPredictions reads the stream and folds settlements onto their beliefs.
//
// Wagers are refused rather than ignored, mirroring Load: pointing the belief
// scorer at a bet log should fail loudly, not return a number computed over the
// wrong population.
func LoadPredictions(path string) ([]SettledPrediction, error) {
	recs, _, skipped, err := scan(path)
	if err != nil {
		return nil, err
	}
	warnSkipped(path, skipped)

	out := make([]SettledPrediction, 0, len(recs))
	for _, r := range recs {
		if r.Kind == KindBet {
			return nil, fmt.Errorf(
				"betlog: %s holds wagers, not belief predictions — read it with "+
					"`edgectl log score`", path)
		}
		out = append(out, SettledPrediction{ID: r.ID, Recorded: r.Time, Prediction: *r.Pred,
			Result: r.Result, SettleAt: r.SettleAt, Note: r.Note})
	}
	return out, nil
}

// SettleBelief appends an outcome for a recorded belief.
//
// Separate from Settle only so the caller cannot accidentally settle a wager
// from the settlement pass over a belief log; the appended entry is identical.
func SettleBelief(path, id string, happened bool, note string) error {
	r := NotOccurred
	if happened {
		r = Occurred
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: r, Note: note})
}

// VoidBelief records that the scenario could not be measured — too few plays
// carrying xpass, a cancelled game — rather than that it did not happen.
//
// Void is excluded from scoring by Result.Counts(). The distinction matters:
// counting an unmeasurable game as "did not occur" would bias every base rate
// downward, and the games it happens to are not a random sample.
func VoidBelief(path, id, reason string) error {
	if reason == "" {
		return fmt.Errorf("betlog: voiding %s needs a reason", id)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: Void, Note: reason})
}

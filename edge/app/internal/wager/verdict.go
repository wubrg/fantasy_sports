package wager

import "fmt"

// EdgeSource says where a wager's positive expectation is coming from.
//
// This is the question the whole scenario decomposition exists to answer. A
// wager can be +EV because the market's own read on the game already justifies
// it, or because you disagree with the market, or not at all. Those are very
// different situations and only the middle one is what a narrative bet claims
// to be.
type EdgeSource int

const (
	// MarketAlone: the required belief is weaker than what the game line
	// already implies, so the wager is +EV on the market's own view.
	//
	// This is not a free win. It usually means the prop and the game line were
	// set by different models and disagree with each other, which is a real and
	// findable edge -- the rigorous form of the "In-House Arbitrage" signal.
	// But it is also the most likely place to have made an error in q or r,
	// because a genuinely mispriced prop is rarer than a mistaken estimate. It
	// is flagged for scrutiny, not celebrated.
	MarketAlone EdgeSource = iota

	// DisagreementRequired: the wager needs more belief than the market has
	// but no more than you claim. Your read is what carries it. This is the
	// normal case for a narrative bet and the one worth recording.
	DisagreementRequired

	// BeyondYourRead: even your own stated belief does not justify the price.
	BeyondYourRead
)

func (e EdgeSource) String() string {
	switch e {
	case MarketAlone:
		return "market-alone"
	case DisagreementRequired:
		return "disagreement-required"
	default:
		return "beyond-your-read"
	}
}

// Classification places a wager against both the market's view and yours.
type Classification struct {
	Source   EdgeSource
	Required float64 // s*
	Market   float64 // the game line's implied scenario probability
	Yours    float64 // your stated scenario probability
	AtLeast  bool    // whether Required is a floor or a ceiling

	// Disagreement is how far your belief sits from the market's, signed so
	// that positive always means "in the direction that helps this wager".
	Disagreement float64
}

// Classify compares the required belief against the market's and yours.
//
// The comparison respects the direction of the requirement. When the scenario
// helps the wager (q > r) a higher belief is better and Required is a floor;
// when it hurts, a lower belief is better and Required is a ceiling. Getting
// this backwards would invert every verdict, so it is handled explicitly rather
// than assumed.
func Classify(req BeliefRequirement, sMarket, sYours float64) (Classification, error) {
	for name, v := range map[string]float64{"sMarket": sMarket, "sYours": sYours} {
		if v < 0 || v > 1 {
			return Classification{}, fmt.Errorf("wager: %s = %v out of range [0,1]", name, v)
		}
	}

	c := Classification{
		Required: req.Required,
		Market:   sMarket,
		Yours:    sYours,
		AtLeast:  req.AtLeast,
	}
	if req.AtLeast {
		c.Disagreement = sYours - sMarket
	} else {
		c.Disagreement = sMarket - sYours
	}

	switch {
	case req.AlwaysClears:
		// The baseline alone beats the price, so no belief is doing any work.
		c.Source = MarketAlone
	case req.Unreachable:
		c.Source = BeyondYourRead
	case req.Satisfied(sMarket):
		c.Source = MarketAlone
	case req.Satisfied(sYours):
		c.Source = DisagreementRequired
	default:
		c.Source = BeyondYourRead
	}
	return c, nil
}

// Margin is how much room your belief has over the requirement, in probability
// points. Negative means the requirement is not met.
func (c Classification) Margin() float64 {
	if c.AtLeast {
		return c.Yours - c.Required
	}
	return c.Required - c.Yours
}

// Rung is one step of an alternate-line ladder: a line, its price, and the
// conditional probabilities for that specific line.
//
// Q and R are per-rung because they must be. P(100+ yards | shootout) is not
// P(50+ yards | shootout), and reusing one pair across a ladder would quietly
// assert they were the same.
type Rung struct {
	Line  float64
	Price American
	Q, R  float64
}

// LadderStep is one rung's analysis.
type LadderStep struct {
	Rung
	Requirement    BeliefRequirement
	Classification Classification
	Err            error // set when this rung alone could not be analysed
}

// AnalyzeLadder evaluates every rung of an alternate-line ladder against the
// same pair of beliefs.
//
// It reports the required belief per rung rather than a probability per rung.
// That is deliberate: measurement showed that absolute tail probabilities from
// a short game log are not trustworthy, while "which rung demands the smallest
// leap" is a ranking, and rankings survive a lot of misspecification that
// levels do not.
//
// A rung that fails to analyse does not abort the ladder -- its error is
// recorded and the rest are still evaluated, since a single bad q/r pair should
// not hide the other rungs.
func AnalyzeLadder(rungs []Rung, sMarket, sYours float64) ([]LadderStep, error) {
	if len(rungs) == 0 {
		return nil, fmt.Errorf("wager: no ladder rungs supplied")
	}
	out := make([]LadderStep, 0, len(rungs))
	for _, r := range rungs {
		step := LadderStep{Rung: r}
		req, err := RequiredScenarioProb(r.Price, r.Q, r.R)
		if err != nil {
			step.Err = err
			out = append(out, step)
			continue
		}
		step.Requirement = req
		class, err := Classify(req, sMarket, sYours)
		if err != nil {
			step.Err = err
			out = append(out, step)
			continue
		}
		step.Classification = class
		out = append(out, step)
	}
	return out, nil
}

// BestRung returns the index of the rung with the most room between your belief
// and what the price requires, along with whether any rung qualified at all.
//
// Ranking by margin rather than by expected value is the point. EV depends on
// absolute probabilities, which the measurements say are not reliable at these
// sample sizes; margin depends on the ordering of requirements, which is far
// more robust.
func BestRung(steps []LadderStep) (index int, ok bool) {
	best := -1
	var bestMargin float64
	for i, s := range steps {
		if s.Err != nil || s.Classification.Source == BeyondYourRead {
			continue
		}
		m := s.Classification.Margin()
		if best == -1 || m > bestMargin {
			best, bestMargin = i, m
		}
	}
	if best == -1 {
		return 0, false
	}
	return best, true
}

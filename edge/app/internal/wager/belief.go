package wager

import "fmt"

// This file decomposes a wager into the belief it requires.
//
// A hit rate answers "is this player's baseline rate distinguishable from the
// hurdle?" At the sample sizes a season provides, it essentially never is. But
// a narrative wager is not a claim about baseline -- it is a claim that THIS
// week differs, because of a role change, a matchup, or a forced game script.
// The historical interval answers the wrong question.
//
// So the question changes from "is my edge real?" (unanswerable) to "what would
// I have to believe for this to be +EV?" -- which is a single number, is
// arguable, and can be checked against what the game line already implies.

// BlendedProb is the total probability of a hit under a scenario decomposition.
//
//	P(hit) = q·s + r·(1−s)
//
// where s is the scenario's probability, q is P(hit | scenario) and r is
// P(hit | no scenario).
func BlendedProb(s, q, r float64) (float64, error) {
	for name, v := range map[string]float64{"s": s, "q": q, "r": r} {
		if v < 0 || v > 1 {
			return 0, fmt.Errorf("wager: %s = %v out of range [0,1]", name, v)
		}
	}
	return q*s + r*(1-s), nil
}

// BeliefRequirement is what a price demands of your read on the game.
type BeliefRequirement struct {
	Breakeven float64 // the price's hurdle, vig included
	Q, R      float64 // P(hit | scenario), P(hit | not)
	Required  float64 // s*, the scenario probability at which EV is exactly 0

	// AtLeast is true when the scenario helps the wager (q > r), so the
	// requirement reads "s must be at least Required". When the scenario
	// hurts (q < r) the inequality flips and it reads "s must be at most
	// Required" -- the case for an Under that a shootout would break.
	AtLeast bool

	// AlwaysClears is true when the wager is +EV even if the scenario never
	// happens. The baseline alone beats the price, so the narrative is not
	// carrying the bet and should not be used to justify it.
	AlwaysClears bool

	// Unreachable is true when even certainty about the scenario fails to
	// clear the price.
	Unreachable bool
}

// RequiredScenarioProb solves for the scenario probability at which a wager
// breaks even.
//
//	s* = (p* − r) / (q − r)
//
// The output is a sentence rather than a verdict: "this is +EV if you believe
// there is at least a 32% chance of a shootout." That can be argued with, and
// can be wrong in a way you would notice.
func RequiredScenarioProb(price American, q, r float64) (BeliefRequirement, error) {
	for name, v := range map[string]float64{"q": q, "r": r} {
		if v < 0 || v > 1 {
			return BeliefRequirement{}, fmt.Errorf("wager: %s = %v out of range [0,1]", name, v)
		}
	}
	breakeven, err := price.Breakeven()
	if err != nil {
		return BeliefRequirement{}, err
	}
	if q == r {
		return BeliefRequirement{}, fmt.Errorf(
			"wager: q and r are both %v, so the outcome does not depend on the scenario — "+
				"either the scenario is irrelevant to this wager or one of the two is wrong", q)
	}

	req := BeliefRequirement{
		Breakeven: breakeven,
		Q:         q,
		R:         r,
		Required:  (breakeven - r) / (q - r),
		AtLeast:   q > r,
	}

	// Interpret the solution against the [0,1] range s actually lives in.
	if req.AtLeast {
		req.AlwaysClears = req.Required <= 0
		req.Unreachable = req.Required > 1
	} else {
		// P(hit) falls as s rises, so the baseline is the best case.
		req.AlwaysClears = req.Required >= 1
		req.Unreachable = req.Required < 0
	}
	return req, nil
}

// Satisfied reports whether a stated belief meets the requirement.
func (b BeliefRequirement) Satisfied(s float64) bool {
	if b.AlwaysClears {
		return true
	}
	if b.Unreachable {
		return false
	}
	if b.AtLeast {
		return s >= b.Required
	}
	return s <= b.Required
}

// Sensitivity is how far the required belief moves per unit change in the
// conditional probabilities.
//
//	∂s*/∂q = −s* / (q − r)
//	∂s*/∂r = (p* − q) / (q − r)²
//
// Worth reporting because the answer is often counterintuitive. For a +450
// ladder rung with q=0.40 and r=0.08, s* moves about 1 point per point of q but
// about 2.1 points per point of r -- the baseline rate matters twice as much as
// the scenario rate, which is not where attention naturally goes.
type Sensitivity struct {
	PerPointOfQ float64
	PerPointOfR float64
}

// Sensitivity computes how fragile the requirement is to its inputs.
func (b BeliefRequirement) Sensitivity() Sensitivity {
	d := b.Q - b.R
	return Sensitivity{
		PerPointOfQ: -b.Required / d,
		PerPointOfR: (b.Breakeven - b.Q) / (d * d),
	}
}

// Dominant names the input the conclusion is most sensitive to.
func (s Sensitivity) Dominant() string {
	if abs(s.PerPointOfR) > abs(s.PerPointOfQ) {
		return "r"
	}
	return "q"
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// BlendedEV is the expected value of a wager under a scenario decomposition,
// for either bankroll.
//
// Routing through EV rather than EVRealMoney matters here: a scenario bet on a
// longshot is the framework's "Conversion" wager, and pricing a bonus bet with
// the cash formula is exactly the error found in the source material.
func BlendedEV(b Bankroll, s, q, r float64, price American, stake float64) (float64, error) {
	p, err := BlendedProb(s, q, r)
	if err != nil {
		return 0, err
	}
	return EV(b, p, price, stake)
}

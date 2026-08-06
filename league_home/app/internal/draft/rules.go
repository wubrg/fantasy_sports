// Package draft builds the Hit or Miss auction draft board: keeper pricing,
// player valuation, and the live draft state the draft room serves.
//
// The league's own rules live in leagues/hit_or_miss/, and where Sleeper
// disagrees with them, the league rules win. Keeper pricing is the sharpest
// example: Sleeper does not enforce the league's escalating keeper ladder,
// so the amount it records on a keeper pick is not what the manager is
// actually charged. This package recomputes it.
package draft

import "fmt"

// Rules captures the keeper pricing rules from leagues/hit_or_miss/draft.md.
//
// A keeper costs its prior value plus an increment that grows with each
// consecutive year the player is kept, floored at Minimum:
//
//	price = max(priorValue+increment, Minimum)
//
// The league documents increments for the first three keeps only. Beyond
// that the ladder is undefined, so Price extrapolates and flags it rather
// than silently inventing a number — see FlagIncrementExtrapolated.
type Rules struct {
	// Minimum is the league keeper floor ($10).
	Minimum int
	// Increments are the surcharges by keep count, with index 0 being the
	// first time a player is kept ($5, $10, $15).
	Increments []int
	// ExtraIncrement is added per keep beyond the documented ladder, used
	// only alongside FlagIncrementExtrapolated.
	ExtraIncrement int
	// WaiverBidCarriesForward decides whether a winning FAAB bid becomes
	// a keeper's prior value.
	//
	// draft.md is genuinely ambiguous here. One sentence says a keeper
	// costs "previous draft or FA value" plus the increment, which would
	// carry the bid; a bullet says "Undrafted players (i.e., free agent
	// pickups) are the $10 minimum", which would not.
	//
	// League practice settles it: De'Von Achane, Kyren Williams, Puka
	// Nacua, and Nico Collins were all in-season pickups and all were
	// kept at $10 and then $20, regardless of what was bid for them. So
	// this defaults to false, matching what the league actually does.
	WaiverBidCarriesForward bool
	// SelfPickupResets decides whether a manager re-adding a player they
	// already owned resets that player's cost basis and keep count.
	//
	// It defaults to false because league practice says no: Amon-Ra St.
	// Brown and Sam LaPorta both show up as in-season free agent adds by
	// the manager already holding them — ordinary bye-week roster churn —
	// yet both kept climbing the ladder uninterrupted.
	SelfPickupResets bool
	// TradeResetsEscalationFrom is the first season in which a trade
	// resets a player's keep count, restarting the ladder at the
	// first-keep increment. Empty means it never does.
	//
	// This is season-scoped because the league changed the rule recently
	// and did not apply it retroactively. Kenneth Walker is the evidence:
	// traded before 2024 and kept in 2025 for $31, which is his $21 basis
	// plus the second-keep $10 rather than the third-keep $15 his
	// uninterrupted count would have earned. Prior seasons keep the
	// prices they were actually charged.
	//
	// Only the count resets. The cost basis still follows the player, so
	// the ladder restarts from what the previous manager paid.
	TradeResetsEscalationFrom string
}

// tradeResets reports whether a change of hands resets the keep count in
// the given season.
func (r Rules) tradeResets(season string) bool {
	if r.TradeResetsEscalationFrom == "" {
		return false
	}
	return season >= r.TradeResetsEscalationFrom
}

// DefaultRules returns the Hit or Miss keeper rules as written in
// leagues/hit_or_miss/draft.md: $10 minimum, +$5/+$10/+$15 by keep count.
func DefaultRules() Rules {
	return Rules{
		Minimum: 10,
		// draft.md documents +$5/+$10/+$15 for the first three keeps. The
		// fourth rung is an LM ruling (see below), not in the doc.
		Increments:                []int{5, 10, 15, 15},
		ExtraIncrement:            5,
		TradeResetsEscalationFrom: TradeResetRuleSeason,
	}
}

// TradeResetRuleSeason is the first season the league's current trade rule
// applies: a trade resets a keeper's escalation instead of preserving it,
// while the cost basis still follows the player.
//
// This is an LM ruling, effective going forward and explicitly not applied
// retroactively. It cannot be inferred from the record — across 2021-2025 a
// trade preserved escalation, and Amon-Ra St. Brown climbed $10 -> $20 ->
// $35 across two managers. Backdating it also breaks previously-correct
// rows. Prior seasons therefore keep the prices they were actually charged.
const TradeResetRuleSeason = "2026"

// FourthKeepIncrement is the surcharge for a fourth consecutive keep.
//
// Continuing the ladder's own step would make it +$20, and the league
// agreed that is the principled answer. It is set to +$15 by LM ruling as
// the counterpart to honoring Kenneth Walker's 2025 price as charged: he is
// the only player who reaches a fourth keep in 2026, and the league chose
// to let both the undercharge and the softer rung stand together.
const FourthKeepIncrement = 15

// Keeper pricing flags. These mark prices the league rules do not fully
// determine, so they can be surfaced for an LM ruling instead of being
// quietly assumed correct.
const (
	// FlagFloored means the ladder produced less than the league minimum
	// and the price was raised to it. Expected and benign — it is how a
	// cheap or free-agent keeper reaches $10.
	FlagFloored = "floored-to-minimum"
	// FlagIncrementExtrapolated means the player has been kept more times
	// than draft.md defines an increment for. The league has run since
	// 2021, so a fourth consecutive keep is reachable and unwritten.
	FlagIncrementExtrapolated = "increment-extrapolated-beyond-rules"
)

// Price returns what the league charges to keep a player whose prior value
// was priorValue and who is being kept for the keepCount-th consecutive
// time (keepCount 1 is the first time).
//
// It returns the price and any flags describing rule gaps applied along the
// way. An error is returned only for a nonsensical keepCount, since a zeroth
// keep is a caller bug rather than a rules question.
func (r Rules) Price(priorValue, keepCount int) (int, []string, error) {
	if keepCount < 1 {
		return 0, nil, fmt.Errorf("draft: keep count must be at least 1, got %d", keepCount)
	}
	// A player acquired for nothing (free agent add) has no prior value;
	// negative input would be a data error, so treat it as zero.
	if priorValue < 0 {
		priorValue = 0
	}

	var flags []string
	increment, ok := r.increment(keepCount)
	if !ok {
		flags = append(flags, FlagIncrementExtrapolated)
	}

	price := priorValue + increment
	if price < r.Minimum {
		price = r.Minimum
		flags = append(flags, FlagFloored)
	}
	return price, flags, nil
}

// priorValueOf returns the value an acquisition carries into next year's
// keeper price. Auction and keeper costs always carry; a winning FAAB bid
// only carries when WaiverBidCarriesForward is set, which league practice
// says it should not be.
func (r Rules) priorValueOf(a Acquisition) int {
	if a.Method == MethodWaiver && !r.WaiverBidCarriesForward {
		return 0
	}
	return a.Cost
}

// increment returns the surcharge for the keepCount-th keep, reporting
// whether it came from the documented ladder or was extrapolated past it.
func (r Rules) increment(keepCount int) (int, bool) {
	if keepCount <= len(r.Increments) {
		return r.Increments[keepCount-1], true
	}
	// Continue the ladder's own step size past where it is written down.
	last := 0
	if len(r.Increments) > 0 {
		last = r.Increments[len(r.Increments)-1]
	}
	beyond := keepCount - len(r.Increments)
	return last + beyond*r.ExtraIncrement, false
}

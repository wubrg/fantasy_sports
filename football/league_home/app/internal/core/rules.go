package core

import (
	"embed"
	"encoding/json"
)

// RosterSlot is one starting-lineup position's requirements, from
// rosters.md's Starting Lineup table.
type RosterSlot struct {
	Position    string `json:"position"`
	Starters    int    `json:"starters"`
	MaxOnRoster int    `json:"max_on_roster"`
}

// RosterRules is the full rosters.md transcription (excluding keepers and
// waivers, which get their own structs).
type RosterRules struct {
	StartingSlots []RosterSlot `json:"starting_slots"`
	BenchSlots    int          `json:"bench_slots"`
	IRSlots       int          `json:"ir_slots"`
}

// KeeperRules is the full keeper ruleset from rosters.md and draft.md.
// Pricing is intentionally a formula (NewValue = max(PreviousValue +
// KeepCount*IncrementPerKeep, MinimumValue)) rather than a lookup table,
// since draft.md's own example table is just that formula's output for
// keep counts 1-3 — encoding the formula keeps it correct for any future
// keep count instead of needing a new row added by hand.
type KeeperRules struct {
	MaxKeepers                    int `json:"max_keepers"`
	MinimumValue                  int `json:"minimum_value"`
	IncrementPerKeepCount         int `json:"increment_per_keep_count"`
	LockHoursBeforeDraft          int `json:"lock_hours_before_draft"`
	ExpansionLockHoursBeforeDraft int `json:"expansion_lock_hours_before_draft"`
}

// NewValue computes a keeper's new auction value per draft.md's pricing
// rule: previousValue + 5*keepCount, floored at the league minimum.
// keepCount is 1 for a player's first time being kept, 2 for the second, etc.
func (k KeeperRules) NewValue(previousValue, keepCount int) int {
	v := previousValue + k.IncrementPerKeepCount*keepCount
	if v < k.MinimumValue {
		return k.MinimumValue
	}
	return v
}

// WaiverRules is the free-agent/waiver ruleset from rosters.md.
type WaiverRules struct {
	YearlyBudget        int    `json:"yearly_budget"`
	MinimumBid          int    `json:"minimum_bid"`
	ProcessingSchedule  string `json:"processing_schedule"`
	RespectsUndroppable bool   `json:"respects_undroppable"`
}

// DraftRules is the auction-draft ruleset from draft.md.
type DraftRules struct {
	Format     string `json:"format"`
	BaseBudget int    `json:"base_budget"`
}

// PlayoffFormat is the playoff structure for one league size, from
// league_fees_and_dues.md's Playoffs section.
type PlayoffFormat struct {
	LeagueSize   int `json:"league_size"`
	StartWeek    int `json:"start_week"`
	EndWeek      int `json:"end_week"`
	PlayoffTeams int `json:"playoff_teams"`
	ByeTeams     int `json:"bye_teams"`
}

// MajorityVote is the High Council vote thresholds for one league size,
// from policies_and_procedures.md.
type MajorityVote struct {
	LeagueSize      int `json:"league_size"`
	Majority        int `json:"majority"`
	SurplusMajority int `json:"surplus_majority"`
}

// GovernanceRules is the league-roles and voting ruleset from
// policies_and_procedures.md and league_members.md.
type GovernanceRules struct {
	Roles         []string       `json:"roles"`
	MajorityVotes []MajorityVote `json:"majority_votes"`
}

// Rules is the full current-season ruleset, excluding scoring: scoring
// lives in Sleeper's own league.scoring_settings (the actual source of
// truth points get computed from), so it'll be added later as a live
// Sleeper-backed lookup instead of hand-transcribed here, where it could
// silently drift out of sync (as scoring.md's own half-PPR ambiguity
// already shows). This otherwise deliberately holds only today's rules,
// not a history of past changes — see football/league_home/README.md.
type Rules struct {
	Roster            RosterRules     `json:"roster"`
	Keepers           KeeperRules     `json:"keepers"`
	Waivers           WaiverRules     `json:"waivers"`
	Draft             DraftRules      `json:"draft"`
	TradeDeadlineWeek int             `json:"trade_deadline_week"`
	Playoffs          []PlayoffFormat `json:"playoffs"`
	Governance        GovernanceRules `json:"governance"`
}

//go:embed data/rules.json
var rulesFS embed.FS

// Rules returns the current ruleset.
func (s *Service) Rules() (Rules, error) {
	raw, err := rulesFS.ReadFile("data/rules.json")
	if err != nil {
		return Rules{}, err
	}
	var r Rules
	if err := json.Unmarshal(raw, &r); err != nil {
		return Rules{}, err
	}
	return r, nil
}

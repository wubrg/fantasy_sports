package core

import (
	"embed"
	"encoding/json"
)

//go:embed data/schedule.json
var scheduleFS embed.FS

// ScheduleEvent is one entry in the league's season calendar: either a
// week-anchored event (Week > 0, e.g. trade deadline) or a recurring one
// (Recurring true, e.g. daily waiver processing). It mirrors the Google
// Calendar mentioned in ../communication.md by hand, since that calendar
// has no API league_home can read from.
//
// Week-anchored entries here are kept consistent with Rules
// (TradeDeadlineWeek, Playoffs) rather than re-deriving their own numbers,
// since Rules is the source of truth for those.
type ScheduleEvent struct {
	Label     string `json:"label"`
	Detail    string `json:"detail"`
	Week      int    `json:"week,omitempty"`
	Recurring bool   `json:"recurring,omitempty"`
}

// Schedule returns the current season's non-matchup calendar events.
func (s *Service) Schedule() ([]ScheduleEvent, error) {
	raw, err := scheduleFS.ReadFile("data/schedule.json")
	if err != nil {
		return nil, err
	}
	var events []ScheduleEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, err
	}
	return events, nil
}

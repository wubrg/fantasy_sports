package core

import "leaguehome/internal/sleeper"

// State returns the current NFL season/week, so callers (e.g. "show me
// this week's matchups") don't need to know what week it is.
func (s *Service) State() (sleeper.NFLState, error) {
	return s.sleeper.State()
}

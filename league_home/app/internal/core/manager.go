package core

import (
	"embed"
	"encoding/json"
	"strings"
)

//go:embed data/managers.json
var managersFS embed.FS

// Manager is a real person who has owned a team in the league at some
// point, kept stable across seasons so Sleeper data, locally-curated
// history, and rivalries can all be joined to the same identity even as
// Sleeper display names and team names change year to year. Transcribed
// from league_members.md plus the name variants found in data/history.json
// (e.g. "Chris Bushjost" vs "Chris Buschjost").
type Manager struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Active  bool     `json:"active"`
}

// Managers returns every manager who has ever owned a team in the league,
// past and present.
func (s *Service) Managers() ([]Manager, error) {
	raw, err := managersFS.ReadFile("data/managers.json")
	if err != nil {
		return nil, err
	}
	var managers []Manager
	if err := json.Unmarshal(raw, &managers); err != nil {
		return nil, err
	}
	return managers, nil
}

// ResolveManager finds the manager whose name or alias matches name
// (case-insensitive), for normalizing the name-spelling variants found
// across history.json and Sleeper's own display names. It does not split
// multi-winner fields like "Sam Wieberg / Dakota Graham" — callers with
// those need to split on " / " first.
func ResolveManager(managers []Manager, name string) (Manager, bool) {
	for _, m := range managers {
		if strings.EqualFold(m.Name, name) {
			return m, true
		}
		for _, a := range m.Aliases {
			if strings.EqualFold(a, name) {
				return m, true
			}
		}
	}
	return Manager{}, false
}

package draft

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
)

// Owners maps a Sleeper handle to the manager who owns that team. Reports
// label a team by owner and handle -- both stable -- rather than by the
// Sleeper team name, which the league changes week to week and so cannot be
// relied on to identify anyone.
//
// Keys are lowercased so a handle matches regardless of how it is cased in
// Sleeper or in the CSV; the displayed handle is whatever the caller passes.
type Owners map[string]string

// LoadOwners reads a handle,owner CSV. A missing file yields an empty map and
// no error: the mapping is optional, and without it Label falls back to the
// bare handle, which still identifies the team.
func LoadOwners(path string) (Owners, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Owners{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("draft: opening owners %s: %w", path, err)
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("draft: reading owners %s: %w", path, err)
	}

	out := Owners{}
	if len(records) == 0 {
		return out, nil
	}
	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	handleCol, ok1 := cols["handle"]
	ownerCol, ok2 := cols["owner"]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("draft: owners %s needs handle and owner columns", path)
	}

	for _, rec := range records[1:] {
		if handleCol >= len(rec) || ownerCol >= len(rec) {
			continue
		}
		handle := strings.TrimSpace(rec[handleCol])
		owner := strings.TrimSpace(rec[ownerCol])
		if handle == "" || owner == "" {
			continue
		}
		out[strings.ToLower(handle)] = owner
	}
	return out, nil
}

// Label renders a team label for a Sleeper handle: "Owner (handle)" when the
// owner is known, or the bare handle when it is not. The handle is printed as
// Sleeper reports it; the lookup is case-insensitive.
func (o Owners) Label(handle string) string {
	key := strings.ToLower(strings.TrimSpace(handle))
	if owner, ok := o[key]; ok && owner != "" {
		return fmt.Sprintf("%s (%s)", owner, handle)
	}
	return handle
}

package draft

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// leanDoc is the on-disk shape of a YAML lean set.
//
// Players are grouped under the read rather than carrying it as a field,
// because that is the shape the file is actually edited in: you decide you
// like someone and add one line under a heading you already believe. The
// per-row form asked for the same four columns on every line, and the two
// optional ones were empty almost every time.
//
// caps and notes are separate sections for the same reason. Both are rare —
// a handful of entries against dozens of reads — and pushing them out of the
// list keeps the common line to a name.
type leanDoc struct {
	Must      []string          `yaml:"must,omitempty"`
	Up        []string          `yaml:"up,omitempty"`
	Down      []string          `yaml:"down,omitempty"`
	DND       []string          `yaml:"dnd,omitempty"`
	Undecided []string          `yaml:"undecided,omitempty"`
	Caps      map[string]int    `yaml:"caps,omitempty"`
	Notes     map[string]string `yaml:"notes,omitempty"`
}

// leanHeadings lists the headings that carry reads, in the order a report
// should show them.
var leanHeadings = []string{"must", "up", "down", "dnd", "undecided"}

// ParseLeansYAML reads a lean set in the grouped YAML form, returning the
// reads and the players listed as undecided.
//
// The signature matches ParseLeans so the two formats stay interchangeable
// while both are supported.
func ParseLeansYAML(r io.Reader) (Leans, []string, error) {
	dec := yaml.NewDecoder(r)
	// An unknown heading is a typo, and silently ignoring it would drop
	// every read underneath — the exact failure this format is meant to
	// make impossible.
	dec.KnownFields(true)

	var doc leanDoc
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			// No document at all is no reads, which is legitimate.
			return Leans{}, nil, nil
		}
		return nil, nil, fmt.Errorf("reading leans: %w (headings are %s, plus caps and notes)",
			err, strings.Join(leanHeadings, ", "))
	}

	out := Leans{}
	// listed tracks every name the file mentions, so a cap or note under a
	// name that appears in no group can be called out rather than ignored.
	listed := map[string]string{}
	groups := []struct {
		lean    Lean
		players []string
	}{
		{LeanMust, doc.Must},
		{LeanUp, doc.Up},
		{LeanDown, doc.Down},
		{LeanDND, doc.DND},
	}
	for _, g := range groups {
		for _, name := range g.players {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := normalizeName(name)
			if held, taken := listed[key]; taken {
				return nil, nil, fmt.Errorf("%s is listed twice (as %s and under %s); "+
					"one file holds one read per player", name, held, g.lean)
			}
			listed[key] = string(g.lean)
			out[key] = PlayerLean{Player: name, Lean: g.lean}
		}
	}

	var undecided []string
	for _, name := range doc.Undecided {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := normalizeName(name)
		if held, taken := listed[key]; taken {
			return nil, nil, fmt.Errorf("%s is listed twice (as %s and under undecided); "+
				"one file holds one read per player", name, held)
		}
		listed[key] = "undecided"
		undecided = append(undecided, name)
	}

	if err := attach(doc, out, listed); err != nil {
		return nil, nil, err
	}
	return out, undecided, nil
}

// attach folds the caps and notes sections onto the reads they name.
func attach(doc leanDoc, out Leans, listed map[string]string) error {
	// Sorted so a file with two bad entries always reports the same one
	// first; map order would make the error message flap.
	for _, name := range sortedKeys(doc.Caps) {
		key := normalizeName(name)
		if _, ok := listed[key]; !ok {
			return fmt.Errorf("caps names %s, who is not listed under any heading", name)
		}
		cap := doc.Caps[name]
		if cap < 1 {
			return fmt.Errorf("bad cap %d for %s (whole dollars, at least 1)", cap, name)
		}
		// A cap on an undecided player is a number waiting for a read, so
		// it is recorded only if there is a read to hang it on.
		if pl, ok := out[key]; ok {
			pl.Cap = cap
			out[key] = pl
		}
	}
	for _, name := range sortedKeys(doc.Notes) {
		key := normalizeName(name)
		if _, ok := listed[key]; !ok {
			return fmt.Errorf("notes names %s, who is not listed under any heading", name)
		}
		if pl, ok := out[key]; ok {
			pl.Note = strings.TrimSpace(doc.Notes[name])
			out[key] = pl
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FormatLeansYAML renders reads in the grouped form.
//
// Order within a group is the order given, not alphabetical: a hand-written
// set is often ordered by how much you care, and a conversion that reshuffled
// it would lose something the file was saying.
func FormatLeansYAML(leans []PlayerLean, undecided []string) ([]byte, error) {
	doc := leanDoc{Undecided: undecided}
	for _, pl := range leans {
		switch pl.Lean {
		case LeanMust:
			doc.Must = append(doc.Must, pl.Player)
		case LeanUp:
			doc.Up = append(doc.Up, pl.Player)
		case LeanDown:
			doc.Down = append(doc.Down, pl.Player)
		case LeanDND:
			doc.DND = append(doc.DND, pl.Player)
		default:
			// No read is the undecided group, however it was expressed.
			doc.Undecided = append(doc.Undecided, pl.Player)
			continue
		}
		if pl.Cap > 0 {
			if doc.Caps == nil {
				doc.Caps = map[string]int{}
			}
			doc.Caps[pl.Player] = pl.Cap
		}
		if note := strings.TrimSpace(pl.Note); note != "" {
			if doc.Notes == nil {
				doc.Notes = map[string]string{}
			}
			doc.Notes[pl.Player] = note
		}
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	// Two spaces reads better on a phone than yaml.v3's default four.
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("draft: writing leans: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("draft: writing leans: %w", err)
	}
	return []byte(b.String()), nil
}

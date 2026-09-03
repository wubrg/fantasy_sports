package draft

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Preferences are the personal filters that make positional scarcity your own
// rather than the league's. Owning a piece of an offense takes the rest of it
// off your board, and your own starter's handcuff never goes on it — so the
// count of startable players left is smaller for you than for the room. See
// D8 in docs/backlog.md and the strategy notes in data/draftroom_2026.md.
//
// The zero value filters nothing: a board built without a preferences file is
// exactly the league's, which is why an absent file loads to Preferences{}.
type Preferences struct {
	// OnePerOffense blocks a second player from an NFL team you already own a
	// piece of, unless the pair forms an allowed stack shape.
	OnePerOffense bool
	// NoHandcuffs blocks a second player at the same position on an NFL team
	// you already own — the running back behind your running back. Same team
	// plus same position is the proxy: Sleeper carries no depth chart, so
	// "the backup" cannot be named, but two backs on one team is one anyway.
	NoHandcuffs bool
	// Stacks are the position pairs allowed to share one offense, order
	// independent. A pair here overrides OnePerOffense; it never overrides
	// NoHandcuffs, because a same-position pair is a handcuff, not a stack.
	Stacks []Stack
	// FlexPositions narrows which positions you will actually start in the
	// flex. Empty means the league's own rule, whatever that is.
	//
	// A preference, emphatically not a shape change. The league's flex takes a
	// tight end and eleven other managers will use it that way, so the pool
	// must keep pricing against the league rule — flex demand is split across
	// the eligible positions to set replacement depth, and narrowing that
	// would move every price on the board. This only governs which of your own
	// players fills your own flex.
	FlexPositions []string
	// Offenses are NFL teams (as abbreviations) you have named as targets:
	// deep, points-rich offenses whose players carry the "rich offense"
	// trait on the board. This is a display signal, not a filter -- it
	// changes nothing about blocking, scarcity, or bids.
	Offenses []string
}

// OffenseSet is the target offenses as a lookup keyed by abbreviation, for
// tagging players by team. Empty when none are named.
func (p Preferences) OffenseSet() map[string]bool {
	if len(p.Offenses) == 0 {
		return nil
	}
	out := make(map[string]bool, len(p.Offenses))
	for _, t := range p.Offenses {
		out[t] = true
	}
	return out
}

// Stack is one allowed same-offense position pair, such as QB with WR.
type Stack struct{ A, B string }

// DefaultPreferences is what a present-but-unspecified field falls back to:
// both filters on, and the two pass-catcher stacks a quarterback anchors.
func DefaultPreferences() Preferences {
	return Preferences{
		OnePerOffense: true,
		NoHandcuffs:   true,
		Stacks:        []Stack{{A: "QB", B: "WR"}, {A: "QB", B: "TE"}},
	}
}

// Active reports whether any filtering happens at all. When false, effective
// scarcity equals league scarcity and nothing on the board is blocked.
func (p Preferences) Active() bool { return p.OnePerOffense || p.NoHandcuffs }

// Allows reports whether two positions may share one offense as a stack.
// Order independent. A position never stacks with itself — that is a handcuff.
func (p Preferences) Allows(a, b string) bool {
	a, b = normPos(a), normPos(b)
	if a == b {
		return false
	}
	for _, s := range p.Stacks {
		sa, sb := normPos(s.A), normPos(s.B)
		if (sa == a && sb == b) || (sa == b && sb == a) {
			return true
		}
	}
	return false
}

// prefsDoc is the on-disk YAML shape. The toggles are pointers so an omitted
// key can fall back to the default rather than to Go's false, and Stacks is a
// pointer so an omitted key defaults while an explicit empty list means "no
// stacks allowed" — the two must not read the same.
type prefsDoc struct {
	OnePerOffense *bool       `yaml:"one_per_offense,omitempty"`
	NoHandcuffs   *bool       `yaml:"no_handcuffs,omitempty"`
	Stacks        *[][]string `yaml:"stacks,omitempty"`
	Offenses      *[]string   `yaml:"offenses,omitempty"`
	FlexPositions *[]string   `yaml:"flex_positions,omitempty"`
}

// ParsePreferences reads the personal draft filters from YAML. An unknown key
// is an error rather than a silent no-op, the same choice ParseLeansYAML makes,
// because a filter you think is on and is not costs you the whole point of it.
func ParsePreferences(r io.Reader) (Preferences, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var doc prefsDoc
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			// A present but empty file is a deliberate "use the defaults".
			return DefaultPreferences(), nil
		}
		return Preferences{}, fmt.Errorf("reading preferences: %w "+
			"(keys are one_per_offense, no_handcuffs, stacks, offenses, flex_positions)", err)
	}

	p := DefaultPreferences()
	if doc.OnePerOffense != nil {
		p.OnePerOffense = *doc.OnePerOffense
	}
	if doc.NoHandcuffs != nil {
		p.NoHandcuffs = *doc.NoHandcuffs
	}
	if doc.Stacks != nil {
		p.Stacks = nil
		for _, pair := range *doc.Stacks {
			if len(pair) != 2 {
				return Preferences{}, fmt.Errorf(
					"stack %v needs exactly two positions", pair)
			}
			p.Stacks = append(p.Stacks, Stack{A: normPos(pair[0]), B: normPos(pair[1])})
		}
	}
	if doc.FlexPositions != nil {
		p.FlexPositions = nil
		for _, pos := range *doc.FlexPositions {
			p.FlexPositions = append(p.FlexPositions, normPos(pos))
		}
	}
	if doc.Offenses != nil {
		p.Offenses = nil
		var bad []string
		for _, name := range *doc.Offenses {
			if strings.TrimSpace(name) == "" {
				continue
			}
			abbr, ok := ResolveTeam(name)
			if !ok {
				bad = append(bad, name)
				continue
			}
			p.Offenses = append(p.Offenses, abbr)
		}
		// A team named but not recognised is a target you think is on and is
		// not — the same silent failure the strict parser exists to prevent —
		// so all unresolved names are reported at once rather than dropped.
		if len(bad) > 0 {
			return Preferences{}, fmt.Errorf(
				"offenses: unrecognised team(s): %s (use a city, nickname, full name, or abbreviation)",
				strings.Join(bad, ", "))
		}
	}
	return p, nil
}

// LoadPreferences reads the file at path. A missing file is not an error: it
// disables the feature, matching how LoadOverrides and LoadAliases degrade.
func LoadPreferences(path string) (Preferences, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("draft: opening preferences %s: %w", path, err)
	}
	defer f.Close()
	return ParsePreferences(f)
}

func normPos(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// RosterShape narrows a league shape to the positions you will actually start
// in the flex.
//
// Used where a roster of yours is assembled or scored, never where the pool is
// priced: the league's flex is what the league's flex is, and pricing against
// a narrower one would quietly move every number on the board. Returns the
// shape unchanged when no preference is set.
func (p Preferences) RosterShape(shape PoolState) PoolState {
	if len(p.FlexPositions) == 0 {
		return shape
	}
	// Only positions the league's own flex allows: a preference may narrow the
	// rule, never widen it past what the lineup would accept.
	allowed := map[string]bool{}
	for _, pos := range shape.FlexPositions {
		allowed[pos] = true
	}
	var out []string
	for _, pos := range p.FlexPositions {
		if allowed[pos] {
			out = append(out, pos)
		}
	}
	if len(out) == 0 {
		return shape
	}
	shape.FlexPositions = out
	return shape
}

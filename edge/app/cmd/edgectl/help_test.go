package main

import (
	"strings"
	"testing"

	"edge/internal/scenario"
)

// TestBasisHelpMatchesTheParser guards the surface a new reader trusts first.
//
// The -basis help said "total or margin" for as long as it took to add two more
// bases, and -outcome named two of four. A help string that duplicates a parser
// is a second source of truth, and this is what it looks like when it drifts.
// Both are now derived; this fails if anyone writes one out by hand again.
func TestBasisHelpMatchesTheParser(t *testing.T) {
	advertised := scenario.BasisNames()
	for _, b := range scenario.Bases() {
		if !strings.Contains(advertised, b.String()) {
			t.Errorf("basis %q is accepted but not advertised in %q", b, advertised)
		}
		got, err := scenario.ParseBasis(b.String())
		if err != nil {
			t.Errorf("basis %q is advertised but the parser rejects it: %v", b, err)
			continue
		}
		if got != b {
			t.Errorf("round trip: %q parsed to %q", b, got)
		}
	}
	// And nothing advertised should be unparseable.
	for _, name := range strings.Split(advertised, ", ") {
		if _, err := scenario.ParseBasis(strings.TrimSpace(name)); err != nil {
			t.Errorf("advertised basis %q does not parse: %v", name, err)
		}
	}
	if _, err := scenario.ParseBasis("no_such_basis"); err == nil {
		t.Error("an unknown basis was accepted")
	}
}

// TestOutcomeHelpMatchesTheArtifact checks the same for outcomes, which are
// read from the fitted grid rather than from a list in the CLI.
func TestOutcomeHelpMatchesTheArtifact(t *testing.T) {
	c, err := scenario.LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	advertised := outcomeNames()
	fitted := c.OutcomeNames()
	if len(fitted) == 0 {
		t.Fatal("the grid fits no outcomes")
	}
	for _, name := range fitted {
		if !strings.Contains(advertised, name) {
			t.Errorf("outcome %q is fitted but not advertised in %q", name, advertised)
		}
	}
	for _, name := range strings.Split(advertised, ", ") {
		found := false
		for _, f := range fitted {
			if f == strings.TrimSpace(name) {
				found = true
			}
		}
		if !found {
			t.Errorf("outcome %q is advertised but not fitted", name)
		}
	}
}

// TestBasisAliasesResolve checks the shorthands, which are the one thing the
// single table cannot make structurally safe: an alias could point at a
// canonical name that no longer exists.
func TestBasisAliasesResolve(t *testing.T) {
	for alias, canonical := range map[string]string{
		"t": "total", "m": "margin", "proe": "offense_proe", "success": "success_rate",
	} {
		got, err := scenario.ParseBasis(alias)
		if err != nil {
			t.Errorf("alias %q does not parse: %v", alias, err)
			continue
		}
		if got.String() != canonical {
			t.Errorf("alias %q resolved to %q, want %q", alias, got, canonical)
		}
	}
}

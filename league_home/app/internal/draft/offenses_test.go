package draft

import (
	"strings"
	"testing"
)

func TestParsePreferencesOffensesResolve(t *testing.T) {
	body := "offenses:\n  - Chicago Bears\n  - LAC\n  - Bengals\n  - New England\n"
	p, err := ParsePreferences(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParsePreferences: %v", err)
	}
	want := []string{"CHI", "LAC", "CIN", "NE"}
	if len(p.Offenses) != len(want) {
		t.Fatalf("offenses = %v, want %v", p.Offenses, want)
	}
	for i, a := range want {
		if p.Offenses[i] != a {
			t.Errorf("offenses[%d] = %q, want %q", i, p.Offenses[i], a)
		}
	}
	set := p.OffenseSet()
	if !set["CHI"] || set["GB"] {
		t.Errorf("OffenseSet wrong: %v", set)
	}
}

func TestParsePreferencesUnknownOffenseErrors(t *testing.T) {
	body := "offenses:\n  - Chicago Bears\n  - Gotham Rogues\n"
	_, err := ParsePreferences(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "Gotham Rogues") {
		t.Fatalf("expected an error naming the bad team, got %v", err)
	}
}

func TestBuildSignalsTagsTargetOffense(t *testing.T) {
	in := SignalInputs{
		Values: []PlayerValue{
			{PlayerID: "1", Name: "Bear Guy", Position: "WR", Price: 20},
			{PlayerID: "2", Name: "Packer Guy", Position: "WR", Price: 20},
		},
		Teams:          map[string]string{"1": "CHI", "2": "GB"},
		Offenses:       map[string]bool{"CHI": true},
		Leans:          Leans{},
		RecommendedBid: 50,
	}
	out := BuildSignals(in)
	byID := map[string]PlayerSignals{}
	for _, p := range out {
		byID[p.PlayerID] = p
	}
	if !byID["1"].Traits.Has(TraitOffense) {
		t.Errorf("Bear Guy (CHI) should carry the rich-offense trait")
	}
	if byID["2"].Traits.Has(TraitOffense) {
		t.Errorf("Packer Guy (GB) should not carry it — GB is not a target")
	}
}

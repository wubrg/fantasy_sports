package draft

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"leaguehome/internal/sleeper"
)

func TestParseOverrides(t *testing.T) {
	in := `season,player_id,price,reason
2025,4034,31,"LM ruling: kept at second-keep rate"
2025,9509,10,rookie stash
`
	got, err := ParseOverrides(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	ov, ok := got.Lookup("2025", "4034")
	if !ok || ov.Price != 31 {
		t.Fatalf("lookup = %+v, ok=%v", ov, ok)
	}
	if !strings.Contains(ov.Reason, "second-keep") {
		t.Errorf("reason = %q", ov.Reason)
	}
	if _, ok := got.Lookup("2024", "4034"); ok {
		t.Error("a 2025 ruling must not apply to 2024")
	}
}

// TestParseOverridesTolerantOfColumnOrder matters because this file is
// hand-maintained; requiring a fixed column order would make it fragile.
func TestParseOverridesTolerantOfColumnOrder(t *testing.T) {
	in := "reason,price,season,player_id\ntraded midseason,25,2025,777\n"
	got, err := ParseOverrides(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if ov, ok := got.Lookup("2025", "777"); !ok || ov.Price != 25 {
		t.Errorf("lookup = %+v, ok=%v", ov, ok)
	}
}

func TestParseOverridesRejectsBadRows(t *testing.T) {
	cases := map[string]string{
		"missing column": "season,player_id\n2025,4034\n",
		"bad price":      "season,player_id,price\n2025,4034,lots\n",
		"negative price": "season,player_id,price\n2025,4034,-5\n",
		"blank player":   "season,player_id,price\n2025,,10\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseOverrides(strings.NewReader(in)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestParseOverridesSkipsBlankLines(t *testing.T) {
	in := "season,player_id,price,reason\n\n2025,4034,31,ok\n\n"
	got, err := ParseOverrides(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 override, got %d", len(got))
	}
}

func TestLoadOverridesMissingFileIsNotAnError(t *testing.T) {
	got, err := LoadOverrides(filepath.Join(t.TempDir(), "nope.csv"))
	if err != nil {
		t.Fatalf("a missing rulings file should be fine, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected an empty override set, got %d", len(got))
	}
}

// TestOverrideReplacesPriceAndChainsForward is the behaviour that makes a
// single ruling worth recording: it fixes that year and every year after,
// because the next season chains off the ruled price.
func TestOverrideReplacesPriceAndChainsForward(t *testing.T) {
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p1", "10", false)}),
		season("2024", []sleeper.DraftPick{pick("alice", "p1", "15", true)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p1", "40", true)}),
	}
	overrides := Overrides{}
	overrides.Add(Override{Season: "2024", PlayerID: "p1", Price: 30, Reason: "LM ruling"})

	l, err := BuildLedgerWithOverrides(seasons, DefaultRules(), overrides)
	if err != nil {
		t.Fatal(err)
	}

	ruled := findEntry(t, l, "2024", "p1")
	if ruled.LeaguePrice != 30 {
		t.Errorf("2024 price = %d, want the ruled 30", ruled.LeaguePrice)
	}
	if !slices.Contains(ruled.Flags, FlagOverridden) {
		t.Errorf("expected %q flag, got %v", FlagOverridden, ruled.Flags)
	}

	// 2025 is a second keep off the ruled $30, so $30 + $10.
	next := findEntry(t, l, "2025", "p1")
	if next.PriorValue != 30 || next.LeaguePrice != 40 {
		t.Errorf("2025 prior=%d price=%d, want 30/40 — the ruling must chain forward",
			next.PriorValue, next.LeaguePrice)
	}
	if slices.Contains(next.Flags, FlagOverridden) {
		t.Error("2025 was computed, not ruled, and should not be flagged as an override")
	}
}

func TestBuildLedgerWithoutOverridesIsUnaffected(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p1", "10", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p1", "15", true)}),
	}
	l, err := BuildLedgerWithOverrides(seasons, DefaultRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2025", "p1"); e.LeaguePrice != 15 {
		t.Errorf("price = %d, want 15", e.LeaguePrice)
	}
}

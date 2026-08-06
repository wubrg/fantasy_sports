package draft

import (
	"slices"
	"strings"
	"testing"

	"leaguehome/internal/sleeper"
)

// tradeOf moves a player to bob's roster.
func tradeOf(playerID string) sleeper.Transaction {
	return sleeper.Transaction{
		Type: "trade", Status: "complete", Created: 100,
		Adds: map[string]int{playerID: 2}, RosterIDs: []int{1, 2},
	}
}

// TestTradeResetsEscalationFrom2026 is the league's current rule: a trade
// restarts the ladder rather than preserving it. The cost basis still
// follows the player, so the restart begins from what was last paid.
func TestTradeResetsEscalationFrom2026(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p1", "20", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p1", "25", true)}, tradeOf("p1")),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}

	// Without the reset this would be a second keep: $25 + $10 = $35.
	entries, err := l.PriceDeclared("2026", []Declared{{OwnerID: "bob", PlayerID: "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	if e.KeepCount != 1 || e.PriorValue != 25 || e.LeaguePrice != 30 {
		t.Errorf("keepCount=%d prior=%d price=%d, want 1/25/30 — the trade restarts the ladder from the carried basis",
			e.KeepCount, e.PriorValue, e.LeaguePrice)
	}
	if !slices.Contains(e.Flags, FlagEscalationResetByTrade) {
		t.Errorf("expected %q flag, got %v", FlagEscalationResetByTrade, e.Flags)
	}
}

// TestTradeRuleIsNotRetroactive protects the historical record: prior
// seasons were charged under the old rule and must not be recomputed.
func TestTradeRuleIsNotRetroactive(t *testing.T) {
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p2", "10", false)}),
		season("2024", []sleeper.DraftPick{pick("alice", "p2", "15", true)}, tradeOf("p2")),
		season("2025", []sleeper.DraftPick{pick("bob", "p2", "25", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	// 2025 is a second keep under the pre-2026 rule: $15 + $10.
	e := findEntry(t, l, "2025", "p2")
	if e.KeepCount != 2 || e.LeaguePrice != 25 {
		t.Errorf("keepCount=%d price=%d, want 2/25 — the 2026 rule must not reach backwards",
			e.KeepCount, e.LeaguePrice)
	}
	if slices.Contains(e.Flags, FlagEscalationResetByTrade) {
		t.Errorf("2025 predates the rule and should not be flagged: %v", e.Flags)
	}
}

// TestNoTradeMeansNoReset guards against the reset firing on an ordinary
// keep by the same manager.
func TestNoTradeMeansNoReset(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p3", "20", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p3", "25", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := l.PriceDeclared("2026", []Declared{{OwnerID: "alice", PlayerID: "p3"}})
	if err != nil {
		t.Fatal(err)
	}
	if e := entries[0]; e.KeepCount != 2 || e.LeaguePrice != 35 {
		t.Errorf("keepCount=%d price=%d, want 2/35", e.KeepCount, e.LeaguePrice)
	}
}

// TestExpansionDraftOverrideSetsLadderPosition covers Jaxon Smith-Njigba's
// case: a player taken in a new-owner or expansion draft has his keeper
// value assigned at selection time, and nothing in Sleeper records it. The
// ruling has to pin both the price and the ladder position so the following
// year escalates from the right rung.
func TestExpansionDraftOverrideSetsLadderPosition(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p4", "40", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p4", "45", true)}),
	}
	overrides := Overrides{}
	overrides.Add(Override{
		Season: "2026", PlayerID: "p4", Price: 10, KeepCount: 1,
		Reason: "selected in the new-owner expansion draft at the assigned value",
	})

	l, err := BuildLedgerWithOverrides(seasons, DefaultRules(), overrides)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := l.PriceDeclared("2026", []Declared{{OwnerID: "bob", PlayerID: "p4"}})
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	if e.LeaguePrice != 10 {
		t.Errorf("price = %d, want the ruled 10", e.LeaguePrice)
	}
	if e.KeepCount != 1 {
		t.Errorf("keepCount = %d, want the ruled 1", e.KeepCount)
	}
	if !slices.Contains(e.Flags, FlagOverridden) {
		t.Errorf("expected %q flag, got %v", FlagOverridden, e.Flags)
	}
}

func TestParseOverridesKeepCount(t *testing.T) {
	in := "season,player_id,price,keep_count,reason\n2026,9488,10,1,expansion draft\n2026,777,25,,ordinary ruling\n"
	got, err := ParseOverrides(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if ov, _ := got.Lookup("2026", "9488"); ov.KeepCount != 1 {
		t.Errorf("keep_count = %d, want 1", ov.KeepCount)
	}
	// Blank means "don't pin it", not zero-as-a-value.
	if ov, _ := got.Lookup("2026", "777"); ov.KeepCount != 0 {
		t.Errorf("blank keep_count = %d, want 0", ov.KeepCount)
	}
}

func TestParseOverridesRejectsBadKeepCount(t *testing.T) {
	for _, in := range []string{
		"season,player_id,price,keep_count\n2026,9488,10,zero\n",
		"season,player_id,price,keep_count\n2026,9488,10,0\n",
		"season,player_id,price,keep_count\n2026,9488,10,-2\n",
	} {
		if _, err := ParseOverrides(strings.NewReader(in)); err == nil {
			t.Errorf("expected an error for %q", in)
		}
	}
}

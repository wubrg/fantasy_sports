package draft

import (
	"strings"
	"testing"

	"leaguehome/internal/sleeper"
)

func TestReconcileSeparatesMatchesFromMismatches(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p1", "10", false)}),
		season("2025", []sleeper.DraftPick{
			pick("alice", "p1", "15", true), // agrees with the ladder
			pick("bob", "p2", "99", true),   // no prior record, disagrees
		}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	r := Reconcile(seasons, l, 12, 200)

	if r.Total != 2 || r.Matched != 1 {
		t.Errorf("matched %d of %d, want 1 of 2", r.Matched, r.Total)
	}
	if len(r.Mismatches) != 1 || r.Mismatches[0].PlayerID != "p2" {
		t.Errorf("unexpected mismatches: %+v", r.Mismatches)
	}
}

// TestReconcileExcludesCorruptSeasons guards the 2022 case: comparing
// computed prices against records known to be wrong measures nothing, so
// those entries must not drag down the agreement rate.
func TestReconcileExcludesCorruptSeasons(t *testing.T) {
	bad := season("2022", []sleeper.DraftPick{pick("alice", "p1", "1", true)})
	bad.Corrupt = true
	bad.CorruptReason = "all keepers recorded at $1"

	seasons := []SeasonData{
		season("2021", []sleeper.DraftPick{pick("alice", "p1", "30", false)}),
		bad,
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	r := Reconcile(seasons, l, 12, 200)

	if r.Total != 0 {
		t.Errorf("corrupt season entries should not be scored, got Total=%d", r.Total)
	}
	if len(r.Mismatches) != 0 {
		t.Errorf("corrupt season should produce no rulings, got %+v", r.Mismatches)
	}
	if len(r.Seasons) != 2 || !r.Seasons[1].Corrupt {
		t.Fatalf("expected 2022 summary marked corrupt, got %+v", r.Seasons)
	}
}

// TestReconcileSortsMismatchesByMagnitude checks that the biggest dollar
// disagreements surface first, and that a large overcharge outranks a small
// undercharge — ordering is by magnitude, not sign.
func TestReconcileSortsMismatchesByMagnitude(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{
			pick("alice", "small", "5", false),
			pick("alice", "big", "5", false),
			pick("bob", "negative", "5", false),
		}),
		season("2025", []sleeper.DraftPick{
			// Ladder says $10 for each; Sleeper's recorded amounts differ
			// by +2, -50, and +30 respectively.
			pick("alice", "small", "8", true),
			pick("alice", "big", "60", true),
			pick("bob", "negative", "-20", true),
		}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	r := Reconcile(seasons, l, 12, 200)
	if len(r.Mismatches) != 3 {
		t.Fatalf("expected 3 mismatches, got %d: %+v", len(r.Mismatches), r.Mismatches)
	}
	if got := r.Mismatches[0].PlayerID; got != "big" {
		t.Errorf("largest variance should sort first, got %q", got)
	}
	if got := r.Mismatches[2].PlayerID; got != "small" {
		t.Errorf("smallest variance should sort last, got %q", got)
	}
}

func TestSeasonSummaryPool(t *testing.T) {
	s := SeasonSummary{LeagueSpend: 507}
	if got := s.Pool(12, 200); got != 1893 {
		t.Errorf("Pool = %d, want 1893", got)
	}
}

func TestNamesFallBackToOwnerID(t *testing.T) {
	n := Names{"u1": "Bijan Mustardson", "u2": ""}
	if got := n.Of("u1"); got != "Bijan Mustardson" {
		t.Errorf("of(u1) = %q", got)
	}
	// An empty or missing name must not render as a blank column.
	if got := n.Of("u2"); got != "u2" {
		t.Errorf("of(u2) = %q, want the raw ID", got)
	}
	if got := n.Of("unknown"); got != "unknown" {
		t.Errorf("of(unknown) = %q, want the raw ID", got)
	}
}

func TestWriteTextIncludesCorruptWarningAndRulings(t *testing.T) {
	bad := season("2022", []sleeper.DraftPick{pick("alice", "p1", "1", true)})
	bad.Corrupt = true
	bad.CorruptReason = "all keepers recorded at $1"

	seasons := []SeasonData{
		season("2021", []sleeper.DraftPick{pick("alice", "p1", "30", false)}),
		bad,
		season("2023", []sleeper.DraftPick{pick("alice", "p9", "99", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	if err := Reconcile(seasons, l, 12, 200).WriteText(&sb, Names{"alice": "Team Alice"}, 12, 200); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"EXCLUDED", "all keepers recorded at $1", "NEEDS AN LM RULING", "Team Alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n%s", want, out)
		}
	}
}

func TestWriteBudgetsShowsPerOwnerRemaining(t *testing.T) {
	entries := []Entry{
		{OwnerID: "u1", Name: "Saquon Barkley", LeaguePrice: 56},
		{OwnerID: "u1", Name: "Jayden Daniels", LeaguePrice: 16},
		{OwnerID: "u2", Name: "Courtland Sutton", LeaguePrice: 10},
	}
	var sb strings.Builder
	if err := WriteBudgets(&sb, entries, Names{"u1": "Team One", "u2": "Team Two"}, 200); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, "128") {
		t.Errorf("expected Team One to have $128 left:\n%s", out)
	}
	if !strings.Contains(out, "190") {
		t.Errorf("expected Team Two to have $190 left:\n%s", out)
	}
}

package draft

import "testing"

func TestChrisdellLeans(t *testing.T) {
	rows := []map[string]string{
		{"player": "Big Up", "position": "WR", "pos_rank": "50", "ecr": "80", "vs_ecr": "30"},
		{"player": "Big Down", "position": "TE", "pos_rank": "30", "ecr": "10", "vs_ecr": "-20"},
		{"player": "Marginal", "position": "RB", "pos_rank": "20", "ecr": "25", "vs_ecr": "5"},
		{"player": "", "vs_ecr": "40"},
	}
	leans, err := chrisdellLeans(rows)
	if err != nil {
		t.Fatalf("chrisdellLeans: %v", err)
	}
	by := map[string]PlayerLean{}
	for _, l := range leans {
		by[l.Player] = l
	}
	if len(leans) != 2 {
		t.Fatalf("got %d reads, want 2: %+v", len(leans), leans)
	}
	if by["Big Up"].Lean != LeanUp {
		t.Errorf("Big Up (+30) should lean up, got %q", by["Big Up"].Lean)
	}
	if by["Big Down"].Lean != LeanDown {
		t.Errorf("Big Down (-20) should lean down, got %q", by["Big Down"].Lean)
	}
	if _, ok := by["Marginal"]; ok {
		t.Error("Marginal (+5, under threshold) should produce no read")
	}
	if by["Big Up"].Source != "chrisdell" {
		t.Errorf("source = %q, want chrisdell", by["Big Up"].Source)
	}
}

func TestDellSharpFlag(t *testing.T) {
	cases := []struct {
		delta int
		want  SharpState
	}{
		{dellSharpThreshold, SharpUp},
		{dellSharpThreshold + 5, SharpUp},
		{-dellSharpThreshold, SharpDown},
		{dellSharpThreshold - 1, SharpNone},
		{0, SharpNone},
	}
	for _, c := range cases {
		if got := (PlayerSignals{DellDelta: c.delta}).DellSharp(); got != c.want {
			t.Errorf("DellSharp(%d) = %v, want %v", c.delta, got, c.want)
		}
	}
}

package draft

import "testing"

// TestCielySharpFlag — the threshold boundary, mirroring TestDellSharpFlag.
//
// Off-by-one here is invisible on a board: the flag simply appears on a few
// more or a few fewer players and nothing looks wrong.
func TestCielySharpFlag(t *testing.T) {
	cases := []struct {
		delta int
		want  SharpState
	}{
		{cielySharpThreshold, SharpUp},
		{cielySharpThreshold + 5, SharpUp},
		{-cielySharpThreshold, SharpDown},
		{-cielySharpThreshold - 5, SharpDown},
		{cielySharpThreshold - 1, SharpNone},
		{-cielySharpThreshold + 1, SharpNone},
		{0, SharpNone},
	}
	for _, c := range cases {
		if got := (PlayerSignals{CielyDelta: c.delta}).CielySharp(); got != c.want {
			t.Errorf("CielySharp(%d) = %v, want %v", c.delta, got, c.want)
		}
	}
}

// The two flags answer to separate thresholds on purpose, so one may be tuned
// without moving the other. A single shared constant would make any change to
// Ciely's sensitivity a silent change to Chris Dell's.
func TestCielyAndDellReadTheirOwnDeltas(t *testing.T) {
	p := PlayerSignals{CielyDelta: cielySharpThreshold, DellDelta: 0}
	if p.CielySharp() != SharpUp {
		t.Error("CielySharp did not read CielyDelta")
	}
	if p.DellSharp() != SharpNone {
		t.Error("DellSharp fired on Ciely's delta")
	}

	q := PlayerSignals{CielyDelta: 0, DellDelta: dellSharpThreshold}
	if q.DellSharp() != SharpUp {
		t.Error("DellSharp did not read DellDelta")
	}
	if q.CielySharp() != SharpNone {
		t.Error("CielySharp fired on Dell's delta")
	}
}

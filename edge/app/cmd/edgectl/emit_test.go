package main

import (
	"os"
	"strings"
	"testing"
)

// TestMarketBlockHeaderMatchesSource is the load-bearing assertion: the header
// and rule rows `emit market` prints must be byte-for-byte what the operative
// template documents, because a downstream consumer pattern-matches on them.
// It reads the header out of docs/frameworks/urps-wager-engine.md rather than
// trusting a copy, so the two cannot drift apart silently.
func TestMarketBlockHeaderMatchesSource(t *testing.T) {
	src, err := os.ReadFile("../../../docs/frameworks/urps-wager-engine.md")
	if err != nil {
		t.Fatalf("cannot read the source template: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, marketBlockHeader) {
		t.Errorf("emit's header is not present verbatim in urps-wager-engine.md:\n  emit: %q", marketBlockHeader)
	}
	if !strings.Contains(text, marketBlockRule) {
		t.Errorf("emit's rule row is not present verbatim in urps-wager-engine.md:\n  emit: %q", marketBlockRule)
	}
}

func TestRenderMarketBlock(t *testing.T) {
	fair := 0.5514
	hold := 0.0401
	rows := []marketRow{
		{
			ID: "pit-abc123", Game: "PIT @ CLE", Market: "moneyline", Selection: "PIT",
			Book: "consensus", American: -135, ImpliedRaw: 0.5745,
			FairDevig: &fair, Breakeven: 0.5745, Hold: &hold,
		},
		{
			ID: "jaylen-warren-eaaf95", Game: "PIT Steelers @ CLE Browns", Market: "First TD Scorer",
			Selection: "Jaylen Warren", Book: "draftkings", American: 450,
			ImpliedRaw: 0.1818, FairDevig: nil, Breakeven: 0.1818, Hold: nil,
		},
	}
	out := renderMarketBlock(rows, "2026-09-30 10:37")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if lines[0] != "## MARKET  (as of 2026-09-30 10:37, operator-supplied)" {
		t.Errorf("heading = %q", lines[0])
	}
	if lines[1] != marketBlockHeader {
		t.Errorf("header row = %q", lines[1])
	}
	if lines[2] != marketBlockRule {
		t.Errorf("rule row = %q", lines[2])
	}
	// The two-sided game line carries a numeric fair_devig and hold.
	wantGame := "| pit-abc123 | PIT @ CLE | moneyline | PIT | consensus | -135 | 0.5745 | 0.5514 | 0.5745 | 0.0401 |"
	if lines[3] != wantGame {
		t.Errorf("game-line row =\n  %q\nwant\n  %q", lines[3], wantGame)
	}
	// The one-sided prop shows an em dash for fair_devig and hold, not a zero.
	wantProp := "| jaylen-warren-eaaf95 | PIT Steelers @ CLE Browns | First TD Scorer | Jaylen Warren | draftkings | +450 | 0.1818 | — | 0.1818 | — |"
	if lines[4] != wantProp {
		t.Errorf("prop row =\n  %q\nwant\n  %q", lines[4], wantProp)
	}
}

func TestRenderProvenance(t *testing.T) {
	out := renderProvenance("2026-09-30 10:37", 765, 763)
	for _, want := range []string{
		"DATA PROVENANCE",
		"  Market block timestamp : 2026-09-30 10:37",
		"  Markets supplied       : 765",
		"  Props with projections : 0 of 763",
		"  Computation            : all EV/probability figures from edgectl; none derived in-model",
		"CALIBRATION WARNING",
		"  swings EV by 12.5 points -- larger than most edges this process detects.",
		"  These are not forecasts.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("provenance block missing line:\n  %q\ngot:\n%s", want, out)
		}
	}
}

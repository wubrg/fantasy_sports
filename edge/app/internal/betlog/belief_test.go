package betlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/wager"
)

func kickoff() time.Time { return time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC) }

func samplePrediction() Prediction {
	return Prediction{
		Season: 2026, Week: 1,
		GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "efficient_offense",
		Belief: 0.44, Confidence: 0.7,
		Source: "prompt", Model: "test-model",
		Kickoff:     kickoff(),
		GeneratedAt: kickoff().Add(-3 * time.Hour),
	}
}

func beliefPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "beliefs.jsonl")
}

func TestPredictionRoundTrip(t *testing.T) {
	path := beliefPath(t)
	p := samplePrediction()
	base := 0.3243
	p.SBaseRate = &base

	id, err := Record(path, p, kickoff().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadPredictions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d predictions, want 1", len(got))
	}
	if got[0].ID != id {
		t.Errorf("id %q, want %q", got[0].ID, id)
	}
	if got[0].Prediction.Belief != 0.44 {
		t.Errorf("belief %v, want 0.44", got[0].Prediction.Belief)
	}
	if got[0].Result != Open {
		t.Errorf("a fresh prediction is %q, want open", got[0].Result)
	}
	// A frozen reference must survive the round trip, and must stay
	// distinguishable from absent.
	if got[0].Prediction.SBaseRate == nil || *got[0].Prediction.SBaseRate != base {
		t.Errorf("frozen base rate did not survive: %v", got[0].Prediction.SBaseRate)
	}
	if got[0].Prediction.SIncumbent != nil {
		t.Errorf("an absent reference decoded as %v, want nil — before week 4 there is "+
			"no incumbent, and that must not read as a probability of zero",
			*got[0].Prediction.SIncumbent)
	}
}

// TestBeliefSettlementIsAppendedNotRewritten mirrors the wager-side guarantee:
// the original forecast must survive settlement byte for byte, or the log
// cannot show what was believed beforehand.
func TestBeliefSettlementIsAppendedNotRewritten(t *testing.T) {
	path := beliefPath(t)
	id, err := Record(path, samplePrediction(), kickoff().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SettleBelief(path, id, true, "total 54"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("settlement rewrote the original prediction instead of appending after it")
	}
	got, err := LoadPredictions(path)
	if err != nil {
		t.Fatal(err)
	}
	happened, known := got[0].Occurred()
	if !known || !happened {
		t.Errorf("settled as occurred, read back happened=%v known=%v", happened, known)
	}
}

// TestBeliefCannotBeResettled is the property the whole forward-only test rests
// on, and it must hold for beliefs exactly as it does for wagers.
func TestBeliefCannotBeResettled(t *testing.T) {
	path := beliefPath(t)
	id, err := Record(path, samplePrediction(), kickoff().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := SettleBelief(path, id, false, "first"); err != nil {
		t.Fatal(err)
	}
	// Appending a second settlement is possible — the file is append-only —
	// but the log must then refuse to load rather than take the last write.
	if err := SettleBelief(path, id, true, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPredictions(path); err == nil {
		t.Fatal("a second settlement was accepted; hindsight is what this log exists to prevent")
	} else if !strings.Contains(err.Error(), "already settled") {
		t.Errorf("error should name the double settlement, got: %v", err)
	}
}

// TestKindsAreIsolated: pointing either reader at the other's log must fail
// loudly. Silently returning a number computed over the wrong population is the
// dangerous outcome — a belief has no stake, so folding one into ROI would
// reintroduce exactly the stake-less-row bug PlaceBet's validation prevents.
func TestKindsAreIsolated(t *testing.T) {
	beliefs := beliefPath(t)
	if _, err := Record(beliefs, samplePrediction(), kickoff().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(beliefs); err == nil {
		t.Error("Load accepted a belief log; it must refuse")
	} else if !strings.Contains(err.Error(), "beliefs score") {
		t.Errorf("the refusal should name the right command, got: %v", err)
	}

	bets := filepath.Join(t.TempDir(), "bets.jsonl")
	if _, err := PlaceBet(bets, Bet{
		Selection: "x", Price: wager.American(-110), Bankroll: wager.RealMoney.String(),
		Stake: 1, Predicted: 0.5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPredictions(bets); err == nil {
		t.Error("LoadPredictions accepted a bet log; it must refuse")
	} else if !strings.Contains(err.Error(), "log score") {
		t.Errorf("the refusal should name the right command, got: %v", err)
	}
}

// TestKickoffGateUsesBothClocks. Either clock alone leaves a hole: a forecast
// honestly timestamped but ingested late passes the first, and a backdated one
// ingested on time passes the second.
func TestKickoffGateUsesBothClocks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		generatedAt time.Time
		now         time.Time
		wantErr     string
	}{
		{"both before kickoff", kickoff().Add(-3 * time.Hour), kickoff().Add(-time.Hour), ""},
		{"generated after kickoff", kickoff().Add(time.Minute), kickoff().Add(-time.Hour), "not before kickoff"},
		{"ingested after kickoff", kickoff().Add(-3 * time.Hour), kickoff().Add(time.Minute), "after its kickoff"},
		{"exactly at kickoff is late", kickoff(), kickoff().Add(-time.Hour), "not before kickoff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := samplePrediction()
			p.GeneratedAt = tc.generatedAt
			_, err := Record(beliefPath(t), p, tc.now)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected refusal: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("expected a refusal mentioning %q, got none", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error %v does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestVoidIsNotTheSameAsDidNotHappen. Counting an unmeasurable game as "did not
// occur" would bias every base rate downward, and the games it happens to —
// weather, short blowouts — are not a random sample.
func TestVoidIsNotTheSameAsDidNotHappen(t *testing.T) {
	path := beliefPath(t)
	id, err := Record(path, samplePrediction(), kickoff().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := VoidBelief(path, id, "only 14 plays carried xpass"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPredictions(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := got[0].Occurred(); known {
		t.Error("a voided belief reported a known outcome")
	}
	if got[0].Result.Counts() {
		t.Error("a voided belief counts toward scoring; it must be excluded")
	}
	if got[0].Note == "" {
		t.Error("the void has no reason recorded")
	}
	if err := VoidBelief(path, id, ""); err == nil {
		t.Error("voiding without a reason was accepted")
	}
}

func TestPredictionValidateRejectsUnscorable(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		mutate     func(*Prediction)
	}{
		{"belief above one", "not a probability", func(p *Prediction) { p.Belief = 1.4 }},
		{"belief negative", "not a probability", func(p *Prediction) { p.Belief = -0.1 }},
		{"no scenario", "no scenario", func(p *Prediction) { p.Scenario = "" }},
		{"no game", "no game id", func(p *Prediction) { p.GameID = "" }},
		{"no kickoff", "no kickoff", func(p *Prediction) { p.Kickoff = time.Time{} }},
		{"confidence out of range", "confidence", func(p *Prediction) { p.Confidence = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := samplePrediction()
			tc.mutate(&p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v does not mention %q", err, tc.want)
			}
		})
	}
}

// TestUnknownKindIsSkippedNotFatal keeps a log written by a newer binary
// readable by an older one. Adding a third event type should not be a
// migration — but the skip is counted and warned about, because silence would
// hide real corruption.
func TestUnknownKindIsSkippedNotFatal(t *testing.T) {
	path := beliefPath(t)
	if _, err := Record(path, samplePrediction(), kickoff().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"kind":"hedge","id":"x","time":"2026-09-13T12:00:00Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := LoadPredictions(path)
	if err != nil {
		t.Fatalf("an unknown kind made the log unreadable: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d predictions, want 1", len(got))
	}
}

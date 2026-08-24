package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/scenario"
)

// The belief harness is built around refusals, so almost every test here is a
// test that something is rejected. A gate that fails open is worse than no gate:
// it produces a number that looks like evidence.

func writeTestJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func f64(v float64) *float64 { return &v }

func gridDefinitions() map[string]scenario.Definition {
	c, err := scenario.LoadConditionals()
	if err != nil {
		panic(err)
	}
	return c.Definitions
}

// testPack is a two-game week whose kickoffs are in the future, so the gate
// under test is the one being exercised rather than the clock.
func testPack(kick time.Time) inputPack {
	return inputPack{
		Season: 2026, Week: 1,
		// The real definitions, read from the embedded grid — a hand-written copy
		// here would be the second definition this whole design avoids.
		Definitions: gridDefinitions(),
		BaseRates: map[string]float64{
			"shootout": 0.3378, "blowout_loss": 0.2617,
			"pass_heavy": 0.3353, "efficient_offense": 0.3243,
		},
		Games: []packGame{{
			GameID: "2026_01_DEN_KC", Away: "DEN", Home: "KC",
			Kickoff: kick, TotalLine: f64(47.5), SpreadLine: f64(3.5),
			Teams: map[string]packTeam{"DEN": {}, "KC": {}},
		}},
	}
}

func ingestFixture(t *testing.T, preds []forecast, generatedAt, kick time.Time) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	packPath := writeTestJSON(t, dir, "week01.input.json", testPack(kick))
	sum, err := fileSHA(packPath)
	if err != nil {
		t.Fatal(err)
	}
	ff := forecastFile{
		Season: 2026, Week: 1,
		InputPack: packPath, InputPackSHA: sum,
		Model: "test", Prompt: "belief-v1", GeneratedAt: generatedAt,
		Predictions: preds,
	}
	filePath := writeTestJSON(t, dir, "week01.predictions.json", ff)
	return packPath, filePath, filepath.Join(dir, "log.jsonl")
}

func mustFail(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a refusal mentioning %q, got none", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %v does not mention %q", err, want)
	}
}

func TestIngestAcceptsACleanFile(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.44, Confidence: 0.6},
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "efficient_offense", Belief: 0.5},
	}, time.Now(), kick)

	if err := beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}); err != nil {
		t.Fatal(err)
	}
	if err := beliefsList([]string{"-log", log}); err != nil {
		t.Fatal(err)
	}
}

// The hash is what binds a forecast to the facts it saw. Paths are mutable.
func TestIngestRefusesAWrongPackHash(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.44},
	}, time.Now(), kick)

	// Change the pack after the forecast was written against it.
	b, _ := os.ReadFile(pack)
	var pk inputPack
	if err := json.Unmarshal(b, &pk); err != nil {
		t.Fatal(err)
	}
	pk.Games[0].TotalLine = f64(60)
	writeTestJSON(t, filepath.Dir(pack), filepath.Base(pack), pk)

	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}),
		"different facts")
}

// A forecaster that volunteers a price or a player has drifted off contract.
// Ignoring the field would hide the drift, which is the failure mode this whole
// exercise exists to measure.
func TestIngestRefusesAnUnknownField(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.44},
	}, time.Now(), kick)

	raw, _ := os.ReadFile(file)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	ps := m["predictions"].([]any)
	ps[0].(map[string]any)["price"] = -110
	writeTestJSON(t, filepath.Dir(file), filepath.Base(file), m)

	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}), "price")
}

func TestIngestRefusesAWrongUnitOrTeam(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)

	// shootout is a property of the GAME; carrying a team means the forecaster
	// misunderstood what it was forecasting.
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "shootout", Belief: 0.44},
	}, time.Now(), kick)
	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}),
		"property of the game")

	// A team that is not in the game at all.
	pack, file, log = ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Team: "SF", Scenario: "pass_heavy", Belief: 0.4},
	}, time.Now(), kick)
	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}),
		"not a side of")
}

// A second opinion on the same claim is exactly the hindsight the log exists to
// prevent — news breaks, and the forecast quietly improves.
func TestIngestRefusesADuplicateClaim(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "pass_heavy", Belief: 0.40},
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "pass_heavy", Belief: 0.55},
	}, time.Now(), kick)
	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}), "twice")
}

// Both clocks. A file honestly written but ingested late fails the wall clock;
// a backdated one ingested on time fails its own timestamp.
func TestIngestRefusesLatePredictions(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.44},
	}, time.Now().Add(-3*time.Hour), past)

	err := beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log})
	mustFail(t, err, "after kickoff")

	// -drop-late keeps the rest and records the loss rather than pretending it
	// did not happen.
	if err := beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log, "-drop-late"}); err != nil {
		t.Fatalf("-drop-late should salvage the file: %v", err)
	}
}

func TestIngestRefusesABeliefOutOfRange(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 1.4},
	}, time.Now(), kick)
	mustFail(t, beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}),
		"not a probability")
}

// The market's own view must be frozen onto the record, and the spread sign has
// to come out right for both sides. This is the code path that already shipped
// one sign bug in this repository.
func TestIngestFreezesReferencesWithTheRightSpreadSign(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	// spread_line +3.5 means the HOME side (KC) is favoured by 3.5.
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "blowout_loss", Belief: 0.15},
		{GameID: "2026_01_DEN_KC", Team: "DEN", Scenario: "blowout_loss", Belief: 0.35},
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.40},
	}, time.Now(), kick)
	if err := beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}); err != nil {
		t.Fatal(err)
	}
	preds, err := betlog.LoadPredictions(log)
	if err != nil {
		t.Fatal(err)
	}
	var kc, den, shoot *float64
	for _, sp := range preds {
		switch {
		case sp.Prediction.Scenario == "shootout":
			shoot = sp.Prediction.SMarket
		case sp.Prediction.Team == "KC":
			kc = sp.Prediction.SMarket
		case sp.Prediction.Team == "DEN":
			den = sp.Prediction.SMarket
		}
	}
	if kc == nil || den == nil || shoot == nil {
		t.Fatalf("a market reference was not frozen: kc=%v den=%v shootout=%v", kc, den, shoot)
	}
	// The favourite must be LESS likely to lose by more than a touchdown.
	if !(*kc < *den) {
		t.Errorf("KC is favoured by 3.5 yet its P(blowout loss) %.3f is not below DEN's %.3f — "+
			"the spread sign is inverted", *kc, *den)
	}
	if *shoot <= 0 || *shoot >= 1 {
		t.Errorf("shootout market probability %.3f is not a probability", *shoot)
	}
	// Base rates travel too, so a refit cannot move them retroactively.
	for _, sp := range preds {
		if sp.Prediction.SBaseRate == nil {
			t.Errorf("%s carries no frozen base rate", sp.Prediction.Key())
		}
	}
}

// TestSettleIsIdempotent is the guard that keeps a careless re-run from
// bricking the log. Without it a second pass appends a duplicate settlement,
// and LoadPredictions then refuses the whole file for good — the no-resettle
// rule turning into data loss.
func TestSettleIsIdempotent(t *testing.T) {
	kick := time.Now().Add(48 * time.Hour)
	pack, file, log := ingestFixture(t, []forecast{
		{GameID: "2026_01_DEN_KC", Scenario: "shootout", Belief: 0.44},
		{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "efficient_offense", Belief: 0.52},
		{GameID: "2026_01_DEN_KC", Team: "DEN", Scenario: "pass_heavy", Belief: 0.61},
	}, time.Now(), kick)
	if err := beliefsIngest([]string{"-file", file, "-pack", pack, "-log", log}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(file)
	outPath := writeTestJSON(t, dir, "week01.outcomes.json", outcomePack{
		Season: 2026, Week: 1, Definitions: gridDefinitions(),
		Rows: []outcomeRow{
			{GameID: "2026_01_DEN_KC", Scenario: "shootout", Status: "settled", Occurred: true},
			{GameID: "2026_01_DEN_KC", Team: "KC", Scenario: "efficient_offense",
				Status: "settled", Occurred: false},
			// Unmeasurable is not the same as did-not-happen.
			{GameID: "2026_01_DEN_KC", Team: "DEN", Scenario: "pass_heavy",
				Status: "unavailable", Reason: "too few plays carrying xpass"},
		},
	})

	if err := beliefsSettle([]string{"-outcomes", outPath, "-log", log}); err != nil {
		t.Fatal(err)
	}
	preds, err := betlog.LoadPredictions(log)
	if err != nil {
		t.Fatal(err)
	}
	var settled, voided int
	for _, sp := range preds {
		if _, known := sp.Occurred(); known {
			settled++
		}
		if sp.Result == betlog.Void {
			voided++
		}
	}
	if settled != 2 || voided != 1 {
		t.Errorf("settled=%d voided=%d, want 2 and 1", settled, voided)
	}

	// Second run: nothing more may be written, and the log must still load.
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := beliefsSettle([]string{"-outcomes", outPath, "-log", log}); err != nil {
		t.Fatalf("a second settle run errored: %v", err)
	}
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("the second run appended %d bytes; settlement is not idempotent",
			len(after)-len(before))
	}
	if _, err := betlog.LoadPredictions(log); err != nil {
		t.Errorf("the log stopped loading after a second settle: %v", err)
	}
}

// A pack built against different thresholds is measuring something else, and
// must be refused rather than silently substituted.
func TestSettleRefusesAMovedThreshold(t *testing.T) {
	dir := t.TempDir()
	defs := map[string]scenario.Definition{}
	for k, v := range gridDefinitions() {
		defs[k] = v
	}
	moved := defs["shootout"]
	moved.Threshold = 44
	defs["shootout"] = moved

	outPath := writeTestJSON(t, dir, "bad.outcomes.json", outcomePack{
		Season: 2026, Week: 1, Definitions: defs,
	})
	mustFail(t, beliefsSettle([]string{"-outcomes", outPath,
		"-log", filepath.Join(dir, "log.jsonl")}), "different definition")
}

// TestScoreReadsOutTheWholeMeasurement drives score end to end on a synthetic
// week whose forecaster has a real, known edge. It checks the command runs and
// exists mainly so the output is exercised — the numbers it prints are the
// thing a person will act on, and an unexercised report is one that breaks
// silently the first week it matters.
func TestScoreReadsOutTheWholeMeasurement(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log.jsonl")
	kick := time.Now().Add(48 * time.Hour)

	// 60 games. The forecaster leans the right way on a third of them and
	// abstains on the rest, which is the shape the contract actually produces.
	var rows []outcomeRow
	for i := 0; i < 60; i++ {
		gid := "2026_01_G" + string(rune('A'+i/26)) + string(rune('A'+i%26))
		// A forecaster with genuine but imperfect resolution: it spreads its
		// calls, is right more often than not on the confident ones, and
		// abstains on two thirds of the slate. A single repeated belief would
		// exercise none of the report -- Brier is exactly 0.25 at p=0.5 whatever
		// happens, so the interval collapses and the slope has nothing to fit.
		informed := i%3 == 0
		belief, abstain := 0.3243, true
		happened := i%5 == 0
		if informed {
			abstain = false
			switch i % 9 {
			case 0:
				belief, happened = 0.62, true
			case 3:
				belief, happened = 0.55, true
			default:
				belief, happened = 0.28, false
			}
		}
		// Alternate scenario, confidence and flag so the splits have something
		// to split on.
		scen := "efficient_offense"
		if i%2 == 1 {
			scen = "shootout"
		}
		conf := 0.2
		if informed {
			conf = 0.8
		}
		p := betlog.Prediction{
			// All in week 1: the outcome pack is one week, and settle filters by
			// week. Varying it here silently left two thirds of the log open,
			// which made every survivor share one confidence band and hid the
			// split entirely.
			Season: 2026, Week: 1, GameID: gid, Team: "KC",
			Scenario: scen, Belief: belief, Confidence: conf,
			Source: "prompt", Kickoff: kick, GeneratedAt: time.Now(),
			SBaseRate: f64(0.3243),
			Abstained: abstain, Flagged: informed && i%9 == 0,
		}
		if scen == "shootout" {
			p.Team = ""
		}
		if _, err := betlog.Record(log, p, time.Now()); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, outcomeRow{GameID: gid, Team: p.Team,
			Scenario: scen, Status: "settled", Occurred: happened})
	}
	outPath := writeTestJSON(t, dir, "week01.outcomes.json", outcomePack{
		Season: 2026, Week: 1, Definitions: gridDefinitions(), Rows: rows,
	})
	if err := beliefsSettle([]string{"-outcomes", outPath, "-log", log}); err != nil {
		t.Fatal(err)
	}
	if err := beliefsScore([]string{"-log", log, "-bar", "0.10"}); err != nil {
		t.Fatal(err)
	}
}

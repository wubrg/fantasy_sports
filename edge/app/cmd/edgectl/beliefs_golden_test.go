package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/scenario"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the golden score/ingest output")

// captureStdout runs f and returns everything it printed to os.Stdout. Used to
// pin the belief-score terminal output byte-for-byte across the ScoreResult
// refactor: the output is a pure function of the fixture (fixed bootstrap seed,
// no timestamps printed), so any diff is a behavior change.
func captureStdout(t *testing.T, f func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	ferr := f()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if ferr != nil {
		t.Fatalf("the command errored: %v", ferr)
	}
	return string(out)
}

// goldenScoreLog builds a deterministic settled belief log rich enough to
// exercise every section WriteTerminal will render: all four scenarios (two
// decision, two not), every reference type frozen on the rows that carry one,
// q/r from a real validated site so the E2 diagnostic fires, abstentions,
// flags, and a spread of confidence.
func goldenScoreLog(t *testing.T) (logPath, outPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "log.jsonl")
	kick := time.Now().Add(48 * time.Hour)

	c, err := scenario.LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}
	eoQ, eoR, _, _ := c.BestWagerableSite("efficient_offense")
	shQ, shR, _, _ := c.BestWagerableSite("shootout")

	scen := []string{"efficient_offense", "shootout", "pass_heavy", "blowout_loss"}
	var rows []outcomeRow
	for i := 0; i < 80; i++ {
		gid := "2026_01_G" + string(rune('A'+i/26)) + string(rune('A'+i%26))
		s := scen[i%4]
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
		conf := 0.2
		if informed {
			conf = 0.8
		}
		p := betlog.Prediction{
			Season: 2026, Week: 5, GameID: gid, Team: "KC",
			Scenario: s, Belief: belief, Confidence: conf,
			Source: "prompt", Kickoff: kick, GeneratedAt: time.Now(),
			SBaseRate: f64(0.3721),
			Abstained: abstain, Flagged: informed && i%9 == 0,
		}
		// Freeze references the way ingest would, per scenario, deterministically.
		switch s {
		case "efficient_offense":
			p.SIncumbent, p.SLine = f64(0.40), f64(0.36)
			if eoQ > eoR {
				p.Q, p.R = f64(eoQ), f64(eoR)
			}
		case "shootout":
			p.Team = ""
			p.SMarket = f64(0.34)
			if shQ > shR {
				p.Q, p.R = f64(shQ), f64(shR)
			}
		case "pass_heavy":
			p.SIncumbent, p.SLine = f64(0.33), f64(0.31)
		case "blowout_loss":
			p.Team = "KC"
			p.SMarket = f64(0.25)
		}
		if _, err := betlog.Record(logPath, p, time.Now()); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, outcomeRow{GameID: gid, Team: p.Team,
			Scenario: s, Status: "settled", Occurred: happened})
	}
	outPath = writeTestJSON(t, dir, "week05.outcomes.json", outcomePack{
		Season: 2026, Week: 5, Definitions: gridDefinitions(), Rows: rows,
	})
	if err := beliefsSettle([]string{"-outcomes", outPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}
	return logPath, outPath
}

// TestScoreOutputIsStable pins the full terminal output of `beliefs score`
// against a golden file. Generated with -update-golden before the ScoreResult
// refactor; if the refactor changes any printed byte, this fails. That is the
// behavior-preservation guarantee the plan requires.
func TestScoreOutputIsStable(t *testing.T) {
	log, _ := goldenScoreLog(t)
	got := captureStdout(t, func() error {
		return beliefsScore([]string{"-log", log, "-bar", "0.10", "-hold", "0.06"})
	})

	golden := filepath.Join("testdata", "score.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", golden, len(got))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run -update-golden first): %v", err)
	}
	if got != string(want) {
		t.Errorf("beliefs score output changed from the golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestScoreResultCarriesEverythingTheTerminalShows proves the object is the
// single source of truth: serialize the ScoreResult to JSON (what the web
// handler will send), read it back, render THAT with WriteTerminal, and it must
// match rendering the original. If the terminal showed a number the struct did
// not carry, the round-trip would drop it and this would fail.
func TestScoreResultCarriesEverythingTheTerminalShows(t *testing.T) {
	log, _ := goldenScoreLog(t)
	preds, err := betlog.LoadPredictions(log)
	if err != nil {
		t.Fatal(err)
	}
	res, err := score(preds, scoreOpts{vs: refAuto, bar: 0.10, hold: 0.06})
	if err != nil {
		t.Fatal(err)
	}

	var direct bytes.Buffer
	res.WriteTerminal(&direct)

	blob, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var roundTripped ScoreResult
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		t.Fatal(err)
	}
	var viaJSON bytes.Buffer
	roundTripped.WriteTerminal(&viaJSON)

	if direct.String() != viaJSON.String() {
		dl := bytes.Split(direct.Bytes(), []byte("\n"))
		jl := bytes.Split(viaJSON.Bytes(), []byte("\n"))
		for i := 0; i < len(dl) && i < len(jl); i++ {
			if !bytes.Equal(dl[i], jl[i]) {
				t.Fatalf("round-trip changed line %d:\n  direct: %q\n  json:   %q", i, dl[i], jl[i])
			}
		}
		t.Fatalf("round-trip changed the render (length %d vs %d)", len(dl), len(jl))
	}
}

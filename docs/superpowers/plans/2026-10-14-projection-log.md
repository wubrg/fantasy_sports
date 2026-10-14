# Projection Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record the grid's own median point-estimate alongside the market's posted line for every player/outcome a report prices, settle those against realized box scores after the week, and score in aggregate whether the grid's median tracks reality better than the book's number.

**Architecture:** A third append-only JSONL record kind (`Projection`/`SettledProjection`) added to the existing `internal/betlog` package, which already hosts two parallel record kinds (`Bet`, `Prediction`) sharing one stream format. Written via a new `-projections <path>` flag on `edgectl scenario` (additive — the existing `-log` flag is untouched). Settled via a new Python script that reads realized values out of the same `stats_player_week_<season>.csv` `player.py` already reads, paired with a new `edgectl projections settle` command — the same two-step shape as `beliefpack.py outcomes` → `edgectl beliefs settle`. Scored via new, dedicated MAE/win-rate/bias math (`edgectl projections score`) — explicitly not a reuse of `betlog.Score` or `beliefs score`, which measure probabilistic calibration, a different question from point-estimate accuracy.

**Tech Stack:** Go (`edge/app`, package `internal/betlog`, `cmd/edgectl`), Python 3 (`edge/model/analysis`).

**Spec:** `docs/superpowers/specs/2026-10-14-projection-log-design.md`

## Global Constraints

- Append-only JSONL, same convention as the betlog/ledger/belief log — no new database engine.
- The new log lives at `beliefs/projections.jsonl`, committed to git for the same predates-kickoff
  proof reason the belief log is (ADR-002).
- No logging of REFUSED grid cells — nothing to record without a median.
- No retrofit of existing reports (Week 1–6) — the log starts empty when this ships.
- `-log`'s existing behavior on `edgectl scenario` is completely unchanged; `-projections` is a
  new, independent, additive flag.

---

## Discovery notes (read before starting — these resolve ambiguity the spec left open)

The spec describes the design; these are the exact mechanics found reading the real code, which
the tasks below depend on:

1. **`betlog.go`'s `scan()` function (`app/internal/betlog/betlog.go:317-396`) is NOT generically
   kind-agnostic today**, despite the package comment's framing. Its switch statement
   (`scan():341`) hard-codes `case KindBet, KindBelief:` for registering a new record into the
   `byID`/`order` maps; anything else falls into `default: skipped[en.Kind]++` and is silently
   dropped. A third `KindProjection` needs this switch extended, or every projection written would
   be invisible to loading and un-settleable. This is Task 1, and it is the one place this plan
   touches code shared with `Bet`/`Prediction` — done carefully, as a pure addition, so existing
   betlog/belief behavior is provably unchanged (existing tests must stay green afterward).

2. **`Entry` (`betlog.go:148-166`) has no field for a raw numeric settlement value.** `Prediction`
   settles with a `Result` (occurred/not-occurred, a string enum via the `Won`/`Lost` aliasing
   trick already used for beliefs). A `Projection` settles with an `Actual *float64` — a real
   number, not an enum — which needs a new `Entry.Actual *float64` field carried through `scan()`'s
   `KindSettle` case into `record.Actual`.
   - **Decision:** still set `Entry.Result` on a projection's settlement too, reusing the existing
     `settleable()`/double-settle-prevention machinery for free rather than writing a second
     integrity check. `const Measured = Won` (new alias, mirroring `Occurred = Won` in `belief.go`)
     marks "this was settled with a real Actual value"; the existing `Void` is reused as-is for "the
     player didn't play, Actual is deliberately nil" — no new Result value needed there, since
     `Void` already means exactly that elsewhere in this package.

3. **`resolveConditionals` (`app/cmd/edgectl/scenario.go:231-360`) computes the grid's cell median
   internally (`qc.CellMedian`, via `app/internal/scenario/conditionals.go:793`'s `QR` function)
   but currently discards it** — its return signature is `(float64, float64, string, error)` (q, r,
   source, error), and `qc`/`rc` (the richer `Conditional` structs carrying `CellMedian`) never
   leave the function. It has exactly one caller (`scenario.go:158`), so widening its return is
   low-risk. When conditionals are **stated** by the operator (`-q`/`-r` passed directly, source
   `"stated"`), there is no grid cell and no median — `resolveConditionals` must report this
   distinguishably (a zero value is not enough, since 0 yards is a real possible median; a `bool`
   or pointer return is needed).

4. **The outcomes-settlement file format mirrors `beliefpack.py`'s `outcomes()` → `outcomeRow`
   Go struct (`app/cmd/edgectl/beliefs.go:61-68`) in shape, but is keyed differently.** Belief
   outcomes key on `(game_id, team, scenario)` because a scenario is a property of one game. A
   projection's realized value is a property of `(player, outcome, week)` only — **not**
   `scenario`, because the same player/outcome can appear in the log multiple times under
   different scenarios (e.g. Cooper Kupp's receiving yards projected under both `shootout` and
   `efficient_offense` in the same week, per the real Week 6 SEA@DEN report), all settling against
   the one real number he actually gained. The Python settlement script computes each
   `(player, outcome)` pair's realized value once; the Go settle command fans that one value out to
   every matching unsettled `Projection` regardless of which scenario it was logged under.

5. **Player-name matching must reuse `player.py`'s exact discipline** (`model/analysis/player.py`,
   the `main()` function's substring-match-with-ambiguity-refusal: `args.player.lower() in
   r["name"].lower()`, erroring if more than one name matches). A second, divergent name-matching
   implementation in the new settlement script would eventually disagree with `player.py`'s, the
   same silent-drift risk the spec already flags for the `OUTCOMES` column mapping.

6. **No pytest files exist anywhere under `model/`** (confirmed: `find model -iname "*test*.py"`
   returns nothing real). This repo's established Python verification convention is running a
   script directly against real cached data and reading its output — not a test suite. The Python
   task below follows that convention rather than introducing pytest where nothing else uses it.

---

## Task 1: Extend `betlog`'s shared substrate for a third record kind

**Files:**
- Modify: `app/internal/betlog/betlog.go:36-44` (Kind consts), `:148-166` (Entry struct),
  `:296-307` (record struct), `:317-396` (`scan()`)
- Test: `app/internal/betlog/betlog_test.go` (existing file — add to it)

**Interfaces:**
- Produces: `KindProjection Kind = "projection"` constant; `Entry.Projection *Projection`
  and `Entry.Actual *float64` fields; `record.Projection *Projection` and `record.Actual
  *float64` fields; `scan()` registers `KindProjection` entries into `byID`/`order` the same way
  `KindBet`/`KindBelief` do, and carries `en.Actual` through into `r.Actual` on the `KindSettle`
  branch. `Projection` itself is defined in Task 2 (forward-referenced here as an opaque type —
  this task only needs the type to exist as a pointer field; Task 2 fills in its shape).
- Consumes: nothing from later tasks.

This task is pure plumbing with no behavior change for `Bet`/`Prediction` — every existing test in
this package must still pass unmodified afterward, which is the regression check.

- [ ] **Step 1: Add a forward-declared `Projection` type stub so this file compiles standalone**

Create `app/internal/betlog/projection.go` with just enough for this task to compile (Task 2 fills
in the rest):

```go
package betlog

import "time"

// Projection is defined fully in Task 2. This stub exists so betlog.go's
// struct fields compile before that task lands.
type Projection struct {
	Season   int
	Week     int
	Player   string
	Outcome  string
	Scenario string
	Kickoff  time.Time
}
```

- [ ] **Step 2: Add the `KindProjection` constant and `Measured` alias**

In `app/internal/betlog/betlog.go`, after line 44 (`KindSettle Kind = "settle"`):

```go
	// KindProjection is the grid's own point-estimate for a player/outcome,
	// with no wager and no scenario claim implied — see projection.go.
	KindProjection Kind = "projection"
```

In `app/internal/betlog/belief.go`, next to the existing `Occurred`/`NotOccurred` aliases
(after line 18):

```go
// Measured is the settlement result for a Projection: the realized value was
// found and recorded. The same wire value as Won/Occurred -- one stream
// format -- but neither word fits what actually happened here, so this gets
// its own name at the call site even though it is byte-identical on disk.
const Measured = Won
```

- [ ] **Step 3: Add the new fields to `Entry` and `record`**

In `app/internal/betlog/betlog.go`, `Entry` struct (after line 154, the `Prediction` field):

```go
	// Projection is set on a KindProjection entry, never alongside Bet or
	// Prediction.
	Projection *Projection `json:"projection,omitempty"`
	// Actual is set on a KindSettle entry that settles a Projection -- the
	// player's realized value for that outcome, or nil if the player did not
	// play (the entry's Result is Void in that case, same convention as a
	// belief that could not be measured).
	Actual *float64 `json:"actual,omitempty"`
```

In the `record` struct (after line 301, the `Pred *Prediction` field):

```go
	Proj   *Projection
	Actual *float64
```

- [ ] **Step 4: Extend `scan()` to register and settle `KindProjection` entries**

In `app/internal/betlog/betlog.go`, the registration switch (line 341-354): change
`case KindBet, KindBelief:` to `case KindBet, KindBelief, KindProjection:`, and add a nil-check
and field assignment alongside the existing two:

```go
		case KindBet, KindBelief, KindProjection:
			if en.Kind == KindBet && en.Bet == nil {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: bet entry with no bet", path, line)
			}
			if en.Kind == KindBelief && en.Prediction == nil {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: belief entry with no prediction", path, line)
			}
			if en.Kind == KindProjection && en.Projection == nil {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: projection entry with no projection", path, line)
			}
			if _, dup := byID[en.ID]; dup {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: duplicate id %q", path, line, en.ID)
			}
			byID[en.ID] = &record{ID: en.ID, Kind: en.Kind, Time: en.Time,
				Bet: en.Bet, Pred: en.Prediction, Proj: en.Projection, Result: Open}
			order = append(order, en.ID)
```

In the `KindSettle` branch (after line 382, `r.LegResults = en.LegResults`), add:

```go
			r.Actual = en.Actual
```

- [ ] **Step 5: Build and run the existing package tests to confirm no regression**

Run: `cd edge/app && go build ./... && go test ./internal/betlog/...`
Expected: builds clean, all existing tests pass unmodified (confirms `Bet`/`Prediction` behavior
is untouched).

- [ ] **Step 6: Write a failing test for `scan()` registering a projection kind**

Add to `app/internal/betlog/betlog_test.go`:

```go
func TestScanRegistersProjectionEntries(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	proj := &Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", Kickoff: time.Now().Add(time.Hour)}
	if err := Append(path, Entry{Kind: KindProjection, ID: "p1", Time: time.Now(), Projection: proj}); err != nil {
		t.Fatal(err)
	}
	actual := 34.0
	if err := Append(path, Entry{Kind: KindSettle, ID: "p1", Time: time.Now(), Result: Measured, Actual: &actual}); err != nil {
		t.Fatal(err)
	}
	recs, _, skipped, err := scan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped kinds, got %v", skipped)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Proj == nil || recs[0].Proj.Player != "Cooper Kupp" {
		t.Fatalf("projection not carried through scan: %+v", recs[0])
	}
	if recs[0].Actual == nil || *recs[0].Actual != 34.0 {
		t.Fatalf("actual not carried through scan: %+v", recs[0].Actual)
	}
}
```

- [ ] **Step 7: Run it to confirm it passes**

Run: `cd edge/app && go test ./internal/betlog/... -run TestScanRegistersProjectionEntries -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
cd edge/app
git add internal/betlog/betlog.go internal/betlog/belief.go internal/betlog/projection.go internal/betlog/betlog_test.go
git commit -m "betlog: extend the shared stream substrate for a third record kind (Projection)"
```

---

## Task 2: `Projection` / `SettledProjection` types, `RecordProjection`, `Validate`, `LoadProjections`

**Files:**
- Modify: `app/internal/betlog/projection.go` (replace Task 1's stub with the real definitions)
- Test: `app/internal/betlog/projection_test.go` (new)

**Interfaces:**
- Consumes: `Kind`, `KindProjection`, `Entry`, `Append`, `scan`, `warnSkipped`, `Measured`,
  `NewID` (all from Task 1 / existing `betlog.go`).
- Produces: `Projection` (full struct), `(p Projection) Validate() error`,
  `(p Projection) Key() string`, `RecordProjection(path string, p Projection, now time.Time) (string,
  error)`, `SettledProjection` struct, `LoadProjections(path string) ([]SettledProjection,
  error)`. These are what Task 3 (settle/void) and Task 4 (score) and Task 5/6 (the CLI) build on.

- [ ] **Step 1: Write the failing test for `Validate` and `Key`**

```go
package betlog

import (
	"testing"
	"time"
)

func validProjection() Projection {
	return Projection{
		Season: 2026, Week: 6, Player: "Cooper Kupp", Outcome: "receiving_yards",
		Scenario: "shootout", OurMedian: 21.0, MarketLine: 21.0, Baseline: 25.8, Trend: 0.0007,
		GeneratedAt: time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC),
		Kickoff:     time.Date(2026, 10, 16, 0, 15, 0, 0, time.UTC),
	}
}

func TestProjectionKey(t *testing.T) {
	p := validProjection()
	want := "2026/6/Cooper Kupp/receiving_yards/shootout"
	if got := p.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestProjectionValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Projection)
	}{
		{"no season", func(p *Projection) { p.Season = 0 }},
		{"no week", func(p *Projection) { p.Week = 0 }},
		{"no player", func(p *Projection) { p.Player = "" }},
		{"no outcome", func(p *Projection) { p.Outcome = "" }},
		{"no scenario", func(p *Projection) { p.Scenario = "" }},
		{"no kickoff", func(p *Projection) { p.Kickoff = time.Time{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validProjection()
			c.mut(&p)
			if err := p.Validate(); err == nil {
				t.Errorf("expected Validate to reject %s", c.name)
			}
		})
	}
}

func TestProjectionValidateAcceptsAValidOne(t *testing.T) {
	if err := validProjection().Validate(); err != nil {
		t.Errorf("expected a valid projection to pass, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails (the real `Projection` type doesn't have these fields yet)**

Run: `cd edge/app && go test ./internal/betlog/... -run TestProjection -v`
Expected: FAIL (compile error — `Projection` is still Task 1's stub, missing `OurMedian` etc.)

- [ ] **Step 3: Replace the stub with the real type and its methods**

Replace the entire contents of `app/internal/betlog/projection.go`:

```go
package betlog

import (
	"fmt"
	"math"
	"time"
)

// Projection is what the grid said about a player/outcome, with nothing
// staked and no recommendation implied -- present whenever the grid prices a
// cell, whether or not the report ends up recommending a wager on it.
//
// It exists to answer a different question than Bet or Prediction do: not
// "did we recommend the right thing" but "was our own median a better
// point-estimate of what the player would do than the market's posted line."
// Recommendation count is small and selection-biased; every priced
// player/outcome is a much larger, much less biased sample of that question.
type Projection struct {
	Season   int    `json:"season"`
	Week     int    `json:"week"`
	Player   string `json:"player"`
	Outcome  string `json:"outcome"`  // receiving_yards, passing_yards, ...
	Scenario string `json:"scenario"` // shootout, pass_heavy, ...

	// OurMedian is the fitted grid's cell median for this player/outcome under
	// this scenario, in the outcome's own units -- the same number printed as
	// "median NN yds" in edgectl scenario's CONDITIONALS block.
	OurMedian float64 `json:"our_median"`
	// MarketLine is the operator-supplied -line: the book's posted O/U number,
	// same units as OurMedian.
	MarketLine float64 `json:"market_line"`

	Baseline float64 `json:"baseline"` // the player's own prior mean, for context
	Trend    float64 `json:"trend"`

	InputPack    string `json:"input_pack,omitempty"`
	InputPackSHA string `json:"input_pack_sha256,omitempty"`

	GeneratedAt time.Time `json:"generated_at"`
	Kickoff     time.Time `json:"kickoff"`
}

// Key is the join key settlement uses: one outcome, for one player, in one
// week, under one scenario. Mirrors Prediction.Key's shape.
func (p Projection) Key() string {
	return fmt.Sprintf("%d/%d/%s/%s/%s", p.Season, p.Week, p.Player, p.Outcome, p.Scenario)
}

// Validate rejects a projection that cannot be scored or settled.
func (p Projection) Validate() error {
	if p.Season <= 0 || p.Week <= 0 {
		return fmt.Errorf("betlog: projection needs a season and week, got %d/%d", p.Season, p.Week)
	}
	if p.Player == "" {
		return fmt.Errorf("betlog: projection has no player")
	}
	if p.Outcome == "" {
		return fmt.Errorf("betlog: projection has no outcome")
	}
	if p.Scenario == "" {
		return fmt.Errorf("betlog: projection has no scenario")
	}
	if math.IsNaN(p.OurMedian) || math.IsInf(p.OurMedian, 0) {
		return fmt.Errorf("betlog: our_median %v is not a real number", p.OurMedian)
	}
	if math.IsNaN(p.MarketLine) || math.IsInf(p.MarketLine, 0) {
		return fmt.Errorf("betlog: market_line %v is not a real number", p.MarketLine)
	}
	if p.Kickoff.IsZero() {
		return fmt.Errorf("betlog: projection has no kickoff, so it cannot be shown to predate one")
	}
	return nil
}

// Record appends a projection to the log, refusing one made after its own
// kickoff for the same reason Prediction.Record does -- see belief.go and
// ADR-002. `now` is passed rather than read so the gate is testable.
func RecordProjection(path string, p Projection, now time.Time) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if !p.GeneratedAt.IsZero() && !p.GeneratedAt.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s was generated at %s, which is not before kickoff at %s — a projection "+
				"made after the game started is not a projection",
			p.Key(), p.GeneratedAt.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	if !now.Before(p.Kickoff) {
		return "", fmt.Errorf(
			"betlog: %s cannot be recorded at %s, after its kickoff at %s",
			p.Key(), now.UTC().Format(time.RFC3339), p.Kickoff.UTC().Format(time.RFC3339))
	}
	id := NewID(now, p.Outcome+"-"+p.Player+"-"+p.Scenario)
	if err := Append(path, Entry{Kind: KindProjection, ID: id, Time: now, Projection: &p}); err != nil {
		return "", err
	}
	return id, nil
}

// SettledProjection is a projection joined to what the player actually did.
// Actual is nil until settled, and a *float64 rather than a bare float so
// "0 yards, really" and "not yet known" are distinguishable.
type SettledProjection struct {
	ID         string
	Recorded   time.Time
	Projection Projection
	Result     Result // Open, Measured, or Void
	Actual     *float64
	SettleAt   time.Time
	Note       string
}

// LoadProjections reads the stream and folds settlements onto their
// projections. Mirrors LoadPredictions: a betlog or belief file is refused
// rather than silently scored over the wrong population.
func LoadProjections(path string) ([]SettledProjection, error) {
	recs, _, skipped, err := scan(path)
	if err != nil {
		return nil, err
	}
	warnSkipped(path, skipped)

	out := make([]SettledProjection, 0, len(recs))
	for _, r := range recs {
		if r.Kind == KindBet {
			return nil, fmt.Errorf(
				"betlog: %s holds wagers, not projections — read it with `edgectl log score`", path)
		}
		if r.Kind == KindBelief {
			return nil, fmt.Errorf(
				"betlog: %s holds belief predictions, not projections — read it with "+
					"`edgectl beliefs score`", path)
		}
		out = append(out, SettledProjection{ID: r.ID, Recorded: r.Time, Projection: *r.Proj,
			Result: r.Result, Actual: r.Actual, SettleAt: r.SettleAt, Note: r.Note})
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/betlog/... -run TestProjection -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Write and run a failing test for `LoadProjections` refusing the wrong file kind, then confirm it passes**

```go
func TestLoadProjectionsRefusesABetLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	if _, err := PlaceBet(path, Bet{Selection: "x", Price: -110, Bankroll: "real money", Stake: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjections(path); err == nil {
		t.Error("expected LoadProjections to refuse a file holding wagers")
	}
}

func TestLoadProjectionsRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, err := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != id {
		t.Fatalf("expected 1 projection with id %q, got %+v", id, loaded)
	}
	if loaded[0].Result != Open {
		t.Errorf("expected an unsettled projection to be Open, got %v", loaded[0].Result)
	}
	if loaded[0].Actual != nil {
		t.Errorf("expected Actual to be nil before settlement, got %v", *loaded[0].Actual)
	}
}
```

Run: `cd edge/app && go test ./internal/betlog/... -run TestLoadProjections -v`
Expected: both PASS

- [ ] **Step 6: Write and run a failing-then-passing test for the predates-kickoff gate**

```go
func TestRecordRefusesAfterKickoff(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection() // Kickoff is in the future relative to time.Now() in the fixture
	p.Kickoff = time.Now().Add(-time.Minute) // already kicked off
	if _, err := RecordProjection(path, p, time.Now()); err == nil {
		t.Error("expected Record to refuse a projection recorded after its own kickoff")
	}
}
```

Run: `cd edge/app && go test ./internal/betlog/... -run TestRecordRefusesAfterKickoff -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd edge/app
git add internal/betlog/projection.go internal/betlog/projection_test.go
git commit -m "betlog: add Projection/SettledProjection, RecordProjection, Validate, LoadProjections"
```

---

## Task 3: Settle and void a projection

**Files:**
- Modify: `app/internal/betlog/projection.go` (append `SettleProjection`/`VoidProjection`)
- Test: `app/internal/betlog/projection_test.go`

**Interfaces:**
- Consumes: `Measured` (Task 1), `Void`, `Append`, `Entry` (existing `betlog.go`).
- Produces: `SettleProjection(path, id string, actual float64, note string) error`,
  `VoidProjection(path, id, reason string) error` — what Task 6's `edgectl projections settle`
  command calls per row.

- [ ] **Step 1: Write the failing test**

```go
func TestSettleProjection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))

	if err := SettleProjection(path, id, 41.0, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].Result != Measured {
		t.Errorf("expected Measured, got %v", loaded[0].Result)
	}
	if loaded[0].Actual == nil || *loaded[0].Actual != 41.0 {
		t.Fatalf("expected Actual 41.0, got %v", loaded[0].Actual)
	}
}

func TestVoidProjectionRequiresAReason(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))

	if err := VoidProjection(path, id, ""); err == nil {
		t.Error("expected VoidProjection to require a reason")
	}
	if err := VoidProjection(path, id, "player inactive"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadProjections(path)
	if loaded[0].Result != Void {
		t.Errorf("expected Void, got %v", loaded[0].Result)
	}
	if loaded[0].Actual != nil {
		t.Errorf("expected nil Actual on a void, got %v", *loaded[0].Actual)
	}
}

func TestSettleProjectionCannotBeResettled(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/log.jsonl"
	p := validProjection()
	id, _ := RecordProjection(path, p, p.Kickoff.Add(-time.Hour))
	if err := SettleProjection(path, id, 41.0, "first"); err != nil {
		t.Fatal(err)
	}
	// Appending a second settlement is possible -- the file is append-only --
	// but the log must then refuse to LOAD rather than take the last write.
	// This mirrors TestBeliefCannotBeResettled (belief_test.go) exactly: the
	// second Settle/SettleBelief call itself succeeds, because Append has no
	// read-before-write check; only scan()'s replay (exercised via Load) is
	// where the double-settle is caught. A test asserting the second
	// SettleProjection call's own return value would be testing behavior
	// this package does not have anywhere else, and no new code should be
	// added to manufacture it for projections alone.
	if err := SettleProjection(path, id, 50.0, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjections(path); err == nil {
		t.Fatal("a second settlement was accepted; hindsight is what this log exists to prevent")
	} else if !strings.Contains(err.Error(), "already settled") {
		t.Errorf("error should name the double settlement, got: %v", err)
	}
}
```

`projection_test.go`'s existing import block (from Task 2) is `"testing"` and `"time"` only — this
test adds a new import, `"strings"`, to that same block (mirroring `belief_test.go`'s own import
list, which already has it for exactly this assertion style).

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./internal/betlog/... -run "TestSettleProjection|TestVoidProjection" -v`
Expected: FAIL (compile error — functions don't exist yet)

- [ ] **Step 3: Implement**

Append to `app/internal/betlog/projection.go`:

```go
// SettleProjection appends a realized value for a previously recorded
// projection.
func SettleProjection(path, id string, actual float64, note string) error {
	if math.IsNaN(actual) || math.IsInf(actual, 0) {
		return fmt.Errorf("betlog: actual %v is not a real number", actual)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(),
		Result: Measured, Actual: &actual, Note: note})
}

// VoidProjection records that a projection could not be settled -- the
// player did not play, a name could not be matched, whatever the reason. Void
// is excluded from scoring (Result.Counts()), the same convention VoidBelief
// uses and for the same reason: an unmeasurable case is not the same claim as
// "the player did worse than projected."
func VoidProjection(path, id, reason string) error {
	if reason == "" {
		return fmt.Errorf("betlog: voiding %s needs a reason", id)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: Void, Note: reason})
}
```

Note: `SettleProjection`/`VoidProjection` need no double-settle check of their own. Both are pure
`Append()` wrappers, exactly like `SettleBelief`/`VoidBelief` — a second `SettleProjection` call
on an already-settled id succeeds at the append level (the file is append-only; nothing checks
before writing). The refusal `TestSettleProjectionCannotBeResettled` exercises is enforced
generically at LOAD time by `scan()`'s existing `r.Result != Open` check (`betlog.go:367-373`,
added in Task 1), which applies to every kind, including `KindProjection`, for free — it fires
when `LoadProjections` is called, not inside `SettleProjection` itself. **Do not add a
pre-settlement existence/duplicate check inside `SettleProjection` or `VoidProjection`** — that
would be new, duplicated logic the package doesn't have anywhere else (confirmed against
`SettleBelief`/`VoidBelief`, which have none), and would make projections behave inconsistently
with how `Bet` and `Prediction` already work.

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/betlog/... -run "TestSettleProjection|TestVoidProjection" -v`
Expected: PASS (all three)

- [ ] **Step 5: Commit**

```bash
cd edge/app
git add internal/betlog/projection.go internal/betlog/projection_test.go
git commit -m "betlog: add SettleProjection and VoidProjection"
```

---

## Task 4: Score a settled population — MAE, win rate, signed bias, per outcome

**Files:**
- Modify: `app/internal/betlog/projection.go` (append `ProjectionScore`, `ScoreProjections`,
  `ByOutcome`)
- Test: `app/internal/betlog/projection_test.go`

**Interfaces:**
- Consumes: `SettledProjection` (Task 2).
- Produces: `ProjectionScore` struct, `ScoreProjections(projs []SettledProjection, filter
  func(SettledProjection) bool) (ProjectionScore, error)`, `ByOutcome(projs
  []SettledProjection) (map[string]ProjectionScore, error)` — what Task 6's `edgectl projections
  score` command calls and prints.

- [ ] **Step 1: Write the failing test, pinning a worked example by hand**

Four settled rows, hand-computed expected values (worked by hand so the test is checking the
implementation against an independently-derived answer, the same discipline `ev_test.go` uses
pinning the source documents' own worked examples):

| player/outcome | our_median | market_line | actual | our error | market error | closer |
|---|---|---|---|---|---|---|
| A | 50 | 60 | 55 | 5 | 5 | tie (not a win for either) |
| B | 50 | 60 | 48 | 2 | 12 | ours |
| C | 50 | 60 | 65 | 15 | 5 | market |
| D | 50 | 60 | 52 | 2 | 8 | ours |

Our MAE = (5+2+15+2)/4 = 6.0. Market MAE = (5+12+5+8)/4 = 7.5. Win rate (strictly closer than
market) = 2/4 = 0.5 (row A is a tie, counted as neither side's win). Our signed bias = mean(50-55,
50-48, 50-65, 50-52) = mean(-5, +2, -15, -2) = -5.0. Market signed bias = mean(60-55, 60-48,
60-65, 60-52) = mean(+5, +12, -5, +8) = +5.0.

```go
func TestScoreProjectionsMatchesAHandWorkedExample(t *testing.T) {
	mk := func(median, line, actual float64) SettledProjection {
		a := actual
		return SettledProjection{
			Projection: Projection{Outcome: "receiving_yards", OurMedian: median, MarketLine: line},
			Result:     Measured, Actual: &a,
		}
	}
	rows := []SettledProjection{mk(50, 60, 55), mk(50, 60, 48), mk(50, 60, 65), mk(50, 60, 52)}

	s, err := ScoreProjections(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.N != 4 {
		t.Errorf("N = %d, want 4", s.N)
	}
	if math.Abs(s.OurMAE-6.0) > 1e-9 {
		t.Errorf("OurMAE = %v, want 6.0", s.OurMAE)
	}
	if math.Abs(s.MarketMAE-7.5) > 1e-9 {
		t.Errorf("MarketMAE = %v, want 7.5", s.MarketMAE)
	}
	if math.Abs(s.WinRate-0.5) > 1e-9 {
		t.Errorf("WinRate = %v, want 0.5", s.WinRate)
	}
	if math.Abs(s.OurBias-(-5.0)) > 1e-9 {
		t.Errorf("OurBias = %v, want -5.0", s.OurBias)
	}
	if math.Abs(s.MarketBias-5.0) > 1e-9 {
		t.Errorf("MarketBias = %v, want 5.0", s.MarketBias)
	}
}

func TestScoreProjectionsExcludesUnsettledAndVoid(t *testing.T) {
	a := 55.0
	rows := []SettledProjection{
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Measured, Actual: &a},
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Open}, // not yet settled
		{Projection: Projection{OurMedian: 50, MarketLine: 60}, Result: Void}, // unmeasurable
	}
	s, err := ScoreProjections(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.N != 1 {
		t.Errorf("N = %d, want 1 (only the Measured row counts)", s.N)
	}
}

func TestByOutcomeGroupsSeparately(t *testing.T) {
	a := 55.0
	rows := []SettledProjection{
		{Projection: Projection{Outcome: "receiving_yards", OurMedian: 50, MarketLine: 60}, Result: Measured, Actual: &a},
		{Projection: Projection{Outcome: "rushing_yards", OurMedian: 40, MarketLine: 45}, Result: Measured, Actual: &a},
	}
	byOutcome, err := ByOutcome(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(byOutcome) != 2 {
		t.Fatalf("expected 2 outcome groups, got %d: %+v", len(byOutcome), byOutcome)
	}
	if byOutcome["receiving_yards"].N != 1 || byOutcome["rushing_yards"].N != 1 {
		t.Errorf("expected 1 row per outcome, got %+v", byOutcome)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./internal/betlog/... -run "TestScoreProjections|TestByOutcome" -v`
Expected: FAIL (compile error)

- [ ] **Step 3: Implement**

Append to `app/internal/betlog/projection.go`:

```go
// ProjectionScore compares the grid's median against the market's line, over
// a settled population. Unlike Calibration (betlog.go) or the belief log's
// scoring, this measures point-estimate accuracy, not probabilistic
// calibration -- "was our number closer to reality," not "did a stated
// probability come true at the stated rate." Different question, different
// math, deliberately not reused from either existing scorer.
type ProjectionScore struct {
	N int // settled rows scored; pending and void rows are excluded

	OurMAE    float64 // mean(|OurMedian - Actual|)
	MarketMAE float64 // mean(|MarketLine - Actual|)

	// WinRate is the fraction of rows where our median was STRICTLY closer to
	// the actual than the market's line. A tie counts toward neither side.
	WinRate float64

	// OurBias and MarketBias are mean(estimate - actual): positive means
	// systematically projecting high, negative means systematically low. Two
	// sides can have the same MAE while one is honest noise and the other is a
	// consistent directional error, and only the signed mean distinguishes them.
	OurBias    float64
	MarketBias float64
}

// ScoreProjections computes a ProjectionScore over settled rows only --
// Result == Measured, Actual != nil. Pending (Open) and unmeasurable (Void)
// rows contribute nothing, mirroring how beliefs score excludes ungraded
// predictions.
func ScoreProjections(projs []SettledProjection, filter func(SettledProjection) bool) (ProjectionScore, error) {
	var s ProjectionScore
	var ourAbsSum, marketAbsSum, ourSignedSum, marketSignedSum float64
	var wins int
	for _, p := range projs {
		if filter != nil && !filter(p) {
			continue
		}
		if p.Result != Measured || p.Actual == nil {
			continue
		}
		actual := *p.Actual
		ourErr := p.Projection.OurMedian - actual
		marketErr := p.Projection.MarketLine - actual
		ourAbs, marketAbs := math.Abs(ourErr), math.Abs(marketErr)

		s.N++
		ourAbsSum += ourAbs
		marketAbsSum += marketAbs
		ourSignedSum += ourErr
		marketSignedSum += marketErr
		if ourAbs < marketAbs {
			wins++
		}
	}
	if s.N > 0 {
		s.OurMAE = ourAbsSum / float64(s.N)
		s.MarketMAE = marketAbsSum / float64(s.N)
		s.WinRate = float64(wins) / float64(s.N)
		s.OurBias = ourSignedSum / float64(s.N)
		s.MarketBias = marketSignedSum / float64(s.N)
	}
	return s, nil
}

// ByOutcome scores settled projections grouped by outcome -- receiving_yards
// is not passing_yards, and a pooled number would hide which one the grid
// actually has skill at, the same reasoning BySource (betlog.go) applies to
// scenario source.
func ByOutcome(projs []SettledProjection) (map[string]ProjectionScore, error) {
	seen := map[string]bool{}
	for _, p := range projs {
		seen[p.Projection.Outcome] = true
	}
	out := map[string]ProjectionScore{}
	for outcome := range seen {
		want := outcome
		s, err := ScoreProjections(projs, func(p SettledProjection) bool {
			return p.Projection.Outcome == want
		})
		if err != nil {
			return nil, err
		}
		out[outcome] = s
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/betlog/... -run "TestScoreProjections|TestByOutcome" -v`
Expected: PASS (all three)

- [ ] **Step 5: Run the full package test suite**

Run: `cd edge/app && go test ./internal/betlog/...`
Expected: all green, including every test from Tasks 1-3.

- [ ] **Step 6: Commit**

```bash
cd edge/app
git add internal/betlog/projection.go internal/betlog/projection_test.go
git commit -m "betlog: score settled projections (MAE, win rate, signed bias), by outcome"
```

---

## Task 5: Expose the grid's median from `resolveConditionals`, and write it via `edgectl scenario -projections`

**Files:**
- Modify: `app/cmd/edgectl/scenario.go:158` (call site), `:231-360` (`resolveConditionals`
  signature and body)
- Test: `app/cmd/edgectl/scenario_test.go` (existing file — add to it; if it doesn't exist yet,
  create it following the pattern in `board_props_report_test.go`)

**Interfaces:**
- Consumes: `betlog.Record`, `betlog.Projection` (Task 2).
- Produces: `resolveConditionals` now returns `(q, r float64, condSource string, median float64,
  hasMedian bool, err error)` — the two new values at the end so the three existing positional
  values keep their positions for anyone else reading the diff. `scenarioCmd` writes a
  `betlog.Projection` when `-projections` is set and `hasMedian` is true.

- [ ] **Step 1: Write the failing test for the signature change**

Since `resolveConditionals` has exactly one real caller and real grid data is required to
exercise the "pooled" path meaningfully, this step adds a narrow unit test confirming the
**"stated" path reports `hasMedian = false`** (the case that needs no grid data and is cheap to
test in isolation):

```go
package main

import (
	"testing"

	"edge/internal/scenario"
)

func TestResolveConditionalsStatedHasNoMedian(t *testing.T) {
	q, r, source, median, hasMedian, err := resolveConditionals(
		"receiving_yards", "shootout", 0.6, 0.4,
		scenario.Site{Posted: 49, Baseline: 50, Trend: 0.01}, 52.5, 0.95, "total", 50, "over")
	if err != nil {
		t.Fatal(err)
	}
	if source != "stated" {
		t.Errorf("source = %q, want stated", source)
	}
	if hasMedian {
		t.Errorf("expected hasMedian = false when q/r are operator-stated, got median %v", median)
	}
	if q != 0.6 || r != 0.4 {
		t.Errorf("q/r = %v/%v, want 0.6/0.4 (passed straight through, unchanged)", q, r)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestResolveConditionalsStatedHasNoMedian -v`
Expected: FAIL (compile error — wrong number of return values for the current signature)

- [ ] **Step 3: Widen `resolveConditionals`'s signature and both return paths**

In `app/cmd/edgectl/scenario.go`, change the function signature (line 231-234) from:

```go
func resolveConditionals(
	outcome, name string, q, r float64, at scenario.Site, line, confidence float64,
	basis string, threshold float64, side string,
) (float64, float64, string, error) {
```

to:

```go
func resolveConditionals(
	outcome, name string, q, r float64, at scenario.Site, line, confidence float64,
	basis string, threshold float64, side string,
) (qv, rv float64, condSource string, ourMedian float64, hasMedian bool, err error) {
```

Update every `return` inside it to carry the two new values:

- Line 236: `return 0, 0, "", fmt.Errorf(...)` → `return 0, 0, "", 0, false, fmt.Errorf(...)`
  (repeat for every other early-error return in the function — lines 239-241's error path,
  242-244, 247-249, 254-256, 261 (the `LoadConditionals` error), 267 (the `CheckDefinition`
  error), 278 (the `scenario.ErrScenarioNotPriceable` wrapped error), and the plain `err` return
  at the end of the `c.QR` error branch)
- Line 239 (the stated-q/r success path): `return q, r, "stated", nil` →
  `return q, r, "stated", 0, false, nil`
- The final return (line 360): `return qc.Prob, rc.Prob, "pooled", nil` →
  `return qc.Prob, rc.Prob, "pooled", qc.CellMedian, true, nil`

- [ ] **Step 4: Update the one call site**

In `app/cmd/edgectl/scenario.go:158`, change:

```go
	qv, rv, condSource, err := resolveConditionals(
```

to:

```go
	qv, rv, condSource, ourMedian, hasMedian, err := resolveConditionals(
```

(The variable names `qv`, `rv`, `condSource` already exist downstream unchanged; `ourMedian` and
`hasMedian` are new local variables Step 6 below uses.)

- [ ] **Step 5: Run to confirm the new test passes and nothing else broke**

Run: `cd edge/app && go build ./... && go test ./cmd/edgectl/... -run TestResolveConditionals -v`
Expected: PASS. If `go build` fails, it means another return path inside `resolveConditionals`
was missed in Step 3 — the compiler error names the exact line.

- [ ] **Step 6: Add the `-projections` flag and the write path**

In `app/cmd/edgectl/scenario.go`, add the flag next to the existing `-log` flag (near line 35):

```go
	projectionsPath := fs.String("projections", "",
		"append this priced player/outcome to a projection log at this path -- "+
			"independent of -log, and fires even when no wager is recommended")
```

After the existing `-log` block (the `if *logPath != "" { ... }` block near line 202-215 in the
current file), add:

```go
	if *projectionsPath != "" {
		if !hasMedian {
			return fmt.Errorf(
				"-projections has nothing to log: q and r came from stated -q/-r, not the " +
					"fitted grid, so there is no cell median to compare against the market line")
		}
		now := time.Now()
		proj := betlog.Projection{
			Season: 0, Week: 0, // see note below -- these are not derivable from this command's own flags
			Player: pick(*label, "unlabelled"), Outcome: *outcome, Scenario: sc.Name,
			OurMedian: ourMedian, MarketLine: *line,
			Baseline: *baseline, Trend: *trend,
			GeneratedAt: now, Kickoff: now.Add(7 * 24 * time.Hour), // see note below
		}
		id, err := betlog.RecordProjection(*projectionsPath, proj, now)
		if err != nil {
			return err
		}
		fmt.Printf("  projection logged as %s\n", id)
	}
```

**This step has two real gaps `edgectl scenario` cannot resolve on its own, and the plan should
not paper over them with a fabricated value:**

1. **Season and Week.** `edgectl scenario` has no `-season`/`-week` flags today — nothing else in
   this command needs them. A `Projection` requires both (`Validate` rejects `Season <= 0`). Add
   two new required flags, `-season` and `-week`, used only when `-projections` is set:

   ```go
   season := fs.Int("season", 0, "season, required when -projections is set")
   week := fs.Int("week", 0, "the NFL week this projection is FOR, required when -projections is set")
   ```

   and validate them inside the `-projections` block before constructing `proj`:

   ```go
   if *season <= 0 || *week <= 0 {
       return fmt.Errorf("-projections requires -season and -week")
   }
   ```

   then use `*season, *week` instead of the placeholder `0, 0` above.

2. **Kickoff.** `edgectl scenario` also has no kickoff flag — the command prices a wager, it
   doesn't know what game it's for beyond `-total`/`-spread`. The predates-kickoff gate
   (`RecordProjection`) needs a real kickoff to check against. Add a third flag:

   ```go
   kickoffFlag := fs.String("kickoff", "", "ISO8601 kickoff time, required when -projections is set (e.g. 2026-10-16T00:15:00Z)")
   ```

   parsed with `time.Parse(time.RFC3339, *kickoffFlag)` inside the `-projections` block (error out
   on a bad format), replacing the placeholder `now.Add(7 * 24 * time.Hour)` above with the real
   parsed time.

   Rewrite the `-projections` block to reflect both additions:

   ```go
   if *projectionsPath != "" {
       if !hasMedian {
           return fmt.Errorf(
               "-projections has nothing to log: q and r came from stated -q/-r, not the " +
                   "fitted grid, so there is no cell median to compare against the market line")
       }
       if *season <= 0 || *week <= 0 {
           return fmt.Errorf("-projections requires -season and -week")
       }
       kickoff, err := time.Parse(time.RFC3339, *kickoffFlag)
       if err != nil {
           return fmt.Errorf("-kickoff: %w", err)
       }
       now := time.Now()
       proj := betlog.Projection{
           Season: *season, Week: *week,
           Player: pick(*label, "unlabelled"), Outcome: *outcome, Scenario: sc.Name,
           OurMedian: ourMedian, MarketLine: *line,
           Baseline: *baseline, Trend: *trend,
           GeneratedAt: now, Kickoff: kickoff,
       }
       id, err := betlog.RecordProjection(*projectionsPath, proj, now)
       if err != nil {
           return err
       }
       fmt.Printf("  projection logged as %s\n", id)
   }
   ```

   Add `"time"` to the import block if not already present (it is — `time.Now()` is unused
   elsewhere in this file today, confirm with `goimports`/`go build` after this edit).

- [ ] **Step 7: Write a CLI-level integration test**

```go
func TestScenarioProjectionsFlagWritesAProjection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/projections.jsonl"
	kickoff := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	err := scenarioCmd([]string{
		"-outcome", "receiving_yards", "-name", "shootout",
		"-total", "40.5", "-threshold", "50",
		"-baseline", "25.8", "-trend", "0.0007",
		"-line", "21", "-price", "-116", "-belief", "0.5",
		"-label", "Cooper Kupp",
		"-projections", path, "-season", "2026", "-week", "6", "-kickoff", kickoff,
	})
	if err != nil {
		t.Fatalf("scenarioCmd: %v", err)
	}
	loaded, err := betlog.LoadProjections(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 projection written, got %d", len(loaded))
	}
	if loaded[0].Projection.Player != "Cooper Kupp" || loaded[0].Projection.MarketLine != 21 {
		t.Errorf("unexpected projection content: %+v", loaded[0].Projection)
	}
}

func TestScenarioProjectionsFlagRefusedWhenQRStated(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/projections.jsonl"
	kickoff := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	err := scenarioCmd([]string{
		"-outcome", "receiving_yards", "-name", "shootout",
		"-total", "49", "-threshold", "50",
		"-q", "0.6", "-r", "0.4", // stated, not looked up -- no median exists
		"-line", "21", "-price", "-116", "-belief", "0.5",
		"-projections", path, "-season", "2026", "-week", "6", "-kickoff", kickoff,
	})
	if err == nil {
		t.Error("expected an error: -projections has nothing to log when q/r are stated")
	}
}
```

(Note: `-total 40.5` above was verified directly against the real, current
`app/internal/scenario/artifacts/conditionals.json` before this task was dispatched — it prices
cleanly, PRICEABLE, median 17 yds. `-total 49` does NOT price at these coordinates — it falls into
a different posted-total band whose direction doesn't hold out of sample, confirmed by running the
built binary directly. If a future refit of `conditionals.json` changes this, the fallback is the
same either way: `python3 model/analysis/player.py --player <real player> --season 2026 --week <n>
--outcome receiving_yards --total <n> --scenario shootout` against the real cache, copy its printed
`-baseline`/`-trend`/`edgectl scenario` command into the test.)

- [ ] **Step 8: Run to confirm both pass**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestScenarioProjections -v`
Expected: PASS (both)

- [ ] **Step 9: Run the full `cmd/edgectl` test suite to confirm no regression**

Run: `cd edge/app && go test ./cmd/edgectl/...`
Expected: all green, including every pre-existing test (confirms `-log`'s behavior and every
other `scenario` flag path is unaffected).

- [ ] **Step 10: Commit**

```bash
cd edge/app
git add cmd/edgectl/scenario.go cmd/edgectl/scenario_test.go
git commit -m "edgectl scenario: expose the grid's cell median, add -projections to log it"
```

---

## Task 6: `edgectl projections settle` and `edgectl projections score`

**Files:**
- Create: `app/cmd/edgectl/projections.go`
- Modify: `app/cmd/edgectl/main.go` (register the `projections` subcommand)
- Test: `app/cmd/edgectl/projections_test.go` (new)

**Interfaces:**
- Consumes: `betlog.LoadProjections`, `betlog.SettleProjection`, `betlog.VoidProjection`,
  `betlog.ScoreProjections`, `betlog.ByOutcome` (all from Tasks 2-4).
- Produces: `projectionsCmd(args []string) error`, dispatching `settle` and `score` — registered
  in `main.go` the same way `beliefsCmd` is.

The outcomes-settlement file this command's `settle` subcommand reads is produced by Task 7's
Python script. Its shape (defined here, consumed by both):

```json
{
  "season": 2026,
  "week": 6,
  "rows": [
    {"player": "Cooper Kupp", "outcome": "receiving_yards", "status": "settled", "actual": 34.0, "reason": ""},
    {"player": "Sam Darnold", "outcome": "passing_yards", "status": "unavailable", "actual": null, "reason": "no stats_player_week row for this player/week"}
  ]
}
```

`status` is `"settled"` or `"unavailable"` — mirroring `beliefpack.py outcomes`' `outcomeRow`
shape (`settled`/`pending`/`unavailable`), minus `pending` (there is no "game not played yet"
state here the way there is for a scenario — the settlement step only ever runs after the week is
over, so a row is either found or it isn't).

- [ ] **Step 1: Write the failing test for `projectionsSettle`**

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
)

func writeOutcomesFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "outcomes.json")
	body := map[string]any{
		"season": 2026, "week": 6,
		"rows": []map[string]any{
			{"player": "Cooper Kupp", "outcome": "receiving_yards", "status": "settled", "actual": 34.0, "reason": ""},
			{"player": "Sam Darnold", "outcome": "passing_yards", "status": "unavailable", "actual": nil, "reason": "inactive"},
		},
	}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProjectionsSettle(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)

	kupp := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 21,
		Kickoff: past.Add(time.Minute)}
	darnold := betlog.Projection{Season: 2026, Week: 6, Player: "Sam Darnold",
		Outcome: "passing_yards", Scenario: "shootout", OurMedian: 200, MarketLine: 220,
		Kickoff: past.Add(time.Minute)}
	if _, err := betlog.RecordProjection(logPath, kupp, past); err != nil {
		t.Fatal(err)
	}
	if _, err := betlog.RecordProjection(logPath, darnold, past); err != nil {
		t.Fatal(err)
	}

	outcomesPath := writeOutcomesFixture(t, dir)
	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}

	loaded, err := betlog.LoadProjections(logPath)
	if err != nil {
		t.Fatal(err)
	}
	byPlayer := map[string]betlog.SettledProjection{}
	for _, p := range loaded {
		byPlayer[p.Projection.Player] = p
	}
	if byPlayer["Cooper Kupp"].Result != betlog.Measured || *byPlayer["Cooper Kupp"].Actual != 34.0 {
		t.Errorf("Kupp not settled correctly: %+v", byPlayer["Cooper Kupp"])
	}
	if byPlayer["Sam Darnold"].Result != betlog.Void {
		t.Errorf("Darnold should be void, got %+v", byPlayer["Sam Darnold"])
	}
}

func TestProjectionsSettleIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)
	kupp := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 21,
		Kickoff: past.Add(time.Minute)}
	betlog.RecordProjection(logPath, kupp, past)
	outcomesPath := writeOutcomesFixture(t, dir)

	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}
	// Running it again must not error or double-settle.
	if err := projectionsCmd([]string{"settle", "-outcomes", outcomesPath, "-log", logPath}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestProjectionsSettle -v`
Expected: FAIL (compile error — `projectionsCmd` does not exist)

- [ ] **Step 3: Implement `app/cmd/edgectl/projections.go`**

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"edge/internal/betlog"
)

func projectionsCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: edgectl projections settle|score")
	}
	switch args[0] {
	case "settle":
		return projectionsSettle(args[1:])
	case "score":
		return projectionsScore(args[1:])
	}
	return fmt.Errorf("edgectl projections: unknown subcommand %q (want settle or score)", args[0])
}

type outcomesFile struct {
	Season int                `json:"season"`
	Week   int                `json:"week"`
	Rows   []outcomesFileRow  `json:"rows"`
}

type outcomesFileRow struct {
	Player  string   `json:"player"`
	Outcome string   `json:"outcome"`
	Status  string   `json:"status"` // settled | unavailable
	Actual  *float64 `json:"actual"`
	Reason  string   `json:"reason"`
}

func projectionsSettle(args []string) error {
	fs := flag.NewFlagSet("projections settle", flag.ExitOnError)
	outcomesPath := fs.String("outcomes", "", "the realized-outcomes file for this week (required)")
	logPath := fs.String("log", "beliefs/projections.jsonl", "the projection log")
	dry := fs.Bool("n", false, "print the plan and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outcomesPath == "" {
		return fmt.Errorf("projections settle: -outcomes is required")
	}

	f, err := os.Open(*outcomesPath)
	if err != nil {
		return fmt.Errorf("projections settle: opening %s: %w", *outcomesPath, err)
	}
	defer f.Close()
	var of outcomesFile
	if err := json.NewDecoder(f).Decode(&of); err != nil {
		return fmt.Errorf("projections settle: %s: %w", *outcomesPath, err)
	}

	type key struct{ player, outcome string }
	result := map[key]outcomesFileRow{}
	for _, r := range of.Rows {
		result[key{r.Player, r.Outcome}] = r
	}

	projs, err := betlog.LoadProjections(*logPath)
	if err != nil {
		return err
	}

	var settle, void []betlog.SettledProjection
	var pending, already int
	for _, sp := range projs {
		p := sp.Projection
		if p.Season != of.Season || p.Week != of.Week {
			continue
		}
		if sp.Result != betlog.Open {
			already++
			continue
		}
		r, ok := result[key{p.Player, p.Outcome}]
		if !ok {
			pending++
			continue
		}
		switch r.Status {
		case "settled":
			if r.Actual == nil {
				return fmt.Errorf(
					"projections settle: %s is marked settled but carries no actual value", p.Key())
			}
			settle = append(settle, sp)
		case "unavailable":
			void = append(void, sp)
		default:
			return fmt.Errorf("projections settle: %s: unknown status %q", p.Key(), r.Status)
		}
	}

	fmt.Printf("SETTLE PROJECTIONS  %d week %d\n", of.Season, of.Week)
	fmt.Printf("  settle   %d\n  void     %d\n  pending  %d\n  already  %d\n",
		len(settle), len(void), pending, already)
	if *dry {
		fmt.Printf("\n-n: nothing written\n")
		return nil
	}
	for _, sp := range settle {
		r := result[key{sp.Projection.Player, sp.Projection.Outcome}]
		if err := betlog.SettleProjection(*logPath, sp.ID, *r.Actual, ""); err != nil {
			return err
		}
	}
	for _, sp := range void {
		// Looked up fresh from the result map, by (player, outcome) -- not from
		// a parallel slice indexed by position. A shared reasons slice appended
		// to in mixed settle/void iteration order, then read back via
		// reasons[len(settle)+i], only lines up when every settle match happens
		// to precede every void match in iteration order -- which log order
		// does not guarantee. (The sibling edgectl beliefs settle has exactly
		// this bug today, at app/cmd/edgectl/beliefs.go:391 -- out of scope to
		// fix here since it is existing, already-shipped code, but this task's
		// own new code must not reproduce it.)
		r := result[key{sp.Projection.Player, sp.Projection.Outcome}]
		reason := r.Reason
		if reason == "" {
			reason = "unmeasurable"
		}
		if err := betlog.VoidProjection(*logPath, sp.ID, reason); err != nil {
			return err
		}
	}
	return nil
}

func projectionsScore(args []string) error {
	fs := flag.NewFlagSet("projections score", flag.ExitOnError)
	logPath := fs.String("log", "beliefs/projections.jsonl", "the projection log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projs, err := betlog.LoadProjections(*logPath)
	if err != nil {
		return err
	}
	byOutcome, err := betlog.ByOutcome(projs)
	if err != nil {
		return err
	}

	outcomes := make([]string, 0, len(byOutcome))
	for o := range byOutcome {
		outcomes = append(outcomes, o)
	}
	sort.Strings(outcomes)

	fmt.Printf("PROJECTIONS SCORE  %s\n\n", *logPath)
	if len(outcomes) == 0 {
		fmt.Printf("  no settled projections yet\n")
		return nil
	}
	for _, o := range outcomes {
		s := byOutcome[o]
		fmt.Printf("  %s  (n=%d)\n", o, s.N)
		if s.N == 0 {
			fmt.Printf("    nothing settled yet\n\n")
			continue
		}
		fmt.Printf("    our MAE      %.1f\n", s.OurMAE)
		fmt.Printf("    market MAE   %.1f\n", s.MarketMAE)
		fmt.Printf("    win rate     %.1f%%  (closer to actual than the market's line)\n", s.WinRate*100)
		fmt.Printf("    our bias     %+.1f  (positive = we project high)\n", s.OurBias)
		fmt.Printf("    market bias  %+.1f\n", s.MarketBias)
		if s.N < 20 {
			fmt.Printf("    SMALL SAMPLE: n=%d is not enough to trust the win rate above\n", s.N)
		}
		fmt.Println()
	}
	return nil
}
```

- [ ] **Step 4: Register the subcommand in `main.go`**

In `app/cmd/edgectl/main.go`, add a case to the switch (after the existing `case "beliefs":` line):

```go
	case "projections":
		err = projectionsCmd(os.Args[2:])
```

Also add a line to `usage()`'s help text (`app/cmd/edgectl/main.go`, near the existing
`edgectl beliefs ingest|settle|list|score` line at `main.go:136`), matching the convention every
other subcommand follows:

```
  edgectl projections settle|score
        Realized values for a week's logged projections (grid median vs. market
        line), and whether the grid or the market tracked reality more closely.
```

- [ ] **Step 5: Run the tests to confirm they pass**

Run: `cd edge/app && go build ./... && go test ./cmd/edgectl/... -run TestProjectionsSettle -v`
Expected: PASS (both)

- [ ] **Step 6: Write and run a failing-then-passing test for `projections score`**

```go
func TestProjectionsScore(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "projections.jsonl")
	past := time.Now().Add(-time.Hour)
	p := betlog.Projection{Season: 2026, Week: 6, Player: "Cooper Kupp",
		Outcome: "receiving_yards", Scenario: "shootout", OurMedian: 21, MarketLine: 25,
		Kickoff: past.Add(time.Minute)}
	id, _ := betlog.RecordProjection(logPath, p, past)
	if err := betlog.SettleProjection(logPath, id, 30, ""); err != nil {
		t.Fatal(err)
	}
	if err := projectionsCmd([]string{"score", "-log", logPath}); err != nil {
		t.Fatal(err)
	}
}
```

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestProjectionsScore -v`
Expected: PASS (this test only checks the command runs without error — the math itself is
already pinned by Task 4's `TestScoreProjectionsMatchesAHandWorkedExample`, so this is an
integration smoke test, not a second math check)

- [ ] **Step 7: Run the full test suite**

Run: `cd edge/app && go test ./...`
Expected: all packages green.

- [ ] **Step 8: Commit**

```bash
cd edge/app
git add cmd/edgectl/projections.go cmd/edgectl/projections_test.go cmd/edgectl/main.go
git commit -m "edgectl: add projections settle and projections score commands"
```

---

## Task 7: `model/analysis/projections_outcomes.py`

**Files:**
- Create: `model/analysis/projections_outcomes.py`

**Interfaces:**
- Consumes: `fit_conditionals.OUTCOMES`, `fit_conditionals.col_sum`, `fit_conditionals.CACHE`
  (existing, `model/analysis/fit_conditionals.py`).
- Produces: a runnable script, output piped to a file consumed by Task 6's `edgectl projections
  settle -outcomes <file>` (the exact JSON shape is defined in Task 6).

- [ ] **Step 1: Write the script**

```python
#!/usr/bin/env python3
"""Realized values for every projection logged for one season/week.

Reads beliefs/projections.jsonl for every (player, outcome) pair projected
for the given season/week, looks up what that player actually did in
stats_player_week_<season>.csv -- the same file player.py's own baseline/
trend lookup reads -- and emits one row per unique (player, outcome) pair.
A projection logged under several scenarios in the same week (the same
player's receiving yards projected under both `shootout` and
`efficient_offense`, say) settles against the one real number either way, so
this computes each pair once rather than once per logged row.

Reuses fit_conditionals.OUTCOMES for the column mapping (yards_field, a
single column or several to sum) so this can never drift from what the grid
itself was fit on, and reuses player.py's own substring-match-with-ambiguity-
refusal rule for matching a logged player name against the CSV's own names,
so a player this script can't uniquely identify is reported as unavailable
rather than guessed at.

Usage:
  python3 model/analysis/projections_outcomes.py --season 2026 --week 6 \
      --projections ../beliefs/projections.jsonl \
      > ../beliefs/2026/week06.projections-outcomes.json
"""
import argparse
import csv
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import fit_conditionals as F


def _load_projections(path, season, week):
    """(player, outcome) pairs needing settlement, from the raw JSONL log.

    Only KindProjection ("projection") entries are read; settle/other kinds
    are skipped. This does not check what is already settled -- the Go
    settle command is what skips already-settled rows, the same division of
    responsibility beliefpack.py's outcomes() has with `edgectl beliefs
    settle` (the Python side always recomputes everything scheduled; the Go
    side is what is idempotent).
    """
    pairs = set()
    p = Path(path)
    if not p.exists():
        return pairs
    for line in p.read_text().splitlines():
        line = line.strip()
        if not line:
            continue
        entry = json.loads(line)
        if entry.get("kind") != "projection":
            continue
        proj = entry.get("projection") or {}
        if proj.get("season") != season or proj.get("week") != week:
            continue
        pairs.add((proj["player"], proj["outcome"]))
    return pairs


def _realized_value(player, outcome_name, season, week):
    """The player's realized stat for this outcome/week, or (None, reason)."""
    outcome = F.OUTCOMES.get(outcome_name)
    if outcome is None:
        return None, f"unknown outcome {outcome_name!r}"

    path = F.CACHE / f"stats_player_week_{season}.csv"
    if not path.exists():
        return None, f"{path} not found -- run `make data`"

    matches = {}
    rows_for_week = []
    for r in csv.DictReader(path.open()):
        if r.get("season_type") != "REG":
            continue
        if r.get("position") not in outcome.positions:
            continue
        name = r.get("player_display_name") or r.get("player_name") or ""
        if player.lower() in name.lower():
            matches[r.get("player_id", "")] = name
        if int(F.num(r.get("week", 0))) == week:
            rows_for_week.append(r)

    if not matches:
        return None, f"no {outcome_name} player matching {player!r} in {season}"
    if len(matches) > 1:
        names = ", ".join(sorted(set(matches.values())))
        return None, f"{player!r} matches {len(matches)} players: {names}"
    pid = next(iter(matches))

    for r in rows_for_week:
        if r.get("player_id", "") == pid and r.get("week") and int(F.num(r["week"])) == week:
            return F.col_sum(r, outcome.yards_field), None

    return None, f"no {outcome_name} row for {player} in week {week} (inactive, bye, or name mismatch)"


def outcomes(projections_path, season, week):
    rows = []
    for player, outcome_name in sorted(_load_projections(projections_path, season, week)):
        value, reason = _realized_value(player, outcome_name, season, week)
        if value is None:
            rows.append({"player": player, "outcome": outcome_name, "status": "unavailable",
                         "actual": None, "reason": reason})
        else:
            rows.append({"player": player, "outcome": outcome_name, "status": "settled",
                         "actual": value, "reason": ""})
    return {"season": season, "week": week, "rows": rows}


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--season", type=int, required=True)
    ap.add_argument("--week", type=int, required=True, help="the week just played")
    ap.add_argument("--projections", required=True, help="path to beliefs/projections.jsonl")
    args = ap.parse_args(argv)

    out = outcomes(args.projections, args.season, args.week)
    print(json.dumps(out, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

- [ ] **Step 2: Verify against real cached data**

This repo has no pytest suite under `model/` (confirmed: `find model -iname "*test*.py"` returns
nothing) — its established verification convention is running a script directly against real
cached data and reading the output, not a test framework. Follow that:

Run (from `edge/`, with a real, already-logged projection for a real settled week — Week 5 is
fully played and its data is already in the refreshed cache from this session's Week 6 prep):

```bash
# First, by hand, create one real test projection entry for a real, known
# Week 5 player/outcome, to exercise the script against real settled data:
python3 -c "
import json
from datetime import datetime, timezone
entry = {
    'kind': 'projection', 'id': 'test-verify-1', 'time': datetime.now(timezone.utc).isoformat(),
    'projection': {
        'season': 2026, 'week': 5, 'player': 'Cooper Kupp', 'outcome': 'receiving_yards',
        'scenario': 'shootout', 'our_median': 17.0, 'market_line': 60.0,
        'baseline': 25.8, 'trend': 0.0007,
        'generated_at': '2026-10-08T12:00:00Z', 'kickoff': '2026-10-11T16:25:00Z',
    },
}
print(json.dumps(entry))
" > /tmp/test_projections.jsonl

python3 model/analysis/projections_outcomes.py --season 2026 --week 5 \
    --projections /tmp/test_projections.jsonl
```

Expected: valid JSON on stdout, with one row for `Cooper Kupp`/`receiving_yards`, `status`
`"settled"`, and an `actual` value — read it and confirm by hand against a known real source (the
SF-SEA Week 5 report, or `stats_player_week_2026.csv` directly, grepping for Kupp's week 5 row) that
the number matches what Kupp actually did.

- [ ] **Step 3: Verify the ambiguous-name and missing-player cases print real reasons, not crash**

```bash
python3 -c "
import json
entry = {
    'kind': 'projection', 'id': 'test-verify-2', 'time': '2026-10-08T12:00:00Z',
    'projection': {
        'season': 2026, 'week': 5, 'player': 'Nobody Realname', 'outcome': 'receiving_yards',
        'scenario': 'shootout', 'our_median': 10, 'market_line': 10,
        'generated_at': '2026-10-08T12:00:00Z', 'kickoff': '2026-10-11T16:25:00Z',
    },
}
print(json.dumps(entry))
" > /tmp/test_projections2.jsonl

python3 model/analysis/projections_outcomes.py --season 2026 --week 5 \
    --projections /tmp/test_projections2.jsonl
```

Expected: valid JSON, one row, `status: "unavailable"`, a real `reason` string naming the player
wasn't found — no traceback, no crash.

- [ ] **Step 4: Commit**

```bash
git add model/analysis/projections_outcomes.py
git commit -m "model: add projections_outcomes.py -- realized values for settling the projection log"
```

---

## Task 8: Wire `-projections` into the URPS report-writing workflow

**Files:**
- Modify: `.claude/skills/urps/SKILL.md` (workflow section — the spec's own stated location for
  this change)

**Interfaces:**
- Consumes: nothing (this is documentation, not code).
- Produces: nothing new in code; makes `-projections` actually get used going forward.

This is the step that makes Tasks 1-7 matter in practice — without it, the new flag exists but
nothing calls it, same as `-log` sat unused all season per the spec's own Problem section.

- [ ] **Step 1: Read the current workflow section**

Run: read `.claude/skills/urps/SKILL.md`'s step 3 ("Price it") in full — this is where `edgectl
scenario` is already documented as the per-player pricing command.

- [ ] **Step 2: Add the `-projections` requirement**

Edit step 3's `edgectl scenario` example command to include `-projections`, `-season`, `-week`,
and `-kickoff` alongside the existing flags, and add a sentence stating this is **non-optional**:
every `edgectl scenario` call made while pricing a report's players passes `-projections`,
whether or not the result becomes a named recommendation — this is the mechanism that makes the
Week 6 SEA-vs-DEN-style "our median vs. the market's line" comparison possible after the fact,
for every player priced, not just the ones that got bet.

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/urps/SKILL.md
git commit -m "urps skill: require -projections on every scenario pricing call"
```

---

## Final verification

- [ ] **Run the full Go test suite one more time from a clean state**

Run: `cd edge/app && go build ./... && go test ./...`
Expected: every package green, zero regressions from the pre-existing baseline.

- [ ] **Confirm the spec's own testing section is covered**

Cross-check `docs/superpowers/specs/2026-10-14-projection-log-design.md`'s Testing section
against what was built: `Projection.Validate`/`RecordProjection` (Task 2), `LoadProjections` (Task 2), the
new score function pinned against a worked example (Task 4), `edgectl scenario -projections`
writes a well-formed `Projection` (Task 5), `edgectl projections settle` ingests realized
outcomes including the pending/void case (Task 6), `edgectl projections score` matches the pinned
worked example (Task 6, reusing Task 4's math), the Python outcomes script reads real cached data
correctly (Task 7). All covered.

- [ ] **End-to-end smoke test against real data**

Using the real, already-ingested Week 6 SEA@DEN pricing from this session (Cooper Kupp receiving
yards, grid median 21 vs. DK's 21 de-facto line — already computed and written up in this week's
report, just never logged): run `edgectl scenario` with `-projections` for that real case,
confirm it logs, then (once Week 6 is actually played) run `projections_outcomes.py` and
`edgectl projections settle`/`score` against the real result. This step cannot complete until
Week 6's games are played — note it as the first real end-to-end validation, to run after this
week's Thursday/Sunday/Monday slate finishes.

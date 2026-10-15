# SGP Correlation Pricing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `edgectl parlay` price same-game (correlated) legs under an operator-supplied correlation factor instead of refusing them outright, report a same-game combo's **floor** — the minimum price (in American odds) at which the combo still has edge under our own belief and correlation assumption, so the operator can shop books for a price at or longer than that floor — and report the breakeven correlation a book's real SGP price requires.

**Architecture:** A new, pure-math file in `internal/wager` (`correlation.go`) implements the Fréchet–Hoeffding bound interpolation and its inverse, plus pairwise chaining for 3+ legs — no dependency on the CLI layer, testable in isolation with hand-worked examples. The CLI layer (`cmd/edgectl/ladder.go`, which already hosts `parlayCmd`/`guardSameGame`/`printParlay`) gains a new `-correlation` flag, parses it into per-game chains, lifts `guardSameGame`'s refusal only for the named games, collapses each correlated group into one synthetic "leg" (a blended price + probability) before handing the leg list to the existing, untouched `wager.RoundRobin`/`wager.BuildTicket` pipeline. That collapsed group's resulting single-combo price is, by construction, the combo's **floor** — the minimum price that still breaks even under the operator's own leg probabilities and correlation belief — and `printParlay` prints it labeled as such, unconditionally, whenever a correlated group exists, so the operator can go shop that exact combo at different books and compare their quote against the floor without needing to supply one in advance. `-book`, when supplied, additionally reports the breakeven correlation a book's *specific* real price requires, as before.

**Tech Stack:** Go (`edge/app`, package `internal/wager`, `cmd/edgectl`).

**Spec:** `docs/superpowers/specs/2026-10-14-sgp-correlation-pricing-design.md`

## Global Constraints

- No N-way joint distribution modeling — the Fréchet bound is exact for exactly 2 events; 3+ legs
  use pairwise chaining, an explicitly-flagged approximation, never silently presented as exact.
- No automatic correlation estimation — `ρ` is always operator-supplied, never inferred.
- `guardSameGame`'s default behavior is unchanged — same-game legs are refused unless their game
  tag has a matching `-correlation` entry.
- `edgectl parlay` without `-correlation` must be byte-identical in behavior to today — this is a
  strictly additive flag.

---

## Discovery notes (read before starting — exact state of the real code as of this plan)

- `internal/wager/parlay.go` (237 lines) is pure math: `Combo`, `CombineParlay`, `combineIdx`,
  `combinations`, `RoundRobin`, `Ticket`, `BuildTicket`. None of this file is modified by this
  plan — the new correlation math lives in a sibling file, `internal/wager/correlation.go`, and
  the CLI composes a *reduced* leg list before calling into this file's existing functions
  unchanged.
- `cmd/edgectl/ladder.go` hosts the CLI layer: `parlayLeg` struct (`:209`), `parlayCmd` (`:216`),
  `parseSizes`/`parseLegs` (`:262`, `:279`), `guardSameGame` (`:307`), `printParlay` (`:323`),
  `comboName` (`:381`). Also two small, directly reusable helpers already in this file:
  `parseAmerican(tok string) (wager.American, error)` (`:14`) and
  `parseFloatList(s string) ([]float64, error)` (`:24`, reads `"0.6,0.5,0.31"` into a `[]float64`
  — reused as-is for parsing one game's comma-separated `ρ` list after splitting the
  `-correlation` value on `;` and `:`).
- `internal/wager/belief.go`'s `BeliefRequirement` struct and `RequiredScenarioProb` function
  (`:36`, `:65`) are the house pattern for "what would you have to believe" — the new
  `RequiredCorrelation` function in this plan mirrors its shape (a small result struct, a
  breakeven-style solve) without being a generic reuse, since the underlying math differs.
- `wager.American` has `.Decimal()`, `.ImpliedRaw()`, `.Breakeven()` (`:88`, literally
  `return a.ImpliedRaw()` — same number, different name for context), and the package-level
  `AmericanFromDecimal(dec float64) (American, error)` (`:95`). `internal/wager/ev.go` already has
  `validProb(p float64) error` (`:27`), reused as-is for validating `ρ`'s own range check isn't
  quite `validProb` (ρ ranges `[-1,1]`, not `[0,1]`) but the probabilities fed into the Fréchet
  math (`p1`, `p2`, …) are validated with it.
- **Hand-worked reference values** (independently verified before writing this plan, used to pin
  every test below): `p1=0.6, p2=0.5`.
  - `ρ=0`: `p_joint = p1×p2 = 0.30` (today's existing independent-product result, exactly).
  - `ρ=0.5`: `p_indep=0.30`, `min(p1,p2)=0.50`, `p_joint = 0.30 + 0.5×(0.50−0.30) = 0.40`.
  - `ρ=−0.4`: `p_indep=0.30`, `max(0, p1+p2−1)=max(0,0.10)=0.10`,
    `p_joint = 0.30 + 0.4×(0.10−0.30) = 0.30 − 0.08 = 0.22`.
  - Inverse round-trips: solving `ρ*` for `p*=0.40` (positive branch, since `0.40 ≥ p_indep`)
    returns `ρ*=0.50`; solving for `p*=0.22` (negative branch) returns `ρ*=−0.40`.
  - 3-leg chain: `p1=0.6, p2=0.5, p3=0.4`, `rhos=[0.5, −0.2]`. Step 1: `blend(p1,p2,0.5)=0.40`
    (from above). Step 2: `blend(0.40, p3=0.40, −0.2)`: `p_indep=0.16`,
    `max(0,0.40+0.40−1)=max(0,−0.20)=0`, `p_joint = 0.16 + 0.2×(0 − 0.16) = 0.16 − 0.032 = 0.128`.
    Final chained result: **0.128**.

---

## Task 1: `FrechetBlend` — the forward math

**Files:**
- Create: `internal/wager/correlation.go`
- Test: `internal/wager/correlation_test.go`

**Interfaces:**
- Consumes: `validProb` (`internal/wager/ev.go:27`).
- Produces: `FrechetBlend(p1, p2, rho float64) (float64, error)` — what Task 2 (inverse), Task 3
  (chaining), and Task 6 (CLI integration) all build on.

- [ ] **Step 1: Write the failing test, pinning the three hand-worked values above**

```go
package wager

import (
	"math"
	"testing"
)

func TestFrechetBlend(t *testing.T) {
	cases := []struct {
		name       string
		p1, p2, rho float64
		want       float64
	}{
		{"independence (rho=0) reproduces the product", 0.6, 0.5, 0.0, 0.30},
		{"positive correlation interpolates toward the comonotonic ceiling", 0.6, 0.5, 0.5, 0.40},
		{"negative correlation interpolates toward the countermonotonic floor", 0.6, 0.5, -0.4, 0.22},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FrechetBlend(c.p1, c.p2, c.rho)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("FrechetBlend(%v, %v, %v) = %v, want %v", c.p1, c.p2, c.rho, got, c.want)
			}
		})
	}
}

func TestFrechetBlendRejectsOutOfRangeRho(t *testing.T) {
	if _, err := FrechetBlend(0.5, 0.5, 1.5); err == nil {
		t.Error("rho > 1 should be rejected")
	}
	if _, err := FrechetBlend(0.5, 0.5, -1.5); err == nil {
		t.Error("rho < -1 should be rejected")
	}
}

func TestFrechetBlendRejectsOutOfRangeProbabilities(t *testing.T) {
	if _, err := FrechetBlend(1.5, 0.5, 0.0); err == nil {
		t.Error("p1 > 1 should be rejected")
	}
	if _, err := FrechetBlend(0.5, -0.1, 0.0); err == nil {
		t.Error("p2 < 0 should be rejected")
	}
}

// At the extremes rho=+1 and rho=-1, the bound itself is the answer -- no
// interpolation left.
func TestFrechetBlendAtTheExtremes(t *testing.T) {
	got, err := FrechetBlend(0.6, 0.5, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.5) > 1e-9 { // min(0.6, 0.5)
		t.Errorf("rho=1 should equal min(p1,p2)=0.5, got %v", got)
	}
	got, err = FrechetBlend(0.6, 0.5, -1.0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.1) > 1e-9 { // max(0, 0.6+0.5-1) = 0.1
		t.Errorf("rho=-1 should equal max(0,p1+p2-1)=0.1, got %v", got)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./internal/wager/... -run TestFrechetBlend -v`
Expected: FAIL (compile error — `FrechetBlend` does not exist yet)

- [ ] **Step 3: Implement**

Create `internal/wager/correlation.go`:

```go
// Package wager, this file: pricing two or more CORRELATED (same-game) legs
// under an operator-supplied correlation factor, instead of refusing them the
// way parlay.go's independence contract requires.
//
// The joint probability of two events is mathematically bounded:
//
//	max(0, p1 + p2 - 1)  <=  P(both)  <=  min(p1, p2)
//
// -- the countermonotonic floor (the two never happen together beyond what
// overlap is forced) and the comonotonic ceiling (the rarer leg's own
// probability, if the two always happen together). The independent case,
// p1*p2, sits somewhere inside that range for any real positively- or
// negatively-associated pair. rho in [-1,1] is a correlation factor that
// linearly interpolates from independence toward whichever bound its sign
// names. This is exact and bounded by construction: no clamping anywhere,
// because the formula cannot produce an invalid probability for any rho in
// that range.
package wager

import "fmt"

// FrechetBlend computes the joint probability of two events under a
// correlation factor rho, via Frechet-Hoeffding bound interpolation.
//
// rho=0 reproduces p1*p2 exactly (today's existing independent-product
// result). rho=1 means the two events always happen together (P(both) =
// min(p1,p2)). rho=-1 means they happen together only as much as the floor
// forces (P(both) = max(0, p1+p2-1)).
func FrechetBlend(p1, p2, rho float64) (float64, error) {
	if err := validProb(p1); err != nil {
		return 0, fmt.Errorf("wager: p1: %w", err)
	}
	if err := validProb(p2); err != nil {
		return 0, fmt.Errorf("wager: p2: %w", err)
	}
	if rho < -1 || rho > 1 {
		return 0, fmt.Errorf("wager: rho = %v is out of range [-1,1]", rho)
	}
	indep := p1 * p2
	if rho >= 0 {
		upper := p1
		if p2 < upper {
			upper = p2
		}
		return indep + rho*(upper-indep), nil
	}
	lower := p1 + p2 - 1
	if lower < 0 {
		lower = 0
	}
	return indep + (-rho)*(lower-indep), nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/wager/... -run TestFrechetBlend -v`
Expected: PASS (all four tests)

- [ ] **Step 5: Run the full `internal/wager` suite to confirm no regression**

Run: `cd edge/app && go test ./internal/wager/...`
Expected: all existing tests (parlay, odds, ev, belief) still green — this task adds a new file
and touches nothing else.

- [ ] **Step 6: Commit**

```bash
cd edge/app
git add internal/wager/correlation.go internal/wager/correlation_test.go
git commit -m "wager: add FrechetBlend, the forward correlation math for same-game legs"
```

---

## Task 2: `RequiredCorrelation` — the inverse (breakeven ρ\*)

**Files:**
- Modify: `internal/wager/correlation.go`
- Test: `internal/wager/correlation_test.go`

**Interfaces:**
- Consumes: `FrechetBlend` (Task 1), for the round-trip test only (the implementation itself does
  not call `FrechetBlend` — it inverts the same formula directly).
- Produces: `CorrelationRequirement` struct, `RequiredCorrelation(p1, p2, pTarget float64)
  (CorrelationRequirement, error)` — what Task 6 (CLI `-book` integration) calls.

- [ ] **Step 1: Write the failing test**

```go
func TestRequiredCorrelationRoundTrips(t *testing.T) {
	// Forward: FrechetBlend(0.6, 0.5, 0.5) = 0.40 (pinned in Task 1). The
	// inverse, solving for the rho that produces 0.40, must return 0.5.
	req, err := RequiredCorrelation(0.6, 0.5, 0.40)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required-0.5) > 1e-9 {
		t.Errorf("Required = %v, want 0.5", req.Required)
	}

	// Negative branch: FrechetBlend(0.6, 0.5, -0.4) = 0.22 (also pinned in
	// Task 1). The inverse for 0.22 must return -0.4.
	req, err = RequiredCorrelation(0.6, 0.5, 0.22)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required-(-0.4)) > 1e-9 {
		t.Errorf("Required = %v, want -0.4", req.Required)
	}
}

func TestRequiredCorrelationRejectsATargetOutsideTheBounds(t *testing.T) {
	// min(p1,p2) = 0.5 when p1=0.6, p2=0.5 -- a target above that is not
	// reachable by any rho in [-1,1] and must be refused, not silently
	// report a rho outside [-1,1].
	if _, err := RequiredCorrelation(0.6, 0.5, 0.9); err == nil {
		t.Error("a target above min(p1,p2) is unreachable and should be refused")
	}
	// max(0, p1+p2-1) = 0.1 -- a target below that is equally unreachable.
	if _, err := RequiredCorrelation(0.6, 0.5, 0.05); err == nil {
		t.Error("a target below max(0,p1+p2-1) is unreachable and should be refused")
	}
}

func TestRequiredCorrelationAtIndependence(t *testing.T) {
	// A target exactly equal to the independent product requires rho=0.
	req, err := RequiredCorrelation(0.6, 0.5, 0.30)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(req.Required) > 1e-9 {
		t.Errorf("Required = %v, want 0", req.Required)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./internal/wager/... -run TestRequiredCorrelation -v`
Expected: FAIL (compile error)

- [ ] **Step 3: Implement**

Append to `internal/wager/correlation.go`:

```go
// CorrelationRequirement is the inverse of FrechetBlend: what rho a target
// joint probability (typically a book's own raw implied price) requires you
// to believe, mirroring RequiredScenarioProb's "what would you have to
// believe" shape (belief.go) for a different quantity.
type CorrelationRequirement struct {
	P1, P2   float64
	Target   float64
	Required float64 // rho*, in [-1,1]
}

// RequiredCorrelation solves for the rho that makes FrechetBlend(p1, p2, rho)
// equal pTarget. Refuses a target outside [max(0,p1+p2-1), min(p1,p2)] --
// that range is what any rho in [-1,1] can ever produce, so a target outside
// it is not a correlation question at all; the two input probabilities
// themselves disagree with the target.
func RequiredCorrelation(p1, p2, pTarget float64) (CorrelationRequirement, error) {
	if err := validProb(p1); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: p1: %w", err)
	}
	if err := validProb(p2); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: p2: %w", err)
	}
	if err := validProb(pTarget); err != nil {
		return CorrelationRequirement{}, fmt.Errorf("wager: target: %w", err)
	}
	indep := p1 * p2
	upper := p1
	if p2 < upper {
		upper = p2
	}
	lower := p1 + p2 - 1
	if lower < 0 {
		lower = 0
	}
	if pTarget > upper+1e-9 || pTarget < lower-1e-9 {
		return CorrelationRequirement{}, fmt.Errorf(
			"wager: target %.4f is outside [%.4f, %.4f], the range any correlation factor "+
				"in [-1,1] can produce for p1=%.4f, p2=%.4f -- no rho explains this gap",
			pTarget, lower, upper, p1, p2)
	}

	var rho float64
	switch {
	case pTarget >= indep && upper != indep:
		rho = (pTarget - indep) / (upper - indep)
	case pTarget < indep && indep != lower:
		rho = -(indep - pTarget) / (indep - lower)
	} // pTarget == indep, or upper/lower degenerately equal indep: rho stays 0

	return CorrelationRequirement{P1: p1, P2: p2, Target: pTarget, Required: rho}, nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/wager/... -run TestRequiredCorrelation -v`
Expected: PASS (all three)

- [ ] **Step 5: Commit**

```bash
cd edge/app
git add internal/wager/correlation.go internal/wager/correlation_test.go
git commit -m "wager: add RequiredCorrelation, the breakeven rho a target price requires"
```

---

## Task 3: `ChainFrechet` — pairwise chaining for 3+ legs

**Files:**
- Modify: `internal/wager/correlation.go`
- Test: `internal/wager/correlation_test.go`

**Interfaces:**
- Consumes: `FrechetBlend` (Task 1).
- Produces: `ChainFrechet(probs []float64, rhos []float64) (float64, error)` — what Task 6 (CLI
  integration) calls for every correlated group, including the 2-leg case (where it is identical
  to one direct `FrechetBlend` call).

- [ ] **Step 1: Write the failing test, pinning the 3-leg worked example**

```go
func TestChainFrechetThreeLegs(t *testing.T) {
	// p1=0.6, p2=0.5, p3=0.4, rhos=[0.5, -0.2] -> 0.128 (hand-worked, see this
	// plan's Discovery Notes). Step 1 blends p1,p2 under rho[0]; step 2
	// blends that result against p3 under rho[1].
	got, err := ChainFrechet([]float64{0.6, 0.5, 0.4}, []float64{0.5, -0.2})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.128) > 1e-9 {
		t.Errorf("ChainFrechet = %v, want 0.128", got)
	}
}

func TestChainFrechetMatchesFrechetBlendForTwoLegs(t *testing.T) {
	chained, err := ChainFrechet([]float64{0.6, 0.5}, []float64{0.5})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := FrechetBlend(0.6, 0.5, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(chained-direct) > 1e-9 {
		t.Errorf("ChainFrechet(2 legs) = %v, FrechetBlend = %v -- must match exactly", chained, direct)
	}
}

func TestChainFrechetOrderMatters(t *testing.T) {
	// Same three probabilities and the same two rhos, but blending p1 with p3
	// first (instead of p1 with p2) must produce a DIFFERENT result -- this
	// is the real, acknowledged limitation of pairwise chaining (it is not a
	// true N-way joint, and the plan does not claim it is).
	probs := []float64{0.75, 0.5, 0.2}
	rhos := []float64{0.6, -0.5}

	orderA, err := ChainFrechet([]float64{probs[0], probs[1], probs[2]}, rhos)
	if err != nil {
		t.Fatal(err)
	}
	orderB, err := ChainFrechet([]float64{probs[0], probs[2], probs[1]}, rhos)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(orderA-orderB) < 1e-9 {
		t.Error("chaining in a different leg order should generally produce a different result " +
			"(if this ever legitimately coincides, pick different probs/rhos for this test)")
	}
}

func TestChainFrechetRequiresOneFewerRhoThanLegs(t *testing.T) {
	if _, err := ChainFrechet([]float64{0.6, 0.5, 0.4}, []float64{0.5}); err == nil {
		t.Error("3 legs need 2 rhos; 1 should be refused")
	}
	if _, err := ChainFrechet([]float64{0.6, 0.5}, nil); err == nil {
		t.Error("2 legs need 1 rho; 0 should be refused")
	}
}

func TestChainFrechetRequiresAtLeastTwoLegs(t *testing.T) {
	if _, err := ChainFrechet([]float64{0.6}, nil); err == nil {
		t.Error("a single leg has nothing to correlate against")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./internal/wager/... -run TestChainFrechet -v`
Expected: FAIL (compile error)

- [ ] **Step 3: Implement**

Append to `internal/wager/correlation.go`:

```go
// ChainFrechet blends N probabilities pairwise, in listing order, one rho
// per chain step (len(rhos) must be len(probs)-1): blend probs[0] and
// probs[1] under rhos[0], then blend that result against probs[2] under
// rhos[1], and so on.
//
// For N=2 this is identical to one FrechetBlend call. For N>=3 it is a real,
// explicitly-acknowledged approximation: treating a chained intermediate
// result as a single Bernoulli probability for the next blend has no
// guarantee of landing within the TRUE N-way Frechet bounds, and the error
// is not characterized. The order the legs are chained in matters -- this is
// not a defect to fix, it is the nature of the approximation, which is why
// the CLI layer (ladder.go) prints a caveat on every 3+-leg correlated combo
// rather than presenting this as exact.
func ChainFrechet(probs []float64, rhos []float64) (float64, error) {
	if len(probs) < 2 {
		return 0, fmt.Errorf("wager: ChainFrechet needs at least 2 probabilities, got %d", len(probs))
	}
	if len(rhos) != len(probs)-1 {
		return 0, fmt.Errorf(
			"wager: %d probabilities need %d rho values (one per chain step), got %d",
			len(probs), len(probs)-1, len(rhos))
	}
	running := probs[0]
	for i := 1; i < len(probs); i++ {
		blended, err := FrechetBlend(running, probs[i], rhos[i-1])
		if err != nil {
			return 0, fmt.Errorf("chain step %d (blending running result against leg %d): %w", i, i+1, err)
		}
		running = blended
	}
	return running, nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./internal/wager/... -run TestChainFrechet -v`
Expected: PASS (all five)

- [ ] **Step 5: Run the full `internal/wager` suite**

Run: `cd edge/app && go test ./internal/wager/...`
Expected: all green — Tasks 1-3 are additive, nothing else in this package changed.

- [ ] **Step 6: Commit**

```bash
cd edge/app
git add internal/wager/correlation.go internal/wager/correlation_test.go
git commit -m "wager: add ChainFrechet for 3+ same-game legs (pairwise approximation)"
```

---

## Task 4: Parse `-correlation`'s nested flag format

**Files:**
- Modify: `cmd/edgectl/ladder.go`
- Test: `cmd/edgectl/ladder_test.go` (existing file — check; if it doesn't exist, create it
  following the `package main` convention every other `cmd/edgectl` test file uses, e.g.
  `board_props_report_test.go`)

**Interfaces:**
- Consumes: `parseFloatList` (`ladder.go:24`, existing, unmodified).
- Produces: `correlationSpec` struct, `parseCorrelation(s string) ([]correlationSpec, error)` —
  what Task 5 (`guardSameGame` extension) and Task 6 (`parlayCmd` integration) both consume.

- [ ] **Step 1: Write the failing test**

```go
func TestParseCorrelation(t *testing.T) {
	specs, err := parseCorrelation("mia@lv:0.3;was@phi:0.5,0.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d: %+v", len(specs), specs)
	}
	if specs[0].game != "mia@lv" || len(specs[0].rhos) != 1 || specs[0].rhos[0] != 0.3 {
		t.Errorf("spec 0 = %+v, want game mia@lv, rhos [0.3]", specs[0])
	}
	if specs[1].game != "was@phi" || len(specs[1].rhos) != 2 ||
		specs[1].rhos[0] != 0.5 || specs[1].rhos[1] != 0.2 {
		t.Errorf("spec 1 = %+v, want game was@phi, rhos [0.5, 0.2]", specs[1])
	}
}

func TestParseCorrelationEmptyStringIsNoSpecsNotAnError(t *testing.T) {
	specs, err := parseCorrelation("")
	if err != nil {
		t.Fatal(err)
	}
	if specs != nil {
		t.Errorf("empty -correlation should produce no specs, got %+v", specs)
	}
}

func TestParseCorrelationRejectsAMalformedEntry(t *testing.T) {
	if _, err := parseCorrelation("noColonHere"); err == nil {
		t.Error("an entry with no game:rhos separator should be rejected")
	}
	if _, err := parseCorrelation("mia@lv:not-a-number"); err == nil {
		t.Error("a non-numeric rho should be rejected")
	}
	if _, err := parseCorrelation(":0.5"); err == nil {
		t.Error("an empty game tag should be rejected")
	}
}

func TestParseCorrelationRejectsARhoOutsideRange(t *testing.T) {
	if _, err := parseCorrelation("mia@lv:1.5"); err == nil {
		t.Error("a rho above 1 should be rejected at parse time, not deferred to FrechetBlend")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestParseCorrelation -v`
Expected: FAIL (compile error — `correlationSpec`/`parseCorrelation` don't exist)

- [ ] **Step 3: Implement**

Add to `cmd/edgectl/ladder.go`, near `parseLegs` (around line 279) — same section of the file:

```go
// correlationSpec is one game's correlation chain: the rhos to apply, in
// leg-listing order, one per chain step (N-1 values for N same-game legs).
type correlationSpec struct {
	game string
	rhos []float64
}

// parseCorrelation reads "-correlation"'s nested format: semicolon-separated
// per-game entries, each "game:rho[,rho,...]". Empty string is "no
// correlation specs", not an error -- -correlation is entirely optional.
func parseCorrelation(s string) ([]correlationSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var specs []correlationSpec
	for _, entry := range strings.Split(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		fields := strings.SplitN(entry, ":", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("-correlation entry %q: want game:rho[,rho,...]", entry)
		}
		game := strings.TrimSpace(fields[0])
		if game == "" {
			return nil, fmt.Errorf("-correlation entry %q: empty game tag", entry)
		}
		rhos, err := parseFloatList(fields[1])
		if err != nil {
			return nil, fmt.Errorf("-correlation entry %q: %w", entry, err)
		}
		for _, r := range rhos {
			if r < -1 || r > 1 {
				return nil, fmt.Errorf("-correlation entry %q: rho %v is out of range [-1,1]", entry, r)
			}
		}
		specs = append(specs, correlationSpec{game: game, rhos: rhos})
	}
	return specs, nil
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestParseCorrelation -v`
Expected: PASS (all four)

- [ ] **Step 5: Commit**

```bash
cd edge/app
git add cmd/edgectl/ladder.go cmd/edgectl/ladder_test.go
git commit -m "edgectl: parse the -correlation flag's nested game:rho[,rho] format"
```

---

## Task 5: Lift `guardSameGame`'s refusal for covered games only

**Files:**
- Modify: `cmd/edgectl/ladder.go`
- Test: `cmd/edgectl/ladder_test.go`

**Interfaces:**
- Consumes: `parlayLeg` (existing), `correlationSpec` (Task 4).
- Produces: `guardSameGame(legs []parlayLeg, covered map[string]bool) error` (signature change —
  adds a second parameter; the one existing call site, in `parlayCmd`, is updated in this task) —
  what Task 6 calls with the set of game tags named in `-correlation`.

- [ ] **Step 1: Write the failing test**

```go
func TestGuardSameGameRefusesUncoveredPairs(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	if err := guardSameGame(legs, nil); err == nil {
		t.Error("two same-game legs with no correlation coverage should still be refused")
	}
}

func TestGuardSameGameAllowsCoveredPairs(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	covered := map[string]bool{"mia@lv": true}
	if err := guardSameGame(legs, covered); err != nil {
		t.Errorf("a covered game's same-game legs should be allowed, got: %v", err)
	}
}

func TestGuardSameGameStillRefusesOtherUncoveredGames(t *testing.T) {
	// mia@lv is covered; was@phi is not. A same-game pair in was@phi must
	// still be refused even though some other game in the same call is
	// covered.
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: -120, game: "was@phi", label: "C"},
		{price: +110, game: "was@phi", label: "D"},
	}
	covered := map[string]bool{"mia@lv": true}
	if err := guardSameGame(legs, covered); err == nil {
		t.Error("was@phi has no correlation coverage and should still be refused")
	}
}

func TestGuardSameGameStillRequiresAGameTag(t *testing.T) {
	legs := []parlayLeg{{price: -110, game: "", label: "A"}}
	if err := guardSameGame(legs, nil); err == nil {
		t.Error("a leg with no game tag at all should still be refused, covered or not")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestGuardSameGame -v`
Expected: FAIL (compile error — current signature takes one argument)

- [ ] **Step 3: Update `guardSameGame`'s signature and body**

In `cmd/edgectl/ladder.go`, replace the existing function (around line 304-321):

```go
// guardSameGame is the correlation firewall: parlay math assumes independent
// legs, which holds only across different games. Two legs sharing a game tag
// are correlated and must be built as an SGP at the book -- UNLESS that
// game's tag appears in covered (from -correlation), in which case the
// caller has supplied a correlation factor and intends to price it, not
// refuse it. covered may be nil, which behaves exactly as before this flag
// existed: every same-game pair is refused.
func guardSameGame(legs []parlayLeg, covered map[string]bool) error {
	seen := map[string]int{}
	for i, l := range legs {
		if l.game == "" {
			return fmt.Errorf("leg %d has no game tag; every leg needs price:game[:label] so same-game (correlated) legs can be rejected", i+1)
		}
		if covered[l.game] {
			continue
		}
		if j, ok := seen[l.game]; ok {
			return fmt.Errorf(
				"legs %d and %d are both in game %q — they are correlated (one game state), not independent. Build that as an SGP at the book and price the ONE combined number, or supply -correlation %q:<rho> to price it here",
				j+1, i+1, l.game, l.game)
		}
		seen[l.game] = i
	}
	return nil
}
```

Note the refined error message: it now mentions `-correlation` as the alternative to building the
SGP at the book, since that option exists now. The seen-map dedup/tag-required checks are
otherwise unchanged from the original.

- [ ] **Step 4: Update the one existing call site**

In `parlayCmd` (`ladder.go:233` in the pre-this-plan file), change:

```go
	if err := guardSameGame(legs); err != nil {
```

to:

```go
	if err := guardSameGame(legs, nil); err != nil {
```

This keeps `parlayCmd`'s behavior **exactly** as before for this task — `nil` covered means
nothing is lifted yet. Task 6 is what actually threads real coverage through from a parsed
`-correlation` flag; this task only prepares the function signature for that, and must not change
`parlayCmd`'s own behavior yet (confirmed by Step 5's regression check).

- [ ] **Step 5: Run the tests to confirm they pass, and confirm no regression**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestGuardSameGame -v`
Expected: PASS (all four)

Run: `cd edge/app && go test ./cmd/edgectl/...`
Expected: all existing tests pass unmodified — `parlayCmd` with no `-correlation` flag still
refuses every same-game pair exactly as before, since Step 4 passes `nil`.

- [ ] **Step 6: Commit**

```bash
cd edge/app
git add cmd/edgectl/ladder.go cmd/edgectl/ladder_test.go
git commit -m "edgectl: guardSameGame accepts covered game tags, lifting the refusal selectively"
```

---

## Task 6: `parlayCmd` integration — collapse correlated groups, price them, wire `-book`

**Files:**
- Modify: `cmd/edgectl/ladder.go`
- Test: `cmd/edgectl/ladder_test.go`

**Interfaces:**
- Consumes: `parseCorrelation` (Task 4), `guardSameGame` (Task 5), `wager.ChainFrechet`,
  `wager.RequiredCorrelation` (Task 3, Task 2), `wager.RoundRobin`, `wager.BuildTicket`,
  `wager.AmericanFromDecimal` (all pre-existing, unmodified).
- Produces: a `collapsedLeg` type and `collapseLegs` function, and `parlayCmd`'s behavior when
  `-correlation` is supplied — what Task 7 (`printParlay` updates) renders.

This is the task where everything composes. Read it fully before starting; the steps build on
each other more than in previous tasks.

- [ ] **Step 1: Write the failing test for the simplest real case — a pure 2-leg SGP, no other legs**

```go
func TestParlayCmdCorrelationTwoLegSGP(t *testing.T) {
	// Both legs in the same game, covered by -correlation. With no other
	// legs, this collapses to exactly one combo, priced via FrechetBlend
	// instead of straight multiplication.
	//
	// Leg prices -110 (raw implied ~52.38%) and +150 (raw implied 40.00%).
	// p_indep ~= 0.2095. rho=0.3 should price ABOVE that independent
	// product (positive correlation raises the joint probability), so the
	// resulting combo's implied probability must exceed the naive product.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd: %v", err)
	}
}

func TestCollapseLegsTwoLegGroup(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
	}
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3}}}
	collapsed, err := collapseLegs(legs, specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 1 {
		t.Fatalf("want 1 collapsed slot (both legs merge), got %d: %+v", len(collapsed), collapsed)
	}
	if len(collapsed[0].members) != 2 {
		t.Errorf("collapsed slot should carry both original legs, got %d members", len(collapsed[0].members))
	}
	if collapsed[0].prob == nil {
		t.Fatal("a correlated slot must always carry a derived probability")
	}
	pA, _ := wager.American(-110).ImpliedRaw()
	pB, _ := wager.American(150).ImpliedRaw()
	wantProb, err := wager.FrechetBlend(pA, pB, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(*collapsed[0].prob-wantProb) > 1e-9 {
		t.Errorf("collapsed prob = %v, want %v (FrechetBlend of the two legs' raw implied)", *collapsed[0].prob, wantProb)
	}
	if collapsed[0].caveat {
		t.Error("a 2-leg group is exact, not an approximation -- caveat should be false")
	}
}

func TestCollapseLegsLeavesIndependentLegsAlone(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: -120, game: "was@phi", label: "B"}, // different game, no correlation spec
	}
	collapsed, err := collapseLegs(legs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 2 {
		t.Fatalf("want 2 collapsed slots (no grouping happened), got %d", len(collapsed))
	}
	for _, c := range collapsed {
		if len(c.members) != 1 {
			t.Errorf("an independent leg's slot should carry exactly 1 member, got %d", len(c.members))
		}
		if c.prob != nil {
			t.Error("an independent leg with no -p override should carry no derived probability " +
				"(nil means: use raw implied, the existing default)")
		}
	}
}

func TestCollapseLegsThreeLegGroupSetsCaveat(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: +200, game: "mia@lv", label: "C"},
	}
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3, 0.2}}}
	collapsed, err := collapseLegs(legs, specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collapsed) != 1 || len(collapsed[0].members) != 3 {
		t.Fatalf("want 1 slot with 3 members, got %+v", collapsed)
	}
	if !collapsed[0].caveat {
		t.Error("a 3-leg chained group is an approximation and must set caveat=true")
	}
}

func TestCollapseLegsRejectsAWrongRhoCountForAGroup(t *testing.T) {
	legs := []parlayLeg{
		{price: -110, game: "mia@lv", label: "A"},
		{price: +150, game: "mia@lv", label: "B"},
		{price: +200, game: "mia@lv", label: "C"},
	}
	// 3 legs need 2 rhos; only 1 given.
	specs := []correlationSpec{{game: "mia@lv", rhos: []float64{0.3}}}
	if _, err := collapseLegs(legs, specs, nil); err == nil {
		t.Error("a 3-leg group with only 1 rho should be refused, not silently truncated or padded")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run "TestParlayCmdCorrelationTwoLegSGP|TestCollapseLegs" -v`
Expected: FAIL (compile error — `collapsedLeg`/`collapseLegs` don't exist, `parlayCmd` has no
`-correlation` flag yet)

- [ ] **Step 3: Add the `collapsedLeg` type and `collapseLegs` function**

Add to `cmd/edgectl/ladder.go`, after `parseCorrelation` (Task 4):

```go
// collapsedLeg is either a single independent leg or a correlated same-game
// group already blended into one fair price -- the unit parlayCmd actually
// feeds into wager.RoundRobin/wager.BuildTicket, which only ever see
// independent probabilities (correlation is resolved BEFORE this point, not
// inside that pure-math layer).
type collapsedLeg struct {
	price   wager.American // the slot's own price: the leg's own price if independent, or the
	                        // group's blended fair price (via AmericanFromDecimal) if correlated
	prob    *float64        // the slot's own probability, if one was derived or supplied; nil
	                        // means "use this price's own raw implied," the existing default
	members []parlayLeg     // the original leg(s) this slot represents (len 1 = independent)
	caveat  bool            // true when this slot came from chaining 3+ legs (an approximation)
}

// collapseLegs groups legs sharing a game named in specs into one
// collapsedLeg each (blended via wager.ChainFrechet), leaving every other
// leg as its own independent slot. probs, if non-nil, is one operator-
// supplied true probability per ORIGINAL leg (the existing -p convention) --
// used as that leg's own probability inside a correlated group's chain
// instead of its raw implied, exactly mirroring how -p already overrides raw
// implied for independent legs elsewhere in this file.
//
// Slots preserve the relative order legs were listed in: a correlated
// group's slot sits at the position of that group's FIRST member, and only
// that one slot represents the whole group afterward.
func collapseLegs(legs []parlayLeg, specs []correlationSpec, probs []float64) ([]collapsedLeg, error) {
	bySpec := map[string]correlationSpec{}
	for _, s := range specs {
		bySpec[s.game] = s
	}

	legProb := func(i int) (float64, error) {
		if probs != nil && i < len(probs) {
			return probs[i], nil
		}
		return legs[i].price.ImpliedRaw()
	}

	// Gather each covered game's member indices, in listing order.
	groupIdx := map[string][]int{}
	var groupOrder []string
	for i, l := range legs {
		if _, covered := bySpec[l.game]; !covered {
			continue
		}
		if _, seen := groupIdx[l.game]; !seen {
			groupOrder = append(groupOrder, l.game)
		}
		groupIdx[l.game] = append(groupIdx[l.game], i)
	}

	placed := map[int]bool{}
	var out []collapsedLeg
	for i, l := range legs {
		if placed[i] {
			continue
		}
		spec, covered := bySpec[l.game]
		if !covered {
			out = append(out, collapsedLeg{price: l.price, members: []parlayLeg{l}})
			continue
		}
		idxs := groupIdx[l.game]
		if i != idxs[0] {
			// Not this group's first member -- already folded into the slot
			// emitted when we reached idxs[0].
			continue
		}
		if len(spec.rhos) != len(idxs)-1 {
			return nil, fmt.Errorf(
				"-correlation %q: %d legs in this game need %d rho values (one per chain step), got %d",
				l.game, len(idxs), len(idxs)-1, len(spec.rhos))
		}
		groupProbs := make([]float64, len(idxs))
		members := make([]parlayLeg, len(idxs))
		for j, legIdx := range idxs {
			p, err := legProb(legIdx)
			if err != nil {
				return nil, fmt.Errorf("leg %d: %w", legIdx+1, err)
			}
			groupProbs[j] = p
			members[j] = legs[legIdx]
			placed[legIdx] = true
		}
		blended, err := wager.ChainFrechet(groupProbs, spec.rhos)
		if err != nil {
			return nil, fmt.Errorf("-correlation %q: %w", l.game, err)
		}
		price, err := wager.AmericanFromDecimal(1 / blended)
		if err != nil {
			return nil, fmt.Errorf("-correlation %q: %w", l.game, err)
		}
		out = append(out, collapsedLeg{
			price: price, prob: &blended, members: members, caveat: len(idxs) >= 3,
		})
	}
	return out, nil
}
```

- [ ] **Step 4: Run the `collapseLegs` tests to confirm they pass**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestCollapseLegs -v`
Expected: PASS (all five)

- [ ] **Step 5: Wire `-correlation` into `parlayCmd` itself**

In `cmd/edgectl/ladder.go`, add the new flag to `parlayCmd`'s flag block (near the existing
`book := fs.Int(...)` line, around line 222):

```go
	correlation := fs.String("correlation", "",
		`same-game legs' correlation, keyed by game tag: "game:rho[,rho,...][;game:rho,...]" -- `+
			`lifts the same-game refusal only for the named games`)
```

Replace the body from `legs, err := parseLegs(*legsFlag)` (around line 229) through the
`printParlay(legs, ticket, *book)` call (around line 258) with:

```go
	legs, err := parseLegs(*legsFlag)
	if err != nil {
		return err
	}
	specs, err := parseCorrelation(*correlation)
	if err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, s := range specs {
		covered[s.game] = true
	}
	if err := guardSameGame(legs, covered); err != nil {
		return err
	}

	probs, err := parseFloatList(*pFlag)
	if err != nil {
		return fmt.Errorf("-p: %w", err)
	}
	if probs != nil && len(probs) != len(legs) {
		return fmt.Errorf("-p: got %d probabilities for %d legs", len(probs), len(legs))
	}

	collapsed, err := collapseLegs(legs, specs, probs)
	if err != nil {
		return err
	}

	prices := make([]wager.American, len(collapsed))
	// reducedProbs stays nil (today's existing "no EV reporting" default)
	// UNLESS -p was supplied OR at least one correlated group exists --
	// a correlated group always has its own derived probability, so once
	// one exists the ticket can always compute EV, with every independent
	// slot defaulting to its own raw implied exactly as -p already does per
	// leg when supplied.
	needProbs := probs != nil
	for _, c := range collapsed {
		if c.prob != nil {
			needProbs = true
		}
	}
	var reducedProbs []float64
	if needProbs {
		reducedProbs = make([]float64, len(collapsed))
	}
	for i, c := range collapsed {
		prices[i] = c.price
		if !needProbs {
			continue
		}
		if c.prob != nil {
			reducedProbs[i] = *c.prob
			continue
		}
		p, err := c.price.ImpliedRaw()
		if err != nil {
			return err
		}
		reducedProbs[i] = p
	}

	sizes, err := parseSizes(*rr, len(collapsed))
	if err != nil {
		return err
	}
	combos, err := wager.RoundRobin(prices, sizes)
	if err != nil {
		return err
	}
	ticket, err := wager.BuildTicket(prices, combos, *total, reducedProbs)
	if err != nil {
		return err
	}

	printParlay(collapsed, ticket, *book)
	return nil
```

Note `printParlay`'s signature changes from `(legs []parlayLeg, t *wager.Ticket, book int)` to
`(collapsed []collapsedLeg, t *wager.Ticket, book int)` — Task 7 rewrites that function's body to
match; this task only updates the call site. Until Task 7 lands, the package will not compile —
**these two tasks must be executed back to back, in order** (Task 6 then Task 7), not
interleaved with anything else.

- [ ] **Step 6: Commit (together with Task 7 — see that task's own commit step)**

Do not commit yet. Proceed directly to Task 7; the two are one logical change split for
reviewability, and the package does not compile between them.

---

## Task 7: `printParlay` — render correlated groups, the breakeven ρ\*, and the caveat

**Files:**
- Modify: `cmd/edgectl/ladder.go`
- Test: `cmd/edgectl/ladder_test.go`

**Interfaces:**
- Consumes: `collapsedLeg` (Task 6), `wager.RequiredCorrelation` (Task 2).
- Produces: the final, compiling `printParlay` — completes Task 6's call site change.

- [ ] **Step 1: Write the failing test**

Since `printParlay` writes to stdout and this codebase's existing tests for it (none currently
exist — it was previously untested directly, only exercised indirectly via `parlayCmd`) don't
capture output, follow that same convention: test via `parlayCmd`'s exit behavior and via a
focused unit test on the new breakeven-reporting logic, factored out as its own function for
testability (mirroring how Task 6 factored `collapseLegs` out of `parlayCmd` for the same reason).

```go
func TestCorrelatedBookComparisonReportsBreakeven(t *testing.T) {
	// Two legs, rho=0.3, -book 150 (the book's own quoted combined price).
	// This must compute RequiredCorrelation against the group's two
	// underlying leg probabilities and the book's raw implied -- confirm it
	// does not error and returns a real, in-range value for this case.
	pA, _ := wager.American(-110).ImpliedRaw()
	pB, _ := wager.American(150).ImpliedRaw()
	bookImplied, _ := wager.American(150).ImpliedRaw()
	req, err := wager.RequiredCorrelation(pA, pB, bookImplied)
	if err != nil {
		t.Fatal(err)
	}
	if req.Required < -1 || req.Required > 1 {
		t.Errorf("breakeven rho = %v, should be in [-1,1]", req.Required)
	}
}

func TestParlayCmdCorrelationWithBookFlag() {
	t.Helper() // placeholder removed below -- see full test body
}
```

Replace that last placeholder stub with the real test (the stub above is only to show the
structure; write this instead):

```go
func TestParlayCmdCorrelationWithBookFlag(t *testing.T) {
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-book", "150",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd with -book on a correlated combo: %v", err)
	}
}

func TestParlayCmdCorrelationFloorPrintsWithoutBookFlag(t *testing.T) {
	// The floor-shopping workflow this feature exists for: compute the
	// floor BEFORE knowing any book's real price, then go shop books for a
	// quote at or longer than it. No -book flag at all -- confirms the
	// floor line's print path (hasCorrelatedGroup && len(t.Combos)==1 in
	// printParlay) does not depend on -book being supplied.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:Leg A,+150:mia@lv:Leg B",
		"-correlation", "mia@lv:0.3",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd (floor only, no -book): %v", err)
	}
}

func TestParlayCmdThreeLegCorrelatedPrintsNoError(t *testing.T) {
	// Exercises the 3+-leg caveat path end to end -- this test only confirms
	// it runs without error; the caveat text itself is confirmed present by
	// reading printParlay's source in this task's own self-review, since
	// capturing stdout is not this codebase's established test convention
	// (see board_props_report.go's printPropsReport, tested the same way).
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B,+200:mia@lv:C",
		"-correlation", "mia@lv:0.3,0.2",
		"-total", "10",
	})
	if err != nil {
		t.Fatalf("parlayCmd with a 3-leg correlated group: %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd edge/app && go test ./cmd/edgectl/... -run "TestCorrelatedBookComparison|TestParlayCmdCorrelation|TestParlayCmdThreeLeg" -v`
Expected: FAIL — compile error, since `printParlay`'s signature mismatch from Task 6 means the
whole `cmd/edgectl` package does not build yet.

- [ ] **Step 3: Rewrite `printParlay`**

Replace the existing `printParlay` function (originally `ladder.go:323-379`, now called with a
`[]collapsedLeg` instead of `[]parlayLeg` per Task 6's call site change) with:

```go
func printParlay(collapsed []collapsedLeg, t *wager.Ticket, book int) {
	totalMembers := 0
	for _, c := range collapsed {
		totalMembers += len(c.members)
	}
	fmt.Printf("PARLAY / ROUND ROBIN  (%d legs, %d combo(s), $%.2f total)\n\n", totalMembers, len(t.Combos), t.TotalRisk)
	fmt.Printf("  legs:\n")
	for i, c := range collapsed {
		if len(c.members) == 1 {
			l := c.members[0]
			lbl := l.label
			if lbl != "" {
				lbl = "  " + lbl
			}
			fmt.Printf("    [%d] %+d  %s%s\n", i+1, int(l.price), l.game, lbl)
			continue
		}
		fmt.Printf("    [%d] SGP (%s, %d legs, rho-blended): %+d fair\n",
			i+1, c.members[0].game, len(c.members), int(c.price))
		for _, m := range c.members {
			lbl := m.label
			if lbl != "" {
				lbl = "  " + lbl
			}
			fmt.Printf("          %+d%s\n", int(m.price), lbl)
		}
		if c.caveat {
			fmt.Printf("      CAVEAT: 3+ same-game legs are priced by chaining pairwise Frechet\n")
			fmt.Printf("      blends in listing order, not a true N-way joint. This is a structured\n")
			fmt.Printf("      approximation, not an exact bound -- reorder the legs or price fewer\n")
			fmt.Printf("      at a time if the result looks wrong.\n")
		}
	}

	fmt.Printf("\n  %-14s %-8s %8s %8s %12s", "combo", "price", "decimal", "stake", "payout(win)")
	if t.HasEV {
		fmt.Printf(" %8s %9s", "winP", "EV")
	}
	fmt.Println()
	fmt.Println("  " + strings.Repeat("-", 70))
	for _, c := range t.Combos {
		name := comboName(c.Legs)
		payout := c.Stake * c.Decimal
		fmt.Printf("  %-14s %-8s %8.2f %8.2f %12.2f", name, fmt.Sprintf("%+d", int(c.Price)), c.Decimal, c.Stake, payout)
		if t.HasEV {
			fmt.Printf(" %7.1f%% %+9.4f", c.WinProb*100, c.EV)
		}
		fmt.Println()
	}

	if t.HasEV {
		fmt.Printf("\n  TICKET  EV %+.2f    P(any cashes) %.1f%%    P(profit) %.1f%%\n", t.EV, t.PAny*100, t.PProfit*100)
		fmt.Printf("\n  outcome by legs won:\n")
		var keys []int
		for k := range t.Dist {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			d := t.Dist[k]
			if d.Prob < 1e-9 {
				continue
			}
			fmt.Printf("    %d of %d win   P %5.1f%%   E[net] %+8.2f\n", k, len(collapsed), d.Prob*100, d.NetSum)
		}
	}

	// A correlated group's single combo price is, by construction, the
	// floor: the minimum price that still breaks even under the operator's
	// own leg probabilities and correlation belief. Print it unconditionally
	// whenever a correlated group exists -- this is the primary output the
	// floor-shopping workflow needs, and it does not depend on -book being
	// supplied at all. (A ticket with more than one combo, e.g. a round
	// robin mixing a correlated group with independent cross-game legs,
	// has no single "the" floor to name this way -- each combo row above
	// already carries its own price, so this block only fires for the
	// single-combo case.)
	hasCorrelatedGroup := false
	for _, c := range collapsed {
		if len(c.members) > 1 {
			hasCorrelatedGroup = true
		}
	}
	if hasCorrelatedGroup && len(t.Combos) == 1 {
		floor := t.Combos[0].Price
		fmt.Printf("\n  FLOOR (this combo): %+d — the minimum price that still breaks even under the\n", int(floor))
		fmt.Printf("  leg probabilities and correlation factor(s) supplied above (implied %.1f%%).\n", t.Combos[0].Implied*100)
		fmt.Printf("  Shop this exact combo at other books; a quote at %+d or longer keeps the edge.\n", int(floor))
	}

	if book != 0 && len(t.Combos) == 1 {
		bookImpl, _ := wager.American(book).ImpliedRaw()
		fmt.Printf("\n  book offers %+d on the combined parlay (implied %.1f%%); the fair\n", book, bookImpl*100)
		fmt.Printf("  price is %+d (implied %.1f%%). ", int(t.Combos[0].Price), t.Combos[0].Implied*100)
		if book > int(t.Combos[0].Price) {
			fmt.Printf("The book pays MORE than fair — a positive gap.\n")
		} else {
			fmt.Printf("The book pays no better than fair.\n")
		}
		// Only one collapsed slot AND it is a correlated group: the breakeven
		// rho the book's price requires is reportable. A single independent
		// leg, or a 1-combo parlay of several independent legs, has no
		// correlation question to answer here.
		if len(collapsed) == 1 && len(collapsed[0].members) == 2 {
			m := collapsed[0].members
			p1, _ := m[0].price.ImpliedRaw()
			p2, _ := m[1].price.ImpliedRaw()
			req, err := wager.RequiredCorrelation(p1, p2, bookImpl)
			if err == nil {
				fmt.Printf("  the book's price requires believing these two legs correlate at rho %s %.2f to be fair.\n",
					atLeastOrAtMost(req.Required), req.Required)
			} else {
				fmt.Printf("  the book's price implies a correlation outside what any rho in [-1,1] can explain: %v\n", err)
			}
		}
	}

	fmt.Printf("\n  NOTE: legs are treated as INDEPENDENT (probabilities multiply) unless covered\n")
	fmt.Printf("  by -correlation. Same-game legs with no -correlation entry are still refused —\n")
	fmt.Printf("  build those as an SGP at the book, or supply -correlation to price them here.\n")
}

// atLeastOrAtMost phrases the breakeven-rho sentence in the right direction:
// a higher required rho means you need MORE correlation for the price to be
// fair, so the price looks good if you believe correlation is AT LEAST that
// high; it never makes sense to phrase a positive requirement as "at most."
func atLeastOrAtMost(required float64) string {
	if required >= 0 {
		return "at least"
	}
	return "at most"
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd edge/app && go test ./cmd/edgectl/... -run "TestCorrelatedBookComparison|TestParlayCmdCorrelation|TestParlayCmdThreeLeg" -v`
Expected: PASS (all tests, including the ones from Task 6 that could not compile until this step)

- [ ] **Step 5: Run the full `cmd/edgectl` suite**

Run: `cd edge/app && go test ./cmd/edgectl/...`
Expected: all green, including every pre-existing test file in this package.

- [ ] **Step 6: Commit Tasks 6 and 7 together** (the package did not compile between them)

```bash
cd edge/app
git add cmd/edgectl/ladder.go cmd/edgectl/ladder_test.go
git commit -m "edgectl parlay: collapse correlated same-game groups, price via Frechet blending, report breakeven rho against -book"
```

---

## Task 8: Regression — `edgectl parlay` without `-correlation` is byte-identical to today

**Files:**
- Test: `cmd/edgectl/ladder_test.go`

**Interfaces:**
- Consumes: everything built in Tasks 1-7.
- Produces: nothing new — this task is pure verification, the explicit regression check the spec's
  own Testing section requires.

- [ ] **Step 1: Write the test**

```go
func TestParlayCmdWithoutCorrelationUnchanged(t *testing.T) {
	// Every pre-existing call shape must still work exactly as before --
	// -correlation is additive and optional. This exercises a plain
	// independent-legs round robin with -p and -book, the richest
	// pre-existing path, and confirms it still succeeds with no
	// -correlation flag at all.
	err := parlayCmd([]string{
		"-legs", "-110:mia@lv:LV -3,+250:was@phi:McLaurin ATD,-130:nyg@dal:Dal ML",
		"-rr", "2",
		"-p", "0.55,0.40,0.30",
		"-total", "30",
	})
	if err != nil {
		t.Fatalf("parlayCmd (no -correlation): %v", err)
	}

	// Same-game legs with no -correlation flag at all must still be refused,
	// unchanged.
	err = parlayCmd([]string{
		"-legs", "-110:mia@lv:A,+150:mia@lv:B",
		"-total", "10",
	})
	if err == nil {
		t.Error("same-game legs with no -correlation flag should still be refused")
	}
}
```

- [ ] **Step 2: Run to confirm it passes**

Run: `cd edge/app && go test ./cmd/edgectl/... -run TestParlayCmdWithoutCorrelationUnchanged -v`
Expected: PASS — if this fails, something in Tasks 5-7 broke the default (no-`-correlation`) path,
which the Global Constraints section of this plan treats as a hard requirement, not a nice-to-have.

- [ ] **Step 3: Run the entire repo's Go test suite one more time**

Run: `cd edge/app && go build ./... && go test ./...`
Expected: every package green.

- [ ] **Step 4: Commit**

```bash
cd edge/app
git add cmd/edgectl/ladder_test.go
git commit -m "edgectl parlay: pin the no--correlation path as an explicit regression test"
```

---

## Final verification

- [ ] **Spec coverage check** — cross-reference
  `docs/superpowers/specs/2026-10-14-sgp-correlation-pricing-design.md` section by section:
  - §1 (Fréchet math, forward + inverse) — Tasks 1, 2.
  - §2 (leg probabilities via existing `-p` convention) — Task 6's `legProb` closure.
  - §3 (pairwise chaining, 3+ legs, caveat) — Task 3 (math), Task 7 (the printed caveat text,
    copied verbatim from the spec).
  - §4 (CLI surface: `-correlation` flag format, composability into round robins, scoped
    single-game-at-a-time honesty) — Tasks 4, 5, 6.
  - **Floor framing** (clarified by the operator after the spec was approved, folded into this
    plan rather than re-opening the spec: the forward Fréchet price IS the floor — a minimum-odds
    breakeven the operator can shop books against — no new math, only Task 7's unconditional
    FLOOR print, independent of `-book`) — Task 7.
  - Testing section — every bullet has a corresponding task: forward/inverse pinned examples
    (Tasks 1, 2), chain-order-matters (Task 3), CLI refusal-lifted-only-for-named-games (Task 5),
    `-book`/breakeven-ρ reporting (Task 7), round-robin composability (Task 6's `collapseLegs`
    feeding the existing `RoundRobin`), regression (Task 8).
  All covered.

- [ ] **End-to-end smoke test against a real price**, once this is merged: price a real same-game
  combo from this week's actual DK captures the way this session did once by hand for the Javonte
  Williams 25+ rush yards + ATD combo (naive independent ~53.3%, book's real price ~55.6%) — run
  it through `edgectl parlay -correlation` with a guessed ρ, and separately run the `-book`
  breakeven report against DK's real combined price, to see in practice what ρ the book was really
  charging for that pairing. This is the first live validation against a real wager, not a
  synthetic test case.

---
title: "Belief probe — 2026 week 1 forecast run"
doc_version: 2.0.0
status: READY TO INGEST — v2 forecast binds to the committed pack; nothing blocking
date: 2026-09-06
owner: wubrg
relates_to:
  - ../frameworks/belief-probe.md
  - ../ADR-002-belief-log-tracked-in-git.md
  - ../ADR-003-week01-binds-to-the-pack-as-pasted.md
  - ../ADR-004-week01-forecast-method.md
  - ../ADR-005-week01-claim-and-abstention-policy.md
---

# 2026 week 1 — belief probe forecast run

## 1. The request, as received

Verbatim shape of the prompt handed to the forecaster (full text is the operative block of
[`frameworks/belief-probe.md`](../frameworks/belief-probe.md), rendered as
`beliefs/2026/week01.prompt.md`):

> Forecast every row of the 2026 week 1 pack — four scenarios (`shootout`, `blowout_loss`,
> `pass_heavy`, `efficient_offense`) across 16 games — as a probability in `[0,1]`, with a
> self-reported confidence, an `abstained` flag where there is no read, and typed claims for every
> factual assertion beyond the pack. Echo the pack sha. Strict JSON, unknown fields refused,
> `generated_at` before the first kickoff. Flag roughly ten rows as the ones that would be bet.

Row count: 16 `shootout` (game unit) + 32 each of `blowout_loss`, `pass_heavy`,
`efficient_offense` (team unit) = **112 rows**. A file that omits rows is refused.

## 2. RESOLVED — the pack discrepancy, and what it was hiding

> **Outcome:** neither pack is used. The week-1 pack was regenerated in-repo on 2026-09-06 and the
> forecast binds to it (`2392b66a…`). It ingests. The history below is kept because the diagnosis
> is what found the stale base rates *and* six moved lines.

### The original discrepancy

**The pack pasted into the session and the pack committed at
`edge/beliefs/2026/week01.input.json` are different artifacts.**

| | pasted pack | committed pack |
|---|---|---|
| `pack_sha256` | `42bca9d8…a985347` | `e4465d9e…ca14c61a1` |
| base rate `pass_heavy` | 0.2648 | 0.3353 |
| base rate `efficient_offense` | 0.3721 | 0.3243 |
| base rate `shootout` / `blowout_loss` | 0.3378 / 0.2617 | identical |
| slate, kickoffs, totals, spreads | identical, all 16 games | identical |
| FORM | absent (week 1) | absent (week 1) |

The two differ **only** in the two base rates. Every market number the forecast actually leans on
is byte-identical between them. Reconstructing the pasted pack by editing the two base rates into
the committed file does **not** reproduce `42bca9d8…` (it hashes to `e47d061d…`), so the pasted
pack was generated from a different cache snapshot and its exact bytes are not recoverable here.

Consequence, from `runIngest` (`edge/app/cmd/edgectl/beliefs_result.go:580`): ingest refuses the
whole file when `input_pack_sha256` does not equal the sha of the `-pack` file on disk. So:

- a forecast echoing `42bca9d8…` (what the contract instructs) **will not ingest** against the
  committed pack until the matching pack file is committed;
- a forecast echoing `e4465d9e…` ingests today, but claims to have been written against facts that
  differ in two base rates from the ones actually shown.

**Root cause, found after the forecast was written:** the committed pack is stale, not the pasted
one. `beliefpack.py base_rates()` reads `base_rate_held_out` from the committed `belief.json`,
which carries 0.3721 and 0.2648 — the pasted pack's numbers. The committed `week01.input.json`
(2026-08-24) predates that fix and carries the in-sample 0.3243 / 0.3353. Per `beliefpack.py`'s own
docstring, the in-sample number is worth ~30% of the target edge to a forecaster that does nothing
but repeat it, which is why it was corrected.

Resolved in [ADR-003](../ADR-003-week01-binds-to-the-pack-as-pasted.md): bind to the pasted pack.
Operator confirmed 2026-09-06.

## 3. Plan

Chunked so each chunk fits a small session.

| # | chunk | output | status |
|---|---|---|---|
| C7 | Regenerate the pack in-repo once `games.csv` was fetchable; discover six moved lines | `week01.input.json` sha `2392b66a…` | DONE |
| C8 | Tier 1 offseason context in the pack, prompt and falsifier (see [the other plan](./2026-pack-offseason-context.md)) | `beliefpack.py`, `falsify.go`, spec | DONE |
| C9 | Re-forecast against the new pack, staff included | `week01.forecast.json` v2 | DONE |
| C1 | Read the framework, the ingest/falsify code, the committed pack; establish what is checked and what is not | this document §2, §4 | DONE |
| C2 | Record the run's assumptions as ADRs | ADR-003/004/005 | DONE |
| C3 | Build the forecast generator: market-derived baselines + explicit per-row deviations | `beliefs/2026/week01.forecast.build.py` | DONE |
| C4 | Emit and self-validate the 112-row file (row coverage, unit rules, probability bounds, claim grammar) | `beliefs/2026/week01.forecast.json` | DONE |
| C5 | `make check`, commit, push to `claude/belief-pack-nfl-forecast-ihx2up` | branch | DONE |
| C6 | Operator answers §6, then ingest (`edgectl beliefs ingest`) before the 2026-09-09 20:20 ET kickoff | log.jsonl entry | BLOCKED on §6 |

## 4. What the checker will and will not do to this file

From `edge/app/cmd/edgectl/falsify.go`:

- `form`, `market`, `schedule` claims are adjudicated **against the pack**. A wrong restatement of
  a pack number is the one way to lose a prediction to the falsifier. This run therefore states
  **no** claim of those three types at all (§ADR-005).
- `usage` and `injury` are counted as *deferred, unchecked* — the pack carries neither.
- `personnel` and `narrative` cannot be checked; a prediction resting only on them is reported as
  **unauditable**, which is not the same as wrong.

**Expected tally for this file: zero checked, zero deferred, the large majority unauditable.** That
is the honest consequence of a forecaster whose information is prior-season scheme identity and
whose knowledge of September 2026 injury reports is nil. Inventing `injury:` claims to lower the
unauditable count would be precisely the failure mode the probe exists to catch.

## 5. Method summary

Full derivation in [ADR-004](../ADR-004-week01-forecast-method.md). In short:

1. **`shootout`** — normal model on the posted total, σ = 14.0 (widened from the ~13.5 full-season
   value for week-1 uncertainty), threshold 50.5; then a disclosed −0.02 week-1 tilt and a
   variance read on two games.
2. **`blowout_loss`** — normal model on the team's expected margin (spread, sign-flipped for the
   away side), σ = 14.5, threshold −7.5.
3. **`pass_heavy`** — a per-team prior-season **scheme identity** prior, nudged by game context.
   This is the scenario where the binding opponent is a logistic on total and spread alone, so it
   is where an outside read can actually add something, and it is where most flags sit.
4. **`efficient_offense`** — logistic in the market-implied team total, centred on the pack's base
   rate, plus a per-offence adjustment for teams whose success rate historically runs above or
   below their points, minus a uniform week-1 rust tilt.

## 6. OPEN DECISION — needs an operator answer before C6

**Q1. Which pack sha does the week-1 forecast bind to?** — **CLOSED 2026-09-06: neither.** The pack
was regenerated in-repo and the forecast binds to `2392b66a…`, which carries the held-out base
rates *and* the current market. Answered (a) first; regeneration then made the choice moot in the
right direction. See ADR-003's *Resolution*.

- **(a) The pasted pack, `42bca9d8…`** *(chosen)*. Contract-faithful: the
  prompt says echo the sha of the pack you were shown, and per §2 it is also the *correct* pack.
  Cost: it does not ingest until the file hashing to `42bca9d8…` is committed **from the machine
  that generated it** — a fresh regeneration cannot reproduce the sha, because the pack embeds its
  own `generated_at` and the cache's `fetched_at`.
- **(b) The committed pack, `e4465d9e…`**. Ingests today. Cost: the file asserts a binding to facts
  that were not the ones shown — a small lie in exactly the field the design uses to prevent
  after-the-fact substitution.
- **(c) Both**, as two files. Cost: two entries per row in `log.jsonl` if both are ingested; the
  duplicate guard only fires *within* one file.

**Q2. What goes in the `model` field?** — **ANSWERED: keep the neutral string, 2026-09-06.** The session's operating policy forbids writing a model
identifier into artifacts pushed to a repository, but the output contract asks for "your name and
version". Default taken: the neutral string `llm-forecaster/belief-v1`. If the log should carry the
exact model string for the experiment's provenance, the operator sets it by hand — one field, one
line.

## 7. What the emitted file contains

`beliefs/2026/week01.forecast.json` — 112 rows, no omissions:

| | count |
|---|---|
| rows | 112 (16 `shootout`, 32 each of the other three) |
| abstained | 10 — 4 `pass_heavy` (ARI, BAL, MIA, TEN), 6 `efficient_offense` (ARI, CLE, JAX, LV, NYG, TEN) |
| flagged | 11 — 10 `pass_heavy`, 1 `efficient_offense` |
| claims | 57 — **14 `coaching`, adjudicated at ingest**, 42 `narrative`, 1 `personnel`; 0 `form`/`market`/`schedule`, 0 `injury`/`usage` |

The abstention set *moved* rather than shrank, which is the interesting part: the STAFF block
promoted JAX, LV and NYG out of abstention (their new head coach is himself the play-caller) and
pushed BAL and MIA into it (defensive head coaches, unknown play-callers). Knowing who the head
coach is resolved three teams and disqualified two.

**Validated against the real gate.** `edgectl beliefs ingest -n`, run on a scratch copy with the
committed sha substituted so every check *except* the sha binding is exercised:

```
BELIEFS  2026 week 1  from llm-forecaster/belief-v1
  pack     beliefs/2026/week01.input.json  (sha 2392b66a1ccf…)
  ready    112
  claims   14 checked, 43 unverifiable, 0 untyped, 0 deferred
  NOTE     40 rest entirely on unverifiable claims — not wrong, but
           they cannot be audited either way
```

Run against the **real committed pack**, no substitution: 112 ready, nothing rejected, nothing
untyped, no unit or kickoff violations. The v1 run of this same command reported `0 checked, 55
unverifiable` — the fourteen checked claims are the measured effect of ADR-006. `go vet`, `gofmt`
and `go test ./...` in `edge/app` are green.

## 8. Progress / token-budget tracker

- 2026-09-06 — C1–C5 complete in one session; file emitted, self-validated against the real ingest
  gate, committed and pushed. C6 blocked on Q1 (§6).
- 2026-09-06 — operator answered Q1 (a) and Q2 (keep neutral). Root cause of the pack discrepancy
  found and recorded in ADR-003: the committed pack is the stale one.
- 2026-09-06 — pack regenerated, Tier 1 shipped, forecast re-emitted as v2 and validated against
  the committed pack. **Nothing is blocking.**
- **C6, the only step left**, before 2026-09-09 20:20 ET — drop `-n` to write the log:
  `edgectl beliefs ingest -file beliefs/2026/week01.forecast.json -pack beliefs/2026/week01.input.json`
- Regenerating the pack again before ingest would change its sha (it embeds `generated_at`) and
  invalidate the forecast. If the lines move again and that matters more than the binding, re-run
  `week01.forecast.build.py` afterwards — it reads the pack and re-hashes it, so the pair stays
  consistent by construction.

## 9. Changelog

| version | date | change |
|---|---|---|
| 2.0.0 | 2026-09-06 | v2 forecast. Pack regenerated in-repo (six lines had moved); staff context folded in per ADR-006; four flags withdrawn and four added; 14 claims now checked at ingest. Q1 closed by regeneration. |
| 1.1.0 | 2026-09-06 | Root cause of the pack discrepancy found (committed pack is stale; the pasted one carries the held-out base rates from `belief.json`). Q1 and Q2 answered. C6 rewritten for the machine that holds the cache. |
| 1.0.0 | 2026-09-06 | First issue. Records the request, the pasted-vs-committed pack discrepancy, the plan, the method summary, the emitted file's shape and its validation, and the two open decisions. |

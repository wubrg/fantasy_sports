---
title: "Belief probe — 2026 week 1 forecast run"
doc_version: 1.0.0
status: IN PROGRESS — C1–C5 done, C6 blocked on §6 Q1
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

## 2. The blocking discrepancy — the pasted pack is not the committed pack

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

Resolved in [ADR-003](../ADR-003-week01-binds-to-the-pack-as-pasted.md). **Open decision surfaced
to the operator — see §6.**

## 3. Plan

Chunked so each chunk fits a small session.

| # | chunk | output | status |
|---|---|---|---|
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

**Q1. Which pack sha does the week-1 forecast bind to?**

- **(a) The pasted pack, `42bca9d8…`** *(taken as the default, ADR-003)*. Contract-faithful: the
  prompt says echo the sha of the pack you were shown. Cost: it does not ingest until the matching
  `week01.input.json` is committed (regenerate with `make belief-pack SEASON=2026 WEEK=1` and
  confirm it hashes to `42bca9d8…`).
- **(b) The committed pack, `e4465d9e…`**. Ingests today. Cost: the file asserts a binding to facts
  that were not the ones shown — a small lie in exactly the field the design uses to prevent
  after-the-fact substitution.
- **(c) Both**, as two files. Cost: two entries per row in `log.jsonl` if both are ingested; the
  duplicate guard only fires *within* one file.

**Q2. What goes in the `model` field?** The session's operating policy forbids writing a model
identifier into artifacts pushed to a repository, but the output contract asks for "your name and
version". Default taken: the neutral string `llm-forecaster/belief-v1`. If the log should carry the
exact model string for the experiment's provenance, the operator sets it by hand — one field, one
line.

## 7. What the emitted file contains

`beliefs/2026/week01.forecast.json` — 112 rows, no omissions:

| | count |
|---|---|
| rows | 112 (16 `shootout`, 32 each of the other three) |
| abstained | 11 — 5 `pass_heavy` (ARI, JAX, LV, NYG, TEN), 6 `efficient_offense` (ARI, CLE, JAX, LV, NYG, TEN) |
| flagged | 11 — 9 `pass_heavy`, 2 `efficient_offense` |
| claims | 55, all `narrative`; 0 `form`/`market`/`schedule`, 0 `injury`/`usage` |

**Validated against the real gate.** `edgectl beliefs ingest -n`, run on a scratch copy with the
committed sha substituted so every check *except* the sha binding is exercised:

```
BELIEFS  2026 week 1  from llm-forecaster/belief-v1
  ready    112
  claims   0 checked, 55 unverifiable, 0 untyped, 0 deferred
  NOTE     53 rest entirely on unverifiable claims — not wrong, but
           they cannot be audited either way
```

Nothing rejected, nothing untyped, no unit or kickoff violations. The unverifiable count is the
predicted consequence of ADR-005, not a surprise. Run unmodified, ingest refuses on the sha, with
the error §2 predicts. `go test ./...` in `edge/app` is green; no Go code was touched.

## 8. Progress / token-budget tracker

- 2026-09-06 — C1–C5 complete in one session; file emitted, self-validated against the real ingest
  gate, committed and pushed. C6 blocked on Q1 (§6).
- Next session, if Q1 is answered (a): `make belief-pack SEASON=2026 WEEK=1`, confirm the sha is
  `42bca9d8…`, commit the pack, ingest before 2026-09-09 20:20 ET. If (b): re-run
  `week01.forecast.build.py` with `PACK_SHA` and the two `BASE` entries switched to the committed
  pack's, which changes only the 11 abstained placeholders.

## 9. Changelog

| version | date | change |
|---|---|---|
| 1.0.0 | 2026-09-06 | First issue. Records the request, the pasted-vs-committed pack discrepancy, the plan, the method summary, the emitted file's shape and its validation, and the two open decisions. |

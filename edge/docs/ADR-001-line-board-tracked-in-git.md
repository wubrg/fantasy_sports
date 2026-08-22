# ADR-001: The line board is tracked in git

**Status:** Accepted
**Date:** 2026-08-20
**Deciders:** wubrg

---

## Context

`edgectl board scaffold` generates `edge/moneylines/week01.yaml` … `week18.yaml` — one file per
week of the NFL regular season, 272 games total, each carrying a slot for seven sportsbooks across
three markets. That is roughly 5,700 cells and ~6,000 lines of YAML.

Two properties of these files pull in opposite directions:

- **They are generated.** The schedule, the game ids, the kickoff times, and the prefilled
  `consensus` column all come from `edge/model/data/raw/games.csv`, which is itself gitignored and
  regenerable from nflverse. A scaffold run reproduces the whole structure from scratch.
- **They are hand-entered data.** Every price under `fanatics`, `draftkings`, `fanduel`, `betmgm`,
  `caesars`, and `bet365` is typed in by a person reading a sportsbook app. Nothing regenerates
  those. If they are lost, they are lost.

The precedent in this repo cuts the other way: `edge/model/data/raw/` is gitignored precisely
because it is a large, regenerable cache. A reader could reasonably expect `moneylines/` to follow
the same rule, since it is also large and also produced by a generator.

---

## Decision

**Track `edge/moneylines/` in git.**

The files are committed in full, including the empty slots produced by scaffolding.

---

## Rationale

**The irreplaceable content dominates.** The generated scaffolding is cheap to reproduce; the
hand-entered prices are not reproducible at any price. A sportsbook does not publish what it was
offering last Tuesday, so a price not written down when it was seen is gone permanently. Losing
`data/raw/` costs a download. Losing `moneylines/` costs a season of observations.

**Prices are historical evidence, not current state.** The board's value compounds over time. A
recorded line is what a de-vig was computed against, what a bet was actually placed at, and what a
later calibration check is scored against. This mirrors the reasoning already applied to
`~/fanatics-bonus.jsonl`, the append-only prediction log: a prediction is only meaningful if it
demonstrably predates its outcome. Version history supplies exactly that guarantee for prices —
the commit timestamp proves when a number was observed, which no amount of later editing can fake.

**A concrete case already exists.** A board recorded `SF +150 / LAR -150`, implying a 0.00%
overround — not a price any book posts. The true value was `-180`. That error was found and
corrected days later, and the correction changed a bet's recorded fair probability. Without
history there is no record that the number ever changed, nor of what was believed at the moment
the wager was placed.

**Diff noise is real but tolerable.** Re-running `scaffold` rewrites all 18 files, and yaml.v3
re-encodes values it may quote differently than they were typed. The mitigations are that
scaffold is deterministic (map keys are encoded in sorted order, so a no-op run produces a no-op
diff) and that it never overwrites a filled-in cell, so re-running is safe at any moment. In
practice scaffold runs rarely — at season rollover and after a schedule reissue.

---

## Alternatives Considered

### A: Gitignore `moneylines/`, matching `data/raw/`

- **Pro:** Consistent with the existing cache rule; no diff noise; repo stays small.
- **Con:** Hand-entered prices exist only on one machine, with no backup and no history. This is
  the decisive objection — it puts unreproducible data one `rm -rf` from gone.

### B: Track the files but gitignore empty scaffolding, committing only games with prices

- **Pro:** Small diffs; only real data in history.
- **Con:** There is no clean mechanism for it. Empty cells are cells within a file, not separate
  files, so this needs a filter or a smudge/clean driver — machinery that silently rewrites files
  on checkout, for a problem that is only cosmetic.

### C: Commit prices to a separate append-only log, keeping YAML as a working scratch file

- **Pro:** Strongest history story; matches the betlog pattern exactly.
- **Con:** Two sources of truth for the same number, and the reconciliation problem that follows.
  Worth revisiting if price *revisions* (a line moving over a week) become something to analyze
  rather than just record — the YAML holds only the latest value per cell, so intra-week movement
  is currently visible in git history but not queryable by the tooling.

---

## Consequences

- Anyone cloning the repo gets the full price history.
- `make board` produces diffs. That is expected and not a signal that something is wrong.
- The season rollover is a deliberate act: scaffolding 2027 adds ~6,000 lines in one commit, and
  the prior season stays in history rather than being deleted.
- If the repo later grows uncomfortable, the escape hatch is Alternative C rather than
  Alternative A — move to an append-only price log and keep history, rather than discarding it.

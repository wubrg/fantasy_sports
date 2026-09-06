# ADR-004: How the 2026 week-1 beliefs were derived

**Status:** Accepted
**Date:** 2026-09-06
**Deciders:** wubrg
**Scope:** one forecast run (2026 week 1), no prior form available.

---

## Context

Week 1 has no FORM block: prior form needs three earlier games. The forecaster therefore has
exactly three inputs — the posted total, the posted spread, and whatever it knows about football
that is not in the pack. The scoring rule says the hardest opponent binds, and for the two
scenarios with no market line that opponent is a logistic fitted on the posted total and spread
alone. Beating it means adding information those two numbers do not contain.

Every number in the emitted file is reproducible from
`edge/beliefs/2026/week01.forecast.build.py`; nothing was typed in by hand row-by-row.

## Decision

Four derivations, each a market-derived baseline plus a stated deviation.

### 1. `shootout` (combined > 50)

`P = 1 − Φ((50.5 − total) / σ)`, `σ = 14.0`.

- Threshold 50.5, not 50: scores are integers and the scenario is strictly `> 50`.
- σ = 14.0 rather than the ~13.5 that fits a full season: week-1 team strength is less known, so
  the combined-score distribution is wider. This *raises* P for totals below 50 and *lowers* it
  above — a mean-reversion effect, not a directional bet.
- Then a uniform **−0.02** week-1 tilt (vanilla game plans, no live-rep continuity, defences ahead
  of offences in September), and a further variance shrink on two games where an elite defence and
  a low posted total point at a low-variance script.

### 2. `blowout_loss` (margin < −7)

`P = Φ((−7.5 − m) / σ)`, `m` = the team's expected margin (the posted spread, sign-flipped for the
away side), `σ = 14.5`.

- Threshold −7.5 for the same integer-margin reason; −7 exactly does not qualify.
- σ widened to 14.5 for week 1, which pulls both tails of lopsided games toward the middle.
- Deviations only where a specific read exists (a defence that compresses margin variance, an
  offence whose floor invites a rout).

### 3. `pass_heavy` (PROE > 3.0)

**Not derived from the line.** A per-team prior on `P(PROE > 3)`, set from prior-season play-calling
identity, then nudged ±0.02–0.05 for game context (big underdogs a touch higher, big favourites a
touch lower).

Rationale: PROE is the most stable team trait among the four scenarios — it is a property of the
play-caller, not of the score — and it is invisible to a model that sees only a total and a spread.
This is where an outside read is worth the most, so it carries most of the flags and the highest
confidences. It is also where a wrong read costs the most, so teams whose 2026 play-calling could
not be vouched for are abstained rather than guessed (ADR-005).

### 4. `efficient_offense` (success rate > 0.46)

`logit P = logit(base) + 0.15 × (implied_team_total − 22.5) + adj`, where
`implied_team_total = (total ± spread)/2` and `base` is the pack's base rate for the scenario.

- The 0.15/point slope puts a team implied at 27 points near 0.5 and one implied at 18 near 0.2,
  which is the right order of magnitude for how points and success rate co-move.
- `adj` is the only real content: teams whose success rate historically runs **above** their points
  (methodical, high-floor offences) get +0.02…+0.05; teams whose points come from explosives,
  defence or field position, or whose interior line caps the floor, get −0.02…−0.05.
- A uniform **−0.02** week-1 rust tilt, for the same reason as `shootout`.

## Consequences

- On `shootout` and `blowout_loss` the file mostly agrees with the market, by construction. That is
  deliberate: the market is the binding opponent there and a loud disagreement with no information
  behind it is worse than silence.
- The disagreement budget is spent on `pass_heavy`, and secondarily on `efficient_offense`, where
  the binding opponent is a two-variable logistic.
- Every deviation is a **stated** constant in the build script, so a later reviewer can re-run the
  file with a deviation zeroed and see what it was worth.

## Alternatives rejected

- **Reverse-engineering the incumbent.** The framework asks explicitly for a genuine read; matching
  `belief.json` would measure nothing.
- **Nudging every row a point or two off the base rate.** Perfectly calibrated, worth nothing, and
  the framework names it as the failure to avoid.

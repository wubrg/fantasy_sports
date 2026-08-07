# Findings

Measured results from `edge/model/analysis/`. Each is reproducible by running the named script
against the nflverse cache (`python3 edge/model/ingest/nflverse.py` first).

---

## 1. The scenario model's normality assumption was wrong

`fit_residuals.py` · 5,698 games, 2005–2025

The scenario layer assumed final totals and margins were normal, with σ = 10.0 and 13.5. **The
total assumption was badly wrong** — the real residual sd is 13.30, not 10.0.

Held out on 2022–2025, seasons the fit never saw:

| | normal (shipped) | empirical | |
|---|---|---|---|
| total | 4.30 pts | **0.78 pts** | mean absolute calibration gap |
| margin | 3.80 pts | **1.89 pts** | |

The old model claimed 7.5% where reality was 14.7%, and 73.6% where reality was 57.4% — overconfident
in both tails, exactly as a too-small σ predicts.

**Fix:** empirical residual CDF, embedded as `edge/app/internal/scenario/artifacts/residuals.json`.

### Supporting measurements

- **Dispersion is flat across line level.** 12.8–13.9 in every bucket, for both quantities. One pooled
  residual distribution is defensible.
- **Dispersion is era-stable for totals, mildly declining for margins.** Margin sd fell 13.67
  (2005–11) → 12.41 (2022–25), about 4σ and monotonic. Total sd held at 13.0–13.4.
- **Levels moved; dispersion did not.** This is the "relationships survive level shifts" argument,
  measured. It is why the fit pools from 2014/2016 rather than cutting at a guessed era boundary.
- **Fit window is a bias-variance tradeoff.** Fitting from 2005 includes stale data and hurts;
  fitting from 2019 has too few games and hurts more. Chosen by rolling-origin CV — a single
  two-season fold proved unstable, picking a 534-game window that scored 1.02 on the fold and 2.20
  on the real holdout.

### The push atom

The empirical distribution represents something a normal cannot: **2.41% of games land exactly on the
spread** (0.80% on the total). A continuous distribution assigns that event probability zero.

That event is the push that forfeits a FanDuel bonus bet outright — the rule `CheckBonusMarket` has
guarded against since it was written. The model can now price it.

---

## 2. Utilization trend does predict production — but only the tail is bettable

`utilization_lag.py` · 39,127 player-games, 1,389 players, 2005–2025, errors clustered by player

The corpus claims: *"A player's utilization trend is a leading indicator of future production, whereas
a box score is a lagging indicator."*

**It holds.** Controlling for season-to-date target share, the two-game trend carries additional
information in every era:

| era | n | trend β (yards) | t | ΔR² |
|---|---|---|---|---|
| all | 39,127 | 41.99 (se 4.09) | 10.26 | +0.0032 |
| 2005–11 | 6,746 | 37.97 | 4.37 | +0.0038 |
| 2012–17 | 13,317 | 34.99 | 4.75 | +0.0019 |
| 2018–21 | 9,235 | 48.55 | 5.84 | +0.0039 |
| 2022–25 | 9,829 | 48.91 | 6.86 | +0.0042 |

The mechanism checks out too — the effect is cleaner on **targets** than on yards (t = 11.9,
ΔR² = +0.0054), which is what "a bet on opportunity" predicts.

Standard errors are clustered by player. A player contributes dozens of correlated observations, and
naive OLS errors would have overstated significance badly on a sample this size.

### But significance is not an edge

Game-to-game receiving yards have an sd of 34.3, so a coefficient of 42 yards per unit of trend
converts to very little unless the trend is large:

| trend percentile | share points | yards | P(over) shift |
|---|---|---|---|
| p75 | +3.0 | +1.3 | +1.5 pts |
| **p90** | **+5.9** | **+2.5** | **+2.9 pts** |
| p95 | +7.9 | +3.3 | +3.9 pts |
| p99 | +12.0 | +5.0 | +5.8 pts |

Against a −110 hurdle of 52.4%, an ordinary uptick is worth about a point and **cannot clear the
vig**. A role change at the 90th percentile or beyond — roughly **+6 share points across two games** —
is worth 3 points or more, which can.

**Decision rule:** act on role changes, not upticks. The threshold is around +6 share points.

### Caveat that bounds all of the above

This measures information relative to **season-to-date share**, not relative to whatever the
sportsbook actually uses. If books already weight recent form — and they probably do to some degree —
the real edge is smaller than these numbers. Establishing that needs historical prop lines, which are
not freely available. Treat these figures as an **upper bound**.

### Era answer, as a byproduct

The coefficient is stable and if anything strengthening (38 → 49). There is no era boundary that
invalidates the relationship, which answers the 2005-vs-2012 question for this purpose: pool
everything, and prefer recency only where dispersion drifts.

---

## 3. Pooled conditionals: shootout confirmed, "trash time" reversed

`fit_conditionals.py` · 27,288 player-games, 2014–2025 · 64 cells published, 16 dropped for n < 100

`q` and `r` were operator guesses. They are now looked up from a grid pooled across player-games
sharing an opportunity band, a role-trend band, and a game script — cells of hundreds to thousands of
observations, which is the sample size the per-player route never had.

### Shootout behaves as the corpus predicts

Median receiving yards, scenario occurred vs not:

| projected targets | delta |
|---|---|
| 0–4 | +3 to +4 |
| 4–6 | +4 to +8 |
| 6–8 | +9 to +11 |
| 8–11 | +13 to +23 |

Positive in every cell, and growing with volume — alphas benefit from a shootout far more than
rotational players do. This is the clean case.

### "Trash time correlation" is backwards as measured

Edge of Vigor's Tier 3 predicts a garbage-time boost: a team down 14 must throw, so its receivers see
more work. **Measured on final margin, every cell is negative** — −1 to −13 yards.

The reason is the end-state proxy. Final margin conflates "trailed and threw a lot" with "was simply
bad," and the second dominates: losing by more than a touchdown mostly identifies offenses that did
not function, which swamps the late-game volume.

**This does not refute the garbage-time mechanism. It refutes final margin as a proxy for it.**
Separating them needs play-by-play — time remaining crossed with score differential — which is out of
scope here. The scenario is named `blowout_loss` rather than `trailing` so the artifact says what it
measures, and `TestBlowoutLossHurts` pins the sign so that a future play-by-play definition flipping
it is a deliberate, visible change.

Usefully, the negative direction exercises machinery that already existed: when `q < r` the belief
requirement flips from a floor to a ceiling, and the decomposition has handled that since it was
written.

### How uncertainty travels

Each cell stores a quantile table rather than a single probability, so `P(yards > L)` is answered at
any line. The cell's `n` becomes a Wilson interval at query time, reusing the hit-rate layer's code —
so a pooled estimate and an empirical one report uncertainty the same way, and a thin cell says so
instead of looking confident.

---

## Data note

`target_share` in nflverse only starts in 2009, but raw `targets` reaches back to 2005, so share is
computed from targets and team-week totals. Validated against the precomputed column where both
exist: **mean absolute difference 0.0015**.

`snap_counts` only exists from 2012 — Pro-Football-Reference's snap data begins there. Any analysis
depending on snap share is limited to 2012 forward.

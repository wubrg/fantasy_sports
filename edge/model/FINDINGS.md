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

> **Caveat on those two numbers.** The calibration thresholds were chosen by hand. Re-running the
> whole selection-and-holdout pipeline under 11 alternative threshold grids, the empirical model
> beats the normal one on **every** grid — the direction is not an artifact — but the shipped grid
> happens to produce the best total figure of any tested (range 0.78–1.37, median ≈ 1.0). The honest
> headline for totals is **~1.0**, with 0.78 being roughly 0.25 points optimistic from grid choice.
> Margin ranges 1.60–2.59 across grids against a shipped 1.89.

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
  - **For totals this selects on real signal** (CV scores range 2.40 → 1.22 across candidate starts).
  - **For margins it does not.** Five of six candidates fall within a 0.15-point spread, and the
    "chosen" window swings across 2005/2008/2012/2014 under alternative threshold grids. Calling the
    margin window "selected by CV" is technically true and practically meaningless — it is selecting
    on noise. Harmless, since margin dispersion is flat across those windows anyway, but the
    provenance claim overstated what happened.

### The push atom

The empirical distribution represents something a normal cannot: games landing exactly on the line.
A continuous distribution assigns that event probability zero.

| sample | n | spread push | total push |
|---|---|---|---|
| **2005–2025** | 5,698 | **2.56%** | **1.49%** |
| 2016–2021 (total fit window) | 1,622 | 2.22% | 0.80% |
| 2014–2021 (margin fit window) | 2,156 | 2.41% | 0.97% |

> **Correction.** This previously read "2.41% … (0.80% on the total)" under a heading citing 5,698
> games from 2005–2025. Those two figures come from two *different* fit windows, neither of them the
> sample named. On the cited sample the values are 2.56% and 1.49% — the total-push atom is nearly
> twice what was claimed, so the error understated the case for the empirical model rather than
> inflating it. Conditional on an integer line, on that same 2005–2025 sample, the rates are
> **4.65% and 3.01%**.
>
> **Second correction.** This paragraph originally closed with "4.82% and 2.86%" — the *1999–2025
> whole-file* figures, attached to the 2005–2025 sample. A correction whose entire point was that
> numbers had been quoted from the wrong window closed by quoting numbers from the wrong window.
> Caught by a second audit.

That event is the push that forfeits a FanDuel bonus bet outright — the rule `CheckBonusMarket` has
guarded against since it was written. The model can now price it.

---

## 2. Utilization trend does predict production — but only the tail is bettable

`utilization_lag.py` · 39,126 player-games, 1,389 players, 2005–2025, errors clustered by player

The corpus claims: *"A player's utilization trend is a leading indicator of future production, whereas
a box score is a lagging indicator."*

**It holds.** Controlling for season-to-date target share, the two-game trend carries additional
information in every era:

| era | n | trend β (yards) | t | ΔR² |
|---|---|---|---|---|
| all | 39,126 | 42.00 (se 4.09) | 10.27 | +0.0032 |
| 2005–11 | 6,746 | 37.97 | 4.37 | +0.0038 |
| 2012–17 | 13,317 | 34.99 | 4.75 | +0.0019 |
| 2018–21 | 9,235 | 48.55 | 5.84 | +0.0039 |
| 2022–25 | 9,828 | 48.97 | 6.86 | +0.0042 |

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

`fit_conditionals.py` · 27,287 player-games, 2014–2025 · 64 cells published, 16 dropped for n < 100

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

### "Trash time correlation" is backwards as measured — with one exception

Edge of Vigor's Tier 3 predicts a garbage-time boost: a team down 14 must throw, so its receivers see
more work. **Measured on final margin, 14 of 15 cells are negative** — −1 to −13 yards.

> **Correction.** This section previously read "every cell is negative." That was wrong, and an
> adversarial audit caught it. The exception is `0–4 projected targets` with a declining trend:
> **+2.0** (median 13.0 with the blowout vs 11.0 without). At that volume the medians are 11–13
> yards and a two-yard gap is noise, but the claim as written was false, and
> `TestBlowoutLossHurts` passed only because it probed a single point rather than the grid. It has
> been rewritten to assert over every cell and to require that positive cells stay confined to the
> lowest-volume band.
>
> The audit also refit on 2014–2021 and evaluated on 2022–2025: the shootout sign survives **14/14**
> cells, blowout_loss only **10/13**, with roughly **2.7 points** of non-noise held-out error on `q`
> against a 2.4-point vig cushion at −110.

### blowout_loss is now gated off

A second audit went further, and the scenario **cannot be priced any more**. `Lookup` and `QR` refuse
it; `edgectl` prints the reason and lists what you can use instead. The cells still ship — the fit
stays reproducible and the data remains available for the work that would validate it.

Three independent failures, any one disqualifying:

1. **The direction is not consistent across cells** — the dominant sign (negative) holds in
   **14 of 15**, against shootout's 15/15. One band disagrees with the rest.
2. **The sign is unresolved almost everywhere.** A player-level cluster bootstrap of the median delta
   clears zero in only **3 of 15** cells. The lone positive cell's CI is [0.0, 4.0]. Twelve of
   fifteen signs are noise.
3. **It does not survive out of sample** — 10/13 against shootout's 14/14.

> **Correction, 2026-08-22.** This section previously led with a fourth claim: that the direction
> *inverts at ordinary lines*, with `q > r` at 6.5, 20.5 and 24.5 receiving yards. Those crossings
> are real as raw counts — `validate.py` finds 15 of them for blowout_loss — but **not one clears
> two standard errors**, and neither does shootout's single crossing at 6.5 yards. Measured against
> sampling error the test does not separate the two scenarios at all, so it cannot be what
> disqualifies one of them. It is now reported as evidence and explicitly not gated on; had it been,
> it would have rejected shootout on a 1.3-point wobble at a line ~88% of player-games clear either
> way. The disqualification rests on 1 and 3 above, each sufficient alone.

The first correction to this section rewrote the *test* into median space, where all of this is
invisible, and called the defect fixed. It was not: median-space and probability-space disagree, and
`edgectl` consumes the latter. Moving a test away from the failure is worse than leaving it, because
the failure then looks resolved.

**What would un-gate it:** defining the scenario on play-by-play — time remaining crossed with score
differential — rather than final margin. Which is precisely what this whole result argues for.

Shootout passes: positive in 15/15 cells and 14/14 out of sample, resolved in 11/15.

These numbers are no longer typed here by hand. `fit_conditionals.py` runs `validate.py` on every
fit, writes the measured note into the artifact, and **fails if the evidence and the recorded
verdict disagree** — so a scenario cannot quietly stop qualifying and keep its flag. The bootstrap
records its seed and resample count in the artifact, which is why resolution reads 11/15 here
against the 10/15 originally reported: one cell sits near the boundary and the earlier figure came
from an unrecorded seed. Reproducible now, rather than merely repeatable.

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
any line, and the interval is computed at query time reusing the hit-rate layer's Wilson code.

Two corrections from the audit, both now in effect:

- **Intervals are built on effective sample size, not raw count.** Cells pool repeat players, so
  rows are not independent. Each cell now carries a measured design effect (ANOVA ICC over players):
  median **1.10**, max 1.67, so `n_eff/n ≈ 0.91`. Intervals widen ~9%, moving lower bounds down by a
  median of 0.23 points. 55 of 64 cells are discounted. Real, but it changes essentially no verdicts
  — an order of magnitude less important than the blowout_loss issue above.
- **Probabilities are clamped inside what a finite sample supports.** A quantile table's endpoints
  are the extreme values observed, so a line outside that range returned exactly 0 or 1 — and since
  `hits` then equalled `n`, Wilson reported things like `[0.987, 1.000]` on a cell where 2% of
  observations disagreed. Estimates are now bounded to `[1/(2n), 1 − 1/(2n)]`; mid-range values are
  untouched.

---

## Data note

`target_share` in nflverse only starts in 2009, but raw `targets` reaches back to 2005, so share is
computed from targets and team-week totals. Validated against the precomputed column where both
exist: **mean absolute difference 0.0015**.

`snap_counts` only exists from 2012 — Pro-Football-Reference's snap data begins there. Any analysis
depending on snap share is limited to 2012 forward.

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

`fit_conditionals.py` · 37,078 receiving + 37,078 reception + 5,726 passing player-games, 2009–2025 · 267 cells published, 45 dropped for n < 100

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
   **14 of 16**, against shootout's 16/16. Two bands disagree with the rest.
2. **The sign is unresolved almost everywhere.** A player-level cluster bootstrap of the median delta
   clears zero in only **6 of 16** cells, against shootout's 12.
3. **It does not survive out of sample** — 12/15 against shootout's 15/15.

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

## 4. Pass rate over expected separates better than shootout, and is gated anyway

`analysis/proe.py --gate1` · play-by-play 2009–2025, 8,862 team-weeks

PROE is nflverse's `pass_oe`: actual pass minus `xpass`, the modelled probability the play is a
pass given down, distance, score differential and time. Its team-week mean is pass tendency with
game script already divided out — which is the only reason it is worth a 300 MB table, since raw
pass rate is derivable from the weekly file and is mostly game script.

**It is not a re-measurement of what the grid has.** Against the `shootout` indicator, r = +0.098;
against team margin, +0.020. `xpass` did divide out the script.

**It persists**, prior-to-realized r = +0.429, so there is something to forecast.

**It separates better than the scenario we ship.** `q − r` at a 52.5-yard line is +0.140 across
all games, against shootout's +0.09 to +0.12 — and it holds *within* projected-target bands,
growing with volume: +0.052 at 0–4 targets, +0.096 at 4–6, +0.181 at 6–8, +0.217 at 8–11. That
rules out the obvious objection, that it is the targets axis under another name; a duplicate would
collapse inside the bands rather than widen.

The mechanism is visible in the coefficients. Prior PROE regressed on receiving yards is null on
its own (t = 0.15) and stays null with projected targets controlled. The tendency reaches yards
**through volume**, not around it, which is the volume-over-efficiency thesis rather than a
contradiction of it — and it is why a direct regression is the wrong test for a scenario. The
decomposition's path is prior PROE → realized PROE → separation in `q` and `r`, and collapsing
those two steps into one regression destroys the middle one.

**Gated off regardless.** It fails out of sample in one cell of sixteen — 6–8 targets, +3 to +6 pt
trend: +14.5 yards of separation in 2009–2021, −0.5 in 2022–2025 on 65 observations. Every other
criterion clears, by wider margins than shootout: consistent in 17/17 cells against 16/16,
resolved in 13/17 against 12/16. The failure is a half-yard median gap straddling zero, which is
noise rather than a reversal.

It is gated because the rule requires every out-of-sample cell and was written before this
scenario existed. Loosening it here would be fitting the bar to the answer, which is the failure
this whole section of the pipeline was built to prevent.

**The defensive half — the "funnel defense" — failed the cheap gate and was never fitted.**
Opponent-induced PROE persists at only r = +0.124 and its prior form is null on yards (t = 1.49).
A funnel is measurable after the fact and not forecastable, so there is nothing to condition on.

**Accepted, 2026-08-22.** The operator has accepted this specific failure and `pass_heavy` is
priceable for receiving yards and receptions. The rule is unchanged and still returns False — the
artifact records both `rule_says: false` and the override — and `edgectl` prints the failing cell
whenever the scenario is priced, and states whether *your* wager falls inside it. That is the
difference between an exception and a softer bar: an exception costs only the scenario it names,
while a softened rule stops discriminating for everything measured afterwards.

**What would settle it properly:** more evidence in that one cell. The other candidate — an
out-of-sample test accounting for magnitude rather than sign alone — was tested and rejected. See
below.

### The magnitude-aware out-of-sample test was tried, and it does not work

`pass_heavy` fails on a single out-of-sample cell whose test-half delta is −0.5 yards on 65
observations: a sign flip that is plainly noise. The obvious fix is to stop counting a near-zero
flip as a disagreement — classify each cell as agree, disagree, or *uninformative*, where
uninformative means the test-half delta's player-clustered bootstrap interval covers zero, and
require zero disagreements rather than universal agreement.

Tested on the two scenarios whose verdicts are already settled, **before** applying it to the one
it would have admitted:

| scenario | sign-only | three-way (agree / disagree / uninformative) |
|---|---|---|
| `shootout` | 15/15 | 15 / 0 / 0 |
| `blowout_loss` | 12/15 | 12 / **0** / 3 |
| `pass_heavy` | 15/16 | 15 / **0** / 1 |

Every one of `blowout_loss`'s three disagreements is within noise. Under the new criterion all
three scenarios pass, including the one gated for failing this very test. **The criterion stops
discriminating**, and a gate that cannot fail is not a gate. Rejected; the sign-only rule stands
and `pass_heavy` stays gated.

The exercise did establish something worth keeping, which is that **the existing test is weaker
than it appears.** `shootout`'s 0/15 against `blowout_loss`'s 3/15 is roughly p = 0.1 on a
binomial — it discriminates partly by counting coin flips as evidence. The honest strengthening
is more held-out seasons, which is a matter of waiting rather than of choosing a better threshold.
Recorded here so the limitation is visible rather than implied by a passing grade.

**What would un-gate `blowout_loss`:** defining the scenario on play-by-play — time remaining crossed with score
differential — rather than final margin. Which is precisely what this whole result argues for.

Shootout passes: positive in 16/16 cells and 15/15 out of sample, resolved in 12/16.

These numbers are no longer typed here by hand. `fit_conditionals.py` runs `validate.py` on every
fit, writes the measured note into the artifact, and **fails if the evidence and the recorded
verdict disagree** — so a scenario cannot quietly stop qualifying and keep its flag. The bootstrap
records its seed and resample count in the artifact, so the figures are reproducible rather than
merely repeatable.

**The window was extended from 2014 to 2009 on 2026-08-22**, to publish the one cell that mattered
most and was missing: 8–11 projected targets with a rising role. That is high volume meeting climbing
usage — the corpus's "usage vacuum", the strongest edge it claims — and it held 97 observations
against a floor of 100. The trade is era dilution, measured before it was taken: cells that already
published move by at most 2.5 points of `P(>52.5)`, most by under one, and the effect strengthens
rather than weakens (15/15 → 16/16 consistent, 14/14 → 15/15 out of sample). Merging 8–11 with 11+
would have published the same cell for free, but the bands do not behave alike: at a 100.5 line the
11+ band's *no-shootout* rate (0.289) exceeds 8–11's *with-shootout* rate (0.277), so pooling
understates a 12-target projection by roughly 11 points on `q`.

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

## 5. Passing yards fits, and its gate is weaker than the receiving one

`fit_conditionals.py` · 5,726 usable QB game-weeks, 2009–2025 · 63 cells

The grid now fits two outcomes. The opportunity axis is not shared: a pass-catcher competes for a
fixed pool of team targets so his opportunity is a *share* of it, while a quarterback takes
essentially all his team's attempts and has no share to hold. Passing therefore conditions on the
QB's own prior attempt volume, with a trend measured in attempts rather than share points.

Passing uses a coarser **4×3** grid against receiving's 5×4. That is forced: about **32 starting
quarterbacks a week against 237 pass-catchers**, a structural ceiling no amount of fetching moves.
The sub-28-attempt band drops entirely, which is correct — those are relief appearances.

**All three scenarios validate for passing yards, and one of them is gated for receiving.**

| | receiving | passing |
|---|---|---|
| `shootout` | validated · 16 cell pairs | validated · 10 |
| `pass_heavy` | **gated** · 15/16 out of sample | validated · 10 |
| `blowout_loss` | **gated** · 14/16 consistent | validated · 7 |

**Read that with the caveat, not as three new green lights.** The separations are real and
comparable once scaled to each outcome's own baseline — `shootout` +23.0% of a 234-yard baseline
against +28.8% of a 26-yard one, `pass_heavy` +23.7% against +30.8%, `blowout_loss` −12.4% against
−16.3%. These are the same effects at the same rough strength.

What differs is the evidence available to *falsify* them. `qualifies()` demands consistency in
every cell and agreement in every out-of-sample cell, and does not scale with cell count — so a
10-pair grid whose out-of-sample test sees 5 cells is a much easier bar than a 16-pair grid seeing
15. `blowout_loss` is negative in 14 of 16 receiving cells and 7 of 7 passing cells: the effect is
identical and only the larger grid is big enough to show it wobbling.

That is a limitation of the rule, recorded rather than tuned away — the same decision taken over
the magnitude-aware out-of-sample test in §4. Scaling the bar to cell count is a real open
question and should be settled on its own, not while it would change a verdict.

## 6. Receptions needed a different instrument, not a different bar

`fit_conditionals.py` · same 37,078 player-games as receiving yards · 102 cells

Receptions reuse receiving yards' rows and opportunity axis exactly; only the outcome column
differs. The verdicts do not match, which is the point of gating per outcome.

**The first fit said the effect was absent, and it was wrong.** `shootout` came out consistent in
4 of 16 cells and resolved in 0 of 16 — against 16/16 and 12/16 for yards, on identical games.

The cause is that `validate.py` measured location with the **median**, and receptions are a count
running 0–21 with a cell median of 2 to 5. **Twelve of sixteen cells had a median delta of exactly
zero** while every one of their means was positive. A real shift of half a reception cannot move a
median with that little resolution. The instrument was blind, not the effect absent.

Switching to the mean for discrete outcomes was checked for self-service before adoption, the same
way §4's rejected criterion was:

| | consistent (median → mean) | out of sample (median → mean) |
|---|---|---|
| receiving `shootout` | 16/16 → 16/16 | 15/15 → 15/15 |
| receiving `blowout_loss` | 14/16 → 15/16 | 12/15 → 13/15 |
| receiving `pass_heavy` | 17/17 → 17/17 | 15/16 → 15/16 |
| passing (all three) | unchanged | unchanged |
| receptions `shootout` | 4/16 → **16/16** | 2/15 → **15/15** |

**No settled verdict moves.** Both gated scenarios stay gated, passing is untouched, and receptions
still fails two of its three scenarios. It fixes a blind instrument rather than lowering a bar. The
median is kept for yardage, where it is the more robust choice against a long right tail that
single-digit counts do not have.

**A second discrete correction, smaller and quieter.** Reception lines are half-integers and a count
has no probability mass between its values: `P(X > 3.5)` is exactly `P(X > 3)`. Sampling that
distribution at 2% quantile steps and interpolating toward the next integer invented mass that
cannot exist — measured at up to **1.44 percentage points** of error, bounded by half a step exactly
as theory predicts. Discrete cells now store an exact CDF (about twelve points) and are read without
interpolation. Continuous outcomes still interpolate, and a test pins both halves.

**Verdicts.** `shootout` validates. `blowout_loss` fails at 8/16 consistent — conditioning on
projected targets absorbs most of what a blowout does to a catch *count*, while leaving the yardage
effect intact. `pass_heavy` fails out of sample in one cell of sixteen, **the same cell that fails
it for receiving yards** — 6–8 targets, +3 to +6 pt trend, where a +14.5-yard and +1.22-reception
effect over 2009–2021 goes to −0.5 and −0.02 across 2022–2025 on 65 games. Those are not two
independent failures: receptions and receiving yards are measured on the same player-games, so it
is one failure seen twice.

## Data note

`target_share` in nflverse only starts in 2009, but raw `targets` reaches back to 2005, so share is
computed from targets and team-week totals. Validated against the precomputed column where both
exist: **mean absolute difference 0.0015**.

`snap_counts` only exists from 2012 — Pro-Football-Reference's snap data begins there. Any analysis
depending on snap share is limited to 2012 forward.

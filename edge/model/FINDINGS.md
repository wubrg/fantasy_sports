# Findings

> **Review notice, 2026-08-23.** An independent reviewer reproduced every published verdict and
> then found defects these sections do not reflect: §4's `pass_heavy` is 90% a volume identity and
> its stated defence is the identity's signature; §6's self-service check has since gone stale with
> three permissive flips; §8's statistic contains its own outcome and manufactures a 3× ratio under
> a pure null; and the decomposition is miscalibrated because `q` and `r` are pooled over the
> variable used to form `s`. Two of the author's own worries — multiple comparisons and the
> clustering choice — were tested and **refuted in his favour**. See
> [`../docs/reviews/2026-08-23-adversarial.md`](../docs/reviews/2026-08-23-adversarial.md) before
> relying on any section below.

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

`fit_conditionals.py` · 37,078 receiving + 37,078 reception + 13,388 rushing + 5,726 passing player-games, 2009–2025 · 351 cells published, 57 dropped for n < 100

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

> **`pass_heavy` was withdrawn for receiving yards on 2026-08-23.** Holding *realised* targets
> fixed, its coefficient collapses from t = 14.25 to **t = 1.97** — 90% of the separation is
> volume the projection failed to anticipate, so it measures the inadequacy of this project's own
> projected targets rather than a market inefficiency. The defence recorded above — that separation
> holds within projected-target bands and *widens* with volume — is the identity's signature, not a
> refutation. It survives volume conditioning for receptions (t = 9.17), rushing (t = −9.80) and
> passing (t = 5.05) and is kept there; receiving yards is targets × catch rate × yards-per-catch
> and this scenario acts only on the first term. The override granted for it is withdrawn with it.

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

## 7. Rushing inverts the sign, and that is the point

`fit_conditionals.py` · 13,388 RB game-weeks, 2009–2025 · 84 cells

**RB only.** A quarterback's carries are scrambles and kneels and a receiver's are jet sweeps —
median 3 and 1 against a back's 8 — so they are not a share of the same designed-run pool. Pooling
them would repeat the error §5 avoided by giving quarterbacks their own axis. The corpus's
"Konami Code" mobile-QB angle is a different model and is not this one.

**The axis was measured, not borrowed.** Carry-share trend has an sd of **0.131** against target
share's ~0.05: backfields consolidate and split far more sharply than target trees do, so the
+6-share-point target threshold does not transfer. Bands sit at the same multiples of their own sd
that the target bands sit at in theirs.

The trend is a **much stronger** signal here than it is for receiving. RB rushing yards on baseline
carry share plus trend, errors clustered by player: **β = 48.74, t = 18.25, ΔR² = +0.0268** — an
order of magnitude more explanatory power than target-share trend manages for receiving yards
(+0.0032). On carries themselves it is stronger still (ΔR² = +0.0418), which is the
volume-over-efficiency thesis appearing where it should.

**Verdicts, and the sign flip.**

| scenario | rushing | direction |
|---|---|---|
| `pass_heavy` | **validated** · 13/13 consistent, 11/11 out of sample | **negative** |
| `blowout_loss` | fails by one cell on each criterion · 11/12, 8/9 | negative |
| `shootout` | fails · 10/14, resolved 2/14, 5/13 out of sample | — |

`pass_heavy` is the cleanest validation in the grid, and it is **negative**: a team throwing more
than the situation called for produces *fewer* rushing yards. The same scenario helps a receiver,
helps a quarterback, and hurts a back — the carries went somewhere. That sign was predicted before
the fit and measured after it, rather than assumed either way. At 14 projected carries with a
rising role, a 74.5-yard line runs `q` = 12.3% against `r` = 34.3%.

`shootout` failing is informative rather than disappointing: a high-scoring game says points *were*
scored, not *how*. They can arrive through the air or on the ground and the total alone cannot tell
which. `pass_heavy` measures the how directly, and validates.

> **Correction, 2026-08-23.** This paragraph said "a high **posted** total". `shootout` is defined
> on the **realized** final total (`games.csv` `total`, not `total_line`), so the wording named the
> wrong quantity. The reasoning survives; the sentence did not. Related and worth stating: because
> a receiver's own yards feed his team's points, `shootout` is mildly self-referential — splitting
> it, own-team points over 27 is worth +7.52 yards while opponent points over 27 is worth +3.44.

`blowout_loss` misses each criterion by exactly one cell, with the right direction — a team losing
badly abandons the run. The closest miss in the grid.

## 8. Out-of-sample failures concentrate where the effect was already noise

`fit_conditionals.py` + `validate.py` · 141 testable cells across all four outcomes

The gate is per SCENARIO: if any one cell fails out of sample, nothing in that scenario can be
priced. That granularity turns out to be wrong, and the evidence is uniform across the grid.

| | held out of sample | median &#124;fit effect&#124; |
|---|---|---|
| cells that **failed** | — | **1.0** |
| cells that **held** | — | **5.0** |
| low-volume opportunity bands | 82/99 (83%) | 2.0 |
| high-volume opportunity bands | 38/42 (90%) | 12.5 |

**A cell fails out of sample when its effect was too small to measure in the first place.** The
failing cells carry a fifth of the effect size of the holding ones.

The consequence is concrete. `blowout_loss` is gated for receiving yards on 12/15 out of sample —
but **all three of its alpha-band cells held**, at −13.0 → −16.5, −14.0 → −6.0 and −17.5 → −14.0
yards. Every failure sat in a low-volume band whose fit effect was ±1–2 yards to begin with. A
wager on an alpha receiver in a blowout script is supported by three cells that all held, and is
blocked by wobble in slices nobody would bet. `pass_heavy` shows the same shape: one failure in the
6–8 band, all four alpha cells holding at +6.5 → +25.5, +14.0 → +25.5, +13.0 → +16.5 and
+7.5 → +16.5.

**What is not yet decided** is whether to act on this. Gating per cell would price the alpha-band
wager with no override at all, and it follows the evidence — but it is also selection on the cells
that passed, and that is only defensible because failures track tiny effects rather than falling at
random. That reasoning has to hold for every scenario measured afterwards, not just the ones in
front of us today.

Three options are on the table: report each cell's own held-out record at pricing time and leave
the gate alone; gate per cell; or gate per cell with a minimum effect-size floor that encodes the
pattern above explicitly. Recorded here so the measurement survives the decision being deferred.

## 9. Four belief signals gated; one survived, and the best one is not a scenario

`analysis/signals.py --gate1` · play-by-play 2009–2025, 8,862 team-weeks

Four candidates from data already cached, each held to the gates PROE went through.

| signal | persists | vs shootout | vs margin | ΔR² (realized) | separation | verdict |
|---|---|---|---|---|---|---|
| `success_rate` | +0.318 | +0.302 | +0.389 | +0.0142 | +0.128 | **fitted** |
| `chunk_rate` | **+0.141** | +0.274 | +0.257 | +0.0242 | +0.150 | rejected |
| `tempo` (formation) | +0.765 | +0.038 | −0.290 | +0.0005 | +0.020 | rejected |
| *plays run* (real pace) | **+0.138** | +0.136 | — | +0.0143 | — | rejected |

**`chunk_rate` has the largest effect in the table and fails anyway.** It persists at +0.141 —
the same range as the defensive pass funnel rejected at +0.124 in §4. Explosiveness is measurable
after the fact and not forecastable, so there is nothing to condition on before kickoff. Rejecting
it is the precedent set by that earlier decision, applied to a signal that is more tempting.

**`tempo` fails for the opposite reason, and is misnamed.** It persists at +0.765 — the most
forecastable quantity measured anywhere in this project — and does nothing: prior form null on
receiving yards (t = −1.34), two points of separation.

> **Correction, 2026-08-23.** What this measures is the shotgun-or-no-huddle rate, which is a
> **formation choice, not pace**: its correlation with plays actually run is **+0.064**, and shotgun
> is near-universal now (mean 0.67). The name promised a test of tempo and the metric did not
> deliver one.
>
> Measured properly, **plays run** matters a great deal — ΔR² = **+0.0143** beyond projected
> targets at t = 21.06, twenty-six times what the formation metric managed, and it survives
> conditioning on script and success rate. But it **persists at only +0.138**, the same range as
> `chunk_rate` (+0.141) and the defensive funnel (+0.124). A team's recent play volume barely
> predicts this week's, and the prior form actually turns negative (t = −5.97).
>
> So the rejection stands and the reason changes: the formation metric fails on **signal**, and
> real pace fails on **persistence**. Two different failures behind one verdict, and the original
> entry implied a test that had not been run.

**`success_rate` was the one that needed a real test**, because it is far more entangled with game
script than PROE: r = +0.302 against the shootout indicator and +0.389 against margin, where PROE
manages +0.098 and +0.020. Teams that succeed win, so much of the raw effect could have been the
script the grid already conditions on. Measured directly: shootout and blowout together explain
ΔR² = +0.0127 of receiving yards; adding success rate takes it to +0.0196, an extra +0.0068 at
t = 17.23, with the coefficient falling from 54.25 to 41.47. About a quarter of the raw effect is
overlap and three quarters is not.

Fitted as `efficient_offense` (success rate > 0.46, roughly the 66th percentile, base rate near
shootout's). It **validates for passing yards only** — 10/10 consistent, 6/6 out of sample — and
misses by one cell for receptions and rushing, by three for receiving. It is positive for all four
outcomes including rushing, unlike `pass_heavy`: an efficient offence sustains drives and the back
eats too, where a pass-heavy one takes his carries away.

### The snap counts are a better axis than the one in use

`snap_counts` has been fetched since the ingest shipped and read by nothing. It is not a scenario —
a snap-share trend is known before kickoff, so it is a conditioning variable, and the honest test is
whether it beats the target-share trend the grid already conditions on.

It does. On 28,054 observations carrying both (2012–2025, 86% of weekly rows match a snap record):

| trend axis | β | t | ΔR² |
|---|---|---|---|
| target share (in use) | 45.50 | 9.49 | +0.00340 |
| **snap share** | 2172.18 | **13.85** | **+0.00530** |
| both | — | — | +0.00605 |

Snap share carries **56% more explanatory power**, and the two are only about 30% overlapping —
which is the corpus's Tier 2 example measured, since it describes a role change in *snaps*
("70% of snaps last week, up from 40%") rather than targets.

Not acted on here. Switching the axis costs the 2009–2011 seasons, drops the 14% of rows with no
snap record, changes every cell in the grid and forces a full revalidation. Using both would beat
either, and multiplies cell count — the density constraint that ruled out a fourth axis in §4.
Recorded as a measured option, not a pending change.

## 10. The usage vacuum — the corpus's strongest claim — does not survive measurement

`analysis/signals.py --gate1` · injuries 2009–2025, 6,882 team-weeks with a player Out

`edge-of-vigor.md` calls this Tier 3, "The Error", worth **+15 percentage points** of probability
and the strongest edge it describes: *"The WR1 just got ruled out 5 minutes ago. The WR2 (who is
priced at 30 yards) will now see 10 targets. The math is simply broken."*

Measured as the baseline target share of the teammates listed **Out** — a star missing is worth
several times a rotational player missing, so the signal is vacated *share*, not a count of bodies:

| tested on | β | t | ΔR² | q − r at 52.5 |
|---|---|---|---|---|
| every pass-catcher | 11.85 | 4.56 | +0.00054 | **−0.004** |
| **the top remaining receiver** | 3.32 | 0.44 | +0.00003 | **−0.055** |

**Statistically significant across all receivers, practically nil, and negative where the claim
actually lives.** A 15-point vacuum moves the average pass-catcher by 0.3 yards. The top remaining
receiver — the WR2 the framework is describing — clears 52.5 yards **50.1%** of the time with a
large vacuum against **55.6%** without, and averages 61.4 yards against 63.9. He does worse.

**The mechanism is that the vacated share does not concentrate.** Fifteen points of share going out
delivers **+0.7 points** to the average remaining receiver: the targets disperse, or are not thrown
at all. The framework assumes the opportunity lands on one man, and it does not.

Two readings are consistent with the negative sign on the alpha, and this data cannot separate
them. Defences can commit to the remaining threat once the other one is gone, which would make the
extra looks worth less than they appear. Or a team missing a 15-point receiver is often depleted
more broadly — a backup quarterback, more than one absence — and the whole offence is worse.
Either way the net measured effect on the intended beneficiary is negative, so the signal is
rejected at Gate 1 and never fitted. A scenario whose q equals its r carries no information, and
`RequiredScenarioProb` refuses to price one.

**A caveat on scope, stated because it bounds the claim.** "Out" on the injury report is a fact
about availability; it is not the framework's *"ruled out 5 minutes ago"*. A genuinely late scratch
may behave differently from a Friday designation, because the market has less time to move. This
data cannot distinguish them, and the historical prop lines that would settle whether books price
the vacuum are not available.

The injuries table is kept in the ingest regardless. It is small, and it is the only source here
for who did not play — which is a fact worth being able to check independently of whether it
predicts anything.

## 11. The cohort mismatch is the larger defect, and fixing it breaks the gate

Findings C1 and C3 of the [adversarial review](../docs/reviews/2026-08-23-adversarial.md) were
triaged as one item: `q` and `r` are pooled over the posted total that `s` is derived from (C1),
and `q` is a cohort probability judged against a player-specific line (C3). Measured, they are not
equally serious, and the review's proposed fix addresses the smaller one.

**Both reproduce.** Held out on 2022–2025, fitting on 2014–2021, with the line placed at each
player's own prior mean. Error is predicted minus actual, so negative means the grid *underrates*
the over:

| stratified by posted total | error | | stratified by the player's own baseline | error |
|---|---|---|---|---|
| 0–42 | +1.93pp | | under 35 yds | +2.75pp |
| 42–45 | −0.23pp | | 35–50 | −2.15pp |
| 45–48 | −0.96pp | | 50–70 | −5.84pp |
| 48–51 | −1.15pp | | 70+ | **−8.01pp** |
| 51+ | **−3.12pp** | | | |

The vig cushion at −110 is 2.38pp. **C3 is four times it**, and it is monotone: the grid overrates
small players and badly underrates big ones. That is the trap the review described — a line set
near a 70-yard player's median sits far out in a cohort whose median is 43, so `q` collapses and
the tool returns `beyond-your-read` on exactly the wagers a person actually places.

**Aggregate calibration hides all of it.** Pooled over every stratum the grid is accurate to
−0.00pp. Averaging over the strata cancels the errors, and a first pass at this measurement
reported the grid as well-calibrated for that reason. Calibration has to be measured *within* the
variable suspected of carrying the error or it cannot find it.

**The fix is normalization, not the proposed axis.** Storing the distribution of yards relative to
the player's own prior mean, and conditioning on a baseline tier:

| | cells | worst by posted total | worst by own baseline |
|---|---|---|---|
| today | 33 | 3.12pp | 8.01pp |
| + posted-total axis (the review's proposal) | 58 | **1.47pp** | 8.33pp |
| ratio to own mean + baseline tier | 8 | 2.68pp | **1.78pp** |

The posted-total axis fixes C1 and leaves C3 untouched. The normalization fixes C3 and *also*
takes C1 from 3.12 to 2.68 without an axis at all — because part of what looked like posted-total
miscalibration was the cohort mismatch showing up correlated with game totals. C1 and C3 were
correctly triaged as one defect; the review named the weaker half as the cure.

**The result generalizes.** Worst-stratum error by own baseline, before → after, on a temporal
split and a random one:

| outcome | today (time) | today (random) | proposed (time) | proposed (random) |
|---|---|---|---|---|
| receiving_yards | 8.01pp | 10.48pp | 1.78pp | 4.67pp |
| receptions | 9.97pp | 9.78pp | 2.13pp | 2.59pp |
| rushing_yards | 9.37pp | 10.39pp | 2.53pp | 4.35pp |
| passing_yards | 4.70pp | 11.19pp | 4.68pp | 4.92pp |

### The part that stops this from shipping

**Cell count is the gate's strength, and the axis set sets the cell count.** Collapsing to 8 cells
made five scenarios pass that fail today — including `pass_heavy` for receiving yards, withdrawn
one commit earlier as a volume identity. A rule reading "the direction holds in every cell" is
easier to satisfy the fewer cells there are, so an axis design chosen for calibration silently
re-tunes the gate.

Requiring the baseline tier and then taking the axis set with the *most* cells subject to a
coverage floor avoids that, and yields more cells than today for every outcome — 58, 52, 29 and 21
against 33, 33, 28 and 19. The gate gets stricter. And then:

```
receiving_yards  shootout   today     positive in 16/16 cells; 12/16 bootstrap-resolved; 15/15 out of sample
                            proposed  positive in 29/29 cells; 15/29 bootstrap-resolved; 20/24 out of sample
```

**`shootout` for receiving yards — the only thing the tool can price today — fails the corrected
grid.** Not on direction, which is now 29/29 rather than 16/16. On the out-of-sample test, which
demands the direction hold in *every* held-out cell: 15/15 becomes 20/24.

The effect did not weaken. The gate did. `qualifies()` is **not scale-invariant**: for a real
effect with per-cell error probability `e`, an all-cells rule passes with probability
`(1−e)^k`, which falls as `k` rises. Re-cutting the same data into more cells therefore rejects
findings it previously accepted, and the number of cells is a design choice, not evidence.

This is [task #9](../docs/reviews/2026-08-23-adversarial.md), per-cell gating, arriving as a
blocker rather than an improvement. **The calibration fix cannot land until the gate is
scale-invariant**, because shipping both at once would change two methods in one step and leave
no way to attribute the verdict changes to either.

Nothing above is in the artifact. The two quantities are plumbed; the axis change is not made.

> **Unblocked 2026-08-23 by §12**, which replaced the whole-scenario gate with a per-site one.
> `k` no longer appears in the rule, so re-cutting the grid cannot by itself reject anything. The
> axis change described here is now free to land on its own evidence, and is the next piece of
> work rather than a blocked one.

## 12. The gate was measuring the grid's shape, not the evidence

Section 11 could not ship because fixing the calibration re-cut the grid into more cells, and the
gate rejected `shootout` for receiving yards as a result — while its direction *improved* from
16/16 to 29/29. That is not a close call about one scenario. It is a defect in the rule.

**The old rule was not scale-invariant.** `qualifies()` required the direction to hold in *every*
cell and *every* out-of-sample cell. For a real effect with per-cell error probability `e`, an
all-cells rule passes with probability `(1−e)^k`. As `k` grows that falls to zero — so re-cutting
the same data more finely rejects findings it previously accepted, and `k` is a design choice about
axes rather than anything the data said.

The same flaw is why the bootstrap was *reported but never gated on*. §3 recorded the reason
plainly: "requiring all of them would reject a real effect." That was correct, and it was the
scale-dependence talking. The test was sound; the conjunction over `k` cells was not.

### The gate now rules on a site

A **site** is one (opportunity band, trend band) coordinate of one scenario — the q cell and the r
cell together. It is the unit a price is actually formed at, so it is the unit to gate. A site is
priceable only if, **at that site**:

| test | requirement |
|---|---|
| direction | its delta agrees with the scenario's dominant sign |
| resolution | the player-clustered bootstrap interval clears zero |
| out of sample | that sign holds in the seasons after 2021 |

`k` does not appear. Adding axes no longer changes any surviving site's verdict, and the bootstrap
becomes gateable for the first time — per site, requiring resolution is just "this effect is
separated from zero here", with nothing to compound.

Absence of held-out evidence is a **refusal, not a pass**. 21 sites are refused on that alone,
15 of them in passing yards, where there are ~32 quarterbacks to fill cells. "We never checked" is
not a reason to price something.

### A scenario must have a direction before its sites can agree with one

A site is judged by agreement with the scenario's dominant sign, so where that sign is a coin flip
the agreeing sites are simply the lucky half. Receptions under `blowout_loss` leans one way in
**8 of 16** cells; rushing under `shootout` in **10 of 14**. Both would otherwise have contributed
2 priceable sites each.

So the dominant sign must first beat chance — a two-sided binomial test at α = 0.05. This is not
the old rule returning in disguise: the all-cells rule asked whether *every* cell agreed and got
harder to satisfy as the grid was cut finer, while this asks whether the agreement *rate* beats
chance and gets **easier** with more cells, which is how evidence is supposed to behave.

### Is this multiple comparisons with better manners?

It is the obvious charge — one 95% interval across ~30 sites resolves about 1.5 by luck — so it was
measured rather than argued. The scenario label was shuffled across games, preserving its marginal
rate, the player clustering and the season structure, and destroying only its link to production.
12 permutations per scenario, receiving yards:

| scenario | real | under the null |
|---|---|---|
| shootout | 12/16 = 75% | 1.6% |
| blowout_loss | 6/16 = 38% | 1.6% |
| pass_heavy | 12/17 = 71% | 0.5% |
| efficient_offense | 12/17 = 71% | 2.6% |

**The three-test conjunction passes 12 of 774 site-tests under the null — 1.6%.** That is below the
5% a single 95% interval would give, and 20–45× below the real pass rates. The conjunction, not the
granularity, is what does the work.

### What it publishes

118 of 222 sites, against 82 sites inside the four scenarios the old whole-scenario gate passed:

| outcome | shootout | blowout_loss | pass_heavy | efficient_offense |
|---|---|---|---|---|
| receiving_yards | 12/16 | 0/16 vetoed | 0/17 vetoed | **12/17** |
| receptions | 15/16 | 0/16 no direction | 16/17 | **15/17** |
| rushing_yards | 0/14 no direction | **6/12** | 10/13 | **10/14** |
| passing_yards | 6/10 | 5/7 | 5/10 | 6/10 |

It is both more permissive and more demanding, which is the point. `efficient_offense` was gated
everywhere for missing by a cell or two; 12, 15 and 10 of its sites hold on their own evidence.
Passing yards passed all four scenarios wholesale and now loses 4–5 sites in each, having never
been checked out of sample where the held-out seasons are thin.

### What an operator can still do

The statistical gate is measured, so there is nothing recorded for it to drift from. Two human
levers remain, and both are narrower than before:

- A **veto** removes a pairing for a reason no test can see. `receiving_yards`/`pass_heavy` (a
  volume identity, §11) and `receiving_yards`/`blowout_loss` (needs a play-by-play definition, not
  final margin). It can only ever subtract.
- An **accepted failure** now names *one site* by its four coordinates, and is checked against that
  site's measured verdict. It previously attached to a whole scenario, so the warning fired on
  every wager anywhere in it — including the fifteen cells that passed cleanly. A warning that
  appears when it does not apply is one a reader learns to skip.

The purely statistical verdicts that used to be recorded by hand — "holds in only 13 of 16 out of
sample", "misses each criterion by a single cell" — are gone, because that is exactly the judgement
the per-site gate now makes, at the granularity a wager is priced at.

## Data note

`target_share` in nflverse only starts in 2009, but raw `targets` reaches back to 2005, so share is
computed from targets and team-week totals. Validated against the precomputed column where both
exist: **mean absolute difference 0.0015**.

`snap_counts` only exists from 2012 — Pro-Football-Reference's snap data begins there. Any analysis
depending on snap share is limited to 2012 forward.

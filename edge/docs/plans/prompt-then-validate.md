# Prompt to find candidates, tool to validate them

A work plan, not a design doc. Every phase has a gate that can fail, and failing one is a result.

## Why this is worth building

[FINDINGS §16](../../model/FINDINGS.md) measured the ceiling. Replace `s` with the truth — 1 if the
scenario occurred, 0 if not — and the screened tail strategy earns **+7% to +18%** at a 6% hold.
Nothing on the belief side can beat knowing the answer, so that bounds the whole problem.

The fitted belief model **loses 10–25%** on the same wagers. Everything between those two numbers
is what a better estimate of `s` is worth, and it is the only thing worth working on.

The bar is explicit:

```
s_you − s_book  >  P_book × hold / (q − r)
```

which comes out at **+0.03 to +0.16** on a good site at depth. A read that genuinely knows a game
is fifteen points likelier than its base rate to be a shootout clears it. Whether such a read
exists is the question this plan tests.

## The constraint that shapes everything

**An LLM cannot be backtested on historical NFL games.** It has seen them. Asking a model whether
Week 12 of 2024 was likely to be a shootout is not a forecast, and no amount of careful prompting
makes it one. Every historical number a prompt produces about a played game is uninterpretable.

So the test is **forward-only**. That is slow, and it is the only honest option. The plan below is
arranged so that everything except the scoring can be built and verified now, and the scoring
starts the week the season does.

## The rule the prompt must obey

The recorded failures with prompts on this project were: hallucinated coaches, roster updates from
the wrong season, and wagers that did not match the book. All three are the same mistake — asking
the prompt for something that is already written down.

> **The prompt is never asked for a number the tool can compute or a fact the cache contains.**

It is asked for exactly one thing: a judgement about this week's game script, with its reasons
stated as claims that can be checked. Everything else — baselines, trends, target shares, prior
form, base rates, prices — comes from the cache or the board.

---

## Phase 0 — the ceiling · **PASSED**

Oracle bound measured at +7% to +18%. `make backtest-oracle`. Recorded as §16.

Had this come back negative, none of the rest would be worth building, and that is why it was run
first.

## Phase 1 — the shape list · tool only, no prompt

Enumerate every (outcome, scenario, site, depth) worth hunting:

- required `s`-edge below a threshold (start at +0.10)
- cell thick enough to price
- **the site's own held-out calibration**, not the grid's average — `receiving_yards` is
  trustworthy at 2× the line and `rushing_yards`'s baseline half is not

Output is a ranked list of *shapes*, containing no player names. It costs nothing, it constrains
the prompt, and it is the thing that stops the search being "look at everything".

**Gate:** at least a few dozen shapes clear +0.10. If almost nothing does, the strategy is dead
regardless of how good the belief gets.

## Phase 2 — the prompt contract

**Input:** the shape list, this week's schedule, posted totals from the board.

The contract is written out in full, with its output schema and claim taxonomy, in
[`docs/frameworks/belief-probe.md`](../frameworks/belief-probe.md) — the document you actually
paste. It is not restated here.

**Never requested:** player baselines, trends, target shares, historical rates, prices, or the
identity of which player to bet. The tool knows all of those.

## Phase 3 — automatic falsification

Every claim is checked against the cache *before* the belief is used:

| claim about | checked against |
|---|---|
| who is out | `injuries` |
| team form | `signals.prior_form`, `proe.prior_form` |
| which week, which season | supplied by the tool, so it cannot be wrong |

**These are two separate tests and must not be run together.**

| | question | population |
|---|---|---|
| **A — reliability** | does it state things that are false? | every prediction |
| **B — edge** | given a forecast that is not false, does `s` beat the base rate? | predictions that survive |

Test B is conditional on passing the falsifier, and **that is correct rather than a bias**: the
falsifier is part of the strategy, not a diagnostic beside it. In deployment a forecast caught
inventing evidence is discarded before any wager, so the quantity worth estimating is the edge of
the forecasts that survive. Conditioning on a filter you actually apply is evaluating the real
pipeline.

The falsifier is incomplete, and that is recorded rather than assumed away: `personnel` and
`narrative` claims have nothing to check against, so "survived" means *no falsehood was detected*.
Test B's population is therefore slightly generous.

Test A's rate matters on its own terms. A prompt that is sharp when honest but invents a third of
the time yields few usable candidates a week, which is a **coverage** problem — it shrinks the
sample Test B can draw on, and the power table below shows how little slack there is.

Rejected predictions are **logged and settled anyway**, so survivors can be scored against the whole
set on identical outcomes. That measures what the falsifier is worth; a bare rejection count cannot.

## Phase 4 — validate the wager

For a surviving candidate:

1. `player.py` supplies `-baseline` and `-trend`. Never typed.
2. The site's required `s`-edge at the offered line.
3. Compare the prompt's `belief` against base rate + requirement.
4. `edgectl scenario -rungs` against the book's **real** prices.

**Missing input:** prop prices. The board holds moneylines, spreads and totals. Until it holds alt
lines this phase runs on prices typed by hand, one candidate at a time — which is acceptable for a
handful of candidates a week and is not acceptable for a screen.

## Phase 5 — score the belief, without betting

The part that can start immediately, and the reason the plan does not wait on prices.

**Log every prompt belief before kickoff and settle it on whether the scenario occurred.** No stake,
no price, no prop needed. It is a pure prediction log, and it measures the only thing that matters:

- is the prompt's `s` calibrated?
- **does it beat the base rate?** — the requirement is +0.03 to +0.16
- does it beat the fitted belief model, which is the incumbent?

The bet log already records provenance (`stated` against `derived-from-signals`) specifically so
these can be scored apart.

### How fast this can actually decide

The unit here is a **game script**, not a player prop, and there are far more of those than there
are candidates. Every week offers:

| | per week |
|---|---|
| `shootout` (per game) | 16 |
| `blowout_loss`, `pass_heavy`, `efficient_offense` (per team) | 32 each |
| **raw predictions** | **112** |

So 100 in the first week is easy. **It also decides nothing**, and the raw count is misleading
twice over.

**First, they are not independent.** `shootout` is a property of the game and both teams' outcomes
are drawn from the same afternoon. Measured over 2,608 games: `efficient_offense` correlates
+0.109 within a game, `pass_heavy` +0.022, and `blowout_loss` **−0.353** — both teams cannot be
blown out, which makes that pair carry *more* information than two independent draws. For the two
scenarios the belief model covers, a 64-record week is worth about **60 effective** predictions.

**Second, power at that size is poor precisely where it matters.** The table below is the
**corrected** one — the original (retained at the end of this section for the record) was
unreproducible from anything in the repo and wrong three ways the 2026-09-04 review found. This one
comes from `make power-table` (`app/cmd/powersim`), which drives the **shipped** `calib.PairedBrierGain`
and `calib.BootstrapCI` so the table cannot drift from the code that decides the endpoint. It fixes:

- **α.** The old table read the bootstrap lower bound at `alpha=0.05`, which is one-sided **2.5%**,
  not the "one-sided 95%" it claimed. This passes `alpha=0.10` for a genuine one-sided 5% bound.
- **Population.** After the S6 fix the decision rests on `shootout` (16/wk) and `efficient_offense`
  (32/wk) — 48 raw rows a week, not 112 — and a forecaster that abstains freely commits on a
  fraction of them. The `n` column is committed positions at a **generous 40% commit rate**, not raw
  rows × 60.
- **Clustering.** `efficient_offense` rows for a team are correlated across weeks and the bootstrap
  clusters on team-season, not game.

Testing "the prompt beats the reference", one-sided 5%, 500 trials, commit rate 0.40:

| weeks | committed n | edge +0.05 | +0.10 | +0.15 | +0.20 |
|---|---|---|---|---|---|
| 1 | 22 | 10% | 13% | 23% | 33% |
| 3 | 61 | 13% | 26% | 32% | 48% |
| 6 | 121 | 15% | 28% | 46% | 67% |
| 8 | 160 | 19% | **31%** | 58% | 77% |
| 12 | 233 | 17% | 42% | 67% | 87% |
| 18 | 361 | 19% | **49%** | **79%** | 94% |

- **The edge that actually matters (+0.10) never reaches 80% in a season** — 49% at week 18, against
  the old table's 90% at week 8. This is the single most important correction: the decision point was
  chosen as "the first week +0.10 clears 80%", and **there is no such week.**
- **A large edge (+0.15) reaches ~80% only at the end of the regular season (week 18); +0.20 by
  around week 12.**
- **+0.05 is undetectable all season**, and it still clears the +0.03 bar on the best sites. There is
  a real regime where this is profitable and untestable, and no test design removes it.

**This table is an upper bound, for three reasons that all push real power lower:** it models a single
reference, but E1 now requires beating *every* opponent (the hardest binds); it models a perfectly
calibrated forecaster; and its gain-clustering is milder than a real forecaster's persistent
per-team error. Read every cell as "no more than this."

<details><summary>The original (superseded) table, for the record</summary>

| weeks | n | +0.05 | +0.10 | +0.15 | +0.20 |
|---|---|---|---|---|---|
| 1 | 60 | 9% | 21% | 40% | 63% |
| 3 | 180 | 17% | 52% | 85% | 98% |
| 6 | 360 | 30% | 80% | 99% | 100% |
| 8 | 480 | 37% | 90% | 100% | 100% |
| 18 | 1080 | 68% | 100% | 100% | 100% |

It used one-sided 2.5% mislabelled as 5%, `n = weeks × 60` (raw rows, no abstention), game-level
clustering, and a generative model no script in the repo reproduced.
</details>

Three things buy power, and they are worth taking in this order:

1. **Score the stated belief continuously, not in buckets.** A median split throws away the
   ordering. Worth 5–12 points of power, and free.
2. **Include `shootout`.** It has a market number, so those 16 a week are a direct head-to-head
   against a real opponent rather than against a base rate — a harder test and a more informative
   one, since losing it is the strongest evidence available that the read adds nothing.
3. **Have the prompt state its confidence** and weight by it, so its own strong calls are not
   diluted by the ones it was guessing on.

**One scheduling constraint:** `prior_form` needs three prior games, so the fitted belief model
produces nothing until **week 4**. The base-rate test runs from week 1; the head-to-head against
the incumbent starts at week 4. Weeks 1–3 are also when the prompt has least to work with and the
market is weakest, which makes them the most interesting weeks and the least conclusive ones.

## Phase 6 — the decision

### Pre-registered, before any data exists

Four scenarios, several statistics and eighteen weekly looks is a garden of forking paths, and this
project has already been misled three times by pooling alone. So the endpoint is fixed here, on
2026-08-24, with the log empty:

> **Primary endpoint — two claims, and BOTH must pass.** Evaluated at **week 8**, the first point
> the power table puts above 80% for the +0.10 edge that matters. Both intervals are bootstrapped
> clustered by game.
>
> | | claim | statistic | passes when |
> |---|---|---|---|
> | **E1** | it is more accurate than the reference | paired Brier gain, pooled over positions | lower bound of the 95% CI above 0 |
> | **E2** | the wagers it implies would have won | realised edge per unit staked, on rows over the bar, at a 6% hold | lower bound of the 95% CI above 0 |
>
> **Why both.** They are different claims and they can point in opposite directions. A forecaster
> better than the reference by a hair on every row passes E1 with a healthy gain and never disagrees
> by enough to place a single wager — E2 would rest on nothing. One right about a handful of big
> calls and mediocre elsewhere can fail E1 and be the profitable one. Registering E1 alone would let
> "it works" be declared about a source that produces no bets; registering E2 alone would rest the
> whole verdict on the smallest subset in the data.
>
> **E2 is the weaker-powered half and that is accepted.** The bar is what turns a row into a wager,
> and a forecaster abstaining freely — as the contract asks — will put perhaps fifteen to twenty
> rows a week over it, so E2 rests on order 150 wagers by week 8 rather than 480. If E1 passes and
> E2 is merely undecided, that is reported as undecided rather than as a pass.
>
> **Everything else is descriptive.** Reliability, resolution, slope, AUC, per-scenario and
> per-confidence splits, the flagged-versus-rest comparison and survivors-versus-all are all worth
> looking at and none of them is the verdict.

**Amendment, 2026-09-04 (log still empty).** The 2026-09-04 review found two defects in the endpoint
above, both fixable only by changing its definition. Amended here rather than at scoring time because
no data has been observed:

> **E1 is now: beat every opponent, the hardest binds.** "Pooled over positions against the
> reference" scored one auto-picked opponent, and auto prefers the incumbent over the line, so from
> week 4 the two PROE scenarios were scored against the incumbent — which the committed line model
> beats with no football knowledge. E1 now scores each opponent (market, incumbent, line) on its own
> rows and requires the forecast to beat **all** of them; the lowest lower-bound binds and is
> reported. The base rate is a non-binding floor.
>
> **E2 is demoted from a co-equal gate to a diagnostic.** The "both must pass" framing assumed E1 and
> E2 are independent claims. They are not: the probe collects no prop, so a reconstructed wager
> settles on the same game-script outcome that drives E1 — E2 is a rescaling of E1, not a second
> measurement. Its interval was also ~5× too tight, settling each wager at its expected value instead
> of a real 0/1 draw. E2 is now reported as a robustness check with a Bernoulli-settled interval, and
> the **verdict is E1 alone**. E2 being negative or undecided is informative but does not fail the
> endpoint.
>
> **The decision point moves from week 8 to week 18, and the detectable effect from +0.10 to +0.15.**
> Week 8 was chosen as the first week the old table put +0.10 above 80% power. The corrected table
> (above, `make power-table`) shows +0.10 never reaches 80% in a season — 49% at week 18 — so that
> decision point does not exist. The endpoint is re-registered at **week 18** (end of the regular
> season), where +0.15 reaches ~80%; **+0.10 is evaluated and reported but is known to be
> underpowered**, which is itself the finding — a real edge in the +0.05 to +0.10 band is profitable
> and not reliably detectable in one season. The α is now a genuine one-sided 5% (`alpha=0.10` to
> `BootstrapCI`), matching the table.
>
> Reproduce with one command, no hand-assembly:
> `edgectl beliefs score -from-week 1 -to-week 8 -bar 0.10 -hold 0.06`
>
> **Interim looks are permitted and do not stop the trial.** They are for catching a broken harness,
> not for calling a result early.
>
> **The decision weights `shootout` and `efficient_offense`.** `blowout_loss` fails validation
> everywhere it is fitted, and `pass_heavy` is vetoed for receiving yards, so a good belief on
> either cannot be spent. They are still scored, because they inform Test A for free.



If the prompt's `s` beats the base rate by more than the requirement on a real sample, it is a
strategy and the wagering follows. If it does not, it joins the six other belief signals this
project has measured and rejected, and that is a result worth having for the cost.

---

## What could still sink it

- **Prop prices are not collected.** Phase 5 routes around this; Phases 4 and 6 do not.
- **A handful of candidates a week may never reach a decidable sample.** Widening the net costs
  prompt quality, which is the thing being measured.
- **`q` is site-specific.** A prompt that finds a great belief on a badly calibrated site earns
  nothing, which is why Phase 1 screens on the site before the prompt ever runs.

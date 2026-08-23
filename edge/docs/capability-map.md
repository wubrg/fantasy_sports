# What edgectl computes, and what it does not

> **This document is a version behind and is being corrected.** An adversarial review on
> 2026-08-23 found it contradicts the README on what is priceable, and found three critical
> defects in the pricing path that are not yet reflected here. Read
> [`reviews/2026-08-23-adversarial.md`](reviews/2026-08-23-adversarial.md) alongside it.

A map from the framework documents in [`frameworks/`](./frameworks/) to the code that
enforces them, written so the *absences* are as legible as the coverage. The math audit in
[`frameworks/README.md`](./frameworks/README.md#math-audit) checks whether the source
documents are right. This checks whether we implemented them.

Current as of the line board and bankroll ledger landing on `main` (PR #56). All six
packages green under `make check`.

---

## The question this answers

Every wager here passes through three kinds of work:

1. **Arithmetic** — must never be done by a language model. The source documents contain
   four arithmetic errors and one process failure; that is what `internal/wager` exists for.
2. **Mechanical data work** — extracting a game log, computing a usage trend, transcribing
   prices into a table. It *looks* like judgement, but it is not. Wherever there is no
   command for it, it falls to a human or a model, and a model that is asked for a number
   it does not have will supply one. This is the fabrication surface.
3. **Judgement** — what to believe, and whether the world has changed since the line was set.

The design intent is that (1) is always code, (3) is always a person, and (2) migrates from
(3) to (1) over time. This document tracks where that migration has got to.

---

## Covered

### Odds arithmetic — complete

| Framework clause | Implementation |
|---|---|
| Implied probability, both signs (`edge-of-vigor` §2) | `American.ImpliedRaw` (`odds.go:76`) |
| Breakeven / "hurdle rate" (§Table 1) | `American.Breakeven` (`odds.go:87`) |
| Vig, overround, hold (§3) | `Market.Overround`, `Market.Hold` (`odds.go:114,125`) |
| De-vigged fair value — *the framework's gap, not its content* | `Market.FairDevig` (`odds.go:142`) |
| EV, real money (§Strategy C) | `EVRealMoney` (`ev.go:54`) |
| EV, stake-not-returned (§Section 2) | `EVBonusBet` (`ev.go:79`) |
| Profitability Matrix, Key Takeaways, Safety Tax, Tables A–E | pinned by test (`ev_test.go:13,44,87`) |
| The two source errors | pinned as *must not reproduce* (`ev_test.go:188,210`) |

`edgectl market` refuses a one-sided market outright: vig cannot be measured from one price,
and returning zero would read as "no vig" rather than "no data".

The framework asks only "does `p_true` beat implied?". `EdgeReport` answers two questions
instead — `ClearsHurdle` and `BeatsMarket` (`ev.go:127`) — because beating the price and
disagreeing with the market are different claims and only the first is ever surfaced in the
source.

### Bonus bets — complete for both framework paths

Path 1, the longshot imperative (`analytical-hobbyist` Part 1): `edgectl card bonus`,
`edgectl bonus`, `MinPriceForConversion`, `ConversionCeiling`, `TrueConversion`.

Path 2, conversion by hedging (Part 2): `ConvertBonus` (`ev.go:212`) and
`edgectl hedge`. Solves `h = face × m_back / decimal_hedge` for the stake that equalises both
outcomes, and reports the combined hold `implied_back + implied_hedge − 1` — which is what
actually decides the conversion rate. A negative hold is flagged as true arbitrage. Pinned
against the document's *corrected* worked example: `$100` at `+500` hedged at `−550` gives
`$423.08 / $76.92 / 76.92%`, not the printed `$458.33 / $41.67 / 41.67%`.

### Game lines — strong

`edgectl board` is a per-week line board of hand-entered prices, one file per week, tracked
in git on the reasoning in [ADR-001](./ADR-001-line-board-tracked-in-git.md): hand-entered
prices are irreplaceable and their commit timestamps are evidence of when they were observed.

`board report` (`internal/board/report.go`) computes: de-vigging with an overround
plausibility band of 0.5–15% (outside it, prices are flagged Suspect and held out of the
parlay pool rather than trusted); conversion ranking of dogs; disjoint two-leg parlays where
no team and no game may repeat, priced by multiplying *de-vigged* probabilities rather than
the parlay price's own implied; set selection by either max hit-rate or max conversion; line
shopping across books (`report.go:969`); and a deployment frontier that splits funds across
1–12 shots and marks dominated rows.

### Bankroll — strong

`internal/ledger` is append-only JSONL with seven event kinds. **Balances are replayed, never
stored** (`ledger.go:36`), so `-as-of` can answer what was held on a past date. Lots carry a
book and an optional expiry; `expiring` is the command the design says earns its keep,
because "every meaningful loss last campaign was a deadline, not a bad price."

Profit boosts are modelled as bonus bets in disguise (`ledger.go:152`): a 50% boost capped at
$25 is worth `percent × max_stake × profit_multiple × (implied_raw / (1 + hold))`
(`ledger.go:178`) — exactly a $12.50 bonus bet.

### Narrative pricing — the repo's own work, and not in any source document

`RequiredScenarioProb` (`belief.go:65`) inverts `P(hit) = q·s + r·(1−s)` to `s* = (p* − r)/(q − r)`:
the scenario probability at which a wager breaks even. The output is a sentence that can be
argued with — "this is +EV if a shootout is at least 32% likely" — rather than a verdict.
`Classify` names whether the market's own line already carries the bet, your read carries it,
or nothing does. `Sensitivity` reports which of `q` and `r` the conclusion actually hinges on,
which is frequently not the one attention goes to.

### Calibration — better than the frameworks ask for

Neither source document does this at all. `internal/betlog` is an append-only event stream;
settling appends rather than rewrites, so a prediction cannot be edited once the outcome is
known. `BySource` and `ByConditionalSource` score your stated reads separately from the fitted
grid's, because you can be good at one and bad at the other.

---

## Gaps

### Player props are the thin layer — and they are the reason this repo exists

The original prompt behind `edge-of-vigor` was about **player props**. The board is
moneyline, spread and total only: `board import` accepts moneylines alone
(`importer.go:143`), and the ledger states plainly that prop boosts cannot be reasoned about
because "the board carries no prop prices" (`ledger.go:273`). So de-vigging, conversion
ranking, parlay construction and line shopping — all of it — operates on game markets.

For props the tooling is what it has always been: `hitrate`, `scenario`, and price math on
operator-supplied numbers. Every input is typed by hand.

### The fitted grid is four stats, with different scenarios usable on each

`conditionals.json` covers **receiving yards, receptions, rushing yards and passing yards**,
2009–2025, with a different set of scenarios validated on each:

Since 2026-08-23 the gate rules on a **site** — one (opportunity band, trend band) coordinate of
one scenario — rather than on a whole scenario, so "priceable" is a count rather than a flag.
102 of 311 sites are priceable:

| outcome | `shootout` | `blowout_loss` | `pass_heavy` | `efficient_offense` |
|---|---|---|---|---|
| receiving yards | 13/29 | vetoed | vetoed | 12/30 |
| receptions | 10/30 | no direction | 18/30 | 9/30 |
| rushing yards | no direction | 8/11 | 8/12 | 9/12 |
| passing yards | 6/10 | no direction | 5/10 | 4/10 |

No override is currently in force; see below.

So a scenario is not simply on or off for an outcome. Asking for a site that did not survive is
refused by name and reason — "`receiving_yards/shootout` is priceable, but not at posted 46-999,
baseline 70-999, trend +0.06..+99.00: the bootstrap interval does not clear zero". Anything else is
refused with the measurement attached. Stated `-q`/`-r` always win over the grid.

`pass_heavy` is **vetoed for receiving yards**: holding realised targets fixed its coefficient
collapses from t = 14.25 to t = 1.97, so 90% of the separation is volume the projection failed to
anticipate rather than a market inefficiency. It survives that test elsewhere and is kept there.
See `FINDINGS.md` §11.

**The Tier 3 case is now priceable** (closed 2026-08-22). `8–11` projected targets with a
rising role — the corpus's "usage vacuum", high volume meeting climbing usage — held 97
observations against a floor of 100 and could not be priced at all. Extending the fit window
to 2009 publishes it at 139/295. `11+` projected targets still cannot be priced: only the
no-scenario side clears the floor, and `q` needs both.

### The framework's Tier 3 was measured and does not hold

The "usage vacuum" — WR1 out, WR2 eats — is the corpus's strongest claimed edge at +15 points.
Measured on the top remaining receiver it is **negative**: 50.1% clearing 52.5 yards with a large
vacuum against 55.6% without. Fifteen points of vacated share delivers 0.7 points to the average
remaining receiver, so it does not concentrate. Rejected at Gate 1, never fitted. See
`FINDINGS.md` §10.

### `p_true` by simulation does not exist

`urps-wager-engine.md` requires it and says a prop with only a mean projection must be
dropped, because prop lines are medians and player distributions are right-skewed. Nothing
computes it for any stat. `probAbove` (`conditionals.go:182`) is the nearest machinery and
covers receiving yards alone. Today this rule can only be obeyed by dropping nearly
everything.

### Book rules are implemented, tested, and unreachable

`CheckBonusMarket`, `BonusLostOnPush`, `BonusSplittable` (`bankroll.go:127`) encode a rule
where the downside is losing the entire asset, not a worse rate: FanDuel forfeits a bonus bet
on a push, so a pushable market can cost 100% against a hedge that merely refunded.
**No non-test caller exists anywhere in the tree.** `edgectl hedge` takes no `-book` and does not
call it; the rule survives as prose in `card bonus`. Fanatics was added with the correct house
rule (returns the stake) and is equally unreachable.

### ~~The grid does not say which regime an estimate is in~~ — closed

Every lookup now reports the effective observations past the line and labels the estimate
`MEASURED` or `THIN`, and a line beyond anything the cell ever produced is refused rather than
clamped into a small probability. Previously a `q` of 2.3% on seven observations and one of
25.2% on ninety-four printed identically — the thin one with the *narrower* interval.

### ~~The scenario's definition was not recorded, so the query could diverge from it~~ — closed

The artifact now records what each name means (`shootout` = `total > 50`), the fit derives its
predicate from those same fields so the two cannot drift, and a query whose `-threshold` or
`-basis` disagrees is refused before any output. An artifact predating the field fails closed.

### ~~`SCENARIO_STATUS` was asserted prose~~ — closed

The three validation tests are implemented in `model/analysis/validate.py` and run on every fit.
The artifact carries measured evidence with the bootstrap's seed and resample count. The verdict
remains a human judgement, but the fit **fails** when the stated rule and the recorded verdict
disagree. Doing this surfaced a correction to `FINDINGS.md`: the "direction inverts at ordinary
lines" disqualifier does not clear sampling error for either scenario.

### ~~The gate is per scenario, and the evidence says it should be per cell~~ — done 2026-08-23

The old rule required the direction to hold in *every* cell, which passes with probability
`(1−e)^k` for `k` cells: it grew stricter the more finely the grid was cut, so re-cutting the same
data rejected findings it had previously accepted. Cell count is a choice about axes, not evidence.

Each site is now judged on its own — direction, bootstrap resolution, and out-of-sample
persistence, all three, at that site. `k` does not appear. This also made the bootstrap gateable
for the first time: it was measured but never gated on precisely because requiring it in every cell
would have rejected real effects.

Two guards come with it. A scenario's dominant sign must first beat chance (two-sided binomial,
α = 0.05), because a site is judged by agreeing with that sign and agreeing with a coin flip means
nothing — this alone removes receptions/`blowout_loss` and rushing/`shootout`. And the conjunction
was tested against a permutation null: it passes **1.6%** of site-tests when the scenario label is
shuffled across games, against 38–75% for the real thing. See `FINDINGS.md` §12.

### Both directions are priceable

`edgectl scenario -side over|under`. The grid fits `P(output > line)` only and reads the under off
the same cell, mirroring the interval rather than recomputing it. This is safe now and would not
have been on the old count grid, where `P(X > 3)` and `P(X < 4)` differ by the mass sitting exactly
on 3; ratios to a player's own baseline land on a stored quantile point essentially never.

### Every verdict says how much of it is the two constants

`MIN_CELL = 100` and `OOS_SPLIT = 2021` decide which cells exist and what "out of sample" means,
and both were chosen after the rule they feed. Rather than argue for one setting — any single
choice is arguable — every cell carries the share of the 25 `MIN_CELL` × `OOS_SPLIT` combinations
that reach its verdict. Of 103 priceable sites, **86 are firm at every setting and 17 are not**, and
the 17 announce themselves where the price is formed:

```
CAVEAT: this verdict holds at 80% of the swept MIN_CELL x OOS_SPLIT settings, not all
of them — it depends partly on where those two constants were set
```

`make constants` shows what each setting buys. The short version: at `MIN_CELL = 100` a cell's own
Wilson half-width is 9.4pp against a typical q−r separation of 8–14pp, so the threshold resolves
the larger separations and not the smaller — which is why it is not the gate. Publishing a cell
only makes it eligible for the bootstrap, and the bootstrap refuses plenty that clear n = 100.
See `FINDINGS.md` §13.

### A refused site can be overridden, loudly

`ACCEPTED_FAILURES` is keyed by the **site** — outcome, scenario and the four coordinates — and is
checked against that site's measured verdict, so an override cannot quietly cover a scenario. It
must record what was measured, why it was accepted and by whom, and an override on a site that has
started passing is reported stale. `edgectl scenario` prints it at the point of pricing; because
the lookup is now keyed by site, it fires only on the wager it actually applies to. Previously it
attached to the whole scenario and warned on every wager in it, including the cells that passed
cleanly. **None is currently in force.** The one that was — `pass_heavy` for receptions at 6–8 projected
targets — named a site in the grid as it was cut before the 2026-08-23 axis change, and the grid is
no longer cut on projected targets at all. An override cannot be carried across a re-cut: the
failure it accepted was measured on a cell that does not exist any more, and quietly re-pointing it
at the nearest new cell would be accepting a failure nobody measured. The fit enforces this — an
override naming a site the grid lacks is a hard error, not a warning.

### The out-of-sample gate has limited power, and no better version was found

`shootout` clears it 15/15 and `blowout_loss` 12/15 — roughly p = 0.1 on a binomial, so the test
discriminates partly by counting noise as evidence. A magnitude-aware replacement was tested and
rejected: it makes all three scenarios pass, including the two that are gated. See `FINDINGS.md`
§4. Strengthening it needs more held-out seasons, not a different threshold.

### ~~`snap_counts` is fetched and read by nothing~~ — measured, not yet used

Snap-share trend beats the target-share trend the grid conditions on: ΔR² +0.0053 against +0.0034,
t = 13.85 against 9.49, and the two are only ~30% overlapping. Switching costs the 2009–2011
seasons and a full revalidation, so it is recorded as a measured option rather than adopted.
See `FINDINGS.md` §9.

### Filter 2 and 3 inputs are hand-typed over a cache that already holds them

`hitrate -values` is pasted by hand while `model/data/raw/stats_player_week_*.csv` holds those
game logs. `scenario -trend` is typed from memory while `snap_counts_*.csv` is cached and
`utilization_lag.py` is what measured the +6-point threshold the trend bands use. `edge/model/`
is untouched by the board work.

Any extraction command belongs in `model/`, beside the definition it must match: the two-game
trend (`recent(last 2) − baseline(all prior)`) is already duplicated between
`utilization_lag.py:135` and `fit_conditionals.py:211`, and a third copy in Go would be a
third place to drift. A drifted trend does not fail loudly — it silently selects the wrong
grid cell.

### No emitter for the operative template's input contract

`urps-wager-engine.md` demands a MARKET block carrying computed `implied_raw | fair_devig |
breakeven | hold` per row, a PROJECTIONS block, and a closing DATA PROVENANCE / CALIBRATION
block. No command produces any of them, so they are assembled by hand — and hand-assembly is
where a fabricated number enters a report that is otherwise correct.

### Smaller absences

- No qualifying-bet hedge ("Bet & Get") and no two-stage No Sweat calculation. The latter is
  where the corpus's worst error lives: `−$127.55` printed as `+$872.45`, because the losing
  cash stake is never subtracted. Nothing catches it.
- No confidence-tier table (implied +5 / +10 / +15), the framework's central mental model.
- No prop line shopping. Game-line shopping exists; props are not on the board.
- No Kelly or per-wager stake sizing. The board plans *deployment* across parlay shots, which
  is adjacent but not the same thing.
- Market Width is self-contradictory in the source — caution in Part 0, "where errors live"
  in Part II — and is not adjudicated anywhere.

---

## What stays the agent's job

This list is the design, not a shortfall. These are the things a tool should not pretend to
answer:

- **The value of `-belief`.** The tool prices your read; it does not supply one.
- **The Phase 1 screens** — RB Dead Zone, Target Funnel, Late-Round QB. Qualitative, and no
  data behind them in this repo.
- **Whether a gap is a target or a trap.** A large gap makes a stale *projection* more likely,
  not a market error. The source material's own live demonstration failed exactly here.
- **The live world** — inactives, weather, a coordinator change, a cast on the left tackle.
- **Reading prices off a book.** There is no network client in this module and none intended.

Everything else is arithmetic or extraction, and belongs in code. The measure of progress is
how much of category (2) at the top of this document has become a command.

# What edgectl computes, and what it does not

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

### The fitted grid is one stat and one scenario

`conditionals.json` covers **receiving yards only**, 2014–2025. Of its two scenarios,
`blowout_loss` is gated off as unvalidated — its direction inverts at common lines and it
"needs a play-by-play definition rather than final margin." One usable scenario: `shootout`.
Anything else requires stated `-q`/`-r`, which is judgement wearing a number's clothes.

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

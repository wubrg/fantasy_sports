# Campaign: an ordinary week

The bonus-bet campaign has a deadline forcing the pace. A normal week does not, and the work is
mostly deciding which of a hundred available wagers are even askable.

Every command below was run; the output is copied from the run.

**What this walkthrough does not do.** It will not tell you which games to look at. You bring the
player, the price and the belief; the tool prices what you bring and refuses what it cannot support.
There is no screen that surfaces candidates, and building one honestly is still open work.

---

## 1. Prices onto the board

```
$ edgectl board scaffold -week 1     # once per week, from the schedule
$ edgectl board serve -addr :8085    # a phone-shaped form, one book and market at a time
```

Every field saves as it loses focus. There is no submit button and nothing to lose to a reload.
Blank is the normal state for a cell and is never an error; a malformed value **is** one, and
`edgectl board validate` finds them.

If you have a block of prices copied off a book, `board import` parses it:

```
$ edgectl board import -week 1 -book fanatics -n     # -n stops before writing
```

Import handles the **moneyline only** — a pasted blob is one price per team, not a line plus two
prices.

## 2. Read the market

```
$ edgectl board report -week 1 -books fanatics -exclude KC,DEN

  DE-VIGGED LINES  (fair = the market's own estimate, vig removed)
  game         dog         overround      raw     fair      conv
  --------------------------------------------------------------
  NE @ SEA     NE +160         4.56%    38.5%    36.8%     58.9%
  ...

  LINE SHOPPING
  team      consensus  fanatics     gap   payout
  ------------------------------------------------
  ARI            +400      +455     -55    -9.9%
  WAS            +190      +180     +10    +3.6%
```

Two numbers per side, and they answer different questions. **Raw** is the hurdle rate — the win
rate you need to break even at that price, vig included. **Fair** is what the market actually
thinks, with the vig removed. At −110/−110 the market says a coin flip, not 52.4%. Beating the
hurdle by half a point and disagreeing with the market by three points are different claims, and
the source documents only ever surface the first.

Line shopping is free money when it exists: the same wager at a longer price is the same product
bought cheaper.

## 3. Price a prop — if it can be priced

The grid fits four outcomes, and a different set of scenarios is usable on each. This is what is
priceable today:

| outcome | usable scenarios |
|---|---|
| passing yards | `shootout`, `pass_heavy`, `blowout_loss`, `efficient_offense` |
| receiving yards | `shootout`, `pass_heavy`\* |
| receptions | `shootout`, `pass_heavy`\* |
| rushing yards | `pass_heavy` |

\* priced on a recorded operator override; the tool says so every time, and tells you whether your
particular wager sits in the cell that failed.

Ask for a pairing that is not on that list and it refuses, with the measurement:

```
$ edgectl scenario -outcome rushing_yards -name shootout ...
edgectl: scenario: "shootout" is fitted but NOT validated for rushing_yards, so
it cannot be priced.
  positive in 10/14 cells; 2/14 bootstrap-resolved; 5/13 out of sample. A high
  posted total says a game will be scored in, not how — it can arrive through
  the air or on the ground, and the grid cannot tell which from the total alone.
```

A working call looks like this. The question is never *"is my edge real?"* — at a season's sample
size that is unanswerable — but **"what would I have to believe?"**:

```
$ edgectl scenario -name shootout -total 49 -threshold 50 -belief 0.62 \
      -baseline 55 -trend 0.07 -line 52.5 -price 100

  market says 45.6%   you say 62.0%   (you are +16.4 points apart)

  CONDITIONALS from the fitted grid (receiving_yards, 2009-2025)
    55.0 yds baseline, posted total 49.0, +7.0 pt trend
    line 52.5 = 0.95x his baseline
    q = 58.9%  [50.6-67.1]  n=132 (eff 132)  median 62 yds  (scenario occurred)
      MEASURED — ~54 effective observations past the line
    r = 40.9%  [33.8-48.2]  n=174 (eff 174)  median 48 yds  (it did not)
      MEASURED — ~71 effective observations past the line
    note: these cells are thin; treat s* as indicative

  price +100   hurdle 50.0%   q 58.9%   r 40.9%

  REQUIRES  believing the scenario is at least 50.5% likely
  your blended P(hit) ... 52.1%
  EV (real money, stake 1.00) ... +0.0414

  VERDICT   DISAGREEMENT-REQUIRED
```

**Do not type these from memory.** A wrong `-baseline` does not fail — it lands in a neighbouring
cell and prices a different population, with nothing to show that it happened. Read them off the
same data the grid was fitted on:

```
$ python3 model/analysis/player.py --player "Ja'Marr Chase" --season 2024 --week 12 --total 49.5

Ja'Marr Chase (CIN)  2024 week 12, from 11 prior games
  -baseline  96.0 yds
  -trend     +0.0481  (share points)

  cell       posted 46-999, baseline 70-999, trend +0.03..+0.06
             n=133  median 79.0 yds
  PRICEABLE  holds at 80% of knob settings

edgectl scenario -outcome receiving_yards -name shootout \
  -total 49.5 -threshold 50 \
  -baseline 96.0 -trend 0.0481 \
  -line <LINE> -price <PRICE> -belief <YOUR P(scenario)> [-side under]
```

It refuses rather than guesses: fewer than four prior games in the season, an ambiguous name, or
coordinates that fall outside every published cell all stop with a reason.

`-baseline` is **what this player normally does**, in the outcome's own units — his prior mean, off
the game log. The grid prices the line as a ratio to it (`52.5 = 0.95x his baseline`), because that
is what a book sets a line near. A grid holding raw yards answers "what does the cohort do at this
line" when the question was "what does *he* do", and that mismatch measured 8 points at the top tier
against a 2.38-point vig cushion. See [FINDINGS §11](../../model/FINDINGS.md).

`-total` is required for the same reason it always was — it derives the market's scenario
probability — and now also selects the posted-total band the grid is conditioned on. `-trend` is the
role change: share points for the share-based outcomes, raw volume for passing.

**Where `s` comes from.** For `shootout` the tool derives it from the posted total, as above. For
`pass_heavy` and `efficient_offense` there is no market line to read, and you used to have to
invent the number. `edgectl belief -name efficient_offense -prior 0.48` reads it off the team's own
prior form instead, and `edgectl scenario -prior 0.48` uses it directly. Treat it as a **rank**
rather than a probability: the bands hold their order out of sample and drift in level by up to
8 points, and the command says so.

**Unders.** `-side under` prices the other direction off the same cell. It matters more than it
sounds: the grid stores a player's output as a ratio to his own baseline, and the median of that
ratio is below 1.0 because yardage is right-skewed — a player clears his own average rather less
than half the time. So an under at a line near his average is frequently where the grid's value
sits, and until 2026-08-23 the tool could not express one at all.

The three verdicts mean different things. `disagreement-required` is the normal case for a narrative
bet. `market-alone` means the game line already justifies it — a real edge, and also the likeliest
place to have mis-set `q` or `r`. `beyond-your-read` means not even your own belief covers the price.

### Scenarios that need you to supply the probability

`pass_heavy` and `efficient_offense` have no fitted residual distribution, because **books do not
price a team's pass rate over expected or its success rate** — there is no line to derive a
probability from. They require `-smarket`:

```
$ edgectl scenario -name pass_heavy -basis offense_proe -smarket 0.32 -threshold 3 ...
```

That is an honest limit and also a real weakness: the number you supply there is exactly the
unfalsifiable judgement this tool exists to reduce, relocated.

## 4. What the grid refuses, and why that is the feature

- **A line past anything the cell ever produced.** Not a small probability — an absence of evidence.
- **A cell too thin to support the estimate.** Reported as `THIN`, because at a deep line a Wilson
  interval is narrow however little is behind it: a `q` of 2.3% on seven observations prints a
  *tighter* interval than one of 25.2% on ninety-four.
- **A threshold that disagrees with the fit.** `q` and `r` measure a fixed condition; `s` is derived
  from whatever `-threshold` you pass. Blending mismatched ones is not a probability of anything.
- **An unvalidated scenario**, with the evidence attached so you can argue with it.

## 5. Record, settle, score

```
$ edgectl scenario ... -log bets.jsonl
$ edgectl log settle -file bets.jsonl -id <id> -result won -note "cleared by 12"
$ edgectl log score  -file bets.jsonl
```

Teams carrying an open wager are then excluded from next week's parlay set automatically — deriving
that from the log is the point of keeping one. Retyping the list only had to be forgotten once to
put two tickets on the same game.

---

## The honest summary of a week

You can price four prop families against one or two scenarios each, shop game lines across the books
you have entered, build disjoint parlays from a promotional balance, and keep a calibration record
that cannot be edited after the fact.

You cannot find candidates, get prop prices without typing them, or obtain a projection. The belief
is still yours.

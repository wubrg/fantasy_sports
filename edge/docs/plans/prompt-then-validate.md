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

**Output:** structured, one record per candidate —

| field | |
|---|---|
| `game`, `team`, `scenario` | which shape it is claiming |
| `belief` | P(scenario occurs), as a number |
| `claims` | the reasons, each a checkable assertion |
| `confidence` | how sure, so overconfidence can be scored separately |

**Never requested:** player baselines, trends, target shares, historical rates, prices, or the
identity of which player to bet. The tool knows all of those.

## Phase 3 — automatic falsification

Every claim is checked against the cache *before* the belief is used:

| claim about | checked against |
|---|---|
| who is out | `injuries` |
| team form | `signals.prior_form`, `proe.prior_form` |
| which week, which season | supplied by the tool, so it cannot be wrong |

A candidate with a false claim is **rejected and logged as a prompt error**. The rejection rate is
itself a measurement: a prompt that invents a quarter of its evidence is not usable however good
its beliefs look on the ones that survive.

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

**Sample needed:** detecting a +0.10 edge against a base rate near 0.33 needs on the order of 100
predictions before the interval is tighter than the effect. At four candidates a week that is most
of a season. Report interim, decide at the end.

## Phase 6 — the decision

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

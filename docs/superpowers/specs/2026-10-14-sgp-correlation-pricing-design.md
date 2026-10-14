# Self-scored SGP: pricing same-game legs with our own correlation factor

Status: approved by user 2026-10-14, pending implementation plan.

## Problem

`edgectl parlay` refuses any two legs sharing a game tag, by design: parlay math multiplies
leg probabilities assuming independence, which is only true across different games, and
`guardSameGame` (`app/cmd/edgectl/ladder.go`) enforces that split deliberately — the comment on
it says plainly that same-game legs are correlated and "must be built as an SGP at the book."
That refusal is correct as far as it goes, but it leaves one real question unanswered: *given the
book's actual SGP price, is it a good number?*

This session answered that question by hand, once, for a real wager: DK priced a same-game
Javonte Williams 25+ rush yards + Anytime TD combo; the naive independent product of the two
legs' own prices implied roughly 53.3%; the book's actual combined price implied roughly 55.6%.
The gap (about 2.3 probability points) is the correlation the book is charging for — and
working out whether 2.3 points is a "high penalty" or a "modest, defensible one" for two legs
that obviously move together (a good rushing game and a touchdown both come from the same good
game) was pure judgment calls, not a computation, done by reasoning in the chat window rather
than a tool.

This feature makes that computation real: given each leg's own probability and an operator-
supplied correlation factor, price what the combo *should* cost under that belief, and compare
it against what the book actually charges.

## Goals

- Price a same-game combo of 2+ legs using a mathematically bounded correlation model the
  operator supplies, not the book's own number.
- Compare that fair price against the book's actual quoted SGP price (`-book`, already an
  existing flag).
- Report the breakeven correlation factor the book's price requires — the same "what would you
  have to believe" framing `edgectl scenario`'s `s*` already uses — so a real SGP price can be
  judged ("the book needs you to believe these legs correlate at ρ ≥ 0.4 for this to be fair")
  instead of eyeballed.

## Non-goals (YAGNI)

- **No N-way joint distribution modeling.** This project is already honest that `p_true` by
  simulation doesn't exist for any stat (`docs/capability-map.md`) — a full joint model would
  overclaim rigor nothing else here has. The Fréchet–Hoeffding bound (below) is the right level
  of honesty: exact and closed-form for 2 events, an explicitly-flagged approximation beyond that.
- **No automatic correlation estimation.** The operator supplies ρ; the tool never infers or
  suggests one from history — same stance as `-belief`/`-q`/`-r` everywhere else in this codebase
  (an operator's read, never invented).
- **No change to `guardSameGame`'s default behavior.** Same-game legs are refused by default,
  exactly as today; the refusal only lifts for legs covered by an explicit `-correlation` flag.

## Design

### 1. The math — Fréchet–Hoeffding bound interpolation, two events at a time

For two events with true probabilities `p1`, `p2`, the joint probability `P(both)` is
mathematically bounded:

```
max(0, p1 + p2 − 1)  ≤  P(both)  ≤  min(p1, p2)
```

— the countermonotonic floor (legs never both happen beyond what overlap is forced) and the
comonotonic ceiling (the rarer leg's own probability, if the two always happen together). The
independent case, `p1 × p2`, sits somewhere inside that range for any real positively- or
negatively-associated pair. A correlation factor `ρ ∈ [-1, 1]` linearly interpolates from
independence toward whichever bound matches its sign:

```
p_indep = p1 × p2
ρ ≥ 0:  p_joint = p_indep + ρ · (min(p1,p2) − p_indep)
ρ < 0:  p_joint = p_indep + |ρ| · (max(0, p1+p2−1) − p_indep)
```

`ρ = 0` is independence (today's existing parlay math, unchanged). `ρ = 1` means the two legs
always happen together. `ρ = -1` means they happen together only as much as the floor forces.
This is exact and bounded by construction — no clamping, because the formula cannot produce an
invalid probability for any `ρ ∈ [-1,1]`.

**The inverse — breakeven ρ\*, mirroring `RequiredScenarioProb`'s `s* = (p*−r)/(q−r)` shape
exactly:** given the book's actual price (`-book`, converted to its raw implied probability `p*`),
solve for the `ρ` that price requires:

```
p* ≥ p_indep:  ρ* = (p* − p_indep) / (min(p1,p2) − p_indep)
p* <  p_indep:  ρ* = −(p_indep − p*) / (p_indep − max(0, p1+p2−1))
```

Reported as a sentence, same house style as `s*`: *"DK's price requires believing these two legs
correlate at ρ ≥ 0.18 to be fair."* That is the tool version of the by-hand reasoning this
session did once.

### 2. Leg probabilities — same `-p` convention `edgectl parlay` already has

Each leg's own probability defaults to its raw implied (vig included, from its own price) unless
the operator supplies `-p "p1,p2,.."`, exactly as `edgectl parlay` already works for independent
legs. Nothing new here — reusing the existing flag and its existing "operator value wins, always"
precedence rule.

### 3. Three or more same-game legs — pairwise chaining, with a loud, unavoidable caveat

The Fréchet bound above is exact for exactly two events. For three or more same-game legs (a
real case — this week's logged DK SGPs carried 3-leg same-game combos), the tool chains
pairwise: blend leg 1 and leg 2 under `ρ1` into a combined probability, then treat that combined
number as if it were a single leg's probability and blend it against leg 3 under `ρ2`, and so on
— `-correlation` accepts a comma list, one `ρ` per chain step (`N−1` values for `N` same-game
legs), mirroring the existing `-p`/`-weights` per-item list convention elsewhere in this CLI.
Legs chain in the exact order they're listed; the tool never reorders them.

**This is a real approximation and the tool says so on every output that uses it.** Treating a
chained intermediate result as a single Bernoulli probability for the next Fréchet blend has no
guarantee of landing within the true 3-way (or higher) Fréchet bounds, and the error isn't
characterized — the user confirmed this tradeoff explicitly when choosing pairwise chaining over
refusing 3+ legs outright. Every combo built from 3+ same-game legs prints a caveat naming this,
the same way `edgectl belief` already prints a drift caveat on its own bounded-but-imperfect
estimate:

```
CAVEAT: 3+ same-game legs are priced by chaining pairwise Fréchet blends in listing
order, not a true N-way joint. This is a structured approximation, not an exact bound --
reorder the legs or price fewer at a time if the result looks wrong.
```

### 4. CLI surface — extend `edgectl parlay`, not a new command

`edgectl parlay` already has everything else this needs: `-legs "<price>:<game>[:<label>],..."`,
`-rr` (round robin sizes), `-total`, `-p`, and `-book <price>` (the real quoted price, for
comparison — already exists, already does exactly the comparison this feature wants). One new
flag:

```
edgectl parlay -legs "<price>:<game>:<label>,..." -correlation "<game-tag>:<ρ>[,<ρ>,...]" \
    [-p "p,p,.."] [-book <american>] -total <amount>
```

`-correlation` is keyed by game tag, semicolon-separated for multiple games'-worth of same-game
legs in one invocation (`-correlation "mia@lv:0.3;was@phi:0.5,0.2"` covers a 2-leg correlated
group in one game and a 3-leg chained group in another, inside one larger round robin that also
carries independent cross-game legs). Each entry names which game's legs it covers. Inside
`guardSameGame` (`app/cmd/edgectl/ladder.go`), the refusal lifts specifically for legs whose game
tag has a matching `-correlation` entry; legs sharing a game tag with no `-correlation` entry are
still refused exactly as today — the default stays a hard refusal.

**Composability, scoped honestly:** a correlated same-game group prices to one fair probability
(and, via `AmericanFromDecimal`, one fair price) via the math above. That single fair number can
then compose into a larger round robin alongside independent cross-game legs via the existing
straight-multiplication path (`combineIdx`), treating the correlated group as one leg for that
purpose. What this does **not** do is apply correlation reasoning simultaneously across *two
different* same-game groups inside one multi-leg round robin — each `-correlation` entry prices
one game's group independently of any other game's group, which is the only honest scope: no
cross-game correlation is claimed anywhere in this design, only same-game.

## Testing

- `internal/wager`: unit tests for the forward Fréchet blend (pin a worked example: `p1=0.6,
  p2=0.5, ρ=0.5` → a specific expected `p_joint`, and confirm `ρ=0` reproduces today's existing
  independent-product result exactly) and the inverse breakeven solve (confirm it round-trips:
  solving `ρ*` for a `p*` computed forward from a known `ρ` returns that same `ρ`).
- Pairwise chaining: a 3-leg worked example, confirm the chain order matters (swap leg order,
  confirm the result changes) and that the caveat is always attached to a 3+-leg correlated combo.
- CLI: `edgectl parlay -correlation` lifts the refusal only for the named game tag, leaves other
  same-game legs refused; `-book` comparison and breakeven-ρ reporting match the pinned worked
  examples above; a round robin mixing one correlated same-game group with independent cross-game
  legs prices correctly end to end.
- Regression: confirm `edgectl parlay` without `-correlation` is byte-identical in behavior to
  today — this is a strictly additive flag, nothing about the default path changes.

## Migration

None. New flag, new math, no existing data or behavior changes for any call that doesn't pass it.

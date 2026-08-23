# edge

Wager math for prices you read off a sportsbook yourself.

You bring a price and a belief. The tool tells you **what that price requires you to believe**, and
refuses to answer when it does not know. That second half is most of the value: it will tell you a
line is past anything it has ever seen, that a cell is too thin to carry the estimate, or that a
scenario has not earned the right to be priced — rather than returning a confident number built on
nothing.

It does not fetch odds, propose wagers, or have opinions about players. There is no network client
in this module and none intended.

## Contents

| | |
|---|---|
| [Campaign: converting a bonus bet](docs/campaigns/bonus-bet-campaign.md) | A promo with a deadline, end to end |
| [Campaign: an ordinary week](docs/campaigns/weekly-slate.md) | Board, props, logging, scoring |
| [Why this tool refuses things](docs/philosophy.md) | The reasoning behind the design |
| [Capability map](docs/capability-map.md) | What is implemented, and what is not |
| [Adversarial review, 2026-08](docs/reviews/2026-08-23-adversarial.md) | Two reviewers, the defects they found, and the fix order |
| [Findings](model/FINDINGS.md) | Every measured claim, with the script that produced it |
| [Source frameworks](docs/frameworks/README.md) | The transcribed corpus, with its errors annotated |
| [ADR-001](docs/ADR-001-line-board-tracked-in-git.md) | Why the line board is tracked in git |

`edgectl help` is the map of commands; **every command documents its own flags** — `edgectl
scenario -h`, `edgectl ledger add -h`, `edgectl board report -h`. Those are the reference.

## Why it exists

The methodology came from a set of shared chat links referenced from prompts. Those links returned
**only a page title** to any model that fetched them — about 47 characters. The frameworks were
never reaching the model, which is a sufficient explanation for a season of inconsistent output on
its own. Exporting them to PDF did not fix it: the fonts carry no character maps, so text extraction
recovers page headers and nothing else.

Transcribing them by hand fixed that, and the transcription turned up **four arithmetic errors** in
the source material. One reports a $127.55 loss as an $872.45 profit. It survived because precise
arithmetic on the wrong formula reads as more authoritative than obvious nonsense.

Hence the two rules everything here follows:

**The CLI computes; the model never does.** Odds arithmetic is easy to get subtly wrong and
impossible to spot-check in prose, so it lives in tested Go and each source error is pinned by a
test asserting the corrected figure and that the printed one is not reproduced.

**Missing data fails loudly.** Nothing returns a zero for an input it could not interpret. A zeroed
EV reads as "no edge"; the truth is "no data", and those must not look alike.

## What it does

**Prices and EV.** `market` reports both sides of a market — the hurdle rate, the de-vigged fair
value, overround and hold. Those last two answer different questions: the hurdle is what you need to
break even *including* the vig, fair is what the market actually thinks. At −110/−110 the market
says a coin flip, not 52.4%. The source documents use the raw number for both.

**Bonus bets.** `card bonus`, `bonus` and `hedge`. A bonus bet is positive EV at any price — the
stake is not yours and is not returned — so the only question is how much of its face value
converts to cash, and that depends on the price and the combined hold. `hedge` solves for the cash
stake that pays the same either way.

**The line board.** `board` keeps one file per week of prices you typed off a book, tracked in git
because hand-entered prices are irreplaceable and their commit timestamps are evidence of when you
saw them. `board serve` is a phone-shaped form; `board report` de-vigs every line, ranks dogs by
what a bonus bet actually converts, builds disjoint parlays, and shops across books.

**The bankroll.** `ledger` records what you hold per book and when each piece of it expires.
Balances are replayed from an append-only log rather than stored, so it can answer what you held on
a past date. `expiring` is the one that earns its keep — every meaningful loss in the last campaign
was a deadline, not a bad price.

**Prop pricing.** `scenario` is the heart of it. A hit rate asks whether a player's *baseline* rate
beats the price, and at a season's sample size it essentially never does. A narrative wager was
never a claim about baseline — it claims *this week differs*. So the question becomes "what would I
have to believe?", which is a single number you can argue with:

```
$ edgectl scenario -name shootout -total 49 -threshold 50 \
      -belief 0.62 -baseline 55 -trend 0.07 -line 52.5 -price +100

  market says 45.6%   you say 62.0%   (you are +16.4 points apart)
  q = 55.0%  [48.6-61.5]  n=244 (eff 223)  median 59 yds  (scenario occurred)
  r = 43.5%  [39.3-47.8]  n=522 (eff 522)  median 49 yds  (it did not)

  REQUIRES  believing the scenario is at least 56.5% likely
  VERDICT   DISAGREEMENT-REQUIRED
```

**The calibration log.** `log` records the prediction before the outcome and settles by appending,
never by rewriting — so a prediction cannot be edited once you know the answer. Nothing in the
source frameworks does this, and it is the only thing that will ever tell you whether your reads are
worth anything.

## What can actually be priced

The grid fits four prop outcomes, and a different set of scenarios has earned the right to price
each one:

The gate rules on a **site** — one scenario at one (opportunity band, trend band) coordinate —
rather than on a whole scenario, because that is the unit a price is formed at. So a scenario is
not on or off for an outcome; some of its cells hold and some do not. 102 of 311 sites are
priceable:

| outcome | `shootout` | `blowout_loss` | `pass_heavy` | `efficient_offense` |
|---|---|---|---|---|
| receiving yards | 13/29 | vetoed | vetoed | 12/30 |
| receptions | 10/30 | no direction | 18/30 | 9/30 |
| rushing yards | no direction | 8/11 | 8/12 | 9/12 |
| passing yards | 6/10 | no direction | 5/10 | 4/10 |

No override is in force. The one that was — `pass_heavy` for receptions — named a site in the
grid as it was cut before 2026-08-23, and an override cannot be carried across a re-cut: the
failure it accepted was measured on a cell that no longer exists.

### What it is worth, measured

Scored on a season the grid never saw — fitted 2009–2024, run against 2025 — the tool's
probabilities are right to within **0.74pp on 16,512 wagers**. And every one of those wagers loses
at −110. That is the finding, not a contradiction: **calibration is not an edge.** A perfectly
calibrated number priced at the market's own line returns the vig, negatively.

What the backtest actually produces is the price you would need: **+137** on a line at a player's
average, **+185** at 1.15× it. Whether a book offers that is a question about prices, and this
repository does not collect prop prices. See [Findings §15](model/FINDINGS.md).

`model/analysis/player.py` reads `-baseline` and `-trend` off the cache for a given player and
upcoming week, tells you which cell they land in and whether it is priceable, and prints the
`edgectl` command. Typing those two numbers from memory is the one mistake here that fails
silently — a wrong baseline prices a different population and says nothing.

Ask for a site that did not survive and you get told which one and why, not a shrug:

```
scenario: receiving_yards/shootout is priceable, but not at posted 46-999, baseline 70-999,
  trend +0.06..+99.00.
  the bootstrap interval does not clear zero; the seasons after 2021 are too thin here to check it.
```

Anything else is refused with the measurement attached.

`edgectl belief` supplies the other half. `P(hit) = q·s + r·(1−s)`: the grid fits `q` and `r`, and
`s` — how likely the scenario is — came off the posted total for `shootout` and off the spread for
`blowout_loss`. For the other two there was no market to read, so the operator invented it. A
team's own prior form predicts it, and now does: `edgectl belief -name efficient_offense -prior
0.48` returns `s = 0.487`, and `edgectl scenario -prior 0.48` uses it directly. The bands hold
their *order* out of sample and drift in *level*, so the command says to treat `s` as a rank. The list is short on purpose — of six
candidate signals tested, one survived, and the corpus's strongest claimed edge measured *backwards*.
See [Findings](model/FINDINGS.md).

## Getting started

```
make help          # every target
make demo          # the whole tool, worked end to end — no data needed
make check         # Go tests
make data          # ~500 MB of open NFL data, once, only if you want to refit
```

`make demo` runs off committed artifacts, so it works on a fresh clone with no network.

## Refitting

```
make verify-model   # re-run the fits read-only, print calibration
make findings       # reproduce every claim in model/FINDINGS.md
make fit            # REWRITES the committed artifacts
```

`make fit` changes what `edgectl` prices. Read the diff — a half-yard shift in one cell median once
moved a documented belief requirement by 4.2 points.

**Rebuild after fitting.** The grid is embedded into the binary at build time, so `edgectl` keeps
serving the old artifact until `make build` runs. A refit that appears to have done nothing has
usually done exactly this.

## Layout

```
edge/
  Makefile             Go + the Python model pipeline
  moneylines/          the line board: one file per week, prices per book
  docs/                campaigns, philosophy, capability map, source frameworks
  app/                 Go, one dependency (gopkg.in/yaml.v3, for the board)
    cmd/edgectl/       CLI
    internal/wager/    odds, vig, de-vigging, EV, hit rate, belief decomposition
    internal/scenario/ game line -> scenario probability; the fitted q/r grid
    internal/board/    per-week line board: schema, scaffold, validation, report
    internal/ledger/   bankroll: append-only, balances replayed, expiry
    internal/betlog/   append-only prediction log and calibration
  model/               Python, stdlib only
    FINDINGS.md        every measured claim, with the script that produced it
    ingest/            nflverse -> local cache (gitignored)
    analysis/          fits, signal gates, validation; emits the artifacts
```

`internal/wager` is pure computation. Odds acquisition is deliberately out of scope: prices are
supplied by the operator, who read them off a book.

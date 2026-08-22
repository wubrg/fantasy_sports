# edge

Wager math and the framework documents it enforces.

Two things live here:

- **`docs/frameworks/`** — the NFL wagering frameworks, transcribed to Markdown, plus the operative
  prompt template derived from them. Start at
  [`docs/frameworks/README.md`](docs/frameworks/README.md).
- **[`docs/capability-map.md`](docs/capability-map.md)** — which clauses of those frameworks are
  actually implemented, which are not, and what is left for a person to judge.
- **`app/`** — a Go module whose `internal/wager` package does the arithmetic, and an `edgectl` CLI
  that exposes it.

## Why this exists

The frameworks were referenced from prompts as bare Gemini share links. Those links returned only a
page title to any model that read them — roughly 47 characters. The methodology was never reaching
the model, which is a sufficient explanation for a season of inconsistent output on its own.

Exporting the links to PDF did not fix it: the exports use subsetted web fonts with no `/ToUnicode`
maps, so `pdftotext` recovers page headers and nothing else. The transcription in
`docs/frameworks/` is the fix, and it is the artifact everything should reference.

While transcribing, the arithmetic in the source documents was independently verified. **Four
numeric errors were found**, plus one process failure that matters more than any of them. All are
documented inline in the transcripts and pinned as tests here. See
[`docs/frameworks/README.md`](docs/frameworks/README.md#math-audit).

## The two invariants

Everything in `app/` follows from two rules:

**1. The CLI computes; the model never does.** Odds arithmetic is easy to get subtly wrong and
impossible to spot-check in prose — the source documents got 14 Profitability Matrix cells exactly
right and then used the wrong profit multiplier for three rows of a bonus-bet table, and the error
survived because it looked like all the others. So the arithmetic lives in tested Go, and the
prompt template restates results rather than deriving them.

**2. Missing data fails loudly.** No function here returns a zero for an input it could not
interpret. A zeroed EV reads as "no edge"; the truth is "no data," and those must not look alike.
`edgectl market` refuses to report vig from one side of a market, because it cannot be measured
from one side.

## Usage

```
$ cd app && make build

$ ./edgectl market -a -110 -b -110 -p 0.53
side       price   implied(raw)   fair(de-vig)
A          -110         52.38%          50.00%
B          -110         52.38%          50.00%

ovrround   4.76 pts   hold   4.55%
hurdle    52.38% on side A (raw implied — this includes the vig)

p_true    53.00%   stake 1.00
EV real  +0.0118   EV bonus +0.4818
clears hurdle: true    beats market: true
```

Note the two separate comparisons. **Hurdle** is the raw implied probability — the win rate needed
to break even at this price, vig included. **Fair (de-vig)** is what the market actually thinks. At
−110/−110 the market says a coin flip, not 52.4%. The source documents use the raw number for both
questions, which is right for "should I bet this?" and wrong for "do I disagree with the market?"

```
$ ./edgectl bonus -odds 1000 -stake 25
fair ceiling   22.73   (90.9% of face, = stake × (1−p))
```

Bonus bets are Stake Not Returned: the stake is not returned on a win, so a loss costs nothing and
there is no downside term in the EV. Value therefore rises with odds length, without bound. The
closed form `EV = stake × (1 − p)` appears in none of the source documents, though every worked
example in them satisfies it.

That 90.9% is a **ceiling at fair odds**. Real longshot markets carry heavy hold, so realised
conversion lands nearer 60–80%.

## Scenario betting

A hit rate asks whether a player's *baseline* rate beats the price. At a season's
sample size it essentially never does — clearing a −110 hurdle at 95% confidence needs 13 of 17.
Fitting a distribution and simulating does not rescue it: measured, it narrows the interval about
10% at a standard line while losing calibration in the tail, where nominal 95% coverage falls to
86% even when the fitted family is exactly right.

But a narrative wager was never a claim about baseline. It claims *this week differs* — a role
change, a target funnel, a forced game script. So the question changes from "is my edge real?" to
**"what would I have to believe?"**

```
$ ./edgectl scenario -name shootout -total 44 -threshold 50 \
    -belief 0.40 -q 0.40 -r 0.08 -price 450 -label "Player A — 100+ rec yds"

SCENARIO  shootout (total > 50.0) p=0.274 [derived-from-line]
  market says 27.4%   you say 40.0%   (you are +12.6 points apart)

  REQUIRES  believing the scenario is at least 31.8% likely
  VERDICT   DISAGREEMENT-REQUIRED
  your read is what carries this. Margin over the requirement: +8.2 pts

  SENSITIVITY  s* moves -0.99 pts per point of q, -2.13 per point of r
  the conclusion is driven most by r
```

With `P(hit) = q·s + r·(1−s)`, the price's breakeven `p*` gives `s* = (p* − r) / (q − r)`. The game
total and spread supply the market's own view of `s`, so the disagreement is explicit rather than
implied — and the verdict says where the expectation actually comes from:

| Verdict | Meaning |
|---|---|
| `market-alone` | +EV on the market's own read. Usually the prop and the game line disagree — a real edge, but also the likeliest place to have mis-set `q` or `r`. Flagged, not celebrated. |
| `disagreement-required` | Your read carries it. The normal case for a narrative bet. |
| `beyond-your-read` | Not even your own belief justifies the price. |

`-rungs line:price:q:r,...` runs an alternate-line ladder, ranked by how much room your belief has
over each requirement rather than by EV — orderings survive misspecification that probability levels
do not.

## Where q and r come from

`-q` and `-r` can be stated, or looked up from a grid fitted on 27,288 player-games:

```
$ ./edgectl scenario -name shootout -total 49 -threshold 50 -belief 0.55 \
    -targets 7.5 -trend 0.07 -line 52.5 -price 250

  CONDITIONALS from the fitted grid (receiving_yards, 2014-2025)
    7.5 projected targets, +7.0 pt trend, line 52.5
    q = 56.5%  [48.7-63.5]  n=169   median 59 yds  (scenario occurred)
    r = 44.5%  [39.5-49.5]  n=378   median 49 yds  (it did not)
```

Three axes: **projected targets** (volume over efficiency — every measurement here has agreed that
opportunity drives yards more than per-target skill), **role trend** (with the boundary at the
measured +6-share-point actionability threshold), and **game script** (the axis that separates `q`
from `r` at all).

Stated values always win. The grid cannot see a cast on the left tackle. But both sources are
recorded and scored separately, so a season tells you which of you is better at what.

## The calibration log

```
$ ./edgectl scenario ... -log bets.jsonl     # records the prediction
$ ./edgectl log settle -file bets.jsonl -id <id> -result won
$ ./edgectl log score  -file bets.jsonl
```

Nothing in the source frameworks does this, and it is the only thing that will ever tell you whether
the narrative adjustments are worth anything. A claim that is never checked is indistinguishable
from a hunch.

The file is an append-only event stream. Settling appends a settlement rather than rewriting the
bet, so **a prediction cannot be edited once the outcome is known** — hindsight is the failure mode
a calibration log exists to prevent. Scoring reports predicted versus realised hit rate, excludes
pushes and voids, honours the bankroll rules, and splits by where each scenario probability came
from, so you can find out whether your reads or the line-derived ones were better.

## Getting started

```
make help          # every target
make demo          # the whole tool, worked end to end — no data needed
make check         # Go tests
make data          # ~185 MB of open NFL data, once, only if you want to refit
```

`make demo` is the fastest way to see what this does. Everything in it runs off
committed artifacts, so it works on a fresh clone with no network.

### A worked slate

**1. The bonus card, printed once and kept on your phone.** Needs no data ever — a stake-not-returned
wager is +EV at any price, so the only question is conversion, and that depends on price alone.

```
$ make -C edge build && ./edge/app/edgectl card bonus
  YOUR FLOOR: +234  (for 70% conversion)
  +300   25.0% wins   75.0% conversion   sweet spot
```

**2. Is there an edge in the player's own history?**

```
$ edgectl hitrate -line 52.5 -side over \
    -values "61,58,55,72,39,58,44,66,71,49,63,57,80,41,54,68,45" -price -110

over 52.5 — 12 of 17 games
  point estimate   70.6%
  95% interval     46.9% – 86.7%
  offered -110 — hurdle 52.4% — UNPROVEN
```

70.6% over a full season and it still can't be separated from a coin flip. That is the normal
answer, and it is why the scenario layer exists.

**3. Ask the question that can be answered instead.** Not "is my edge real?" but "what would I have
to believe?"

```
$ edgectl scenario -name shootout -total 49 -threshold 50 \
    -belief 0.55 -targets 7.5 -trend 0.07 -line 52.5 -price +100

  market says 45.6%   you say 55.0%   (you are +9.4 points apart)
  q = 56.5%  [49.1-64.1]  n=169 (eff 164)   (scenario occurred)
  r = 44.5%  [39.5-49.7]  n=378 (eff 359)   (it did not)

  REQUIRES  believing the scenario is at least 45.8% likely
  VERDICT   DISAGREEMENT-REQUIRED
  your read is what carries this. Margin over the requirement: +9.2 pts
```

Three verdicts are possible and they mean different things. `disagreement-required` is the normal
case for a narrative bet. `market-alone` means the game line already justifies it — a real edge, but
also the likeliest place to have mis-set `q` or `r`. `beyond-your-read` means not even your own
belief covers the price.

**4. Which rung of a ladder needs the smallest leap.**

```
$ edgectl scenario ... -rungs "52.5:-110:0.72:0.48,75.5:145:0.55:0.26,100.5:450:0.40:0.08"

  line     price  requires   margin       EV   verdict
  52.5      -110     18.3%   +36.7%  +5.5636   market-alone
  75.5      +145     51.1%    +3.9%  +6.0828   disagreement-required
  100.5     +450     31.8%   +23.2%  +11.5200  market-alone
```

Ranked by margin, not EV — at these sample sizes the *ordering* of requirements survives what the
probability levels do not.

**5. Record it, then find out whether you were any good.** Add `-log bets.jsonl` to any scenario
call, then:

```
$ edgectl log settle -file bets.jsonl -id <id> -result won
$ edgectl log score  -file bets.jsonl
```

### Refitting the model

```
make verify-model   # re-run both fits read-only, print calibration
make findings       # reproduce every claim in model/FINDINGS.md
make fit            # REWRITES the committed artifacts
```

`make fit` changes what `edgectl` prices. Read the diff.

## Running the board over Tailscale

The line board is a phone-shaped form, and the phone is where the prices are.
It runs as a launchd agent on the desktop and is mounted into the tailnet at
`/edge`, the same way `leagueweb`, `canton` and `draftroom` are:

```sh
make board-install     # copy the plist into ~/Library/LaunchAgents
make board-load        # start it, and start it at every login
make board-serve-mount # tailscale serve --bg --set-path=/edge localhost:8085
```

Then, from anything on the tailnet:

```
https://<desktop-name>.<your-tailnet>.ts.net/edge
```

`make board-status` / `board-serve-status` show whether each half is up;
`board-unload` and `board-serve-unmount` reverse them. The one-time setup
(plist placeholders, launchd caveats) is the same as the other three apps and
is written out in `league_home/README.md` — it is not repeated here.

**`tailscale serve` is tailnet-only, and this must never become
`tailscale funnel`.** The board records what you wagered and the prices you
took. `funnel` would publish that to the public internet.

### Why the page uses base-relative URLs

`--set-path` strips the mount before forwarding, so the server itself needs no
prefix awareness: a request to `/edge/api/board` reaches it as `/api/board`.
The subtlety is in the browser. A relative `fetch("api/board")` resolves
against the *current* path, which is right at `/edge/` and wrong at `/edge` —
there it resolves to the tailnet root, outside the mount, and 404s. So
`static/app.js` derives a `BASE` that forces the trailing slash and is correct
at `/`, `/edge` and `/edge/` alike. Test the **no-slash** URL when changing
this; it is the one that breaks.

## Layout

```
edge/
  Makefile             working entry point: Go + the Python model pipeline
  README.md
  docs/frameworks/     transcribed source material + operative template
  docs/ADR-*.md        architecture decisions
  moneylines/          the line board: one file per week, prices per book
                       (tracked in git on purpose -- see docs/ADR-001)
  app/                 Go, one dependency (gopkg.in/yaml.v3, for the board)
    Makefile           build / test / vet / fmt / lint / check
    cmd/edgectl/       CLI
    internal/wager/    odds, vig, de-vigging, EV, hit rate, belief decomposition
    internal/scenario/ game line -> scenario probability; pooled q/r grid
      artifacts/       committed JSON, read via go:embed
    internal/betlog/   append-only prediction log and calibration
    internal/board/    per-week line board: schema, scaffold, validation
  model/               Python, stdlib only
    FINDINGS.md        every measured claim, with the script that produced it
    ingest/            nflverse -> local cache (gitignored)
    analysis/          fits and validation; emits the artifacts above
    data/raw/          the cache itself (gitignored, ~185 MB)
```

Two Makefiles, deliberately. The **root** one builds `edge/app` alongside the other two Go modules,
so `make check` at the repo root covers this app's Go code the same way it covers `canton` and
`league_home`. The **`edge/` one** is the entry point for working on this app specifically, and is
the only place that knows about the Python side.

`app` is the third Go module in this repo, alongside `league_home/app` and `canton/app`, and is
wired into the root `Makefile` delegator. `make check` from the repo root covers it.

## Scope

`internal/wager` is pure computation — no network code, and none intended. Odds acquisition is
deliberately out of scope: prices are supplied by the operator, who read them off a book.

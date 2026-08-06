# edge

Wager math and the framework documents it enforces.

Two things live here:

- **`docs/frameworks/`** — the NFL wagering frameworks, transcribed to Markdown, plus the operative
  prompt template derived from them. Start at
  [`docs/frameworks/README.md`](docs/frameworks/README.md).
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

## Layout

```
edge/
  README.md
  docs/frameworks/    transcribed source material + operative template
  app/
    Makefile          build / test / vet / fmt / lint / check
    cmd/edgectl/      CLI
    internal/wager/   odds conversion, vig, de-vigging, EV
```

`app` is the third Go module in this repo, alongside `league_home/app` and `canton/app`, and is
wired into the root `Makefile` delegator. `make check` from the repo root covers it.

## Scope

`internal/wager` is pure computation — no network code, and none intended. Odds acquisition is
deliberately out of scope: prices are supplied by the operator, who read them off a book.

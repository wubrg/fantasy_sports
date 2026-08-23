# Campaign: converting a bonus bet before it expires

A promotional bonus with a deadline is the campaign this tool was built around. The money is not
yours until it has been through a wager, the offer dies on a date, and the arithmetic that decides
how much of it survives is the arithmetic the source documents got wrong three times.

Every command below was run; the output is copied from the run, not paraphrased.

**What this walkthrough does not do.** It never fetches a price. Every number you see on a
sportsbook you type in yourself. There are no prop prices on the board, so this campaign is game
lines only.

---

## 1. Record what you hold, and when it dies

```
$ edgectl ledger add -kind grant -book fanatics -asset bonus -amount 50 \
      -expiry 2026-09-14 -note "week 1 promo"

recorded grant 20260823T112944-grant-fanatics-6ddd900001
  lot 20260823T112944-grant-fanatics-6ddd900001 — fanatics bonus 50.00, expires 2026-09-14 00:00
  use that lot id with -lot when you place, convert or expire it
```

The ledger is append-only and balances are replayed rather than stored, so `-as-of` can answer what
you held on a past date. The expiry is the field that matters most:

```
$ edgectl ledger expiring -within 720h

EXPIRING WITHIN 720h

  lot                  book         asset               value  deadline
  --------------------------------------------------------------------
  20260823T1129…       fanatics     bonus               50.00  Mon 2026-09-14 (21d 17h)

  every meaningful loss in the last campaign was a deadline, not a bad price.
```

## 2. Get the week's prices onto the board

```
$ edgectl board scaffold -week 1        # once; never overwrites a price you entered
$ edgectl board serve -addr :8085       # then type prices from your phone
```

`scaffold` builds the week from the schedule with a slot per book, prefilled with a `consensus`
column from `games.csv`. **Consensus is a reference, not a book you can bet.** Until you enter a
real book's numbers, the report can tell you what a side is *worth* and not what you can *get*.

## 3. Read the board, with your money and your rules

```
$ edgectl board report -week 1 -books fanatics -book-funds fanatics=50 \
      -exclude KC,DEN -expiry 2026-09-14

  16 of 16 games priced at fanatics.

  DE-VIGGED LINES  (fair = the market's own estimate, vig removed)
  game         dog         overround      raw     fair      conv
  --------------------------------------------------------------
  NE @ SEA     NE +160         4.56%    38.5%    36.8%     58.9%
  SF @ LA      SF +150         4.29%    40.0%    38.4%     57.5%
  ...

  PER-BOOK ALLOCATION
  book          tickets    balance      stake
  --------------------------------------------
  fanatics            4     $50.00     $12.50

  DISJOINT PARLAY SET — 4 shot(s) at $25.00
  legs                                     price  true p    conv    return
  --------------------------------------------------------------------------
  SF + IND                                  +525   14.7%   77.2%     $9.65
  ATL + NYG                                 +475   15.9%   75.5%     $9.43
  CAR + GB                                  +350   20.4%   71.2%     $8.90
  NYJ + BUF                                 +329   21.3%   70.1%     $8.76
```

Three flags earn their place here.

**`-book-funds`** deploys a *promotional* balance, which cannot move between books. Naming a book
you are not reading is refused outright:

```
$ edgectl board report -week 1 -book consensus -book-funds fanatics=50
edgectl: -book-funds names "fanatics", which is not in -books; funds at a book
you are not reading cannot be deployed
```

**`-exclude`** takes **teams**, not games. A standing rule like *never bet on or against the
Chiefs* therefore needs **both sides named** — `-exclude KC,DEN` in a week they play Denver.
Excluding `KC` alone leaves the Denver leg eligible in that same game. Teams already carrying an
open wager in your log are excluded automatically; `-exclude` is for rules the log cannot know.

Note also that exclusion applies to the **parlay set**, not to the informational tables. Excluded
teams still appear in de-vigged lines and line shopping, because those are describing the market
rather than proposing a wager.

**`-expiry`** decides whether waiting is an option. With a deadline in hand the report can say
deploy now rather than hold for a better week.

## 4. Convert, rather than ride

A bonus bet is positive EV at any price — the stake is not yours and is not returned — so the only
question is how much of its face value becomes cash. Riding a longshot maximises theoretical value
and wins under 20% of the time. Converting locks it in:

```
$ edgectl hedge -face 25 -back 455 -against -600

  CONVERTING $25.00 of bonus at +455
  hedging -600 on the other side, at another book

    cash to stake           $97.50
    guaranteed profit       $16.25
    conversion               65.0%
    combined hold            3.73%

  Below the 70% goal but above 60%, which the framework calls
  acceptable for speed or simplicity.
```

**The hedge must be at a different sportsbook.** Backing both sides at one is the clearest signal
of an arber and is what gets an account promo-banned, which ends the strategy rather than the
wager. Conversion is decided almost entirely by the combined hold of the two prices, so a better
market helps far more than resizing the stake.

## 5. Record the prediction before you know the answer

Add `-log bets.jsonl` to a `scenario` call, or let `board report -log` read your existing
commitments. Settling appends rather than rewrites, so a prediction cannot be edited once the
outcome is known — which is the entire integrity property a calibration log has.

```
$ edgectl log settle -file bets.jsonl -id <id> -result won
$ edgectl log score  -file bets.jsonl

CALIBRATION

  nothing settled yet (0 open, 0 excluded)
```

## 6. Find out whether you were any good

`log score` reports predicted against realised hit rate, excludes pushes and voids, honours each
wager's own bankroll rules, and splits by where the scenario probability came from — so after a
season it can tell you whether your own reads or the line-derived ones were better.

Nothing in the source frameworks does this, and it is the only thing that will ever tell you
whether the narrative adjustments are worth anything.

---

## What went wrong last time, and what stops it here

The source documents' own worked examples get this campaign wrong three times: a `-250` bonus bet
valued at `$30.08` when it is `$28.57`, a conversion hedge printed as `$458.33 / 41.67%` when it is
`$423.08 / 76.92%`, and a "No Sweat" branch reported as a `+$872.45` profit when it is a `−$127.55`
loss — because the losing cash stake is never subtracted.

All three are pinned as tests in `internal/wager`, asserting the corrected figure **and** that the
printed one is not reproduced. See `../frameworks/README.md#math-audit`.

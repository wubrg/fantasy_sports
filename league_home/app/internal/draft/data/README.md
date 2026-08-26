# Draft room data

## Where the data lives

**This repo is public and holds no vendor data.** Proprietary sources live in
a separate private repo, by default the sibling `../fantasy_sports_data`.

The draft room finds it by, in order: `-data <dir>` → `DRAFTROOM_DATA_DIR` →
`../fantasy_sports_data` resolved against the repo root. A missing directory
is a hard error naming the fix, never a silent fallback to an empty board.

```
fantasy_sports_data/       private — must never gain a public remote
  raw/<source>/<date>/     immutable saved exports
  normalized/              generated CSVs
  manifest.json            provenance
```

## Ingest policy

Openly published data is fetched automatically. **Subscriber content is
never fetched — it is exported by hand from a copy you already have rights
to, and it never enters this repo.**

Ciely's projections, the FantasyPoints and Athletic articles, Subvertadown's
sheets, and Peaked's cheat sheets are paid products; hundreds of rows of
someone's projections in a public repo is redistribution whatever the intent.
What *is* tracked here:

| Tracked | Why |
|---|---|
| `tools/` | the extractors — code, not data |
| `aliases.csv` | our name-matching fixes, not vendor content |
| `rulings.csv` | the league's own commissioner decisions |
| `preferences.example.yaml` | template for the personal draft filters (below) |
| `OPEN-QUESTIONS.md` | our notes |

Your live `preferences.yaml` is gitignored — it is personal strategy, like the
draftroom notes.

Anyone cloning this re-runs the extractors against their own copies.

### keeper-locks.csv, and the week keepers lock

This file carries two things at once, which is what lets the board follow the
league from "nobody has decided" to "keepers are final" without a switch to
flip on the day:

| | |
|---|---|
| a row | this player is kept |
| an owner appearing at all | **this owner has finished deciding** |

An owner in the file is declared: his listed players are his whole keeper set,
the surplus heuristic stops guessing for him, and on draft night his keepers
come **off the board** rather than staying biddable — a declaration is a fact
and cannot be drafted, where a projection is a guess and can.

An owner absent has **not entered yet** and is still projected. Absence is a
gap, never a claim that he keeps nobody. A team really keeping nobody says so:

```csv
owner,player
Sam,Rome Odunze
Sam,Zay Flowers
Bob,none          # declared, keeping nobody — his whole $200 stays in the pool
```

`none`, `-` and `nobody` all work. Such a row must name its owner, since there
is no player to read the roster from; the loader refuses a blank one rather
than record a declaration against nobody.

The board reports how far along this is on every load — *"keepers declared for
1 of 12 teams — still projected for Adam, Ben, …"* — because a half-filled
file prices a board that is part fact and part guess, and nothing on the page
would otherwise say which.

## Layout

```
raw/<source>/<date>/     immutable exports, never hand-edited
normalized/              generated CSVs in one schema
tools/                   per-source extractors
aliases.csv              source name -> Sleeper player_id
rulings.csv              LM keeper rulings (season,player_id,price,keep_count,reason)
preferences.yaml         personal one-per-offense / no-handcuff filters (gitignored; see .example)
```

## Sources

| Source | Access | How it gets here |
|---|---|---|
| Sleeper | free, keyless | auto-fetched by `draftroom` |
| Jake Ciely (The Athletic) | subscriber | export the xlsx, run `tools/extract_ciely.py` |
| Subvertadown | free tool, client-side only | save the rendered pages, run `tools/extract_subvertadown.py` |
| FantasyPros ECR | free account, JS-rendered | export the four CSV views per variant, run `tools/extract_fantasypros.py` |
| FantasyPoints | subscriber | save the article text into `raw/` |
| Peaked in High Skool | Patreon | image cheat sheet in `raw/peaked/`, not yet parsed |
| Late-Round (JJ Zachariason) | $29.99 draft guide | not yet ingested — see OPEN-QUESTIONS |

## Refreshing the sources

```
DATA=../../../../../fantasy_sports_data     # or $DRAFTROOM_DATA_DIR

python3 tools/extract_ciely.py \
    $DATA/raw/ciely/<date>/2026FFBProjections.xlsx \
    $DATA/normalized/ciely-2026.csv

python3 tools/extract_subvertadown.py \
    $DATA/raw/subvertadown/<date>/sheets \
    $DATA/normalized/subvertadown-2026.csv

python3 tools/extract_fantasypros.py \
    $DATA/raw/fantasypros/<date> \
    $DATA/normalized/fantasypros-2026.csv
```

All are dependency-free — they parse the xlsx zip/XML and the saved CSV/HTML
directly, no pip install. `make extractor-test` covers them, and it runs as
part of `make check`.

### Subvertadown specifics

Saved-page captures are required because the tool renders entirely
client-side — a plain fetch returns an empty shell. Three baselines are read
from the `stock-*` sheets; the `qbstream-*` variants were verified identical
(0 differing rows of 218) and are skipped.

The value cell is the tricky part: it holds both a `<template>` tooltip body
that corrupts the number if left in, and the ECR arrow icons that are real
signal. Icons are read from raw markup first, then templates dropped, then
text taken. `ecr_up` and `ecr_down` are **independent** — a player both flags
is contested, not neutral.

His workbook's default settings match Hit or Miss on everything except
**interceptions (−2 vs our −1)**, so the extractor recomputes fantasy
points from his raw stat components under league scoring and writes both
his number and ours, keeping the difference auditable.

### FantasyPros specifics

The rankings render client-side, so the views — overview, ranks, stats, notes —
are hand-exported as CSVs from a logged-in session into
`raw/fantasypros/<date>/`, alongside a separate set of **projection** exports
(see below). Three **variants** are ingested and packed into one
file under a `baseline` column, mirroring Subvertadown's three baselines:
`consensus` (every ranker, free), and `top10` / `top20` (the consensus of last
year's ten / twenty most accurate experts). The subsets diverge from
consensus — last year's best experts rank Bijan Robinson over Jahmyr Gibbs
where the full field does the reverse — so each consensus row also carries
`rank_vs_top10` / `rank_vs_top20` (consensus rank minus the subset rank;
positive = the sharps rate him higher).

**Projections come from the projection exports, not the `stats` view.** That
view is the "last season" reference column FantasyPros shows beside its
rankings — 2025 **actuals**. Scoring it produced an FP column that floored every
2026 rookie at nothing (Jeremiyah Love, ranked RB16, carried zeroes) and
underpriced everyone who missed time, all while reading as a second projection.
The `…-projections-qb-hilo-…` and `…-projections-flx-hilo-…` exports are primary
— between them they cover every position and carry every stat component — with
the flat per-position exports read only to fill a player the banded pair missed.
In the banded files each player is three consecutive rows: named average, then
an unnamed `high`, then an unnamed `low`. The `stats` view is now read for one
thing only, a DST's published total, since the projections carry no defenses.

Like Ciely, the point total is recomputed under league scoring so FantasyPros
is a second projection, with the published total and the delta kept auditable.
The high and low are recomputed the same way rather than scaled from the
published total — a passer's low carries *more* interceptions, not fewer, and
only recomputation sees that. **Interceptions** are published in these exports
and scored at the league's −1; nothing is estimated. **Fumbles** are published
too, recorded as `fumbles_lost`, and deliberately **not** scored: `SCORING` has
to stay key-for-key with `extract_ciely.py`, whose workbook has no fumble term,
or FP stops being comparable to Value in a way nothing on the board would
reveal. A player FantasyPros ranks but does not project gets an **empty**
`league_points` rather than a zero — zero reads as a player who scores nothing
and floors him at a dollar; empty reads as no FP opinion, and the board draws
it as `—`. Kickers are
dropped (no kicker slot); `JAC` is rewritten to Sleeper's `JAX` so the Jaguars
DST resolves, while Washington stays `WAS` (Sleeper keys that defense under
`WAS`, not the `WSH` it uses for skill players).

## Basis matters

Every source's numbers carry assumptions — scoring, team count, budget,
roster shape. A $40 auction value from a 10-team full-PPR sheet is not the
same $40 as one from our 12-team half-PPR league. `Basis` in `sources.go`
records those assumptions and `Rescale` converts between pools, returning
the multiplier so it shows up in reports rather than happening silently.

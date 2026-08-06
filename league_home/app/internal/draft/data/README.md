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
| `OPEN-QUESTIONS.md` | our notes |

Anyone cloning this re-runs the extractors against their own copies.

## Layout

```
raw/<source>/<date>/     immutable exports, never hand-edited
normalized/              generated CSVs in one schema
tools/                   per-source extractors
aliases.csv              source name -> Sleeper player_id
rulings.csv              LM keeper rulings (season,player_id,price,keep_count,reason)
```

## Sources

| Source | Access | How it gets here |
|---|---|---|
| Sleeper | free, keyless | auto-fetched by `draftroom` |
| Jake Ciely (The Athletic) | subscriber | export the xlsx, run `tools/extract_ciely.py` |
| Subvertadown | free tool, client-side only | save the rendered pages, run `tools/extract_subvertadown.py` |
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
```

Both are dependency-free — they parse the xlsx zip/XML and the saved HTML
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

## Basis matters

Every source's numbers carry assumptions — scoring, team count, budget,
roster shape. A $40 auction value from a 10-team full-PPR sheet is not the
same $40 as one from our 12-team half-PPR league. `Basis` in `sources.go`
records those assumptions and `Rescale` converts between pools, returning
the multiplier so it shows up in reports rather than happening silently.

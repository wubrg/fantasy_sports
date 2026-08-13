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

The rankings render client-side, so the four views — overview, ranks, stats,
notes — are hand-exported as CSVs from a logged-in session into
`raw/fantasypros/<date>/`. Three **variants** are ingested and packed into one
file under a `baseline` column, mirroring Subvertadown's three baselines:
`consensus` (every ranker, free), and `top10` / `top20` (the consensus of last
year's ten / twenty most accurate experts). The subsets diverge from
consensus — last year's best experts rank Bijan Robinson over Jahmyr Gibbs
where the full field does the reverse — so each consensus row also carries
`rank_vs_top10` / `rank_vs_top20` (consensus rank minus the subset rank;
positive = the sharps rate him higher).

Like Ciely, the point total is recomputed under league scoring so FantasyPros
is a second projection, with the published total and the delta kept auditable.
**One deliberate limitation:** the stats export ships no interception or fumble
column, so the recompute omits the negative-play penalty — small for skill
players (0–2 pts) but large for passers (~11–14), where `league_points` reads
high. Ciely's INT-aware projection stays the QB source of record. Kickers are
dropped (no kicker slot); `JAC` is rewritten to Sleeper's `JAX` so the Jaguars
DST resolves, while Washington stays `WAS` (Sleeper keys that defense under
`WAS`, not the `WSH` it uses for skill players).

## Basis matters

Every source's numbers carry assumptions — scoring, team count, budget,
roster shape. A $40 auction value from a 10-team full-PPR sheet is not the
same $40 as one from our 12-team half-PPR league. `Basis` in `sources.go`
records those assumptions and `Rescale` converts between pools, returning
the multiplier so it shows up in reports rather than happening silently.

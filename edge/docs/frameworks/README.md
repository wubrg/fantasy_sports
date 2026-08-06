# Wagering frameworks

Transcribed source material for NFL wager analysis, plus the operative prompt template
derived from it.

## Why these are Markdown and not PDFs

The frameworks originally lived as Gemini share links, referenced from prompts as
*"always frame wagers using the Edge of Vigor model found here: `<link>`"*. Those links
returned **only a page title** to any model that fetched them — roughly 47 characters of
visible text. The frameworks were never reaching the model. Reports built on them were
inconsistent because the model was improvising a methodology it had never been shown.

The links were then exported to PDF. That did not fix it. All five exports are browser
print-to-PDF captures whose fonts are subsetted with **no `/ToUnicode` maps**, so glyphs
render on screen but carry no character codes. `edge-of-vigor.pdf` has 94 font objects
and 8 `/ToUnicode` maps. Measured extraction:

| Source PDF | Pages | Size | `pdftotext` yield |
|---|---|---|---|
| `URPS-v3.pdf` | 154 | 74 MB | 25 KB, almost all page headers |
| `positive-ev-data-sources.pdf` | 45 | 24 MB | 7 KB |
| `edge-of-vigor.pdf` | 36 | 10 MB | 4.8 KB |
| `analytical-hobbyist.pdf` | 20 | 9.5 MB | 3 KB |
| `prompt-engineering.pdf` | 1 | 20 KB | 107 chars |

The content was recoverable only by rasterizing each page and reading it visually. These
Markdown files are that transcription, and they are the artifact everything else should
reference. The PDFs are gitignored — ~118 MB total, and `URPS-v3.pdf` is close to
GitHub's 100 MB per-file limit.

**Regression test for the original bug:** `grep -ri "bonus bet" edge/docs/frameworks/`
should return real prose. Against the PDFs it returns nothing useful.

## Files

| File | Source | Pages | Status |
|---|---|---|---|
| [`urps-wager-engine.md`](./urps-wager-engine.md) | derived | — | **Operative.** Use this one. |
| [`urps-wager-engine.source.md`](./urps-wager-engine.source.md) | `URPS-v3.pdf` | 150–154 | Archive |
| [`edge-of-vigor.md`](./edge-of-vigor.md) | `edge-of-vigor.pdf` | all 36 | Archive |
| [`analytical-hobbyist.md`](./analytical-hobbyist.md) | `analytical-hobbyist.pdf` | all 20 | Archive |
| [`positive-ev-data-sources.md`](./positive-ev-data-sources.md) | `positive-ev-data-sources.pdf` | all 45 | Archive |

`URPS-v3.pdf` pp.1–149 are a research report *about* the template and are not
transcribed; the executable artifact starts at p.150.

## Known gap: `prompt-engineering.pdf` is blank

`prompt-engineering.pdf` is a single **blank page** — 107 characters, all of it header
and footer. The share page never loaded before printing. Its URL is
`https://gemini.google.com/share/2b14c8870a8e`, which `URPS-v3.pdf` p.2 cites as the
**URPS model** document.

**This one needs re-exporting** to be recovered. It is not blocking: URPS-v3 covers the
URPS model in depth across its research sections, and the executable template does not
depend on it.

## The source material mandates simulation — the operative template does not

`URPS-v3.pdf` p.151, Phase 2, reads:

> *"You must **simulate** the retrieval of market data from the following specific
> sources. You must cite the sportsbook for every line."*

Simulated lines plus mandatory per-book citations produces fabricated DraftKings,
FanDuel, BetMGM and Bet365 prices that are formatted to look sourced. This is a likely
direct cause of the inconsistency the frameworks were meant to fix, and it compounds
badly with the framework's own EV tables: precise arithmetic on invented prices reads as
far more authoritative than it is.

`urps-wager-engine.source.md` preserves that wording as archive.
`urps-wager-engine.md` replaces it: market data is supplied by the user or the wager is
omitted, and the model never retrieves, simulates, infers, estimates, or recalls a price.

## Math audit

The EV arithmetic in the source documents was independently verified. All 14 cells of the
"Profitability Matrix" (`edge-of-vigor.pdf` p.8), the Key Takeaways, and all three
bonus-bet worked examples are **arithmetically correct**. Four methodology gaps were
found and are corrected in the operative template; see
[`../../app/internal/wager/`](../../app/internal/wager/), where each correction is pinned
by a test:

1. **No de-vigging.** The source compares your estimate to the book's *raw* implied
   probability. Correct as a breakeven hurdle, wrong as a proxy for the market's opinion
   — raw implied sums to >100%. At −110/−110 the market says 50/50, not 52.4/52.4.
2. **Calibration is never addressed.** Every output is only as good as `p_true`. At +150
   a 5-point estimate error swings EV by 12.5 points, larger than most edges the table
   is designed to find.
3. **"Underdogs Scale Fast"** is valid arithmetic pointing at the market segment with the
   highest vig and the strongest adverse favorite–longshot bias.
4. **Vig terminology.** 4.8% is the overround in percentage points; hold on balanced
   action is 4.8 / 104.8 = **4.58%**.

Bonus bets: the source gives `EV = p × profit` and three worked examples. The closed form
it misses is that at fair odds `EV_BB = stake × (1 − p)`, which reproduces all three
exactly. Its implied 90.9% conversion at +1000 is a **ceiling at fair odds**; real
longshot markets are juiced and realized conversion is typically 60–75%.

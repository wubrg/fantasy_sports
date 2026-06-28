# NFL Awards Reference — Build Plan
**Version:** 2.0
**Created:** 2026-06-17
**Goal:** Build a master NFL/AFL awards/Pro Bowl/All-Pro reference doc covering
1960–2025 (AFL founding through present), organized for fast position group
context lookups.

---

## Scope

- **Years:** 1960–2025 (66 seasons: 1994–2025 done; 1960–1993 in progress, see
  "1960–1993 Extension" below)
- **Data included:** Named AP awards, All-Pro/All-AFL (1st + 2nd team), Pro
  Bowl/AFL All-Star Game
- **Data excluded (deferred):** Statistical leaders per position (planned for
  a later chunk)
- **Output file:** `NFL_AWARDS_REFERENCE_vX.X.md` (1994–2025); the 1960–1993
  extension lands directly in `data/canton_data.json` via `cantonctl`, per
  the live database becoming the actual source of truth — see `app/README.md`

---

## Document Structure (see ADR-001)

Flat markdown table, one row per player-award per year:

```
| Year | Player | Position | Team | Award | Notes |
```

Sorted: Year DESC → Award type → Position

Award values (1994–2025): `AP MVP`, `AP OPOY`, `AP DPOY`, `AP OROTY`, `AP DROTY`, `AP CPOTY`, `SB MVP`, `All-Pro 1st`, `All-Pro 2nd`, `Pro Bowl`

Award values added for the 1960–1993 extension: `AFL MVP`, `AFL ROY`, `All-AFL 1st`, `All-AFL 2nd`, `AFL All-Star` — see `ADR-002-pre-merger-award-taxonomy.md` for which codes are valid in which years, and the franchise-lineage team-code table (no new team codes needed).

---

## Chunk Tracker

| Chunk | Scope | Status | Output Version | Notes |
|---|---|---|---|---|
| 1 | Named AP awards 1994–2025 (MVP, OPOY, DPOY, ROTY×2, Comeback, SB MVP) | ✅ DONE | v0.1 | ~220 rows; 7 [verify] entries |
| 2 | All-Pro 1st + 2nd, 2010–2025 | ✅ DONE | v0.2 | 923 new rows; team docs regenerated |
| 3 | All-Pro 1st + 2nd, 1994–2009 | ✅ DONE | v0.8 | 833 rows |
| 4 | Pro Bowl 2013–2025 | ✅ DONE | v0.8 | 933 rows |
| 5 | Pro Bowl 2001–2012 | ✅ DONE | v0.8 | 1,004 rows |
| 6 | Pro Bowl 1994–2000 + final review + dedup | ✅ DONE | v0.8 | 588 rows; superseded by `data/canton_data.json` (4,498 rows total, 88 [verify]-flagged, zero duplicates) |

## 1960–1993 Extension

Per ADR-002: 1970–1993 is structurally identical to 1994–2025 (no new award
codes), so it's split the same way the original build was (named awards,
then All-Pro, then Pro Bowl). 1960–1969 is the two-league AFL/NFL era and
needs the four new AFL-side codes from ADR-002, chunked separately because of
that added complexity and generally lower source confidence.

| Chunk | Scope | Status | Notes |
|---|---|---|---|
| 7 | Named awards 1970–1993 (`AP MVP`, `AP OROTY` continuous; `AP DPOY` 1971+; `AP OPOY` 1972+; `AP DROTY` continuous from 1967; `SB MVP`; no `AP CPOTY` — gap 1967–1997) | Not started | |
| 8 | Named awards 1960–1969, NFL side (`AP MVP`, `AP OROTY`, `AP DROTY` 1967+, `AP CPOTY` 1963–1966 only, `SB MVP` 1966 only) + AFL side (`AFL MVP`, `AFL ROY` through 1966 / `AP OROTY`+`AP DROTY` 1967–1969) | Not started | Lower confidence — competing AP/UPI/Sporting News sources; `[UPI]`-flag fallback per ADR-002 |
| 9 | All-Pro 1st + 2nd, 1970–1993 (NFL only, no AFL) | Not started | |
| 10 | All-Pro 1st + 2nd (NFL) + All-AFL 1st + 2nd (AFL), 1960–1969 | Not started | |
| 11 | Pro Bowl, 1970–1993 | Not started | |
| 12 | Pro Bowl (NFL) + AFL All-Star (AFL), 1960–1969 | Not started | |

Each chunk lands via `cantonctl add` (or a bulk JSON merge + `import`),
followed by `cantonctl export-json` to refresh the tracked snapshot, same
workflow as the live `app/` already uses for 1994–2025 maintenance.

## Verify List

Superseded by the `nt` field in `data/canton_data.json` (88 rows carry a
`[verify]` note as of v0.8 — mostly Pro Bowl depth/backup selections in
1994–1998 and 2013–2018, plus a couple of contested All-Pro slot calls).
Query the JSON for `[verify]` rather than maintaining this list by hand. The
1960–1993 extension will add `[UPI]`-flagged rows too (see ADR-002) — these
are a deliberate source-attribution note, not a confidence flag like
`[verify]`.

---

## Source Strategy

**1994–2025 (done):** PFR awards index pages (single page per award, all years)
  - MVP: `pro-football-reference.com/awards/ap-nfl-mvp-award.htm`
  - OPOY: `pro-football-reference.com/awards/ap-offensive-player-of-the-year-award.htm`
  - DPOY: `pro-football-reference.com/awards/ap-defensive-player-of-the-year-award.htm`
  - OROTY: `pro-football-reference.com/awards/ap-offensive-rookie-of-the-year-award.htm`
  - DROTY: `pro-football-reference.com/awards/ap-defensive-rookie-of-the-year-award.htm`
  - Comeback: `pro-football-reference.com/awards/ap-comeback-player-of-the-year-award.htm`
  - SB MVP: `pro-football-reference.com/awards/sb-mvp-award.htm`
  - All-Pro: `pro-football-reference.com/years/[YEAR]/allpro.htm`
  - Pro Bowl: `pro-football-reference.com/pro-bowl/[YEAR].htm`

**1960–1993 (in progress):** PFR itself returns HTTP 403 to automated
fetches in the current environment (its anti-bot wall), so this extension
sources from Wikipedia's per-award pages (e.g. `AP NFL Offensive Player of
the Year`, `American Football League Most Valuable Player award`,
`1960 All-AFL Team`) plus aggregated web search, cross-checked across
sources where they disagree. See ADR-002 for the AP-first/UPI-fallback
source-of-record policy for the new AFL-side awards.

---

## Change Log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-06-17 | Initial plan created |
| 1.1 | 2026-06-20 | Marked chunks 3–6 done (shipped in v0.8); replaced hand-maintained verify list with a pointer to the data's `[verify]` notes |
| 2.0 | 2026-06-28 | Extended scope to 1960–1993 per ADR-002: added chunks 7–12, the four new AFL-side award codes, franchise-lineage team-code notes, and an updated source strategy (Wikipedia/search instead of direct PFR fetches, which now 403 in this environment) |

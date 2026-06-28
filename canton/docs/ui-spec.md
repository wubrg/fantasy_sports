# NFL Awards Reference — UI Spec
**Version:** 1.0  
**Date:** 2026-06-18  
**Purpose:** Implementation spec for Claude Code/Opus session. Provides context, data schema, and UI requirements.

---

## Context

The awards reference system is complete. The data file (`canton_data.json`) contains 4,498 rows covering every AP award, All-Pro selection, and Pro Bowl selection for all 32 NFL franchises from 1994–2025.

This spec defines a standalone interactive web UI to query that data.

---

## Data File

**File:** `canton_data.json`  
**Format:** JSON with `meta` and `data` keys  
**Data rows:** 4,498  
**Size:** ~396 KB

### Row Schema

```json
{
  "yr": 2024,          // integer: season year (1994–2025)
  "pl": "Patrick Mahomes",  // string: player name
  "pos": "QB",         // string: position code (see positions list)
  "u": "O",            // string: unit — "O" (offense), "D" (defense), "ST" (special teams)
  "tm": "KC",          // string: franchise code (32 teams, franchise-normalized)
  "aw": "AP MVP",      // string: award code (see awards list)
  "nt": ""             // string: notes (Super Bowl ref, injury note, [verify] flag, etc.)
}
```

### Award Codes
`AP MVP` · `AP OPOY` · `AP DPOY` · `AP OROTY` · `AP DROTY` · `AP CPOTY` · `SB MVP` · `All-Pro 1st` · `All-Pro 2nd` · `Pro Bowl`

### Team Codes (32 franchises, franchise-normalized)
`ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LAC LAR LV MIA MIN NE NO NYG NYJ PHI PIT SEA SF TB TEN WAS`

### Unit Values
- `"O"` — Offense (QB, RB, FB, WR, TE, T/OT, G/OG, C)
- `"D"` — Defense (DE, DT, OLB, ILB, MLB, LB, CB, S, FS, SS, DB, Edge)
- `"ST"` — Special Teams (K, P, KR, PR, ST, LS)

### Notable Data Quality
- All named AP awards (MVP/OPOY/DPOY/ROTY/CPOTY) and SB MVP: ✅ fully verified
- All-Pro 1st + 2nd: ✅ fully verified
- Pro Bowl 1999–2025: high confidence (Wikipedia-sourced)
- Pro Bowl 1994–1998 and 2013–2018: some `[verify]` in `nt` field — real entries, lower confidence on details

---

## UI Requirements

### Filters (all interactive, no submit button — live filtering)

| Filter | Type | Values |
|---|---|---|
| **Team** | Dropdown (32 teams + "All Teams") | team code → full name from meta |
| **Unit** | 3-button toggle | All · Offense · Defense · Special Teams |
| **Award Type** | Multi-select or dropdown | All + each award code |
| **Year Range** | Dual slider or two year inputs | 1994–2025 |

### Results Display

- **Results table** with columns: Year · Player · Pos · Award · Notes
- Sorted by Year DESC by default
- Show row count ("X results")
- Named AP awards + SB MVP should visually stand out (they're rarer / more prestigious)

### Stretch goals (nice-to-have)
- Award type breakdown cards at top (e.g. "Patrick Mahomes: 3× AP MVP, 4× Pro Bowl, 3× All-Pro 1st")
- Player search / filter
- Export filtered results as CSV

---

## Implementation Approach (for discussion)

### Option A — React artifact with embedded JSON
Embed all 4,498 rows as a JS constant. ~394 KB inline. Simplest approach, always works, zero dependencies.

### Option B — Static HTML + fetch
Write a standalone `index.html` + `data.js` file pair. Open locally in browser. Good for saving/sharing.

### Option C — Vite/React project via Claude Code
Full local project: `npm create vite`, import JSON, ship production build. Best developer experience, full control.

**Recommendation:** Start with **Option A** (React artifact) for instant prototyping + feedback. Port to Option C if performance or sharing requirements emerge.

---

## Files to Pass Into Code Session

1. `canton_data.json` — the full dataset (396 KB)
2. This spec doc

---

## Change Log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-06-18 | Initial spec created. |


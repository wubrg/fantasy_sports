# NFL Awards Reference — Build Plan
**Version:** 1.0  
**Created:** 2026-06-17  
**Goal:** Build a master NFL awards/Pro Bowl/All-Pro reference doc covering 1994–2025 (modern cap era), organized for fast position group context lookups.

---

## Scope

- **Years:** 1994–2025 (32 seasons)
- **Data included:** Named AP awards, All-Pro (1st + 2nd team), Pro Bowl
- **Data excluded (deferred):** Statistical leaders per position (planned for a later chunk)
- **Output file:** `NFL_AWARDS_REFERENCE_vX.X.md`

---

## Document Structure (see ADR-001)

Flat markdown table, one row per player-award per year:

```
| Year | Player | Position | Team | Award | Notes |
```

Sorted: Year DESC → Award type → Position

Award values: `AP MVP`, `AP OPOY`, `AP DPOY`, `AP OROTY`, `AP DROTY`, `AP CPOTY`, `SB MVP`, `All-Pro 1st`, `All-Pro 2nd`, `Pro Bowl`

---

## Chunk Tracker

| Chunk | Scope | Status | Output Version | Notes |
|---|---|---|---|---|
| 1 | Named AP awards 1994–2025 (MVP, OPOY, DPOY, ROTY×2, Comeback, SB MVP) | ✅ DONE | v0.1 | ~220 rows; 7 [verify] entries |
| 2 | All-Pro 1st + 2nd, 2010–2025 | ✅ DONE | v0.2 | 923 new rows; team docs regenerated |
| 3 | All-Pro 1st + 2nd, 1994–2009 | ⏳ Pending | v0.3 | |
| 4 | Pro Bowl 2013–2025 | ⏳ Pending | v0.4 | |
| 5 | Pro Bowl 2001–2012 | ⏳ Pending | v0.5 | |
| 6 | Pro Bowl 1994–2000 + final review + dedup | ⏳ Pending | v1.0 | |

## Verify List (chunk 1 [verify] entries to confirm in a future session)
- 2013 AP DROTY: Sheldon Richardson, DT, NYJ
- 2009 AP OROTY: Percy Harvin, WR, MIN
- 2001 AP OROTY: Anthony Thomas, RB, CHI
- 2001 AP DROTY: Kendrell Bell, LB, PIT
- 2000 AP OROTY: Mike Anderson, RB, DEN
- 1995 AP DROTY: Hugh Douglas, DE, NYJ
- 1994 AP DROTY: Tim Bowens, DT, MIA

---

## Source Strategy

- **Named awards:** PFR awards index pages (single page per award, all years)
  - MVP: `pro-football-reference.com/awards/ap-nfl-mvp-award.htm`
  - OPOY: `pro-football-reference.com/awards/ap-offensive-player-of-the-year-award.htm`
  - DPOY: `pro-football-reference.com/awards/ap-defensive-player-of-the-year-award.htm`
  - OROTY: `pro-football-reference.com/awards/ap-offensive-rookie-of-the-year-award.htm`
  - DROTY: `pro-football-reference.com/awards/ap-defensive-rookie-of-the-year-award.htm`
  - Comeback: `pro-football-reference.com/awards/ap-comeback-player-of-the-year-award.htm`
  - SB MVP: `pro-football-reference.com/awards/sb-mvp-award.htm`
- **All-Pro:** PFR All-Pro pages by year: `pro-football-reference.com/years/[YEAR]/allpro.htm`
- **Pro Bowl:** PFR Pro Bowl pages by year: `pro-football-reference.com/pro-bowl/[YEAR].htm`

---

## Change Log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-06-17 | Initial plan created |

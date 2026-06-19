# ADR-001: NFL Awards Reference Document Structure

**Status:** Accepted  
**Date:** 2026-06-17  
**Deciders:** wubrg  

---

## Context

We need a reference document for NFL award winners, Pro Bowl selections, and All-Pro designations covering 1994–2025. The primary use case is answering position group context questions during player lookups (e.g., "who were the notable WRs in 1995?").

---

## Decision

Use a **flat markdown table** as the primary structure, with one row per player-award per year.

Schema:
```
| Year | Player | Position | Team | Award | Notes |
```

Sorted: Year DESC, then Award type, then Position within year.

Award values standardized to:
- `AP MVP`
- `AP OPOY`
- `AP DPOY`
- `AP OROTY`
- `AP DROTY`
- `AP CPOTY` (Comeback Player of the Year)
- `SB MVP`
- `All-Pro 1st`
- `All-Pro 2nd`
- `Pro Bowl`

---

## Alternatives Considered

### A: Year-by-year sections with nested tables by position
- **Pro:** More readable for manual browsing by year
- **Con:** Much harder to build incrementally; poor for scanning across years

### B: By-player organization
- **Pro:** Great for career lookups
- **Con:** Bad for position group context (the primary use case); hard to filter by year

### C: Flat table (chosen)
- **Pro:** Simple to build incrementally; easy to filter by Year + Position; works well for lookup queries; single source of truth
- **Con:** Less human-scannable than nested sections, but this is a machine-assist doc, not a human-read doc

---

## Consequences

- Document will have ~4,400 rows at completion
- Position group filtering = scan Year column + Position column
- New data is appended and re-sorted by year
- Dedup pass needed at v1.0 (some players may appear in both All-Pro and Pro Bowl in the same year — that's intentional, each row = one award)
- Statistical leaders deferred to a separate document (not part of this ADR scope)

---

## Notes

- Pro Bowl was replaced by "Pro Bowl Games" format in 2023; selections still recorded as `Pro Bowl` for consistency
- All-Pro 1st and 2nd team are separate award values, not collapsed
- Super Bowl MVP is recorded in the year of the *season* played (e.g., Super Bowl played in Feb 2025 → Year = 2024)

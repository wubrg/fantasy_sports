# ADR-002: Award Taxonomy for the Pre-1994 / Pre-Merger Era (1960–1993)

**Status:** Accepted
**Date:** 2026-06-28
**Deciders:** wubrg

---

## Context

The dataset is being extended backward from 1994 to 1960 (AFL founding), per the
Roadmap note in `../README.md`. 1960–1969 was a genuinely two-league era — the
AFL and NFL operated as separate organizations with separate MVP awards,
separate All-League teams, and separate all-star games — before merging in
1970. Several of the existing named-award codes (from ADR-001) also didn't
exist yet for parts of this range, even on the NFL side. Folding all of this
into ADR-001's fixed taxonomy without comment would silently misrepresent
which awards actually existed when, so this ADR records the decisions
needed to extend it correctly.

This ADR does not revisit team codes: confirmed by checking the existing
1994-2025 data, relocated/renamed franchises are already recorded under their
*current* franchise code regardless of the team's name/city at the time (e.g.
1994 LA Raiders → `LV`, 1996 Tennessee Oilers → `TEN`). The same lineage-code
convention extends cleanly back to 1960 with zero new team codes:

| Code | Franchise as it existed 1960–1993 |
|---|---|
| `TEN` | Houston Oilers (1960–1996, before the Tennessee/Titans rename) |
| `IND` | Baltimore Colts (1960–1983) |
| `LV`  | Oakland Raiders (1960–1981), LA Raiders (1982–1994) |
| `ARI` | St. Louis Cardinals (1960–1987), Phoenix Cardinals (1988–1993) |
| `LAC` | LA Chargers (1960 only), San Diego Chargers (1961–1993) |
| `KC`  | AFL Dallas Texans (1960–1962, before moving to KC) |
| `NYJ` | AFL New York Titans (1960–1962, before renaming to Jets) |

All other teams existing in this window map to their already-seeded code with
no ambiguity (expansion teams just have a later first-eligible year — e.g. no
`MIN` rows before 1961, no `ATL` before 1966, no `NO` before 1967, no `CIN`
before 1968, no `TB`/`SEA` before 1976, no `CAR`/`JAX` before 1995, etc. —
that's a data-entry fact, not a schema concern).

---

## Decision

### A. NFL-side named awards: same codes, narrower year ranges

These ADR-001 codes are reused unchanged for the NFL side of 1960–1993, but
several didn't exist for the entire window — don't backfill rows for years
before an award existed:

| Code | Valid from | Note |
|---|---|---|
| `AP MVP` | 1957 (continuous) | Covers the full 1960–1993 range. Officially named "NFL Most Outstanding Player" pre-1961 and "Player of the Year" in 1962; recorded as `AP MVP` regardless, per ADR-001's existing naming-is-standardized-not-literal precedent. |
| `AP OROTY` | 1957 (continuous) | Never split from a unified award — AP named an *offensive* rookie of the year from the very start. Covers the full range. |
| `AP DROTY` | 1967+ | No rows 1960–1966. |
| `AP DPOY` | 1971+ | No rows 1960–1970. |
| `AP OPOY` | 1972+ | No rows 1960–1971. |
| `AP CPOTY` | 1963–1966 only | The AP gave this award only briefly before a long gap (not resumed until 1998, outside this ADR's scope). Only seasons 1963, 1964, 1965, 1966 get a row; none 1960–1962 or 1967–1993. |
| `SB MVP` | 1966+ | Super Bowl I covered the 1966 season (played Jan 1967), per ADR-001's existing "recorded in the year of the season played" convention. No rows 1960–1965 — there was no Super Bowl yet. |
| `All-Pro 1st` / `All-Pro 2nd` | 1940 (continuous) | AP All-Pro predates this whole window; applies to NFL teams every year 1960–1993. Does **not** apply to AFL teams 1960–1969 (see below). |
| `Pro Bowl` | 1951 (continuous) | NFL's own all-star game; applies to NFL teams every year 1960–1969, and to every team 1970–1993 post-merger. Does **not** apply to AFL teams 1960–1969 (see below). |

### B. AFL-side (1960–1969 only): four new award codes

The AFL ran its own, separate award structure for its entire ten-season
existence, distinct from the NFL's. Rather than conflating it into the NFL
codes above (which would misrepresent two different leagues' honors as one),
add four new codes, valid only for AFL teams in 1960–1969:

| Code | Name | Maps to (NFL-side equivalent) |
|---|---|---|
| `AFL MVP` | AFL Most Valuable Player / Player of the Year | `AP MVP` |
| `AFL ROY` | AFL Rookie of the Year | `AP OROTY` (the AFL also didn't split O/D until partway through; see source note below) |
| `All-AFL 1st` / `All-AFL 2nd` | All-AFL Team | `All-Pro 1st` / `All-Pro 2nd` |
| `AFL All-Star` | AFL All-Star Game selection | `Pro Bowl` |

No AFL equivalents are added for `AP DPOY`/`AP OPOY`/`AP CPOTY`/`SB MVP` — the
AFL had no separate award resembling these during its run.

**Source-of-record note for `AFL MVP`/`AFL ROY`:** unlike the NFL side (where
the AP has been the sole consistent voting body since 1957), AFL-era awards
were named by competing bodies — AP, UPI, and The Sporting News — who
sometimes disagreed in the same season (e.g. 1963's AFL MVP was a three-way
split: UPI picked Lance Alworth, AP picked Tobin Rote, Sporting News picked
Clem Daniels). To keep one row per award per year, the same AP-first
precedent ADR-001 already established is reused: take the AP's pick where AP
made one for that season; if AP didn't select that year, fall back to UPI and
flag it `[UPI]` in the `nt` field (mirroring the existing `[verify]`
convention for lower-confidence rows). AFL ROY follows the same precedent —
AP started selecting in 1961 (matching UPI's pick those years); the 1960
season falls back to UPI and gets `[UPI]`. From 1967 on, AP split AFL ROY
into offense/defense the same year it did for the NFL side — those rows use
the existing `AP OROTY`/`AP DROTY` codes directly rather than `AFL ROY`,
since at that point the AFL's rookie award already had the same
offense/defense structure as the NFL's.

### C. `units` and `teams` reference tables: unchanged

No new rows needed in either table — see the franchise-lineage table above
for teams; units (`O`/`D`/`ST`) are unaffected by era.

### D. Sourcing: Wikipedia + web search, not direct PFR fetches

The original 1994–2025 build's source strategy (`build-plan.md`) lists
Pro Football Reference page-per-award URLs. Live-testing in this environment
found `pro-football-reference.com` returns HTTP 403 to automated fetches —
its anti-bot wall blocks the tool used here, unlike when the original chunks
were built. Wikipedia's award-specific pages (`AP NFL Offensive Player of
the Year`, `American Football League Most Valuable Player award`, etc.) and
aggregated web search are used instead. Where a search result surfaces a PFR
page's content as a snippet without a full fetch, or where multiple
secondary sources disagree, the row gets a `[verify]` note, same as the
original build's Pro Bowl depth-chart uncertainty.

---

## Consequences

- Award taxonomy grows from 10 codes (ADR-001) to 14: the 10 existing codes,
  reused with narrower year ranges where applicable, plus `AFL MVP`,
  `AFL ROY`, `All-AFL 1st`, `All-AFL 2nd`, `AFL All-Star`.
- `internal/store/store.go`'s `awardSeed`/`awardOrder` need the four new
  codes added (no schema/column changes — same `award_types` reference table,
  just more seeded rows).
- `docs/ui-spec.md`'s award list and team-year-range copy need updating to
  describe the new codes and the 1960 start year.
- Confidence is necessarily lower for 1960–1969 than for 1994–2025: more
  competing sources, no direct PFR access. Expect a higher `[verify]` rate
  for this window than the ~2% rate ADR-001's build achieved.
- 1970–1993 (post-merger, pre-cap) needs **no new codes at all** — it's
  structurally identical to 1994–2025, just earlier. That portion can be
  built as a straightforward extension; the AFL-era 1960–1969 portion is the
  one that needs the new taxonomy above.

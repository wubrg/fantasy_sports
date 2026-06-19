# NFL Awards Reference

Master reference for NFL named AP awards, Super Bowl MVPs, All-Pro selections, and
Pro Bowl selections, 1994–2025, across all 32 franchises.

## Files

- `NFL_AWARDS_REFERENCE_v0.8.md` — current master document (flat table, 4,498 rows, per ADR-001)
- `data/nfl_awards_data.json` — same dataset as structured JSON (`meta` + `data`), used by the lookup app/UI
- `docs/ADR-001-awards-reference-structure.md` — why the flat-table structure was chosen
- `docs/build-plan.md` — chunked build plan and source strategy (PFR, etc.)
- `docs/stat-lookup-guide.md` — lookup format and source priority for player season-stat chat queries
- `docs/ui-spec.md` — spec for the interactive filterable awards browser UI
- `archive/team_breakdowns_v0.2/` — early per-team award docs (v0.2). Superseded by the v0.8 master
  and JSON, which include all-Pro 1994–2009 and Pro Bowl data the v0.2 team docs lack. Kept for
  reference only; only 18 of 32 teams have a v0.2 file.

## Data Quality

- Named AP awards, SB MVP, All-Pro 1st/2nd: fully verified
- Pro Bowl: high confidence 1999–2025 (Wikipedia-sourced); some `[verify]` flagged entries in
  1994–1998 and 2013–2018 (real selections, lower-confidence details)

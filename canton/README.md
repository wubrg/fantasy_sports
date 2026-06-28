# Canton

Master reference for NFL named AP awards, Super Bowl MVPs, All-Pro selections, and
Pro Bowl selections, 1994–2025, across all 32 franchises. ("Canton" is the
short, Tailscale-URL-friendly name for the app that used to be called
`nflawards` — see Roadmap below for why.)

## Files

- `NFL_AWARDS_REFERENCE_v0.8.md` — historical master document (flat table, 4,498 rows, per ADR-001).
  No longer updated by hand; see `app/` for the live data source.
- `data/canton_data.json` — version-controlled snapshot of the dataset (`meta` + `data`). Not the
  live source; regenerate with `app/cantonctl export-json` after making changes via the app's database.
- `data/canton.db` — the live SQLite database the app reads/writes (gitignored; build it from the
  JSON snapshot, see `app/README.md`).
- `docs/ADR-001-awards-reference-structure.md` — why the flat-table structure was chosen
- `docs/build-plan.md` — chunked build plan and source strategy (PFR, etc.)
- `docs/stat-lookup-guide.md` — lookup format and source priority for player season-stat chat queries
- `docs/ui-spec.md` — spec for the interactive filterable awards browser UI
- `app/` — Go server + `cantonctl` admin CLI; see `app/README.md` for setup, updating data, and running it
  over Tailscale
- `quicklook.html` — single-file, no-server mobile lookup: team / offense-or-defense / year-range
  filters over a snapshot of `data/canton_data.json` embedded directly in the page. Open it
  straight from disk (or any static host) on a phone for quick lookups before the full Tailscale-backed
  `app/` is deployed. It's a static snapshot, not live — regenerate it after the data changes (see the
  comment at the top of the file).
- `archive/team_breakdowns_v0.2/` — early per-team award docs (v0.2). Superseded by the v0.8 master
  and JSON, which include all-Pro 1994–2009 and Pro Bowl data the v0.2 team docs lack. Kept for
  reference only; only 18 of 32 teams have a v0.2 file.

## Data Quality

- Named AP awards, SB MVP, All-Pro 1st/2nd: fully verified
- Pro Bowl: high confidence 1999–2025 (Wikipedia-sourced); some `[verify]` flagged entries in
  1994–1998 and 2013–2018 (real selections, lower-confidence details)
- Structural checks (run against `canton_data.json`): no missing fields, no invalid team/award/unit
  codes, zero exact duplicate rows, full year coverage for every named award. The SQLite schema now
  enforces the first three of those automatically on every future write.

## Roadmap

Renamed from `nflawards` to `canton` (June 2026) for two reasons: a shorter
Tailscale URL (`/canton` vs `/nflawards`), and because the app is expected to
broaden beyond a pure awards-and-Pro-Bowl reference into a more general NFL
historical/stat lookup tool over time. What exactly that grows into — season
stat lookups, more award categories, something else — is not planned out
yet; this note is just a placeholder marking the intent so the name doesn't
need to change again later. No scope work has started.

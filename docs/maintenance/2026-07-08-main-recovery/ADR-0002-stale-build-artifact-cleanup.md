# ADR-0002: Delete stale football/ build artifacts

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg

---

## Context

Origin's repo-flatten commit (`7d519c3`) moved all tracked source out of `football/` into root-level `canton/`, `league_home/`, `leagues/`, `archive/`. After that move, `football/` still contained ~54MB of **untracked** leftovers from the old layout:

```
football/nfl_awards/app/nflctl
football/nfl_awards/app/nflawards
football/nfl_awards/data/nfl_awards.db (+ -shm/-wal)
football/league_home/app/leagueweb
football/league_home/app/leaguebot
football/league_home/app/leaguectl
```

`git ls-files football/` returns nothing — none of this was ever tracked. `canton/app/README.md` confirms the `.db` is intentionally not checked into git ("binary, no useful diffs... Build it") and is regenerated from `canton/data/canton_data.json`. A fresher `canton/data/canton.db` (1.7MB, includes the 1960–1993 historical load) already exists at the new location, dated after the old `nfl_awards.db` (835KB, pre-historical-load).

## Decision

Delete `football/` entirely once the merge is committed. Nothing in it is tracked, referenced by current Makefiles, or newer than its replacement at the new path.

## Consequences

- Frees ~54MB.
- Removes the untracked-files noise from `git status`.
- If `canton`/`leagueweb` binaries are needed again, `make build` regenerates them at the new `canton/app` / `league_home/app` paths.

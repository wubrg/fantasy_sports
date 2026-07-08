# Plan: Get main healthy (v1.1)

**Date:** 2026-07-08
**Status:** Blocked on Chunk 5 (needs your local verification)

## Background

See `REQUEST.md` for the original ask and `ADR-0001` through `ADR-0004` for the decisions behind each step below.

`main` diverged from `origin/main` after a Claude Code session landed a large repo reorg (flatten `football/` → root, rename `nfl_awards` → `canton`, archive ruby/python) plus 9 follow-on commits, while a local-only commit hardcoded Mac paths into two plist templates. A merge was started and left mid-conflict.

## Chunks

### Chunk 1 — Diagnosis (complete)
- Confirmed merge state, conflict scope (2 files), divergence (1 vs 10 commits), stale `football/` artifacts, stray branch/stash, remote layout.

### Chunk 2 — Decisions (complete)
- All 4 decisions captured in ADR-0001–0004 and `REQUEST.md`.

### Chunk 3 — Resolve + verify (blocked on verification, otherwise complete)
1. ✅ Resolved `canton/app/com.canton.serve.plist.template` and `league_home/app/com.leagueweb.serve.plist.template` per ADR-0001 (hardcoded paths, updated to new locations).
2. ✅ Staged the resolved files, completed the merge commit (`9b0cd26`, "Merge branch 'master'").
3. ⚠️ **Build/test verification could not be run from this session** — the Cowork sandbox has no Go toolchain and no permission to install one (no sudo, apt locked down, go.dev not reachable). **Action needed from you:** run `make check` (or `go build ./... && go test ./...` in `canton/app` and `league_home/app`) locally and let me know the result.
4. Recorded in `CHANGELOG.md`.

### Chunk 4 — Cleanup (complete)
1. ✅ Deleted `football/` (ADR-0002) — freed 54MB.
2. ✅ Deleted local `master` branch and `claude/league-home-sleeper-verify-89djru` branch + its orphaned stash (ADR-0003).
3. Recorded in `CHANGELOG.md`.

### Chunk 5 — Push (blocked)
1. **Waiting on Chunk 3 step 3** — do not push until `make check` passes locally, per ADR-0004.
2. Once you confirm, push `main` to `origin/main`.
3. Final `git status` / `git log` sanity check, record in `CHANGELOG.md`.

## Open items (not in scope for this pass)

- Older stashes `stash@{1}`–`{4}` — left untouched per ADR-0003, may be worth a separate review/cleanup pass.
- Remote branches `origin/claude/nfl-stat-master-migration-29t40x` and `origin/rules-and-proceedings-updates` — open work, not touched.
- The Makefile's `canton-install`/`leagueweb-install` targets still print "fill in the /REPLACE/WITH/... placeholders" even though the templates are now pre-filled (ADR-0001 consequence) — cosmetic, not fixed here.

## Progress tracker

| Chunk | Status |
|---|---|
| 1. Diagnosis | ✅ Done |
| 2. Decisions | ✅ Done |
| 3. Resolve + verify | ⚠️ Merge done; build/test verification needs you (no Go in sandbox) |
| 4. Cleanup | ✅ Done |
| 5. Push | ⬜ Blocked on Chunk 3 verification |

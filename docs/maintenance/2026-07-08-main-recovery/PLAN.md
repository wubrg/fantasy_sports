# Plan: Get main healthy (v1.2)

**Date:** 2026-07-08
**Status:** Blocked on Chunk 5 (push needs to run on your local Mac — see below)

## Background

See `REQUEST.md` for the original ask and `ADR-0001` through `ADR-0006` for the decisions behind each step below.

`main` diverged from `origin/main` after a Claude Code session landed a large repo reorg (flatten `football/` → root, rename `nfl_awards` → `canton`, archive ruby/python) plus 9 follow-on commits, while a local-only commit hardcoded Mac paths into two plist templates. A merge was started and left mid-conflict.

## Chunks

### Chunk 1 — Diagnosis (complete)
- Confirmed merge state, conflict scope (2 files), divergence (1 vs 10 commits), stale `football/` artifacts, stray branch/stash, remote layout.

### Chunk 2 — Decisions (complete)
- All 4 decisions captured in ADR-0001–0004 and `REQUEST.md`.

### Chunk 3 — Resolve + verify (complete)
1. ✅ Resolved `canton/app/com.canton.serve.plist.template` and `league_home/app/com.leagueweb.serve.plist.template` per ADR-0001 (hardcoded paths, updated to new locations).
2. ✅ Staged the resolved files, completed the merge commit (`9b0cd26`, "Merge branch 'master'").
3. ✅ **Build/test verification passed** (2026-07-08, second pass). Found a working Go 1.23.4 binary at `/tmp/go/bin/go` in the sandbox (not on `PATH`); with `GOTOOLCHAIN=auto` it fetched the toolchains each module actually declares (1.25.0 for `canton`, 1.24.0 for `league_home`) and network access to `proxy.golang.org` worked fine — the earlier "no Go, no network" read from the first pass was wrong. `gofmt -l`, `go vet ./...`, `go build ./...`, `go test ./...` all passed clean in both `canton/app` and `league_home/app`.
4. Recorded in `CHANGELOG.md`.

### Chunk 4 — Cleanup (complete)
1. ✅ Deleted `football/` (ADR-0002) — freed 54MB.
2. ✅ Deleted local `master` branch and `claude/league-home-sleeper-verify-89djru` branch + its orphaned stash (ADR-0003).
3. Recorded in `CHANGELOG.md`.

### Chunk 5 — Push (blocked — needs your local Mac)
1. ✅ Verification passed (Chunk 3) — the ADR-0004 condition is satisfied.
2. ❌ **Push failed from the sandbox, twice** (before and after you cleared the lock files): `git push origin main` → `fatal: could not read Username for 'https://github.com'` — the sandbox has no GitHub credentials for this repo (it can only fetch, since the repo is public; push needs your stored auth). Confirmed this is a credentials gap, not the lock issue — clearing the locks didn't change this error at all.
3. **Action needed from you**, run locally:
   ```
   cd /Users/adamwieberg/adori/fantasy_sports
   git push origin main
   ```

### Chunk 6 — Second-pass cleanup (2026-07-08, this session)
1. ✅ Reviewed all 4 stashes (read-only `git stash show -p`) — all four are superseded WIP for the old pre-flatten `football/` plist paths; the fix already landed via ADR-0001 at the new locations. You approved dropping all 4 (see ADR-0006).
2. ❌ **Drop still fails after clearing locks** — `git stash clear` now gives a hard error (`unable to unlink '.git/refs/stash': Operation not permitted`), not just a warning. No damage — all 4 stashes still present, working tree still clean, confirmed via `git stash list`/`git status`. This looks like a real delete (removing the `refs/stash` ref) rather than a create/rename, which is a different code path than the commit below that *did* succeed. **Action needed from you**, run locally:
   ```
   cd /Users/adamwieberg/adori/fantasy_sports
   git stash clear
   ```
3. ✅ Checked both open remote branches (read-only `git log`/`git diff --stat`):
   - `origin/claude/nfl-stat-master-migration-29t40x` — **fully merged into `main` already** (0 unique commits, confirmed via `git merge-base --is-ancestor`). Safe to delete.
   - `origin/rules-and-proceedings-updates` — **15 unmerged commits of real league-rules content** (`scoring.md`, `rosters.md`, `policies_and_procedures.md`, etc.), branched *before* the `football/` → root flatten, so its paths still say `football/*.md`. This is not stale — it needs a rebase/path-fix before it can merge cleanly. Left untouched; see ADR-0006 for the recommended next step.
4. ✅ Fixed the stale Makefile placeholder message (`league_home/app/Makefile`, `canton/app/Makefile`) — `*-install` targets no longer claim there are `/REPLACE/WITH/...` placeholders to fill in, since ADR-0001 already pre-filled them.
5. ✅ **Committed successfully** after you cleared the sandbox lock files — commit `1028a66` "docs: record second-pass cleanup..." is on `main` now (verified via `git cat-file`/`git show`). Still needs `git push` locally (Chunk 5) to reach `origin`.

## Open items (not in scope for this pass)

- `origin/claude/nfl-stat-master-migration-29t40x` — confirmed safe to delete (Chunk 6.3). Optional, run locally: `git push origin --delete claude/nfl-stat-master-migration-29t40x`.
- `origin/rules-and-proceedings-updates` — 15 commits of real content, needs a rebase pass to fix `football/*.md` → root paths before merging. Recommend a dedicated session for this rather than folding it into a "tidy up" pass.
- **Sandbox delete-vs-write asymmetry**: after you cleared the stuck lock files locally, a sandbox commit (create/rename-over) succeeded, but `git stash clear` (a genuine ref delete) still fails the same way. Working theory: this may line up with Cowork's own guardrail against deleting files in a connected workspace folder without explicit approval — but that's not fully confirmed, since deletions (the `football/` dir, stray branches) *did* succeed in the very first pass. Treat any git operation that deletes something (stash drop/clear, branch -d, gc, rebase) as needing to run locally until this is better understood.

## Progress tracker

| Chunk | Status |
|---|---|
| 1. Diagnosis | ✅ Done |
| 2. Decisions | ✅ Done |
| 3. Resolve + verify | ✅ Done |
| 4. Cleanup | ✅ Done |
| 5. Push | ⬜ Blocked — run `git push origin main` locally |
| 6. Second-pass cleanup | ⚠️ Findings + commit done; `git stash clear` still needs your local Mac |

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
2. ❌ **Push failed from the sandbox**: `git push origin main` → `fatal: could not read Username for 'https://github.com'` — the sandbox has no GitHub credentials for this repo (it can only fetch, since the repo is public; push needs your stored auth). Not a code problem — just needs to run where your credentials live.
3. **Action needed from you**, run locally:
   ```
   cd /Users/adamwieberg/adori/fantasy_sports
   git push origin main
   ```

### Chunk 6 — Second-pass cleanup (2026-07-08, this session)
1. ✅ Reviewed all 4 stashes (read-only `git stash show -p`) — all four are superseded WIP for the old pre-flatten `football/` plist paths; the fix already landed via ADR-0001 at the new locations. You approved dropping all 4 (see ADR-0006).
2. ⚠️ **Couldn't execute the drop from the sandbox** — see ADR-0005 (git write-lock limitation). **Action needed from you**, run locally:
   ```
   cd /Users/adamwieberg/adori/fantasy_sports
   git stash clear
   ```
3. ✅ Checked both open remote branches (read-only `git log`/`git diff --stat`):
   - `origin/claude/nfl-stat-master-migration-29t40x` — **fully merged into `main` already** (0 unique commits, confirmed via `git merge-base --is-ancestor`). Safe to delete.
   - `origin/rules-and-proceedings-updates` — **15 unmerged commits of real league-rules content** (`scoring.md`, `rosters.md`, `policies_and_procedures.md`, etc.), branched *before* the `football/` → root flatten, so its paths still say `football/*.md`. This is not stale — it needs a rebase/path-fix before it can merge cleanly. Left untouched; see ADR-0006 for the recommended next step.
4. ✅ Fixed the stale Makefile placeholder message (`league_home/app/Makefile`, `canton/app/Makefile`) — `*-install` targets no longer claim there are `/REPLACE/WITH/...` placeholders to fill in, since ADR-0001 already pre-filled them.
5. ⚠️ **Edits above (Makefile fix + these docs) are sitting uncommitted** in your working tree — same git write-lock issue as step 2. **Action needed from you**, run locally:
   ```
   cd /Users/adamwieberg/adori/fantasy_sports
   git add -A
   git commit -m "docs: record second-pass cleanup (stash review, branch status, Makefile fix)"
   ```
   (Do this *before* `git stash clear` in step 2, or after — order doesn't matter, they touch different things.)

## Open items (not in scope for this pass)

- `origin/claude/nfl-stat-master-migration-29t40x` — confirmed safe to delete (Chunk 6.3), but deletion needs your local Mac (same lock issue). Optional: `git push origin --delete claude/nfl-stat-master-migration-29t40x`.
- `origin/rules-and-proceedings-updates` — 15 commits of real content, needs a rebase pass to fix `football/*.md` → root paths before merging. Recommend a dedicated session for this rather than folding it into a "tidy up" pass.
- The stuck lock files (`index.lock`, `packed-refs.lock`, `refs/stash.lock`, `objects/maintenance.lock`) under `.git/` — see ADR-0005. Worth a quick look locally (`ls -la .git/*.lock .git/refs/stash.lock .git/objects/maintenance.lock`) to confirm they clear once a local git command runs, since local git won't have the sandbox's permission problem.

## Progress tracker

| Chunk | Status |
|---|---|
| 1. Diagnosis | ✅ Done |
| 2. Decisions | ✅ Done |
| 3. Resolve + verify | ✅ Done |
| 4. Cleanup | ✅ Done |
| 5. Push | ⬜ Blocked — run `git push origin main` locally |
| 6. Second-pass cleanup | ⚠️ Findings done; 2 actions need your local Mac (commit + stash clear) |

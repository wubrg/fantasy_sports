# ADR-0006: Disposition of leftover stashes and open remote branches (second pass)

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg

---

## Context

ADR-0003 (first pass) deliberately left 4 stashes (`stash@{1}`–`{4}` at the time) and 2 remote branches out of scope. This second pass ("tidy up everything") reviewed both.

### Stashes

All 4 stashes contain the same kind of change: hardcoded absolute-path edits to the plist launch-agent templates, but targeting the **old, pre-flatten** file layout (`football/league_home/app/...`, `football/nfl_awards/app/...`), which no longer exists after the repo-flatten commit (`7d519c3`).

- `stash@{0}` and `stash@{3}` are identical — basic path fix, no `-prefix` flag.
- `stash@{1}` — same fix, plus the `-prefix` flag for shared-Tailscale-hostname setups.
- `stash@{2}` — a **broken** variant: duplicated path segment (`football/football/league_home/app`) and an inconsistent path (`adamwieberg/fantasy_sports` instead of `adamwieberg/adori/fantasy_sports`).

The underlying problem all 4 were trying to solve — hardcoded local paths in the plist templates — was already solved fresh, at the correct new locations, by ADR-0001 during the merge. None of the 4 stashes apply to files that still exist at those paths.

### Remote branches

- `origin/claude/nfl-stat-master-migration-29t40x` — `git merge-base --is-ancestor origin/claude/nfl-stat-master-migration-29t40x main` returns true; `main` is 30 commits ahead of it with 0 commits unique to the branch. Fully absorbed into `main` already.
- `origin/rules-and-proceedings-updates` — 15 commits ahead of `main`, none merged. `git merge-base main origin/rules-and-proceedings-updates` lands on `34b28f0` ("everything but rosters and scoring finished"), which predates the `7d519c3` flatten commit. The branch's file paths (`football/communication.md`, `football/scoring.md`, `football/policies_and_procedures.md`, etc.) reflect the pre-flatten layout and would conflict/misplace if merged as-is. Diffstat: 9 files, +359/-206 — real league-rules content (scoring, rosters, dues, policies, role history), not scaffolding.

## Options considered

**Stashes:**
1. Drop all 4 — nothing left to recover, already superseded.
2. Keep them "just in case."

**`nfl-stat-master-migration-29t40x`:**
1. Delete the remote branch — fully merged, no unique content.
2. Leave it.

**`rules-and-proceedings-updates`:**
1. Merge as-is now.
2. Delete it (it's "old").
3. Leave it untouched and flag that it needs a rebase/path-fix pass before merging.

## Decision

- **Stashes:** drop all 4 (wubrg confirmed). Execution deferred to local Mac per ADR-0005.
- **`nfl-stat-master-migration-29t40x`:** confirmed safe to delete; left as an open item for wubrg to action locally (`git push origin --delete claude/nfl-stat-master-migration-29t40x`) rather than executed automatically in this pass, since branch deletion wasn't explicitly asked for this session — only a status check was.
- **`rules-and-proceedings-updates`:** **left untouched.** Option 1 (merge as-is) would misplace 9 files under a `football/` directory that no longer exists. Option 2 (delete) would destroy 15 commits of real, un-landed league-rules content — not appropriate to do silently as part of a "tidy up" pass. Recommend treating the rebase/path-fix as its own dedicated task, not folded into general repo maintenance.

## Consequences

- Stash list will be empty once wubrg runs `git stash clear` locally.
- `origin/claude/nfl-stat-master-migration-29t40x` remains on the remote until wubrg explicitly deletes it — flagged in `PLAN.md` open items, not actioned here.
- `origin/rules-and-proceedings-updates` remains as-is. Whoever picks up that rebase should expect to move each touched file from `football/<name>.md` to `<name>.md` (or wherever the equivalent content now lives, if it was folded into `README.md`/`CONTRIBUTING.md` during the flatten) and resolve content conflicts against whatever's changed at the new locations since `34b28f0`.

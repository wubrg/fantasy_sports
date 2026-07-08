# ADR-0003: Clean up stray branch and orphaned stash

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg

---

## Context

`git branch -vv -a` showed two local branches beyond `main`:

- `master` — sitting at `859d33c`, identical to `origin/main`'s tip (`origin/master` is an alias of `origin/main`). This was the source ref for the in-progress merge (`MERGE_MSG: "Merge branch 'master'"`).
- `claude/league-home-sleeper-verify-89djru` — a Claude Code verification branch at `5d62a43` ("Add season history and a season selector to league home"), 7 commits behind its own remote counterpart, and already superseded — that same commit content landed on `main` via `678d5d1`.

There's also an orphaned stash tied to that branch (`stash@{0}: WIP on claude/league-home-sleeper-verify-89djru: ...`), plus 4 older stashes on `main` from earlier plist/tailscale work (`stash@{1}`–`{4}`).

## Decision

Delete the local `master` branch (pure duplicate of `origin/main`, not needed once the merge lands) and the local `claude/league-home-sleeper-verify-89djru` branch + its associated stash (`stash@{0}`), since that work is already merged into `main`.

**Left untouched:** `stash@{1}`–`{4}` (older WIP unrelated to this merge — out of scope for this pass; flagged for a separate review) and the two remote-only branches `origin/claude/nfl-stat-master-migration-29t40x` and `origin/rules-and-proceedings-updates`, which represent other open work not touched by this recovery.

## Consequences

- `git branch -a` and `git stash list` become less noisy.
- No commit content is lost — everything on the deleted branch/stash is already reachable from `main` (`678d5d1`) or was superseded by later work.
- If any of the older stashes (`{1}`–`{4}`) or the two open remote branches are still wanted, they remain available for a follow-up pass.

# ADR-0005: Hand off remaining git write operations to the local Mac

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg (implicit — environment constraint, not a preference choice)

---

## Context

During the second-pass "tidy up" session, routine git commands run from the Cowork sandbox against the mounted repo (`git fetch`, later `git stash clear`) left behind lock files that could not be removed from the sandbox side:

- `.git/index.lock`
- `.git/objects/maintenance.lock`
- `.git/refs/stash.lock`
- `.git/packed-refs.lock`

Every attempt to delete them (`rm -f`) failed with `Operation not permitted`, even though `stat` showed them owned by the sandbox's own user. This is consistent with a permissions/flag boundary between the sandbox's view of the mounted folder and the real filesystem on the Mac — not a live concurrent git process (repeated retries with delays didn't clear it, and the files were created *by the sandbox's own commands*, not by anything else running in the repo).

Once `index.lock` existed, every subsequent git write in the sandbox — `git add`, `git commit`, `git stash clear`/`drop`, `git push` — failed immediately with "Another git process seems to be running in this repository... remove the file manually to continue." A test `git add`/`git checkout --` on a throwaway `README.md` edit confirmed this: `add` failed outright, and `checkout --` also failed, so the test edit had to be reverted with a direct file edit instead of git.

Reads were unaffected throughout: `git status`, `git log`, `git diff`, `git fetch`, `git stash list`, `git stash show -p`, and `git merge-base` all worked normally even with the stale locks present.

## Options considered

1. **Keep retrying / try to force-clear the locks from the sandbox.** Already attempted (sleep + retry, direct `rm -f`) — didn't work. Continuing to hammer at it risks leaving more stuck lock files without resolving anything.
2. **Do all remaining git writes (commit, stash clear, push) from the sandbox anyway, ignoring the errors.** Not viable — the commands fail outright, they don't silently corrupt anything, but they also don't do anything.
3. **Stop attempting git writes from the sandbox for the rest of this session; document exactly what still needs to happen, and hand those specific commands to wubrg to run locally**, where git won't hit this permission boundary. Continue using read-only git commands (status/log/diff/fetch) for any further investigation this session.

## Decision

**Option 3.** No commits, stashes, or pushes are attempted from the sandbox for the remainder of this session. Everything that still needs a git write is written up as an exact command block in `PLAN.md` (Chunk 5 and Chunk 6) for wubrg to run locally. File edits that don't require git (Makefile fix, doc updates) were still made directly and are left as uncommitted working-tree changes for wubrg to review and commit.

## Consequences

- Push (Chunk 5) and stash clear + commit (Chunk 6) are now blocked on wubrg running a short list of commands locally — see `PLAN.md`.
- No data was lost or corrupted; `git status`/`git log` confirm the working tree and commit history are exactly where the first-pass session left them.
- Worth a quick local check (`ls -la .git/*.lock .git/refs/stash.lock .git/objects/maintenance.lock`) after running local git commands, to confirm the locks clear on their own once a local (non-sandboxed) git process touches the repo. If they don't clear locally either, that's a different, more concerning problem worth investigating on its own.
- Future Cowork sessions on this repo should expect git writes to work for the *first* command or two in a session, then potentially lock up — plan git-heavy work accordingly, or verify lock-file state early.

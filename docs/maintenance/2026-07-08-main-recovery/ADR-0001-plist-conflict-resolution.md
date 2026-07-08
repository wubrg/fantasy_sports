# ADR-0001: Resolve plist.template merge conflicts by keeping hardcoded local paths

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg

---

## Context

Merging `origin/main` into local `main` conflicted in two files:

- `canton/app/com.canton.serve.plist.template`
- `league_home/app/com.leagueweb.serve.plist.template`

Both sides touched the same lines:

- **HEAD** (local commit `f06b27e`, "hardcode local mac paths"): replaced the `/REPLACE/WITH/ABSOLUTE/PATH/TO/...` placeholders with the real absolute path `/Users/adamwieberg/adori/fantasy_sports/football/...` (old, pre-rename location).
- **origin/main** (Claude Code's repo-flatten commit `7d519c3` and later): kept the placeholder convention, just updated it to reflect the new `canton/` and `league_home/` locations.

The Makefiles (`canton-install`, `leagueweb-install` targets) copy the `.template` file to `~/Library/LaunchAgents/` and print "Edit ... to fill in the /REPLACE/WITH/... placeholders" — i.e., the documented workflow assumes the checked-in template stays generic and gets filled in *after* copying.

## Options considered

1. **Keep placeholder convention** (update placeholder text to new paths, drop the local hardcoding). Matches the documented Makefile workflow; keeps personal absolute paths out of git history.
2. **Keep hardcoded path** (update the real absolute path to point at the new `canton/app` / `league_home/app` locations instead of the old `football/...` ones).

## Decision

**Option 2 — keep hardcoded path**, updated to the new post-rename locations. wubrg runs this repo solo on a single known Mac, and prefers the template to be immediately usable without a manual post-copy edit step. Personal-path leakage into a single-user repo was judged an acceptable tradeoff for convenience.

## Consequences

- `canton/app/com.canton.serve.plist.template` and `league_home/app/com.leagueweb.serve.plist.template` now contain `/Users/adamwieberg/adori/fantasy_sports/canton/app/...` and `/Users/adamwieberg/adori/fantasy_sports/league_home/app/...` respectively — real paths, not placeholders.
- The Makefile's printed instruction ("Edit ... to fill in the /REPLACE/WITH/... placeholders") is now slightly stale for this repo's actual state — there's nothing left to replace. Not fixed as part of this pass; flagged as a follow-up.
- If this repo is ever cloned to a different machine/path, these two template files will need manual editing again (the thing placeholders were meant to signal).

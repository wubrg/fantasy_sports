# Changelog: main-recovery-2026-07-08

## 2026-07-08

- Diagnosed repo state: `main` diverged from `origin/main` (1 vs 10 commits), merge in progress with 2 conflicting plist template files, ~54MB stale `football/` build artifacts, 1 stray branch + orphaned stash.
- Captured request, decisions (ADR-0001–0004), and plan.
- Resolved both plist template conflicts per ADR-0001 (hardcoded paths updated to new `canton/app` / `league_home/app` locations).
- Completed the merge: commit `9b0cd26` "Merge branch 'master'" on `main`.
- **Could not run `go build`/`go test` verification from the Cowork sandbox** — no Go toolchain installed, no sudo/apt access, `go.dev` not reachable from the sandbox network allowlist. Verification deferred to wubrg running `make check` locally.
- Deleted stale `football/` directory (54MB, untracked pre-rename build artifacts) per ADR-0002.
- Deleted local `master` branch and `claude/league-home-sleeper-verify-89djru` branch + its orphaned stash per ADR-0003.
- Push to `origin/main` held per ADR-0004 pending local build/test confirmation.

## 2026-07-08 (second pass — "tidy up" request)

- Re-ran verification: found a working Go 1.23.4 binary at `/tmp/go/bin/go` (not on `PATH`) in the sandbox, and confirmed `proxy.golang.org` **is** reachable — the first pass's "no Go, no network" was inaccurate. With `GOTOOLCHAIN=auto`, `gofmt -l`, `go vet ./...`, `go build ./...`, and `go test ./...` all passed clean in both `canton/app` (toolchain 1.25.0) and `league_home/app` (toolchain 1.24.0).
- Attempted `git push origin main` — failed: no GitHub credentials in the sandbox (`fatal: could not read Username for 'https://github.com'`). Push must run locally.
- Discovered a **sandbox git write-lock limitation** (ADR-0005): `.git/index.lock`, `.git/objects/maintenance.lock`, `.git/refs/stash.lock`, and `.git/packed-refs.lock` all got created by routine git commands (`fetch`, `stash clear`) and could not be removed (`Operation not permitted`), which then blocked every further git write (add/commit/stash clear) for the rest of the session. Reads (`status`, `log`, `diff`, `fetch`) kept working throughout. A test edit to `README.md` was made and reverted via direct file edit (not git) to confirm the working tree was still clean.
- Reviewed all 4 leftover stashes (read-only) — all superseded WIP for pre-flatten `football/` plist paths, already fixed elsewhere via ADR-0001. wubrg approved dropping all 4 (ADR-0006); execution blocked by the lock issue above, handed off to local Mac.
- Checked the 2 open remote branches (read-only): `claude/nfl-stat-master-migration-29t40x` is fully merged into `main` (safe to delete); `rules-and-proceedings-updates` has 15 unmerged commits of real league-rules content, based pre-flatten so its paths still say `football/*.md` — needs a rebase, not stale (ADR-0006).
- Fixed the stale Makefile placeholder message in `league_home/app/Makefile` and `canton/app/Makefile` (`*-install` targets no longer claim `/REPLACE/WITH/...` placeholders need filling in, since ADR-0001 pre-filled them).
- Docs updated: `PLAN.md` → v1.2, this changelog, `ADR-0005`, `ADR-0006`. Makefile fix + these doc edits sit uncommitted, pending local commit (git write-lock).

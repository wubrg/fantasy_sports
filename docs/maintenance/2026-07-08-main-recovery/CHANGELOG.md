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

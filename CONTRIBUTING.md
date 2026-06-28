# Contributing

This repo holds two independent Go modules:

- `league_home/app` (module `leaguehome`) — `leaguectl`, `leaguebot`,
  `leagueweb`. See `league_home/README.md`.
- `canton/app` (module `canton`) — `canton`, `cantonctl`. See
  `canton/app/README.md`.

Everything else in the repo (the `leagues/*` league documents, the
`archive/python` script, the `archive/ruby` gemfile stub) is unrelated
reference material, not part of either Go module.

## Commands

Run `make list` to see every available target with a description — it's the
canonical reference, not duplicated here. It works the same from the repo
root (operates on both modules) or from inside either module's own directory
(`league_home/app`, `canton/app`; operates on just that one), since each has
its own Makefile and the root Makefile is a thin delegator into both.

Common targets: `build`, `test`, `vet`, `fmt`, `fmt-check`, `lint`, `clean`,
`check` (the bundle to run before committing — `fmt-check` + `vet` + `test`).

`lint` runs `golangci-lint` with its default linter set (no `.golangci.yml`
is checked in). It currently reports pre-existing `errcheck` findings in both
modules (unchecked errors on `Close()`/`Parse()`/`Encode()` calls) — that's
why `lint` isn't part of `check`. Fixing those is fair game for a PR, but not
a blocker for unrelated changes.

## Before committing

Run `make check` from the repo root. It must pass before you commit.

## Deployment

Both `leagueweb` and `canton` can run persistently on a Mac via `launchd`
+ `tailscale serve`. The full walkthrough (one-time setup, plist
placeholders, path-mount caveats) lives in `league_home/README.md`
and `canton/app/README.md` — don't duplicate it here. Once
that one-time setup is done, the day-to-day commands are `make` targets:
`leagueweb-load`/`-unload`/`-restart`/`-status`,
`leagueweb-serve-mount`/`-serve-unmount`/`-serve-status`, and the
`canton-*` equivalents (see `make list`). These targets are macOS-only
and untested by `check`.

## Branches and decisions

No strict branch-naming convention is enforced; existing branches are
topic-based slugs (some agent-generated with a random suffix). Name yours
for what it does.

Architecturally significant decisions are recorded as ADRs alongside the
project they affect, e.g. `canton/docs/ADR-001-awards-reference-structure.md`.
Add a new `ADR-NNN-*.md` there (or under the relevant module's `docs/`) for
decisions worth that level of permanence.

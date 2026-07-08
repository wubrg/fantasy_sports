# ADR-0004: Push completed merge to origin/main

**Status:** Accepted
**Date:** 2026-07-08
**Deciders:** wubrg

---

## Context

Local `main` and `origin/main` diverged (1 local commit vs. 10 remote commits). A merge is in progress locally. Once conflicts are resolved and the merge commit is made, local `main` will contain both histories. Since the histories diverged, this cannot be a fast-forward push — it pushes a genuine merge commit.

## Decision

Push to `origin/main` in this same session, **conditional on verification passing first**: `go build ./...` and `go test ./...` (or the Makefile's `check` target) succeed in both `canton/app` and `league_home/app` after the merge commit.

## Consequences

- `origin/main` and local `main` converge; no more divergence.
- If verification fails, the push is held and the failure is reported back before any push is attempted — this ADR does not authorize pushing a broken build.

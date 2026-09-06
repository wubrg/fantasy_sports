# ADR-003: The 2026 week-1 forecast binds to the pack as pasted, not the pack on disk

**Status:** Accepted, with an open operator question
**Date:** 2026-09-06
**Deciders:** wubrg
**Scope:** one forecast run (2026 week 1). Not a standing policy.

---

## Context

The pack pasted to the forecaster carries `pack_sha256 = 42bca9d8…a985347` and base rates
`pass_heavy 0.2648`, `efficient_offense 0.3721`. The pack committed at
`edge/beliefs/2026/week01.input.json` carries `e4465d9e…ca14c61a1` and `0.3353` / `0.3243`.

Everything else is identical: the same 16 games, the same kickoffs, the same totals and spreads,
the same absent FORM, the same `shootout` and `blowout_loss` base rates. The pasted pack cannot be
reconstructed by editing the committed one (the edited file hashes to `e47d061d…`), so its bytes
are not recoverable from inside this repository.

`runIngest` refuses a file whose `input_pack_sha256` differs from the sha of the `-pack` file on
disk (`beliefs_result.go:580`). Only one of the two shas can be written.

## Decision

**Echo `42bca9d8…`** — the sha of the pack actually shown.

## Consequences

- The file does not ingest against the currently committed pack. That is a *visible* failure with a
  loud error message, which is the failure mode to prefer.
- Unblocking it is one command: regenerate the week-1 pack (`make belief-pack SEASON=2026 WEEK=1`)
  and confirm it hashes to `42bca9d8…`. If it does not, the pasted pack came from a cache snapshot
  this repository no longer has, and the operator must choose option (b) or (c) in the plan's §6.
- Where the two packs disagree — the two base rates — the **pasted** values are used, because they
  are the ones the forecaster was shown. This affects only abstained rows, where the contract says
  to write the base rate as a neutral placeholder: 11 rows in this file.

## Alternatives rejected

- **Echo `e4465d9e…`.** Ingests today. Rejected as the default because the sha field exists
  precisely to prove which facts a forecast saw; writing a sha for a pack that was not shown
  defeats the one property ([ADR-002](./ADR-002-belief-log-tracked-in-git.md)) the whole probe
  rests on. Cheap to switch to if the operator wants it — one field, plus the 11 abstained
  placeholders.
- **Silently pick whichever ingests.** Rejected: it hides a real inconsistency in the pack pipeline,
  and the discrepancy in `pass_heavy` (0.2648 vs 0.3353) is large enough to matter to whoever
  reads the score later.

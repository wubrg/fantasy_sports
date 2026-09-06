# ADR-005: Claim types and abstention policy for the 2026 week-1 forecast

**Status:** Accepted
**Date:** 2026-09-06
**Deciders:** wubrg
**Scope:** one forecast run (2026 week 1).

---

## Context

The falsifier adjudicates `form`, `market` and `schedule` claims against the pack, defers `usage`
and `injury` (the pack carries neither), and cannot check `personnel` or `narrative` at all. A
prediction resting only on the last two is reported **unauditable**.

The forecaster's actual information for this run is prior-season scheme and personnel identity. It
has no access to September 2026 injury reports, practice participation, depth charts or preseason
usage.

## Decision

1. **No `form`, `market` or `schedule` claims are stated at all.** The pack forbids restating its
   own numbers, and those three types are the only way a claim can void its prediction. There is
   nothing to gain and a prediction to lose.
2. **No `injury` or `usage` claims are stated either.** Those types would lower the unauditable
   count, but this forecaster holds no verified week-1 2026 injury or usage fact. A typed claim it
   cannot support is an invented fact wearing a checkable costume — the exact failure
   (`"Surtain shadows on over 80% of routes"`) the prompt was rewritten to stop. The deferred
   checker would pass it today and convict it in a season's time, which is worse than never
   writing it.
3. **Claims are `narrative` and `personnel` only**, and only on rows where a deviation from the
   market-derived baseline is actually being made. Rows that sit on the baseline carry no claims,
   which keeps `OnlyUnverifiable` from counting rows that assert nothing.
4. **Abstain where the number would be a bare restatement of the line with nothing added.** In
   practice: `pass_heavy` and `efficient_offense` for teams whose 2026 play-caller or starting
   quarterback this forecaster cannot vouch for. Abstained rows carry the pack's base rate and a
   low confidence, per the contract.
5. **Confidence is set from the stability of the underlying trait**, not from how much the number
   deviates: highest on scheme-identity `pass_heavy` reads (~0.6–0.7), middling on
   `efficient_offense` (~0.45–0.6), low on `shootout` and `blowout_loss` (~0.35–0.5) where the
   market is already the best available estimate, lowest (0.15) on abstentions.

## Consequences

- The ingest tally will read approximately: **0 checked, 0 deferred, most rows unauditable.** That
  is a true statement about this forecaster's evidence, and it is the number that should be
  compared against a future run that does have injury data in the pack.
- The falsifier cannot reject any row of this file. That is not the falsifier failing — it is a
  file that made no claim of a type the pack can rule on.
- If the probe wants auditable claims from an LLM forecaster, the pack has to carry injury
  `report_status` and weekly usage, exactly as `falsify.go` already says. Until then this policy is
  the honest one.

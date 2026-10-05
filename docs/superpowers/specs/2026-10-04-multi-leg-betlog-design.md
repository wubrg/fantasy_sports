# Structured multi-leg bets: placement, settlement, and display

Status: approved by user 2026-10-04, pending implementation plan.

## Problem

Every parlay, SGP, and round robin logged through `edgectl` tonight used a
single free-text `-selection` string describing all the legs together. This
loses information the system already has a place to put:

- `betlog.Bet` already has a `Legs []Leg` field, and `betlog.Settle` already
  accepts a `legResults []Leg` parameter — built for the "a leg voided,
  the book repriced the rest" case — but **nothing populates either one at
  placement time**, and the HTTP settle endpoint doesn't expose
  `legResults` to callers at all yet.
- Round robins have no representation as a *round robin* at all. A round
  robin on 8 players by 3s/4s gets pre-expanded into 126 separate `bet
  place` calls, one per combo, each an independent betlog row with no link
  back to the other 125. Settling means clicking won/lost/push/void 126
  times by hand, and the log tab lists all 126 rows flat.

The fix is not a bulk-settle button over N identical-looking rows — a round
robin's combos don't share an outcome (a 3-leg round robin on 3 players has
exactly one winning combo if 2 of 3 legs hit, not 0 or 3 of them). The real
fix is settling each **leg** once and deriving every combo's result from
that, which `edge/app/internal/wager/parlay.go`'s existing `RoundRobin` /
`combineIdx` functions already do for *pricing* — this reuses the same math
for *settlement*.

## Scope

In scope: a new way to place and settle round robins, parlays, and SGPs as
one record with structured legs, in both the CLI and the GUI (board-serve
log tab), plus the log tab display.

Out of scope (confirmed with the user during brainstorming):
- Auto-recomputing a book's own repriced SGP price when a leg voids. That's
  the book's proprietary correlated-SGP pricing, which this project cannot
  reproduce — stays a manual "type the number the book actually paid" step,
  exactly as the existing repriced-settle flow already works.
- Any live score/stats feed. Leg results are entered by hand, same as every
  other settle action in this app today. (Sleeper has no stats endpoint —
  confirmed by reading `edge/model/ingest/sleeper.py` — and the nflverse
  stats CSV lags by days; neither is a same-day data source.)
- Migrating already-logged pre-exploded round-robin rows (e.g. tonight's
  56/70-combo entries) into the new shape. They stay as-is; only new
  placements use the new path.

## Data model

One additive field on `betlog.Bet`, alongside the existing `Legs []Leg`:

```go
type Bet struct {
    // ... existing fields unchanged ...
    Legs []Leg `json:"legs,omitempty"` // already exists

    // RRSizes is the combo sizes for a round robin (e.g. [3] or [2,3]).
    // Nil means "not a round robin": if Legs is also nil this is an
    // ordinary single-leg bet; if Legs is populated this is a straight
    // parlay/SGP of all legs together (one combo, size == len(Legs)).
    RRSizes []int `json:"rr_sizes,omitempty"`

    // StakePerCombo is the stake on EACH combo a round robin produces.
    // Only meaningful when RRSizes is set; Stake continues to mean the
    // total amount risked (StakePerCombo × combo count) so every existing
    // at-risk/realized figure that sums Stake keeps working unmodified.
    StakePerCombo float64 `json:"stake_per_combo,omitempty"`
}
```

This covers all three shapes with one schema, matching how `RoundRobin`
already treats a straight parlay as the single-size-equal-to-leg-count case:

| shape | `Legs` | `RRSizes` |
|---|---|---|
| single bet (today's default) | nil | nil |
| parlay / SGP | populated | nil |
| round robin | populated | `[2]`, `[3]`, `[2,3]`, ... |

No new `Kind`, no new file, no migration. `betlog.Load`, `Score`,
`BySource`, and every period-report consumer that doesn't know about
`RRSizes` keeps working exactly as today, because `Stake`/`Payout`/`Result`
on the top-level `Entry` remain the source of truth for aggregate figures —
`RRSizes`/`Legs` are additive detail, not a replacement for those fields.

## Placement

**CLI** — a new subcommand, `edgectl bet place-rr`:

```
edgectl bet place-rr -legs "<price>:<game>:<label>,..." -rr "2,3" \
  -total <amount> -book <book> -bankroll cash|bonus|no-sweat \
  -week <n> [-predicted <p>] [-narrative <text>]
```

Mirrors `edgectl parlay`'s `-legs`/`-rr`/`-total` flags exactly (same
cross-game-only enforcement, same error on two legs sharing a game) so a
user who already priced a round robin with `edgectl parlay` can place it
with the identical flags. `-total` is split evenly across the combo count
(`wager.RoundRobin` already reports the combo count), written as
`StakePerCombo`. One call, one record, no change to `edgectl parlay` itself
(it stays a pure pricing calculator, no betlog write).

A plain parlay/SGP (one combo) stays reachable through the existing
`edgectl bet place -legs "sel:price,sel:price"` flag — already wired
end-to-end from CLI to `betlog.Bet.Legs` via `toBetlogLegs` in
`board_log_api.go`. No change needed there beyond actually using it, which
is a documentation/habit fix, not a code fix.

**GUI** — the log tab's bet-entry form gains a leg-row UI mirroring the
bets tab's existing parlay-builder (`legRow()` / `#c-leg-add` in `app.js`):
"add leg" button, each row taking selection/price/game-tag, plus an
optional round-robin-size input (text field, comma-separated, e.g. "2,3")
that's empty for a plain parlay. Submits to `/api/place` with the existing
`Legs` field plus two new optional JSON fields, `rr_sizes` and
`stake_per_combo` (computed client-side from `-total`-equivalent ÷ combo
count, same split `place-rr` does). `placeReq` and `handlePlace` in
`board_log_api.go` gain the two fields and pass them through to
`betlog.Bet` unchanged otherwise.

## Settlement

**Per-leg marking, not per-combo.** The settle request gains one field:

```go
type settleReq struct {
    // ... existing fields unchanged ...
    LegResults []struct {
        Selection string `json:"selection"`
        Result    string `json:"result"` // won, lost, push, void
    } `json:"leg_results,omitempty"`
}
```

`handleSettle` already calls `journal.Settle(..., legResults, ...)` — today
it hardcodes `nil` for that parameter. The only change is threading
`req.LegResults` through instead, converting to `[]betlog.Leg` the same way
`handlePlace` already converts `req.Legs` via `toBetlogLegs`.

**Computing the outcome from legs**, by bet shape:

- *Single bet* (`Legs` nil): unchanged, today's behavior exactly.
- *Parlay/SGP* (`Legs` set, `RRSizes` nil):
  - every leg won → bet **won** at the stored `Price` (already known,
    nothing to recompute).
  - any leg lost → bet **lost**.
  - no loser, but a push/void present among the legs → falls back to
    today's existing "repriced" flow: prompt for the price the book
    actually paid. This is the one case explicitly left manual (see
    Scope) — a correlated SGP's reduced price is the book's own
    calculation, not ours to guess at.
- *Round robin* (`Legs` set, `RRSizes` set): call
  `wager.RoundRobin(legPrices, RRSizes)` to regenerate every combo (never
  stored — derived fresh from `Legs` + `RRSizes` every time, so there is
  nothing to keep in sync). For each combo (a set of leg indices):
  - any leg in the combo lost → combo **lost**, stake forfeited.
  - every leg in the combo won → combo **won**, payout from the combo's
    already-known stored price (computed at placement, unchanged).
  - a push/void leg in the combo, no loser → **this case is safe to
    auto-compute**, unlike the SGP case above: round-robin legs are
    cross-game/independent by the hard rule `edgectl parlay` already
    enforces (it refuses same-game legs outright), so dropping a voided
    leg and recombining the remainder is pure arithmetic over
    already-known per-leg prices via the same `combineIdx` function
    `RoundRobin` already uses for pricing — not a guess at proprietary
    book math. Confirmed with the user as the one case where
    auto-recompute is in scope, specifically because it's mechanical, not
    correlated.
  - Total settlement: sum each combo's realized profit/loss across
    `StakePerCombo`, write one `Entry` with the aggregate `Result` (won if
    net positive relative to stake risked is not the rule — see below),
    `SettledPrice` left nil (not meaningful for a multi-combo wager), and
    the per-combo breakdown recorded in a new, purely informational
    narrative/log line (not a new stored field — nothing downstream reads
    per-combo detail, only the aggregate).
  - **Result semantics**: all combos win → `Won`; all combos lose → `Lost`
    (both reuse the existing enum unchanged — the common case for a short
    round robin isn't actually mixed). Only when combos split does the
    existing `Result` enum fall short; add `Mixed` as a new `betlog.Result`
    value for that case, counted in `Score`/calibration by realized P&L
    rather than a binary hit. Existing single-leg and all-or-nothing
    parlay bets are unaffected — they never produce `Mixed`.

## Display (log tab)

A multi-leg `Entry` (any `Legs` present) renders as one card instead of a
flat row:

- Header: selection summary, combined price (or "round robin, N combos" for
  an `RRSizes` bet), total stake, total payout/realized.
- Body: one line per leg, each with its own won/lost/push/void buttons
  (same button set the per-bet settle row already uses, just scoped to a
  leg instead of the whole bet).
- A single "settle" action enabled once every leg has a result, computing
  and submitting the aggregate per the rules above. Before every leg is
  resolved, the card shows as open with no settle action available — never
  a partial/guessed settlement.
- For a round robin specifically, a collapsed-by-default "N combos" detail
  showing each combo's computed result, openable on tap — informational
  only, not separately interactive.

Old pre-exploded round-robin rows (tonight's 56/70-row entries) keep
rendering exactly as today, one flat row each — they have no `RRSizes` and
nothing about them changes.

## Testing

- `wager` package: new tests confirming the void/push combo-recompute
  arithmetic (a combo with a voided leg reprices to the remaining legs'
  `combineIdx` price) against hand-computed examples.
- `betlog` package: round-trip tests for `RRSizes`/`StakePerCombo` through
  `Append`/`Load`, and a `Mixed`-result settle test.
- `journal` package: a round-robin settle integration test — place with
  `place-rr`-equivalent legs, settle a mix of won/lost legs, assert the
  realized total matches a hand-computed sum.
- CLI: a smoke test for `edgectl bet place-rr` matching the existing
  `edgectl parlay` flag-parsing tests for `-legs`/`-rr`.
- GUI: manual test per the existing convention in this codebase (no JS test
  suite) — place a round robin through the form, settle it leg by leg,
  confirm the combo summary matches `edgectl parlay`'s own pricing for the
  same legs.

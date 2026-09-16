# CLI / GUI consolidation — design

Date: 2026-09-13
Status: approved design, pending implementation plan

## Problem

The `edgectl` CLI and the `board serve` GUI are meant to share one core, with the
GUI preserving and presenting state. In practice they have drifted:

- **Placing a bet exists in two forms.** The GUI's place handler
  (`app/cmd/edgectl/board_log_api.go`) debits the ledger *and* writes the betlog
  as one operation. The CLI's `ledger add -kind place` writes only the ledger.
  A session of bets logged via the CLI this way was invisible in the GUI's log
  tab, because the betlog was never written. The orchestration that ties the two
  logs together lives only in the GUI, so the CLI cannot reproduce it.
- **Per-book money management is heavier than the workflow needs.** There is one
  real bank account and each book is zeroed out weekly, so per-book cash/bonus
  balance tracking is not worth presenting. `board_funds_api.go` (638 lines) is
  almost entirely that surface.
- **The multi-book board is not useful.** Prices are pulled from DraftKings
  because it is the easiest source, and DK is treated as consensus for computing
  edge/EV. The per-book columns, tabs, and dropdowns add friction without value.

What must be preserved: which **book** a bet or boost is placed at (location, not
balance), and the board-vs-consensus comparison capability (dormant, see below).

## Goals

1. One shared code path for placing and settling a bet, used by both the CLI and
   the GUI, so the two logs can never disagree the way they did this session.
2. Remove the per-book money-management surface from the GUI while keeping the
   ledger's accounting intact underneath.
3. Collapse the board to DK-only pricing (DK = consensus), as the operating mode.
4. Keep tracking where a boost or bet is made, and let a boost be applied when a
   bet is placed.

## Non-goals (YAGNI)

- **No change to the money data model.** The ledger keeps replaying per-book
  cash/bonus/boost lots exactly as today; only its GUI presentation changes.
- **No removal of the board-vs-consensus capability.** The multi-book schema and
  the comparison code in `board report` stay. They are dormant because manual
  multi-book line pulls are not sustainable, not because they are unwanted.
- **No glanceable-state redesign** of the GUI in this pass.
- **No automated consensus/odds pull.** (`internal/oddspull` hints at a future
  unlock here; explicitly out of scope.)

## Design

### 1. Core: `internal/journal`

A new package that sits above `betlog` and `ledger` (imports both; neither
imports it — clean one-directional layering). It owns the single definition of a
bet's lifecycle.

```
journal.Place(betlogPath, ledgerPath string, req PlaceRequest, now time.Time) (id string, err error)
journal.Settle(betlogPath, ledgerPath string, id string, result betlog.Result, returns *ledger.Lot, note string, now time.Time) error
```

`PlaceRequest` carries the `betlog.Bet` (selection, price, stake, bankroll, week,
narrative), the `book` (location), and an optional `BoostLotID`.

`Place` executes in the current safe order so a failure never leaves the two logs
disagreeing:

1. Prepare and validate the ledger debit for the stake (draws the book's
   `cash`/`bonus` lot per the bet's bankroll) — the existing `s.debit` logic,
   lifted into the core.
2. Write the betlog entry; obtain the wager id.
3. Append the tied ledger draw events (`Wager=id`, `Week`).
4. If `BoostLotID` is set, append a ledger `expire` on that boost lot, noted with
   the wager id. This is the "pick-on-place marks it used" behavior. **This is new
   behavior** — today's GUI place handler does not touch boosts at all.

`Settle` writes the betlog settle and the ledger settle (with any returns)
together, for the same reason.

Existing files from prior sessions (`~/fanatics-bonus.jsonl`, `~/bankroll.jsonl`)
stay valid — the core reads and writes the same formats. No data migration.

### 2. CLI and GUI as thin clients

Both surfaces stop owning place/settle logic and call `journal`.

**CLI — new `edgectl bet` command:**

```
edgectl bet place  -selection <text> -price <american> -stake <n> -book <name>
                   -bankroll <cash|bonus> -week <n> [-boost <lotid>] [-narrative <text>]
edgectl bet settle -id <wager> -result <won|lost|push|void> [-returns <n> -returns-asset cash]
```

`bet place` / `bet settle` are the blessed path — one call writes both logs via
`journal`. Raw `ledger add -kind place` remains as a documented low-level escape
hatch; its help gains a line noting it writes the ledger only and that `bet place`
is the way to also record the betlog.

**GUI — handlers shrink.** The `board_log_api.go` place and settle handlers become
*parse request → call `journal` → return JSON*. The `s.debit` helper moves into
the core. After this and section 4, there is exactly one code path that can place
or settle a bet, and the CLI can do everything the GUI can.

### 3. DK-only board pricing

DK is the board and the board is consensus.

- **GUI:** one price column. Enter DK `ml / spread / total` per game; no book tabs,
  no book dropdown, no per-book cells. Price entry scopes to week + market only.
- **Reports/edge/EV:** `board report` drops its required `-book` picker and reads
  DK. Everything computing edge/EV/de-vig uses DK as the reference. `board import`
  defaults to DK.
- **Schema stays multi-book; UI goes single-book.** The week YAML keeps its
  per-book map, so existing `consensus`/`fanatics` prices are not destroyed; the
  simplified UI and reports read/write the `draftkings` key and ignore the rest.
- **One-time backfill:** a `board import`-style helper copies existing
  `consensus` prices into `draftkings` so current boards are not empty on the
  switch.

Consequences on the record:

- The **"best price vs consensus"** output of `board report` has no meaning with a
  single book and is not surfaced in the DK-only path. De-vigging DK's two-sided
  markets and ranking dogs still works. The comparison code is retained (non-goal:
  removal) for when consensus data exists again.

### 4. Drop the funds UI, add the lean boosts list

`board_funds_api.go` collapses from full per-book money management to a small
boosts API.

- **Remove** the per-book cash/bonus balance screens from the GUI. The ledger
  still tracks all of it underneath; `edgectl ledger balances` remains for the
  full accounting on demand.
- **Add a "Boosts on hand" panel** — reads the live (unexpired) boost lots from
  the ledger and shows **book · terms (%/min-odds/max/market) · expiry**. Add a
  boost (ledger `grant`) and retire a boost (ledger `expire`) from the panel.
- **Pick-on-place** — the bet-entry form shows a boost dropdown filtered to the
  selected book's live boosts; choosing one passes the lot id to `journal.Place`,
  which marks it used.

Both "track where it's made" needs are covered: **book** is on every bet (log tab)
and on every boost (boosts panel).

## Testing

- **`internal/journal`:** unit tests for `Place` and `Settle` — both logs written,
  correct order, boost marked used when supplied, failure partway leaves a
  recoverable state (matching the current handler's guarantees). This is the layer
  that most needs isolated tests and did not exist before.
- **CLI:** `bet place` / `bet settle` write both files; `ledger add -kind place`
  still writes ledger-only (escape hatch unchanged).
- **GUI handlers:** existing `board_log_api_test.go` / `board_place_test.go` adapt
  to assert they now delegate to `journal` and still produce the same on-disk
  result.
- **Board:** `board report` works with DK only and no `-book`; the
  consensus→draftkings import helper is covered.
- **Regression:** placing the same bet through the CLI and through the GUI yields
  byte-equivalent log entries (the anti-drift guarantee).

## Migration

- One-time `consensus` → `draftkings` copy for existing week boards.
- No change required to `~/fanatics-bonus.jsonl` or `~/bankroll.jsonl`; today's
  manually-split entries remain valid.

## Open consequences

- Board-vs-consensus is dormant until a consensus source (manual or automated via
  `oddspull`) is reliably available. Its removal is explicitly a non-goal.

# Shared Place/Settle Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the CLI and the board GUI one shared core for placing and settling a bet, so the betlog and the ledger can never drift apart the way they did when bets logged via `ledger add` were invisible in the GUI.

**Architecture:** A new `internal/journal` package sits above `internal/betlog` and `internal/ledger` (imports both; neither imports it). It owns `Place` and `Settle`, each of which writes the betlog entry AND the tied ledger events as one operation. The GUI handlers and a new `edgectl bet` command both become thin callers of `journal`.

**Tech Stack:** Go (module `edge`), standard library `net/http` for the GUI, `flag` for the CLI. Tests are standard `go test` with temp-file fixtures.

**Spec:** `edge/docs/superpowers/specs/2026-09-13-cli-gui-consolidation-design.md`

## Global Constraints

- Go module is `edge`; all commands run from `edge/app`. Test with `go test ./...`. Build with `go build -o edgectl ./cmd/edgectl`.
- The binary `edge/app/edgectl` is gitignored; rebuild it to exercise the CLI manually.
- Default paths: ledger `~/bankroll.jsonl`, betlog `~/fanatics-bonus.jsonl`. Never hardcode these in `journal` — always take them as parameters.
- Both logs are append-only JSONL. Never rewrite a line; settling appends.
- `betlog.Result` and `ledger.Result` are distinct Go types with identical string values (`"won"`, `"lost"`, `"push"`, `"void"`). Convert by string.
- Preserve the existing safe ordering: validate/prepare the ledger debit BEFORE writing the betlog, so a failed debit leaves no orphan betlog entry.
- Commit messages end with the two trailers from the environment (Co-Authored-By + Claude-Session).

## Out of scope (do not touch in this plan)

- `scenario.go` and `log.go` call `betlog.PlaceBet` / `betlog.Settle` directly to record and settle *predictions* (no money). They are calibration flows, not wagers, and stay as they are.
- `board_funds_api.go`'s `withdrawFrom` and the funds-adjust endpoint (Plan 3).
- DK-only board pricing (Plan 2).

---

### Task 1: `journal.Place`

**Files:**
- Create: `app/internal/journal/journal.go`
- Test: `app/internal/journal/journal_test.go`

**Interfaces:**
- Consumes: `betlog.Bet`, `betlog.PlaceBet(path, Bet) (string, error)`; `ledger.Load`, `ledger.Balances`, `ledger.AppendFile`, `ledger.NewID`, `ledger.Event`, `ledger.Lot`, `ledger.KindPlace`, `ledger.KindExpire`, `ledger.Cash`, `ledger.Bonus`.
- Produces:
  - `type PlaceRequest struct { Bet betlog.Bet; Book string; BoostLotID string }`
  - `func Place(betlogPath, ledgerPath string, req PlaceRequest, now time.Time) (id string, err error)`
  - `func AssetForBankroll(bankroll string) string`

- [ ] **Step 1: Write the failing test**

```go
package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
	"edge/internal/wager"
)

func tmp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func grant(t *testing.T, path, id, book, asset string, amount float64) {
	t.Helper()
	ev := ledger.Event{
		Kind: ledger.KindGrant, ID: id, Time: time.Now(),
		Creates: &ledger.Lot{ID: id, Book: book, Asset: asset, Amount: amount},
	}
	if err := ledger.AppendFile(path, ev); err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func TestPlace_writesBetlogAndDebitsLedger(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 10)

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "bonus bet", Stake: 4, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if id == "" {
		t.Fatal("expected a wager id")
	}

	// Betlog has the bet.
	bets, err := betlog.Load(bl)
	if err != nil {
		t.Fatalf("betlog.Load: %v", err)
	}
	if len(bets) != 1 || bets[0].Bet.Selection != "Barkley ATD" {
		t.Fatalf("betlog missing the bet: %+v", bets)
	}
	if bets[0].ID != id {
		t.Fatalf("betlog id %q != returned id %q", bets[0].ID, id)
	}

	// Ledger has a place tied to the wager.
	evs, err := ledger.Load(lg)
	if err != nil {
		t.Fatalf("ledger.Load: %v", err)
	}
	var placed float64
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Wager == id {
			placed += e.Amount
		}
	}
	if placed != 4 {
		t.Fatalf("expected 4 placed against %s, got %v", id, placed)
	}
}

func TestPlace_appliesBoost(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-cash", "fanatics", ledger.Cash, 50)
	// A boost lot to be consumed.
	boost := ledger.Event{
		Kind: ledger.KindGrant, ID: "fan-boost", Time: time.Now(),
		Creates: &ledger.Lot{ID: "fan-boost", Book: "fanatics", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.3, MaxStake: 50, MinOdds: -200, RequiresCashStake: true}},
	}
	if err := ledger.AppendFile(lg, boost); err != nil {
		t.Fatalf("grant boost: %v", err)
	}

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:        betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "real money", Stake: 50, Week: 1},
		Book:       "fanatics",
		BoostLotID: "fan-boost",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	evs, _ := ledger.Load(lg)
	sawExpire := false
	for _, e := range evs {
		if e.Kind == ledger.KindExpire && e.Lot == "fan-boost" && e.Wager == id {
			sawExpire = true
		}
	}
	if !sawExpire {
		t.Fatal("boost lot was not expired against the wager")
	}
}

func TestPlace_insufficientBalanceWritesNothing(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 1)

	_, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "too big", Price: wager.American(-110), Bankroll: "bonus bet", Stake: 5, Week: 1},
		Book: "draftkings",
	}, time.Now())
	if err == nil {
		t.Fatal("expected an insufficient-balance error")
	}
	if _, statErr := os.Stat(bl); !os.IsNotExist(statErr) {
		if b, _ := os.ReadFile(bl); strings.TrimSpace(string(b)) != "" {
			t.Fatalf("betlog should be empty after a failed debit, got: %s", b)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd app && go test ./internal/journal/ -run TestPlace -v`
Expected: compile failure — `undefined: Place`, `undefined: PlaceRequest`.

- [ ] **Step 3: Write the implementation**

```go
// Package journal is the one place a wager is placed or settled. It writes the
// betlog (the prediction/log the GUI shows) and the ledger (the bankroll) as a
// single operation, so the two can never disagree. It sits above betlog and
// ledger; neither imports it.
package journal

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
)

// PlaceRequest is a wager to record. Bet is the frozen snapshot written to the
// betlog; Book is where it was placed and drives the ledger debit; BoostLotID,
// when set, names a boost lot to consume against this wager.
type PlaceRequest struct {
	Bet        betlog.Bet
	Book       string
	BoostLotID string
}

// AssetForBankroll maps a betlog bankroll to the ledger asset a place draws
// from: a bonus bet spends bonus, everything else spends cash.
func AssetForBankroll(bankroll string) string {
	if strings.EqualFold(strings.TrimSpace(bankroll), "bonus bet") {
		return ledger.Bonus
	}
	return ledger.Cash
}

// Place records a wager. It prepares and validates the ledger debit first, then
// writes the betlog, then ties the ledger draws (and any boost consumption) to
// the new wager id. The order matters: a failed debit leaves no betlog entry.
func Place(betlogPath, ledgerPath string, req PlaceRequest, now time.Time) (string, error) {
	if req.Bet.Stake <= 0 {
		return "", fmt.Errorf("journal: stake must be positive")
	}

	draws, err := draw(ledgerPath, req.Book, AssetForBankroll(req.Bet.Bankroll), req.Bet.Stake, now)
	if err != nil {
		return "", err
	}

	id, err := betlog.PlaceBet(betlogPath, req.Bet)
	if err != nil {
		return "", err
	}

	for _, ev := range draws {
		ev.Wager = id
		ev.Week = req.Bet.Week
		if err := ledger.AppendFile(ledgerPath, ev); err != nil {
			return "", fmt.Errorf("wager %s was recorded but the bankroll could not be debited: %w", id, err)
		}
	}

	if strings.TrimSpace(req.BoostLotID) != "" {
		exp := ledger.Event{
			Kind: ledger.KindExpire, ID: ledger.NewID(now, req.Book+"-boost-used"), Time: now,
			Lot: req.BoostLotID, Wager: id, Week: req.Bet.Week,
			Note: "boost applied to " + id,
		}
		if err := ledger.AppendFile(ledgerPath, exp); err != nil {
			return "", fmt.Errorf("wager %s was recorded but the boost could not be consumed: %w", id, err)
		}
	}

	return id, nil
}

// draw builds the ledger place events for a stake, oldest-deadline lot first.
// It returns nil (no error) when no book is named or no ledger exists: the
// bankroll is opt-in and a bet can be recorded without one. An insufficient
// balance IS an error — it means money is believed held that is not.
func draw(ledgerPath, book, asset string, stake float64, now time.Time) ([]ledger.Event, error) {
	if strings.TrimSpace(book) == "" {
		return nil, nil
	}
	if _, err := os.Stat(ledgerPath); os.IsNotExist(err) {
		return nil, nil
	}
	events, err := ledger.Load(ledgerPath)
	if err != nil {
		return nil, err
	}
	pos, err := ledger.Balances(events, now)
	if err != nil {
		return nil, err
	}

	var open []ledger.Lot
	for _, l := range pos.Lots {
		if l.Book == book && l.Asset == asset && !l.Unit() && l.Amount > 0 {
			open = append(open, l)
		}
	}
	sort.SliceStable(open, func(i, j int) bool {
		ei, ej := open[i].Expires, open[j].Expires
		switch {
		case ei != nil && ej != nil:
			return ei.Before(*ej)
		case ei != nil:
			return true
		case ej != nil:
			return false
		}
		return open[i].ID < open[j].ID
	})

	var total float64
	for _, l := range open {
		total += l.Amount
	}
	if total+1e-9 < stake {
		return nil, fmt.Errorf("%s holds %.2f of %s, which will not cover a %.2f stake", book, total, asset, stake)
	}

	var out []ledger.Event
	left := stake
	for _, l := range open {
		if left <= 1e-9 {
			break
		}
		take := l.Amount
		if take > left {
			take = left
		}
		out = append(out, ledger.Event{
			Kind: ledger.KindPlace, ID: ledger.NewID(now, book+"-place"), Time: now,
			Lot: l.ID, Amount: take,
		})
		left -= take
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd app && go test ./internal/journal/ -run TestPlace -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add app/internal/journal/journal.go app/internal/journal/journal_test.go
git commit -m "edge: journal.Place — one core that writes betlog + ledger together"
```

---

### Task 2: `journal.Settle`

**Files:**
- Modify: `app/internal/journal/journal.go`
- Test: `app/internal/journal/journal_test.go` (add cases)

**Interfaces:**
- Consumes: `betlog.Load`, `betlog.Settle(path, id, Result, note)`, `betlog.Open`, `betlog.Result`; `ledger.Load`, `ledger.AppendFile`, `ledger.NewID`, `ledger.KindPlace`, `ledger.KindSettle`, `ledger.Result`, `ledger.Lot`, `Place` (Task 1).
- Produces: `func Settle(betlogPath, ledgerPath string, id string, result betlog.Result, returns *ledger.Lot, note string, now time.Time) error`

- [ ] **Step 1: Write the failing test**

```go
func TestSettle_writesBothAndGuardsDoubleSettle(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 10)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: -115, Bankroll: "bonus bet", Stake: 4, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	if err := Settle(bl, lg, id, betlog.Won, nil, "scored", now); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	// Betlog shows settled.
	bets, _ := betlog.Load(bl)
	if bets[0].Result != betlog.Won {
		t.Fatalf("betlog result = %q, want won", bets[0].Result)
	}
	// Ledger has a settle tied to the wager.
	evs, _ := ledger.Load(lg)
	sawSettle := false
	for _, e := range evs {
		if e.Kind == ledger.KindSettle && e.Wager == id {
			sawSettle = true
		}
	}
	if !sawSettle {
		t.Fatal("ledger settle not written")
	}

	// Second settle is refused.
	if err := Settle(bl, lg, id, betlog.Lost, nil, "oops", now); err == nil {
		t.Fatal("expected double-settle to be refused")
	}
}

func TestSettle_predictionWithoutLedgerPlace(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	// A bet recorded with no book -> no ledger place exists.
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet: betlog.Bet{Selection: "pure prediction", Price: 200, Bankroll: "bonus bet", Stake: 1, Week: 1},
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	// Settling must not error even though there is no ledger place to settle.
	if err := Settle(bl, lg, id, betlog.Lost, nil, "", now); err != nil {
		t.Fatalf("Settle prediction-only: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd app && go test ./internal/journal/ -run TestSettle -v`
Expected: compile failure — `undefined: Settle`.

- [ ] **Step 3: Add the implementation to `journal.go`**

```go
// Settle records an outcome for a wager. It appends the betlog settle and, when
// an at-risk ledger place exists for the wager, a ledger settle too. A wager
// recorded without a book has no ledger place; settling it touches only the
// betlog. Double settles are refused: the betlog folds the last outcome on top,
// so a second tap could quietly flip a result.
func Settle(betlogPath, ledgerPath string, id string, result betlog.Result, returns *ledger.Lot, note string, now time.Time) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("journal: which bet? an id is required")
	}
	bets, err := betlog.Load(betlogPath)
	if err != nil {
		return err
	}
	found := false
	for _, b := range bets {
		if b.ID != id {
			continue
		}
		found = true
		if b.Result != "" && b.Result != betlog.Open {
			return fmt.Errorf("%s is already settled as %q; settling again would append a second outcome", id, b.Result)
		}
	}
	if !found {
		return fmt.Errorf("no bet with id %s", id)
	}

	if err := betlog.Settle(betlogPath, id, result, note); err != nil {
		return err
	}

	// Only write a ledger settle if a place for this wager exists.
	if _, statErr := os.Stat(ledgerPath); statErr == nil {
		evs, err := ledger.Load(ledgerPath)
		if err != nil {
			return err
		}
		hasPlace := false
		for _, e := range evs {
			if e.Kind == ledger.KindPlace && e.Wager == id {
				hasPlace = true
				break
			}
		}
		if hasPlace {
			settle := ledger.Event{
				Kind: ledger.KindSettle, ID: ledger.NewID(now, "settle-"+id), Time: now,
				Wager: id, Result: ledger.Result(result), Returns: returns, Note: note,
			}
			if err := ledger.AppendFile(ledgerPath, settle); err != nil {
				return fmt.Errorf("betlog was settled but the ledger settle failed: %w", err)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd app && go test ./internal/journal/ -v`
Expected: PASS (all Place and Settle tests).

- [ ] **Step 5: Commit**

```bash
git add app/internal/journal/journal.go app/internal/journal/journal_test.go
git commit -m "edge: journal.Settle — settle betlog and ledger together, guard double-settle"
```

---

### Task 3: GUI handlers delegate to `journal`

**Files:**
- Modify: `app/cmd/edgectl/board_log_api.go` (`handlePlace` ~171-238, `handleSettle` ~247-296, remove `assetForBankroll` ~306-313 and `debit` ~315-329)
- Modify: `app/cmd/edgectl/board_funds_api.go` (remove `drawFrom` ~203-259; keep `withdrawFrom`)
- Test: `app/cmd/edgectl/board_place_test.go`, `board_log_api_test.go` (adapt)

**Interfaces:**
- Consumes: `journal.Place`, `journal.Settle` (Tasks 1-2).
- Produces: no new exported surface; the handlers keep their HTTP contract (`{"ok":true,"id":...}`).

- [ ] **Step 1: Rewrite `handlePlace` to call `journal.Place`**

Replace the body from the `b := betlog.Bet{...}` construction through the end of the draws loop with:

```go
	b := betlog.Bet{
		Selection: req.Selection,
		Price:     wager.American(req.Price),
		Bankroll:  req.Bankroll,
		Stake:     req.Stake,
		Predicted: req.Predicted,
		Narrative: req.Narrative,
		Week:      req.Week,
	}
	id, err := journal.Place(s.betlogPath, s.ledgerPath, journal.PlaceRequest{
		Bet: b, Book: req.Book,
	}, time.Now())
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
```

- [ ] **Step 2: Rewrite `handleSettle` to call `journal.Settle`**

Replace the double-settle scan and the `betlog.Settle` call (the block from `bets, err := betlog.Load(...)` through the final `writeJSON`) with:

```go
	if err := journal.Settle(s.betlogPath, s.ledgerPath, req.ID, betlog.Result(req.Result), nil, req.Note, time.Now()); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": req.ID, "result": req.Result})
```

- [ ] **Step 3: Delete the now-unused helpers**

Remove `assetForBankroll` and `debit` from `board_log_api.go`, and `drawFrom` from `board_funds_api.go`. Add the import `"edge/internal/journal"` to `board_log_api.go`. Remove now-unused imports (`os`, `sort`, `strings`, `time` may still be used elsewhere in each file — only remove what the compiler flags).

- [ ] **Step 4: Run to verify it fails to compile / tests fail, then passes**

Run: `cd app && go build ./... && go test ./cmd/edgectl/ -run 'Place|Settle|Log' -v`
Expected first: unused-import or undefined errors while mid-edit; after Step 3 completes: PASS. If a test asserted the old `"debited"` field in the place response, update it to assert only `ok` and `id`.

- [ ] **Step 5: Add a regression test that CLI and GUI produce the same on-disk result**

Add to `board_place_test.go`:

```go
func TestPlace_GUIandCoreAgree(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")
	// Grant identical bankrolls in two ledgers, place the same bet through
	// journal.Place directly and through the HTTP handler, and compare the
	// betlog bet fields.
	// (Use the existing test server helper in this file to POST /api/place.)
	_ = bl
	_ = lg
	t.Skip("fill in using this file's server harness; asserts selection/price/stake/id-shape match journal.Place")
}
```

Then replace the `t.Skip` with a real assertion using the harness already present in `board_serve_test.go` / `board_place_test.go` (POST to `/api/place`, load the betlog, compare `Selection`, `Price`, `Stake` to a direct `journal.Place` call on a second pair of temp files).

- [ ] **Step 6: Run and commit**

Run: `cd app && go test ./cmd/edgectl/ -v`
Expected: PASS.

```bash
git add app/cmd/edgectl/board_log_api.go app/cmd/edgectl/board_funds_api.go app/cmd/edgectl/board_place_test.go app/cmd/edgectl/board_log_api_test.go
git commit -m "edge: GUI place/settle delegate to journal; settle now clears at-risk in the ledger"
```

---

### Task 4: `edgectl bet place|settle` CLI command

**Files:**
- Create: `app/cmd/edgectl/bet.go`
- Modify: `app/cmd/edgectl/main.go` (add dispatch case ~55)
- Modify: `app/cmd/edgectl/ledger.go` (add one help line to `-kind` on the `place` path)
- Test: `app/cmd/edgectl/bet_test.go`

**Interfaces:**
- Consumes: `journal.Place`, `journal.Settle`; `betlog.Bet`, `betlog.Result`; `wager.American`; `ledger.Lot`, `ledger.Cash`.
- Produces: `func betCmd(args []string) error`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"path/filepath"
	"testing"

	"edge/internal/betlog"
	"edge/internal/ledger"
	"edge/internal/journal"
	"time"
)

func TestBetPlace_writesBothLogs(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")
	// Seed a bonus lot.
	_ = ledger.AppendFile(lg, ledger.Event{
		Kind: ledger.KindGrant, ID: "dk-bonus", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-bonus", Book: "draftkings", Asset: ledger.Bonus, Amount: 10},
	})

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Barkley ATD", "-price", "-115", "-stake", "4",
		"-book", "draftkings", "-bankroll", "bonus", "-week", "1",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	if len(bets) != 1 {
		t.Fatalf("expected 1 betlog entry, got %d", len(bets))
	}
	evs, _ := ledger.Load(lg)
	placed := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Wager == bets[0].ID {
			placed = true
		}
	}
	if !placed {
		t.Fatal("no ledger place tied to the wager")
	}
	_ = journal.PlaceRequest{} // keep the import honest if unused above
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd app && go test ./cmd/edgectl/ -run TestBetPlace -v`
Expected: compile failure — `undefined: betCmd`.

- [ ] **Step 3: Implement `bet.go`**

```go
package main

import (
	"flag"
	"fmt"
	"time"

	"edge/internal/betlog"
	"edge/internal/journal"
	"edge/internal/ledger"
	"edge/internal/wager"
)

// betCmd is the blessed way to place or settle a wager: one call writes the
// betlog and the ledger together via internal/journal. Raw `ledger add -kind
// place` remains as a low-level escape hatch that writes only the ledger.
func betCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("edgectl bet: want a mode: place or settle")
	}
	switch args[0] {
	case "place":
		return betPlace(args[1:])
	case "settle":
		return betSettle(args[1:])
	default:
		return fmt.Errorf("edgectl bet: unknown mode %q (want place or settle)", args[0])
	}
}

func betPlace(args []string) error {
	fs := flag.NewFlagSet("bet place", flag.ContinueOnError)
	betlogPath := fs.String("betlog", betlog.DefaultPath(), "path to the betlog")
	ledgerPath := fs.String("ledger", ledger.DefaultPath(), "path to the bankroll")
	selection := fs.String("selection", "", "what the wager is (required)")
	price := fs.Int("price", 0, "American price of the wager (required)")
	stake := fs.Float64("stake", 0, "stake (required)")
	book := fs.String("book", "", "sportsbook the bet is placed at")
	bankroll := fs.String("bankroll", "bonus", "bankroll: cash or bonus")
	boost := fs.String("boost", "", "boost lot id to apply and consume")
	week := fs.Int("week", 0, "the NFL week this wager is FOR")
	narrative := fs.String("narrative", "", "free-text note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bank := "bonus bet"
	if *bankroll == "cash" {
		bank = "real money"
	}
	id, err := journal.Place(*betlogPath, *ledgerPath, journal.PlaceRequest{
		Bet: betlog.Bet{
			Selection: *selection, Price: wager.American(*price), Bankroll: bank,
			Stake: *stake, Week: *week, Narrative: *narrative,
		},
		Book: *book, BoostLotID: *boost,
	}, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("placed %s\n", id)
	return nil
}

func betSettle(args []string) error {
	fs := flag.NewFlagSet("bet settle", flag.ContinueOnError)
	betlogPath := fs.String("betlog", betlog.DefaultPath(), "path to the betlog")
	ledgerPath := fs.String("ledger", ledger.DefaultPath(), "path to the bankroll")
	id := fs.String("id", "", "wager id to settle (required)")
	result := fs.String("result", "", "won, lost, push or void (required)")
	returns := fs.Float64("returns", 0, "amount handed back by the book")
	returnsAsset := fs.String("returns-asset", "cash", "asset the returns arrive as")
	book := fs.String("book", "", "book the returns land at (required with -returns)")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ret *ledger.Lot
	if *returns > 0 {
		if *book == "" {
			return fmt.Errorf("-book is required with -returns")
		}
		ret = &ledger.Lot{Book: *book, Asset: *returnsAsset, Amount: *returns}
	}
	if err := journal.Settle(*betlogPath, *ledgerPath, *id, betlog.Result(*result), ret, *note, time.Now()); err != nil {
		return err
	}
	fmt.Printf("settled %s %s\n", *id, *result)
	return nil
}
```

- [ ] **Step 4: Add the dispatch and a `DefaultPath` for each log if missing**

In `main.go`, add after the `ledger` case:

```go
	case "bet":
		err = betCmd(os.Args[2:])
```

Confirm `betlog.DefaultPath()` and `ledger.DefaultPath()` exist and are exported. If they are not (the existing defaults are the unexported `defaultBetlog()` in `board_log_api.go` and `defaultLedgerPath()` in `ledger.go`), add thin exported wrappers in the `betlog` and `ledger` packages that return the same `~/fanatics-bonus.jsonl` and `~/bankroll.jsonl`, and have the existing unexported cmd helpers call them, so there is a single source of truth for each default path.

- [ ] **Step 5: Add the escape-hatch help line to `ledger add`**

In `ledger.go`, change the `-kind` flag usage string so the `place` mention notes: `"deposit, withdraw, grant, convert, place, settle or expire (required; 'place' writes the ledger ONLY — use 'edgectl bet place' to also record the betlog)"`.

- [ ] **Step 6: Run tests and build**

Run: `cd app && go test ./... && go build -o edgectl ./cmd/edgectl`
Expected: PASS, clean build.

- [ ] **Step 7: Manual smoke (optional)**

```bash
cd app && ./edgectl bet place -betlog /tmp/b.jsonl -ledger /tmp/l.jsonl \
  -selection "smoke test" -price -110 -stake 1 -bankroll bonus -week 1
# expect: "placed <id>"; /tmp/b.jsonl has one bet line, /tmp/l.jsonl untouched (no book => no debit)
```

- [ ] **Step 8: Commit**

```bash
git add app/cmd/edgectl/bet.go app/cmd/edgectl/main.go app/cmd/edgectl/ledger.go app/cmd/edgectl/bet_test.go app/internal/betlog/ app/internal/ledger/
git commit -m "edge: edgectl bet place|settle — the blessed path over journal; note ledger add is ledger-only"
```

---

## Self-Review

**Spec coverage (Section 1 + Section 2 of the spec):**
- `journal.Place` writing betlog + ledger + boost consume → Task 1. ✓
- `journal.Settle` writing both → Task 2 (and it fixes the GUI-settle-never-touches-ledger gap). ✓
- GUI handlers become thin clients → Task 3. ✓
- CLI `bet place|settle`; `ledger add` kept as documented escape hatch → Task 4. ✓
- Section 3 (DK-only board) and Section 4 (funds→boosts UI, pick-on-place wiring) are Plans 2 and 3 — out of scope here by design. The boost-consume *mechanism* lives in `journal.Place` now (Task 1), so Plan 3 only wires the UI to it.

**Placeholder scan:** One deliberate scaffold remains — Task 3 Step 5 starts as a `t.Skip` then says to fill it in against the existing server harness in `board_place_test.go`. This is because the exact harness helper name must be read from that file at implementation time; the step names the file and the assertion. Acceptable; not a hidden TODO.

**Type consistency:** `journal.PlaceRequest{Bet, Book, BoostLotID}`, `journal.Place(betlogPath, ledgerPath, req, now)`, `journal.Settle(betlogPath, ledgerPath, id, betlog.Result, *ledger.Lot, note, now)` are used identically in Tasks 1-4. `AssetForBankroll` defined Task 1, not re-defined elsewhere. `betlog.Result` vs `ledger.Result` conversion is by string (`ledger.Result(result)`), consistent with the Global Constraints note.

**Open implementation detail:** Task 4 Step 4 flags that `DefaultPath()` may need adding to the `betlog`/`ledger` packages if the current defaults are unexported in `cmd`. This is a real fork the implementer resolves by reading those two files; both branches are spelled out.

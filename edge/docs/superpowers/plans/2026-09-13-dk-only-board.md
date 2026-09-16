# DK-Only Board Pricing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make DraftKings the board's single, default price source — reports/edge/EV read DK without a required `-book`, import defaults to DK, and the board pricing screen shows one DK column instead of per-book tabs/dropdowns — while keeping the multi-book schema and vs-consensus code intact but dormant.

**Architecture:** Introduce one `board.DefaultBook = "draftkings"` constant and route the report CLI, the report/import HTTP paths, and the board-pricing UI through it. The week YAML keeps every book's map; the simplified UI and the default report path just read/write the `draftkings` key. A one-time `board mirror` subcommand copies existing `consensus` prices into `draftkings` so boards aren't empty on the switch.

**Tech Stack:** Go (module `edge`, standard library). Frontend: vanilla JS (`cmd/edgectl/static/app.js`), verified headless via `node cmd/edgectl/static/smoke.js`.

**Spec:** `edge/docs/superpowers/specs/2026-09-13-cli-gui-consolidation-design.md` (Section 3)

## Global Constraints

- Go module `edge`; commands run from `edge/app`. Test with `go test ./...`; build `go build -o edgectl ./cmd/edgectl`; UI smoke `node cmd/edgectl/static/smoke.js` (also `make smoke`).
- **Do NOT remove** the multi-book schema, the `-book`/`-books` flags, book pooling, per-book funds, or the vs-consensus comparison code. They stay, dormant. DK is the *default*, not the only allowed value.
- `board.Books` (internal/board/board.go:69) is the canonical book list; `board.Consensus = "consensus"` (board.go:82). "draftkings" is already a member of `board.Books`.
- Append-only logs are untouched by this plan.
- Commit messages end with the two trailers:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01XAPcUXYJSuPDaKpHJYz22q`

## Out of scope

- The place-a-bet form's book dropdown (`#e-book`) — bets are still placed at many books; only the board *pricing* view goes DK-only. (That's Plan 3's neighbourhood, and even there the book stays.)
- The funds/boosts UI (Plan 3).

---

### Task 1: `board.DefaultBook` + report defaults to DK

**Files:**
- Modify: `app/internal/board/board.go` (add the constant near `Consensus`, ~line 82)
- Modify: `app/cmd/edgectl/board_report.go:79-88` (the required-book block)
- Modify: `app/cmd/edgectl/board_report_api.go:149-155` (the no-book default)
- Test: `app/cmd/edgectl/board_serve_test.go` (add a report-default case), `app/internal/board/board_test.go` (const presence, optional)

**Interfaces:**
- Produces: `board.DefaultBook` (string const = "draftkings"), consumed by Tasks 2 and by report/import.

- [ ] **Step 1: Add the constant**

In `app/internal/board/board.go`, directly after the `Consensus` const (line 82), add:

```go
// DefaultBook is the book the tools read when none is named. DraftKings is the
// operator's standing price source and stands in for consensus when computing
// edge/EV; a genuine consensus column still exists but is filled only when
// multi-book prices are pulled, which is not routine.
const DefaultBook = "draftkings"
```

- [ ] **Step 2: Write the failing test (report API defaults to DK)**

In `board_serve_test.go`, add:

```go
func TestServeReportDefaultsToDraftKings(t *testing.T) {
	ts, _ := newTestServer(t) // existing helper at board_serve_test.go:16 (seeds a 2-game week 1, serves it; ts auto-closes via t.Cleanup)
	// No ?book / ?books — the report must default to draftkings, not consensus.
	res, err := http.Get(ts.URL + "/api/report?week=1")
	if err != nil {
		t.Fatalf("GET /api/report: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var out struct {
		Book string `json:"book"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Book != "draftkings" {
		t.Fatalf("report book = %q, want draftkings", out.Book)
	}
}
```

`newTestServer` (board_serve_test.go:16) seeds a week-1 board whose games carry a `consensus` column and an empty `fanatics` column — DK is not primed, so this test proves the *default* landed on draftkings rather than consensus.

- [ ] **Step 3: Run it — expect failure**

Run: `cd app && go test ./cmd/edgectl/ -run TestServeReportDefaultsToDraftKings -v`
Expected: FAIL — `report book = "consensus"`.

- [ ] **Step 4: Change the API default**

In `board_report_api.go`, the block at ~149-155 currently ends:

```go
		} else {
			books = []string{board.Consensus}
		}
```

Change the fallback to:

```go
		} else {
			books = []string{board.DefaultBook}
		}
```

- [ ] **Step 5: Change the CLI default**

In `board_report.go`, replace the required-book block (~79-88):

```go
	if len(bookList) == 0 {
		return fmt.Errorf("-book or -books is required\n%s", coverageTable(doc, *week))
	}
```

with a default (keep the comment above it updated to say DK is the default now):

```go
	if len(bookList) == 0 {
		bookList = []string{board.DefaultBook}
	}
```

- [ ] **Step 6: Run tests**

Run: `cd app && go test ./cmd/edgectl/ ./internal/board/ -v 2>&1 | tail -20`
Expected: PASS, including the new test. Existing `-book`/`-books` tests still pass (the default only applies when none is given).

- [ ] **Step 7: Commit**

```bash
git add app/internal/board/board.go app/cmd/edgectl/board_report.go app/cmd/edgectl/board_report_api.go app/cmd/edgectl/board_serve_test.go
git commit -m "edge: board report defaults to DraftKings, no required -book"
```

---

### Task 2: import defaults to DK + `board mirror` consensus→DK backfill

**Files:**
- Modify: `app/cmd/edgectl/board_import.go:23,33` (default the book)
- Create: `app/cmd/edgectl/board_mirror.go` (a `board mirror` subcommand)
- Modify: `app/cmd/edgectl/board.go` (dispatch `mirror` under `board`)
- Test: `app/cmd/edgectl/board_mirror_test.go`

**Interfaces:**
- Consumes: `board.DefaultBook` (Task 1); `board.Doc.SetPrice(gameID, book, market, value)`, `board.Load`/save helpers used by the other board subcommands (read `board_import.go` and `board.go` for the exact loader/saver names and the `-dir` default `defaultBoardDir`).
- Produces: `edgectl board mirror -week N [-from consensus] [-to draftkings]`.

- [ ] **Step 1: Default the import book**

In `board_import.go`, line 23, change:

```go
	book := flags.String("book", "", "book to write, e.g. fanatics (required)")
```

to:

```go
	book := flags.String("book", board.DefaultBook, "book to write (default draftkings)")
```

And line 33, remove the "required" rejection (the default now covers it):

```go
	if *book == "" {
		return fmt.Errorf("-book is required (one of: %v)", board.Books)
	}
```

Replace with a validity check only:

```go
	if !slices.Contains(board.Books, *book) {
		return fmt.Errorf("unknown book %q (one of: %v)", *book, board.Books)
	}
```

(Confirm `slices` is imported; the report command already uses `slices.Contains`.)

- [ ] **Step 2: Write the failing test for `board mirror`**

Create `app/cmd/edgectl/board_mirror_test.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"edge/internal/board"
)

// loadWeek re-reads a week file the same way board_import.go does.
func loadWeek(t *testing.T, dir string, week int) *board.Doc {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, fmt.Sprintf("week%02d.yaml", week)))
	if err != nil {
		t.Fatalf("open week: %v", err)
	}
	defer f.Close()
	doc, err := board.Parse(f)
	if err != nil {
		t.Fatalf("parse week: %v", err)
	}
	return doc
}

func TestBoardMirror_copiesConsensusToDraftKings(t *testing.T) {
	dir := t.TempDir()
	// Seed a week file directly, the same shape newTestServer uses: a consensus
	// ML present, draftkings empty.
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{
				"consensus":  {ML: "+145/-175"},
				"draftkings": {},
			}},
	}}
	path := filepath.Join(dir, "week01.yaml")
	if err := writeDoc(path, doc); err != nil {
		t.Fatalf("writeDoc: %v", err)
	}

	if err := boardMirror([]string{"-dir", dir, "-week", "1"}); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	got := loadWeek(t, dir, 1)
	if got.Games["2026_01_SF_LA"].Books["draftkings"].ML != "+145/-175" {
		t.Fatalf("draftkings ML = %q, want the mirrored consensus value",
			got.Games["2026_01_SF_LA"].Books["draftkings"].ML)
	}
}
```

This uses only real, verified names: `board.Doc`/`board.Game`/`board.Lines` (as constructed in `board_serve_test.go:19-27`), `writeDoc` (board.go:124), and `board.Parse` (the loader `board_import.go:50` uses). No scaffold or invented loader/saver.

- [ ] **Step 3: Run it — expect failure**

Run: `cd app && go test ./cmd/edgectl/ -run TestBoardMirror -v`
Expected: FAIL — `undefined: boardMirror`.

- [ ] **Step 4: Implement `board mirror`**

Create `app/cmd/edgectl/board_mirror.go`. Mirror the structure of `board_import.go` (same flagset style, same loader/saver). Copy every non-empty `-from` cell (ml/spread/total) into `-to`, per game, then save once:

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"edge/internal/board"
)

// boardMirror copies one book's prices into another for a week -- the one-time
// bridge for going DK-only when boards were priced on the consensus column.
// It never overwrites a non-empty destination cell and never clears a source.
func boardMirror(args []string) error {
	fs := flag.NewFlagSet("board mirror", flag.ContinueOnError)
	dir := fs.String("dir", defaultBoardDir, "directory holding the week files")
	week := fs.Int("week", 0, "NFL week to mirror (required)")
	from := fs.String("from", board.Consensus, "source book to copy from")
	to := fs.String("to", board.DefaultBook, "destination book to copy into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}

	// Load exactly as board_import.go does: open week%02d.yaml and board.Parse it.
	path := filepath.Join(*dir, fmt.Sprintf("week%02d.yaml", *week))
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	doc, err := board.Parse(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s is not readable, refusing to write to it: %w", path, err)
	}

	copied := 0
	for _, gid := range doc.GameIDs() {
		src := doc.Games[gid].Books[*from]
		dst := doc.Games[gid].Books[*to]
		for _, m := range []struct {
			name, srcVal, dstVal string
		}{
			{"ml", src.ML, dst.ML},
			{"spread", src.Spread, dst.Spread},
			{"total", src.Total, dst.Total},
		} {
			if m.srcVal == "" || m.dstVal != "" { // nothing to copy, or don't clobber
				continue
			}
			if err := doc.SetPrice(gid, *to, m.name, m.srcVal); err != nil {
				return fmt.Errorf("%s %s: %w", gid, m.name, err)
			}
			copied++
		}
	}

	if err := writeDoc(path, doc); err != nil { // writeDoc: board.go:124
		return err
	}
	fmt.Printf("mirrored %d cells from %s to %s for week %d\n", copied, *from, *to, *week)
	return nil
}
```

This uses the verified real APIs: `board.Parse` (loader, board_import.go:50), `doc.GameIDs()` (board.go:137), `board.Lines` fields `ML`/`Spread`/`Total`, `doc.SetPrice` (importer.go:230), and `writeDoc` (board.go:124). `defaultBoardDir` is the same package-level default the other board subcommands use (read board.go).

- [ ] **Step 5: Dispatch it under `board`**

In `board.go`, find the `switch` over the board subcommand (scaffold/validate/report/serve/import) and add:

```go
	case "mirror":
		return boardMirror(args[1:])
```

Match the exact arg-slicing pattern the sibling cases use.

- [ ] **Step 6: Run tests**

Run: `cd app && go test ./cmd/edgectl/ -run 'TestBoardMirror|Import' -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add app/cmd/edgectl/board_import.go app/cmd/edgectl/board_mirror.go app/cmd/edgectl/board.go app/cmd/edgectl/board_mirror_test.go
git commit -m "edge: board import defaults to DK; board mirror backfills consensus->DK"
```

---

### Task 3: board pricing view is DK-only (JS/HTML)

**Files:**
- Modify: `app/cmd/edgectl/static/index.html` (~32-44: the `#book` selector in the header)
- Modify: `app/cmd/edgectl/static/app.js` (state default; the `#book` wiring at ~1288-1298 and population at ~1366/1376; `render()`/`saveRow()` book usage)
- Modify (if needed): `app/cmd/edgectl/static/smoke.js` (its DOM stub / mock `/api/board` if it references `#book`)

**Interfaces:**
- Consumes: `/api/price` (unchanged; still takes a `book`), `/api/board` (unchanged; still returns all books).
- Produces: no new endpoints. The board view always sends `book: "draftkings"`.

- [ ] **Step 1: Pin the book in state**

In `app.js`, the state defaults (~56-73): make `state.book` always default to `"draftkings"` (replace any persisted/other default). The board view and the report view both read `state.book`; pinning it to draftkings makes both DK by default. Keep reading a persisted `state.book` only if you also keep a way to change it — but since the selector is being removed, set it unconditionally to `"draftkings"` on load.

```js
// board pricing and the default report both read one book: DraftKings.
state.book = "draftkings";
```

- [ ] **Step 2: Remove the book selector from the header**

In `index.html` (~32-44), delete the `<select id="book">` element (and its label, if any). Leave the week selector (`#week`) and the views control (`#views`).

- [ ] **Step 3: Remove the selector's JS wiring**

In `app.js`, delete the `el.book` change listener (~1288-1298) and the code that populates the book dropdown from `data.books` (~1366, 1376). Remove the `el.book` lookup if it now references a missing element (guard or delete). `render()` and `saveRow()` keep using `state.book` (now always "draftkings"); do not hardcode the string in more than one place — they already read `state.book`.

- [ ] **Step 4: Keep the consensus reference display**

Do NOT remove the `.cons` consensus display in `render()`/`fmtCons` (~163-165, 251). It stays as the dormant vs-consensus reference (blank when no consensus is present). This is a deliberate keep, per the spec's non-goal.

- [ ] **Step 5: Make the smoke test run**

Run: `cd app && node cmd/edgectl/static/smoke.js`
If it throws a ReferenceError on `el.book` or its mock lacks `#book`, update `smoke.js`'s DOM stub / mock so `render()` runs to completion again (the smoke test's whole job is to prove `render()` executes). Do not weaken what it asserts — just reflect the removed element.
Expected after fixes: the smoke test prints `ok` lines and exits 0.

- [ ] **Step 6: Verify the price round-trip still works server-side**

Run: `cd app && go test ./cmd/edgectl/ -run 'TestServePrice' -v`
Expected: PASS — the existing `/api/price` round-trip tests are unaffected (the endpoint still takes a book; the UI just always sends draftkings).

- [ ] **Step 7: Manual sanity (optional, if a server is handy)**

Build and serve, load the board, confirm one price column, enter a DK ML, reload, confirm it persisted under `draftkings`:

```bash
cd app && go build -o edgectl ./cmd/edgectl
# ./edgectl board serve -addr :8099 -dir moneylines   # then open http://localhost:8099
```

- [ ] **Step 8: Commit**

```bash
git add app/cmd/edgectl/static/index.html app/cmd/edgectl/static/app.js app/cmd/edgectl/static/smoke.js
git commit -m "edge: board pricing view is DraftKings-only (book selector removed)"
```

---

## Self-Review

**Spec coverage (Section 3):**
- DK is the default price source; report drops required `-book` → Task 1. ✓
- `board import` defaults to DK → Task 2. ✓
- One-time `consensus → draftkings` backfill → Task 2 (`board mirror`). ✓
- GUI shows one DK price column, book selector gone → Task 3. ✓
- Multi-book schema + `-book`/`-books` + vs-consensus code kept dormant → Global Constraints + Task 3 Step 4 (consensus display kept). ✓
- "best price vs consensus" losing meaning with one book → inherent, documented in the spec; no code removed. ✓

**Placeholder scan:** Clean. All names are pinned to verified real APIs — `board.Parse` (board_import.go:50), `writeDoc` (board.go:124), `doc.SetPrice` (importer.go:230), `doc.GameIDs` (board.go:137), `board.Doc`/`Game`/`Lines` (board_serve_test.go:19-27), `newTestServer` (board_serve_test.go:16), `board.DefaultBook` (added in Task 1). No stand-in identifiers remain.

**Type consistency:** `board.DefaultBook` (Task 1) is used identically in Tasks 1 and 2. `state.book` in Task 3 is the existing global. `/api/price` payload shape unchanged. `board.Lines` field names `ML`/`Spread`/`Total` match across the mirror impl and test.

**Open detail (JS, Task 3):** exact line numbers in `app.js`/`index.html` for the `#book` selector and its wiring come from the scout map (selector `#book`; listener ~1288-1298; population ~1366/1376; `render()`~200, `saveRow()`~294). The implementer confirms them against the live file, since line numbers drift; the identifiers (`el.book`, `state.book`, `render`, `saveRow`) are stable anchors.

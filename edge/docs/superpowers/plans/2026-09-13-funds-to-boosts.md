# Funds→Boosts UI + Pick-on-Place Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the GUI's per-book money-management screen with a lean "boosts on hand" view, and let a bet apply a boost when it is placed (pick-on-place), consuming it through the shared `journal.Place` core.

**Architecture:** Three focused changes: (1) the place handler passes an optional boost lot id into `journal.PlaceRequest.BoostLotID` (the consume-and-validate logic already exists in `internal/journal` from Plan 1); (2) the funds view (`renderFunds`) drops its balances / declare-funds / balance-correction sections and keeps the boosts panel, declare-boost, and declare-no-sweat; (3) the place form gains a boost dropdown populated from the selected book's live boosts. The ledger and `/api/funds*` endpoints stay on disk — this hides the money UI, it does not remove the accounting.

**Tech Stack:** Go (module `edge`, std lib). Frontend: vanilla JS (`cmd/edgectl/static/app.js`, `index.html`), verified headless via `node cmd/edgectl/static/smoke.js`.

**Spec:** `edge/docs/superpowers/specs/2026-09-13-cli-gui-consolidation-design.md` (Section 4)

## Global Constraints

- Go module `edge`; run from `edge/app`. Test `go test ./...`; build `go build -o edgectl ./cmd/edgectl`; UI smoke `node cmd/edgectl/static/smoke.js`.
- **Keep the ledger and the `/api/funds`, `/api/funds/adjust` handlers on disk** — the money model is unchanged; only the GUI stops surfacing it. `edgectl ledger balances` remains the way to see full accounting. (Removing the now-unused Go money endpoints is a deliberate future cleanup, out of scope here.)
- **Keep** `/api/boosts`, `/api/funds/expire`, `fundsFor` (the report uses it), and the place/board book dropdowns.
- `journal.Place(betlogPath, ledgerPath, journal.PlaceRequest{Bet, Book, BoostLotID}, now)` already validates a boost lot (exists, book match, RequiresCashStake) and consumes it as a ledger place tied to the wager — do not reimplement that; just pass `BoostLotID`.
- Append-only logs untouched. Commit messages end with:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01XAPcUXYJSuPDaKpHJYz22q`

## Out of scope
- Removing the dead Go money endpoints/handlers (`handleFunds` POST/`addFunds`/`handleAdjust`/`withdrawFrom`) — kept dormant; a later cleanup.
- Any change to `journal` (Plan 1 built the boost path).

---

### Task 1: place handler passes a boost lot id to `journal`

**Files:**
- Modify: `app/cmd/edgectl/board_log_api.go` (the `placeReq` struct ~146-161, and `handlePlace`'s `journal.Place` call ~173)
- Test: `app/cmd/edgectl/board_place_test.go`

**Interfaces:**
- Consumes: `journal.Place`, `journal.PlaceRequest{Bet, Book, BoostLotID}` (from Plan 1); `newBoardServer(dir, ledgerPath string)`; `ledger.AppendFile`, `ledger.Event`, `ledger.Lot`, `ledger.BoostSpec`, `ledger.KindGrant`, `ledger.KindPlace`, `ledger.Cash`, `ledger.Boost`.
- Produces: `placeReq.Boost` (json `"boost"`) threaded to `BoostLotID`.

- [ ] **Step 1: Write the failing test**

The existing `newTestServer` (board_serve_test.go:16) builds a server with an EMPTY ledger path. This test needs a ledger, so it builds the server directly with a temp ledger seeded with a cash lot and a boost lot, then posts a boosted place and asserts the boost was consumed.

```go
func TestPlace_appliesBoostFromRequest(t *testing.T) {
	dir := t.TempDir()
	// A board so newBoardServer is happy.
	doc := &board.Doc{Season: 2026, Week: 1, Games: map[string]*board.Game{
		"2026_01_SF_LA": {Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
			Books: map[string]board.Lines{"consensus": {}, "draftkings": {}}}}}
	if err := writeDoc(filepath.Join(dir, "week01.yaml"), doc); err != nil {
		t.Fatal(err)
	}
	lg := filepath.Join(dir, "bank.jsonl")
	// Seed a draftkings cash lot and a draftkings boost lot.
	must := func(e ledger.Event) {
		if err := ledger.AppendFile(lg, e); err != nil {
			t.Fatal(err)
		}
	}
	must(ledger.Event{Kind: ledger.KindGrant, ID: "dk-cash", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-cash", Book: "draftkings", Asset: ledger.Cash, Amount: 50}})
	must(ledger.Event{Kind: ledger.KindGrant, ID: "dk-boost", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-boost", Book: "draftkings", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.2, MaxStake: 50, MinOdds: -300}}})

	srv, err := newBoardServer(dir, lg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := srv.routes(mux); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	code, body := post(t, ts, "/api/place", map[string]any{
		"selection": "MHJ ATD", "price": -110, "stake": 10,
		"bankroll": "real money", "book": "draftkings", "week": 1,
		"boost": "dk-boost",
	})
	if code != 200 {
		t.Fatalf("place status %d: %v", code, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatal("no wager id returned")
	}

	// The boost lot must have been consumed as a ledger place tied to the wager.
	evs, err := ledger.Load(lg)
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Lot == "dk-boost" && e.Wager == id {
			consumed = true
		}
	}
	if !consumed {
		t.Fatal("boost lot was not consumed against the wager")
	}
}
```

Confirm the imports this needs (`net/http`, `net/http/httptest`, `path/filepath`, `time`, `edge/internal/board`, `edge/internal/ledger`) are present in the test file or add them. `post` (board_serve_test.go:46) returns `(int, map[string]any)`.

- [ ] **Step 2: Run it — expect failure**

Run: `cd app && go test ./cmd/edgectl/ -run TestPlace_appliesBoostFromRequest -v`
Expected: FAIL — the boost is never consumed (placeReq has no `Boost` field yet, so it's dropped).

- [ ] **Step 3: Add the field and thread it**

In `board_log_api.go`, add to the `placeReq` struct:

```go
	// Boost is an optional boost lot id to apply to this wager; the core
	// validates and consumes it.
	Boost string `json:"boost"`
```

In `handlePlace`, change the `journal.Place` call to pass it:

```go
	id, err := journal.Place(s.betlogPath, s.ledgerPath, journal.PlaceRequest{
		Bet: b, Book: req.Book, BoostLotID: req.Boost,
	}, time.Now())
```

- [ ] **Step 4: Run tests**

Run: `cd app && go test ./cmd/edgectl/ -run 'Place' -v`
Expected: PASS (the new test and existing place tests).

- [ ] **Step 5: Commit**

```bash
git add app/cmd/edgectl/board_log_api.go app/cmd/edgectl/board_place_test.go
git commit -m "edge: place handler applies an optional boost lot via journal"
```

---

### Task 2: funds view becomes boosts-only (JS/HTML)

**Files:**
- Modify: `app/cmd/edgectl/static/app.js` (`renderFunds` ~988-1228, `loadFunds` ~976; the view label)
- Modify: `app/cmd/edgectl/static/index.html` (the `#views` button `data-view="funds"` label; the `#funds` container id can stay)
- Modify (if needed): `smoke.js`

**Interfaces:**
- Consumes: `/api/funds` (GET — still read for the `expiring` list), `/api/boosts`, `/api/funds/expire` (unchanged). Drops the UI's use of `POST /api/funds` and `/api/funds/adjust`.

- [ ] **Step 1: Trim `renderFunds` to boosts-only**

In `renderFunds` (app.js:988), the `el.funds.innerHTML` template currently contains these sections in order: `expiring`, `balances`, `<div id="boostbox">`, `declare funds`, `declare a boost`, `declare a no-sweat token`. Remove **only** the **balances** section (app.js ~1007-1031) and the **declare funds** section (~1035-1046). Keep `expiring`, `<div id="boostbox"></div>`, `declare a boost`, and `declare a no-sweat token`.

Then remove the two now-dead handler blocks below the template: the `.fix` balance-correction loop (~1086-1110, posts `/api/funds/adjust`) and the `f-add` declare-funds handler (posts `POST /api/funds`). Keep the `b-add` (declare boost → `/api/boosts`) and `n-add` (no-sweat → `/api/boosts`) handlers, and the `loadBoosts()`/`renderBoosts()` flow that fills `#boostbox`.

Update the section `<h2>` from any "funds"/"balances" framing to "boosts on hand" where appropriate; the view is now about tokens, not money.

- [ ] **Step 2: Relabel the view**

In `index.html`, the `#views` segmented control has a `data-view="funds"` button — change its visible text from "funds" to "boosts". Leave `data-view="funds"` and the `#funds` container id as-is (renaming the routing key is churn for no gain; only the label changes). If `syncView()`/`loadFunds()` reference the label anywhere, they key off `data-view`/ids, not the text, so no JS routing change is needed — verify.

- [ ] **Step 3: Keep the money accounting reachable — a one-line pointer**

Add a short `<p class="muted">` under the boosts view noting that full cash/bonus balances live in `edgectl ledger balances` (the money UI moved out of the GUI by design). This keeps the removed capability discoverable.

- [ ] **Step 4: Smoke test**

Run: `cd app && node cmd/edgectl/static/smoke.js`
If removing the balances/declare-funds markup or the `.fix`/`f-add` handlers leaves a dangling reference (e.g. a `getElementById("f-add")` with no element — harmless if guarded, a throw if not), fix it so `render()`/`renderFunds()` run clean. The smoke harness auto-mocks `getElementById`, so missing elements return a stub rather than null; still, verify the run exits 0 with `ok` lines and update the stub only if a genuine ReferenceError appears.
Expected: exit 0.

- [ ] **Step 5: Confirm the endpoints the view still needs are intact server-side**

Run: `cd app && go test ./cmd/edgectl/ -run 'Boost|Fund' -v`
Expected: PASS — `/api/boosts` and `/api/funds` GET are unchanged; this proves nothing server-side broke.

- [ ] **Step 6: Commit**

```bash
git add app/cmd/edgectl/static/app.js app/cmd/edgectl/static/index.html app/cmd/edgectl/static/smoke.js
git commit -m "edge: GUI funds view becomes boosts-only (money management moves to the CLI)"
```

---

### Task 3: pick-on-place boost dropdown (JS)

**Files:**
- Modify: `app/cmd/edgectl/static/app.js` (`betEntryForm` ~604-620, `wireBetEntry` ~622-654)
- Modify (if needed): `smoke.js`

**Interfaces:**
- Consumes: `GET /api/boosts` (returns the live boosts, each with `book` and an `id`), `POST /api/place` (now accepts `boost`, from Task 1).

- [ ] **Step 1: Add a boost `<select>` to the place form**

In `betEntryForm()`, add a boost dropdown after the `#e-book` select, defaulting to a "— no boost" option:

```js
      <select id="e-boost"><option value="">— no boost</option></select>
```

- [ ] **Step 2: Populate it from the selected book's live boosts**

In `wireBetEntry()`, after grabbing the button, fetch the boosts once and (re)fill `#e-boost` whenever the book changes, listing only boosts whose `book` matches the chosen book. Add:

```js
  const boostSel = document.getElementById("e-boost");
  const bookSel = document.getElementById("e-book");
  let allBoosts = [];
  async function loadBoostOptions() {
    try {
      const res = await fetch(BASE + "api/boosts");
      const r = await res.json();
      allBoosts = (r && r.boosts) || [];
    } catch (e) { allBoosts = []; }
    fillBoostOptions();
  }
  function fillBoostOptions() {
    if (!boostSel) return;
    let book = bookSel ? bookSel.value : "";
    if (book.startsWith("—")) book = "";
    const opts = ['<option value="">— no boost</option>'];
    for (const b of allBoosts) {
      if (b.kind !== "boost") continue; // no-sweat tokens are a different flow
      if (book && b.book !== book) continue;
      const label = `${b.book} ${b.label || (Math.round((b.percent || 0) * 100) + "% boost")}`;
      opts.push(`<option value="${b.id}">${label}</option>`);
    }
    boostSel.innerHTML = opts.join("");
  }
  if (bookSel) bookSel.addEventListener("change", fillBoostOptions);
  loadBoostOptions();
```

The `/api/boosts` shape is confirmed: response is `{"boosts": [...]}`, each item has `id`, `book`, `kind` ("boost"|"nosweat"), `percent`, `label` (`handleBoosts` in board_funds_api.go; `renderBoosts` reads the same fields). The `kind !== "boost"` filter excludes no-sweat tokens deliberately — `journal.Place` consumes a boost lot but does not apply a no-sweat's refund-on-loss, so no-sweats are not offered on the place form here.

- [ ] **Step 3: Send the chosen boost with the place**

In `wireBetEntry`'s click handler, include the boost id in the POST body:

```js
        body: JSON.stringify({
          selection: sel, price, stake,
          bankroll: document.getElementById("e-bank").value,
          book, week: Number(document.getElementById("e-week").value) || 0,
          boost: (document.getElementById("e-boost") || {}).value || "",
          narrative: "Entered from the log tab.",
        }),
```

- [ ] **Step 4: Smoke test**

Run: `cd app && node cmd/edgectl/static/smoke.js`
The smoke harness stubs `fetch`; ensure the added `loadBoostOptions()` call (which fetches on form wire-up) does not throw under the stub — guard the fetch in a try/catch (already shown). Verify exit 0.

- [ ] **Step 5: Manual sanity (optional, if a server is handy)**

Build and serve; open the log tab; pick a book that has a live boost; confirm the boost dropdown lists it; place a small bet with the boost; confirm (via `edgectl ledger balances` or the boosts view) the boost was consumed.

- [ ] **Step 6: Commit**

```bash
git add app/cmd/edgectl/static/app.js app/cmd/edgectl/static/smoke.js
git commit -m "edge: pick-on-place — apply a book's boost when logging a bet"
```

---

## Self-Review

**Spec coverage (Section 4):**
- Remove per-book cash/bonus balance screens from the GUI → Task 2 (balances + declare-funds + fix/adjust removed). ✓
- Keep a lean boosts-on-hand list (book, terms, expiry) with add/retire → Task 2 keeps `expiring`, `#boostbox`/`renderBoosts`, declare-boost, declare-no-sweat, and `/api/funds/expire` retire. ✓
- Ledger accounting stays; `edgectl ledger balances` remains → Global Constraints + Task 2 Step 3 pointer. ✓
- Pick-on-place: a boost dropdown on the place form filtered to the book, marking the boost used via the core → Task 1 (handler threads `boost`→`BoostLotID`; journal consumes+validates) + Task 3 (the dropdown). ✓
- "book on every bet and every boost" tracking preserved → the place book dropdown and the boosts' book field are untouched. ✓

**Placeholder scan:** Clean. `/api/boosts` shape is confirmed (`{"boosts":[{id,book,kind,percent,label,...}]}`). Task 1's test uses verified names (`newBoardServer(dir, ledgerPath)`, `writeDoc`, `post`, `ledger.AppendFile`/`Event`/`Lot`/`BoostSpec`/`KindGrant`/`KindPlace`/`Cash`/`Boost`, `board.Doc`/`Game`/`Lines`). All steps carry real code.

**Type consistency:** `placeReq.Boost` (json `"boost"`) in Task 1 matches the `boost` key the place POST sends in Task 3. `journal.PlaceRequest{Bet, Book, BoostLotID}` matches Plan 1's shape. The view routing key stays `data-view="funds"`/`#funds` (Task 2 only relabels text), so `syncView`/`loadFunds` are unaffected. The dropdown filters `kind === "boost"`, matching the `handleBoosts` `kind` field.

**Open detail (JS line numbers):** app.js line ranges (`renderFunds` ~988-1228, `betEntryForm` ~604, `wireBetEntry` ~622) drift; the implementer confirms against the live file using the stable identifiers (`renderFunds`, `betEntryForm`, `wireBetEntry`, `#e-book`, `#boostbox`, the `f-add`/`.fix` handlers).

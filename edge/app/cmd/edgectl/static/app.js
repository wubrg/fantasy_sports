"use strict";

// The board, scoped to one week and one book (DraftKings) at a time.
//
// Prices come in from HAR captures, synced onto the board from the props tab
// -- there is no hand-typed entry form anymore; see oddssync.go and
// board_props_sync_api.go for how a capture becomes a board cell.

// Every request is resolved against BASE rather than written as a root-
// relative path, because this page is served two ways.
//
// Locally it sits at "/". Over Tailscale it is mounted with
// `tailscale serve --set-path=/edge`, which strips the mount before
// forwarding -- so the backend still sees /api/board and needs no prefix
// awareness. The catch is on this side: a relative "api/board" resolves
// against the CURRENT path, so it is correct at /edge/ and wrong at /edge,
// where it escapes the mount and hits the tailnet root. Deriving the base and
// forcing the trailing slash is correct at "/", "/edge" and "/edge/" alike.
const BASE = location.pathname.endsWith("/") ? location.pathname : location.pathname + "/";

const STORE = "edgectl.board";

const el = {
  week: document.getElementById("week"),
  views: document.getElementById("views"),
  report: document.getElementById("report"),
  betlog: document.getElementById("betlog"),
  props: document.getElementById("props"),
  funds: document.getElementById("funds"),
  period: document.getElementById("period"),
  beliefs: document.getElementById("beliefs"),
  help: document.getElementById("help"),
  banner: document.getElementById("banner"),
  pasteToggle: document.getElementById("pasteToggle"),
  pasteBody: document.getElementById("pasteBody"),
  blob: document.getElementById("blob"),
  preview: document.getElementById("preview"),
  apply: document.getElementById("apply"),
  diff: document.getElementById("diff"),
};

const state = load();
if (!["bets", "log", "props", "funds", "period", "beliefs", "help"].includes(state.view)) state.view = "bets";
let data = null;      // last /api/board payload

// ---- selection, remembered ---------------------------------------------

// The selection is persisted because a week is entered over several sittings
// and a reload that dropped you back on week 1 / the first book would cost a
// scroll and a wrong-column entry every time.
function load() {
  let s = {};
  try { s = JSON.parse(localStorage.getItem(STORE) || "{}"); } catch (e) { s = {}; }
  return {
    week: Number(s.week) || 1,
    // The board prices one book: DraftKings. Prices arrive via the props-sync
    // endpoints (see board_props_sync_api.go), which always sync draftkings,
    // so this is pinned rather than user-selectable.
    book: "draftkings",
  };
}

function save() {
  try { localStorage.setItem(STORE, JSON.stringify(state)); } catch (e) { /* private mode */ }
}

// ---- the bets view -------------------------------------------------------
//
// The whole point of entering a board is to find out what to bet, and that
// answer used to live only in `edgectl board report` on a terminal. Prices are
// typed on a phone with a book's app open next to it; making the operator walk
// to a desk to read the result is the friction this page exists to remove.

function pct(x) { return (x * 100).toFixed(1) + "%"; }
// Shared rather than defined inside one renderer. It was local to renderLog,
// and using it from renderReport threw at the moment a bankroll existed --
// which is to say, the first time the feature it formats was exercised.
function money(x) { return "$" + Number(x || 0).toFixed(2); }
function amer(n) { return n > 0 ? "+" + n : String(n); }

async function loadReport() {
  el.report.innerHTML = `<p class="muted">working…</p>`;
  try {
    const res = await fetch(BASE + "api/report?week=" + encodeURIComponent(state.week) +
      "&books=" + encodeURIComponent((state.books || [state.book]).join(",")) +
      "&shots=" + encodeURIComponent(state.shots || 4));
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    renderReport(r);
  } catch (e) {
    el.report.innerHTML = `<p class="muted">could not build the report: ${e.message}</p>`;
  }
}

function renderReport(r) {
  lastReport = r;
  const out = [];

  // What this is a report OF. After switching selectors -- or looking at a
  // screenshot later -- there must be no ambiguity about which books and how
  // much of them produced these numbers.
  out.push(`<div class="scope">week ${r.week} · <b>${(r.books || [r.book]).join(" + ")}</b> ·
    ${r.priced} of ${r.total} priced</div>`);

  // The book pool is a SEPARATE control from the entry selector, deliberately.
  // Entering prices is one book at a time -- you are looking at one app --
  // while pooling for a report is many. One multi-select serving both would
  // make the enter tab nonsensical.
  //
  // Each chip says why it is worth tapping: whether the book has prices this
  // week, and whether it holds funds. A book with neither is the commonest
  // wrong tap, and nothing on the chip would otherwise say so.
  const pooled = new Set(r.books || [r.book]);
  const priced = new Set(r.priced_books || []);
  const funded = r.funds || {};
  out.push(`<div class="chips">${(data && data.books ? data.books : []).map((b) => {
    const bits = [];
    if (priced.has(b)) bits.push("priced");
    if (funded[b] > 0) bits.push(money(funded[b]));
    return `<button type="button" class="chip${pooled.has(b) ? " on" : ""}" data-book="${b}">
      ${b}${bits.length ? `<span class="muted">${bits.join(" · ")}</span>` : ""}
    </button>`;
  }).join("")}</div>`);

  if (!r.priced) {
    out.push(`<section class="rep"><h2>nothing priced</h2>
      <p class="muted">No ${r.book} prices in week ${r.week} yet. Drop a DraftKings
      capture in the ingest folder and sync it from the props tab.</p></section>`);
    el.report.innerHTML = out.join("");
    return;
  }

  // Suspect lines come first. A price the tool cannot believe is the one thing
  // here that is probably a typo rather than a decision, and it is cheapest to
  // fix while the book is still open in the other app.
  if (r.suspect && r.suspect.length) {
    out.push(`<section class="rep warn">
      <h2>check these first</h2>
      ${r.suspect.map(s => `<div class="srow">
        <b>${s.game}</b> <span class="mono">${s.price}</span>
        <div class="muted">${pct(s.overround)} overround — ${s.why}</div>
      </div>`).join("")}
      <p class="muted">Held out of the wagers below: a de-vig you cannot trust
      should not propagate into three more numbers.</p>
    </section>`);
  }

  // The bankroll, when there is one. How far to split it is the deployment
  // decision, and it belongs above the tickets it produces.
  if ((r.frontier || []).length) {
    out.push(`<section class="rep">
      <h2>deploying ${money(Object.values(r.funds || {}).reduce((a, b) => a + b, 0))}</h2>
      ${r.free_split ? `<p class="muted">Splitting further costs nothing here: every row
        below is beaten by the last on BOTH conversion and hit rate.</p>` : ""}
      <div class="front">
        ${r.frontier.map(f => `<button type="button" class="frow${
            f.shots === (state.shots || 4) ? " on" : ""}${f.dominated ? " dom" : ""}"
            data-shots="${f.shots}">
          <b>${f.shots}</b> \u00d7 ${money(f.stake)}
          <span class="muted">${pct(f.conversion)} \u00b7 ${pct(f.any_hit)} hit \u00b7 ${money(f.ev)}</span>
        </button>`).join("")}
      </div>
      ${(r.advice || []).map(x => `<p class="muted">\u2014 ${x}</p>`).join("")}
    </section>`);
  }
  if ((r.alloc || []).length) {
    out.push(`<section class="rep">
      <h2>per-book allocation</h2>
      ${r.alloc.map(a => `<div class="dog${a.idle ? " under" : ""}">
        <span class="team">${a.book}</span>
        <span class="price">${a.tickets} ticket(s)</span>
        <span class="conv">${a.stake ? money(a.stake) + " each" : "\u2014"}</span>
        ${a.unfunded ? `<span class="tag">no balance</span>`
          : a.idle ? `<span class="tag">unused</span>` : ""}
      </div>`).join("")}
      <p class="muted">Each book funds its own tickets: a promotional balance cannot
      move between books. An unused balance is only stranded if it is bonus money.</p>
    </section>`);
  }

  // The wagers. This is the answer the page is for, so it goes above the
  // reasoning rather than below it.
  if (r.set && r.set.length) {
    out.push(`<section class="rep">
      <h2>wagers — ${r.set.length} disjoint shot${r.set.length > 1 ? "s" : ""}${
        r.provisional ? ` <span class="badge">provisional</span>` : ""}</h2>
      ${r.provisional ? `<p class="muted prov">${r.missing} game${r.missing === 1 ? " has" : "s have"}
        no ${r.book} price. This is the best pairing of the ${r.priced} that do, which is not the
        same as the best pairing of the week — expect it to change as you enter more.</p>` : ""}
      ${r.set.map((p, i) => { const bst = (r.boosts || []).find(b => b.ticket === i); return `<div class="bet" data-i="${i}">
        <div class="legs">${p.book && (r.books || []).length > 1
          ? `<span class="bk">${p.book}</span> ` : ""}${p.teams.join(" + ")}</div>
        <div class="nums"><span class="price mono">${amer(p.price)}</span>
          <span class="muted">${pct(p.conversion)} conv · ${pct(p.true_prob)} to hit</span></div>
        <button type="button" class="rec" data-i="${i}">record</button>
        ${bst ? `<div class="boosted">apply <b>${bst.label}</b> \u2014 adds ${money(bst.adds)}${
          bst.capped ? " (capped at its max stake)" : ""}</div>` : ""}
      </div>`; }).join("")}
      ${r.boost_adds ? `<p class="sum">boosts add <b>${money(r.boost_adds)}</b> on top</p>` : ""}
      ${r.prop_boosts ? `<p class="muted">${r.prop_boosts} prop-only boost(s) held and not
        counted \u2014 the board carries no prop prices to match them against.</p>` : ""}
      ${r.cash_boosts ? `<p class="muted">${r.cash_boosts} boost(s) held that need a
        REAL-MONEY stake and will not attach to bonus money \u2014 worth nothing against
        this bankroll, however good the terms look.</p>` : ""}
      <p class="sum">avg conversion <b>${pct(r.avg_conversion)}</b> ·
        <b>${pct(r.any_hit)}</b> chance at least one hits</p>
      <p class="muted">No team appears twice, so every ticket can be live at
      once without one hedging another.${r.unfilled ? ` ${r.unfilled} shot(s)
      could not be built from the games priced so far.` : ""}</p>
    </section>`);
  } else {
    // Distinguish "the board is empty" from "you have already bet it all".
    // Both show no wagers, and only one of them is a problem.
    const n = (r.committed || []).length;
    out.push(`<section class="rep"><h2>no wagers available</h2>
      <p class="muted">${n
        ? `Every priced game is already carrying an open wager, or was excluded.
           This week is spent \u2014 settle what is open, or move to a later week.`
        : `Not enough priced games from distinct matchups to build a disjoint set.
           Enter a few more.`}</p></section>`);
  }

  // Committed wagers, always shown when there are any. A derived exclusion has
  // to be visible: silently dropping half the board leaves "you already bet it"
  // and "the tool is broken" looking identical from outside.
  if ((r.committed || []).length) {
    out.push(`<section class="rep">
      <h2>already committed \u2014 ${r.committed.length} open</h2>
      ${r.committed.map(c => `<div class="dog">
        <span class="team">${c.teams.join(",")}</span>
        <span class="conv muted">${c.selection}</span>
      </div>`).join("")}
      <p class="muted">These games are off the board until they settle. Two tickets on
      one game are one chance counted twice, not two.</p>
    </section>`);
  }

  // Dogs, ranked by what a bonus bet actually converts rather than by price --
  // the distinction the whole tool turns on.
  out.push(`<section class="rep">
    <h2>dogs by conversion</h2>
    <p class="muted">Floor is ${amer(r.floor)} (${pct(r.target)}). Ranked by what a
    bonus bet returns after the vig comes off, not by price.</p>
    ${r.dogs.map(d => `<div class="dog${d.clears ? "" : " under"}">
      <span class="team">${d.team}</span>
      <span class="price mono">${amer(d.price)}</span>
      <span class="conv">${pct(d.conversion)}</span>
      ${d.suspect ? `<span class="tag">suspect</span>`
        : d.clears ? "" : `<span class="tag">below floor</span>`}
    </div>`).join("")}
  </section>`);

  // Line shopping only says something once a second book has prices in it.
  const shop = (r.shop || []).filter(s => s.points_valid && Math.abs(s.points) >= 10);
  if (shop.length) {
    const nBooks = (r.priced_books || []).length;
    out.push(`<section class="rep">
      <h2>${nBooks < 2 ? `${r.book} vs consensus` : `line shopping — ${nBooks} books`}</h2>
      ${nBooks < 2 ? `<p class="muted">Only ${r.book} has prices this week, so there is nothing
        to shop yet — this is that book against the schedule's own number.</p>` : ""}
      ${shop.map(s => `<div class="dog">
        <span class="team">${s.team}</span>
        <span class="price mono">${amer(s.best)}</span>
        <span class="conv ${s.points > 0 ? "good" : "bad"}">${s.points > 0 ? "+" : ""}${s.points}</span>
      </div>`).join("")}
      <p class="muted">Points against the consensus number. A large negative gap
      is a worse price than the market's, and worth a second look before taking it.</p>
    </section>`);
  }

  if (r.notes && r.notes.length) {
    out.push(`<section class="rep warn"><h2>unreadable cells</h2>
      ${r.notes.map(n => `<div class="muted">${n}</div>`).join("")}</section>`);
  }

  // The calculator is a manual tool, not part of the algorithmic answer above
  // it, so it goes last: an arbitrary prop or SGP the board never priced.
  out.push(calcPanel());

  el.report.innerHTML = out.join("");
  wireCalcPanel();
}

// ---- the calculator: a prop hit rate, or a multi-leg wager, typed by hand --
//
// Everything above this point is the board's own algorithmic answer, built
// from prices synced onto it. This is the escape hatch for a prop, an SGP, or
// a cross-game parlay the board never carries a price for -- the same two
// questions `edgectl hitrate` and `edgectl parlay` answer on a terminal,
// reachable here because that is where the game log or the book's leg prices
// actually are.

function calcPanel() {
  return `<section class="rep calc" id="calcPanel">
    <h2>calculator</h2>
    <p class="muted">Price a prop or a multi-leg wager by hand -- the same math
      as <span class="mono">edgectl hitrate</span> / <span class="mono">edgectl parlay</span>.</p>

    <details open>
      <summary>hit rate</summary>
      <div class="fundform">
        <input id="c-values" placeholder="game log: 48,55,60,51,52,49">
        <input id="c-line" inputmode="decimal" placeholder="line 52.5">
        <select id="c-side"><option value="over">over</option><option value="under">under</option></select>
        <input id="c-price" inputmode="tel" placeholder="price (opt) -110">
        <button type="button" id="c-hitrate-go">calculate</button>
      </div>
      <div id="c-hitrate-out" class="out"></div>
    </details>

    <details>
      <summary>multi-leg wager</summary>
      <p class="muted">Tag every leg with the game/event it belongs to. Legs in
        DIFFERENT games combine automatically; two or more legs sharing a game
        tag are refused -- enter the book's own combined price for that SGP
        group as the overall price below instead.</p>
      <div id="c-legs"></div>
      <button type="button" id="c-leg-add">+ add leg</button>
      <div id="c-parlay-combine" class="fundform">
        <button type="button" id="c-parlay-go">combine cross-game legs</button>
      </div>
      <div id="c-parlay-out" class="out"></div>
      <div class="fundform">
        <input id="c-parlay-sel" placeholder="overall selection, e.g. SGP: A + B">
        <input id="c-parlay-price" inputmode="tel" placeholder="overall price">
        <input id="c-parlay-stake" inputmode="decimal" placeholder="stake 0.50">
        <select id="c-parlay-bank"><option value="bonus bet">bonus bet</option><option value="real money">real money</option><option value="no-sweat">no-sweat</option></select>
        <select id="c-parlay-book">${["— log only", "fanatics", "draftkings", "fanduel", "bet365", "betmgm", "caesars"].map(b => `<option>${b}</option>`).join("")}</select>
        <input id="c-parlay-week" inputmode="numeric" value="${(state && state.week) || 1}" title="NFL week">
        <button type="button" id="c-parlay-log">log this wager</button>
      </div>
    </details>
  </section>`;
}

// legRow builds one leg's markup: a selection, a price and a game tag, plus a
// remove button. Two legs are seeded so there is always enough to combine.
function legRow() {
  const div = document.createElement("div");
  div.className = "fundform legrow";
  div.innerHTML = `<input class="c-leg-sel" placeholder="selection">
    <input class="c-leg-price" inputmode="tel" placeholder="price +150">
    <input class="c-leg-game" placeholder="game tag">
    <button type="button" class="rm" title="remove leg">×</button>`;
  return div;
}

function wireCalcPanel() {
  const panel = document.getElementById("calcPanel");
  if (!panel) return;

  // -- hit rate --
  document.getElementById("c-hitrate-go").addEventListener("click", async () => {
    const out = document.getElementById("c-hitrate-out");
    const values = document.getElementById("c-values").value.trim();
    const lineStr = document.getElementById("c-line").value.trim();
    if (!values) { out.innerHTML = `<p class="bad">a hit rate needs a game log</p>`; return; }
    if (lineStr === "") { out.innerHTML = `<p class="bad">enter a line</p>`; return; }
    const line = Number(lineStr);
    if (!Number.isFinite(line)) { out.innerHTML = `<p class="bad">line is not a number</p>`; return; }
    const price = Number(document.getElementById("c-price").value) || 0;
    out.innerHTML = `<p class="muted">working…</p>`;
    try {
      const res = await fetch(BASE + "api/hitrate", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ values, line, side: document.getElementById("c-side").value, price }),
      });
      const r = await res.json();
      if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
      const verdictClass = r.verdict === "supported" ? "good" : r.verdict === "reject" ? "bad" : "warn";
      out.innerHTML = `<p><b>${r.hits} of ${r.n}</b> games${r.pushes ? ` (${r.pushes} push${r.pushes === 1 ? "" : "es"} excluded)` : ""}
          <span class="mono">${pct(r.rate)}</span></p>
        <p class="muted">${Math.round(r.confidence * 100)}% interval <span class="mono">${pct(r.lower)} – ${pct(r.upper)}</span></p>
        ${r.verdict ? `<p class="tag ${verdictClass}">${r.verdict.toUpperCase()} — hurdle ${pct(r.breakeven)}</p>` : ""}
        ${r.n < 20 ? `<p class="muted">${r.n} games is a small sample; widen the window before treating this as a probability.</p>` : ""}`;
    } catch (e) {
      out.innerHTML = `<p class="bad">${e.message}</p>`;
    }
  });

  // -- multi-leg wager --
  const legsBox = document.getElementById("c-legs");
  legsBox.appendChild(legRow());
  legsBox.appendChild(legRow());
  document.getElementById("c-leg-add").addEventListener("click", () => legsBox.appendChild(legRow()));
  legsBox.addEventListener("click", (e) => {
    if (!e.target.closest(".rm")) return;
    if (legsBox.children.length <= 2) return; // a parlay needs at least 2 to combine
    e.target.closest(".legrow").remove();
  });

  function readLegs() {
    return [...legsBox.querySelectorAll(".legrow")].map((row) => ({
      selection: row.querySelector(".c-leg-sel").value.trim(),
      price: Number(row.querySelector(".c-leg-price").value),
      game: row.querySelector(".c-leg-game").value.trim(),
    }));
  }

  document.getElementById("c-parlay-go").addEventListener("click", async () => {
    const out = document.getElementById("c-parlay-out");
    const legs = readLegs();
    if (legs.some((l) => !l.selection || !l.price || !l.game)) {
      out.innerHTML = `<p class="bad">every leg needs a selection, a price and a game tag</p>`;
      return;
    }
    out.innerHTML = `<p class="muted">working…</p>`;
    try {
      const res = await fetch(BASE + "api/parlay/combine", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ legs }),
      });
      const r = await res.json();
      if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
      out.innerHTML = `<p><b class="mono">${amer(r.price)}</b> combined
          <span class="muted">${pct(r.implied)} implied · decimal ${r.decimal.toFixed(3)}</span></p>
        <p class="muted">All legs are cross-game and independent -- this is the product of their
          fair decimals, not a book quote.</p>`;
      // The combine result seeds the overall price/selection, but both stay
      // editable: the operator may still want to type the book's own number.
      document.getElementById("c-parlay-price").value = String(r.price);
      const selField = document.getElementById("c-parlay-sel");
      if (!selField.value.trim()) selField.value = legs.map((l) => l.selection).join(" + ");
    } catch (e) {
      // A same-game refusal is not a failure of the tool -- it is the
      // correlation firewall working. Say so, and point at the fix: the
      // overall price field below, typed from the book's own SGP quote.
      const correlated = /correlated/.test(e.message);
      out.innerHTML = `<p class="bad">${e.message}</p>${correlated
        ? `<p class="muted">Enter the book's own combined price for that group in
             "overall price" below instead of combining it here.</p>` : ""}`;
    }
  });

  document.getElementById("c-parlay-log").addEventListener("click", async () => {
    const btn = document.getElementById("c-parlay-log");
    const legs = readLegs().filter((l) => l.selection || l.price);
    const sel = document.getElementById("c-parlay-sel").value.trim();
    const price = Number(document.getElementById("c-parlay-price").value);
    const stake = Number(document.getElementById("c-parlay-stake").value);
    if (!sel) { alert("what's the overall selection?"); return; }
    if (!price || Math.abs(price) < 100) { alert("overall price, as American odds, e.g. +240 or -150"); return; }
    if (!stake || stake <= 0) { alert("stake?"); return; }
    let book = document.getElementById("c-parlay-book").value;
    if (book.startsWith("—")) book = "";
    btn.disabled = true;
    try {
      const res = await fetch(BASE + "api/place", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          selection: sel, price, stake,
          bankroll: document.getElementById("c-parlay-bank").value,
          book, week: Number(document.getElementById("c-parlay-week").value) || 0,
          narrative: "Entered from the bets-tab calculator.",
          legs: legs.map((l) => ({ selection: l.selection, price: l.price })),
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
      document.getElementById("c-parlay-out").innerHTML = `<p class="good">logged.</p>`;
    } catch (e) {
      alert("not logged: " + e.message);
    } finally {
      btn.disabled = false;
    }
  });
}

async function loadLog() {
  el.betlog.innerHTML = `<p class="muted">reading the log…</p>`;
  try {
    const res = await fetch(BASE + "api/log?week=" + encodeURIComponent(state.week));
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    renderLog(r);
  } catch (e) {
    el.betlog.innerHTML = `<p class="muted">could not read the log: ${e.message}</p>`;
  }
}

// betEntryForm is the free-form bet entry: a prop, a single, anything. It posts
// to the same /api/place the board's record button uses, so a hand-typed prop is
// recorded exactly like a board-placed parlay. With a book the stake is drawn
// from the ledger (bonus bet -> bonus, real money -> cash); "log only" records
// the bet without touching the bankroll, for a book not tracked there.
function betEntryForm() {
  const books = ["— log only", "fanatics", "draftkings", "fanduel", "bet365", "betmgm", "caesars"];
  const wk = (state && state.week) || 1;
  return `<section class="rep">
    <h2>enter a bet</h2>
    <div class="fundform">
      <input id="e-sel" placeholder="selection, e.g. Marvin Harrison Jr. ATD">
      <input id="e-price" inputmode="tel" placeholder="odds +240">
      <input id="e-stake" inputmode="decimal" placeholder="stake 0.50">
      <input id="e-pred" inputmode="decimal" placeholder="win % (opt)" title="your win-probability belief, e.g. 55">
      <select id="e-bank"><option value="bonus bet">bonus bet</option><option value="real money">real money</option><option value="no-sweat">no-sweat</option></select>
      <select id="e-book">${books.map(b => `<option>${b}</option>`).join("")}</select>
      <select id="e-boost"><option value="">— no boost</option></select>
      <input id="e-week" inputmode="numeric" value="${wk}" title="NFL week">
      <label class="chk"><input type="checkbox" id="e-deposit"> deposit stake to fund it</label>
      <button type="button" id="e-add">log bet</button>
    </div>
    <p class="muted">Any single or prop. With a book the stake is drawn from it
    (bonus bet → bonus, real money → cash); "log only" records the bet without
    touching the bankroll. Tick <em>deposit stake to fund it</em> to add a matching
    deposit of that asset to the book first, so the bet logs against fresh funds
    (your existing balance is left untouched).</p>
  </section>`;
}

function wireBetEntry() {
  const btn = document.getElementById("e-add");
  if (!btn) return;
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
  btn.addEventListener("click", async () => {
    const sel = document.getElementById("e-sel").value.trim();
    const price = Number(document.getElementById("e-price").value);
    const stake = Number(document.getElementById("e-stake").value);
    if (!sel) { alert("what's the selection?"); return; }
    if (!price || Math.abs(price) < 100) { alert("odds as American, e.g. +240 or -150"); return; }
    if (!stake || stake <= 0) { alert("stake?"); return; }
    let book = document.getElementById("e-book").value;
    if (book.startsWith("—")) book = "";
    // Belief is optional and entered as a percent (55) or a fraction (0.55);
    // normalize to [0,1]. Blank leaves it 0, which just omits the EV column.
    let predicted = Number(document.getElementById("e-pred").value) || 0;
    if (predicted > 1) predicted /= 100;
    btn.disabled = true;
    try {
      const res = await fetch(BASE + "api/place", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          selection: sel, price, stake, predicted,
          bankroll: document.getElementById("e-bank").value,
          book, week: Number(document.getElementById("e-week").value) || 0,
          boost: (document.getElementById("e-boost") || {}).value || "",
          deposit: !!(document.getElementById("e-deposit") || {}).checked && !!book,
          narrative: "Entered from the log tab.",
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
      loadLog();
      loadBoostOptions(); // a consumed boost should drop off the dropdown
    } catch (e) {
      btn.disabled = false;
      alert("not logged: " + e.message);
    }
  });
}

function renderLog(r) {
  if (!r.entries.length) {
    el.betlog.innerHTML = betEntryForm() + `<section class="rep"><h2>no bets ${r.week ? "for week " + r.week : "recorded"}</h2>
      <p class="muted">Nothing ${r.week ? "logged for week " + r.week : "in " + r.path} yet. Enter one above, or place a wager from
      the bets tab.</p></section>`;
    wireBetEntry();
    return;
  }
  // EV is only meaningful when a belief was recorded; with predicted 0 it just
  // reads back minus the stake. "To win" is the deterministic potential profit
  // on what is still open, and is always shown.
  const hasPred = r.entries.some(e => e.result === "open" && e.predicted > 0);
  el.betlog.innerHTML = betEntryForm() + `
    <div class="scope">${r.week ? "week " + r.week + " · " : ""}${r.count} recorded · ${Math.round(r.open)} open ·
      ${money(r.open_staked_cash ?? r.open_staked ?? r.staked)} cash · ${money(r.open_staked_bonus ?? 0)} bonus at risk · <b>${money(r.open_payout ?? 0)}</b> to win${hasPred ? ` · ${money(r.open_ev ?? r.ev)} expected` : ""}
      · ${money(r.realized ?? 0)} realized</div>
    <section class="rep">
      ${r.entries.map(e => `<div class="logrow ${e.result}" data-id="${e.id}">
        <div class="sel">${e.selection}</div>
        <div class="meta">
          <span class="mono">${e.price > 0 ? "+" : ""}${e.price}</span>
          <span class="muted">${money(e.stake)} ${e.bankroll}</span>
          <span class="muted">to win ${money(e.payout ?? 0)}</span>
          ${e.predicted > 0 ? `<span class="muted">pred ${(e.predicted * 100).toFixed(1)}%</span>` : ""}
          <span class="res">${e.result}</span>
          <span class="when muted">${e.placed}</span>
        </div>
        ${e.result === "open" ? `<div class="settle">
          ${["won", "lost", "push", "void"].map(x =>
            `<button type="button" data-res="${x}" data-id="${e.id}">${x}</button>`).join("")}
          <button type="button" class="repriced" data-id="${e.id}" title="settle at a price the book recomputed, e.g. an SGP leg voided by an injury">repriced…</button>
        </div>` : ""}
      </div>`).join("")}
    </section>
    <section class="rep">
      <p class="muted">Predictions are recorded before the outcome and settled by
      appending, never by rewriting — so nothing here can be re-predicted after the
      fact. Settle with <span class="mono">edgectl log settle</span>, score with
      <span class="mono">edgectl log score</span>.</p>
    </section>`;
  wireBetEntry();
}

// Recording sends the numbers the report DISPLAYED, not a reference to the
// board. A cell holds only the latest price and re-entering prices is what the
// board is for, so anything that recomputed later would let a price update
// rewrite what was predicted -- the hindsight the log exists to prevent.
let lastReport = null;

el.report.addEventListener("click", async (e) => {
  const btn = e.target.closest(".rec");
  if (!btn || !lastReport) return;
  const p = lastReport.set[Number(btn.dataset.i)];
  if (!p) return;

  // Default to the stake this row of the frontier implies, not a hardcoded
  // figure: the deployment decision was already made above.
  const al = (lastReport.alloc || []).find((a) => a.book === p.book);
  const suggested = al && al.stake ? al.stake.toFixed(2) : "12.50";
  const stake = Number(prompt("Stake for " + p.teams.join(" + ") + "?", suggested));
  if (!stake || stake <= 0) return;

  // The week is confirmed explicitly at submit, defaulting to the board's
  // current week. It is what the period report attributes the bet by, so a bet
  // logged early or graded late still lands in the week it was struck for.
  const week = Number(prompt("NFL week this bet is FOR?", lastReport.week)) || lastReport.week;

  btn.disabled = true;
  btn.textContent = "recording…";
  try {
    const res = await fetch(BASE + "api/place", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        selection: p.teams.join(" + ") + " (Week " + lastReport.week + ")",
        price: p.price, stake: stake, predicted: p.true_prob,
        bankroll: "bonus bet", book: p.book, week: week,
        narrative: "Placed from the board at " + lastReport.book + ", week " +
          lastReport.week + ". Conversion " + pct(p.conversion) +
          ". predicted is the product of the de-vigged leg probabilities, not a " +
          "de-vig of the parlay price.",
      }),
    });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
    btn.textContent = "recorded";
    btn.classList.add("done");
    loadLog(); // refresh the log figures now that a bet was added
  } catch (err) {
    btn.disabled = false;
    btn.textContent = "record";
    alert("not recorded: " + err.message);
  }
});

// Settling appends an outcome; it never edits the prediction. A result that
// could rewrite the number predicted would make the whole log worthless, so
// the only thing these buttons can send is an id, an outcome, and -- for a
// wager the book itself repriced -- what it actually paid.
el.betlog.addEventListener("click", async (e) => {
  const repriced = e.target.closest(".settle button.repriced");
  const btn = repriced || e.target.closest(".settle button");
  if (!btn) return;

  let res, settledPrice = 0, note = "settled from the board";
  if (repriced) {
    // A leg voided out of a parlay (an injury) leaves the book paying a
    // different price than the one quoted at placement. This does not rewrite
    // that original quote -- it records what settlement actually paid, the
    // same thing the CLI's -settled-price already does.
    res = (prompt("Settle as won, lost, push or void?", "won") || "").trim().toLowerCase();
    if (!["won", "lost", "push", "void"].includes(res)) return;
    const priceStr = prompt("Price the book actually recomputed this to (American odds, e.g. +110 or -150):");
    if (priceStr === null) return;
    settledPrice = Number(priceStr.replace(/^\+/, ""));
    if (!settledPrice || !Number.isFinite(settledPrice)) { alert("that doesn't look like an American price"); return; }
    note = "settled from the board (repriced: " + (settledPrice > 0 ? "+" : "") + settledPrice + ")";
  } else {
    res = btn.dataset.res;
    if (!confirm(`Settle as ${res.toUpperCase()}? This cannot be undone from here.`)) return;
  }

  for (const b of btn.parentElement.querySelectorAll("button")) b.disabled = true;
  try {
    const r = await fetch(BASE + "api/settle", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: btn.dataset.id, result: res, settled_price: settledPrice, note: note }),
    });
    const body = await r.json();
    if (!r.ok) throw new Error(body.error || ("HTTP " + r.status));
    loadLog();
  } catch (err) {
    for (const b of btn.parentElement.querySelectorAll("button")) b.disabled = false;
    alert("not settled: " + err.message);
  }
});

// ---- the bankroll -------------------------------------------------------
//
// Funds are declared here rather than on a command line because the board is
// used from a phone, and a bankroll nobody can enter is a bankroll nobody
// keeps -- the ledger sat built and empty for exactly that reason. Declaring
// APPENDS a grant rather than setting a total: "I was given $50" and "I have
// $50 left" are different facts, and only the first can be recorded honestly
// after the event.

// Boosts are inventory, not balance. They are listed by CEILING rather than by
// headline percentage, because a promo page is written to be read the other
// way: a 100% boost capped at a $5 stake is worth less than a 25% boost capped
// at $50, and sorting by the big number puts the useless one on top.
async function loadBoosts() {
  try {
    const res = await fetch(BASE + "api/boosts");
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    return renderBoosts(r);
  } catch (e) {
    return `<section class="rep"><h2>boosts</h2>
      <p class="muted">could not read them: ${e.message}</p></section>`;
  }
}

function renderBoosts(r) {
  const all = r.boosts || [];
  const boosts = all.filter(x => x.kind !== "nosweat");
  const nosweats = all.filter(x => x.kind === "nosweat");
  if (!all.length) {
    return `<section class="rep"><h2>profit boosts</h2>
      <p class="muted">None recorded. Worth entering only the ones worth planning
      around \u2014 a small token on a bet you already want is just ticked in the
      slip.</p></section>`;
  }
  const chase = boosts.filter(x => x.chase);
  const rest = boosts.filter(x => !x.chase);
  const del = (x) => `<button type="button" class="promo-del" data-id="${x.id}" title="delete">\u00d7</button>`;
  const tags = (x) => `${x.restricted ? `<span class="tag mkt">${x.market}</span>` : ""}
      ${x.min_odds ? `<span class="tag when">${x.min_odds > 0 ? "+" : ""}${x.min_odds} or longer</span>` : ""}
      ${x.needs_cash ? `<span class="tag">cash only</span>` : ""}
      ${x.expires ? `<span class="tag when">${x.in_hours < 48
        ? x.in_hours + "h" : Math.round(x.in_hours / 24) + "d"}</span>` : ""}`;
  // The equivalent face value, not just the ceiling. They are the same number,
  // but "a $12 bonus bet" says what the ranking MEANS in a unit already
  // understood, where "$12 max" reads as a cap and explains nothing.
  const row = (x) => `<div class="dog${x.chase ? "" : " under"}">
      <span class="team">${x.book}</span>
      <span class="price">${Math.round(x.percent * 100)}% \u00d7 ${money(x.max_stake)}</span>
      <span class="conv">= a ${money(x.ceiling)} bonus bet</span>
      ${tags(x)}${del(x)}
    </div>`;
  const nsrow = (x) => `<div class="dog">
      <span class="team">${x.book}</span>
      <span class="price">no-sweat \u2014 refunds ${money(x.max_stake)}</span>
      <span class="conv">stake back as bonus on a loss</span>
      ${tags(x)}${del(x)}
    </div>`;

  return `<section class="rep">
    <h2>profit boosts \u2014 worth planning around</h2>
    ${chase.length ? chase.map(row).join("")
      : `<p class="muted">None above the ${money(r.floor)} line.</p>`}
    ${rest.length ? `<h2 style="margin-top:.7rem">below the line</h2>
      ${rest.map(row).join("")}
      <p class="muted">Under ${money(r.floor)} at their best. Not worthless \u2014 apply one
      to a wager you already want \u2014 but not worth building a bet around. A
      market-restricted one still points at a market you would otherwise not price.</p>`
      : ""}
    ${nosweats.length ? `<h2 style="margin-top:.7rem">no-sweat tokens</h2>
      ${nosweats.map(nsrow).join("")}
      <p class="muted">A no-sweat is a refund-on-loss right, not a balance: worth
      P(lose) \u00d7 the refund, and best spent on a longshot where that loss is likely.</p>`
      : ""}
    <p class="muted">A boost pays <b>percent \u00d7 stake \u00d7 (1 \u2212 raw) \u00f7 (1 + hold)</b>
    and a bonus bet pays <b>face \u00d7 (1 \u2212 raw) \u00f7 (1 + hold)</b> \u2014 the same instrument
    when <b>face = percent \u00d7 stake</b>. That is why a 100% boost capped at $5 ranks below a
    30% boost capped at $50, however the promo page prints it, and why a boost belongs where a
    bonus bet belongs: the longest price you will take, on the lowest-vig market.</p>
    <p class="muted">The equivalence hides one thing. A bonus bet risks nothing; a boost needs
    the whole stake at risk, and the cash wager under it is negative EV by the vig.</p>
  </section>`;
}

// ---- the props view -----------------------------------------------------
// Reads whatever DraftKings captures sit in the ingest folder and prices them.
// Nothing is uploaded through the page: the operator drops a .har into the
// folder and hits reload, and the newest file's prices win.

async function loadProps() {
  el.props.innerHTML = `<p class="muted">reading the ingest folder…</p>`;
  try {
    const res = await fetch(BASE + "api/props?week=" + encodeURIComponent(state.week));
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    renderProps(r);
  } catch (e) {
    el.props.innerHTML = `<p class="muted">could not read props: ${e.message}</p>`;
  }
}

function renderProps(r) {
  const groups = r.groups || [];
  const head = `<div class="scope">
    <button type="button" id="props-reload">reload</button>
    <button type="button" id="props-sync">sync to board</button>
    ${r.as_of ? `prices as of ${r.as_of}` : "no captures yet"}${
      r.sources && r.sources.length ? ` · ${r.sources.length} file(s)` : ""}${
      r.note ? ` · ${r.note}` : ""}
    <div class="muted">drop a DraftKings .har into ${r.dir || "the ingest folder"} and reload</div>
  </div>
  <div id="props-sync-box"></div>`;
  // A prop's milestone (100+) is already in its label; only append a separate
  // line when it is not, so an over/under ("Over 249.5") reads right without
  // doubling a "JSN 100+ 100".
  const rows = rs => rs.map(x => `<div class="dog">
      <span class="team">${x.selection}${
        x.line != null && !String(x.selection).includes(String(x.line)) ? " " + x.line : ""}</span>
      <span class="price mono">${x.price > 0 ? "+" : ""}${x.price}</span>
      <span class="conv">${pct(x.implied)} impl${
        x.fair != null ? ` · fair ${pct(x.fair)}`
        : x.boost_be != null ? ` · boost ${pct(x.boost_be)}` : ""}</span>
      <span class="muted">${x.market || ""}</span>
    </div>`).join("");

  // Grouped by game first, then by category within it -- a slate is a run of
  // games, and finding one prop used to mean scanning every category's rows
  // for a name. Category order (Passing, Rushing, ...) still comes from the
  // backend; game order is first-seen, which is kickoff order because that is
  // the order captures tend to be dropped in.
  const events = new Map(); // event -> Map(category -> rows[])
  const eventOrder = [];
  for (const g of groups) {
    for (const row of g.rows) {
      const ev = row.event || "(event unknown)";
      if (!events.has(ev)) { events.set(ev, new Map()); eventOrder.push(ev); }
      const cats = events.get(ev);
      if (!cats.has(g.category)) cats.set(g.category, []);
      cats.get(g.category).push(row);
    }
  }

  const gameSections = eventOrder.map(ev => `
    <details class="rep game" open>
      <summary>${ev}</summary>
      ${[...events.get(ev).entries()].map(([cat, rs]) =>
        `<h3>${cat}</h3>${rows(rs)}`).join("")}
    </details>`).join("");

  el.props.innerHTML = head + (eventOrder.length ? gameSections
    : `<section class="rep"><p class="muted">Nothing to price yet. Save a DraftKings capture
        (.har or its JSON) into the ingest folder and hit reload.</p></section>`);

  const reload = document.getElementById("props-reload");
  if (reload) reload.addEventListener("click", loadProps);
  const sync = document.getElementById("props-sync");
  if (sync) sync.addEventListener("click", propsSyncPreview);
}

// ---- props -> board sync -------------------------------------------------
//
// The bets tab reads only the week YAML, so a HAR-derived price never counted
// until it was retyped by hand. This closes that gap the same confirm-first
// way the desktop paste-import flow does: preview the diff, then apply it --
// nothing is written on a single tap.

async function propsSyncPreview() {
  const box = document.getElementById("props-sync-box");
  box.innerHTML = `<p class="muted">checking the board…</p>`;
  try {
    const res = await fetch(BASE + "api/props/sync/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ week: state.week }),
    });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
    if (!body.changes || !body.changes.length) {
      box.innerHTML = `<p class="muted">nothing to sync: the ${body.book} prices on the board already
        match the ingest folder's captures.</p>`;
      return;
    }
    box.innerHTML = `
      <div id="props-sync-diff">${body.changes.map(c =>
        `<div class="add">${c.game_id}  ${c.away} @ ${c.home}  ${c.market}  ${c.old || "(empty)"} → ${c.new}</div>`
      ).join("")}</div>
      <button type="button" id="props-sync-apply">write ${body.changes.length} price(s) to the board</button>`;
    document.getElementById("props-sync-apply").addEventListener("click", propsSyncApply);
  } catch (e) {
    box.innerHTML = `<div class="bad">${e.message}</div>`;
  }
}

async function propsSyncApply() {
  const box = document.getElementById("props-sync-box");
  try {
    const res = await fetch(BASE + "api/props/sync/apply", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ week: state.week }),
    });
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
    box.innerHTML = `<div class="add">wrote ${body.applied} price(s) to the board.</div>`;
  } catch (e) {
    box.innerHTML = `<div class="bad">not applied: ${e.message}</div>`;
  }
}

// ---- the period view ----------------------------------------------------
// Funds show what is held right now; a weekly zero-out to the bank makes that a
// poor record of how a week went. This view sums the week's flows instead:
// money in and out of the bank, realized P&L on what settled, what was staked,
// and what is still open \u2014 the same ledger replayed over a window, not a
// snapshot (see ledger.Period). The window for a week comes from the schedule.

async function loadPeriod() {
  el.period.innerHTML = `<p class="muted">summing the week\u2026</p>`;
  const wk = state.periodWeek ? `?week=${state.periodWeek}` : "";
  try {
    const res = await fetch(BASE + "api/period" + wk);
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || res.status);
    state.periodWeek = r.week;
    save();
    renderPeriod(r);
  } catch (e) {
    el.period.innerHTML = `<p class="muted">could not read the period: ${e.message}</p>`;
  }
}

function renderPeriod(r) {
  const opts = (r.weeks || []).map(w =>
    `<option value="${w}"${w === r.week ? " selected" : ""}>Week ${w}</option>`).join("");
  const head = `<div class="scope">
    <select id="period-week">${opts}</select>
    <span class="muted">${r.start} \u2192 ${r.end}</span>
  </div>`;

  const line = (label, val) =>
    `<div class="dog"><span class="team">${label}</span>` +
    `<span class="price mono">${money(val)}</span></div>`;

  const gap = r.net_to_bank - r.realized_net;
  el.period.innerHTML = head + `
    <section class="rep">
      <h2>capital \u2014 real money moved with your bank</h2>
      ${line("deposited", r.deposits)}
      ${line("withdrawn", r.withdrawals)}
      ${line("net to bank", r.net_to_bank)}
    </section>
    <section class="rep">
      <h2>betting \u2014 realized on this week's wagers (graded)</h2>
      ${line("cash", r.realized_cash)}
      ${line("bonus won", r.realized_bonus)}
      ${line("realized net", r.realized_net)}
    </section>
    <section class="rep">
      <h2>staked \u2014 this week's wagers</h2>
      ${line("cash", r.staked_cash)}
      ${line("bonus", r.staked_bonus)}
    </section>
    <section class="rep">
      <h2>still open \u2014 this week's wagers not yet graded</h2>
      ${line("cash", r.open_staked_cash)}
      ${line("bonus", r.open_staked_bonus)}
    </section>
    <section class="rep">
      <h2>reconcile</h2>
      <p class="muted">net to bank ${money(r.net_to_bank)} vs realized net ${money(r.realized_net)}
      (gap ${money(gap)}). open stake and parked bonus explain a gap; anything
      past that is a mis-logged event worth finding \u2014 the log replays, so it is
      findable.</p>
    </section>`;

  const sel = document.getElementById("period-week");
  if (sel) sel.addEventListener("change", () => {
    state.periodWeek = Number(sel.value);
    save();
    loadPeriod();
  });
}

async function loadFunds() {
  el.funds.innerHTML = `<p class="muted">reading boosts\u2026</p>`;
  try {
    const res = await fetch(BASE + "api/funds");
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    renderFunds(r);
  } catch (e) {
    el.funds.innerHTML = `<p class="muted">could not read boosts: ${e.message}</p>`;
  }
}

function renderFunds(r) {
  const books = (data && data.books) || ["fanatics"];
  const exp = r.expiring || [];
  // Only cash is zeroed here: it is the part of a balance a Tuesday actually
  // withdraws to the bank. Bonus/boost lots are not withdrawable money and
  // already have their own expiry handling.
  const cash = (r.balances || []).filter(b => b.asset === "cash" && b.amount > 0.005);

  el.funds.innerHTML = `
    ${exp.length ? `<section class="rep warn">
      <h2>expiring</h2>
      ${exp.map(e => `<div class="dog${e.expired ? " under" : ""}">
        <span class="team">${e.book}</span>
        <span class="price">${e.label}</span>
        <span class="conv">${e.expired ? "EXPIRED"
          : e.in_hours < 48 ? `${e.in_hours}h left` : `${Math.round(e.in_hours / 24)}d left`}</span>
      </div>`).join("")}
      <p class="muted">Every meaningful loss last campaign was a deadline, not a bad
      price. This is the part of a bankroll worth looking at.</p>
    </section>` : ""}

    <section class="rep">
      <h2>cash balances</h2>
      ${cash.length ? cash.map(b => `<div class="dog">
        <span class="team">${b.book}</span>
        <span class="price">${money(b.amount)}</span>
        <button type="button" class="zero-book" data-book="${b.book}" data-amount="${b.amount}">zero out</button>
      </div>`).join("") : `<p class="muted">no book is holding cash right now.</p>`}
      <p class="muted">Zeroing out withdraws a book's cash to the bank — the same
      kind of event <code>edgectl ledger add -kind withdraw</code> records, so
      it lands in the period report as a withdrawal, not a loss. Use it Tuesday
      morning once last week's cash is pulled out, to start the new week at $0.</p>
    </section>

    <div id="boostbox"></div>

    <p class="muted">Bonus balances and full ledger detail live in <code>edgectl
    ledger balances</code>. This view holds cash balances above, plus the
    boosts and no-sweat tokens on hand below.</p>

    <section class="rep">
      <h2>declare a boost</h2>
      <div class="fundform">
        <select id="b-book">${books.map(x => `<option>${x}</option>`).join("")}</select>
        <input id="b-pct" inputmode="decimal" placeholder="50 (%)">
        <input id="b-max" inputmode="decimal" placeholder="max stake 25">
        <input id="b-min" inputmode="tel" placeholder="min line -200">
        <select id="b-mkt"><option value="any">any market</option><option value="sgp">SGP</option></select>
        <input id="b-exp" placeholder="expires (optional; default week ${state.week}'s end)">
        <label class="chk"><input type="checkbox" id="b-cash"> needs cash</label>
        <button type="button" id="b-add">add</button>
      </div>
      <p class="muted">A boost is a unit, not a balance: spent whole, and never
      counted as money you can wager. Leave expires blank to auto-expire at the
      end of week ${state.week} (the banner's week); type a date only for one
      that outlives this week.</p>
    </section>

    <section class="rep">
      <h2>declare a no-sweat token</h2>
      <div class="fundform">
        <select id="n-book">${books.map(x => `<option>${x}</option>`).join("")}</select>
        <input id="n-max" inputmode="decimal" placeholder="refunds up to 10">
        <select id="n-mkt"><option value="any">any market</option><option value="sgp">SGP</option><option value="atd">ATD</option><option value="ftd">FTD</option></select>
        <input id="n-exp" placeholder="expires (optional; default week ${state.week}'s end)">
        <button type="button" id="n-add">add</button>
      </div>
      <p class="muted">A no-sweat refunds your stake as a bonus if the bet loses —
      a contingent right, not money. Enter the stake it covers. Leave expires
      blank to auto-expire at the end of week ${state.week}.</p>
    </section>`;

  // Zeroing withdraws the full displayed balance to a target of 0. Reusing
  // /api/funds/adjust (rather than a bespoke endpoint) means this goes through
  // the exact same "correct the balance to what it should be" path a typo
  // fix would use -- draws the newest lots first, appends a withdraw event per
  // lot, and the period report already treats a withdraw as a withdraw.
  for (const btn of el.funds.querySelectorAll(".zero-book")) {
    btn.addEventListener("click", async () => {
      const book = btn.dataset.book;
      const amt = money(Number(btn.dataset.amount));
      if (!confirm(`Zero out ${book}'s cash (${amt})? This withdraws it to the bank and cannot be undone from here.`)) return;
      btn.disabled = true;
      try {
        const res = await fetch(BASE + "api/funds/adjust", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ book: book, asset: "cash", target: 0, note: "Tuesday zero-out to the bank" }),
        });
        const body = await res.json();
        if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
        loadFunds();
      } catch (e) {
        btn.disabled = false;
        alert("not zeroed: " + e.message);
      }
    });
  }

  loadBoosts().then((html) => {
    const box = document.getElementById("boostbox");
    if (!box) return;
    box.innerHTML = html;
    // Deleting a promo records an expire event (the ledger is append-only, so a
    // delete is a death). Confirmed, because it cannot be un-clicked cleanly.
    for (const btn of box.querySelectorAll(".promo-del")) {
      btn.addEventListener("click", async () => {
        if (!confirm("Delete this promo? It is recorded as expired (removed).")) return;
        btn.disabled = true;
        try {
          const res = await fetch(BASE + "api/funds/expire", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ id: btn.dataset.id }),
          });
          const body = await res.json();
          if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
          loadFunds();
        } catch (e) {
          btn.disabled = false;
          alert("not deleted: " + e.message);
        }
      });
    }
  });

  const badd = document.getElementById("b-add");
  if (badd) badd.addEventListener("click", async () => {
    // Percent is entered as a whole number because that is how a promo states
    // it, and divided here. Asking for 0.5 invites 50, which is a hundredfold
    // error that looks like a legitimate entry.
    const pct100 = Number(document.getElementById("b-pct").value);
    const max = Number(document.getElementById("b-max").value);
    if (!pct100 || pct100 <= 0) { alert("percent? e.g. 50"); return; }
    if (!max || max <= 0) { alert("max stake? a boost with no cap cannot be valued"); return; }
    badd.disabled = true;
    try {
      const res = await fetch(BASE + "api/boosts", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          book: document.getElementById("b-book").value,
          percent: pct100 / 100,
          max_stake: max,
          market: document.getElementById("b-mkt").value.trim() || "any",
          min_odds: Number(document.getElementById("b-min").value) || 0,
          expires: document.getElementById("b-exp").value.trim(),
          // Blank expires + the banner's week auto-expires this at week's end;
          // an explicit expires date above still overrides it.
          week: state.week,
          needs_cash: document.getElementById("b-cash").checked,
          label: Math.round(pct100) + "% boost",
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
      if (!body.chase) {
        alert(`Recorded, but its ceiling is ${money(body.ceiling)} \u2014 below the line ` +
              `worth building a bet around. Apply it to something you already want.`);
      }
      loadFunds();
    } catch (e) {
      badd.disabled = false;
      alert("not recorded: " + e.message);
    }
  });

  const nadd = document.getElementById("n-add");
  if (nadd) nadd.addEventListener("click", async () => {
    const max = Number(document.getElementById("n-max").value);
    if (!max || max <= 0) { alert("how much does it refund? e.g. 10"); return; }
    nadd.disabled = true;
    try {
      const res = await fetch(BASE + "api/boosts", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          kind: "nosweat",
          book: document.getElementById("n-book").value,
          max_stake: max,
          market: document.getElementById("n-mkt").value,
          expires: document.getElementById("n-exp").value.trim(),
          week: state.week,
          label: "no-sweat",
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
      loadFunds();
    } catch (e) {
      nadd.disabled = false;
      alert("not recorded: " + e.message);
    }
  });
}

function syncView() {
  const v = state.view;
  el.report.hidden = v !== "bets";
  el.betlog.hidden = v !== "log";
  el.props.hidden = v !== "props";
  el.funds.hidden = v !== "funds";
  el.period.hidden = v !== "period";
  el.beliefs.hidden = v !== "beliefs";
  el.help.hidden = v !== "help";
  for (const b of el.views.querySelectorAll("button")) {
    b.classList.toggle("on", b.dataset.view === v);
  }
  if (v === "bets") loadReport();
  if (v === "log") loadLog();
  if (v === "props") loadProps();
  if (v === "funds") loadFunds();
  if (v === "period") loadPeriod();
  if (v === "beliefs") loadBeliefs();
}

// Toggling a book re-runs the report over the new pool. Emptying it falls back
// to the entry book rather than sending nothing, because an empty pool is a
// question with no answer, not a request for every book.
el.report.addEventListener("click", (e) => {
  const chip = e.target.closest(".chip");
  if (!chip) return;
  const b = chip.dataset.book;
  const cur = new Set(state.books && state.books.length ? state.books : [state.book]);
  if (cur.has(b)) cur.delete(b); else cur.add(b);
  state.books = cur.size ? [...cur] : [state.book];
  save();
  loadReport();
});

// Choosing a frontier row re-runs the report at that split.
el.report.addEventListener("click", (e) => {
  const row = e.target.closest(".frow");
  if (!row) return;
  state.shots = Number(row.dataset.shots);
  save();
  loadReport();
});

el.views.addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b) return;
  state.view = b.dataset.view;
  save();
  syncView();
});

// ---- selectors ----------------------------------------------------------

el.week.addEventListener("change", () => {
  state.week = Number(el.week.value); save(); refresh();
  if (state.view === "log") loadLog(); // the log is scoped to the selected week
  if (state.view === "props") loadProps(); // props captures are scoped by filename week
});


function fillSelect(sel, values, current, label) {
  sel.innerHTML = "";
  for (const v of values) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = label ? label(v) : v;
    sel.appendChild(o);
  }
  sel.value = current;
  if (sel.value !== String(current) && values.length) sel.value = values[0];
  return sel.value;
}

// ---- paste import (desktop) --------------------------------------------

el.pasteToggle.addEventListener("click", () => {
  el.pasteBody.hidden = !el.pasteBody.hidden;
});

async function importCall(path) {
  const res = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ week: state.week, book: state.book, market: "ml", blob: el.blob.value }),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
  return body;
}

el.preview.addEventListener("click", async () => {
  el.apply.disabled = true;
  try {
    const body = await importCall("api/import/preview");
    if (!body.changes || !body.changes.length) {
      el.diff.textContent = "nothing to change: these prices are already on the board.";
      return;
    }
    el.diff.innerHTML = body.changes.map((c) =>
      `<div class="add">${c.game_id}  ${c.away} @ ${c.home}  ${c.old || "(empty)"} → ${c.new}</div>`
    ).join("");
    // Confirmation is not optional: a blob that shifted by one entry produces
    // a diff that is entirely plausible until you read the team names.
    el.apply.disabled = false;
  } catch (e) {
    el.diff.innerHTML = `<div class="bad">${e.message}</div>`;
  }
});

el.apply.addEventListener("click", async () => {
  el.apply.disabled = true;
  try {
    const body = await importCall("api/import/apply");
    el.diff.innerHTML = `<div class="add">wrote ${body.applied} price(s).</div>`;
    el.blob.value = "";
    refresh();
  } catch (e) {
    el.diff.innerHTML = `<div class="bad">${e.message}</div>`;
  }
});

// ---- load ---------------------------------------------------------------

async function refresh(retried) {
  try {
    const res = await fetch(BASE + "api/board?week=" + encodeURIComponent(state.week));
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
    data = body;
    el.banner.hidden = true;
    syncView();

    state.week = fillSelect(el.week, data.weeks, data.week, (w) => "Week " + w) * 1;
    // state.book is pinned to draftkings (see load); there is no book selector
    // to populate. data.books still carries every book for the report's chip
    // pool.
    save();
  } catch (e) {
    // A remembered week whose file has since been removed would otherwise
    // leave the page with no week selector to escape from. Fall back once.
    if (!retried && state.week !== 1) {
      state.week = 1;
      save();
      return refresh(true);
    }
    el.banner.hidden = false;
    el.banner.textContent = "could not load the board: " + e.message;
  }
}

// ---- beliefs view: the belief-probe weekly loop in the browser ----------

function bpEsc(s) {
  const d = document.createElement("div");
  d.textContent = s == null ? "" : String(s);
  return d.innerHTML;
}

function bpNum(x, dp = 4) {
  return (typeof x === "number" && isFinite(x)) ? x.toFixed(dp) : "—";
}

async function loadBeliefs() {
  el.beliefs.innerHTML = `
    <section class="bp">
      <div class="bp-head">
        <h2>belief probe — week <span id="bpWeek">${state.week}</span></h2>
        <span class="muted">pack generation stays <code>make belief-pack</code>; this reads it</span>
      </div>
      <div id="bpPrompt" class="bp-panel"><p class="muted">loading the pack…</p></div>
      <div class="bp-panel">
        <h3>paste the model's forecast</h3>
        <textarea id="bpBlob" rows="6" spellcheck="false" placeholder='{"predictions":[ … ]}'></textarea>
        <div class="bp-actions">
          <button type="button" id="bpPreview">preview</button>
          <button type="button" id="bpApply" disabled>apply to log</button>
          <label class="muted"><input type="checkbox" id="bpPartial"> allow a partial file</label>
        </div>
        <div id="bpDiff"></div>
      </div>
      <div class="bp-panel">
        <div class="bp-head"><h3>score</h3><button type="button" id="bpScoreBtn">load score</button></div>
        <div id="bpScore"><p class="muted">scores settled weeks; run after games finish.</p></div>
      </div>
    </section>`;

  const promptBox = document.getElementById("bpPrompt");
  try {
    const res = await fetch(BASE + "api/beliefs/pack?week=" + encodeURIComponent(state.week));
    const p = await res.json();
    if (!res.ok) throw new Error(p.error || ("HTTP " + res.status));
    bpRenderPrompt(promptBox, p);
  } catch (e) {
    promptBox.innerHTML = `<p class="muted">no pack for week ${state.week}: ${bpEsc(e.message)}</p>`;
  }

  document.getElementById("bpPreview").addEventListener("click", () => bpIngest(false));
  document.getElementById("bpApply").addEventListener("click", () => bpIngest(true));
  document.getElementById("bpScoreBtn").addEventListener("click", bpLoadScore);
}

function bpRenderPrompt(box, p) {
  const rows = (p.games || []).map(g => `<tr>
    <td>${bpEsc(g.game_id)}</td><td>${bpEsc(g.away)}</td><td>${bpEsc(g.home)}</td>
    <td>${g.total_line ?? "—"}</td><td>${g.spread_line ?? "—"}</td></tr>`).join("");
  box.innerHTML = `
    <div class="bp-head">
      <h3>the pasteable prompt <span class="muted">sha ${bpEsc((p.sha || "").slice(0, 12))}</span></h3>
      <button type="button" id="bpCopy">copy prompt</button>
    </div>
    <pre id="bpPromptText" class="bp-prompt">${bpEsc(p.prompt)}</pre>
    <h3>slate</h3>
    <table class="bp-table"><thead><tr><th>game</th><th>away</th><th>home</th><th>total</th><th>spread</th></tr></thead>
      <tbody>${rows}</tbody></table>`;
  document.getElementById("bpCopy").addEventListener("click", async (ev) => {
    try {
      await navigator.clipboard.writeText(p.prompt);
      ev.target.textContent = "copied";
      setTimeout(() => (ev.target.textContent = "copy prompt"), 1200);
    } catch { ev.target.textContent = "copy failed"; }
  });
}

async function bpIngest(apply) {
  const diff = document.getElementById("bpDiff");
  const blob = document.getElementById("bpBlob").value.trim();
  if (!blob) { diff.innerHTML = `<p class="muted">paste a forecast first.</p>`; return; }
  diff.innerHTML = `<p class="muted">checking the gates…</p>`;
  try {
    const res = await fetch(BASE + "api/beliefs/ingest", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        week: state.week, blob, apply,
        partial: document.getElementById("bpPartial").checked,
      }),
    });
    const r = await res.json();
    if (!res.ok) {
      diff.innerHTML = `<p class="bp-refuse">refused: ${bpEsc(r.error)}</p>`;
      document.getElementById("bpApply").disabled = true;
      return;
    }
    bpRenderIngest(diff, r.result, r.applied);
    // Enable apply only after a clean preview; disable again once applied.
    document.getElementById("bpApply").disabled = apply;
  } catch (e) {
    diff.innerHTML = `<p class="bp-refuse">could not ingest: ${bpEsc(e.message)}</p>`;
  }
}

function bpRenderIngest(box, res, applied) {
  const t = res.falsifier || {};
  const late = (res.late || []).length, missing = (res.missing || []).length;
  box.innerHTML = `
    <div class="bp-ingest">
      <p><strong>${applied ? "applied" : "preview"}</strong> — ${res.ready} ${applied ? "written" : "ready to write"}.</p>
      <ul>
        <li>claims: ${t.checked || 0} checked, ${t.unverifiable || 0} unverifiable, ${t.untyped || 0} untyped, ${t.deferred || 0} deferred</li>
        ${t.falsified ? `<li class="bp-refuse">${t.falsified} falsified — contradict the pack, excluded from the edge score</li>` : ""}
        ${res.only_narrative ? `<li>${res.only_narrative} rest only on unverifiable claims</li>` : ""}
        ${late ? `<li>${late} after kickoff</li>` : ""}
        ${missing ? `<li class="bp-refuse">${missing} rows missing — accepted under partial</li>` : ""}
      </ul>
    </div>`;
}

async function bpLoadScore() {
  const box = document.getElementById("bpScore");
  box.innerHTML = `<p class="muted">scoring…</p>`;
  try {
    const res = await fetch(BASE + "api/beliefs/score?from=1&to=18&vs=auto");
    const r = await res.json();
    if (!res.ok) throw new Error(r.error || ("HTTP " + res.status));
    bpRenderScore(box, r);
  } catch (e) {
    box.innerHTML = `<p class="muted">could not score: ${bpEsc(e.message)}</p>`;
  }
}

function bpRenderScore(box, s) {
  if (s.notice) { box.innerHTML = `<p class="muted">${bpEsc(s.notice)}</p>`; return; }
  const v = s.verdict || {};
  let verdict = "not yet decidable", cls = "muted";
  if (v.e1_decidable && v.e1_pass) { verdict = "PASS — beats every opponent"; cls = "bp-pass"; }
  else if (v.e1_decidable) { verdict = "FAIL — loses to " + bpEsc(v.binding_opponent); cls = "bp-refuse"; }

  const refRows = (s.by_reference || []).map(row => `<tr>
    <td>${bpEsc(row.name)}${row.binding ? "" : " (floor)"}</td>
    <td>${row.n}</td>
    <td>${row.gain_undefined ? "—" : bpNum(row.gain, 5)}</td>
    <td>[${bpNum(row.lo, 4)}, ${bpNum(row.hi, 4)}]</td></tr>`).join("");

  const e2 = v.e2_wagers > 0
    ? `E2 diagnostic: expected ROI ${bpNum(v.e2_edge, 4)} [${bpNum(v.e2_lo, 4)}, ${bpNum(v.e2_hi, 4)}] on ${v.e2_wagers} implied wagers <span class="muted">— a robustness check, not independent of E1</span>`
    : `E2 diagnostic: no wagers implied yet`;

  box.innerHTML = `
    <div class="bp-verdict ${cls}">VERDICT — ${verdict}</div>
    <p class="muted">the decision is E1 accuracy on ${(v.decision_scenarios || []).join(" and ")}, vs the hardest opponent.</p>
    <h3>by reference <span class="muted">(each opponent on its own rows; one-sided 5%)</span></h3>
    <table class="bp-table"><thead><tr><th>opponent</th><th>n</th><th>gain</th><th>95% one-sided</th></tr></thead>
      <tbody>${refRows || `<tr><td colspan="4" class="muted">no reference rows yet</td></tr>`}</tbody></table>
    <p>${e2}</p>
    <p class="muted">Brier ${bpNum(s.calib && s.calib.Brier)} · reliability ${bpNum(s.calib && s.calib.Reliability)} · resolution ${bpNum(s.calib && s.calib.Resolution)} · ${s.positions} positions, ${s.abstained} abstained</p>`;
}

refresh();

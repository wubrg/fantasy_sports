// Draft board. One screen, one job: when a player is nominated, show what
// he costs, what he is worth, and the most you should pay.
//
// The server owns every number. This file filters and draws — it must never
// compute a price, or the page and the terminal would eventually disagree.

// Matched to the server's own Sleeper poll: it can't have newer data than
// that, and /api/scratch/view is served from the cached snapshot, so asking
// this often costs no Sleeper traffic at all.
const POLL_MS = 2000;

// The positions the board filters on, in lineup order rather than
// alphabetical: mid-auction the toggles are found by their place in the
// row, not by reading them.
const POSITIONS = ["QB", "RB", "WR", "TE"];

// A repaint budget, not a filter. 446 rows re-rendered every two seconds
// is wasted work nobody scrolls to — but a cap that hides rows silently is
// worse than no cap, so drawRows says on screen whenever it binds.
const ROW_CAP = 120;

let snap = null;
let scratch = null;
let filter = "";
let affordableOnly = false;

// Filter state lives here, not in the DOM, because the poll rebuilds the
// table every two seconds and would otherwise reset what you asked for
// mid-bid.
let positions = new Set();
let aboveReplacementOnly = false;

// Player IDs currently on the scratch roster, so board rows can show it.
function scratchIDs() {
  if (!scratch) return new Set();
  return new Set([...(scratch.starters || []), ...(scratch.bench || [])].map(s => s.playerId));
}

async function fetchJSON(url, options) {
  const res = await fetch(url, options);
  if (!res.ok) throw new Error(`${url}: ${res.status} ${await res.text()}`);
  return res.json();
}

function money(n) { return `$${n}`; }

function signed(n) {
  const s = n > 0 ? `+${n}` : `${n}`;
  return `<span class="${n > 0 ? "good" : n < 0 ? "bad" : ""}">${s}</span>`;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, c => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]
  ));
}

// Flags carry meaning, so they get colour rather than being dumped as text.
// The lean cycle, matching leanCycle in leanedit.go. Clicking steps through
// it, so setting one and undoing it are the same gesture — which is what you
// want with a few seconds on the clock.
const LEAN_CYCLE = ["", "must", "up", "down", "dnd"];

function nextLean(current) {
  const i = LEAN_CYCLE.indexOf(current || "");
  return LEAN_CYCLE[(i < 0 ? 0 : i + 1) % LEAN_CYCLE.length];
}

function flagHTML(p) {
  const out = [];
  const lean = (p.Lean && p.Lean.Lean) || "";
  // Always rendered, even when unset, so every row has the same control in
  // the same place. An empty one is a dot you can click rather than a gap
  // you have to know about.
  const label = { must: "MUST", dnd: "DND", up: "+", down: "-" }[lean] || "·";
  const cls = { must: "flag must", dnd: "flag dnd" }[lean] || "flag";
  out.push(`<span class="${cls} lean-set" data-lean="${esc(p.Name)}"` +
    ` data-next="${nextLean(lean)}" title="click: ${nextLean(lean) || "no read"}">${label}</span>`);

  // Blocked leads the info flags: he is off your board because your own
  // roster already spent that offense, which changes the decision more than
  // anything else here.
  if (p.BlockedReason) {
    out.push(`<span class="flag blocked" title="off your board — ${esc(p.BlockedReason)}">blocked: ${esc(p.BlockedReason)}</span>`);
  }

  // Naming the dissenting set, not just flagging dissent: you want to know
  // which read to go and check before the bidding starts.
  for (const by of (p.Lean && p.Lean.contestedBy) || []) {
    out.push(`<span class="flag vs" title="${esc(by)}">vs ${esc(by.split(" ")[0])}</span>`);
  }

  for (const t of p.Traits || []) {
    out.push(`<span class="flag trait trait-${esc(t)}">${esc(t)}</span>`);
  }

  if (p.ECR === "contested") out.push(`<span class="flag split">split</span>`);
  else if (p.ECR === "upside") out.push(`<span class="flag">ecr+</span>`);
  else if (p.ECR === "downside") out.push(`<span class="flag">ecr-</span>`);

  // Sharp-expert divergence: the most-accurate-expert subset against the full
  // field. Distinct from ecr± above, which is the wider industry. Threshold
  // mirrors sharpRankThreshold in signals.go.
  const sharp = p.SharpRankDelta || 0;
  if (sharp <= -5) out.push(`<span class="flag sharp-up" title="the most-accurate experts rank him ${-sharp} spots higher than consensus">sharp+</span>`);
  else if (sharp >= 5) out.push(`<span class="flag sharp-down" title="the most-accurate experts rank him ${sharp} spots lower than consensus">sharp-</span>`);

  if (p.Availability) {
    out.push(`<span class="flag hurt">${esc(p.Availability.toLowerCase())}</span>`);
  }
  const spread = baselineSpread(p);
  if (spread >= 10) out.push(`<span class="flag">swing $${spread.toFixed(0)}</span>`);
  return out.join("");
}

function baselineSpread(p) {
  const vals = Object.values(p.VBD || {});
  if (!vals.length) return 0;
  return Math.max(...vals) - Math.min(...vals);
}

function adjustedEdge(p) {
  const bias = (snap.bias || {})[p.Position] || 0;
  return (p.Value - p.Cost) - bias;
}

function draw() {
  if (!snap) return;
  const me = snap.me;

  document.getElementById("budget").textContent = money(me.Budget);
  document.getElementById("slots").textContent = me.OpenSlots;
  document.getElementById("maxbid").textContent = money(snap.maxBid);

  const ceiling = document.getElementById("ceiling");
  ceiling.textContent = money(snap.recommended);
  ceiling.title = snap.risk ? `${snap.risk.Band}: $${Math.round(snap.risk.PerStarter)} per remaining starter vs the league's $${Math.round(snap.risk.LeaguePerStarter)}` : "";
  ceiling.className = "value " + (snap.risk && (snap.risk.Band === "risky" || snap.risk.Band === "dangerous") ? "bad" : "");

  const needs = Object.entries(me.StartersNeeded || {})
    .filter(([, n]) => n > 0)
    .map(([pos, n]) => `${n} ${pos}`).join(", ");
  document.getElementById("needs").textContent = needs || "lineup full";
  document.getElementById("pool").textContent =
    `$${snap.dollars} over ${snap.slots} slots · ${snap.baseline}`;

  drawPivot();
  drawMustHaves();
  drawRows();
  drawMini("bias", Object.entries(snap.bias || {})
    .sort((a, b) => b[1] - a[1])
    .map(([pos, v]) => [pos, signed(Math.round(v))]));
  drawMini("scarcity", Object.entries(snap.scarcity || {})
    .sort((a, b) => a[1].TopScarcityPct - b[1].TopScarcityPct)
    .map(([pos, s]) => {
      // Startable against starting spots: below 1.0 the room is about to
      // start replacement level at the position.
      const cover = s.Cover ? s.Cover.toFixed(2) + "x" : "—";
      const thin = s.Cover > 0 && s.Cover < 1 ? " bad" : "";
      // Your effective count: the same tally over your board, once the
      // offenses you already own a piece of are off it. Shown only where it
      // differs from the room's, so the column stays quiet until it bites.
      const eff = (snap.effectiveScarcity || {})[pos];
      const yours = eff && eff.Startable !== s.Startable
        ? ` · <span class="yours" title="startable on your board, after your one-per-offense / no-handcuff preferences">${eff.Startable} yours</span>`
        : "";
      return [pos, `<span class="${thin.trim()}">${s.Startable} startable · ${cover}</span>${yours}`];
    }));
  drawPriceLines();
  drawSold();
  drawScratch();

  document.getElementById("status").textContent =
    `${snap.players.length} available · updated ${new Date().toLocaleTimeString()}` +
    (snap.warnings && snap.warnings.length ? ` · ${snap.warnings.join("; ")}` : "");
}

function drawPivot() {
  const el = document.getElementById("pivot");
  if (!snap.hasPivot) { el.classList.add("hidden"); return; }
  el.classList.remove("hidden");
  el.innerHTML = `<span class="name">${esc(snap.pivot.Name)}</span>${esc(snap.pivot.Reason)}`;
}

function drawMustHaves() {
  const el = document.getElementById("musthaves");
  const m = snap.mustHaves;
  if (!m || !m.Players || !m.Players.length) { el.classList.add("hidden"); return; }
  el.classList.remove("hidden");
  const names = m.Players.map(p => `${esc(p.Player)} $${p.Cap}`).join(", ");
  const perSlot = m.SlotsLeft > 0 ? (m.Remaining / m.SlotsLeft) : 0;
  const thin = m.SlotsLeft > 0 && perSlot < 2;
  el.classList.toggle("thin", thin);
  el.textContent = `must-haves: ${names} = $${m.Committed}, leaving $${m.Remaining}` +
    ` for ${m.SlotsLeft} slots (~$${perSlot.toFixed(0)} each)` +
    (thin ? "  — too thin to field a roster" : "");
}

// Above the bar the server drew. scarcity carries the same threshold the
// Startable count was taken at, so a row that survives this is a row that
// number counted — the sidebar and the board cannot disagree.
function aboveReplacement(p) {
  const s = (snap.scarcity || {})[p.Position];
  // No summary for the position means no bar to judge him against. Showing
  // him is the safe failure: hiding a player the server said nothing about
  // is how you miss a nomination.
  if (!s) return true;
  return p.CielyPoints >= s.Threshold;
}

function anyFilter() {
  return positions.size > 0 || filter !== "" || affordableOnly || aboveReplacementOnly;
}

function drawRows() {
  const tbody = document.getElementById("rows");
  const needle = filter.toLowerCase();

  // Filter first, cap second. The cap is a repaint budget, so it has to
  // land on what you asked for rather than on the first 120 of everything.
  const matched = snap.players.filter(p => {
    if (positions.size && !positions.has(p.Position)) return false;
    // Name only: the position toggles are the discoverable way to do what
    // typing "RB" used to, and they can express two positions at once.
    if (needle && !p.Name.toLowerCase().includes(needle)) return false;
    if (affordableOnly && p.MyMaxBid > snap.maxBid) return false;
    if (aboveReplacementOnly && !aboveReplacement(p)) return false;
    return true;
  });
  const rows = matched.slice(0, ROW_CAP);
  drawCounts(matched, rows.length);

  const inScratch = scratchIDs();
  tbody.innerHTML = rows.map(p => {
    const lean = (p.Lean && p.Lean.Lean) || "";
    const tooRich = p.MyMaxBid > snap.maxBid;
    let myMax;
    if (lean === "dnd") myMax = `<span class="mymax dnd">—</span>`;
    else if (tooRich) myMax = `<span class="mymax bad">$${snap.maxBid}!</span>`;
    else myMax = `<span class="mymax${p.BidRule === "must-have" ? " must" : ""}">$${p.MyMaxBid}</span>`;

    const trying = inScratch.has(p.PlayerID);
    return `<tr class="${tooRich ? "unaffordable" : ""} ${trying ? "in-scratch" : ""} ${p.BlockedReason ? "blocked" : ""}">
      <td>${esc(p.Name)}</td>
      <td class="pos pos-${esc(p.Position)}">${esc(p.Position)}</td>
      <td class="num ecr" title="FantasyPros expert-consensus positional rank">${p.ECRRank ? esc(p.Position) + p.ECRRank : "—"}</td>
      <td class="num">${p.Cost ? money(p.Cost) : "—"}</td>
      <td class="num">${money(p.Value)}</td>
      <td class="num fp" title="FantasyPros second projection, re-solved into dollars against the same pool">${p.FPValue ? money(p.FPValue) : "—"}</td>
      <td class="num">${p.Cost ? signed(p.Value - p.Cost) : "—"}</td>
      <td class="num">${p.Cost ? signed(Math.round(adjustedEdge(p))) : "—"}</td>
      <td class="num">${myMax}</td>
      <td class="num">${p.ScarcityPct ? Math.round(p.ScarcityPct) + "%" : "—"}</td>
      <td>${flagHTML(p)}</td>
      <td class="sell">
        <button class="mine" data-player="${esc(p.Name)}" data-mine="1">me</button>
        <button data-player="${esc(p.Name)}" data-mine="0">them</button>
      </td>
      <td><button class="try" data-try="${esc(p.Name)}" data-in="${trying ? 1 : 0}"
          title="${trying ? "remove from the scratch roster" : "try him on the scratch roster"}"
          >${trying ? "\u2212" : "+"}</button></td>
    </tr>`;
  }).join("");
}

// What you are looking at, always. The replacement bar is aggressive
// enough to empty a position you still have to start, and the cap can bite
// on top of it, so a hidden row is only acceptable while the board is
// saying how many it hid.
function drawCounts(matched, drawn) {
  const parts = [];
  if (anyFilter()) {
    const inView = POSITIONS.filter(pos => !positions.size || positions.has(pos));
    // The running total only earns its space when more than one position
    // is in view; with one selected the per-position line already says it.
    if (inView.length > 1) {
      parts.push(`${matched.length} of ${snap.players.length}`);
    }
    for (const pos of inView) {
      const total = snap.players.filter(p => p.Position === pos).length;
      if (!total) continue;
      const shown = matched.filter(p => p.Position === pos).length;
      parts.push(`<span class="pos-${pos}">${pos}</span> ` +
        `<span class="${shown === 0 ? "bad" : ""}">${shown}</span>/${total}`);
    }
  }
  if (drawn < matched.length) {
    parts.push(`<span class="trunc">showing first ${drawn} of ${matched.length}</span>`);
  }
  document.getElementById("hint").innerHTML = parts.join(" · ");
}

function drawPosFilter() {
  document.getElementById("posfilter").innerHTML = POSITIONS.map(pos =>
    `<button class="pospill pos-${pos}${positions.has(pos) ? " on" : ""}"` +
    ` data-pos="${pos}">${pos}</button>`).join("");
}

function drawMini(id, pairs) {
  document.getElementById(id).innerHTML = pairs
    .map(([k, v]) => `<tr><td>${esc(k)}</td><td class="num">${v}</td></tr>`)
    .join("");
}

// What a bid buys, by position. Its own renderer rather than drawMini,
// which only does two columns.
//
// The historical figure sits under the live one only where the two differ
// enough to change a decision. Printing both on every cell doubles the ink
// for rows where they agree, and the whole point of the panel is to be
// readable in the second before you bid.
function drawPriceLines() {
  const lines = snap.priceLines || {};
  const order = ["QB", "RB", "WR", "TE"];
  const tiers = (lines.RB || lines.WR || {}).tiers || [];

  const head = `<tr><td></td>` +
    tiers.map(t => `<td class="num">top-${t}</td>`).join("") + `</tr>`;

  const rows = order.filter(pos => lines[pos]).map(pos => {
    const l = lines[pos];
    const cells = l.live.map((v, i) => {
      const past = (l.history || [])[i] || 0;
      // Two conditions, because either alone gets it wrong. A ratio with no
      // floor shouted about a $5-vs-$3 quarterback line; dividing by the
      // live figure made the test lopsided, so $30-vs-$40 showed while
      // $40-vs-$30 hid the same ten dollars. It was hiding the largest
      // divergence on the board — an $11 gap at the top-five receiver line.
      const gap = Math.abs(past - v);
      const differs = past > 0 && v > 0 &&
        gap >= 5 && gap / Math.max(past, v) >= 0.20;
      const note = differs ? `<span class="was">${money(past)}</span>` : "";
      return `<td class="num">${v ? money(v) : "—"}${note}</td>`;
    }).join("");
    return `<tr><td class="pos-${esc(pos)}">${esc(pos)}</td>${cells}</tr>`;
  }).join("");

  document.getElementById("pricelines").innerHTML = head + rows;
}

function drawSold() {
  const el = document.getElementById("soldlist");
  const entries = Object.entries(snap.__sold || {});
  if (!entries.length) { el.innerHTML = `<li class="empty">none yet</li>`; return; }
  el.innerHTML = entries.map(([name, price]) =>
    `<li>${esc(name)}<span class="price">${price ? "$" + price : "them"}</span></li>`).join("");
}

function drawScratch() {
  if (!scratch) return;
  const m = scratch.metrics || {};
  document.getElementById("s-spent").textContent = `$${m.Spend || 0}`;
  document.getElementById("s-left").textContent = `$${scratch.budgetLeft}`;
  document.getElementById("s-slots").textContent = scratch.slotsLeft;
  document.getElementById("s-popr").textContent = Math.round(m.POPR || 0);

  // A roster you cannot afford is the one thing worth shouting about.
  document.getElementById("s-left").className =
    "v" + (scratch.budgetLeft < 0 ? " bad" : "");

  const rows = [];
  for (const s of scratch.starters || []) {
    // A keeper is already yours: no price to imagine, nothing to remove.
    if (s.kept) {
      rows.push(`<tr class="kept"><td class="slot">${esc(s.slot)}</td>` +
        `<td>${esc(s.name)} <span class="tag">kept</span></td>` +
        `<td class="price">$${s.price}</td><td></td></tr>`);
      continue;
    }
    rows.push(`<tr><td class="slot">${esc(s.slot)}</td><td>${esc(s.name)}</td>` +
      `<td class="price" data-reprice="${esc(s.name)}" title="click to change the price">$${s.price}</td>` +
      `<td class="drop" data-drop="${esc(s.name)}" title="remove">&times;</td></tr>`);
  }
  for (const slot of scratch.unfilled || []) {
    rows.push(`<tr class="hole"><td class="slot">${esc(slot)}</td>` +
      `<td colspan="3">empty</td></tr>`);
  }
  for (const s of scratch.bench || []) {
    rows.push(`<tr class="bench"><td class="slot">BN</td><td>${esc(s.name)}</td>` +
      `<td class="price" data-reprice="${esc(s.name)}" title="click to change the price">$${s.price}</td>` +
      `<td class="drop" data-drop="${esc(s.name)}" title="remove">&times;</td></tr>`);
  }
  document.getElementById("s-lineup").innerHTML = rows.join("");

  // What the lineup is made of. This is what the roster shapes were for,
  // read off the roster you are building instead of one a search invented.
  const mix = Object.entries(scratch.traits || {})
    .map(([t, n]) => `<span class="flag trait trait-${esc(t)}">${n} ${esc(t)}</span>`)
    .join(" ");
  document.getElementById("s-mix").innerHTML = mix;

  // Nothing to clear means nothing to click. The button clears the players
  // you are trying on, not the keepers, so with only keepers on the panel a
  // live button would visibly do nothing — which is exactly how a working
  // one gets reported as broken.
  document.getElementById("s-clear").disabled = !!scratch.empty;

  const note = document.getElementById("s-note");
  if (scratch.empty) {
    note.textContent = "Your keepers are already here. Click + on a row to try a player alongside them; nothing on this panel touches the live board.";
  } else {
    const bits = [`${m.MyGuys || 0} of your guys`];
    if (m.Injured) bits.push(`${m.Injured} carrying an injury designation`);
    if (m.DNDViolations) bits.push(`${m.DNDViolations} do-not-draft!`);
    if ((scratch.dropped || []).length) {
      bits.push(`dropped (drafted for real): ${scratch.dropped.join(", ")}`);
    }
    note.textContent = bits.join(" \u00b7 ");
  }
}

// setLean records a personal read. The response is the rebuilt board, so
// MY MAX and the must-have line move with it in the same paint.
async function setLean(player, lean) {
  snap = await fetchJSON("api/lean", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ player, lean }),
  });
  draw();
}

async function scratchAction(action, player, price) {
  const res = await fetchJSON("api/scratch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action, player, price: price || 0 }),
  });
  snap = res.board;
  scratch = res.scratch;
  draw();
}

// ---- recording sales -------------------------------------------------
//
// You know a sale closed before the API does, so this is the fast path.
// Your own buys debit the budget immediately; everyone else's just leave
// the board.

async function recordSale(player, mine) {
  let price = 0;
  if (mine) {
    const answer = prompt(`What did you pay for ${player}?`);
    if (answer === null) return;
    price = parseInt(answer, 10);
    if (!Number.isFinite(price) || price < 1) return;
  }
  const sold = snap.__sold || {};
  sold[player] = price;
  snap = await fetchJSON("api/sold", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ player, price, mine }),
  });
  snap.__sold = sold;
  draw();
}

document.addEventListener("click", async ev => {
  const btn = ev.target.closest("button[data-player]");
  if (btn) {
    await recordSale(btn.dataset.player, btn.dataset.mine === "1");
    return;
  }
  const tryBtn = ev.target.closest("button[data-try]");
  if (tryBtn) {
    const name = tryBtn.dataset.try;
    await scratchAction(tryBtn.dataset.in === "1" ? "remove" : "add", name);
    return;
  }
  const leanEl = ev.target.closest("[data-lean]");
  if (leanEl) {
    await setLean(leanEl.dataset.lean, leanEl.dataset.next);
    return;
  }
  const reprice = ev.target.closest("[data-reprice]");
  if (reprice) {
    const name = reprice.dataset.reprice;
    const now = prompt(`What do you think ${name} goes for?`, reprice.textContent.replace("$", ""));
    if (now !== null && now.trim() !== "") {
      await scratchAction("add", name, Math.max(1, parseInt(now, 10) || 1));
    }
    return;
  }
  const drop = ev.target.closest("[data-drop]");
  if (drop) {
    await scratchAction("remove", drop.dataset.drop);
    return;
  }
  if (ev.target.id === "s-clear") {
    await scratchAction("clear", "");
    return;
  }
  if (ev.target.id === "reloadleans") {
    const button = ev.target;
    button.disabled = true;
    try {
      // What each player went for lives only on this page. Replacing the
      // snapshot without carrying it forward would wipe the record of the
      // draft so far, which reloading a lean file has no business doing.
      const sold = snap && snap.__sold;
      snap = await fetchJSON("api/leans/reload", { method: "POST", body: "{}" });
      snap.__sold = sold || {};
      draw();
    } catch (err) {
      // The reads on the board are still the ones that loaded, so say what
      // happened rather than leaving the click looking like it worked.
      document.getElementById("status").textContent = String(err);
    } finally {
      button.disabled = false;
    }
    return;
  }
  if (ev.target.id === "undoall") {
    snap = await fetchJSON("api/undo", { method: "POST", body: "{}" });
    snap.__sold = {};
    draw();
  }
});

// ---- input -----------------------------------------------------------

document.getElementById("search").addEventListener("input", ev => {
  filter = ev.target.value.trim();
  drawRows();
});

document.getElementById("affordable").addEventListener("change", ev => {
  affordableOnly = ev.target.checked;
  drawRows();
});

document.getElementById("above").addEventListener("change", ev => {
  aboveReplacementOnly = ev.target.checked;
  drawRows();
});

// Multi-select: deciding between a back and a receiver is the normal case,
// and a single needle could never show both lists at once.
document.getElementById("posfilter").addEventListener("click", ev => {
  const pill = ev.target.closest("button[data-pos]");
  if (!pill) return;
  const pos = pill.dataset.pos;
  if (positions.has(pos)) positions.delete(pos); else positions.add(pos);
  drawPosFilter();
  drawRows();
});

document.addEventListener("keydown", ev => {
  const search = document.getElementById("search");
  if (ev.key === "/" && document.activeElement !== search) {
    ev.preventDefault();
    search.focus();
    search.select();
  } else if (ev.key === "Escape") {
    // Everything, not just the text. Escape is the one key you hit when a
    // name is called and the board is showing the wrong slice of players.
    search.value = "";
    filter = "";
    positions.clear();
    affordableOnly = false;
    aboveReplacementOnly = false;
    document.getElementById("affordable").checked = false;
    document.getElementById("above").checked = false;
    search.blur();
    drawPosFilter();
    drawRows();
  }
});

drawPosFilter();

// ---- polling ---------------------------------------------------------

async function refresh() {
  try {
    const sold = snap && snap.__sold;
    const res = await fetchJSON("api/scratch/view");
    snap = res.board;
    scratch = res.scratch;
    snap.__sold = sold || {};
    draw();
  } catch (err) {
    document.getElementById("status").textContent = `error: ${err.message}`;
  }
}

refresh();
setInterval(refresh, POLL_MS);

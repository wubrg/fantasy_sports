// Draft board. One screen, one job: when a player is nominated, show what
// he costs, what he is worth, and the most you should pay.
//
// The server owns every number. This file filters and draws — it must never
// compute a price, or the page and the terminal would eventually disagree.

// Matched to the server's own Sleeper poll: it can't have newer data than
// that, and /api/scratch/view is served from the cached snapshot, so asking
// this often costs no Sleeper traffic at all.
const POLL_MS = 2000;

let snap = null;
let scratch = null;
let filter = "";
let affordableOnly = false;

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
function flagHTML(p) {
  const out = [];
  const lean = p.Lean && p.Lean.Lean;
  if (lean === "must") out.push(`<span class="flag must">MUST</span>`);
  if (lean === "dnd") out.push(`<span class="flag dnd">DND</span>`);
  if (lean === "up") out.push(`<span class="flag">+</span>`);
  if (lean === "down") out.push(`<span class="flag">-</span>`);

  // Naming the dissenting set, not just flagging dissent: you want to know
  // which read to go and check before the bidding starts.
  for (const by of (p.Lean && p.Lean.contestedBy) || []) {
    out.push(`<span class="flag vs" title="${esc(by)}">vs ${esc(by.split(" ")[0])}</span>`);
  }

  if (p.ECR === "contested") out.push(`<span class="flag split">split</span>`);
  else if (p.ECR === "upside") out.push(`<span class="flag">ecr+</span>`);
  else if (p.ECR === "downside") out.push(`<span class="flag">ecr-</span>`);

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
      return [pos, `<span class="${thin.trim()}">${s.Startable} startable · ${cover}</span>`];
    }));
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

function drawRows() {
  const tbody = document.getElementById("rows");
  const needle = filter.toLowerCase();
  const rows = [];

  for (const p of snap.players) {
    if (needle && !p.Name.toLowerCase().includes(needle) &&
        p.Position.toLowerCase() !== needle) continue;
    const tooRich = p.MyMaxBid > snap.maxBid;
    if (affordableOnly && tooRich) continue;
    rows.push(p);
    if (rows.length >= 120) break;
  }

  document.getElementById("hint").textContent =
    needle ? `${rows.length} matching` : "";

  const inScratch = scratchIDs();
  tbody.innerHTML = rows.map(p => {
    const lean = (p.Lean && p.Lean.Lean) || "";
    const tooRich = p.MyMaxBid > snap.maxBid;
    let myMax;
    if (lean === "dnd") myMax = `<span class="mymax dnd">—</span>`;
    else if (tooRich) myMax = `<span class="mymax bad">$${snap.maxBid}!</span>`;
    else myMax = `<span class="mymax${p.BidRule === "must-have" ? " must" : ""}">$${p.MyMaxBid}</span>`;

    const trying = inScratch.has(p.PlayerID);
    return `<tr class="${tooRich ? "unaffordable" : ""} ${trying ? "in-scratch" : ""}">
      <td>${esc(p.Name)}</td>
      <td class="pos pos-${esc(p.Position)}">${esc(p.Position)}</td>
      <td class="num">${p.Cost ? money(p.Cost) : "—"}</td>
      <td class="num">${money(p.Value)}</td>
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

function drawMini(id, pairs) {
  document.getElementById(id).innerHTML = pairs
    .map(([k, v]) => `<tr><td>${esc(k)}</td><td class="num">${v}</td></tr>`)
    .join("");
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

  const note = document.getElementById("s-note");
  if (scratch.empty) {
    note.textContent = "Click + on a row to try a player here. Nothing on this panel touches the live board.";
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

document.addEventListener("keydown", ev => {
  const search = document.getElementById("search");
  if (ev.key === "/" && document.activeElement !== search) {
    ev.preventDefault();
    search.focus();
    search.select();
  } else if (ev.key === "Escape") {
    search.value = "";
    filter = "";
    search.blur();
    drawRows();
  }
});

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

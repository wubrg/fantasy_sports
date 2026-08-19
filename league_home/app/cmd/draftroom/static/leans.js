// Every read you hold, on one page, editable. The board shows a lean as one
// pill on a row you happen to be looking at; this shows the file — grouped the
// way the YAML is grouped, so what you see here and what you would see in an
// editor are the same shape.
//
// The server owns the file. Nothing here is optimistic: an edit posts, and the
// whole set is re-fetched, so the screen shows what was actually written
// rather than what we asked for.

// Every path is relative, no leading slash — behind tailscale serve
// --set-path the mount prefix is stripped before it reaches this app, and an
// absolute path would escape the mount. See the normalizer in leans.html.
const API = "api/leans";

// The order the file itself is in, and so the order of the page. The server
// ships it as `cycle`; this is the fallback if an older build does not.
const ORDER = ["must", "up", "down", "dnd", ""];

const GROUPS = {
  must: { what: "Must-haves", note: "Bid to value plus half his swing, capped by roster safety." },
  up: { what: "Lean up", note: "Worth about 15% more than the model says." },
  down: { what: "Lean down", note: "Worth about 15% less — still biddable at the right price." },
  dnd: { what: "Do not draft", note: "Never bid, whatever the board says he is worth." },
  "": { what: "Favorites, no read yet", note: "Names you kept in front of you and have not ruled on. The work list." },
};

// Pill label per read, matching the board so a MUST looks like a MUST in both
// places. The empty read is a dot: an affordance in the same spot on every row
// rather than a gap you have to know about.
const LABEL = { must: "MUST", up: "+", down: "-", dnd: "DND", "": "·" };
const CLASS = { must: "must", up: "up", down: "down", dnd: "dnd", "": "none" };
const WORD = { must: "must-have", up: "lean up", down: "lean down", dnd: "do not draft", "": "clear the read" };

let data = null;
let names = null;      // board names, fetched once, only for the add box
let query = "";
let busy = false;

function esc(s) {
  return String(s).replace(/[&<>"]/g, c => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]
  ));
}

// The server answers a rejected edit in plain text ("no player named ... on
// the board"), and that sentence is the whole diagnosis — so it is thrown as
// the message rather than buried behind a status code.
async function fetchJSON(url, options) {
  const res = await fetch(url, options);
  const body = await res.text();
  if (!res.ok) throw new Error(body.trim() || `${url}: ${res.status}`);
  return body ? JSON.parse(body) : null;
}

function showError(msg) {
  const el = document.getElementById("err");
  el.textContent = msg;
  el.classList.remove("hidden");
}

function clearError() {
  document.getElementById("err").classList.add("hidden");
}

function cycle() {
  return (data && data.cycle && data.cycle.length) ? data.cycle : ORDER;
}

// ---- loading ---------------------------------------------------------

async function load() {
  data = await fetchJSON(API);
  draw();
}

// Every edit re-reads the set afterwards. A read can move a row between
// groups, drop it out of the file entirely, or be refused outright, and none
// of that is knowable from the request we sent.
async function post(body) {
  if (busy) return;
  busy = true;
  try {
    await fetchJSON("api/lean", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    clearError();
    await load();
  } catch (err) {
    showError(String(err.message || err));
  } finally {
    busy = false;
  }
}

// ---- pieces of a row -------------------------------------------------

function readButtons(player, lean, onBoard) {
  // A name the board does not know cannot be posted at all — the API rejects
  // it — so the control says so instead of offering a click that 400s.
  const off = onBoard === false;
  return cycle().map(v => {
    const on = v === (lean || "") ? " on" : "";
    const title = off
      ? "no player of this name on the board — fix the spelling in the file"
      : WORD[v];
    return `<button class="flag ${CLASS[v]}${on}" data-player="${esc(player)}"` +
      ` data-lean="${esc(v)}" title="${esc(title)}"${off ? " disabled" : ""}>${LABEL[v]}</button>`;
  }).join("");
}

function starButton(player, fav) {
  return `<button class="star${fav ? " on" : ""}" data-fav="${esc(player)}"` +
    ` data-on="${fav ? "1" : "0"}" title="${fav ? "a favorite — click to drop the star" : "keep this name in front of me"}">` +
    `${fav ? "★" : "☆"}</button>`;
}

function signals(r) {
  const out = [];
  for (const a of r.against || []) {
    out.push(`<span class="flag vs" title="${esc(a)}">vs ${esc(a.split(" ")[0])}</span>`);
  }
  if (r.onBoard === false) {
    out.push(`<span class="flag offboard" title="this read reaches no player, so it never fires">no player</span>`);
    if (r.suggestion) {
      out.push(`<button class="flag sugg" data-add="${esc(r.suggestion)}" data-lean="${esc(r.lean || "")}"` +
        ` title="write the same read against the real name — the misspelling stays until you delete it from the file">` +
        `did you mean ${esc(r.suggestion)}?</button>`);
    }
  }
  if (r.cap) out.push(`<span class="flag range" title="hard ceiling from the file — read-only here">cap $${esc(r.cap)}</span>`);
  return out.join("");
}

function rowHTML(r) {
  const pos = r.position || "";
  return `<tr>` +
    `<td class="name">${esc(r.player)}</td>` +
    `<td class="pos pos-${esc(pos)}">${esc(pos)}</td>` +
    `<td class="read">${readButtons(r.player, r.lean, r.onBoard)}</td>` +
    `<td class="fav">${starButton(r.player, !!r.favorite)}</td>` +
    `<td class="src">${esc(r.source || "")}</td>` +
    `<td class="sig">${signals(r)}</td>` +
    `<td class="note">${esc(r.note || "")}</td>` +
    `</tr>`;
}

function table(rows) {
  return `<table>${rows.map(rowHTML).join("")}</table>`;
}

// ---- the page --------------------------------------------------------

function draw() {
  const rows = data.rows || [];
  const sets = data.sets || [];

  const contested = rows.filter(r => (r.against || []).length);
  const offboard = rows.filter(r => r.onBoard === false);
  const favs = rows.filter(r => r.favorite);

  document.getElementById("n-reads").textContent = rows.filter(r => r.lean).length;
  document.getElementById("n-favs").textContent = favs.length;
  document.getElementById("n-contested").textContent = contested.length;
  document.getElementById("n-offboard").textContent = offboard.length;
  document.getElementById("writable").textContent = data.writable || "nowhere — no writable set";

  drawAttention(contested, offboard);
  drawSets(sets);
  drawGroups(rows, sets);

  document.getElementById("status").textContent =
    `${rows.length} names across ${sets.length} set${sets.length === 1 ? "" : "s"} · ` +
    `updated ${new Date().toLocaleTimeString()}`;
}

// The pre-draft triage. Contested reads are a decision you have not made yet;
// unreachable ones are a read you think you hold and do not.
function drawAttention(contested, offboard) {
  const el = document.getElementById("attention");
  if (!contested.length && !offboard.length) { el.classList.add("hidden"); return; }
  el.classList.remove("hidden");

  const parts = [`<h2>Before draft night</h2>`];

  if (offboard.length) {
    parts.push(`<h3>${offboard.length} read${offboard.length === 1 ? "" : "s"} reach nobody</h3>`);
    parts.push(`<p class="note">The name matches no player on the board, so the read never
      fires and nothing on the board ever says so. Fix the spelling in the file
      — the API will not take an edit against a name it cannot find.</p>`);
    parts.push(table(offboard));
  }

  if (contested.length) {
    parts.push(`<h3>${contested.length} read${contested.length === 1 ? "" : "s"} another set argues with</h3>`);
    parts.push(`<p class="note">Yours still wins. Worth knowing which ones you are
      overruling someone on before the bidding, not during it.</p>`);
    parts.push(`<table>${contested.map(r =>
      `<tr>` +
      `<td class="name">${esc(r.player)}</td>` +
      `<td class="pos pos-${esc(r.position || "")}">${esc(r.position || "")}</td>` +
      `<td class="read">${readButtons(r.player, r.lean, r.onBoard)}</td>` +
      `<td class="fav">${starButton(r.player, !!r.favorite)}</td>` +
      `<td>${(r.against || []).map(a => `<span class="flag vs">${esc(a)}</span>`).join("")}</td>` +
      `<td class="note">${esc(r.note || "")}</td>` +
      `</tr>`).join("")}</table>`);
  }

  el.innerHTML = parts.join("");
}

function drawSets(sets) {
  document.getElementById("sets-count").textContent =
    `${sets.length} in precedence order`;
  document.getElementById("sets").innerHTML = sets.map((s, i) => {
    const tags = [];
    if (s.writable) tags.push(`<span class="tag writes">edits land here</span>`);
    if (s.generated) tags.push(`<span class="tag" title="built by a tool — regenerating it discards hand edits">generated</span>`);
    return `<tr>` +
      `<td class="ord">${i + 1}</td>` +
      `<td class="who">${esc(s.name)}${tags.join("")}` +
      `<span class="path">${esc(s.path || "")}</span></td>` +
      `<td class="num">${s.reads} read${s.reads === 1 ? "" : "s"}</td>` +
      `</tr>`;
  }).join("");
}

function drawGroups(rows, sets) {
  const out = [];

  for (const lean of cycle()) {
    // The empty group is not "no read" — it is the favorites you have not
    // ruled on. A row with neither a read nor a star is not in the file.
    const mine = rows.filter(r => (r.lean || "") === lean);
    const g = GROUPS[lean] || { what: lean, note: "" };
    out.push(`<section class="group g-${lean || "none"}">` +
      `<h2><span class="what">${esc(g.what)}</span> <span class="count">${mine.length}</span></h2>` +
      (g.note ? `<p class="note">${g.note}</p>` : "") +
      (mine.length ? table(mine) : `<p class="note">none</p>`) +
      `</section>`);
  }

  // Undecided names are written down in a set but ruled on nowhere — they
  // carry no read at all, so they cannot appear in the groups above.
  for (const s of sets) {
    const u = s.undecided || [];
    if (!u.length) continue;
    out.push(`<section class="group">` +
      `<h2><span class="what">Undecided in ${esc(s.name)}</span> <span class="count">${u.length}</span></h2>` +
      `<p class="note">Written down, never ruled on. Give one a read and it moves up the page.</p>` +
      `<table>${u.map(n =>
        `<tr><td class="name">${esc(n)}</td><td class="pos"></td>` +
        `<td class="read">${readButtons(n, "", true)}</td>` +
        `<td class="fav">${starButton(n, false)}</td>` +
        `<td class="src">${esc(s.name)}</td><td class="sig"></td><td class="note"></td></tr>`
      ).join("")}</table></section>`);
  }

  document.getElementById("groups").innerHTML = out.join("");
}

// ---- adding a player -------------------------------------------------
//
// A read can only be written against a name the board knows, so the add box
// searches the board rather than taking free text — the misspellings in the
// "reaches nobody" list above are exactly what free text produces.

async function ensureNames() {
  if (names) return;
  const res = await fetchJSON("api/scratch/view");
  names = (res.board.players || []).map(p => ({ name: p.Name, pos: p.Position }));
}

function drawAdd() {
  const box = document.getElementById("addresults");
  const hint = document.getElementById("addhint");
  const q = query.trim().toLowerCase();
  if (!q) { box.classList.add("hidden"); hint.textContent = ""; return; }
  if (!names) { hint.textContent = "loading the board…"; return; }

  const held = new Set((data.rows || []).map(r => r.player));
  const hits = names.filter(p => p.name.toLowerCase().includes(q));
  const shown = hits.slice(0, 12);

  hint.textContent = hits.length > shown.length
    ? `${hits.length} match — showing ${shown.length}, keep typing`
    : `${hits.length} match`;

  box.classList.remove("hidden");
  box.innerHTML = shown.length
    ? `<table>${shown.map(p =>
        `<tr><td class="name">${esc(p.name)}</td>` +
        `<td class="pos pos-${esc(p.pos)}">${esc(p.pos)}</td>` +
        `<td class="read">${readButtons(p.name, "", true)}</td>` +
        `<td class="fav">${starButton(p.name, false)}</td>` +
        `<td class="have">${held.has(p.name) ? "already in your file" : ""}</td></tr>`
      ).join("")}</table>`
    : `<p class="note">nobody on the board by that name</p>`;
}

// ---- events ----------------------------------------------------------

document.addEventListener("click", async ev => {
  const read = ev.target.closest("[data-lean]");
  if (read && read.dataset.player) {
    await post({ player: read.dataset.player, lean: read.dataset.lean });
    return;
  }
  // The suggestion button writes the read against the real name. It cannot
  // delete the misspelling — that name is not on the board, so the API has no
  // handle on it — which is why the title says the file still needs an edit.
  const add = ev.target.closest("[data-add]");
  if (add) {
    await post({ player: add.dataset.add, lean: add.dataset.lean });
    return;
  }
  const fav = ev.target.closest("[data-fav]");
  if (fav) {
    // setLean:false, or starring a name would also blank his read.
    await post({ player: fav.dataset.fav, favorite: fav.dataset.on !== "1", setLean: false });
    return;
  }
  if (ev.target.id === "reloadleans") {
    const button = ev.target;
    button.disabled = true;
    try {
      await fetchJSON("api/leans/reload", { method: "POST", body: "{}" });
      clearError();
      await load();
    } catch (err) {
      showError(String(err.message || err));
    } finally {
      button.disabled = false;
    }
  }
});

const addBox = document.getElementById("add");
addBox.addEventListener("input", async () => {
  query = addBox.value;
  drawAdd();
  try {
    await ensureNames();
  } catch (err) {
    showError(`could not read the board to search it: ${err.message || err}`);
    return;
  }
  drawAdd();
});

document.addEventListener("keydown", ev => {
  if (ev.key === "/" && document.activeElement !== addBox) {
    ev.preventDefault();
    addBox.focus();
  }
  if (ev.key === "Escape" && document.activeElement === addBox) {
    addBox.value = "";
    query = "";
    drawAdd();
    addBox.blur();
  }
});

load().catch(err => {
  showError(`could not load your leans: ${err.message || err}`);
  document.getElementById("status").textContent = "not loaded";
});

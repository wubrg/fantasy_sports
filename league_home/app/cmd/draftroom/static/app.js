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

// Sort state, kept here for the same reason as the filters above: the poll
// rebuilds the table every two seconds and would otherwise drop what you
// clicked. null sortKey means the server's own order — most expensive first.
let sortKey = null;
let sortDir = null;

// Research-mode criteria filters. They narrow the board to players who fall
// inside a range or carry a trait, and apply only while a keeper scenario is
// active — a research-only gate so the draft-night board is never quietly
// filtered by a range you set and forgot.
const TRAITS = ["floor", "redzone", "upside", "targets", "bellcow", "discounted", "offense"];
let critAAV = { min: null, max: null };
let critTier = { min: null, max: null };
let critCost = { min: null, max: null };
let critTraits = new Set();
let critTraitMode = "any"; // "any" | "all"

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

  for (const t of p.Traits || []) {
    out.push(`<span class="flag trait trait-${esc(t)}">${esc(t)}</span>`);
  }

  // Signals start here, after the traits. A lean set disagreeing with your read
  // leads them: it is the only one that names a specific read to go and check,
  // where the rest report what the field thinks. It used to sit above the
  // traits, which made it look like one of your own reads rather than an
  // outside opinion.
  for (const by of (p.Lean && p.Lean.contestedBy) || []) {
    out.push(`<span class="flag vs" title="${esc(by)}">vs ${esc(by.split(" ")[0])}</span>`);
  }

  if (p.ECR === "contested") out.push(`<span class="flag split">split</span>`);
  else if (p.ECR === "upside") out.push(`<span class="flag">ecr+</span>`);
  else if (p.ECR === "downside") out.push(`<span class="flag">ecr-</span>`);

  // Sharp-expert divergence: the most-accurate-expert subset against the full
  // field. Distinct from ecr± above, which is the wider industry. Threshold
  // mirrors sharpRankThreshold in signals.go.
  const sharp = p.SharpRankDelta || 0;
  if (sharp >= 15) out.push(`<span class="flag sharp-up" title="the most-accurate experts rank him ${sharp} spots higher than consensus">sharp+</span>`);
  else if (sharp <= -15) out.push(`<span class="flag sharp-down" title="the most-accurate experts rank him ${-sharp} spots lower than consensus">sharp-</span>`);

  // Chris Dell's own read, one trusted expert kept apart from the sharp
  // subset above. Threshold mirrors dellSharpThreshold in leangen.go.
  const dell = p.DellDelta || 0;
  if (dell >= 12) out.push(`<span class="flag sharp-up" title="Chris Dell ranks him ${dell} spots higher than consensus">dell+</span>`);
  else if (dell <= -12) out.push(`<span class="flag sharp-down" title="Chris Dell ranks him ${-dell} spots lower than consensus">dell-</span>`);

  if (p.Availability) {
    out.push(`<span class="flag hurt">${esc(p.Availability.toLowerCase())}</span>`);
  }
  const spread = baselineSpread(p);
  if (spread >= 10) out.push(`<span class="flag">swing $${spread.toFixed(0)}</span>`);

  // FantasyPros is the one source that publishes a high and a low rather than
  // a single number, so it is the only place the board can show what a
  // projection does not know. Distinct from swing above: swing is how much his
  // price moves with where replacement is drawn, this is how little the
  // projection itself commits to.
  if (wideBand(p)) {
    out.push(`<span class="flag range" title="FantasyPros' own high and low for him come out $${p.FPLow}–$${p.FPHigh}` +
      ` against an FP value of $${p.FPValue} — a range wider than the number itself. The projection is not committing.">` +
      `range $${p.FPLow}–$${p.FPHigh}</span>`);
  }
  return out.join("");
}

function baselineSpread(p) {
  const vals = Object.values(p.VBD || {});
  if (!vals.length) return 0;
  return Math.max(...vals) - Math.min(...vals);
}

// fpCellTitle explains the FP number on the row, and where FantasyPros
// published a range, what that number is bracketed by. A dash means they rank
// him but do not project him, which is a different thing from projecting him
// low — worth saying, because the two look identical in an empty cell.
function fpCellTitle(p) {
  if (!p.FPValue) {
    return p.ECRRank
      ? "FantasyPros ranks him but publishes no projection for him, so there is no FP dollar figure — not the same thing as projecting him at nothing."
      : "FantasyPros does not cover him.";
  }
  let s = "FantasyPros' projection, re-solved into dollars against the same pool as Value.";
  if (p.FPLow && p.FPHigh) {
    s += ` Their own low and high for him come out $${p.FPLow}–$${p.FPHigh}.`;
  }
  return s;
}

// A band is worth flagging when it is wide against the player's own value, not
// when it is wide in dollars. Absolute width just finds expensive players:
// Gibbs' band is the narrowest on the board proportionally (29%) and among the
// widest in dollars, so a dollar threshold would flag the single most confident
// projection there is. Requiring the high to clear a floor keeps it off players
// who are noise at either end of the range.
const BAND_RATIO = 1.5;   // band at least 1.5x the value itself
const BAND_HIGH_FLOOR = 20; // and his high is a real roster piece
function wideBand(p) {
  if (!p.FPLow || !p.FPHigh || !p.FPValue) return false;
  if (p.FPHigh < BAND_HIGH_FLOOR) return false;
  return (p.FPHigh - p.FPLow) / p.FPValue >= BAND_RATIO;
}

function adjustedEdge(p) {
  const bias = (snap.bias || {})[p.Position] || 0;
  return (p.Value - p.Cost) - bias;
}

// Click-to-sort. Each column reads one value off a player, says whether it is
// a number or a name, and carries the direction its first click should take —
// best-first for ranks and tiers, biggest-first for dollars and points. value
// returns null for a cell the board draws as "—"; those always sort last, so a
// player Boris never tiered does not masquerade as tier zero at the top.
const SORT_COLS = {
  name:  { value: p => p.Name, type: "str", dir: "asc" },
  pos:   { value: p => p.Position, type: "str", dir: "asc" },
  ecr:   { value: p => p.ECRRank || null, type: "num", dir: "asc" },
  bc:    { value: p => p.BorisTier || null, type: "num", dir: "asc" },
  cost:  { value: p => p.Cost || null, type: "num", dir: "desc" },
  value: { value: p => p.Value, type: "num", dir: "desc" },
  fp:    { value: p => p.FPValue || null, type: "num", dir: "desc" },
  edge:  { value: p => (p.Cost ? p.Value - p.Cost : null), type: "num", dir: "desc" },
  vspos: { value: p => (p.Cost ? adjustedEdge(p) : null), type: "num", dir: "desc" },
  mymax: { value: p => (p.MyMaxBid > 0 ? p.MyMaxBid : null), type: "num", dir: "desc" },
  ps:    { value: p => p.ScarcityPct || null, type: "num", dir: "desc" },
};

// ---- field guide ------------------------------------------------------
//
// One blurb per number on the screen, written once and used twice: as the
// hover tooltip on the header, and as a row in the guide overlay. Keeping
// them in one place is the point — a tooltip that has drifted away from the
// help screen is worse than no tooltip, because you believe it.
//
// Each entry is [key, label, text]. For the board, key matches the header's
// data-sort (or data-help, for the columns that do not sort). For the strip,
// key is the id of the .value span the number is drawn into.

const COLUMN_HELP = [
  ["name", "Player", "His name. Sorts A\u2013Z."],
  ["pos", "Pos", "His position."],
  ["ecr", "ECR", "FantasyPros' expert-consensus positional rank; 1 is best. A second opinion on ordering, not on price."],
  ["bc", "BC", "Boris Chen's half-PPR tier within the position; 1 is best. His tiers come from a mixture model over expert consensus, ours from dollar-value gaps \u2014 kept beside each other rather than blended."],
  ["cost", "Cost", "What it will take to win him. The national average auction value, restated against this room's money and slots."],
  ["value", "Value", "What he is worth. Median projections re-solved against the same pool. Pay this and you should get this production."],
  ["fp", "FP", "FantasyPros' projection re-solved into dollars against the same pool as Value \u2014 a like-for-like second opinion, so where the two sources diverge is visible rather than blended away. Theirs is the one source that publishes a high and a low as well as a middle; hover the cell for that range. A dash means they rank him but do not project him, which is not the same as projecting him at nothing. See \u201cReasoning about FP\u201d in this guide."],
  ["edge", "Edge", "Value \u2212 Cost. Positive is a bargain; negative means you are paying for a range of outcomes a median projection cannot see."],
  ["vspos", "vs Pos", "Edge with the position's median edge subtracted \u2014 how much better he looks than the rest of his own position. This is the number to act on: a raw +$15 on a tight end when every tight end reads +$8 is a baseline artifact."],
  ["mymax", "My Max", "The most you will bid, once your reads are applied. A must-have is a deliberate overpay; a value clipped to what you can actually afford is marked; do-not-draft shows as \u2014."],
  ["ps", "PS%", "The share of positive value left at his position after he goes. High means the position holds up without him; low means he is most of what is left."],
  ["flags", "Flags", "Your reads, the industry's signals, and what kind of player he is. Click a read pill on a row to change it."],
];

const STRIP_HELP = [
  ["budget", "budget", "The money you have left."],
  ["slots", "slots", "Roster spots you still have to fill. Every one of them needs at least a dollar, which is what caps your max bid."],
  ["maxbid", "max bid", "The most you can bid on one player and still fill the roster \u2014 your budget less $1 for every other open slot. A physical limit, not advice."],
  ["ceiling", "safe ceiling", "The most you can pay for one player before the rest of your roster suffers. Scored as your dollars per remaining starting spot against the league's, held at the stretched band. You do not pick it and it moves with the draft: when the room overspends early, the yardstick drops and this rises on its own. It is per-player, so it does not stop you committing to several \u2014 the must-have line does that job. Hover the number itself for the live band."],
  ["needs", "still starting", "The starting spots still empty on your roster. \u201clineup full\u201d means every starter is filled and the rest is bench."],
  ["pool", "pool", "What is left in the room: total dollars over total open slots, and the replacement curve prices are solved against. There is no inflation multiplier \u2014 prices are re-solved from the money and players actually remaining, so keeper inflation falls out of that by itself."],
];

// guideRows renders one help table. Same shape as the legend, so the moved
// legend markup and these generated rows sit together without restyling.
function guideRows(entries) {
  return entries.map(([, label, text]) =>
    `<tr><td><span class="guide-key">${esc(label)}</span></td><td>${esc(text)}</td></tr>`).join("");
}

function drawGuide() {
  document.getElementById("guide-strip").innerHTML = guideRows(STRIP_HELP);
  document.getElementById("guide-cols").innerHTML = guideRows(COLUMN_HELP);

  // The same text as a hover tooltip. Board headers key off data-sort, except
  // the ones that do not sort and carry data-help instead.
  const cols = new Map(COLUMN_HELP.map(([k, , text]) => [k, text]));
  for (const th of document.querySelectorAll("#board thead th")) {
    const text = cols.get(th.dataset.sort || th.dataset.help);
    if (text) th.title = text;
  }

  // On the strip the title goes on the label, not the value: draw() rewrites
  // #ceiling's own title every poll with the live risk band, and that more
  // specific reading should keep winning when you hover the number.
  const strip = new Map(STRIP_HELP.map(([k, , text]) => [k, text]));
  for (const stat of document.querySelectorAll("#strip .stat")) {
    const value = stat.querySelector(".value");
    const label = stat.querySelector(".label");
    const text = value && strip.get(value.id);
    if (text && label) label.title = text;
  }
}

function guideOpen() {
  return !document.getElementById("guide").classList.contains("hidden");
}

function toggleGuide(show) {
  document.getElementById("guide").classList.toggle("hidden", !show);
}

// compareBy sorts by one column. Missing cells sink to the bottom whichever way
// the column points; a real tie keeps the server's order, since Array.sort is
// stable and the list arrives most-expensive-first.
function compareBy(col, dir) {
  return (a, b) => {
    const av = col.value(a), bv = col.value(b);
    if (av == null && bv == null) return 0;
    if (av == null) return 1;
    if (bv == null) return -1;
    const c = col.type === "str"
      ? String(av).localeCompare(String(bv))
      : av - bv;
    return dir === "desc" ? -c : c;
  };
}

// cycleSort steps one header through default direction, its opposite, then off
// (back to the server's order). Clicking a different header starts it fresh at
// that column's own default direction.
function cycleSort(key) {
  const col = SORT_COLS[key];
  if (!col) return;
  if (sortKey !== key) {
    sortKey = key;
    sortDir = col.dir;
  } else if (sortDir === col.dir) {
    sortDir = col.dir === "asc" ? "desc" : "asc";
  } else {
    sortKey = null;
    sortDir = null;
  }
  drawSortIndicators();
  drawRows();
}

// drawSortIndicators marks the active header so the caret matches the order.
function drawSortIndicators() {
  for (const th of document.querySelectorAll("#board thead th[data-sort]")) {
    th.classList.toggle("sorted-asc", th.dataset.sort === sortKey && sortDir === "asc");
    th.classList.toggle("sorted-desc", th.dataset.sort === sortKey && sortDir === "desc");
  }
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

  drawResearch();
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
  drawKept();

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

// Research mode is whatever keeper scenario the server is holding: empty means
// the live draft-night board, anything else is a hypothetical pool. The page
// derives its whole mode from that one field, so a reload lands you back where
// the server is rather than in a mode the page invented.
function drawResearch() {
  const scenario = snap.keeperScenario || "";
  const research = scenario !== "";
  document.body.classList.toggle("research", research);
  document.getElementById("research-badge").classList.toggle("hidden", !research);
  document.getElementById("scenario-wrap").classList.toggle("hidden", !research);
  document.getElementById("criteria").classList.toggle("hidden", !research);
  const teams = document.getElementById("teams-panel");
  teams.classList.toggle("hidden", !research);
  // Restore the collapsed choice whenever the panel reappears.
  teams.classList.toggle("collapsed", localStorage.getItem("teamsCollapsed") === "1");
  for (const b of document.querySelectorAll(".mode-btn")) {
    b.classList.toggle("on", (b.dataset.mode === "research") === research);
  }
  if (research) document.getElementById("keeper-scenario").value = scenario;
}

function drawKept() {
  const panel = document.getElementById("kept-panel");
  if ((snap.keeperScenario || "") === "") { panel.classList.add("hidden"); return; }
  panel.classList.remove("hidden");
  // Restore the collapsed state chosen earlier, so a redraw does not reopen
  // a box the user closed.
  panel.classList.toggle("collapsed", localStorage.getItem("keptCollapsed") === "1");
  const kept = snap.kept || [];
  document.getElementById("kept-count").textContent =
    kept.length ? `${kept.length} off the pool` : "full pool";
  document.getElementById("kept").innerHTML = kept.length
    ? kept.map(k =>
        `<tr class="kept-${esc(k.tier)}"><td>${esc(k.name)}</td>` +
        `<td class="pos pos-${esc(k.position)}">${esc(k.position)}</td>` +
        `<td class="num">$${k.price}</td>` +
        `<td>${k.tier === "lock" ? '<span class="kept-lock">lock</span>' : ""}</td></tr>`
      ).join("")
    : `<tr><td colspan="4" class="empty">nobody assumed kept — the full pool</td></tr>`;
}

// setScenario switches the board between the live view ("") and a research
// keeper scenario. The response is the rebuilt board, same path as a sale, so
// values and scarcity move in one paint. The 2s poll then keeps it, since the
// scenario lives on the server.
async function setScenario(scenario) {
  const sold = snap && snap.__sold;
  snap = await fetchJSON("api/keepers", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scenario }),
  });
  snap.__sold = sold || {};
  draw();
}

// ---- team sampling (research mode) -----------------------------------

let savedTeams = [];
const JSON_POST = { "Content-Type": "application/json" };

function surname(name) {
  const parts = String(name).trim().split(/\s+/);
  return parts.length > 1 ? parts.slice(1).join(" ") : name;
}

// teamPicks flattens a sampled team's starters and bench into the pick list the
// shortlist and the scratch loader both speak. Keepers (Held) are dropped: they
// are always yours, so the scratch loader re-adds them itself — including them
// here would double them on the roster.
function teamPicks(team) {
  return [...(team.starters || []), ...(team.bench || [])]
    .filter(s => !s.Held)
    .map(s => ({
      playerId: s.Player.PlayerID, name: s.Player.Name,
      position: s.Player.Position, price: s.Price,
    }));
}

function teamCardHTML(picks, meta, kind) {
  const line = picks.map(p =>
    `<span class="tp pos-${esc(p.position)}">${esc(surname(p.name))} <em>$${p.price}</em></span>`
  ).join("");
  const guys = meta.myGuys ? `${meta.myGuys} of your guys` : "";
  const actions = kind === "saved"
    ? `<button data-scratch-saved="${esc(meta.id)}">→ scratch</button>` +
      `<button class="del" data-del="${esc(meta.id)}">remove</button>`
    : `<button data-save>save</button><button data-scratch-sample>→ scratch</button>`;
  return `<div class="team-card">
    <div class="team-head"><span class="team-spend">$${meta.spend}</span>` +
    `<span class="team-popr" title="starting points above replacement">POPR ${Math.round(meta.popr)}</span>` +
    (guys ? `<span class="team-guys">${guys}</span>` : "") +
    (meta.created ? `<span class="team-when">${esc(meta.created)}</span>` : "") +
    `</div><div class="team-line">${line}</div>` +
    `<div class="team-actions">${actions}</div></div>`;
}

async function sampleTeams(objective, button) {
  const results = document.getElementById("teams-results");
  results.innerHTML = `<p class="note">sampling…</p>`;
  try {
    const teams = await fetchJSON("api/teams", {
      method: "POST", headers: JSON_POST, body: JSON.stringify({ objective }),
    });
    if (!teams.length) { results.innerHTML = `<p class="note">no legal teams from this pool.</p>`; return; }
    results.innerHTML = teams.map(t => {
      const picks = teamPicks(t);
      const card = teamCardHTML(picks, { spend: t.spend, popr: t.popr, myGuys: t.myGuys }, "sample");
      // Stash the picks + meta on the node so save/scratch need no re-sample.
      return card;
    }).join("");
    // Bind the sampled picks to each card in order.
    [...results.querySelectorAll(".team-card")].forEach((el, i) => {
      el.__team = { objective, picks: teamPicks(teams[i]), spend: teams[i].spend, popr: teams[i].popr, myGuys: teams[i].myGuys };
    });
  } catch (err) {
    results.innerHTML = `<p class="note">${esc(String(err))}</p>`;
  }
}

async function refreshSaved() {
  savedTeams = await fetchJSON("api/teams/saved");
  drawSavedTeams();
}

function drawSavedTeams() {
  const wrap = document.getElementById("saved-teams");
  document.getElementById("saved-head").classList.toggle("hidden", savedTeams.length === 0);
  wrap.innerHTML = savedTeams.map(t =>
    teamCardHTML(t.picks || [], t, "saved")).join("");
}

async function loadPicksIntoScratch(picks) {
  await fetchJSON("api/scratch", { method: "POST", headers: JSON_POST, body: JSON.stringify({ action: "clear" }) });
  let res;
  for (const p of picks) {
    res = await fetchJSON("api/scratch", {
      method: "POST", headers: JSON_POST,
      body: JSON.stringify({ action: "add", player: p.name, price: p.price }),
    });
  }
  if (res) { snap = res.board; scratch = res.scratch; draw(); }
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

// Research mode is any active keeper scenario — the same field drawResearch
// derives the whole mode from. The criteria filters only bite here.
function researchActive() {
  return (snap && (snap.keeperScenario || "")) !== "";
}

// inRange keeps a player whose value sits within a set window. A player with no
// value in the field (0/"—") falls outside any window you set on purpose: "no
// AAV" is not "inside an AAV range", so a range excludes him rather than
// letting a zero pass as the low end.
function inRange(v, r) {
  if (r.min == null && r.max == null) return true;
  if (!v) return false;
  if (r.min != null && v < r.min) return false;
  if (r.max != null && v > r.max) return false;
  return true;
}

function traitMatch(p) {
  if (!critTraits.size) return true;
  const has = new Set(p.Traits || []);
  const sel = [...critTraits];
  return critTraitMode === "all" ? sel.every(t => has.has(t)) : sel.some(t => has.has(t));
}

function critActive() {
  return critAAV.min != null || critAAV.max != null ||
    critTier.min != null || critTier.max != null ||
    critCost.min != null || critCost.max != null ||
    critTraits.size > 0;
}

function meetsCriteria(p) {
  return inRange(p.AAV, critAAV) &&
    inRange(p.BorisTier, critTier) &&
    inRange(p.Cost, critCost) &&
    traitMatch(p);
}

function anyFilter() {
  return positions.size > 0 || filter !== "" || affordableOnly || aboveReplacementOnly ||
    (researchActive() && critActive());
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
    // Research-only criteria, gated so the draft-night board is never filtered
    // by a range left over from a research session.
    if (researchActive() && !meetsCriteria(p)) return false;
    return true;
  });
  // Sort before the cap, so the 120 that survive it are the top of the order
  // you asked for rather than the top of the server's.
  if (sortKey) matched.sort(compareBy(SORT_COLS[sortKey], sortDir));
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
      <td class="num bc" title="Boris Chen half-PPR tier (within position; 1 is best)">${p.BorisTier ? "T" + p.BorisTier : "—"}</td>
      <td class="num">${p.Cost ? money(p.Cost) : "—"}</td>
      <td class="num">${money(p.Value)}</td>
      <td class="num fp" title="${esc(fpCellTitle(p))}">${p.FPValue ? money(p.FPValue) : "—"}</td>
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

// The research-mode trait chips, mirroring the position pills: outline off,
// filled on.
function drawCritTraits() {
  document.getElementById("crit-traits").innerHTML = TRAITS.map(t =>
    `<button class="critpill trait-${t}${critTraits.has(t) ? " on" : ""}"` +
    ` data-trait="${t}">${t}</button>`).join("");
}

// clearCriteria resets every range and trait, and the inputs that hold them, so
// one gesture (the clear button or Escape) empties the whole panel. The any/all
// mode is a preference, not a filter value, so it is left alone.
function clearCriteria() {
  critAAV = { min: null, max: null };
  critTier = { min: null, max: null };
  critCost = { min: null, max: null };
  critTraits.clear();
  for (const id of ["crit-aav-min", "crit-aav-max", "crit-tier-min",
    "crit-tier-max", "crit-cost-min", "crit-cost-max"]) {
    document.getElementById(id).value = "";
  }
  drawCritTraits();
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

  const sampleBtn = ev.target.closest(".sample-btn");
  if (sampleBtn) {
    await sampleTeams(sampleBtn.dataset.objective, sampleBtn);
    return;
  }
  const saveBtn = ev.target.closest("button[data-save]");
  if (saveBtn) {
    const t = saveBtn.closest(".team-card").__team;
    savedTeams = await fetchJSON("api/teams/save", {
      method: "POST", headers: JSON_POST,
      body: JSON.stringify({ objective: t.objective, spend: t.spend, popr: t.popr, myGuys: t.myGuys, picks: t.picks }),
    });
    drawSavedTeams();
    saveBtn.textContent = "saved ✓";
    saveBtn.disabled = true;
    return;
  }
  const scratchSample = ev.target.closest("button[data-scratch-sample]");
  if (scratchSample) {
    await loadPicksIntoScratch(scratchSample.closest(".team-card").__team.picks);
    return;
  }
  const scratchSaved = ev.target.closest("button[data-scratch-saved]");
  if (scratchSaved) {
    const res = await fetchJSON("api/teams/scratch", {
      method: "POST", headers: JSON_POST, body: JSON.stringify({ id: scratchSaved.dataset.scratchSaved }),
    });
    snap = res.board; scratch = res.scratch; draw();
    return;
  }
  const delBtn = ev.target.closest("button[data-del]");
  if (delBtn) {
    savedTeams = await fetchJSON("api/teams/delete", {
      method: "POST", headers: JSON_POST, body: JSON.stringify({ id: delBtn.dataset.del }),
    });
    drawSavedTeams();
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

// ---- research mode ---------------------------------------------------

for (const b of document.querySelectorAll(".mode-btn")) {
  b.addEventListener("click", () => {
    // Draft night is the live board (no scenario); Research enters with
    // whatever the dropdown last showed, defaulting to the expected keepers.
    if (b.dataset.mode === "research") {
      setScenario(document.getElementById("keeper-scenario").value || "expected");
    } else {
      setScenario("");
    }
  });
}

document.getElementById("keeper-scenario").addEventListener("change", ev => {
  setScenario(ev.target.value);
});

// The Kept box can be collapsed to a header, so it does not push the rest of
// the panel down when you have finished reading it. The choice sticks.
document.getElementById("kept-head").addEventListener("click", () => {
  const collapsed = document.getElementById("kept-panel").classList.toggle("collapsed");
  localStorage.setItem("keptCollapsed", collapsed ? "1" : "");
});

document.getElementById("teams-head").addEventListener("click", () => {
  const collapsed = document.getElementById("teams-panel").classList.toggle("collapsed");
  localStorage.setItem("teamsCollapsed", collapsed ? "1" : "");
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

// Click a column header to sort by it; cycleSort handles the direction cycle.
document.querySelector("#board thead").addEventListener("click", ev => {
  const th = ev.target.closest("th[data-sort]");
  if (th) cycleSort(th.dataset.sort);
});

// ---- research criteria filters ---------------------------------------

function numOrNull(el) {
  const v = el.value.trim();
  if (v === "") return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

// bindRange keeps a {min,max} object in step with its two inputs.
function bindRange(minId, maxId, obj) {
  const update = () => {
    obj.min = numOrNull(document.getElementById(minId));
    obj.max = numOrNull(document.getElementById(maxId));
    drawRows();
  };
  document.getElementById(minId).addEventListener("input", update);
  document.getElementById(maxId).addEventListener("input", update);
}
bindRange("crit-aav-min", "crit-aav-max", critAAV);
bindRange("crit-tier-min", "crit-tier-max", critTier);
bindRange("crit-cost-min", "crit-cost-max", critCost);

document.getElementById("crit-traits").addEventListener("click", ev => {
  const pill = ev.target.closest("button[data-trait]");
  if (!pill) return;
  const t = pill.dataset.trait;
  if (critTraits.has(t)) critTraits.delete(t); else critTraits.add(t);
  drawCritTraits();
  drawRows();
});

document.getElementById("crit-trait-mode").addEventListener("click", ev => {
  critTraitMode = critTraitMode === "any" ? "all" : "any";
  ev.currentTarget.dataset.mode = critTraitMode;
  ev.currentTarget.textContent = critTraitMode;
  drawRows();
});

document.getElementById("crit-clear").addEventListener("click", () => {
  clearCriteria();
  drawRows();
});

document.addEventListener("keydown", ev => {
  const search = document.getElementById("search");
  if (ev.key === "/" && document.activeElement !== search) {
    ev.preventDefault();
    search.focus();
    search.select();
  } else if (ev.key === "?" && !typing(document.activeElement)) {
    ev.preventDefault();
    toggleGuide(!guideOpen());
  } else if (ev.key === "Escape") {
    // The guide first. Escape also wipes every filter, and closing a help
    // screen is not a reason to throw away the slice of the board you set up.
    if (guideOpen()) {
      toggleGuide(false);
      return;
    }
    // Everything, not just the text. Escape is the one key you hit when a
    // name is called and the board is showing the wrong slice of players.
    search.value = "";
    filter = "";
    positions.clear();
    affordableOnly = false;
    aboveReplacementOnly = false;
    sortKey = null;
    sortDir = null;
    clearCriteria();
    document.getElementById("affordable").checked = false;
    document.getElementById("above").checked = false;
    search.blur();
    drawPosFilter();
    drawSortIndicators();
    drawRows();
  }
});

// typing reports whether keystrokes belong to a field rather than the board,
// so "/" and "?" stay usable as shortcuts without eating a search term or a
// digit typed into one of the criteria boxes.
function typing(el) {
  return !!el && (el.tagName === "INPUT" || el.tagName === "SELECT" || el.tagName === "TEXTAREA");
}

document.getElementById("guidebtn").addEventListener("click", () => toggleGuide(true));
for (const el of document.querySelectorAll("[data-guide-close]")) {
  el.addEventListener("click", () => toggleGuide(false));
}

drawGuide();
drawPosFilter();
drawCritTraits();

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
refreshSaved().catch(() => {});

// The arbitrage page: which of my targets rule each other out, and what a
// best-fit line built only from them would cost.
//
// The server owns every judgement here — which pairs are exclusive, what a
// pick costs, which slot it fills. This file draws.

const esc = s => String(s ?? "").replace(/[&<>"']/g, c =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

// The board's own cadence, so this page moves when it does. Matched to
// POLL_MS in app.js: the server cannot have newer data than its last Sleeper
// poll, and the view is cached server-side between board changes, so asking
// this often costs a cache read on most ticks.
const POLL_MS = 1000;

let view = null;
// The last payload drawn, so a tick that changes nothing changes nothing on
// screen. Without this the page would rebuild itself every second under
// whatever you were reading, and a table that reflows while you look at it is
// worse than one a second behind.
let lastPayload = "";

function pct() { return document.getElementById("pct").value; }

async function load() {
  const err = document.getElementById("err");
  try {
    const res = await fetch(`api/arbitrage?pct=${pct()}`);
    if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
    const body = await res.text();
    err.classList.add("hidden");
    if (body === lastPayload) return; // nothing moved; leave the page alone
    lastPayload = body;
    view = JSON.parse(body);
    draw();
  } catch (e) {
    // Into the bar rather than the console: an empty page and a failed fetch
    // look identical otherwise.
    err.textContent = `could not load: ${e.message}`;
    err.classList.remove("hidden");
  }
}

function tag(t) {
  if (t.lean === "must") return `<span class="flag must">MUST</span>`;
  if (t.favorite && t.lean === "up") return `<span class="flag">+</span><span class="flag fav">★</span>`;
  if (t.favorite) return `<span class="flag fav">★</span>`;
  if (t.lean === "up") return `<span class="flag">+</span>`;
  return "";
}

function player(t) {
  return `<span class="pos">${esc(t.position)}</span> ` +
    `<span class="pname">${esc(t.name)}</span> ${tag(t)}` +
    `<span class="money">$${t.value}</span>` +
    `<span class="money dim">max $${t.myMaxBid}</span>`;
}

// One line-up as a table: who fills what, at cost, and what it is worth.
function fitTable(picks) {
  return `<table class="mini fit"><tbody>` + picks.map(p =>
    `<tr><td class="slot">${esc(p.slot)}</td>` +
    `<td class="pos">${esc(p.pick.position)}</td>` +
    `<td class="pname">${esc(p.pick.name)} ${tag(p.pick)}</td>` +
    `<td class="num">$${p.pick.cost}</td>` +
    `<td class="num dim">worth $${p.pick.value}</td></tr>`).join("") +
    `</tbody></table>`;
}

// Surplus is what the line keeps after paying for itself. Shown because a line
// climbs in value all the way to the cap while its surplus falls: spending
// everything always looks better by value alone.
function fitTotal(line, extra) {
  const s = line.surplus;
  const cls = s > 0 ? "good" : s < 0 ? "bad" : "dim";
  return `<div class="fittotal">$${line.spend} for $${line.value} of value ` +
    `<span class="surplus ${cls}">${s >= 0 ? "+" : "-"}$${Math.abs(s)} kept</span>` +
    (line.unfilled && line.unfilled.length
      ? ` &middot; cannot cover ${line.unfilled.map(esc).join(", ")}`
      : ` &middot; every starting slot covered`) + (extra || "") + `</div>`;
}

// The two answers, side by side. Most value is what the cap can buy; best per
// dollar is what is worth buying. They are different questions and the page
// does not pick between them.
function drawBestFit() {
  const el = document.getElementById("bestfit");
  const b = view.bestFit || {};
  document.getElementById("capnote").textContent =
    `= $${b.cap ?? 0} of $${view.budgetLeft ?? 0}, holding $${b.reserve ?? 0} back for the rest of the roster`;

  if (!b.picks || !b.picks.length) {
    el.innerHTML = `<p class="note">Nothing your targets can buy at this ceiling.</p>`;
    return;
  }

  const pd = view.perDollar;
  let right;
  if (view.perDollarIsBest) {
    right = `<div class="agree">The most valuable line is also the one that
      keeps the most. Nothing here is being bought above what it is worth.</div>`;
  } else if (pd) {
    right = fitTable(pd.picks) + fitTotal(pd);
  } else {
    right = `<div class="agree none">No combination of your targets is worth
      more than it costs at these prices. Everything left is priced at or above
      your read on it.</div>`;
  }

  el.innerHTML = `<div class="twoline">
      <section class="fitcol">
        <h3>Most value <span class="mode">what the cap can buy</span></h3>
        ${fitTable(b.picks)}${fitTotal(b)}
      </section>
      <section class="fitcol">
        <h3>Best per dollar <span class="mode">what is worth buying</span></h3>
        ${right}
      </section>
    </div>` + alternativesHTML();
}

// The runner-ups: for each rival anchor, the best line built on him instead,
// and what changing horses costs in value. This is the answer to "does the
// board think I should take him" — it does not; it is showing what else is
// reachable and by how much it trails.
function alternativesHTML() {
  const alts = view.alternatives || [];
  if (!alts.length) return "";
  const best = view.bestFit || {};
  const anchor = line => {
    let a = null;
    for (const p of line.picks || []) {
      if (!a || p.pick.cost > a.cost || (p.pick.cost === a.cost && p.pick.value > a.value)) a = p.pick;
    }
    return a;
  };
  const lead = anchor(best);

  return `<div class="alts">
    <h3>If not ${esc(lead ? lead.name : "him")}
      <span class="mode">the best line built on someone else</span></h3>` +
    alts.map(a => {
      const an = anchor(a);
      const margin = (best.value || 0) - a.value;
      return `<div class="alt">
        <div class="altline">
          <span class="altmargin">${margin === 0 ? "level" : "&minus;$" + margin}</span>
          <span class="pname">${esc(an ? an.name : "\u2014")}</span>
          <span class="dim">${esc(an ? an.position : "")} $${an ? an.cost : 0}</span>
          <span class="altspend">$${a.spend} for $${a.value}
            <span class="surplus ${a.surplus > 0 ? "good" : a.surplus < 0 ? "bad" : "dim"}">${a.surplus >= 0 ? "+" : "-"}$${Math.abs(a.surplus)}</span></span>
        </div>
        <div class="altrest dim">${(a.picks || []).filter(p => !an || p.pick.playerId !== an.playerId)
          .map(p => esc(p.pick.name) + " $" + p.pick.cost).join(" &middot; ")}` +
        (a.unfilled && a.unfilled.length ? ` &middot; ${a.unfilled.map(esc).join(", ")} open` : "") +
        `</div>
      </div>`;
    }).join("") + `</div>`;
}

function drawChain() {
  const el = document.getElementById("chain");
  const note = document.getElementById("chain-note");

  if (!view.chain || !view.chain.length) {
    el.innerHTML = "";
    note.textContent = "No target fills an open starting slot — either the " +
      "lineup is full or your reads have run out.";
    return;
  }

  const held = (view.held || []).length;
  note.textContent = held
    ? `Starting from the ${held} player${held === 1 ? "" : "s"} you already hold, ` +
      `each pick below is the most valuable target still open that fills a slot.`
    : `Each pick is the most valuable target still open that fills a starting slot.`;

  el.innerHTML = view.chain.map((s, i) => {
    const cost = (s.cost || []).length
      ? `<div class="cost">costs you ` +
        s.cost.map(c => `<span class="lost">${esc(c.name)} <span class="dim">${esc(c.position)} $${c.value}</span></span>`).join(", ") +
        `</div>`
      : `<div class="cost none">costs nothing &mdash; no other target shares his offense</div>`;
    return `<div class="step">
      <div class="stepnum">${i + 1}</div>
      <div class="stepbody">
        <div class="pick"><span class="slot">${esc(s.slot || "&mdash;")}</span> ${player(s.pick)}
          <span class="running">running $${s.spend}</span></div>
        ${cost}
      </div>
    </div>`;
  }).join("");

  if (view.unfilled && view.unfilled.length) {
    el.innerHTML += `<div class="unfilled">Your targets do not cover: ` +
      view.unfilled.map(u => `<strong>${esc(u)}</strong>`).join(", ") +
      ` &mdash; those slots need someone you have no read on.</div>`;
  }
  if (view.spend > view.budgetLeft) {
    el.innerHTML += `<div class="overspend">That line costs $${view.spend} against $${view.budgetLeft} left. ` +
      `It is a shape, not a plan &mdash; nothing here is solved against your budget.</div>`;
  }
}

function drawGroups() {
  const el = document.getElementById("groups");
  if (view.inactive) {
    el.innerHTML = `<p class="note">No preference filter is on, so no target ` +
      `rules out any other and this page has nothing to say.</p>`;
    return;
  }
  if (!view.groups || !view.groups.length) {
    el.innerHTML = `<p class="note">No offense carries two of your targets.</p>`;
    return;
  }

  const byID = {};
  for (const g of view.groups) for (const t of g.targets) byID[t.playerId] = t;

  el.innerHTML = view.groups.map(g => {
    const rows = g.targets.map(t => {
      const lost = (g.costs || {})[t.playerId] || [];
      const cost = lost.length
        ? `<span class="takes">take him and you lose ${lost.map(esc).join(", ")}</span>`
        : `<span class="takes free">no cost here</span>`;
      return `<div class="grow">${player(t)}${cost}</div>`;
    }).join("");

    const pairs = (g.pairs || []).map(p => {
      const a = byID[p.a], b = byID[p.b];
      if (!a || !b) return "";
      const mark = p.relation === "stack" ? "=" : "×";
      return `<div class="pair ${esc(p.relation)}">` +
        `<span class="mark">${mark}</span> ${esc(a.name)} ${p.relation === "stack" ? "+" : "/"} ${esc(b.name)}` +
        `<span class="why">${esc(p.relation === "stack" ? "stack, both allowed" : p.relation)}</span></div>`;
    }).join("");

    return `<section class="group"><h3>${esc(g.team)}</h3>${rows}${pairs}</section>`;
  }).join("");
}

function draw() {
  document.getElementById("n-targets").textContent = view.targets ?? "—";
  document.getElementById("n-groups").textContent = (view.groups || []).length;
  document.getElementById("n-chain").textContent = (view.chain || []).length;
  document.getElementById("n-spend").textContent = `$${view.spend || 0}`;
  document.getElementById("n-budget").textContent = `$${view.budgetLeft || 0}`;
  drawBestFit();
  drawChain();
  drawGroups();
}

document.getElementById("reload").addEventListener("click", load);

// The slider label follows the thumb, but the solve only reruns when you let
// go: it is a beam search over every target, not something to run per pixel.
const slider = document.getElementById("pct");
slider.addEventListener("input", () => {
  document.getElementById("pctout").textContent = `${slider.value}%`;
});
// A new ceiling is a different answer, so force the redraw past the
// unchanged-payload check.
slider.addEventListener("change", () => { lastPayload = ""; load(); });

load();
setInterval(load, POLL_MS);

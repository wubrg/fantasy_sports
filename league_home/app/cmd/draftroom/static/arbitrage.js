// The arbitrage page: which of my targets rule each other out, and what a
// best-fit line built only from them would cost.
//
// The server owns every judgement here — which pairs are exclusive, what a
// pick costs, which slot it fills. This file draws.

const esc = s => String(s ?? "").replace(/[&<>"']/g, c =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

let view = null;

async function load() {
  const err = document.getElementById("err");
  try {
    const res = await fetch("api/arbitrage");
    if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
    view = await res.json();
    err.classList.add("hidden");
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
  drawChain();
  drawGroups();
}

document.getElementById("reload").addEventListener("click", load);
load();

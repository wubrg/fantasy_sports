const NAMED_AWARDS = new Set([
  "AP MVP", "AP OPOY", "AP DPOY", "AP OROTY", "AP DROTY", "AP CPOTY", "SB MVP",
]);

// Cross-league meta-groups: each bundles a league-specific NFL code with its
// AFL-era equivalent (1960-1969, see ADR-002) so a user filtering for "MVP"
// or "Pro Bowl" doesn't need to know which league named which award in a
// given year. All-Pro additionally splits into 1st/2nd team tiers, since
// "made All-Pro" and "made 1st-team All-Pro" are meaningfully different
// filters. Awards with no cross-league equivalent (OPOY/DPOY/CPOTY/SB MVP)
// are deliberately left out and rendered as standalone checkboxes instead.
const AWARD_GROUPS = [
  { key: "mvp", label: "MVP", codes: ["AP MVP", "AFL MVP"] },
  { key: "roy", label: "Rookie of the Year", codes: ["AP OROTY", "AP DROTY", "AFL ROY"] },
  {
    key: "allpro",
    label: "All-Pro",
    codes: ["All-Pro 1st", "All-Pro 2nd", "All-AFL 1st", "All-AFL 2nd"],
    tiers: {
      "1st": ["All-Pro 1st", "All-AFL 1st"],
      "2nd": ["All-Pro 2nd", "All-AFL 2nd"],
    },
  },
  { key: "probowl", label: "Pro Bowl", codes: ["Pro Bowl", "AFL All-Star"] },
];

let meta = null;
let rows = [];
let sortKey = "yr";
let sortDir = -1; // -1 = desc, 1 = asc

const state = {
  team: "",
  unit: "",
  awards: new Set(), // standalone (ungrouped) award codes
  groupActive: {}, // AWARD_GROUPS key -> bool
  allProTier: "all", // "all" | "1st" | "2nd"
  yearMin: null,
  yearMax: null,
  search: "",
};

// Flattens the group/tier/standalone selections into the single set of raw
// award codes filteredRows() needs to match against.
function activeAwardCodes() {
  const codes = new Set(state.awards);
  for (const group of AWARD_GROUPS) {
    if (!state.groupActive[group.key]) continue;
    const groupCodes = group.tiers ? group.tiers[state.allProTier] || group.codes : group.codes;
    for (const c of groupCodes) codes.add(c);
  }
  return codes;
}

async function init() {
  const res = await fetch("api/data");
  const json = await res.json();
  meta = json.meta;
  rows = json.data;

  buildTeamSelect();
  buildAwardCheckboxes();
  buildYearInputs();
  wireEvents();
  render();
}

function buildTeamSelect() {
  const sel = document.getElementById("team-select");
  const opt = document.createElement("option");
  opt.value = "";
  opt.textContent = "All Teams";
  sel.appendChild(opt);

  const teams = [...meta.teams].sort((a, b) =>
    meta.team_names[a].localeCompare(meta.team_names[b])
  );
  for (const code of teams) {
    const o = document.createElement("option");
    o.value = code;
    o.textContent = meta.team_names[code];
    sel.appendChild(o);
  }
}

function buildAwardCheckboxes() {
  const container = document.getElementById("award-checkboxes");
  const available = new Set(meta.awards);
  const groupedCodes = new Set();

  for (const group of AWARD_GROUPS) {
    const codes = group.codes.filter((c) => available.has(c));
    if (codes.length === 0) continue;
    for (const c of codes) groupedCodes.add(c);
    container.appendChild(buildAwardGroupControl(group));
  }

  for (const award of meta.awards) {
    if (groupedCodes.has(award)) continue;
    const label = document.createElement("label");
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.value = award;
    cb.addEventListener("change", () => {
      if (cb.checked) state.awards.add(award);
      else state.awards.delete(award);
      render();
    });
    label.appendChild(cb);
    label.append(award);
    container.appendChild(label);
  }
}

function buildAwardGroupControl(group) {
  const wrapper = document.createElement("div");
  wrapper.className = "award-group";

  const label = document.createElement("label");
  const cb = document.createElement("input");
  cb.type = "checkbox";
  cb.addEventListener("change", () => {
    state.groupActive[group.key] = cb.checked;
    render();
  });
  label.appendChild(cb);
  label.append(group.label);
  wrapper.appendChild(label);

  if (group.tiers) {
    const tierToggle = document.createElement("div");
    tierToggle.className = "toggle-group tier-toggle";
    for (const [value, text] of [["all", "All"], ["1st", "1st only"], ["2nd", "2nd only"]]) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = text;
      btn.dataset.tier = value;
      if (value === state.allProTier) btn.classList.add("active");
      btn.addEventListener("click", () => {
        state.allProTier = value;
        for (const b of tierToggle.querySelectorAll("button")) {
          b.classList.toggle("active", b === btn);
        }
        render();
      });
      tierToggle.appendChild(btn);
    }
    wrapper.appendChild(tierToggle);
  }

  return wrapper;
}

function buildYearInputs() {
  const [min, max] = meta.years;
  const minInput = document.getElementById("year-min");
  const maxInput = document.getElementById("year-max");
  minInput.min = min;
  minInput.max = max;
  minInput.value = min;
  maxInput.min = min;
  maxInput.max = max;
  maxInput.value = max;
  state.yearMin = min;
  state.yearMax = max;
}

function wireEvents() {
  document.getElementById("team-select").addEventListener("change", (e) => {
    state.team = e.target.value;
    render();
  });

  document.getElementById("unit-toggle").addEventListener("click", (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    for (const b of e.currentTarget.querySelectorAll("button")) {
      b.classList.toggle("active", b === btn);
    }
    state.unit = btn.dataset.unit;
    render();
  });

  document.getElementById("year-min").addEventListener("input", (e) => {
    state.yearMin = Number(e.target.value);
    render();
  });
  document.getElementById("year-max").addEventListener("input", (e) => {
    state.yearMax = Number(e.target.value);
    render();
  });

  document.getElementById("player-search").addEventListener("input", (e) => {
    state.search = e.target.value.trim().toLowerCase();
    render();
  });

  document.getElementById("export-csv").addEventListener("click", exportCsv);

  for (const th of document.querySelectorAll("th[data-sort]")) {
    th.addEventListener("click", () => {
      const key = th.dataset.sort;
      if (sortKey === key) {
        sortDir *= -1;
      } else {
        sortKey = key;
        sortDir = key === "yr" ? -1 : 1;
      }
      render();
    });
  }
}

function filteredRows() {
  const activeAwards = activeAwardCodes();
  return rows.filter((r) => {
    if (state.team && r.tm !== state.team) return false;
    if (state.unit && r.u !== state.unit) return false;
    if (activeAwards.size > 0 && !activeAwards.has(r.aw)) return false;
    if (r.yr < state.yearMin || r.yr > state.yearMax) return false;
    if (state.search && !r.pl.toLowerCase().includes(state.search)) return false;
    return true;
  });
}

function sortedRows(list) {
  return [...list].sort((a, b) => {
    const av = a[sortKey];
    const bv = b[sortKey];
    if (av < bv) return -1 * sortDir;
    if (av > bv) return 1 * sortDir;
    return b.yr - a.yr;
  });
}

function render() {
  const filtered = sortedRows(filteredRows());
  const body = document.getElementById("results-body");
  body.innerHTML = "";

  for (const r of filtered) {
    const tr = document.createElement("tr");
    if (NAMED_AWARDS.has(r.aw)) tr.classList.add("named-award");
    tr.innerHTML = `
      <td>${r.yr}</td>
      <td>${escapeHtml(r.pl)}</td>
      <td>${escapeHtml(r.pos)}</td>
      <td>${escapeHtml(r.tm)}</td>
      <td>${escapeHtml(r.aw)}</td>
      <td>${escapeHtml(r.nt || "")}</td>
    `;
    body.appendChild(tr);
  }

  document.getElementById("result-count").textContent =
    `${filtered.length} result${filtered.length === 1 ? "" : "s"}`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function exportCsv() {
  const filtered = sortedRows(filteredRows());
  const header = ["Year", "Player", "Pos", "Team", "Award", "Notes"];
  const lines = [header.join(",")];
  for (const r of filtered) {
    lines.push([r.yr, r.pl, r.pos, r.tm, r.aw, r.nt || ""]
      .map((v) => `"${String(v).replace(/"/g, '""')}"`)
      .join(","));
  }
  const blob = new Blob([lines.join("\n")], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "canton_filtered.csv";
  a.click();
  URL.revokeObjectURL(url);
}

init();

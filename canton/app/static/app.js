const NAMED_AWARDS = new Set([
  "AP MVP", "AP OPOY", "AP DPOY", "AP OROTY", "AP DROTY", "AP CPOTY", "SB MVP",
]);

let meta = null;
let rows = [];
let sortKey = "yr";
let sortDir = -1; // -1 = desc, 1 = asc

const state = {
  team: "",
  unit: "",
  awards: new Set(),
  yearMin: null,
  yearMax: null,
  search: "",
};

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
  for (const award of meta.awards) {
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
  return rows.filter((r) => {
    if (state.team && r.tm !== state.team) return false;
    if (state.unit && r.u !== state.unit) return false;
    if (state.awards.size > 0 && !state.awards.has(r.aw)) return false;
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

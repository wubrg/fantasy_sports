const cache = {};

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status} ${await res.text()}`);
  return res.json();
}

async function cached(key, url) {
  if (!(key in cache)) cache[key] = await fetchJSON(url);
  return cache[key];
}

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function fmtPoints(n) {
  return (n > 0 ? "+" : "") + n;
}

async function loadState() {
  const line = document.getElementById("state-line");
  try {
    const st = await fetchJSON("/api/state");
    line.textContent = `${st.season} season (${st.season_type}) — week ${st.week}`;
  } catch (e) {
    line.textContent = "season state unavailable";
  }
}

async function loadStandings() {
  const rows = await cached("standings", "/api/standings");
  const body = document.getElementById("standings-body");
  body.innerHTML = rows.map((r, i) => `
    <tr>
      <td>${i + 1}</td>
      <td>${escapeHtml(r.Team)}</td>
      <td>${r.Wins}-${r.Losses}-${r.Ties}</td>
      <td>${r.PointsFor.toFixed(2)}</td>
      <td>${r.PointsAgainst.toFixed(2)}</td>
    </tr>
  `).join("");
}

async function loadFaab() {
  const rows = await cached("faab", "/api/faab");
  const body = document.getElementById("faab-body");
  body.innerHTML = rows.map((r) => `
    <tr>
      <td>${escapeHtml(r.Team)}</td>
      <td>${r.Remaining}</td>
      <td>${r.Budget}</td>
      <td>${r.Used}</td>
    </tr>
  `).join("");
}

async function loadMatchups(week) {
  const body = document.getElementById("matchups-body");
  const empty = document.getElementById("matchups-empty");
  const rows = await fetchJSON(`/api/matchups?week=${encodeURIComponent(week)}`);
  empty.hidden = rows.length > 0;
  body.innerHTML = rows.map((m) => `
    <tr>
      <td>${escapeHtml(m.Home)} <strong>${m.HomePoints.toFixed(2)}</strong></td>
      <td>vs</td>
      <td><strong>${m.AwayPoints.toFixed(2)}</strong> ${escapeHtml(m.Away)}</td>
    </tr>
  `).join("");
}

async function loadScoring() {
  const categories = await cached("scoring", "/api/scoring");
  const container = document.getElementById("scoring-body");
  container.innerHTML = categories.map((c) => `
    <div class="scoring-card">
      <h3>${escapeHtml(c.Name)}</h3>
      <table>
        <tbody>
          ${c.Entries.map((e) => `
            <tr><td>${escapeHtml(e.Label)}</td><td class="num">${fmtPoints(e.Points)}</td></tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `).join("");
}

async function loadRules() {
  const r = await cached("rules", "/api/rules");
  const container = document.getElementById("rules-body");
  container.innerHTML = `
    <h3>Starting lineup</h3>
    <table>
      <thead><tr><th>Position</th><th>Starters</th><th>Max on roster</th></tr></thead>
      <tbody>
        ${r.roster.starting_slots.map((s) => `
          <tr><td>${escapeHtml(s.position)}</td><td>${s.starters}</td><td>${s.max_on_roster}</td></tr>
        `).join("")}
      </tbody>
    </table>
    <p>Bench: ${r.roster.bench_slots} &nbsp;&middot;&nbsp; IR: ${r.roster.ir_slots}</p>

    <h3>Keepers</h3>
    <p>Max ${r.keepers.max_keepers}, minimum value $${r.keepers.minimum_value},
       +$${r.keepers.increment_per_keep_count} per keep count.
       Locks ${r.keepers.lock_hours_before_draft}h before the draft
       (${r.keepers.expansion_lock_hours_before_draft}h for expansion teams).</p>

    <h3>Waivers</h3>
    <p>$${r.waivers.yearly_budget} yearly budget, $${r.waivers.minimum_bid} minimum bid,
       ${escapeHtml(r.waivers.processing_schedule)}.</p>

    <h3>Draft</h3>
    <p>${escapeHtml(r.draft.format)}, $${r.draft.base_budget} base budget.
       Trade deadline: start of week ${r.trade_deadline_week}.</p>

    <h3>Playoffs</h3>
    <table>
      <thead><tr><th>League size</th><th>Weeks</th><th>Playoff teams</th><th>Byes</th></tr></thead>
      <tbody>
        ${r.playoffs.map((p) => `
          <tr><td>${p.league_size}</td><td>${p.start_week}-${p.end_week}</td>
              <td>${p.playoff_teams}</td><td>${p.bye_teams}</td></tr>
        `).join("")}
      </tbody>
    </table>

    <h3>Governance</h3>
    <p>Roles: ${r.governance.roles.map(escapeHtml).join(", ")}</p>
    <table>
      <thead><tr><th>League size</th><th>Majority</th><th>Surplus majority</th></tr></thead>
      <tbody>
        ${r.governance.majority_votes.map((v) => `
          <tr><td>${v.league_size}</td><td>${v.majority}</td><td>${v.surplus_majority}</td></tr>
        `).join("")}
      </tbody>
    </table>
  `;
}

async function loadManagers() {
  const rows = await cached("managers", "/api/managers");
  const body = document.getElementById("managers-body");
  body.innerHTML = rows.map((m) => `
    <tr>
      <td>${escapeHtml(m.name)}</td>
      <td>${m.active ? "active" : "inactive"}</td>
      <td>${(m.aliases || []).map(escapeHtml).join(", ")}</td>
    </tr>
  `).join("");
}

async function loadHistory() {
  const h = await cached("history", "/api/history");
  const body = document.getElementById("history-body");
  body.innerHTML = h.awards.map((a) => `
    <tr>
      <td>${a.season}</td>
      <td>${escapeHtml(a.grand_champion)}</td>
      <td>${escapeHtml(a.sacko)}</td>
    </tr>
  `).join("");
}

async function loadAnnouncements() {
  const rows = await cached("announcements", "/api/announcements");
  const container = document.getElementById("announcements-body");
  container.innerHTML = rows.map((a) => `
    <article class="announcement">
      <h3>${escapeHtml(a.title)}</h3>
      <p class="meta">${escapeHtml(a.author)} &middot; ${new Date(a.posted_at).toLocaleString()}</p>
      <p>${escapeHtml(a.body)}</p>
    </article>
  `).join("");
}

async function loadSchedule() {
  const rows = await cached("schedule", "/api/schedule");
  const body = document.getElementById("schedule-body");
  body.innerHTML = rows.map((e) => {
    const when = e.recurring ? "recurring" : (e.week ? `week ${e.week}` : "");
    return `
      <tr>
        <td>${escapeHtml(e.label)}</td>
        <td>${when}</td>
        <td>${escapeHtml(e.detail)}</td>
      </tr>
    `;
  }).join("");
}

async function loadRivalries() {
  const [rows, managers] = await Promise.all([
    cached("rivalries", "/api/rivalries"),
    cached("managers", "/api/managers"),
  ]);
  const nameByID = Object.fromEntries(managers.map((m) => [m.id, m.name]));
  const container = document.getElementById("rivalries-body");
  if (rows.length === 0) {
    container.innerHTML = `<p class="empty">No rivalry data yet (needs live Sleeper history to compute).</p>`;
    return;
  }
  container.innerHTML = `
    <table>
      <thead><tr><th>Manager A</th><th>Manager B</th><th>Record</th><th>PF (A vs B)</th></tr></thead>
      <tbody>
        ${rows.map((r) => `
          <tr>
            <td>${escapeHtml(nameByID[r.manager_a_id] || r.manager_a_id)}</td>
            <td>${escapeHtml(nameByID[r.manager_b_id] || r.manager_b_id)}</td>
            <td>${r.wins_a}-${r.wins_b}-${r.ties}</td>
            <td>${r.points_for_a.toFixed(2)} vs ${r.points_for_b.toFixed(2)}</td>
          </tr>
        `).join("")}
      </tbody>
    </table>
  `;
}

const loaders = {
  standings: loadStandings,
  faab: loadFaab,
  matchups: () => loadMatchups(document.getElementById("week-input").value || 1),
  scoring: loadScoring,
  rules: loadRules,
  managers: loadManagers,
  history: loadHistory,
  announcements: loadAnnouncements,
  schedule: loadSchedule,
  rivalries: loadRivalries,
};

function showTab(name) {
  for (const btn of document.querySelectorAll("#tabs button")) {
    btn.classList.toggle("active", btn.dataset.tab === name);
  }
  for (const section of document.querySelectorAll("main > section")) {
    section.classList.toggle("active", section.dataset.tab === name);
  }
  loaders[name]?.().catch((e) => console.error(`loading ${name} failed:`, e));
}

function init() {
  document.getElementById("tabs").addEventListener("click", (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    showTab(btn.dataset.tab);
  });
  document.getElementById("week-load").addEventListener("click", () => {
    loadMatchups(document.getElementById("week-input").value || 1)
      .catch((e) => console.error("loading matchups failed:", e));
  });

  loadState();
  showTab("standings");
}

init();

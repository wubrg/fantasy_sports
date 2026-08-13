#!/usr/bin/env python3
"""Extract FantasyPros ECR into the draft room's normalized CSV.

FantasyPros' rankings pages render client-side, so there is no anonymous
export — these are hand-exported CSVs from a logged-in session, saved under
raw/fantasypros/<date>/. See internal/draft/data/README.md for the policy.

Each ranking VARIANT is exported as four separate views that must be merged:

  overview  (…-ecr-for-…)    POS, BYE, UPSIDE/BUST stars, SOS, ECR-vs-ADP
  ranks     (…-ecr-ranks-…)  BEST, WORST, AVG, STD.DEV of the expert ranks
  stats     (…-ecr-stats-…)  a fantasy-point total and its stat components
  notes     (…-ecr-notes-…)  a per-player prose writeup

Three variants are ingested and packed into one file, distinguished by a
`baseline` column the way Subvertadown packs its three baselines:

  consensus   every ranker (the free ECR)
  top10       consensus of last year's ten most accurate experts
  top20       consensus of last year's twenty most accurate experts

The sharp subsets diverge from consensus — last year's best experts rank
Bijan Robinson over Jahmyr Gibbs where the full consensus does the reverse —
so for every consensus player we also record `rank_vs_top10` / `rank_vs_top20`
= consensus rank minus the subset rank (positive = the sharps rate him higher).

Scoring — recomputed under the league's own rules like extract_ciely.py, so
FantasyPros is a second projection rather than an inherited number. Two
quantities are written and the delta between them kept auditable:

  fantasypros_points  their published total
  league_points       recomputed from the stat components under our scoring
  points_delta        league_points - fantasypros_points

CAVEAT, deliberate: the stats export carries no interception or fumble
column, so league_points omits the league's negative-play penalty. The delta
is therefore that penalty — small for skill players (0-2 pts) but large for
passers (Allen +14, Lamar +11, Purdy +12), where league_points reads high.
Ciely's INT-aware projection stays the QB source of record; FantasyPros'
value for QBs is the ranking, not the recomputed total.

Kickers are dropped: the league has no kicker slot, matching the QB/RB/WR/TE/
DST coverage of the other extractors. Team defenses are kept — they resolve
to Sleeper by the team abbreviation in the TEAM column.

Usage:
    python3 extract_fantasypros.py <raw-dir> <out.csv>

Dependency-free: stdlib only.
"""

import csv
import os
import re
import sys

# Hit or Miss scoring, matching extract_ciely.py. Interceptions and fumbles
# are absent from the export, so they cannot be applied here — see the module
# docstring's caveat.
SCORING = {
    "pass_yards": 0.04,
    "pass_td": 4.0,
    "rush_yards": 0.1,
    "rush_td": 6.0,
    "receptions": 0.5,
    "recv_yards": 0.1,
    "recv_td": 6.0,
}

# Variant label -> the filename prefix its four views share.
VARIANTS = {
    "consensus": "fantasypros",
    "top10": "fantasypros-2025-top10",
    "top20": "fantasypros-2025-top20",
}

POS_RE = re.compile(r"^([A-Za-z]+)(\d+)$")

# FantasyPros writes JAC where Sleeper's team defense is keyed JAX; left
# unfixed the Jaguars DST resolves to nothing, since defenses match on the
# abbreviation alone. Washington is deliberately NOT remapped: Sleeper is
# internally inconsistent and keys its Washington *defense* under WAS (not the
# WSH it uses for Washington skill players), so WAS is already correct here and
# rewriting it to WSH breaks the Commanders DST. Skill-player team is only a
# same-name tiebreak, so the WAS/WSH split there is harmless.
TEAM_FIXES = {"JAC": "JAX"}


def view_path(raw_dir, prefix, view):
    """The four views share a stem. Exports are named "…-for-2026.csv"; the
    overview drops the view word, so it reads "…-ecr-for-2026.csv" and the
    rest "…-ecr-<view>-for-2026.csv"."""
    stem = f"{prefix}-ecr-for-2026.csv" if view == "overview" else f"{prefix}-ecr-{view}-for-2026.csv"
    return os.path.join(raw_dir, stem)


def read_view(path):
    """Return (rows, header) with header cells lowercased and stripped."""
    if not os.path.exists(path):
        sys.exit(f"extract_fantasypros: missing {path}")
    with open(path, encoding="utf-8", errors="replace", newline="") as f:
        rows = list(csv.reader(f))
    if not rows:
        sys.exit(f"extract_fantasypros: empty {path}")
    header = [h.strip().lower() for h in rows[0]]
    return rows[1:], header


def col(header, *names):
    for n in names:
        if n in header:
            return header.index(n)
    return -1


def cell(row, idx):
    return row[idx].strip() if 0 <= idx < len(row) else ""


def as_int(s):
    m = re.search(r"-?\d+", s or "")
    return int(m.group(0)) if m else None


def as_float(s):
    try:
        return float((s or "").strip())
    except ValueError:
        return None


def lead_int(s):
    """First integer in a phrase like '5 out of 5 stars'."""
    m = re.match(r"\s*(\d+)", s or "")
    return int(m.group(1)) if m else None


def name_key(s):
    return re.sub(r"\s+", " ", (s or "")).strip()


def index_by_name(rows, header, name_col):
    """Map cleaned player name -> row, keeping the first of any duplicate."""
    out = {}
    for row in rows:
        name = name_key(cell(row, name_col))
        if name and name not in out:
            out[name] = row
    return out


def parse_variant(raw_dir, prefix):
    """Merge the four views of one variant into player dicts keyed by name."""
    ov_rows, ov = read_view(view_path(raw_dir, prefix, "overview"))
    rk_rows, rk = read_view(view_path(raw_dir, prefix, "ranks"))
    st_rows, st = read_view(view_path(raw_dir, prefix, "stats"))
    nt_rows, nt = read_view(view_path(raw_dir, prefix, "notes"))

    ov_name = col(ov, "player name", "player", "name")
    ranks = index_by_name(rk_rows, rk, col(rk, "player name", "player", "name"))
    stats = index_by_name(st_rows, st, col(st, "player name", "player", "name"))
    notes = index_by_name(nt_rows, nt, col(nt, "player name", "player", "name"))

    # Column indexes per view.
    c_rk = col(ov, "rk")
    c_tier = col(ov, "tiers", "tier")
    c_team = col(ov, "team")
    c_pos = col(ov, "pos", "position")
    c_bye = col(ov, "bye week", "bye")
    c_up = col(ov, "upside")
    c_bust = col(ov, "bust")
    c_sos = col(ov, "sos season", "sos")
    c_adp = col(ov, "ecr vs. adp", "ecr vs adp")

    r_best, r_worst = col(rk, "best"), col(rk, "worst")
    r_avg, r_std = col(rk, "avg.", "avg"), col(rk, "std.dev", "std.dev.", "stddev")

    # stats view has no POS column: RK,TIERS,PLAYER NAME,TEAM,FANTASYPTS,
    # PASS(YDS,TDS),REC(REC,YDS,TDS),RUSH(ATT,YDS,TDS).
    s_pts = col(st, "fantasypts")
    # The three YDS/TDS pairs repeat by name, so index them positionally from
    # FANTASYPTS: pass yds/td, rec/yds/td, rush att/yds/td.
    s0 = s_pts
    s_pass_yds, s_pass_td = s0 + 1, s0 + 2
    s_rec, s_rec_yds, s_rec_td = s0 + 3, s0 + 4, s0 + 5
    s_rush_att, s_rush_yds, s_rush_td = s0 + 6, s0 + 7, s0 + 8

    n_notes = col(nt, "notes")

    players = {}
    for row in ov_rows:
        name = name_key(cell(row, ov_name))
        if not name:
            continue
        pos_raw = cell(row, c_pos)
        m = POS_RE.match(pos_raw)
        if not m:
            continue
        position, pos_rank = m.group(1).upper(), int(m.group(2))
        if position == "K":
            continue  # league has no kicker slot

        s = stats.get(name)
        comp = {k: 0.0 for k in ("pass_yards", "pass_td", "receptions",
                                 "recv_yards", "recv_td", "rush_yards", "rush_td")}
        fantasypros_points = None
        if s is not None:
            fantasypros_points = as_float(cell(s, s_pts)) or 0.0
            comp["pass_yards"] = as_float(cell(s, s_pass_yds)) or 0.0
            comp["pass_td"] = as_float(cell(s, s_pass_td)) or 0.0
            comp["receptions"] = as_float(cell(s, s_rec)) or 0.0
            comp["recv_yards"] = as_float(cell(s, s_rec_yds)) or 0.0
            comp["recv_td"] = as_float(cell(s, s_rec_td)) or 0.0
            comp["rush_yards"] = as_float(cell(s, s_rush_yds)) or 0.0
            comp["rush_td"] = as_float(cell(s, s_rush_td)) or 0.0

        league_points = sum(SCORING[k] * comp[k] for k in SCORING)
        # DST rows carry no stat components to recompute from, so the
        # published number stands and the delta is zero — as in extract_ciely.
        if position == "DST" and fantasypros_points is not None:
            league_points = fantasypros_points

        rrow = ranks.get(name)
        nrow = notes.get(name)
        adp = cell(row, c_adp)

        players[name] = {
            "position": position,
            "pos_rank": pos_rank,
            "player": name,
            "team": TEAM_FIXES.get(cell(row, c_team).upper(), cell(row, c_team).upper()),
            "bye": as_int(cell(row, c_bye)),
            "overall_rank": as_int(cell(row, c_rk)),
            "tier": as_int(cell(row, c_tier)),
            "best": as_int(cell(rrow, r_best)) if rrow else None,
            "worst": as_int(cell(rrow, r_worst)) if rrow else None,
            "avg_rank": as_float(cell(rrow, r_avg)) if rrow else None,
            "stddev": as_float(cell(rrow, r_std)) if rrow else None,
            "ecr_vs_adp": as_int(adp) if adp not in ("", "-") else None,
            "upside": lead_int(cell(row, c_up)),
            "bust": lead_int(cell(row, c_bust)),
            "sos": lead_int(cell(row, c_sos)),
            "fantasypros_points": round(fantasypros_points, 2) if fantasypros_points is not None else None,
            "league_points": round(league_points, 2),
            "points_delta": round(league_points - fantasypros_points, 2) if fantasypros_points is not None else None,
            "pass_yards": round(comp["pass_yards"], 1),
            "pass_td": round(comp["pass_td"], 2),
            "rush_yards": round(comp["rush_yards"], 1),
            "rush_td": round(comp["rush_td"], 2),
            "receptions": round(comp["receptions"], 1),
            "recv_yards": round(comp["recv_yards"], 1),
            "recv_td": round(comp["recv_td"], 2),
        }
    return players


FIELDS = [
    "source", "baseline", "position", "pos_rank", "player", "team", "bye",
    "overall_rank", "tier", "best", "worst", "avg_rank", "stddev", "ecr_vs_adp",
    "upside", "bust", "sos", "fantasypros_points", "league_points", "points_delta",
    "pass_yards", "pass_td", "rush_yards", "rush_td", "receptions", "recv_yards",
    "recv_td", "rank_vs_top10", "rank_vs_top20", "notes",
]


def blank(v):
    return "" if v is None else v


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    raw_dir, out_path = sys.argv[1], sys.argv[2]

    variants = {label: parse_variant(raw_dir, prefix) for label, prefix in VARIANTS.items()}
    consensus = variants["consensus"]
    top10 = {n: p["overall_rank"] for n, p in variants["top10"].items()}
    top20 = {n: p["overall_rank"] for n, p in variants["top20"].items()}

    out_rows = []
    for label, players in variants.items():
        for name, p in players.items():
            row = {f: "" for f in FIELDS}
            row.update({k: blank(v) for k, v in p.items()})
            row["source"] = "fantasypros"
            row["baseline"] = label
            row["notes"] = ""  # notes filled below only for consensus
            # Divergence lives on the consensus spine, where the reader looks.
            if label == "consensus":
                cr = p["overall_rank"]
                if cr is not None and name in top10 and top10[name] is not None:
                    row["rank_vs_top10"] = cr - top10[name]
                if cr is not None and name in top20 and top20[name] is not None:
                    row["rank_vs_top20"] = cr - top20[name]
            out_rows.append((label, name, row))

    # Attach notes (consensus view carries the writeups we keep).
    notes_by_name = {}
    ov_rows, ov = read_view(view_path(raw_dir, VARIANTS["consensus"], "notes"))
    nname = col(ov, "player name", "player", "name")
    nnotes = col(ov, "notes")
    for r in ov_rows:
        notes_by_name[name_key(cell(r, nname))] = cell(r, nnotes)
    for label, name, row in out_rows:
        if label == "consensus":
            row["notes"] = notes_by_name.get(name, "")

    with open(out_path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=FIELDS)
        w.writeheader()
        w.writerows(row for _, _, row in out_rows)

    # Report, per variant, like the sibling extractors.
    for label in VARIANTS:
        players = variants[label]
        n = len(players)
        qbs = [p for p in players.values() if p["position"] == "QB" and p["points_delta"] is not None]
        skill = [p for p in players.values()
                 if p["position"] in ("RB", "WR", "TE") and p["points_delta"] is not None]
        qb_gap = sum(p["points_delta"] for p in qbs) / len(qbs) if qbs else 0.0
        sk_gap = sum(p["points_delta"] for p in skill) / len(skill) if skill else 0.0
        print(f"  {label:9} {n:3} players  "
              f"(recompute delta: QB {qb_gap:+.1f}, skill {sk_gap:+.1f} — the missing INT/fumble penalty)")

    # Divergence summary from the consensus rows.
    moved = [(name, r["rank_vs_top10"]) for _, name, r in out_rows
             if r["baseline"] == "consensus" and isinstance(r["rank_vs_top10"], int)]
    up = sum(1 for _, d in moved if d > 0)
    down = sum(1 for _, d in moved if d < 0)
    print(f"wrote {len(out_rows)} rows to {out_path}")
    print(f"  top10 divergence: {up} players the sharps rank higher, {down} lower, "
          f"{len(moved) - up - down} even (of {len(moved)} shared)")


if __name__ == "__main__":
    main()

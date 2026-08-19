#!/usr/bin/env python3
"""Extract FantasyPros ECR into the draft room's normalized CSV.

FantasyPros' rankings pages render client-side, so there is no anonymous
export — these are hand-exported CSVs from a logged-in session, saved under
raw/fantasypros/<date>/. See internal/draft/data/README.md for the policy.

Each ranking VARIANT is exported as several views that must be merged:

  overview  (…-ecr-for-…)    POS, BYE, UPSIDE/BUST stars, SOS, ECR-vs-ADP
  ranks     (…-ecr-ranks-…)  BEST, WORST, AVG, STD.DEV of the expert ranks
  notes     (…-ecr-notes-…)  a per-player prose writeup
  stats     (…-ecr-stats-…)  LAST SEASON's actual production — see below

Three variants are ingested and packed into one file, distinguished by a
`baseline` column the way Subvertadown packs its three baselines:

  consensus   every ranker (the free ECR)
  top10       consensus of last year's ten most accurate experts
  top20       consensus of last year's twenty most accurate experts

The sharp subsets diverge from consensus — last year's best experts rank
Bijan Robinson over Jahmyr Gibbs where the full consensus does the reverse —
so for every consensus player we also record `rank_vs_top10` / `rank_vs_top20`
= consensus rank minus the subset rank (positive = the sharps rate him higher).

PROJECTIONS come from a separate set of exports, not from the ECR views. This
matters, and used to be wrong: the `…-ecr-stats-…` view is the "last season"
reference column FantasyPros shows beside its rankings, so it holds 2025
ACTUALS, not a 2026 projection. Scoring it produced a column that floored every
2026 rookie at nothing (Jeremiyah Love, ranked RB16, carried zeroes) and
underpriced everyone who missed time, while reading as a second projection. The
projections exports replace it:

  …-projections-qb-hilo-…   QB, with a high and a low beside each projection
  …-projections-flx-hilo-…  RB/WR/TE, ditto, with a POS column
  …-projections-{qb,rb,wr,te}-…  the same numbers without the band

The Hi/Low pair is primary — between them they cover every position and carry
every stat component. The per-position exports are read only to pick up a
player the Hi/Low exports missed. In the Hi/Low files each player is three
consecutive rows: the named row, then an unnamed "high" row, then a "low" one.

Scoring — recomputed under the league's own rules like extract_ciely.py, so
FantasyPros is a second projection rather than an inherited number. The high
and low are recomputed the same way, which is more faithful than scaling their
published total: a passer's low carries MORE interceptions, not fewer, and only
recomputation sees that.

  fantasypros_points  their published total
  league_points       recomputed from the stat components under our scoring
  points_low/high     the same recomputation on the low and high rows
  interceptions       published, and scored at the league's -1
  fumbles_lost        published, and deliberately NOT scored — see below
  points_delta        league_points - fantasypros_points

FUMBLES: the projections carry a fumbles-lost column, and it is recorded here
but kept out of SCORING on purpose. SCORING has to stay key-for-key identical
to extract_ciely.py, whose workbook has no fumble term. The whole point of the
FP column on the board is that it is solved against the same pool as Value in
the same way, so a disagreement is about the player; applying a scoring
component to one source and not the other would bias FP down for fumble-prone
players with nothing on the board to reveal it.

Kickers are dropped: the league has no kicker slot, matching the QB/RB/WR/TE/
DST coverage of the other extractors. Team defenses are kept and are the one
exception to the above — the projections exports have no DST rows, so a DST
keeps the published total from the stats view, which is all that view offers
for a defense anyway (there are no components to recompute). This league starts
no DST, so those rows are carried for completeness rather than used.

Usage:
    python3 extract_fantasypros.py <raw-dir> <out.csv>

Dependency-free: stdlib only.
"""

import csv
import os
import re
import sys

# Hit or Miss scoring, matching extract_ciely.py key for key. Interceptions are
# published in the projections exports and scored at the league's -1 (Ciely's
# workbook uses -2; both extractors restate under our rules). Fumbles are
# available but excluded on purpose — see the module docstring.
SCORING = {
    "pass_yards": 0.04,
    "pass_td": 4.0,
    "interceptions": -1.0,
    "rush_yards": 0.1,
    "rush_td": 6.0,
    "receptions": 0.5,
    "recv_yards": 0.1,
    "recv_td": 6.0,
}

# Every stat component read out of a projections row. A file that does not carry
# one (a QB export has no receiving, a flex export no passing) leaves it zero.
COMPONENTS = tuple(SCORING)

# Variant label -> the filename prefix its ECR views share.
VARIANTS = {
    "consensus": "fantasypros",
    "top10": "fantasypros-2025-top10",
    "top20": "fantasypros-2025-top20",
}

# Projections exports: filename -> component/index map. The exports carry
# repeated YDS/TDS headers, so columns are taken positionally. Indexes not
# listed are columns the league does not score (pass attempts, completions,
# rush attempts).
#
# qb   : Player Team ATT CMP YDS TDS INTS ATT YDS TDS FL FPTS
# flx  : Player Team POS ATT YDS TDS REC YDS TDS FL FPTS   (rush, then receiving)
# rb   : Player Team ATT YDS TDS REC YDS TDS FL FPTS
# wr   : Player Team REC YDS TDS ATT YDS TDS FL FPTS       (receiving first)
# te   : Player Team REC YDS TDS FL FPTS
QB_COLS = {"pass_yards": 4, "pass_td": 5, "interceptions": 6,
           "rush_yards": 8, "rush_td": 9, "fumbles_lost": 10, "points": 11}
FLX_COLS = {"rush_yards": 4, "rush_td": 5, "receptions": 6, "recv_yards": 7,
            "recv_td": 8, "fumbles_lost": 9, "points": 10}
RB_COLS = {"rush_yards": 3, "rush_td": 4, "receptions": 5, "recv_yards": 6,
           "recv_td": 7, "fumbles_lost": 8, "points": 9}
WR_COLS = {"receptions": 2, "recv_yards": 3, "recv_td": 4, "rush_yards": 6,
           "rush_td": 7, "fumbles_lost": 8, "points": 9}
TE_COLS = {"receptions": 2, "recv_yards": 3, "recv_td": 4,
           "fumbles_lost": 5, "points": 6}

# Primary first, then the per-position fills. Order matters: the first file to
# name a player wins, so the banded exports are read before the flat ones.
PROJECTION_SOURCES = [
    ("fantasypros-projections-qb-hilo-for-2026.csv", QB_COLS, True),
    ("fantasypros-projections-flx-hilo-for-2026.csv", FLX_COLS, True),
    ("fantasypros-projections-qb-for-2026.csv", QB_COLS, False),
    ("fantasypros-projections-rb-for-2026.csv", RB_COLS, False),
    ("fantasypros-projections-wr-for-2026.csv", WR_COLS, False),
    ("fantasypros-projections-te-for-2026.csv", TE_COLS, False),
]

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
    """The ECR views share a stem. Exports are named "…-for-2026.csv"; the
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


def score_row(row, cols):
    """(components, fumbles_lost, published_total, league_points) for one
    projection row under this league's scoring."""
    comp = {k: 0.0 for k in COMPONENTS}
    for k in COMPONENTS:
        if k in cols:
            comp[k] = as_float(cell(row, cols[k])) or 0.0
    fumbles = as_float(cell(row, cols["fumbles_lost"])) or 0.0
    published = as_float(cell(row, cols["points"]))
    return comp, fumbles, published, sum(SCORING[k] * comp[k] for k in SCORING)


def parse_projections(raw_dir):
    """Map player name -> projection, from the projections exports.

    In a Hi/Low export a player is three consecutive rows: the named row, then
    an unnamed row tagged "high", then one tagged "low". The band is therefore
    attached to whichever player was named most recently. Every file also has a
    blank sub-header beneath the real one, which falls out of the same check
    since it names nobody.
    """
    out = {}
    for fname, cols, banded in PROJECTION_SOURCES:
        path = os.path.join(raw_dir, fname)
        if not os.path.exists(path):
            # The per-position fills are optional; the banded pair is not.
            if banded:
                sys.exit(f"extract_fantasypros: missing {path}")
            continue
        with open(path, encoding="utf-8", errors="replace", newline="") as f:
            rows = list(csv.reader(f))[1:]
        current = None
        for row in rows:
            name = name_key(cell(row, 0))
            tag = cell(row, 1).lower()
            if name:
                current = name
                if name in out:
                    continue  # an earlier, banded export already has him
                comp, fumbles, published, pts = score_row(row, cols)
                out[name] = {
                    "components": comp,
                    "fumbles_lost": fumbles,
                    "published": published,
                    "points": pts,
                    "points_high": None,
                    "points_low": None,
                }
            elif banded and tag in ("high", "low") and current in out:
                _, _, _, pts = score_row(row, cols)
                out[current]["points_" + tag] = pts
    return out


def parse_variant(raw_dir, prefix, projections):
    """Merge one variant's ECR views with the projections into player dicts."""
    ov_rows, ov = read_view(view_path(raw_dir, prefix, "overview"))
    rk_rows, rk = read_view(view_path(raw_dir, prefix, "ranks"))
    st_rows, st = read_view(view_path(raw_dir, prefix, "stats"))

    ov_name = col(ov, "player name", "player", "name")
    ranks = index_by_name(rk_rows, rk, col(rk, "player name", "player", "name"))
    stats = index_by_name(st_rows, st, col(st, "player name", "player", "name"))

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

    # The stats view is read for one thing only: a DST's published total, since
    # the projections exports have no defenses. Everything else comes from the
    # projections — this view is last season's actuals.
    s_pts = col(st, "fantasypts")

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

        proj = projections.get(name)
        comp = {k: 0.0 for k in COMPONENTS}
        fumbles = None
        published = None
        # Left empty, not zeroed, when FantasyPros ranks a player it does not
        # project. Zero would read as a player who scores nothing and floor him
        # at a dollar on the board; empty reads as "no FP opinion", which is
        # what it is. The board draws those as "—".
        league_points = None
        low = high = None
        if proj is not None:
            comp = proj["components"]
            fumbles = proj["fumbles_lost"]
            published = proj["published"]
            league_points = proj["points"]
            low, high = proj["points_low"], proj["points_high"]

        if position == "DST":
            # No projection exists for a defense; the stats view's single total
            # is all there is, and there are no components to recompute from.
            s = stats.get(name)
            if s is not None:
                published = as_float(cell(s, s_pts)) or 0.0
                league_points = published

        players[name] = {
            "position": position,
            "pos_rank": pos_rank,
            "player": name,
            "team": TEAM_FIXES.get(cell(row, c_team).upper(), cell(row, c_team).upper()),
            "bye": as_int(cell(row, c_bye)),
            "overall_rank": as_int(cell(row, c_rk)),
            "tier": as_int(cell(row, c_tier)),
            "best": as_int(cell(ranks[name], r_best)) if name in ranks else None,
            "worst": as_int(cell(ranks[name], r_worst)) if name in ranks else None,
            "avg_rank": as_float(cell(ranks[name], r_avg)) if name in ranks else None,
            "stddev": as_float(cell(ranks[name], r_std)) if name in ranks else None,
            "ecr_vs_adp": as_int(cell(row, c_adp)) if cell(row, c_adp) not in ("", "-") else None,
            "upside": lead_int(cell(row, c_up)),
            "bust": lead_int(cell(row, c_bust)),
            "sos": lead_int(cell(row, c_sos)),
            "fantasypros_points": round(published, 2) if published is not None else None,
            "league_points": round(league_points, 2) if league_points is not None else None,
            "points_low": round(low, 2) if low is not None else None,
            "points_high": round(high, 2) if high is not None else None,
            "interceptions": round(comp["interceptions"], 2),
            "fumbles_lost": round(fumbles, 2) if fumbles is not None else None,
            "points_delta": (round(league_points - published, 2)
                             if published is not None and league_points is not None else None),
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
    "upside", "bust", "sos", "fantasypros_points", "league_points",
    "points_low", "points_high", "interceptions", "fumbles_lost", "points_delta",
    "pass_yards", "pass_td", "rush_yards", "rush_td", "receptions", "recv_yards",
    "recv_td", "rank_vs_top10", "rank_vs_top20", "notes",
]


def blank(v):
    return "" if v is None else v


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    raw_dir, out_path = sys.argv[1], sys.argv[2]

    projections = parse_projections(raw_dir)
    variants = {label: parse_variant(raw_dir, prefix, projections)
                for label, prefix in VARIANTS.items()}
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
    print(f"  projections: {len(projections)} players "
          f"({sum(1 for p in projections.values() if p['points_high'] is not None)} with a high/low band)")
    for label in VARIANTS:
        players = variants[label]
        n = len(players)
        scored = [p for p in players.values() if p["position"] != "DST"]
        missing = [p for p in scored if p["fantasypros_points"] is None]
        qbs = [p for p in players.values() if p["position"] == "QB" and p["points_delta"] is not None]
        skill = [p for p in players.values()
                 if p["position"] in ("RB", "WR", "TE") and p["points_delta"] is not None]
        qb_gap = sum(p["points_delta"] for p in qbs) / len(qbs) if qbs else 0.0
        sk_gap = sum(p["points_delta"] for p in skill) / len(skill) if skill else 0.0
        print(f"  {label:9} {n:3} players, {len(missing):3} without a projection  "
              f"(recompute delta: QB {qb_gap:+.1f}, skill {sk_gap:+.1f} — "
              f"INTs published, residual is fumbles, which we do not score)")

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

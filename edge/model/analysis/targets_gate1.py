#!/usr/bin/env python3
"""Is targets / pass-attempts fittable as an OUTCOME? A go/no-go measurement.

Every outcome the grid fits today separates OPPORTUNITY (a volume signal --
targets, carries, attempts) from PRODUCTION (yards or a count downstream of that
volume). The scenario axis then splits production into q and r. Targets and pass
attempts break that shape: if the volume IS the outcome, the stat and its own
opportunity are the same column, and there is no distinct opportunity input left
to condition on.

That does not make them unfittable -- the grid conditions on the player's own
prior-mean OUTPUT and a role trend, and for a volume outcome those are just the
prior mean of the volume and its trend, which is well defined. The open question
is whether anything beyond the player's own lagged volume carries information
about this week's count: specifically, whether a team-implied pass-volume signal
read off the posted line (total and spread -- exogenous, set before kickoff, not
derived from the player's realized stats) separates an individual player's
target / attempt count enough to be worth a conditioning axis.

So, mirroring proe.py --gate1, three questions, none needing a populated grid:

  PERSISTENCE   does a player's own prior-games mean predict this week's volume
                at all? If not, the baseline axis is meaningless and nothing is
                fittable.
  SEPARATE AXIS does baseline + trend (the same prior-only pair the grid already
                computes) carry the signal alone, or does a line-derived volume
                proxy add R-squared over it? If the proxy adds ~nothing, there
                is no opportunity axis beyond the player's own lag.
  USABLE PROXY  does the line-derived proxy actually separate a single player's
                count -- top vs bottom implied-volume quartile -- or is it too
                team-level and diffuse to move one player?

The bar for adding an Outcome: persistence is real AND a usable non-circular
signal exists. If the proxy is diffuse at the player level, the honest finding
is that targets/attempts add nothing a conditioning grid can price, and no
Outcome is added. See FINDINGS.md section 19.

Dependency-free beyond the sibling analysis modules. stdlib only.

Usage:
    python3 targets_gate1.py          # both modes
"""

from __future__ import annotations

import csv
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

from utilization_lag import ols_clustered

CACHE = Path(__file__).resolve().parent.parent / "data" / "raw"
FIRST, LAST = 2009, 2025
MIN_PRIOR_GAMES = 4
TREND_WINDOW = 2


def num(s) -> float:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return 0.0
    try:
        return float(s)
    except ValueError:
        return 0.0


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def load_games() -> dict:
    """(season, week, team) -> (total_line, implied team margin from the spread).

    nflverse `spread_line` is the home spread with positive meaning the home
    team is favored by that many points, so the home team's implied margin is
    +spread_line and the away team's is -spread_line. The implied margin is the
    line's statement about game script: a negative one is an underdog, the team
    the market expects to be throwing to catch up.
    """
    path = CACHE / "games.csv"
    out = {}
    for r in csv.DictReader(path.open()):
        if not (r["total_line"].strip() and r["spread_line"].strip()):
            continue
        s, w = int(num(r["season"])), int(num(r["week"]))
        total = num(r["total_line"])
        spread = num(r["spread_line"])  # home favored by this many
        out[(s, w, r["home_team"])] = (total, spread)
        out[(s, w, r["away_team"])] = (total, -spread)
    return out


def load_player_weeks(positions: set, stat_field: str) -> list[dict]:
    rows = []
    for season in range(FIRST, LAST + 1):
        path = CACHE / f"stats_player_week_{season}.csv"
        if not path.exists():
            raise SystemExit(f"{path} not found -- run ingest/nflverse.py")
        for r in csv.DictReader(path.open()):
            if r.get("season_type") != "REG" or r.get("position") not in positions:
                continue
            rows.append(
                {
                    "season": int(num(r["season"])),
                    "week": int(num(r["week"])),
                    "player": r.get("player_id", ""),
                    "team": r.get("team") or r.get("recent_team", ""),
                    "stat": num(r.get(stat_field)),
                }
            )
    return rows


def build(rows: list[dict], games: dict, min_baseline: float) -> list[dict]:
    """One observation per player-game, prior information only.

    baseline = prior-games mean of the volume itself; trend = last-two-games
    mean minus baseline. Exactly the prior-only pair utilization_lag and
    fit_conditionals compute, applied to the volume as its own outcome.
    """
    by_player = defaultdict(list)
    for r in rows:
        by_player[(r["player"], r["season"])].append(r)

    obs = []
    for _key, g in by_player.items():
        g.sort(key=lambda x: x["week"])
        for i, x in enumerate(g):
            if i < MIN_PRIOR_GAMES:
                continue
            prior = g[:i]
            baseline = st.mean(p["stat"] for p in prior)
            if baseline < min_baseline:
                continue
            recent = st.mean(p["stat"] for p in prior[-TREND_WINDOW:])
            ctx = games.get((x["season"], x["week"], x["team"]))
            if ctx is None:
                continue
            total, imp_margin = ctx
            obs.append(
                {
                    "player": x["player"],
                    "season": x["season"],
                    "stat": x["stat"],
                    "baseline": baseline,
                    "trend": recent - baseline,
                    "total": total,
                    "imp_margin": imp_margin,
                }
            )
    return obs


def gate(label: str, positions: set, stat_field: str, min_baseline: float) -> None:
    games = load_games()
    rows = load_player_weeks(positions, stat_field)
    obs = build(rows, games, min_baseline)
    y = [o["stat"] for o in obs]
    g = [o["player"] for o in obs]

    print(f"\n{'=' * 72}\n{label}: {stat_field} as the OUTCOME  "
          f"(positions {sorted(positions)})\n{'=' * 72}")
    print(f"player-weeks {FIRST}-{LAST}: {len(rows)}   usable observations: {len(obs)}   "
          f"players: {len(set(g))}")
    print(f"realized {stat_field}: mean {st.mean(y):.2f}  sd {st.pstdev(y):.2f}")

    # 1. PERSISTENCE -----------------------------------------------------------
    priors = [o["baseline"] for o in obs]
    r = corr(priors, y)
    b, se, r2 = ols_clustered([[1.0, o["baseline"]] for o in obs], y, g)
    t = b[1] / se[1] if se[1] > 0 else 0.0
    print("\nPERSISTENCE  (prior-games mean vs this week; near zero = unforecastable)")
    print(f"  corr(prior mean, realized)   r = {r:+.3f}   n={len(obs)}")
    print(f"  regression  this = {b[0]:.2f} + {b[1]:.3f}*prior   "
          f"t={t:.1f}  R2={r2:.4f}  {'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    # 2. IS A SEPARATE OPPORTUNITY AXIS NEEDED ---------------------------------
    _, _, r2_b = ols_clustered([[1.0, o["baseline"]] for o in obs], y, g)
    _, _, r2_bt = ols_clustered([[1.0, o["baseline"], o["trend"]] for o in obs], y, g)
    bb, sse, r2_btv = ols_clustered(
        [[1.0, o["baseline"], o["trend"], o["total"], o["imp_margin"]] for o in obs], y, g)
    t_total = bb[3] / sse[3] if sse[3] > 0 else 0.0
    t_marg = bb[4] / sse[4] if sse[4] > 0 else 0.0
    print("\nSEPARATE AXIS  (does a line-derived volume proxy add over baseline+trend?)")
    print(f"  R2  baseline only                 {r2_b:.4f}")
    print(f"  R2  baseline + trend              {r2_bt:.4f}   (trend dR2 {r2_bt - r2_b:+.4f})")
    print(f"  R2  + total + implied margin      {r2_btv:.4f}   (proxy dR2 {r2_btv - r2_bt:+.4f})")
    print(f"      posted total     beta {bb[3]:+.3f}  t {t_total:+.1f}  "
          f"{'SIGNIFICANT' if abs(t_total) > 1.96 else 'null'}")
    print(f"      implied margin   beta {bb[4]:+.3f}  t {t_marg:+.1f}  "
          f"{'SIGNIFICANT' if abs(t_marg) > 1.96 else 'null'}")
    print("      (implied margin < 0 = underdog; negative beta means dogs throw more)")

    # 3. USABLE PROXY ----------------------------------------------------------
    # Does the line-derived proxy move a SINGLE player's count, net of who the
    # player is? Residualize the volume on the player's own baseline+trend first,
    # then split the residual by the proxy quartile -- so the comparison is "same
    # player profile, richer vs poorer implied pass environment", not "alphas
    # land in high-total games".
    beta_bt, _, _ = ols_clustered(
        [[1.0, o["baseline"], o["trend"]] for o in obs], y, g)
    for o in obs:
        pred = beta_bt[0] + beta_bt[1] * o["baseline"] + beta_bt[2] * o["trend"]
        o["resid"] = o["stat"] - pred
    # Implied team pass volume index: more volume when the total is high AND the
    # team is an underdog (negative implied margin). Higher = pass-richer.
    for o in obs:
        o["vol_index"] = o["total"] - o["imp_margin"]
    vals = sorted(o["vol_index"] for o in obs)
    q1, q3 = vals[len(vals) // 4], vals[3 * len(vals) // 4]
    hi = [o for o in obs if o["vol_index"] >= q3]
    lo = [o for o in obs if o["vol_index"] <= q1]
    print("\nUSABLE PROXY  (residual volume: top vs bottom implied-pass-env quartile)")
    print(f"  raw mean {stat_field}      hi {st.mean([o['stat'] for o in hi]):.2f}   "
          f"lo {st.mean([o['stat'] for o in lo]):.2f}   "
          f"gap {st.mean([o['stat'] for o in hi]) - st.mean([o['stat'] for o in lo]):+.2f}")
    print(f"  residual (own-profile removed)  hi {st.mean([o['resid'] for o in hi]):+.3f}   "
          f"lo {st.mean([o['resid'] for o in lo]):+.3f}   "
          f"gap {st.mean([o['resid'] for o in hi]) - st.mean([o['resid'] for o in lo]):+.3f}")
    print(f"  residual sd {st.pstdev([o['resid'] for o in obs]):.2f}  -- the gap is "
          f"what a scenario split on the line could hope to recover per player")


def main(argv) -> int:
    gate("PASS-CATCHERS", {"WR", "TE", "RB"}, "targets", 1.0)
    gate("QUARTERBACKS", {"QB"}, "attempts", 10.0)
    print("\nVerdict is read off the three blocks above and recorded in FINDINGS.md 19.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

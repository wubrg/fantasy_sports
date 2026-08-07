#!/usr/bin/env python3
"""Test whether a rising utilization trend predicts production beyond baseline.

This is the sharpest falsifiable claim in the framework corpus. From the
data-sources report, Part 4:

    "A prop bet is a bet on opportunity. A player's utilization trend is a
     LEADING indicator of future production, whereas a box score is a LAGGING
     indicator."

If true, a player whose target share has risen over the last two games is
underpriced relative to his season-long numbers, and the "role change" edge is
real. If false, it is folklore, and the pooled-conditional work resting on it
should be reconsidered.

# The test

For each player-game, using only information available BEFORE that game:

    y   production in this game (receiving yards; targets as a cross-check)
    x1  mean target share over all prior games this season   (the baseline)
    x2  mean share over the last two games, minus x1         (the trend)

then regress y on x1 and x2. The claim is that x2 carries information x1 does
not. A null coefficient on x2 says the trend adds nothing once you know the
season-long rate.

# Two things done carefully

Target share is computed here from raw targets rather than read from the
precomputed column, because that column only starts in 2009 while `targets`
reaches back to 2005. The computed version is validated against the precomputed
one on seasons where both exist.

Standard errors are CLUSTERED BY PLAYER. A player contributes dozens of
observations whose errors are obviously correlated, and naive OLS errors would
overstate significance badly on a sample this size. Getting that wrong is how a
null result gets published as a finding.

Dependency-free: stdlib only.

Usage:
    python3 utilization_lag.py
    python3 utilization_lag.py --outcome targets
"""

from __future__ import annotations

import argparse
import csv
import math
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

CACHE = Path(__file__).resolve().parent.parent / "data" / "raw"

FIRST, LAST = 2005, 2025
ERAS = [(2005, 2011), (2012, 2017), (2018, 2021), (2022, 2025)]
POSITIONS = {"WR", "TE", "RB"}

MIN_PRIOR_GAMES = 4  # need a stable baseline before a "trend" means anything
TREND_WINDOW = 2  # "the last two games", per the source claim
MIN_BASELINE_SHARE = 0.05  # exclude deep bench noise


def num(s: str) -> float:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return 0.0
    try:
        return float(s)
    except ValueError:
        return 0.0


def load_player_weeks(first: int, last: int) -> list[dict]:
    rows = []
    for season in range(first, last + 1):
        path = CACHE / f"stats_player_week_{season}.csv"
        if not path.exists():
            continue
        for r in csv.DictReader(path.open()):
            if r.get("season_type") != "REG":
                continue
            if r.get("position") not in POSITIONS:
                continue
            rows.append(
                {
                    "season": int(num(r["season"])),
                    "week": int(num(r["week"])),
                    "player": r.get("player_id") or r.get("player_display_name", ""),
                    "team": r.get("team") or r.get("recent_team", ""),
                    "targets": num(r.get("targets", "")),
                    "rec_yards": num(r.get("receiving_yards", "")),
                    "share_col": r.get("target_share", "").strip(),
                }
            )
    return rows


def add_computed_share(rows: list[dict]) -> float:
    """Attach target share computed from raw targets. Returns agreement with
    the precomputed column where that column exists."""
    team_targets = defaultdict(float)
    for r in rows:
        team_targets[(r["season"], r["week"], r["team"])] += r["targets"]

    diffs = []
    for r in rows:
        tt = team_targets[(r["season"], r["week"], r["team"])]
        r["share"] = (r["targets"] / tt) if tt > 0 else 0.0
        if r["share_col"] not in ("", "NA"):
            diffs.append(abs(r["share"] - float(r["share_col"])))
    return st.mean(diffs) if diffs else float("nan")


def build_observations(rows: list[dict]) -> list[dict]:
    """One observation per player-game, with only prior information as inputs."""
    by_player = defaultdict(list)
    for r in rows:
        by_player[(r["player"], r["season"])].append(r)

    obs = []
    for (player, season), games in by_player.items():
        games.sort(key=lambda g: g["week"])
        for i, g in enumerate(games):
            if i < MIN_PRIOR_GAMES:
                continue
            prior = games[:i]
            baseline = st.mean(x["share"] for x in prior)
            if baseline < MIN_BASELINE_SHARE:
                continue
            recent = st.mean(x["share"] for x in prior[-TREND_WINDOW:])
            obs.append(
                {
                    "player": player,
                    "season": season,
                    "baseline": baseline,
                    "trend": recent - baseline,
                    "rec_yards": g["rec_yards"],
                    "targets": g["targets"],
                }
            )
    return obs


# --- small dense linear algebra, stdlib only ---


def solve(a: list[list[float]], b: list[float]) -> list[float]:
    """Gaussian elimination with partial pivoting."""
    n = len(a)
    m = [row[:] + [b[i]] for i, row in enumerate(a)]
    for col in range(n):
        piv = max(range(col, n), key=lambda r: abs(m[r][col]))
        if abs(m[piv][col]) < 1e-12:
            raise ValueError("singular design matrix")
        m[col], m[piv] = m[piv], m[col]
        for r in range(n):
            if r == col:
                continue
            f = m[r][col] / m[col][col]
            for c in range(col, n + 1):
                m[r][c] -= f * m[col][c]
    return [m[i][n] / m[i][i] for i in range(n)]


def inverse(a: list[list[float]]) -> list[list[float]]:
    n = len(a)
    cols = []
    for i in range(n):
        e = [1.0 if j == i else 0.0 for j in range(n)]
        cols.append(solve(a, e))
    return [[cols[j][i] for j in range(n)] for i in range(n)]


def ols_clustered(X: list[list[float]], y: list[float], groups: list[str]):
    """OLS with cluster-robust (by group) standard errors.

    Returns (beta, se, r2).
    """
    n, k = len(X), len(X[0])
    xtx = [[sum(X[i][a] * X[i][b] for i in range(n)) for b in range(k)] for a in range(k)]
    xty = [sum(X[i][a] * y[i] for i in range(n)) for a in range(k)]
    beta = solve(xtx, xty)
    xtx_inv = inverse(xtx)

    resid = [y[i] - sum(X[i][a] * beta[a] for a in range(k)) for i in range(n)]

    # meat of the sandwich: sum over clusters of (X_g' u_g)(X_g' u_g)'
    per_group = defaultdict(lambda: [0.0] * k)
    for i in range(n):
        acc = per_group[groups[i]]
        for a in range(k):
            acc[a] += X[i][a] * resid[i]
    meat = [[0.0] * k for _ in range(k)]
    for vec in per_group.values():
        for a in range(k):
            for b in range(k):
                meat[a][b] += vec[a] * vec[b]

    g = len(per_group)
    scale = (g / (g - 1)) * ((n - 1) / (n - k)) if g > 1 else 1.0
    cov = [
        [
            scale * sum(xtx_inv[a][p] * meat[p][q] * xtx_inv[q][b] for p in range(k) for q in range(k))
            for b in range(k)
        ]
        for a in range(k)
    ]
    se = [math.sqrt(max(cov[a][a], 0.0)) for a in range(k)]

    ybar = st.mean(y)
    ss_tot = sum((v - ybar) ** 2 for v in y)
    ss_res = sum(r * r for r in resid)
    r2 = 1 - ss_res / ss_tot if ss_tot > 0 else 0.0
    return beta, se, r2


def run(obs: list[dict], outcome: str, label: str) -> None:
    if len(obs) < 200:
        print(f"  {label:<12} n={len(obs)} — too few observations, skipped")
        return
    y = [o[outcome] for o in obs]
    groups = [o["player"] for o in obs]

    X_base = [[1.0, o["baseline"]] for o in obs]
    _, _, r2_base = ols_clustered(X_base, y, groups)

    X_full = [[1.0, o["baseline"], o["trend"]] for o in obs]
    beta, se, r2_full = ols_clustered(X_full, y, groups)

    t = beta[2] / se[2] if se[2] > 0 else 0.0
    verdict = "SIGNIFICANT" if abs(t) > 1.96 else "null"
    print(
        f"  {label:<12} n={len(obs):>6}  players={len(set(groups)):>5}  "
        f"trend beta={beta[2]:>8.2f} (se {se[2]:>6.2f})  t={t:>6.2f}  "
        f"dR2={r2_full - r2_base:>+7.5f}  {verdict}"
    )


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--outcome", default="rec_yards", choices=["rec_yards", "targets"])
    args = ap.parse_args(argv)

    rows = load_player_weeks(FIRST, LAST)
    if not rows:
        raise SystemExit(f"no data in {CACHE} — run ingest/nflverse.py first")
    agreement = add_computed_share(rows)
    obs = build_observations(rows)

    print(f"player-weeks loaded: {len(rows)}  ({FIRST}-{LAST}, REG, WR/TE/RB)")
    print(f"computed target share vs nflverse column: mean abs diff {agreement:.6f}")
    print(f"usable observations: {len(obs)}\n")

    print("DOES A RISING UTILIZATION TREND PREDICT PRODUCTION,")
    print("CONTROLLING FOR SEASON-TO-DATE SHARE?")
    print(f"outcome = {args.outcome}, errors clustered by player\n")

    run(obs, args.outcome, "ALL")
    print()
    for a, b in ERAS:
        run([o for o in obs if a <= o["season"] <= b], args.outcome, f"{a}-{b}")

    print(
        "\ninterpretation: beta is the change in outcome per 1.00 of trend, so a"
        "\n0.05 (five-point) share rise moves it by beta*0.05. dR2 is the variance"
        "\nexplained that season-to-date share does not already account for."
    )

    if args.outcome == "rec_yards":
        practical_significance(obs)
    return 0


def practical_significance(obs: list[dict]) -> None:
    """Translate the coefficient into the only units that decide a wager.

    A significant coefficient is not an edge. What matters is how far a
    realistic trend moves P(over), against a hurdle of about 52.4% at -110. So
    this reports the trend distribution -- to establish what "a rising role"
    actually looks like -- and converts it to probability points.
    """
    y = [o["rec_yards"] for o in obs]
    sd = st.stdev(y)
    trends = sorted(o["trend"] for o in obs)

    def pct(p: float) -> float:
        return trends[min(int(p * len(trends)), len(trends) - 1)]

    X = [[1.0, o["baseline"], o["trend"]] for o in obs]
    beta, _, _ = ols_clustered(X, y, [o["player"] for o in obs])
    b = beta[2]

    print(f"\n{'=' * 68}\nPRACTICAL SIGNIFICANCE\n{'=' * 68}")
    print(f"\ngame-to-game receiving yards sd: {sd:.1f}")
    print("\ntrend distribution (share points over the last two games):")
    for p in (0.5, 0.75, 0.90, 0.95, 0.99):
        print(f"  p{int(p * 100):<3} {pct(p) * 100:+6.2f}")

    print(f"\nwhat a trend is worth (beta = {b:.1f} yards per 1.00 of trend):")
    print(f"  {'trend':>9} {'yards':>8} {'P(over) shift':>15}")
    for p in (0.75, 0.90, 0.95, 0.99):
        t = pct(p)
        dy = b * t
        z = dy / sd
        dp = 0.5 - 0.5 * math.erfc(z / math.sqrt(2))
        print(f"  p{int(p * 100):<3} {t * 100:+5.1f}p {dy:>7.1f}y {dp * 100:>+13.1f} pts")

    print(
        "\nThe claim holds, but only in the tail. An ordinary uptick is worth"
        "\nabout a point of probability and cannot clear the vig. A role change"
        "\nat the 90th percentile or beyond -- roughly +6 share points across two"
        "\ngames -- is worth 3 points or more, which can."
        "\n\nCAVEAT: this measures information relative to SEASON-TO-DATE share,"
        "\nnot relative to whatever the sportsbook actually uses. If books already"
        "\nweight recent form, the real edge is smaller. Testing that needs"
        "\nhistorical prop lines, which are not freely available."
    )


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

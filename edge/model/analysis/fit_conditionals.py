#!/usr/bin/env python3
"""Fit P(receiving yards > L | opportunity, role trend, game script).

This produces the q and r that the scenario decomposition needs:

    q = P(hit | scenario)      r = P(hit | no scenario)

Until now both were operator guesses. A per-player estimate cannot supply them
-- 17 games is nowhere near enough -- but pooling across player-games with
similar opportunity gives cells with hundreds of observations each, which is
exactly the sample size the whole exercise has been short of.

# Why these three axes

    projected targets   The Late-Round thesis is volume over efficiency, and
                        every measurement in this project has agreed: targets
                        drive yards far more than per-target skill does.

    role trend          utilization_lag.py established that a two-game share
                        trend carries information season-long share does not,
                        but that only a LARGE change is worth acting on. A grid
                        that averaged over trend levels would wash that out, so
                        it gets its own axis with the boundary at the measured
                        threshold (+6 share points).

    game script         The axis that separates q from r. Without it there is
                        no scenario decomposition, only a base rate.

# Counting, not regression

Each cell stores the empirical distribution of yards as a quantile table, so
P(yards > L) is a lookup at any line rather than a fit at one. A thin cell
reports itself through its n, and the Go side turns that into a Wilson interval
at query time using the same code the hit-rate layer already uses. No
distributional assumption anywhere.

Dependency-free: stdlib only.

Usage:
    python3 fit_conditionals.py
    python3 fit_conditionals.py --report      # cell table, write nothing
"""

from __future__ import annotations

import argparse
import csv
import json
import statistics as st
import sys
import time
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"
ARTIFACT = ROOT.parent / "app" / "internal" / "scenario" / "artifacts" / "conditionals.json"

# stats_player_week carries target_share only from 2009 and air yards from 2006,
# but targets reach back to 2005. 2014 is chosen to match the residual fit's
# recency preference rather than to maximise n.
FIRST, LAST = 2014, 2025
POSITIONS = {"WR", "TE", "RB"}

MIN_PRIOR_GAMES = 4
TREND_WINDOW = 2
MIN_BASELINE_SHARE = 0.05
MIN_CELL = 100  # below this a cell is dropped rather than published thin

# Projected targets. Boundaries follow the natural break points of usage --
# rotational, complementary, starter, focal, alpha.
TARGET_BANDS = [(0, 4), (4, 6), (6, 8), (8, 11), (11, 999)]

# Role trend. The +0.06 boundary is the measured actionability threshold from
# utilization_lag.py: below it the effect cannot clear the vig.
TREND_BANDS = [(-99.0, -0.03), (-0.03, 0.03), (0.03, 0.06), (0.06, 99.0)]

# Scenarios, defined on the FINAL game state. That is an end-state proxy for
# what are really path properties, and the naming here reflects what is actually
# measured rather than the narrative it is meant to serve.
#
# In particular: this is "blowout_loss", NOT "trailing". The corpus predicts a
# garbage-time volume boost -- a team down 14 must throw, so its receivers see
# more work. Measured on final margin the effect comes out NEGATIVE in every
# cell, because losing by more than a touchdown mostly identifies offenses that
# did not function, and that swamps the late-game volume.
#
# The measurement does not refute the garbage-time mechanism. It refutes final
# margin as a proxy for it. Separating the two needs play-by-play (time
# remaining crossed with score differential), which is deliberately out of scope
# here -- see FINDINGS.md.
SCENARIOS = {
    "shootout": lambda game_total, team_margin: game_total > 50,
    "blowout_loss": lambda game_total, team_margin: team_margin < -7,
}

QUANTILE_STEPS = 51  # p0..p100 in 2% steps


def num(s) -> float:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return 0.0
    try:
        return float(s)
    except ValueError:
        return 0.0


def load_games() -> dict:
    """(season, week, team) -> (game total, that team's margin)."""
    path = CACHE / "games.csv"
    if not path.exists():
        raise SystemExit(f"{path} not found — run ingest/nflverse.py")
    out = {}
    for r in csv.DictReader(path.open()):
        if not (r["result"].strip() and r["total"].strip()):
            continue
        season, week = int(num(r["season"])), int(num(r["week"]))
        total, result = num(r["total"]), num(r["result"])  # result = home - away
        out[(season, week, r["home_team"])] = (total, result)
        out[(season, week, r["away_team"])] = (total, -result)
    return out


def load_player_weeks() -> list[dict]:
    rows = []
    for season in range(FIRST, LAST + 1):
        path = CACHE / f"stats_player_week_{season}.csv"
        if not path.exists():
            continue
        for r in csv.DictReader(path.open()):
            if r.get("season_type") != "REG" or r.get("position") not in POSITIONS:
                continue
            rows.append(
                {
                    "season": int(num(r["season"])),
                    "week": int(num(r["week"])),
                    "player": r.get("player_id", ""),
                    "team": r.get("team") or r.get("recent_team", ""),
                    "targets": num(r.get("targets")),
                    "yards": num(r.get("receiving_yards")),
                }
            )
    return rows


def build(rows, games) -> list[dict]:
    team_targets = defaultdict(float)
    for r in rows:
        team_targets[(r["season"], r["week"], r["team"])] += r["targets"]
    for r in rows:
        d = team_targets[(r["season"], r["week"], r["team"])]
        r["share"] = r["targets"] / d if d > 0 else 0.0
        r["team_targets"] = d

    by_player = defaultdict(list)
    for r in rows:
        by_player[(r["player"], r["season"])].append(r)

    obs = []
    for _, g in by_player.items():
        g.sort(key=lambda x: x["week"])
        for i, x in enumerate(g):
            if i < MIN_PRIOR_GAMES:
                continue
            prior = g[:i]
            baseline = st.mean(p["share"] for p in prior)
            if baseline < MIN_BASELINE_SHARE:
                continue
            recent = st.mean(p["share"] for p in prior[-TREND_WINDOW:])
            # Projected targets uses only prior information: the player's
            # baseline share against his team's recent pass volume.
            team_vol = st.mean(p["team_targets"] for p in prior[-3:])
            ctx = games.get((x["season"], x["week"], x["team"]))
            if ctx is None:
                continue
            game_total, margin = ctx
            obs.append(
                {
                    "proj_targets": baseline * team_vol,
                    "trend": recent - baseline,
                    "yards": x["yards"],
                    "game_total": game_total,
                    "margin": margin,
                }
            )
    return obs


def band_index(bands, v):
    for i, (lo, hi) in enumerate(bands):
        if lo <= v < hi:
            return i
    return None


def quantiles(values: list[float]) -> list[list[float]]:
    """[[probability, yards], ...] at evenly spaced probabilities."""
    s = sorted(values)
    n = len(s)
    out = []
    for i in range(QUANTILE_STEPS):
        p = i / (QUANTILE_STEPS - 1)
        idx = min(int(p * (n - 1)), n - 1)
        out.append([round(p, 4), round(s[idx], 1)])
    return out


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--report", action="store_true", help="print cells, write nothing")
    args = ap.parse_args(argv)

    games = load_games()
    rows = load_player_weeks()
    obs = build(rows, games)
    print(f"player-weeks {FIRST}-{LAST}: {len(rows)}   usable observations: {len(obs)}\n")

    cells, dropped = [], 0
    for scenario, occurred_fn in SCENARIOS.items():
        for occurred in (True, False):
            for ti, (ta, tb) in enumerate(TARGET_BANDS):
                for ri, (ra, rb) in enumerate(TREND_BANDS):
                    sel = [
                        o
                        for o in obs
                        if ta <= o["proj_targets"] < tb
                        and ra <= o["trend"] < rb
                        and occurred_fn(o["game_total"], o["margin"]) == occurred
                    ]
                    if len(sel) < MIN_CELL:
                        dropped += 1
                        continue
                    ys = [o["yards"] for o in sel]
                    cells.append(
                        {
                            "scenario": scenario,
                            "occurred": occurred,
                            "targets_min": ta,
                            "targets_max": tb,
                            "trend_min": ra,
                            "trend_max": rb,
                            "n": len(sel),
                            "median": round(st.median(ys), 1),
                            "quantiles": quantiles(ys),
                        }
                    )

    print(f"cells: {len(cells)} published, {dropped} dropped for n < {MIN_CELL}\n")

    # Show the thing the decomposition is built on: does the scenario move the
    # distribution at all? If q and r are equal the scenario carries no
    # information, which RequiredScenarioProb rejects outright.
    print("SCENARIO SEPARATION (median yards, occurred vs not)")
    print(f"  {'scenario':>9} {'targets':>9} {'trend':>14} {'occurred':>9} {'not':>7} {'delta':>7}")
    for scenario in SCENARIOS:
        for ta, tb in TARGET_BANDS:
            for ra, rb in TREND_BANDS:
                got = {
                    c["occurred"]: c
                    for c in cells
                    if c["scenario"] == scenario
                    and c["targets_min"] == ta
                    and c["trend_min"] == ra
                }
                if len(got) != 2:
                    continue
                a, b = got[True]["median"], got[False]["median"]
                print(
                    f"  {scenario:>9} {f'{ta}-{tb}':>9} {f'{ra:+.2f}..{rb:+.2f}':>14} "
                    f"{a:>9.1f} {b:>7.1f} {a - b:>+7.1f}"
                )

    if args.report:
        print("\n--report: artifact not written")
        return 0

    ARTIFACT.parent.mkdir(parents=True, exist_ok=True)
    ARTIFACT.write_text(
        json.dumps(
            {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "generated_by": "edge/model/analysis/fit_conditionals.py",
                "outcome": "receiving_yards",
                "seasons": [FIRST, LAST],
                "min_cell": MIN_CELL,
                "note": (
                    "quantiles are [[probability, yards], ...]; P(yards > L) is "
                    "1 minus the interpolated CDF. n supports a Wilson interval "
                    "computed at query time."
                ),
                "cells": cells,
            },
            indent=1,
        )
        + "\n"
    )
    print(f"\nwrote {ARTIFACT} ({ARTIFACT.stat().st_size / 1024:.0f} KB)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

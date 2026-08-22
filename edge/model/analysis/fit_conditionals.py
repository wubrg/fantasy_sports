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

import validate
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"
MANIFEST = CACHE / "manifest.json"
ARTIFACT = ROOT.parent / "app" / "internal" / "scenario" / "artifacts" / "conditionals.json"

# stats_player_week carries target_share only from 2009 and air yards from 2006,
# but targets reach back to 2005. (This fit computes share from raw targets
# rather than reading the column, so 2009 is not a hard floor for it.)
#
# Was 2014, chosen to match the residual fit's recency preference rather than to
# maximise n. Extended to 2009 to publish the cells that matter most and were
# missing: 8-11 projected targets with a rising role -- high volume plus climbing
# usage, which is the corpus's "usage vacuum", the strongest edge it claims, and
# the one situation the grid could not price. That cell held 97 observations
# against a floor of 100.
#
# The trade is era dilution, and it was measured rather than assumed before being
# taken. Extending keeps the effect: shootout goes from 15/15 to 16/16 cells
# consistent and 14/14 to 15/15 out of sample, with resolution improving from
# 11/15 to 12/16. Cells that already published move by at most 2.5 points of
# P(>52.5), most by under one. validate.py re-checks all of this on every fit and
# fails if it stops holding.
#
# What was NOT done, and why: merging 8-11 with 11+ would have published the same
# cell without new data, but the bands do not behave alike -- at a 100.5 line the
# 11+ band's NO-shootout rate (0.289) exceeds 8-11's WITH-shootout rate (0.277),
# so pooling understates a 12-target projection by ~11 points on q. Lowering
# MIN_CELL would have cleared a three-observation shortfall by weakening the floor
# everywhere.
FIRST, LAST = 2009, 2025
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
# more work. Measured on final margin the effect comes out negative in 14 of 15
# cells, because losing by more than a touchdown mostly identifies offenses that
# did not function, and that swamps the late-game volume.
#
# The measurement does not refute the garbage-time mechanism. It refutes final
# margin as a proxy for it.
class ScenarioDef:
    """What a named scenario MEANS, and the test for whether it happened.

    The predicate is derived from the recorded fields rather than written
    alongside them. That is the whole point: the definition used to be a bare
    lambda, so the artifact could record a scenario's NAME but never what the
    name stood for -- and the CLI takes its own -threshold at query time.
    Asking for `-name shootout -threshold 65` produced s = P(total > 65) blended
    against a q measured on total > 50, which is not a probability of anything.
    Both halves now read the same three fields.
    """

    def __init__(self, basis: str, op: str, threshold: float):
        assert basis in ("total", "margin"), basis
        assert op in (">", "<"), op
        self.basis, self.op, self.threshold = basis, op, threshold

    def occurred(self, game_total: float, team_margin: float) -> bool:
        v = game_total if self.basis == "total" else team_margin
        return v > self.threshold if self.op == ">" else v < self.threshold

    def as_json(self) -> dict:
        return {"basis": self.basis, "op": self.op, "threshold": self.threshold}


SCENARIOS = {
    "shootout": ScenarioDef("total", ">", 50),
    "blowout_loss": ScenarioDef("margin", "<", -7),
}

# Whether a scenario is fit to bet on. Cells are still emitted for unvalidated
# scenarios -- the fit stays reproducible and the data stays available for the
# work that would validate them -- but the Go lookup refuses to price a wager
# from one.
#
# blowout_loss failed validation on three independent counts, any one of which
# would be disqualifying:
#
#   1. The direction inverts at ordinary lines. At 7 projected targets, q > r at
#      6.5, 20.5 and 24.5 receiving yards -- mainstream prop lines, in a
#      volume-bearing band, contradicting the negative direction the medians
#      show. A wager priced there would carry the opposite belief requirement
#      from the one the finding implies.
#   2. The sign is unresolved almost everywhere. A player-level cluster
#      bootstrap of the median delta clears zero in only 3 of 15 cells; the one
#      positive cell has a CI of [0.0, 4.0]. Twelve of fifteen signs are noise.
#   3. It does not survive out of sample. Refit on 2014-2021 and evaluated on
#      2022-2025, the sign holds in 10 of 13 cells against shootout's 14 of 14,
#      with roughly 2.7 points of non-noise error on q -- larger than the
#      2.4-point vig cushion at -110.
#
# shootout passes all three: positive in 15 of 15 cells, resolved in 10, and
# 14 of 14 out of sample.
#
# What would un-gate blowout_loss: defining it on play-by-play (time remaining
# crossed with score differential) rather than final margin, which is the
# measurement this whole result argues for.
# Whether a scenario may be priced. The VERDICT is a human judgement and stays
# one; the EVIDENCE behind it is measured every run by validate.py and written
# into the artifact's note, so it can no longer drift away from the data while
# continuing to read as authoritative.
#
# The rule below is the bar the verdict is held to. It is written out rather
# than inferred, and the fit fails if a recorded verdict and the measured
# evidence disagree -- which is the whole mechanism: a scenario cannot quietly
# stop qualifying and keep its flag.
#
# It is deliberately not a computed threshold. Two scenarios is not enough to
# calibrate one on, and a bar reverse-engineered to reproduce the answer already
# written down would look derived while being fitted to its conclusion.
SCENARIO_STATUS = {
    "shootout": {"validated": True},
    "blowout_loss": {
        "validated": False,
        "why": "Needs a play-by-play definition (time remaining crossed with score "
        "differential) rather than final margin, which is the measurement this result "
        "argues for.",
    },
}


def qualifies(ev: dict) -> bool:
    """The stated rule. All three must hold.

    1. The direction is consistent in every cell -- whichever direction it is.
       An effect that reverses from band to band is a description of noise.
    2. It survives out of sample, in every cell the late seasons can speak to.
       A separation that exists only where it was found is not a separation.

    Two things are measured and reported but deliberately NOT gated on:

    Inversions at ordinary prop lines. This was going to be the first
    criterion, on the strength of the note it replaces. Measured against
    sampling error, not one crossing clears 2 SE -- for either scenario. They
    are noise, and gating on a noise count would have rejected shootout on the
    strength of a single 1.3-point wobble at 6.5 yards, a line at which ~88% of
    player-games clear either way.

    Resolution. A player-clustered bootstrap over a few hundred correlated
    games is demanding, and shootout clears only 11 of 15. Requiring all of them
    would reject a real effect; reporting it keeps the reader honest about how
    much of the grid is firmly held.
    """
    return ev["consistent"] == ev["cells"] and ev["oos_agree"] == ev["oos_cells"]

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


def load_player_weeks() -> tuple[list[dict], list[int]]:
    """Every regular-season WR/TE/RB game-week in the fit window.

    A missing season is an error, not a skip. It used to `continue`, which
    made a partial cache produce a quietly smaller grid -- and since the
    artifact stamped the seasons it *asked* for rather than the ones it read,
    the result claimed 2014-2025 either way. A four-season grid labelled as
    twelve is worse than no grid: every q and r it serves is defensible-looking
    and wrong, and nothing downstream can tell.

    Returns the rows and the seasons they actually came from, so the caller can
    stamp what was read instead of what was requested.
    """
    rows = []
    seasons = []
    for season in range(FIRST, LAST + 1):
        path = CACHE / f"stats_player_week_{season}.csv"
        if not path.exists():
            raise SystemExit(
                f"{path} not found -- run ingest/nflverse.py --seasons {FIRST}-{LAST}.\n"
                f"  Refusing to fit {FIRST}-{LAST} from a partial cache: the result would "
                f"be a thinner grid carrying the full window's provenance."
            )
        seasons.append(season)
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
    return rows, seasons


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
    for (player, _season), g in by_player.items():
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
                    "player": player,
                    # Carried for the out-of-sample split in validate.py. The
                    # fit itself pools across seasons and does not use it.
                    "season": x["season"],
                    # Carried so team-week series (PROE, and anything else keyed
                    # that way) can be joined on. build() used to drop both,
                    # which made such a join impossible without reimplementing
                    # this function.
                    "week": x["week"],
                    "team": x["team"],
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


def effective_n(values: list[float], players: list[str]) -> tuple[float, float]:
    """Effective sample size after accounting for repeat players.

    A cell pools many games from the same player, and those observations are not
    independent -- a target hog contributes twenty correlated rows. Feeding the
    raw count into a Wilson interval therefore claims more precision than the
    data supports.

    The correction is the standard design effect, deff = 1 + (m0 - 1) * ICC,
    with ICC estimated by one-way ANOVA over players within the cell. Measured
    across the published grid the design effect runs 1.0 to about 1.4, so this
    widens intervals by roughly 9% typically rather than transforming them --
    cells pool 42 to 847 distinct players, which is what keeps the problem small.

    Returns (n_eff, icc).
    """
    n = len(values)
    groups: dict[str, list[float]] = defaultdict(list)
    for v, p in zip(values, players):
        groups[p].append(v)
    k = len(groups)
    if k < 2 or n <= k:
        return float(n), 0.0

    grand = st.mean(values)
    # Between- and within-group mean squares.
    ss_between = sum(len(g) * (st.mean(g) - grand) ** 2 for g in groups.values())
    ss_within = sum((v - st.mean(g)) ** 2 for g in groups.values() for v in g)
    ms_between = ss_between / (k - 1)
    ms_within = ss_within / (n - k)
    if ms_within <= 0:
        return float(n), 0.0

    # m0 is the size-corrected mean cluster size, not the plain mean.
    sum_sq = sum(len(g) ** 2 for g in groups.values())
    m0 = (n - sum_sq / n) / (k - 1)
    if m0 <= 1:
        return float(n), 0.0

    icc = (ms_between - ms_within) / (ms_between + (m0 - 1) * ms_within)
    icc = max(0.0, min(icc, 1.0))
    deff = 1 + (m0 - 1) * icc
    return n / deff if deff > 0 else float(n), icc


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--report", action="store_true", help="print cells, write nothing")
    args = ap.parse_args(argv)

    games = load_games()
    rows, seasons_read = load_player_weeks()
    obs = build(rows, games)
    print(f"player-weeks {FIRST}-{LAST}: {len(rows)}   usable observations: {len(obs)}\n")

    # Measure the evidence before building cells, so a fit can never emit an
    # artifact whose validation note disagrees with the data in the same file.
    print("VALIDATION")
    status = {}
    for scenario, definition in SCENARIOS.items():
        ev = validate.evidence(obs, definition, TARGET_BANDS, TREND_BANDS, MIN_CELL)
        recorded = SCENARIO_STATUS[scenario]["validated"]
        measured = qualifies(ev)
        note = validate.note(ev)
        if why := SCENARIO_STATUS[scenario].get("why"):
            note = f"{note}. {why}"
        print(f"  {scenario:14} {note}")
        print(f"  {'':14} rule says {measured}, recorded {recorded}")
        if measured != recorded:
            raise SystemExit(
                f"\n{scenario}: the evidence and the recorded verdict disagree.\n"
                f"  measured: {note}\n"
                f"  the stated rule gives validated={measured}, but SCENARIO_STATUS "
                f"says {recorded}.\n"
                f"  Change the verdict, or change the rule and say why -- do not ship "
                f"a flag the data no longer supports."
            )
        status[scenario] = {"validated": recorded, "note": note, "evidence": ev}
    print()

    cells, dropped = [], 0
    for scenario, definition in SCENARIOS.items():
        for occurred in (True, False):
            for ti, (ta, tb) in enumerate(TARGET_BANDS):
                for ri, (ra, rb) in enumerate(TREND_BANDS):
                    sel = [
                        o
                        for o in obs
                        if ta <= o["proj_targets"] < tb
                        and ra <= o["trend"] < rb
                        and definition.occurred(o["game_total"], o["margin"]) == occurred
                    ]
                    if len(sel) < MIN_CELL:
                        dropped += 1
                        continue
                    ys = [o["yards"] for o in sel]
                    n_eff, icc = effective_n(ys, [o["player"] for o in sel])
                    cells.append(
                        {
                            "scenario": scenario,
                            "occurred": occurred,
                            "targets_min": ta,
                            "targets_max": tb,
                            "trend_min": ra,
                            "trend_max": rb,
                            "n": len(sel),
                            "n_eff": round(n_eff, 1),
                            "players": len({o["player"] for o in sel}),
                            "icc": round(icc, 4),
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

    # Which files this fit actually read, hashed. fit_residuals.py has recorded
    # this since it shipped; conditionals never did, and the asymmetry cost a
    # full refit-and-diff to answer "does the committed grid still reproduce?"
    # -- a question the manifest could have answered in a second. Upstream
    # revises historical seasons (a 2014-2021 correction moved five cell
    # medians after this artifact was generated), so "same seasons" is not the
    # same claim as "same data".
    source = {}
    if MANIFEST.exists():
        m = json.loads(MANIFEST.read_text())
        for season in seasons_read:
            name = f"stats_player_week_{season}.csv"
            if name in m:
                source[name] = m[name]

    ARTIFACT.parent.mkdir(parents=True, exist_ok=True)
    ARTIFACT.write_text(
        json.dumps(
            {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "generated_by": "edge/model/analysis/fit_conditionals.py",
                "outcome": "receiving_yards",
                # The seasons READ, not the seasons requested. Identical while
                # load_player_weeks refuses a partial cache, which is the point:
                # the stamp is now derived rather than asserted.
                "seasons": [seasons_read[0], seasons_read[-1]],
                "source": source,
                "min_cell": MIN_CELL,
                # What each name means. Without this the artifact recorded that
                # a cell belonged to "shootout" but not what "shootout" was, so
                # nothing could check the caller's threshold against it.
                "scenario_definitions": {k: v.as_json() for k, v in SCENARIOS.items()},
                "scenario_status": status,
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

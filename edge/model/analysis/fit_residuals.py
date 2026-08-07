#!/usr/bin/env python3
"""Fit empirical residual distributions for game totals and margins.

The scenario model needs P(final total > threshold) and P(final margin >
threshold) given a posted line. It originally assumed those were normal with
sigma 10.0 and 13.5. Measured against 5,698 games, the total assumption was
badly wrong: the real residual sd is 13.30, not 10.0, and the resulting
calibration curve runs +7.2 points off at low probabilities and -16.1 at high.

Rather than fit a better sigma, this drops the shape assumption entirely and
uses the empirical distribution of

    residual = actual - expected

which is exactly what the data gives us and needs no normality. Two findings
from the same measurement make one pooled residual distribution defensible:

    dispersion is flat across line level   12.8-13.9 in every bucket
    dispersion is stable across eras       13.0-13.4 totals, 12.4-13.7 margins

Levels have moved a lot since 2005. Dispersion has not. That is the "relations
survive level shifts" argument, measured rather than asserted, and it is why
this pools from 2005 instead of guessing at an era cutoff.

# Sign convention

`residual` is always ACTUAL MINUS EXPECTED, which is convention-free. The
caller computes expected from its own spread convention and then applies this
distribution. That keeps the artifact from silently encoding whether a
"-3 favourite" means plus or minus three.

Dependency-free: stdlib only.

Usage:
    python3 fit_residuals.py                 # fit, validate, write artifact
    python3 fit_residuals.py --validate-only # report calibration, write nothing
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import statistics as st
import sys
import time
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"
GAMES = CACHE / "games.csv"
MANIFEST = CACHE / "manifest.json"
ARTIFACT = ROOT.parent / "app" / "internal" / "scenario" / "artifacts" / "residuals.json"

# Nested validation. The fit window is a hyperparameter, and choosing it by
# looking at the final holdout would make the reported calibration optimistic —
# the holdout would have been used twice. So:
#
#   SELECT_*   inner validation, used only to choose the window
#   FIT_LAST   everything up to here is available for fitting
#   HOLDOUT_*  touched once, at the end, to report
#
# Measured behaviour is a clean bias-variance tradeoff: fitting from 2005
# includes stale data and hurts, fitting from 2019 has too few games and hurts
# more, and the optimum sits around 2014-2016.
CANDIDATE_STARTS = [2005, 2008, 2010, 2012, 2014, 2016]

# Rolling-origin folds rather than one validation slice. A single two-season
# fold proved unstable: it picked a 534-game fit window that scored 1.02 on the
# fold and 2.20 on the real holdout, which is the fold being overfit rather
# than the window being good. Averaging over several origins fixes that, and it
# also mirrors how the model is actually used — always fitting on the past and
# predicting the next season.
FOLDS = [(2016, 2017), (2018, 2019), (2020, 2021)]
MIN_FIT_GAMES = 500

FIT_LAST = 2021
HOLDOUT_FIRST, HOLDOUT_LAST = 2022, 2025

# The sigmas originally shipped, kept here so the comparison is against what
# was actually in production rather than a strawman.
SHIPPED_SIGMA = {"total": 10.0, "margin": 13.5}


def normal_sf(z: float) -> float:
    """P(Z > z) for a standard normal."""
    return 0.5 * (1 - math.erf(z / math.sqrt(2)))


def load_games() -> list[dict]:
    if not GAMES.exists():
        raise SystemExit(
            f"{GAMES} not found — run:\n    python3 {ROOT}/ingest/nflverse.py"
        )
    rows = []
    for r in csv.DictReader(GAMES.open()):
        if not (r["result"].strip() and r["total"].strip()):
            continue
        if not (r["spread_line"].strip() and r["total_line"].strip()):
            continue
        rows.append(
            {
                "season": int(r["season"]),
                "total_line": float(r["total_line"]),
                "total": float(r["total"]),
                "spread_line": float(r["spread_line"]),
                "result": float(r["result"]),
            }
        )
    return rows


def residuals(rows: list[dict], kind: str) -> list[float]:
    """Actual minus expected, for one quantity."""
    if kind == "total":
        return [r["total"] - r["total_line"] for r in rows]
    return [r["result"] - r["spread_line"] for r in rows]


def empirical_cdf(values: list[float]) -> list[list[float]]:
    """Exact empirical CDF as [[residual, P(X <= residual)], ...].

    Football scores are integers and lines move in half points, so the
    residuals take few distinct values -- roughly 200. Storing the exact CDF at
    those points is both smaller and more faithful than sampling a grid.
    """
    n = len(values)
    counts = defaultdict(int)
    for v in values:
        counts[v] += 1
    out, cum = [], 0
    for v in sorted(counts):
        cum += counts[v]
        out.append([round(v, 2), round(cum / n, 8)])
    return out


def cdf_lookup(table: list[list[float]], x: float) -> float:
    """P(X <= x), linearly interpolated between the tabulated points.

    Mirrors the Go implementation exactly; both are tested against the same
    fixtures so a divergence shows up as a test failure rather than a quiet
    disagreement between languages.
    """
    if x < table[0][0]:
        return 0.0
    if x >= table[-1][0]:
        return 1.0
    lo, hi = 0, len(table) - 1
    while lo < hi - 1:
        mid = (lo + hi) // 2
        if table[mid][0] <= x:
            lo = mid
        else:
            hi = mid
    x0, p0 = table[lo]
    x1, p1 = table[hi]
    if x1 == x0:
        return p1
    return p0 + (p1 - p0) * (x - x0) / (x1 - x0)


def calibration(rows, kind, thresholds, predict, nb=10):
    """Bucketed predicted-vs-actual, and the mean absolute gap over buckets."""
    buckets = defaultdict(lambda: [0, 0, 0.0])
    for r in rows:
        line = r["total_line"] if kind == "total" else r["spread_line"]
        actual = r["total"] if kind == "total" else r["result"]
        for t in thresholds:
            p = predict(t - line)
            hit = actual > t
            k = min(int(p * nb), nb - 1)
            b = buckets[k]
            b[0] += 1
            b[1] += 1 if hit else 0
            b[2] += p
    gaps, weight = 0.0, 0
    rendered = []
    for k in sorted(buckets):
        n, h, ps = buckets[k]
        if n < 50:
            continue
        pred, act = ps / n, h / n
        rendered.append((k / nb, n, pred, act))
        gaps += abs(act - pred) * n
        weight += n
    return rendered, (gaps / weight if weight else 0.0)


def report(name, rendered, mae):
    print(f"  {name}")
    print(f"    {'bucket':>9} {'n':>7} {'predicted':>10} {'actual':>8} {'gap':>7}")
    for lo, n, pred, act in rendered:
        print(
            f"    {lo:.1f}-{lo + 0.1:.1f} {n:>7} {pred * 100:>9.1f}% "
            f"{act * 100:>7.1f}% {(act - pred) * 100:>+6.1f}"
        )
    print(f"    weighted mean absolute gap: {mae * 100:.2f} points\n")


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--validate-only", action="store_true")
    args = ap.parse_args(argv)

    rows = load_games()
    hold = [r for r in rows if HOLDOUT_FIRST <= r["season"] <= HOLDOUT_LAST]

    # Thresholds chosen to span the range a scenario would actually use, not
    # just the middle where any model looks fine.
    thresholds = {
        "total": [38, 41, 44, 47, 50, 53, 56],
        "margin": [-10, -7, -3, 0, 3, 7, 10, 14],
    }

    # --- choose the fit window by rolling-origin cross-validation ---
    folds_desc = ", ".join(f"{a}-{b}" for a, b in FOLDS)
    print(f"{'=' * 62}\nWINDOW SELECTION (rolling-origin CV over {folds_desc})\n{'=' * 62}\n")
    print(f"  {'start':>7} {'total MAE':>11} {'margin MAE':>12}   (mean over folds)")
    chosen, scores = {}, {}
    for start in CANDIDATE_STARTS:
        per_kind = {}
        for kind in ("total", "margin"):
            fold_scores = []
            for fold_first, fold_last in FOLDS:
                inner = [r for r in rows if start <= r["season"] < fold_first]
                if len(inner) < MIN_FIT_GAMES:
                    continue
                val = [r for r in rows if fold_first <= r["season"] <= fold_last]
                tbl = empirical_cdf(residuals(inner, kind))
                _, mae_v = calibration(
                    val, kind, thresholds[kind], lambda d, t=tbl: 1 - cdf_lookup(t, d)
                )
                fold_scores.append(mae_v)
            if fold_scores:
                per_kind[kind] = sum(fold_scores) / len(fold_scores)
        if len(per_kind) != 2:
            continue
        for kind, v in per_kind.items():
            scores.setdefault(kind, {})[start] = v
        print(
            f"  {start:>7} {per_kind['total'] * 100:>10.2f}p "
            f"{per_kind['margin'] * 100:>11.2f}p"
        )
    for kind in ("total", "margin"):
        chosen[kind] = min(scores[kind], key=scores[kind].get)
    print(f"\n  chosen: total from {chosen['total']}, margin from {chosen['margin']}\n")

    # --- refit on everything up to FIT_LAST using the chosen windows ---
    tables, stats = {}, {}
    for kind in ("total", "margin"):
        fit = [r for r in rows if chosen[kind] <= r["season"] <= FIT_LAST]
        res = residuals(fit, kind)
        tables[kind] = empirical_cdf(res)
        stats[kind] = {
            "n": len(res),
            "fit_first_season": chosen[kind],
            "fit_last_season": FIT_LAST,
            "mean": round(st.mean(res), 4),
            "sd": round(st.stdev(res), 4),
            "points": len(tables[kind]),
        }
        print(
            f"{kind:>6}: {chosen[kind]}-{FIT_LAST}  n={len(res)} "
            f"mean={st.mean(res):+.2f} sd={st.stdev(res):.2f} "
            f"({len(tables[kind])} distinct residuals)"
        )

    print(f"\n{'=' * 62}\nHELD-OUT CALIBRATION ({HOLDOUT_FIRST}-{HOLDOUT_LAST})\n{'=' * 62}\n")
    verdict = {}
    for kind in ("total", "margin"):
        sigma = SHIPPED_SIGMA[kind]
        norm_r, norm_mae = calibration(
            hold, kind, thresholds[kind], lambda d, s=sigma: normal_sf(d / s)
        )
        emp_r, emp_mae = calibration(
            hold, kind, thresholds[kind], lambda d, t=tables[kind]: 1 - cdf_lookup(t, d)
        )
        print(f"{kind.upper()}\n")
        report(f"normal, sigma={sigma} (what shipped)", norm_r, norm_mae)
        report("empirical residual CDF", emp_r, emp_mae)
        better = emp_mae < norm_mae
        verdict[kind] = better
        print(
            f"  -> empirical is {'BETTER' if better else 'NOT better'}: "
            f"{emp_mae * 100:.2f} vs {norm_mae * 100:.2f} points of mean absolute gap\n"
        )

    if not all(verdict.values()):
        print("DECISION POINT: empirical did not beat normal everywhere. Not writing.")
        return 1

    if args.validate_only:
        print("--validate-only: artifact not written")
        return 0

    source = {}
    if MANIFEST.exists():
        m = json.loads(MANIFEST.read_text())
        if "games.csv" in m:
            source["games.csv"] = m["games.csv"]

    ARTIFACT.parent.mkdir(parents=True, exist_ok=True)
    ARTIFACT.write_text(
        json.dumps(
            {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "generated_by": "edge/model/analysis/fit_residuals.py",
                "note": (
                    "Residual is ACTUAL MINUS EXPECTED, so the sign convention "
                    "belongs to the caller. cdf is [[residual, P(X <= residual)], ...]."
                ),
                "window_selected_by": "rolling-origin CV",
                "window_selection_folds": [list(f) for f in FOLDS],
                "holdout_seasons": [HOLDOUT_FIRST, HOLDOUT_LAST],
                "source": source,
                "total": {**stats["total"], "cdf": tables["total"]},
                "margin": {**stats["margin"], "cdf": tables["margin"]},
            },
            indent=1,
        )
        + "\n"
    )
    print(f"wrote {ARTIFACT} ({ARTIFACT.stat().st_size / 1024:.1f} KB)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

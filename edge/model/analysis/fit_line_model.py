"""Fit the line-only reference: P(scenario | posted total and spread).

Why this exists. The belief probe measures a forecaster's P(scenario) against a
reference. The base rate and the incumbent are both weak opponents -- a constant,
and a fitted grid on prior form -- and the 2026-09-01 review showed a
four-parameter logistic on numbers ALREADY IN THE PACK (the posted total and
spread) beating the base rate by +0.029, three times the target edge, with no
football knowledge at all.

So that logistic becomes a third, mandatory opponent. Beating it is the only
version of "the forecaster knows something" that means what the plan says: it
must beat not just a constant but the market's own line, mechanically converted.

Scope: efficient_offense and pass_heavy only. shootout and blowout_loss already
carry an s_market derived from the line (FromTotal/FromSpread), so a line model
for them would duplicate a reference they already have. These two have no market
line and are exactly where the exploit lived.

Features, per team-game:  [ 1, (total-45)/5, m/7, |m|/7 ]
where m is THIS team's expected margin (home: +spread_line, away: -spread_line).
The centring and scaling are cosmetic -- they keep the Newton step well
conditioned -- and are recorded in the artifact so the consumer reproduces them
exactly.

Fit once, over every season in the cache, and commit the coefficients. A
four-parameter model does not meaningfully overfit 10k rows, so unlike the base
rate there is no in-sample/held-out gap to worry about; the artifact still
records a held-out log-loss so a later reader can check that claim.

    python3 model/analysis/fit_line_model.py            # writes the artifact
    python3 model/analysis/fit_line_model.py --dry-run  # prints, writes nothing
"""
import argparse
import csv
import json
import math
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F
import proe
import signals as signals_mod

# The two scenarios with no market line -- the only ones this reference is for.
LINE_SCENARIOS = ("efficient_offense", "pass_heavy")

ARTIFACT = F.ARTIFACT.parent / "line_model.json"

# Feature centring/scaling. Recorded in the artifact; the consumer must apply
# the identical transform or the coefficients mean nothing.
TOTAL_CENTER, TOTAL_SCALE = 45.0, 5.0
MARGIN_SCALE = 7.0


def features(total: float, expected_margin: float) -> list[float]:
    """The design row for one team-game. Order is fixed and mirrored in Go/Python consumers."""
    return [
        1.0,
        (total - TOTAL_CENTER) / TOTAL_SCALE,
        expected_margin / MARGIN_SCALE,
        abs(expected_margin) / MARGIN_SCALE,
    ]


def _cache_provenance(names: list[str]) -> dict:
    """sha256 of each cache file, pinned so a restated season shows as a diff."""
    if not F.MANIFEST.exists():
        return {}
    m = json.loads(F.MANIFEST.read_text())
    return {n: m[n] for n in names if n in m}


def sigmoid(z: float) -> float:
    if z >= 0:
        return 1.0 / (1.0 + math.exp(-z))
    e = math.exp(z)
    return e / (1.0 + e)


def fit_logistic(X: list[list[float]], y: list[int], ridge: float = 1e-4,
                 iters: int = 100) -> tuple[list[float], bool]:
    """Newton-Raphson logistic regression, pure Python, tiny p.

    Ridge keeps the Hessian invertible if a feature is degenerate; at 1e-4 it
    does not move a well-posed fit. Returns (coefficients, converged).
    """
    k = len(X[0])
    beta = [0.0] * k
    for _ in range(iters):
        # Gradient g and Hessian H of the penalised negative log-likelihood.
        g = [0.0] * k
        H = [[0.0] * k for _ in range(k)]
        for xi, yi in zip(X, y):
            p = sigmoid(sum(b * x for b, x in zip(beta, xi)))
            w = p * (1 - p)
            r = p - yi
            for a in range(k):
                g[a] += r * xi[a]
                for b in range(k):
                    H[a][b] += w * xi[a] * xi[b]
        for a in range(k):
            g[a] += ridge * beta[a]
            H[a][a] += ridge
        step = _solve(H, g)
        if step is None:
            return beta, False
        beta = [b - s for b, s in zip(beta, step)]
        if max(abs(s) for s in step) < 1e-9:
            return beta, True
    return beta, True


def _solve(A: list[list[float]], b: list[float]) -> list[float] | None:
    """Gaussian elimination with partial pivoting. Returns None if singular."""
    n = len(A)
    M = [row[:] + [b[i]] for i, row in enumerate(A)]
    for col in range(n):
        piv = max(range(col, n), key=lambda r: abs(M[r][col]))
        if abs(M[piv][col]) < 1e-15:
            return None
        M[col], M[piv] = M[piv], M[col]
        for r in range(n):
            if r == col:
                continue
            f = M[r][col] / M[col][col]
            for c in range(col, n + 1):
                M[r][c] -= f * M[col][c]
    return [M[i][n] / M[i][i] for i in range(n)]


def training_rows() -> dict[str, tuple[list[list[float]], list[int], list[int]]]:
    """Build (X, y, seasons) per scenario across every season in the cache."""
    games = {}  # (season, week, team) -> (total, expected_margin)
    with (F.CACHE / "games.csv").open() as fh:
        for r in csv.DictReader(fh):
            if r["game_type"] != "REG":
                continue
            season = F.num(r["season"])
            if season is None or not (F.FIRST <= int(season) <= F.LAST):
                continue
            total, spread = F.num(r["total_line"]), F.num(r["spread_line"])
            if total is None or spread is None:
                continue
            home, away = r["home_team"].strip(), r["away_team"].strip()
            week = int(F.num(r["week"]))
            # spread_line is the HOME team's expected margin.
            games[(int(season), week, home)] = (total, spread)
            games[(int(season), week, away)] = (total, -spread)

    data = {s: ([], [], []) for s in LINE_SCENARIOS}
    for season in range(F.FIRST, F.LAST + 1):
        pbp = F.CACHE / f"play_by_play_{season}.csv.gz"
        if not pbp.exists():
            continue
        proe_tw = proe.team_weeks(season)
        sig_tw = signals_mod.team_weeks(season)
        for name in LINE_SCENARIOS:
            d = F.SCENARIOS[name]
            X, y, seasons = data[name]
            # src is the OBSERVATION key occurred() reads (Scenario.FIELD), not
            # the basis name: offense_proe is read as "proe". Getting this wrong
            # makes every occurred() a None, which silently becomes zero positives.
            if d.basis == "offense_proe":
                tw, field, src = proe_tw, "offense", "proe"
            else:
                tw, field, src = sig_tw, "success_rate", "success_rate"
            for (s, w, team), rec in tw.items():
                g = games.get((s, w, team))
                if g is None:
                    continue
                total, m = g
                hit = d.occurred({src: rec[field]})
                if hit is None:  # quantity missing -- exclude, do not score as a miss
                    continue
                X.append(features(total, m))
                y.append(1 if hit else 0)
                seasons.append(s)
    return data


def held_out_logloss(X, y, seasons) -> float:
    """Leave-one-season-out mean log-loss -- the honest check that 4 params do not overfit."""
    uniq = sorted(set(seasons))
    total_ll, n = 0.0, 0
    for holdout in uniq:
        Xtr = [x for x, s in zip(X, seasons) if s != holdout]
        ytr = [t for t, s in zip(y, seasons) if s != holdout]
        Xte = [x for x, s in zip(X, seasons) if s == holdout]
        yte = [t for t, s in zip(y, seasons) if s == holdout]
        if not Xte or not Xtr:
            continue
        beta, _ = fit_logistic(Xtr, ytr)
        for xi, yi in zip(Xte, yte):
            p = min(max(sigmoid(sum(b * x for b, x in zip(beta, xi))), 1e-9), 1 - 1e-9)
            total_ll += -(yi * math.log(p) + (1 - yi) * math.log(1 - p))
            n += 1
    return total_ll / n if n else float("nan")


def main(argv) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--dry-run", action="store_true", help="print, write nothing")
    ap.add_argument("--out", type=Path, default=ARTIFACT)
    args = ap.parse_args(argv)

    data = training_rows()
    models = {}
    for name in LINE_SCENARIOS:
        X, y, seasons = data[name]
        if len(X) < 100:
            raise SystemExit(f"{name}: only {len(X)} rows; refusing to fit a reference on that")
        beta, converged = fit_logistic(X, y)
        base = sum(y) / len(y)
        models[name] = {
            "coefficients": [round(b, 6) for b in beta],
            "n": len(X),
            "base_rate": round(base, 4),
            "held_out_logloss": round(held_out_logloss(X, y, seasons), 5),
            "converged": converged,
        }
        print(f"{name:20} n={len(X):5} base={base:.4f} "
              f"beta={[round(b,3) for b in beta]} "
              f"heldLL={models[name]['held_out_logloss']:.4f} conv={converged}",
              file=sys.stderr)

    artifact = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "generated_by": "edge/model/analysis/fit_line_model.py",
        "seasons": [F.FIRST, F.LAST],
        "features": ["1", "(total-45)/5", "expected_margin/7", "abs(expected_margin)/7"],
        "transform": {"total_center": TOTAL_CENTER, "total_scale": TOTAL_SCALE,
                      "margin_scale": MARGIN_SCALE},
        "cache": _cache_provenance(["games.csv"]),
        "models": models,
    }
    blob = json.dumps(artifact, indent=1) + "\n"
    if args.dry_run:
        print(blob)
        return 0
    args.out.write_text(blob)
    print(f"wrote {args.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

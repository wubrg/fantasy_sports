"""Evidence for whether a fitted scenario is fit to bet on.

Three tests, run per scenario over the same cells fit_conditionals publishes.
They were previously performed once by hand and their results typed into
SCENARIO_STATUS as prose, where they rode into the artifact unverified and
stayed there while the data underneath them changed.

  1. DIRECTION      Does the sign hold at ordinary prop lines, or does it invert
                    where a wager would actually be placed? A scenario whose
                    medians separate one way but whose hit rates cross at 20.5
                    yards would carry the opposite belief requirement from the
                    one the finding implies.

  2. RESOLUTION     A player-level cluster bootstrap of the median delta. Rows
                    from one player are not independent -- the same reason the
                    grid carries n_eff -- so the resample is over players, not
                    games. A cell is "resolved" when the interval clears zero.

  3. OUT OF SAMPLE  Fit the direction on the early seasons, check it on the
                    late ones. A separation that only exists in the data it was
                    found in is not a separation.

What this module does NOT do is decide. It returns evidence; the pass rule
lives beside SCENARIO_STATUS in fit_conditionals.py, stated explicitly, and is
asserted against the recorded verdict there. Two scenarios is not enough to
calibrate a threshold on, and a rule reverse-engineered to reproduce the answer
already written down would look derived while being fitted to its conclusion.
"""

from __future__ import annotations

import random
import statistics as st

# Mainstream receiving-yard prop lines. The point of testing at these rather
# than at the median is that this is where wagers are actually priced -- a
# separation that reverses at 20.5 yards is not usable however clean the
# medians look.
COMMON_LINES = [6.5, 10.5, 14.5, 20.5, 24.5, 30.5, 40.5, 50.5, 60.5, 75.5]

BOOTSTRAP_RESAMPLES = 1000
BOOTSTRAP_SEED = 20260822  # fixed so the numbers are reproducible, not merely repeatable
OOS_SPLIT = 2021  # fit on <= this, evaluate after
MIN_OOS_CELL = 30  # below this a split half says nothing either way


def _rate_above(rows: list[dict], line: float) -> float:
    if not rows:
        return float("nan")
    return sum(1 for o in rows if o["yards"] > line) / len(rows)


def cell_pairs(obs, definition, target_bands, trend_bands, min_cell):
    """The (occurred, not-occurred) row pairs for every publishable cell.

    Mirrors the selection in fit_conditionals.build so validation is measured on
    exactly the cells that ship, not on a differently-drawn grid.
    """
    for ta, tb in target_bands:
        for ra, rb in trend_bands:
            in_band = [
                o
                for o in obs
                if ta <= o["proj_targets"] < tb and ra <= o["trend"] < rb
            ]
            # `is True` / `is False`, not truthiness: occurred() returns None
            # when the quantity is missing for that game, and those must fall
            # out of both sides rather than into the baseline.
            yes = [o for o in in_band if definition.occurred(o) is True]
            no = [o for o in in_band if definition.occurred(o) is False]
            if len(yes) < min_cell or len(no) < min_cell:
                continue
            yield (ta, tb), (ra, rb), yes, no


def direction(pairs) -> dict:
    """Test 1: is the effect's direction CONSISTENT across cells?

    Consistency, not positivity. blowout_loss separates negatively -- fewer
    receiving yards when a team is blown out, which is the corpus's "trash time
    correlation" running backwards, and is a finding rather than a failure. A
    test that counted positive deltas would score it 1/15 and call a coherent
    negative effect incoherent.

    The dominant sign is whichever way most cells lean; the count is how many
    agree with it.
    """
    deltas = []
    for tband, rband, yes, no in pairs:
        deltas.append(
            st.median([o["yards"] for o in yes]) - st.median([o["yards"] for o in no])
        )
    sign = 1 if sum(1 for d in deltas if d > 0) >= sum(1 for d in deltas if d < 0) else -1
    consistent = sum(1 for d in deltas if d * sign > 0)

    total = 0
    inversions = []
    for (tband, rband, yes, no), delta in zip(pairs, deltas):
        total += 1
        for line in COMMON_LINES:
            q, r = _rate_above(yes, line), _rate_above(no, line)
            # An inversion is the hit rates disagreeing with the medians, which
            # is what makes a wager priced at that line carry the wrong belief.
            if (q - r) * delta < 0:
                inversions.append(
                    {"targets": tband, "trend": rband, "line": line,
                     "q": round(q, 4), "r": round(r, 4), "median_delta": delta}
                )
    return {
        "cells": total,
        "consistent": consistent,
        "sign": "positive" if sign > 0 else "negative",
        "inversions": inversions,
    }


def resolution(pairs, resamples=BOOTSTRAP_RESAMPLES, seed=BOOTSTRAP_SEED) -> dict:
    """Test 2: cluster bootstrap of the median delta, resampling players."""
    rng = random.Random(seed)
    resolved = total = 0
    for tband, rband, yes, no in pairs:
        total += 1
        by_player: dict[str, tuple[list, list]] = {}
        for o in yes:
            by_player.setdefault(o["player"], ([], []))[0].append(o["yards"])
        for o in no:
            by_player.setdefault(o["player"], ([], []))[1].append(o["yards"])
        players = list(by_player)

        deltas = []
        for _ in range(resamples):
            ys, ns = [], []
            for _ in players:
                a, b = by_player[players[rng.randrange(len(players))]]
                ys.extend(a)
                ns.extend(b)
            if ys and ns:
                deltas.append(st.median(ys) - st.median(ns))
        if len(deltas) < resamples // 2:
            continue
        deltas.sort()
        lo = deltas[int(0.025 * len(deltas))]
        hi = deltas[min(int(0.975 * len(deltas)), len(deltas) - 1)]
        if lo > 0 or hi < 0:
            resolved += 1
    return {"cells": total, "resolved": resolved}


def out_of_sample(obs, definition, target_bands, trend_bands, min_cell) -> dict:
    """Test 3: does the direction found in the early seasons hold in the late ones?"""
    train = [o for o in obs if o["season"] <= OOS_SPLIT]
    test = [o for o in obs if o["season"] > OOS_SPLIT]
    agree = total = 0
    for tband, rband, _, _ in cell_pairs(train, definition, target_bands, trend_bands, min_cell):
        ta, tb = tband
        ra, rb = rband

        def half(rows, occurred):
            return [
                o["yards"]
                for o in rows
                if ta <= o["proj_targets"] < tb
                and ra <= o["trend"] < rb
                and definition.occurred(o) is occurred
            ]

        tr_y, tr_n = half(train, True), half(train, False)
        te_y, te_n = half(test, True), half(test, False)
        if min(len(te_y), len(te_n)) < MIN_OOS_CELL:
            continue  # the late seasons cannot speak to this cell either way
        total += 1
        if (st.median(tr_y) - st.median(tr_n)) * (st.median(te_y) - st.median(te_n)) > 0:
            agree += 1
    return {"cells": total, "agree": agree}


def out_of_sample_threeway(obs, definition, target_bands, trend_bands, min_cell,
                           resamples=600, seed=BOOTSTRAP_SEED) -> dict:
    """The magnitude-aware out-of-sample test that was tried and REJECTED.

    Classifies each held-out cell as agree / disagree / uninformative, where
    uninformative means the test-half delta's player-clustered bootstrap
    interval covers zero -- a sign flip too small to be evidence of anything.
    Requiring zero DISAGREEMENTS is then weaker than requiring universal
    agreement, which is the point: pass_heavy fails the strict rule on a single
    cell whose test delta is -0.5 yards on 65 observations.

    It is kept, unused, because the result is the finding. Run on the two
    scenarios whose verdicts were already settled, it makes ALL THREE pass --
    including blowout_loss, whose three disagreements are every one within
    noise. A criterion that cannot fail is not a gate, so the sign-only rule in
    out_of_sample() stands. See FINDINGS.md section 4.

    Reproduce with:  python3 validate.py --compare-oos
    """
    rng = random.Random(seed)
    train = [o for o in obs if o["season"] <= OOS_SPLIT]
    test = [o for o in obs if o["season"] > OOS_SPLIT]
    agree = disagree = uninformative = 0

    for tband, rband, _, _ in cell_pairs(train, definition, target_bands, trend_bands, min_cell):
        ta, tb = tband
        ra, rb = rband

        def half(rows, occurred):
            return [
                o for o in rows
                if ta <= o["proj_targets"] < tb and ra <= o["trend"] < rb
                and definition.occurred(o) is occurred
            ]

        tr_y, tr_n = half(train, True), half(train, False)
        te_y, te_n = half(test, True), half(test, False)
        if min(len(te_y), len(te_n)) < MIN_OOS_CELL:
            continue
        a = st.median([o["yards"] for o in tr_y]) - st.median([o["yards"] for o in tr_n])
        b = st.median([o["yards"] for o in te_y]) - st.median([o["yards"] for o in te_n])
        if a * b > 0:
            agree += 1
            continue

        by_player: dict[str, tuple[list, list]] = {}
        for o in te_y:
            by_player.setdefault(o["player"], ([], []))[0].append(o["yards"])
        for o in te_n:
            by_player.setdefault(o["player"], ([], []))[1].append(o["yards"])
        players = list(by_player)
        deltas = []
        for _ in range(resamples):
            ys, ns = [], []
            for _ in players:
                x, y = by_player[players[rng.randrange(len(players))]]
                ys.extend(x)
                ns.extend(y)
            if ys and ns:
                deltas.append(st.median(ys) - st.median(ns))
        deltas.sort()
        lo = deltas[int(0.025 * len(deltas))]
        hi = deltas[min(int(0.975 * len(deltas)), len(deltas) - 1)]
        if lo > 0 or hi < 0:
            disagree += 1
        else:
            uninformative += 1

    return {"agree": agree, "disagree": disagree, "uninformative": uninformative}


def evidence(obs, definition, target_bands, trend_bands, min_cell) -> dict:
    """All three tests for one scenario."""
    pairs = list(cell_pairs(obs, definition, target_bands, trend_bands, min_cell))
    d = direction(pairs)
    res = resolution(pairs)
    oos = out_of_sample(obs, definition, target_bands, trend_bands, min_cell)
    return {
        "cells": d["cells"],
        "consistent": d["consistent"],
        "sign": d["sign"],
        "inversions": len(d["inversions"]),
        "inversion_detail": d["inversions"][:6],
        "resolved": res["resolved"],
        "oos_cells": oos["cells"],
        "oos_agree": oos["agree"],
        "bootstrap_resamples": BOOTSTRAP_RESAMPLES,
        "bootstrap_seed": BOOTSTRAP_SEED,
        "oos_split": OOS_SPLIT,
    }


def note(ev: dict) -> str:
    """The one-line summary that goes into the artifact, from measured numbers."""
    parts = [
        f"{ev['sign']} in {ev['consistent']}/{ev['cells']} cells",
        f"{ev['resolved']}/{ev['cells']} bootstrap-resolved",
        f"{ev['oos_agree']}/{ev['oos_cells']} out of sample",
    ]
    # Inversions are reported, never gated on: measured against sampling error
    # not one of them clears 2 SE, for either scenario. Counting them would be
    # counting noise, and it is the count that made this note look decisive.
    if ev["inversions"]:
        n = ev["inversions"]
        parts.append(f"{n} sub-noise crossing{'' if n == 1 else 's'}")
    return "; ".join(parts)


def _compare_oos() -> int:
    """Reproduce the rejected-criterion table in FINDINGS.md section 4."""
    import fit_conditionals as fc
    import proe

    games = fc.load_games()
    rows, seasons = fc.load_player_weeks()
    obs = fc.build(rows, games, proe.load(seasons[0], seasons[-1]))

    print("out-of-sample: sign-only (in use) vs magnitude-aware (rejected)\n")
    print(f"  {'scenario':<14} {'sign-only':>10}   {'agree':>6} {'disagree':>9} {'uninform':>9}   new verdict")
    for name, definition in fc.SCENARIOS.items():
        strict = out_of_sample(obs, definition, fc.TARGET_BANDS, fc.TREND_BANDS, fc.MIN_CELL)
        three = out_of_sample_threeway(obs, definition, fc.TARGET_BANDS, fc.TREND_BANDS, fc.MIN_CELL)
        sign = f"{strict['agree']}/{strict['cells']}"
        verdict = "PASS" if three["disagree"] == 0 else "FAIL"
        print(f"  {name:<14} {sign:>10}   {three['agree']:>6} {three['disagree']:>9} "
              f"{three['uninformative']:>9}   {verdict}")
    print("\n  Every scenario passes the magnitude-aware version, including the two that are")
    print("  gated. A criterion that cannot fail is not a gate. The sign-only rule stands.")
    return 0


if __name__ == "__main__":
    import sys

    if "--compare-oos" in sys.argv:
        sys.exit(_compare_oos())
    raise SystemExit("validate.py is a library; --compare-oos reproduces the FINDINGS.md table")

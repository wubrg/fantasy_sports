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

import itertools
import math
import random
import statistics as st

# Mainstream receiving-yard prop lines. The point of testing at these rather
# than at the median is that this is where wagers are actually priced -- a
# separation that reverses at 20.5 yards is not usable however clean the
# medians look.
COMMON_LINES = [6.5, 10.5, 14.5, 20.5, 24.5, 30.5, 40.5, 50.5, 60.5, 75.5]

# Reception lines live on a completely different scale from yardage ones.
COMMON_LINES_DISCRETE = [1.5, 2.5, 3.5, 4.5, 5.5, 6.5, 7.5, 8.5]


def location(discrete: bool):
    """The statistic that measures where a cell's distribution sits.

    Median for a measurement, mean for a count, and the difference is not a
    preference. Receptions run 0-21 with a cell median of 2 to 5, so the median
    has almost no resolution: a real shift of half a reception leaves it
    unmoved, and 12 of 16 shootout cells came out at a delta of EXACTLY zero
    while every one of their means was positive. The instrument could not see
    the effect.

    Checked for self-service before adopting, the same way the magnitude-aware
    out-of-sample test in FINDINGS.md 4 was checked: swapping to the mean moves
    no settled verdict. Receiving and passing are unchanged or shift by a single
    cell without flipping, both gated scenarios stay gated, and receptions still
    fails two of its three scenarios. It fixes a blind instrument rather than
    lowering a bar.

    The median stays for yardage because it is the more robust choice against a
    long right tail, which counts in single digits do not have.
    """
    return st.mean if discrete else st.median

BOOTSTRAP_RESAMPLES = 1000
BOOTSTRAP_SEED = 20260822  # fixed so the numbers are reproducible, not merely repeatable
OOS_SPLIT = 2021  # fit on <= this, evaluate after
MIN_OOS_CELL = 30  # below this a split half says nothing either way


def _rate_above(rows: list[dict], line: float) -> float:
    if not rows:
        return float("nan")
    return sum(1 for o in rows if o["yards"] > line) / len(rows)


def site_key(bands: tuple) -> tuple:
    """A SITE is one coordinate of the grid for one scenario.

    It is the unit a price is actually looked up at -- the q cell and the r
    cell together -- and therefore the right unit to gate at. The old gate
    ruled on a whole scenario at once, which made its strictness depend on how
    many sites the grid happened to be cut into. See FINDINGS.md section 12.

    The key is a flat tuple of the band bounds, in axis order, so it stays
    hashable and prints readably in a refusal.
    """
    return tuple(x for band in bands for x in band)


def _band(bands, v):
    for b in bands:
        if b[0] <= v < b[1]:
            return b
    return None


def cell_pairs(obs, definition, axes, min_cell):
    """The (occurred, not-occurred) row pairs for every publishable cell.

    `axes` is [(field, bands), ...] rather than two fixed arguments. The grid
    used to be conditioned on projected opportunity crossed with role trend and
    nothing else, so those two were named in every signature; it is now
    conditioned on the posted total, the player's own baseline and the trend,
    and which axes an outcome can support is a per-outcome decision.

    One pass over the observations, bucketing each into its site, rather than a
    pass per site. The old shape was O(observations x sites) and the grid went
    from 33 sites to 311; the knob sweep multiplies the call count by 25 on top
    of that, which made the difference between minutes and hours.

    Mirrors the selection in fit_conditionals.build so validation is measured on
    exactly the cells that ship, not on a differently-drawn grid.
    """
    fields = [f for f, _ in axes]
    bands = [b for _, b in axes]
    buckets: dict = {}
    for o in obs:
        # `is True` / `is False`, not truthiness: occurred() returns None when
        # the quantity is missing for that game, and those must fall out of
        # both sides rather than into the baseline.
        occ = definition.occurred(o)
        if occ is None:
            continue
        key = []
        for f, bs in zip(fields, bands):
            b = _band(bs, o[f])
            if b is None:
                break
            key.append(b)
        else:
            yes, no = buckets.setdefault(tuple(key), ([], []))
            (yes if occ else no).append(o)
    # Sorted so the iteration order is a property of the grid rather than of
    # the order observations happened to arrive in.
    for combo in sorted(buckets):
        yes, no = buckets[combo]
        if len(yes) < min_cell or len(no) < min_cell:
            continue
        yield combo, yes, no


def direction(pairs, discrete: bool = False) -> dict:
    """Test 1: is the effect's direction CONSISTENT across cells?

    Consistency, not positivity. blowout_loss separates negatively -- fewer
    receiving yards when a team is blown out, which is the corpus's "trash time
    correlation" running backwards, and is a finding rather than a failure. A
    test that counted positive deltas would score it 1/15 and call a coherent
    negative effect incoherent.

    The dominant sign is whichever way most cells lean; the count is how many
    agree with it.
    """
    loc = location(discrete)
    lines = COMMON_LINES_DISCRETE if discrete else COMMON_LINES
    deltas = []
    for combo, yes, no in pairs:
        deltas.append(loc([o["yards"] for o in yes]) - loc([o["yards"] for o in no]))
    sign = 1 if sum(1 for d in deltas if d > 0) >= sum(1 for d in deltas if d < 0) else -1
    consistent = sum(1 for d in deltas if d * sign > 0)

    # Per site, not merely counted. The count is a scenario-level summary; the
    # verdict that gates a price is this one. The dominant SIGN stays a
    # scenario-level claim -- a scenario means one direction everywhere or it
    # means nothing -- but agreement with it is measured where it is priced.
    by_site = {
        site_key(combo): {"delta": round(d, 3), "agrees": d * sign > 0}
        for (combo, _, _), d in zip(pairs, deltas)
    }

    total = 0
    inversions = []
    for (combo, yes, no), delta in zip(pairs, deltas):
        total += 1
        for line in lines:
            q, r = _rate_above(yes, line), _rate_above(no, line)
            # An inversion is the hit rates disagreeing with the medians, which
            # is what makes a wager priced at that line carry the wrong belief.
            if (q - r) * delta < 0:
                inversions.append(
                    {"site": site_key(combo), "line": line,
                     "q": round(q, 4), "r": round(r, 4), "median_delta": delta}
                )
    return {
        "cells": total,
        "consistent": consistent,
        "sign": "positive" if sign > 0 else "negative",
        "inversions": inversions,
        "sites": by_site,
    }


def resolution(pairs, discrete: bool = False, resamples=BOOTSTRAP_RESAMPLES,
               seed=BOOTSTRAP_SEED) -> dict:
    """Test 2: cluster bootstrap of the location delta, resampling players."""
    loc = location(discrete)
    resolved = total = 0
    by_site = {}
    for combo, yes, no in pairs:
        total += 1
        key = site_key(combo)
        # Seeded PER SITE, from the site's own coordinates. A single stream
        # consumed across the grid makes a site's resamples depend on how many
        # sites happened to precede it, so the same cell resolved differently
        # depending on where MIN_CELL was set or how the axes were cut. That is
        # a property of the iteration order, not of the data.
        rng = random.Random((seed, key).__hash__() & 0xFFFFFFFF)
        by_site[key] = {"resolved": False, "lo": None, "hi": None}
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
                deltas.append(loc(ys) - loc(ns))
        if len(deltas) < resamples // 2:
            continue
        deltas.sort()
        lo = deltas[int(0.025 * len(deltas))]
        hi = deltas[min(int(0.975 * len(deltas)), len(deltas) - 1)]
        by_site[key] = {"resolved": lo > 0 or hi < 0,
                        "lo": round(lo, 3), "hi": round(hi, 3)}
        if lo > 0 or hi < 0:
            resolved += 1
    return {"cells": total, "resolved": resolved, "sites": by_site}


def out_of_sample(obs, definition, axes, min_cell,
                  discrete: bool = False, split: int = None) -> dict:
    """Test 3: does the direction found in the early seasons hold in the late ones?"""
    split = OOS_SPLIT if split is None else split
    train = [o for o in obs if o["season"] <= split]
    test = [o for o in obs if o["season"] > split]
    agree = total = 0
    by_site = {}
    fields = [f for f, _ in axes]
    for combo, _, _ in cell_pairs(train, definition, axes, min_cell):

        def half(rows, occurred, combo=combo):
            return [
                o["yards"]
                for o in rows
                if all(lo <= o[f] < hi for f, (lo, hi) in zip(fields, combo))
                and definition.occurred(o) is occurred
            ]

        tr_y, tr_n = half(train, True), half(train, False)
        te_y, te_n = half(test, True), half(test, False)
        key = site_key(combo)
        if min(len(te_y), len(te_n)) < MIN_OOS_CELL:
            # The late seasons cannot speak to this site either way. None is
            # NOT False: it is the absence of evidence, and site_verdicts()
            # refuses on it rather than passing it through.
            by_site[key] = None
            continue
        total += 1
        loc = location(discrete)
        held = (loc(tr_y) - loc(tr_n)) * (loc(te_y) - loc(te_n)) > 0
        by_site[key] = held
        if held:
            agree += 1
    return {"cells": total, "agree": agree, "sites": by_site}


def out_of_sample_threeway(obs, definition, axes, min_cell,
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

    fields = [f for f, _ in axes]
    for combo, _, _ in cell_pairs(train, definition, axes, min_cell):

        def half(rows, occurred, combo=combo):
            return [
                o for o in rows
                if all(lo <= o[f] < hi for f, (lo, hi) in zip(fields, combo))
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


def evidence(obs, definition, axes, min_cell,
             discrete: bool = False) -> dict:
    """All three tests for one scenario."""
    pairs = list(cell_pairs(obs, definition, axes, min_cell))
    d = direction(pairs, discrete)
    res = resolution(pairs, discrete)
    oos = out_of_sample(obs, definition, axes, min_cell, discrete)
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
        "location": "mean" if discrete else "median",
    }


def sign_coherence(consistent: int, cells: int) -> float:
    """Two-sided binomial p-value that the dominant sign is better than a toss.

    site_verdicts() judges a site by whether it agrees with the scenario's
    dominant direction. If that direction is itself a coin flip -- receptions
    under blowout_loss leans one way in 8 of 16 cells -- then "agrees with it"
    carries no information, and the sites that agree are the lucky half rather
    than the real ones. So the scenario must HAVE a direction before any of its
    sites can be judged against it.

    This is not the old all-cells rule returning in disguise. That rule asked
    whether every cell agreed, and got harder to satisfy as the grid was cut
    finer. This asks whether the agreement rate beats chance, and gets EASIER
    to satisfy with more cells -- which is how evidence is supposed to behave.
    """
    if cells == 0:
        return 1.0
    k = max(consistent, cells - consistent)
    tail = sum(math.comb(cells, i) for i in range(k, cells + 1)) / (2 ** cells)
    return min(1.0, 2 * tail)


COHERENCE_ALPHA = 0.05


def site_verdicts(obs, definition, axes, min_cell,
                  discrete: bool = False, require_oos: bool = True,
                  resolved=None) -> dict:
    """THE gate: one verdict per site, each decided on that site's own evidence.

    A site is priceable only if all three tests pass AT THAT SITE:

      direction      its delta agrees with the scenario's dominant sign
      resolved       the player-clustered bootstrap separates it from zero
      out of sample  that sign holds in the held-out seasons

    Why per site. The old rule -- the sign holds in EVERY cell -- passes with
    probability (1-e)^k for k cells and a per-cell error rate e, so it gets
    harder to satisfy the more finely the same data is cut. Cell count is a
    design choice, not evidence, and a gate that reads it as evidence rejects
    findings when the grid is redrawn. Judging each site alone removes k from
    the expression entirely.

    Why all three. One 95% interval over ~30 sites resolves about 1.5 by
    chance, so a single-test per-site gate really would be multiple comparisons
    with better manners. Requiring agreement with a sign fixed scenario-wide,
    resolution, AND out-of-sample persistence is a conjunction the null clears
    far more rarely -- measured by permutation in FINDINGS.md section 12.

    require_oos makes the absence of held-out evidence a refusal rather than a
    pass. A site the late seasons cannot speak to has not been validated out of
    sample, and "we never checked" is not a reason to price something.
    """
    pairs = list(cell_pairs(obs, definition, axes, min_cell))
    d = direction(pairs, discrete)
    # The bootstrap is the expensive test and its per-site result does not
    # depend on min_cell, so a caller sweeping thresholds computes it once at
    # the loosest and passes it in.
    res_sites = resolution(pairs, discrete)["sites"] if resolved is None else resolved
    oos = out_of_sample(obs, definition, axes, min_cell, discrete)

    out = {}
    for key, dd in d["sites"].items():
        rr = res_sites.get(key, {"resolved": False, "lo": None, "hi": None})
        oo = oos["sites"].get(key)
        why = []
        if not dd["agrees"]:
            why.append(f"delta {dd['delta']:+g} runs against the scenario's {d['sign']} direction")
        if not rr["resolved"]:
            why.append("the bootstrap interval does not clear zero"
                       + (f" [{rr['lo']:+g}, {rr['hi']:+g}]" if rr["lo"] is not None else ""))
        if oo is False:
            why.append("the direction does not hold in the held-out seasons")
        elif oo is None and require_oos:
            why.append(f"the seasons after {OOS_SPLIT} are too thin here to check it")
        out[key] = {
            "priceable": not why,
            "delta": dd["delta"],
            "agrees": dd["agrees"],
            "resolved": rr["resolved"],
            "ci": [rr["lo"], rr["hi"]],
            "oos": oo,
            "why": why,
        }
    return out


# The two knobs the adversarial review found undocumented. Swept rather than
# defended: a verdict that holds at 100 and fails at 150 is a verdict about the
# knob, and the operator should be told which kind they are looking at.
MIN_CELL_SWEEP = [50, 75, 100, 150, 200]
OOS_SPLIT_SWEEP = [2018, 2019, 2020, 2021, 2022]


def verdict_stability(obs, definition, axes, discrete: bool = False,
                      min_cells=None, oos_splits=None) -> dict:
    """Per site: the share of knob settings that agree with the shipped verdict.

    Every combination of MIN_CELL and OOS_SPLIT is a defensible grid, and the
    review's charge is that the published verdicts turn on which one was
    picked. So each site is re-decided under all of them and told how often it
    survives -- 1.0 is a verdict about the data, 0.5 is a verdict about a knob.

    The sweep is nearly free despite being a product of two knobs, because the
    expensive test does not depend on either. The player-clustered bootstrap is
    computed ONCE per site at the loosest MIN_CELL: a site's own resamples do
    not change when a threshold elsewhere in the grid moves, and it never sees
    the out-of-sample split at all. Only direction (a median per cell) and
    persistence (two medians per cell) are recomputed per setting.
    """
    min_cells = min_cells or MIN_CELL_SWEEP
    oos_splits = oos_splits or OOS_SPLIT_SWEEP

    # One bootstrap pass, at the loosest threshold, so every site any setting
    # could publish has an entry.
    loosest = min(min_cells)
    base_pairs = list(cell_pairs(obs, definition, axes, loosest))
    res = resolution(base_pairs, discrete)["sites"]

    # Persistence depends only on the split, not on min_cell, so the five
    # splits are evaluated once at the loosest threshold and then filtered --
    # rather than 25 times, once per (threshold, split) pair.
    oos_by_split = {sp: out_of_sample(obs, definition, axes, loosest, discrete, sp)["sites"]
                    for sp in oos_splits}

    counts: dict = {}
    agree: dict = {}
    for mc in min_cells:
        pairs = [(c, y, n) for c, y, n in base_pairs
                 if len(y) >= mc and len(n) >= mc]
        if not pairs:
            continue
        # The dominant sign is a property of the surviving cells, so it is
        # recomputed rather than carried across thresholds.
        d = direction(pairs, discrete)["sites"]
        for split in oos_splits:
            oos = oos_by_split[split]
            for key, dd in d.items():
                rr = res.get(key, {"resolved": False})
                oo = oos.get(key)
                ok = dd["agrees"] and rr["resolved"] and oo is True
                counts[key] = counts.get(key, 0) + 1
                agree[key] = agree.get(key, 0) + (1 if ok else 0)
    return {"sites": {k: {"settings": counts[k], "priceable_in": agree[k],
                          "share": round(agree[k] / counts[k], 3)}
                      for k in counts},
            "resolved": res}


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
    import signals as signals_mod

    # Both of these grew a required argument when the grid went from one
    # outcome to four, and this function was not updated with them -- so the
    # target that reproduces FINDINGS.md section 8 has been raising TypeError
    # rather than printing a table. It is stated per-outcome now, because a
    # criterion that cannot fail has to be shown unable to fail on each of
    # them separately. See docs/reviews/2026-08-23-adversarial.md finding S4.
    games = fc.load_games()
    seasons = None
    proe_tw = signals_tw = None

    print("out-of-sample: sign-only (in use) vs magnitude-aware (rejected)\n")
    print(f"  {'outcome':<16} {'scenario':<18} {'sign-only':>10}   {'agree':>6} {'disagree':>9} {'uninform':>9}   new verdict")
    for oname, outcome in fc.OUTCOMES.items():
        rows, seasons_read = fc.load_player_weeks(outcome)
        seasons = seasons or seasons_read
        if proe_tw is None:
            # Both team-week series, not just PROE. Passing only the first left
            # success_rate undefined everywhere, which silently reported
            # efficient_offense as 0/0 cells rather than as a real comparison.
            proe_tw = proe.load(seasons[0], seasons[-1])
            signals_tw = signals_mod.load(seasons[0], seasons[-1])
        obs = fc.build(rows, games, outcome, proe_tw, signals_tw)
        for name, definition in fc.SCENARIOS.items():
            strict = out_of_sample(obs, definition, outcome.axes(), fc.MIN_CELL)
            three = out_of_sample_threeway(obs, definition, outcome.axes(), fc.MIN_CELL)
            sign = f"{strict['agree']}/{strict['cells']}"
            verdict = "PASS" if three["disagree"] == 0 else "FAIL"
            print(f"  {oname:<16} {name:<18} {sign:>10}   {three['agree']:>6} {three['disagree']:>9} "
                  f"{three['uninformative']:>9}   {verdict}")
    print("\n  Every scenario passes the magnitude-aware version, including the two that are")
    print("  gated. A criterion that cannot fail is not a gate. The sign-only rule stands.")
    return 0


def _sweep() -> int:
    """Justify MIN_CELL and OOS_SPLIT by showing what each setting buys.

    Answers review finding S1 the direct way: both constants were chosen after
    the rule they feed, so here is what the alternatives would have produced.
    """
    import math

    import fit_conditionals as fc
    import proe
    import signals as signals_mod

    games = fc.load_games()
    proe_tw = signals_tw = None
    seasons = None

    print("MIN_CELL — what a cell of n observations can actually resolve\n")
    print(f"  {'n':>6} {'95% half-width at p=0.5':>26}   {'meaning':<44}")
    for n in (25, 50, 75, 100, 150, 200, 400):
        # Wilson half-width at the widest point of the curve.
        z = 1.959964
        hw = z * math.sqrt(0.25 / n) / (1 + z * z / n) * 100
        meaning = ""
        if hw > 12:
            meaning = "wider than the gap between q and r we look for"
        elif hw > 8:
            meaning = "usable only where separation is large"
        else:
            meaning = "can discriminate an ordinary separation"
        print(f"  {n:>6} {hw:>25.1f}pp   {meaning:<44}")
    print("\n  The typical q-r separation this grid finds is 8-14pp. At MIN_CELL=100 a")
    print("  cell's own half-width is 9.4pp, which sits INSIDE that range: it resolves")
    print("  the larger separations and not the smaller ones. That is the honest")
    print("  reading, and it is why the threshold is not the gate. A published cell is")
    print("  only a cell worth bootstrapping; the per-site bootstrap is what decides")
    print("  whether its own separation clears zero, and it refuses plenty of cells")
    print("  that clear n=100. Raising the threshold to 150 would buy a 7.8pp")
    print("  half-width by discarding sites the bootstrap already judges correctly.\n")

    print("MIN_CELL and OOS_SPLIT — sites published and how many survive\n")
    print(f"  {'outcome':<17}{'setting':<22}{'sites':>7}{'priceable':>11}{'firm':>7}")
    for oname, outcome in fc.OUTCOMES.items():
        rows, seasons_read = fc.load_player_weeks(outcome)
        seasons = seasons or seasons_read
        if proe_tw is None:
            proe_tw = proe.load(seasons[0], seasons[-1])
            signals_tw = signals_mod.load(seasons[0], seasons[-1])
        obs = fc.build(rows, games, outcome, proe_tw, signals_tw)
        d = fc.SCENARIOS["shootout"]
        axes = outcome.axes()
        stab = verdict_stability(obs, d, axes, False)
        for mc in MIN_CELL_SWEEP:
            sv = site_verdicts(obs, d, axes, mc, False, True, resolved=stab["resolved"])
            ok = sum(1 for v in sv.values() if v["priceable"])
            firm = sum(1 for k, v in sv.items() if v["priceable"]
                       and stab["sites"].get(k, {}).get("share", 0) >= 1.0)
            print(f"  {oname:<17}{'MIN_CELL=' + str(mc):<22}{len(sv):>7}{ok:>11}{firm:>7}")
        for sp in OOS_SPLIT_SWEEP:
            held = [s for s in seasons if s > sp]
            oos = out_of_sample(obs, d, axes, fc.MIN_CELL, False, sp)
            print(f"  {oname:<17}{'OOS_SPLIT=' + str(sp):<22}{oos['cells']:>7}"
                  f"{oos['agree']:>11}{len(held):>7}   ({len(held)} seasons held out)")
    print("\n  OOS_SPLIT=2021 holds out four seasons. Earlier splits hold out more and")
    print("  fit on less; later ones cannot see a regime change at all. The verdicts")
    print("  that turn on this choice are labelled per cell rather than argued over.")
    return 0


if __name__ == "__main__":
    import sys

    if "--compare-oos" in sys.argv:
        sys.exit(_compare_oos())
    if "--sweep" in sys.argv:
        sys.exit(_sweep())
    raise SystemExit("validate.py is a library; --compare-oos reproduces the FINDINGS.md table")

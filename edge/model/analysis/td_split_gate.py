#!/usr/bin/env python3
"""Does splitting anytime TD into two TYPE-PURE outcomes beat the combined one?

FINDINGS.md 18 measured `anytime_td` (rushing + receiving TDs, summed) at one
priceable site of 110. The failure was not the effect -- every scenario has a
real dominant direction -- it was RESOLUTION: combined TDs are zero in ~72% of
player-weeks, so within almost every cell more than half the games are 0 and the
median ratio to the player's own baseline is pinned at 0.0 on BOTH sides. A
median delta of zero cannot separate from zero under the player-clustered
bootstrap the pipeline uses to gate a site.

The idea measured here: instead of one combined outcome, fit two type-pure ones
that mirror the existing production outcomes exactly, swapping the TD column in
for the yards/reception column --

    rushing_tds    rushing_tds / carries, RB, like rushing_yards
    receiving_tds  receiving_tds / targets, WR/TE/RB, like receiving_yards

The hope is that a position-pure population (goal-line backs, red-zone receivers)
has a high enough weekly TD rate in its TOP baseline tier to unpin the median
that defeated the combined stat. That is an empirical question about the top
tier's week-to-week zero rate, and it is measured here rather than assumed.

Three questions, mirroring the rigor of targets_gate1.py (which backs
FINDINGS.md 19):

  SITES       fit both through the real pipeline (fit_conditionals.build +
              validate.site_verdicts, the same median instrument every shipped
              outcome is gated on) and count priceable sites per scenario.
  BASELINE    what fraction of weeks is each stat zero, within the position-pure
              population, and where do the baseline quartile cuts land? This is
              what decides whether splitting fixes 18's resolution problem.
  COMBINE     for a real dual-threat cohort (top-quartile RB target share),
              combine the two type-pure P(>=1 TD) lookups under independence,
              P(either) = 1 - (1-p_rush)(1-p_rec), and backtest it against the
              realized anytime-TD rate in held-out seasons. Measure, not assume,
              the independence of a player's rush-TD and rec-TD weeks.

The bar for shipping two new Outcome entries: meaningfully more than the combined
stat's 1/110 -- comparable to what passing_tds cleared (20/39). See FINDINGS.md
section 20 for the verdict.

Dependency-free beyond the sibling analysis modules. stdlib only.

Usage:
    python3 td_split_gate.py          # all three sections
"""

from __future__ import annotations

import csv
import math
import statistics as st
import sys
from collections import Counter, defaultdict

import fit_conditionals as fc
import validate

OOS_SPLIT = validate.OOS_SPLIT  # 2021


def type_pure(name, yfield, ofield, positions, baseline_bands, bands, trend_bands,
              posted_bands):
    """A TD outcome built to mirror its yardage parent, TD column swapped in.

    min_output is the 0.1 floor anytime_td uses, not the parent's 5.0: a TD
    baseline is far smaller than a yardage one, and a ratio to a 0.1 baseline is
    already unstable, so the floor drops the noisiest tail and keeps the rest.
    """
    return fc.Outcome(
        name, yfield, ofield, positions,
        share_based=True, bands=bands, trend_bands=trend_bands,
        min_baseline=fc.MIN_BASELINE_SHARE,
        baseline_bands=baseline_bands, min_output=0.1,
        posted_bands=posted_bands, discrete=True, unit="td",
    )


def outcomes():
    # Baseline tiers are cut at the measured quartiles reported by --baseline.
    rush = type_pure(
        "rushing_tds", "rushing_tds", "carries", {"RB"},
        baseline_bands=[(0, 0.2), (0.2, 0.33), (0.33, 0.5), (0.5, 999)],
        bands=fc.CARRY_BANDS, trend_bands=fc.CARRY_TREND_BANDS,
        posted_bands=[(0, 999)],  # like rushing_yards
    )
    rec = type_pure(
        "receiving_tds", "receiving_tds", "targets", {"WR", "TE", "RB"},
        baseline_bands=[(0, 0.17), (0.17, 0.27), (0.27, 0.43), (0.43, 999)],
        bands=fc.TARGET_BANDS, trend_bands=fc.TREND_BANDS,
        posted_bands=fc.POSTED_BANDS,  # like receiving_yards (two bands)
    )
    return rush, rec


# ---------------------------------------------------------------- SITES

def sites(outcome, obs):
    """Priceable-site count per scenario, reproducing fit_conditionals' report.

    Uses discrete=False exactly as fit_conditionals.py does for every outcome:
    the median is the deliberate conservative instrument since the ratio change
    (FINDINGS 6), and swapping to the mean to manufacture TD sites is the move
    the pipeline exists to refuse (FINDINGS 18)."""
    print(f"\n=== {outcome.name} === usable observations: {len(obs)}")
    print("  scenario            sites  priceable  sign_p    note")
    total_sites = total_price = 0
    for scenario, definition in fc.SCENARIOS.items():
        ev = validate.evidence(obs, definition, outcome.axes(), fc.MIN_CELL, False)
        p_sign = validate.sign_coherence(ev["consistent"], ev["cells"])
        incoherent = p_sign >= validate.COHERENCE_ALPHA
        stab = validate.verdict_stability(obs, definition, outcome.axes(), False)
        sv = validate.site_verdicts(obs, definition, outcome.axes(), fc.MIN_CELL,
                                    False, require_oos=True, resolved=stab["resolved"])
        if incoherent:
            for v in sv.values():
                v["priceable"] = False
        ok = sum(1 for v in sv.values() if v["priceable"])
        n = len(sv)
        total_sites += n
        total_price += ok
        note = "[NO DIRECTION]" if incoherent else ""
        print(f"  {scenario:18}  {n:>4}  {ok:>8}   {p_sign:6.4f}   {note}")
    print(f"  {'TOTAL':18}  {total_sites:>4}  {total_price:>8}")
    return total_sites, total_price


# ---------------------------------------------------------------- BASELINE

def baseline(outcome, obs):
    print(f"\n=== {outcome.name} === population: {len(obs)} usable player-weeks")
    raw = [o["output"] for o in obs]
    n = len(raw)
    c = Counter(raw)
    z, one = c.get(0.0, 0), c.get(1.0, 0)
    print(f"  weekly raw TDs: 0 -> {z/n:.1%}   1 -> {one/n:.1%}   "
          f">=2 -> {(n-z-one)/n:.1%}   (mean {st.mean(raw):.3f}, median 0)")
    base = sorted(o["baseline_yards"] for o in obs)

    def q(p):
        return base[min(int(p * len(base)), len(base) - 1)]

    cuts = [q(.25), q(.50), q(.75)]
    print(f"  prior-mean TD/game quartiles: p25 {cuts[0]:.3f}  p50 {cuts[1]:.3f}  "
          f"p75 {cuts[2]:.3f}  (range {base[0]:.3f}-{base[-1]:.3f})")

    def tier(b):
        return 0 if b < cuts[0] else 1 if b < cuts[1] else 2 if b < cuts[2] else 3

    tiers = defaultdict(list)
    for o in obs:
        tiers[tier(o["baseline_yards"])].append(o)
    print("  tier  n       wk-nonzero%   median-raw   median-ratio")
    for t, lbl in enumerate(["Q1", "Q2", "Q3", "Q4"]):
        g = tiers[t]
        if not g:
            continue
        nz = sum(1 for x in g if x["output"] > 0) / len(g)
        print(f"  {lbl:<4}  {len(g):<6}  {nz:9.1%}   "
              f"{st.median([x['output'] for x in g]):>8.1f}    "
              f"{st.median([x['yards'] for x in g]):>10.3f}")


# ---------------------------------------------------------------- COMBINE

def combine():
    """Dual-threat cohort backtest for the 'combine after the fact' idea."""
    games = fc.load_games()
    rec_oc = fc.OUTCOMES["receiving_yards"]  # for the team target pool
    rec_rows, _ = fc.load_player_weeks(rec_oc)
    tpool = defaultdict(float)
    for r in rec_rows:
        tpool[(r["season"], r["week"], r["team"])] += r["opportunity"]

    rb = defaultdict(dict)
    for season in range(fc.FIRST, fc.LAST + 1):
        path = fc.CACHE / f"stats_player_week_{season}.csv"
        for r in csv.DictReader(path.open()):
            if r.get("season_type") != "REG" or r.get("position") != "RB":
                continue
            wk = int(fc.num(r["week"]))
            rb[(r.get("player_id", ""), season)][wk] = {
                "season": season, "week": wk,
                "team": r.get("team") or r.get("recent_team", ""),
                "rush_td": fc.num(r.get("rushing_tds")),
                "rec_td": fc.num(r.get("receiving_tds")),
                "targets": fc.num(r.get("targets")),
            }

    records = []
    for (pid, season), wks in rb.items():
        g = [wks[w] for w in sorted(wks)]
        for w in g:
            tp = tpool.get((w["season"], w["week"], w["team"]), 0.0)
            w["tshare"] = w["targets"] / tp if tp > 0 else 0.0
        for i in range(len(g)):
            if i < fc.MIN_PRIOR_GAMES:
                continue
            prior, cur = g[:i], g[i]
            n = len(prior)
            records.append({
                "player": pid, "season": cur["season"],
                "tshare": sum(p["tshare"] for p in prior) / n,
                "rate_rush": sum(1 for p in prior if p["rush_td"] > 0) / n,
                "rate_rec": sum(1 for p in prior if p["rec_td"] > 0) / n,
                "rate_any": sum(1 for p in prior
                                if (p["rush_td"] + p["rec_td"]) > 0) / n,
                "base_any": sum(p["rush_td"] + p["rec_td"] for p in prior) / n,
                "rush_hit": 1 if cur["rush_td"] > 0 else 0,
                "rec_hit": 1 if cur["rec_td"] > 0 else 0,
                "any_hit": 1 if (cur["rush_td"] + cur["rec_td"]) > 0 else 0,
            })

    shares = sorted(r["tshare"] for r in records)
    cut = shares[int(0.75 * len(shares))]
    cohort = [r for r in records if r["tshare"] >= cut]
    print(f"\neligible RB player-weeks: {len(records)}")
    print(f"dual-threat cohort = top-quartile prior target share "
          f"(>= {cut*100:.1f}% of team targets): {len(cohort)} weeks, "
          f"{len({r['player'] for r in cohort})} players")

    rh = [r["rush_hit"] for r in cohort]
    ch = [r["rec_hit"] for r in cohort]

    def phi(a, b):
        na = len(a)
        ma, mb = sum(a) / na, sum(b) / na
        cov = sum((x - ma) * (y - mb) for x, y in zip(a, b)) / na
        sa = math.sqrt(sum((x - ma) ** 2 for x in a) / na)
        sb = math.sqrt(sum((y - mb) ** 2 for y in b) / na)
        return cov / (sa * sb) if sa and sb else float("nan")

    pr, pc = sum(rh) / len(rh), sum(ch) / len(ch)
    both = sum(1 for r in cohort if r["rush_hit"] and r["rec_hit"]) / len(cohort)
    print("\nINDEPENDENCE (cohort, all seasons):")
    print(f"  P(rush TD) {pr:.4f}  P(rec TD) {pc:.4f}  "
          f"P(both) observed {both:.4f} vs independence {pr*pc:.4f}")
    print(f"  phi(rush_hit, rec_hit) = {phi(rh, ch):+.4f}")

    test = [r for r in cohort if r["season"] > OOS_SPLIT]
    realized = [r["any_hit"] for r in test]

    def brier(p):
        return sum((a - b) ** 2 for a, b in zip(p, realized)) / len(realized)

    comb = [1 - (1 - r["rate_rush"]) * (1 - r["rate_rec"]) for r in test]
    any_rate = [r["rate_any"] for r in test]
    any_mean = [min(r["base_any"], 1.0) for r in test]
    print(f"\nBACKTEST, held-out cohort weeks (season > {OOS_SPLIT}): {len(test)}")
    print(f"  realized P(>=1 anytime TD)           {sum(realized)/len(realized):.4f}")
    for lbl, p in [("combined P(>=1): 1-(1-pr)(1-pc)", comb),
                   ("anytime P(>=1) tracked directly", any_rate),
                   ("anytime MEAN (18's instrument)  ", any_mean)]:
        print(f"  {lbl}  mean {sum(p)/len(p):.4f}  Brier {brier(p):.4f}")


def main():
    import proe as proe_mod
    import signals as signals_mod

    rush, rec = outcomes()
    games = fc.load_games()
    # The PROE / success-rate team-week series the pass_heavy and
    # efficient_offense scenarios are defined on. Without them those two
    # scenarios report "cannot say" for every game and drop out, understating
    # the denominator -- so load them exactly as fit_conditionals.main() does.
    proe_tw = proe_mod.load(fc.FIRST, fc.LAST)
    signals_tw = signals_mod.load(fc.FIRST, fc.LAST)
    print(f"team-weeks with PROE: {len(proe_tw)}   with rates: {len(signals_tw)}")
    built = {}
    for oc in (rush, rec):
        rows, _ = fc.load_player_weeks(oc)
        built[oc.name] = (oc, fc.build(rows, games, oc, proe_tw, signals_tw))

    print("#" * 72)
    print("SITES -- priceable per scenario (median instrument, as shipped)")
    print("#" * 72)
    for oc, obs in built.values():
        sites(oc, obs)

    print("\n" + "#" * 72)
    print("BASELINE -- zero rate and quartile tiers within the pure population")
    print("#" * 72)
    for oc, obs in built.values():
        baseline(oc, obs)

    print("\n" + "#" * 72)
    print("COMBINE -- dual-threat independence combination, backtested")
    print("#" * 72)
    combine()
    return 0


if __name__ == "__main__":
    sys.exit(main())

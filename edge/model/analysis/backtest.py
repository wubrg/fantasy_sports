"""Score the whole apparatus on seasons it never saw.

Everything before this file measured a component. This measures the product:
take a grid fitted through some season, take the belief model fitted the same
way, and ask what they said about games that came later.

It reads the ARTIFACT rather than rebuilding the grid in memory. A backtest
that reconstructs what it is scoring cannot see anything the serialisation
does, which is exactly how a one-decimal rounding survived every calibration
check this project had (FINDINGS 11).

The two halves are scored SEPARATELY, because P(hit) = q*s + r*(1-s) can be
wrong in two unrelated ways and a single number would not say which:

  A  q and r          conditioned on what actually happened, so s is removed
  B  s                the belief model's own calibration
  C  the product      and what it would have been worth against -110

There are no historical prop lines in this project, so C simulates them at a
multiple of each player's own baseline. That is the same assumption the book
makes when it sets one, and it is stated rather than hidden.

Usage:
  python3 backtest.py --grid <artifact.json> --from 2025
  python3 backtest.py --grid <artifact.json> --from 2025 --since 2026-01-01
"""
import argparse
import bisect
import csv
import json
import statistics as st
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F
import proe
import signals as signals_mod

BREAKEVEN = 0.5238  # -110

# A ladder well past the median, because that is where the thesis lives: a book
# sets its line near the middle and the conditional and unconditional
# distributions diverge most in the tail.
TAIL_MULTS = [1.0, 1.15, 1.3, 1.5, 1.75, 2.0]


def prob_above(quantiles, x) -> float:
    xs = [p[1] for p in quantiles]
    ps = [p[0] for p in quantiles]
    if x <= xs[0]:
        return 1.0
    if x >= xs[-1]:
        return 0.0
    i = bisect.bisect_left(xs, x)
    if xs[i] == x:
        return 1 - ps[i]
    lo, hi = xs[i - 1], xs[i]
    plo, phi = ps[i - 1], ps[i]
    return 1 - (plo + (x - lo) / (hi - lo) * (phi - plo) if hi > lo else plo)


def kickoffs() -> dict:
    """(season, week, team) -> gameday, so a slate can be cut by date."""
    out = {}
    for r in csv.DictReader((F.CACHE / "games.csv").open()):
        if not r["gameday"].strip():
            continue
        k = (int(F.num(r["season"])), int(F.num(r["week"])))
        out[k + (r["home_team"],)] = r["gameday"]
        out[k + (r["away_team"],)] = r["gameday"]
    return out


def load_grid(path: Path) -> tuple[dict, list]:
    art = json.loads(path.read_text())
    index = {}
    for c in art["cells"]:
        index.setdefault((c["outcome"], c["scenario"]), []).append(c)
    return art, index


def find(index, outcome, scenario, occurred, posted, baseline, trend):
    for c in index.get((outcome, scenario), ()):
        if (c["occurred"] == occurred
                and c["posted_min"] <= posted < c["posted_max"]
                and c["baseline_min"] <= baseline < c["baseline_max"]
                and c["trend_min"] <= trend < c["trend_max"]):
            return c
    return None


def belief_p(model: dict, prior: float):
    for b in model["bands"]:
        lo = b["min"] if b["min"] is not None else float("-inf")
        hi = b["max"] if b["max"] is not None else float("inf")
        if lo <= prior < hi:
            return b["p"]
    return None


def main(argv) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--grid", type=Path, required=True, help="a conditionals artifact")
    ap.add_argument("--belief", type=Path,
                    default=F.ARTIFACT.parent / "belief.json")
    ap.add_argument("--from", dest="first_eval", type=int, required=True,
                    help="first season to SCORE (must be after the grid's fit window)")
    ap.add_argument("--since", default=None, help="only games on/after this date, e.g. 2026-01-01")
    ap.add_argument("--tails", action="store_true",
                    help="test the tail thesis: does a belief-derived s beat a base-rate "
                         "blend by more than the vig, and does the gap widen with depth?")
    ap.add_argument("--hold", type=float, default=0.06,
                    help="PROPORTIONAL hold at the line. A flat number of points would "
                         "tax a 12%% line at 20%%, which no book does")
    ap.add_argument("--multiples", default="0.85,1.0,1.15",
                    help="simulated lines, as multiples of the player's own baseline")
    args = ap.parse_args(argv)

    grid, index = load_grid(args.grid)
    belief = json.loads(args.belief.read_text())["scenarios"]
    if grid["seasons"][1] >= args.first_eval:
        raise SystemExit(
            f"the grid was fitted through {grid['seasons'][1]} and you asked to score "
            f"from {args.first_eval}. That is not a backtest -- refit with "
            f"`--through {args.first_eval - 1}`.")
    print(f"grid fitted {grid['seasons'][0]}-{grid['seasons'][1]}, "
          f"scoring {args.first_eval}-{F.LAST}")

    when = kickoffs() if args.since else None
    games = F.load_games()
    proe_tw = signals_tw = None
    seasons = None
    mults = [float(x) for x in args.multiples.split(",")]

    # The team's prior form, so the belief model can supply s for the two
    # scenarios that have no market line to read it off.
    import fit_belief
    prior_by_team = {(o["season"], o["week"], o["team"]): o
                     for o in fit_belief.observations(F.FIRST, F.LAST)}

    rows_A, rows_C = [], []
    for oname, outcome in F.OUTCOMES.items():
        raw, seasons_read = F.load_player_weeks(outcome)
        seasons = seasons or seasons_read
        if proe_tw is None:
            proe_tw = proe.load(seasons[0], seasons[-1])
            signals_tw = signals_mod.load(seasons[0], seasons[-1])
        obs = F.build(raw, games, outcome, proe_tw, signals_tw)
        obs = [o for o in obs if o["season"] >= args.first_eval]
        if args.since:
            obs = [o for o in obs
                   if when.get((o["season"], o["week"], o["team"]), "") >= args.since]

        for sname, definition in F.SCENARIOS.items():
            for o in obs:
                occurred = definition.occurred(o)
                if occurred is None:
                    continue
                site = (o["posted_total"], o["baseline_yards"], o["trend"])
                c = find(index, oname, sname, occurred, *site)
                if c is None or not c["validated"]:
                    continue
                for m in mults:
                    hit = 1.0 if o["yards"] > m else 0.0
                    rows_A.append((oname, sname, occurred, m,
                                   prob_above(c["quantiles"], m), hit))

                # C needs BOTH halves of the site and a modelled s, so it is
                # collected separately rather than inferred from A.
                if sname not in belief:
                    continue
                other = find(index, oname, sname, not occurred, *site)
                if other is None or not other["validated"]:
                    continue
                pf = prior_by_team.get((o["season"], o["week"], o["team"]))
                if pf is None:
                    continue
                s_hat = belief_p(belief[sname], pf[belief[sname]["prior_field"]])
                if s_hat is None:
                    continue
                qc, rc = (c, other) if occurred else (other, c)
                s_base = belief[sname]["base_rate"]
                for m in (TAIL_MULTS if args.tails else mults):
                    q = prob_above(qc["quantiles"], m)
                    r = prob_above(rc["quantiles"], m)
                    hit = 1.0 if o["yards"] > m else 0.0
                    rows_C.append((oname, sname, m, q * s_hat + r * (1 - s_hat), hit,
                                   # what a book pricing off the UNCONDITIONAL
                                   # distribution would have: the same q and r,
                                   # blended at the base rate rather than at
                                   # this team's own.
                                   q * s_base + r * (1 - s_base), q, r, s_hat))

    if args.tails:
        globals()["_BASES"] = {k: v["base_rate"] for k, v in belief.items()}
        _report_tails(rows_C, args.hold)
        return 0
    _report_A(rows_A)
    _report_B(belief, args, when or kickoffs())
    _report_C(rows_C)
    return 0


def _bucket(rows, key):
    out = {}
    for r in rows:
        out.setdefault(key(r), []).append(r)
    return out


def _report_A(rows) -> None:
    print(f"\nA. q and r, conditioned on what actually happened ({len(rows)} reads)")
    print("   s is removed here: each read is scored against games where the scenario")
    print("   did or did not occur, as observed.\n")
    print(f"   {'outcome':<17}{'scenario':<18}{'side':>6}{'n':>7}{'predicted':>11}"
          f"{'actual':>9}{'error':>9}")
    THIN = 200
    worst = worst_thin = 0.0
    thin_rows = []
    for (oname, sname, occ), rs in sorted(_bucket(rows, lambda r: (r[0], r[1], r[2])).items()):
        pred = st.mean(r[4] for r in rs)
        act = st.mean(r[5] for r in rs)
        thin = len(rs) < THIN
        if thin:
            worst_thin = max(worst_thin, abs(pred - act))
            thin_rows.append(oname)
        else:
            worst = max(worst, abs(pred - act))
        print(f"   {oname:<17}{sname:<18}{('q' if occ else 'r'):>6}{len(rs):>7}"
              f"{pred:>11.3f}{act:>9.3f}{(pred - act) * 100:>+8.2f}pp"
              f"{'   thin' if thin else ''}")
    print(f"\n   worst |error| on n >= {THIN}: {worst * 100:.2f}pp"
          f"   (vig cushion at -110 is 2.38pp)")
    if thin_rows:
        # Named from the data rather than asserted: on the full season the thin
        # rows really are all passing yards, and on a one-week slate they are
        # not, so a hardcoded sentence would have been false half the time.
        where = ", ".join(f"{n} ({thin_rows.count(n)})" for n in sorted(set(thin_rows)))
        print(f"   worst |error| on rows under n={THIN}: {worst_thin * 100:.2f}pp")
        print(f"   thin rows by outcome: {where}")


def _report_B(belief, args, when) -> None:
    print(f"\nB. the belief model: does the scenario happen as often as it says?\n")
    obs = [o for o in __import__("fit_belief").observations(F.FIRST, F.LAST)
           if o["season"] >= args.first_eval]
    if args.since:
        # --since is a slate filter and has to apply here too. It did not, so
        # this table reported the whole season under a slate heading.
        obs = [o for o in obs
               if when.get((o["season"], o["week"], o["team"]), "") >= args.since]
        print(f"   restricted to games on/after {args.since}: {len(obs)} team-weeks\n")
    print(f"   {'scenario':<20}{'band':>6}{'n':>7}{'predicted':>11}{'actual':>9}{'error':>9}")
    for sname, model in belief.items():
        for i, b in enumerate(model["bands"]):
            lo = b["min"] if b["min"] is not None else float("-inf")
            hi = b["max"] if b["max"] is not None else float("inf")
            sel = [o for o in obs if lo <= o[model["prior_field"]] < hi]
            if not sel:
                continue
            act = sum(1 for o in sel if o[model["field"]] > model["threshold"]) / len(sel)
            print(f"   {sname:<20}{i + 1:>6}{len(sel):>7}{b['p']:>11.3f}"
                  f"{act:>9.3f}{(b['p'] - act) * 100:>+8.2f}pp")


def fair_price(p: float) -> str:
    """The American price at which a wager with probability p breaks even."""
    if p <= 0 or p >= 1:
        return "n/a"
    a = -100 * p / (1 - p) if p >= 0.5 else 100 * (1 - p) / p
    return f"{a:+.0f}"


def _report_C(rows) -> None:
    print(f"\nC. the product, P(hit) = q*s + r*(1-s), with s from the belief model\n")
    print("   There are no historical prop prices here, so each line sits at a multiple")
    print("   of the player's own baseline and is priced at -110. That is the same")
    print("   assumption a book makes when it sets one, stated rather than hidden.\n")
    print(f"   {'scenario':<20}{'line':>7}{'n':>7}{'predicted':>11}{'actual':>9}"
          f"{'error':>9}{'rel err':>9}{'price needed':>14}")
    for (sname, m), rs in sorted(_bucket(rows, lambda r: (r[1], r[2])).items()):
        pred = st.mean(r[3] for r in rs)
        act = st.mean(r[4] for r in rs)
        # RELATIVE error, because that is the one that matters at plus money.
        # +1.6 points on a 0.108 probability is a 15% overstatement, and a book
        # holding 6% eats a 15% overstatement without noticing.
        rel = (pred - act) / act * 100 if act > 0 else float("nan")
        print(f"   {sname:<20}{m:>7.2f}{len(rs):>7}{pred:>11.3f}{act:>9.3f}"
              f"{(pred - act) * 100:>+8.2f}pp{rel:>+8.1f}%{fair_price(act):>14}")
    print("\n   `price needed` is where these wagers break even. `rel err` is the same")
    print("   error as a share of the probability itself: absolute points flatter a")
    print("   deep line, where a 1.6-point miss on a 10.8% estimate is a 15%")
    print("   overstatement and a 6% hold swallows it whole.")
    if rows:
        pred = st.mean(r[3] for r in rows)
        act = st.mean(r[4] for r in rows)
        print(f"\n   overall: predicted {pred:.3f}, actual {act:.3f}, "
              f"error {(pred - act) * 100:+.2f}pp on {len(rows)} wagers")


def _report_tails(rows, hold) -> None:
    """How much better must your belief be, and does screening on it pay?

    The arithmetic of the thesis. A book prices the alt line off the
    unconditional distribution; you price it off a conditional one. Both use
    the same q and r, so the entire difference is which s they are blended at:

        P_you - P_book = (q - r) * (s_you - s_book)

    Beating a proportional hold needs P_you > P_book * (1 + hold), so

        s_you - s_book  >  P_book * hold / (q - r)

    That threshold is PER SITE AND PER DEPTH, and it varies enormously -- from
    +0.03 where the scenario grips hard to beyond +0.5 where it does not.
    Averaging q and r across sites destroys the pairing and hides this: it was
    the difference between "the thesis fails" and "the thesis works on a tenth
    of the grid".

    Hold is proportional. A book taking a flat 2.5 points on a 12% line would
    be charging a 20% tax, and none of them do.
    """
    print(f"\nCHASING A TAIL   (proportional hold {hold * 100:.0f}%)\n")
    print("   Same q and same r on both sides. The only difference is s, so what")
    print("   follows is attributable to the belief and nothing else.\n")

    # The requirement, per depth, over the sites that actually carry a wager.
    print(f"   {'line':>6}{'n':>8}{'best need':>11}{'p25':>8}{'median':>9}"
          f"{'share under +0.10':>20}")
    for m in sorted({r[2] for r in rows}):
        rs = [r for r in rows if r[2] == m and abs(r[6] - r[7]) > 1e-9 and r[5] > 0.01]
        if not rs:
            continue
        needs = sorted(r[5] * hold / (r[6] - r[7]) for r in rs if r[6] > r[7])
        if not needs:
            continue
        cheap = sum(1 for n in needs if n < 0.10) / len(needs) * 100
        print(f"   {m:>6.2f}{len(rs):>8}{needs[0]:>+11.3f}{needs[len(needs) // 4]:>+8.3f}"
              f"{needs[len(needs) // 2]:>+9.3f}{cheap:>19.1f}%")

    print("\n   `need` is how far your P(scenario) must exceed the market's before the")
    print("   wager is +EV. It does not depend on where the belief comes from.\n")

    # Screened: bet only where the site's own requirement is actually met.
    print(f"   {'line':>6}{'unscreened':>12}{'ROI':>9}   {'screened':>10}{'ROI':>9}"
          f"{'offered':>9}{'actual':>8}")
    for m in sorted({r[2] for r in rows}):
        rs = [r for r in rows if r[2] == m]
        loose = [r for r in rs if r[3] > r[5] * (1 + hold)]
        tight = [r for r in rs
                 if r[6] > r[7] and r[5] > 0.01
                 and (r[8] - _base_of(r)) > r[5] * hold / (r[6] - r[7])]

        def roi(sel):
            if not sel:
                return None
            off = st.mean(r[5] * (1 + hold) for r in sel)
            act = st.mean(r[4] for r in sel)
            return (act / off - 1) * 100 if off > 0 else None, off, act

        lo, tg = roi(loose), roi(tight)
        lo_s = f"{lo[0]:+.1f}%" if lo else "--"
        if tg:
            print(f"   {m:>6.2f}{len(loose):>12}{lo_s:>9}   {len(tight):>10}"
                  f"{tg[0]:>+8.1f}%{tg[1]:>9.3f}{tg[2]:>8.3f}")
        else:
            print(f"   {m:>6.2f}{len(loose):>12}{lo_s:>9}   {0:>10}{'--':>9}{'--':>9}{'--':>8}")
    print("\n   `unscreened` bets wherever our blend beats the offer at all. `screened`")
    print("   bets only where the SITE's own requirement is met -- which is the thesis")
    print("   as actually stated, rather than a blanket bet on every alt line.")


_BASES: dict = {}


def _base_of(row) -> float:
    return _BASES.get(row[1], 0.0)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

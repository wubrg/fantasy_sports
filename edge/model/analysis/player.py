"""What to type into `edgectl scenario` for one player, from the cache.

`-baseline` and `-trend` were being typed from memory, and a wrong value does
not fail -- it lands in a neighbouring cell and prices a different population
with no sign that anything happened. This reads them from the same data the
grid was fitted on, reports which cell they land in and whether that cell is
priceable, and prints the command.

It works for an UPCOMING week, which is the only time it is useful. That rules
out reusing fit_conditionals.build(), which needs the row for the week being
predicted; the prior-only arithmetic is mirrored here instead, and a test
pins the two together.

Usage:
  python3 player.py --player "Ja'Marr Chase" --season 2025 --week 10
  python3 player.py --player Chase --outcome receptions --season 2025 --week 10
"""
import argparse
import csv
import json
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F

ARTIFACT = F.ARTIFACT


def _rows(outcome, season):
    path = F.CACHE / f"stats_player_week_{season}.csv"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run `make data`")
    out = []
    for r in csv.DictReader(path.open()):
        if r.get("season_type") != "REG" or r.get("position") not in outcome.positions:
            continue
        out.append({
            "week": int(F.num(r["week"])),
            "player": r.get("player_id", ""),
            "name": r.get("player_display_name") or r.get("player_name") or "",
            "team": r.get("team") or r.get("recent_team", ""),
            "opportunity": F.num(r.get(outcome.opp_field)),
            "yards": F.num(r.get(outcome.yards_field)),
        })
    return out


def prior_state(rows, outcome, player_id, week):
    """Baseline and trend from games BEFORE `week`, exactly as build() does."""
    pool = defaultdict(float)
    for r in rows:
        pool[(r["week"], r["team"])] += r["opportunity"]
    for r in rows:
        d = pool[(r["week"], r["team"])]
        r["basis"] = (r["opportunity"] / d if d > 0 else 0.0) if outcome.share_based \
            else r["opportunity"]
        r["team_pool"] = d if outcome.share_based else 0.0

    prior = sorted([r for r in rows if r["player"] == player_id and r["week"] < week],
                   key=lambda x: x["week"])
    if len(prior) < F.MIN_PRIOR_GAMES:
        raise SystemExit(
            f"only {len(prior)} prior game(s) this season; the grid needs "
            f"{F.MIN_PRIOR_GAMES} before it will price anything. This is the "
            f"early-season hole, and it is a refusal rather than a guess.")
    # The same function the fit uses, not a second copy of it.
    return dict(F.prior_stats(prior), games=len(prior), team=prior[-1]["team"])


def cell_for(art, outcome, scenario, posted, baseline, trend):
    """The cell these coordinates land in, and whether it may be priced."""
    for c in art["cells"]:
        if (c["outcome"] == outcome and c["scenario"] == scenario and c["occurred"]
                and c["posted_min"] <= posted < c["posted_max"]
                and c["baseline_min"] <= baseline < c["baseline_max"]
                and c["trend_min"] <= trend < c["trend_max"]):
            return c
    return None


def main(argv) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--player", required=True, help="name, or any unique part of one")
    ap.add_argument("--season", type=int, required=True)
    ap.add_argument("--week", type=int, required=True, help="the week being PREDICTED")
    ap.add_argument("--outcome", default="receiving_yards", choices=list(F.OUTCOMES))
    ap.add_argument("--total", type=float, help="the posted game total, from the board")
    ap.add_argument("--scenario", default="shootout", choices=list(F.SCENARIOS))
    args = ap.parse_args(argv)

    outcome = F.OUTCOMES[args.outcome]
    rows = _rows(outcome, args.season)

    matches = {r["player"]: r["name"] for r in rows
               if args.player.lower() in r["name"].lower()}
    if not matches:
        raise SystemExit(f"no {args.outcome} player matching {args.player!r} in {args.season}")
    if len(matches) > 1:
        names = ", ".join(sorted(matches.values()))
        raise SystemExit(f"{args.player!r} matches {len(matches)} players: {names}")
    pid, name = next(iter(matches.items()))

    s = prior_state(rows, outcome, pid, args.week)
    unit = outcome.unit
    print(f"{name} ({s['team']})  {args.season} week {args.week}, "
          f"from {s['games']} prior games")
    print(f"  -baseline  {s['baseline_output']:.1f} {unit}")
    print(f"  -trend     {s['trend']:+.4f}"
          + ("  (share points)" if outcome.share_based else "  (volume)"))

    if args.total is None:
        print("\n  Pass --total <posted game total> to check the cell and get the command.")
        return 0

    art = json.loads(ARTIFACT.read_text())
    c = cell_for(art, args.outcome, args.scenario, args.total,
                 s["baseline_output"], s["trend"])
    if c is None:
        print(f"\n  NO CELL: these coordinates fall outside every published "
              f"{args.outcome}/{args.scenario} cell, so the grid will refuse.")
        return 1
    site = (f"posted {c['posted_min']:g}-{c['posted_max']:g}, "
            f"baseline {c['baseline_min']:g}-{c['baseline_max']:g}, "
            f"trend {c['trend_min']:+.2f}..{c['trend_max']:+.2f}")
    print(f"\n  cell       {site}")
    print(f"             n={c['n']}  median {c['median_output']:.1f} {unit}")
    if not c["validated"]:
        print(f"  REFUSED    {'; '.join(c['why'])}")
        return 1
    stab = c.get("stability")
    firm = "firm at every knob setting" if (stab or 0) >= 1.0 else \
        f"holds at {stab * 100:.0f}% of knob settings" if stab is not None else "stability unmeasured"
    print(f"  PRICEABLE  {firm}")
    print(f"\nedgectl scenario -outcome {args.outcome} -name {args.scenario} \\\n"
          f"  -total {args.total:g} -threshold {F.SCENARIOS[args.scenario].threshold:g} \\\n"
          f"  -baseline {s['baseline_output']:.1f} -trend {s['trend']:.4f} \\\n"
          f"  -line <LINE> -price <PRICE> -belief <YOUR P(scenario)> [-side under]")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

"""Team-week pass rate over expected, from play-by-play.

PROE is the mean of nflverse's `pass_oe` over a team's plays: actual pass minus
`xpass`, the modelled probability the play is a pass given down, distance, score
differential and time remaining. The modelling happens upstream; this aggregates.

Why not raw pass rate, which stats_player_week already supports: raw pass rate is
mostly game script. Trailing teams throw. Since the grid already conditions on a
game-script scenario (`shootout`, total > 50), a raw-pass-rate axis would partly
re-measure something the grid has, and the two would look independent while
being the same thing twice. `xpass` divides that out by construction, which is
the entire reason this file needs play-by-play rather than the weekly table.

Two series, both per team-week:

    offense   the team's own PROE -- how much more it threw than the situation
              called for. Coach and scheme tendency.
    defense   the PROE of the offense it FACED. A "funnel defense" in the
              framework's sense: one whose opponents throw more than the
              situation called for, usually an elite run defence.

Both are also produced in a prior-information-only form, which is the only form
usable as a predictor: a team's PROE COMING INTO a game never includes it. This
mirrors the discipline in fit_conditionals.build, where a player's baseline
target share is computed from prior games only.
"""

from __future__ import annotations

import argparse
import csv
import gzip
import math
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"

# Minimum plays with a populated xpass before a team-week PROE means anything.
# A team runs ~60 offensive plays a game; xpass is undefined on kickoffs, punts,
# extra points and kneels, so the usable count is lower.
MIN_PLAYS = 20


def num(s: str) -> float | None:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return None
    try:
        v = float(s)
    except ValueError:
        return None
    return None if math.isnan(v) else v


def seasons_available() -> list[int]:
    out = []
    for p in CACHE.glob("play_by_play_*.csv.gz"):
        try:
            out.append(int(p.stem.split("_")[-1].replace(".csv", "")))
        except ValueError:
            continue
    return sorted(out)


def team_weeks(season: int) -> dict[tuple[int, int, str], dict]:
    """(season, week, team) -> offense/defense PROE for that game."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    off = defaultdict(list)
    deff = defaultdict(list)
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        # Index the five columns wanted rather than building a 372-key dict per
        # row. Play-by-play is ~48,000 rows a season and DictReader spends most
        # of its time on columns nothing here reads.
        try:
            ix = {c: header.index(c) for c in
                  ("season_type", "pass_oe", "week", "posteam", "defteam")}
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")
        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            oe = num(row[ix["pass_oe"]])
            if oe is None:
                continue  # xpass undefined for this play: kickoff, punt, kneel
            week = num(row[ix["week"]])
            posteam, defteam = row[ix["posteam"]].strip(), row[ix["defteam"]].strip()
            if week is None or not posteam or not defteam:
                continue
            off[(season, int(week), posteam)].append(oe)
            deff[(season, int(week), defteam)].append(oe)

    out = {}
    for key in set(off) | set(deff):
        o, d = off.get(key, []), deff.get(key, [])
        if len(o) < MIN_PLAYS or len(d) < MIN_PLAYS:
            continue
        out[key] = {
            "offense": st.mean(o),
            "defense": st.mean(d),
            "off_plays": len(o),
            "def_plays": len(d),
        }
    return out


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if not (CACHE / f"play_by_play_{season}.csv.gz").exists():
            continue
        out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3) -> dict[tuple[int, int, str], dict]:
    """A team's PROE coming into each game, from earlier games only.

    Never includes the game itself. Without this the series cannot be used as a
    predictor -- it would be reading the outcome it is meant to forecast.
    """
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    out = {}
    for (season, team), games in by_team.items():
        games.sort()
        for i, (week, _) in enumerate(games):
            if i < min_prior:
                continue
            prior = [v for _, v in games[:i]]
            out[(season, week, team)] = {
                "offense_prior": st.mean(p["offense"] for p in prior),
                "defense_prior": st.mean(p["defense"] for p in prior),
                "prior_games": i,
            }
    return out


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is there signal here worth fetching seventeen seasons for?

    Three questions, none of which needs a populated grid -- which matters,
    because a three-season grid cannot be populated: 7,357 observations put only
    14 of 40 cells over the floor and drop the usage-vacuum cell to 19/58. A
    grid pilot at this size would answer nothing.

      PERSISTENCE  does a team's prior PROE predict this game's? If not, the
                   scenario is unforecastable and s cannot be derived at all.
      CONFOUND     is PROE just game script rediscovered? The grid already
                   conditions on `shootout`; a second measure of the same thing
                   would look independent while being a duplicate.
      SIGNAL       does PROE explain receiving yards beyond projected targets,
                   and by enough to matter at a prop line?
    """
    import fit_conditionals as fc
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior PROE vs realized; near zero = unforecastable)")
    for label, key, cur in (("offense", "offense_prior", "offense"),
                            ("defense", "defense_prior", "defense")):
        pairs = [(pf[k][key], tw[k][cur]) for k in pf if k in tw]
        print(f"  {label:<9} r = {corr(*zip(*pairs)):+.3f}   n={len(pairs)}")

    games = fc.load_games()
    rows = [(v["offense"], v["defense"], games[k][0], games[k][1])
            for k, v in tw.items() if k in games]
    off, dfn, tot, mar = zip(*rows)
    shoot = [1.0 if t > 50 else 0.0 for t in tot]
    print("\nCONFOUND  (vs what the grid already conditions on)")
    print(f"  offense PROE vs shootout indicator   r = {corr(off, shoot):+.3f}")
    print(f"  offense PROE vs team margin          r = {corr(off, mar):+.3f}")
    print(f"  defense PROE vs shootout indicator   r = {corr(dfn, shoot):+.3f}")
    print("  raw pass rate would be strongly negative against margin; xpass")
    print("  is meant to divide that out, and near-zero here says it did.")

    fc.FIRST, fc.LAST = first, last
    obs = fc.build(fc.load_player_weeks()[0], games)
    J = []
    for o in obs:
        k = (o["season"], o["week"], o["team"])
        if k in tw and k in pf:
            o = dict(o)
            o["proe"], o["dproe"] = tw[k]["offense"], tw[k]["defense"]
            o["proe_prior"], o["dproe_prior"] = pf[k]["offense_prior"], pf[k]["defense_prior"]
            J.append(o)

    y = [o["yards"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["proj_targets"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<32} {'beta':>9} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("realized offense PROE", "proe"),
                       ("PRIOR offense PROE (usable)", "proe_prior"),
                       ("realized defense PROE", "dproe"),
                       ("PRIOR defense PROE (usable)", "dproe_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["proj_targets"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<32} {b[2]:>9.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals = sorted(o["proe"] for o in J)
    p25, p75 = vals[len(vals) // 4], vals[3 * len(vals) // 4]
    hi = [o for o in J if o["proe"] > p75]
    lo = [o for o in J if o["proe"] < p25]
    print(f"\nSEPARATION  (top vs bottom PROE quartile -- what q and r would be)")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["yards"] > line) / len(hi)
        r = sum(1 for o in lo if o["yards"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12.")
    return 0


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--seasons", default="")
    ap.add_argument("--gate1", action="store_true",
                    help="run the go/no-go tests before committing to a full fetch")
    args = ap.parse_args(argv)

    avail = seasons_available()
    if not avail:
        raise SystemExit("no play_by_play_*.csv.gz in the cache -- run ingest/nflverse.py")
    if args.seasons:
        a, _, b = args.seasons.partition("-")
        first, last = int(a), int(b or a)
    else:
        first, last = avail[0], avail[-1]

    if args.gate1:
        return gate1(first, last)

    print(f"play-by-play seasons cached: {avail}")
    tw = load(first, last)
    print(f"team-weeks with usable PROE ({first}-{last}): {len(tw)}\n")

    vals = [v["offense"] for v in tw.values()]
    vals.sort()
    pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
    print("offense PROE distribution (this is where a threshold would come from)")
    print(f"  mean {st.mean(vals):+.4f}  sd {st.pstdev(vals):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {pct(p):+.4f}")

    dv = sorted(v["defense"] for v in tw.values())
    dpct = lambda p: dv[min(int(p * len(dv)), len(dv) - 1)]
    print("\ndefense PROE (the funnel measure)")
    print(f"  mean {st.mean(dv):+.4f}  sd {st.pstdev(dv):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {dpct(p):+.4f}")

    pf = prior_form(tw)
    print(f"\nteam-weeks with >=3 prior games (usable as a predictor): {len(pf)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

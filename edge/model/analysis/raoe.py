#!/usr/bin/env python3
"""Team-week rush yards over expected, from Next Gen Stats.

RAOE is rush_yards_over_expected -- Next Gen Stats' own model of expected
rush yards given ball carrier speed, defenders in the box and time to the
line of scrimmage, subtracted from actual -- combined across a team's
rushers into a per-attempt team-week rate. It is the rushing-side analogue
of proe.py's pass rate over expected: efficiency with workload divided out,
not a raw yardage total that just tracks who carried the ball more.

Why a per-attempt RATE and not a sum of each rusher's total: a team that
ran twice as many carries would otherwise show double the RAOE for the same
per-carry efficiency, which is a workload artifact, not a rushing-efficiency
read. Summing each rusher's own OWN numerator and denominator and dividing
once -- rather than averaging each rusher's own per-attempt rate -- is what
keeps a 20-carry starter from being weighted the same as a 1-carry wildcat
snap; averaging the rates directly would do exactly that.

Unlike proe.py's source (play-by-play, one row per PLAY, already keyed by
team), NGS rushing is one row per BALL CARRIER per week, so a team-week
figure means aggregating across however many backs touched the ball that
week, not just reading a column.

Also produced in a prior-information-only form, the only form usable as a
predictor: a team's RAOE coming into a game never includes it. Same
discipline as proe.py's prior_form and signals.py's prior_form -- this
mirrors proe.py's shape directly rather than reinventing the traversal.

Only OFFENSE is computed here -- who ran the ball, not who a team ran
against. A defense-faced variant (RAOE allowed) would need a join against
games.csv to find each team's opponent for the week, since NGS rushing
carries no opponent column of its own (unlike play-by-play, which has both
posteam and defteam on every row); left for later if it's ever wanted.

Coverage: 2016 onward, because that is genuinely where Next Gen Stats
tracking-based rushing data starts -- see ingest/nflverse.py's docstring.
Before 2016 this has nothing to compute from, the same way FORM in general
has nothing before a team's third game of a season.
"""

from __future__ import annotations

import argparse
import csv
import gzip
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"
SOURCE = CACHE / "ngs_rushing.csv.gz"

# Minimum total rush attempts in a team-week before its RAOE rate is trusted.
# Chosen from the actual data (see --report), not guessed: across 4,640 real
# REG-season team-weeks, attempts-per-team-week has p25=13, median=17, p75=22.
# 15 sits between the 25th percentile and the median -- it drops the noisiest
# quarter or so of team-weeks (typically a committee backfield split thin by
# an injury, where no single per-carry estimate is reliable) while keeping
# the bulk of real data usable. NGS's own file already appears to floor
# every team-week at >=10 attempts on its own (p1 = 10.0 in the same check),
# so this threshold is a deliberate quality bar on top of that, not a
# restatement of it.
MIN_ATTEMPTS = 15


def num(s: str | None) -> float | None:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return None
    try:
        v = float(s)
    except ValueError:
        return None
    return None if v != v else v  # NaN check without importing math for one use


def team_weeks(season: int) -> dict[tuple[int, int, str], dict]:
    """(season, week, team) -> RAOE per attempt for that team's rushing that week."""
    if not SOURCE.exists():
        raise SystemExit(f"{SOURCE} not found -- run ingest/nflverse.py")

    oe_sum = defaultdict(float)
    att_sum = defaultdict(float)
    rushers = defaultdict(int)
    with gzip.open(SOURCE, "rt", newline="") as fh:
        reader = csv.DictReader(fh)
        for row in reader:
            if row.get("season") != str(season):
                continue
            if row.get("season_type") != "REG":
                continue
            oe = num(row.get("rush_yards_over_expected"))
            att = num(row.get("rush_attempts"))
            week = num(row.get("week"))
            team = (row.get("team_abbr") or "").strip()
            # week 0 is preseason in this file; week must be a real REG week.
            if oe is None or att is None or week is None or int(week) < 1 or not team:
                continue
            key = (season, int(week), team)
            oe_sum[key] += oe
            att_sum[key] += att
            rushers[key] += 1

    out = {}
    for key, total_att in att_sum.items():
        if total_att < MIN_ATTEMPTS:
            continue
        out[key] = {
            "raoe_per_att": oe_sum[key] / total_att,
            "attempts": total_att,
            "rushers": rushers[key],
        }
    return out


def seasons_available() -> list[int]:
    if not SOURCE.exists():
        return []
    seasons = set()
    with gzip.open(SOURCE, "rt", newline="") as fh:
        reader = csv.DictReader(fh)
        for row in reader:
            s = num(row.get("season"))
            if s is not None:
                seasons.add(int(s))
    return sorted(seasons)


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3) -> dict[tuple[int, int, str], dict]:
    """A team's RAOE coming into each game, from earlier games only.

    Never includes the game itself -- see the module docstring and
    proe.py:124 for why this discipline is load-bearing, not incidental.
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
                "raoe_prior": st.mean(p["raoe_per_att"] for p in prior),
                "prior_games": i,
            }
    return out


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--seasons", default="")
    ap.add_argument("--report", action="store_true",
                     help="print the attempts distribution MIN_ATTEMPTS was chosen from")
    args = ap.parse_args(argv)

    avail = seasons_available()
    if not avail:
        raise SystemExit("no ngs_rushing.csv.gz in the cache -- run ingest/nflverse.py")
    if args.seasons:
        a, _, b = args.seasons.partition("-")
        first, last = int(a), int(b or a)
    else:
        first, last = avail[0], avail[-1]

    if args.report:
        # Recompute the raw attempts distribution (pre-threshold) so the
        # MIN_ATTEMPTS comment above stays checkable against the real data
        # rather than trusted on faith.
        att = defaultdict(float)
        with gzip.open(SOURCE, "rt", newline="") as fh:
            reader = csv.DictReader(fh)
            for row in reader:
                if row.get("season_type") != "REG":
                    continue
                a, wk = num(row.get("rush_attempts")), num(row.get("week"))
                if a is None or wk is None or int(wk) < 1:
                    continue
                att[(row["season"], row["week"], row["team_abbr"])] += a
        vals = sorted(att.values())
        n = len(vals)
        print(f"team-weeks (all seasons in cache, REG only): {n}")
        for p in (0.01, 0.05, 0.10, 0.25, 0.50, 0.75, 0.90):
            print(f"  p{int(p*100):<3} {vals[min(int(p * n), n - 1)]:.1f}")
        print(f"  min {vals[0]:.1f}  max {vals[-1]:.1f}")
        return 0

    print(f"ngs_rushing.csv.gz seasons available: {avail}")
    tw = load(first, last)
    print(f"team-weeks with usable RAOE ({first}-{last}, >= {MIN_ATTEMPTS} attempts): {len(tw)}\n")

    vals = sorted(v["raoe_per_att"] for v in tw.values())
    if vals:
        pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
        print("RAOE per attempt distribution")
        print(f"  mean {st.mean(vals):+.4f}  sd {st.pstdev(vals):.4f}")
        for p in (0.10, 0.25, 0.50, 0.75, 0.90):
            print(f"  p{int(p*100):<3} {pct(p):+.4f}")

    pf = prior_form(tw)
    print(f"\nteam-weeks with >=3 prior games (usable as a predictor): {len(pf)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

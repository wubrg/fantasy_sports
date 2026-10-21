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
import beliefpack as B

ARTIFACT = F.ARTIFACT

# Up to this many of a player's prior-season games are pulled in when his
# current-season sample is below MIN_PRIOR_GAMES. Weighted at half a
# current-season game, so a full prior season never outvotes the few games
# that describe this year's role.
FALLBACK_MAX_PRIOR_GAMES = 8
FALLBACK_WEIGHT = 0.5

# The opportunity column that identifies a team's lead passer -- the same field
# the passing outcomes condition on, read from the grid rather than restated.
_PASS_ATTEMPTS_FIELD = F.OUTCOMES["passing_yards"].opp_field


def _rows(outcome, season):
    path = F.CACHE / f"stats_player_week_{season}.csv"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run `make data`")
    out = []
    for r in csv.DictReader(path.open()):
        if r.get("season_type") != "REG" or r.get("position") not in outcome.positions:
            continue
        out.append({
            # Tagged so rows from two seasons can share one list without their
            # week numbers colliding -- both seasons have a week 5, and a team
            # pool keyed on week alone would merge them (see _apply_basis).
            "season": season,
            "week": int(F.num(r["week"])),
            "player": r.get("player_id", ""),
            "name": r.get("player_display_name") or r.get("player_name") or "",
            "team": r.get("team") or r.get("recent_team", ""),
            "opportunity": F.col_sum(r, outcome.opp_field),
            "yards": F.col_sum(r, outcome.yards_field),
        })
    return out


def _apply_basis(rows, outcome):
    """Fill each row's `basis`/`team_pool`, exactly as build() does.

    The pool is keyed (season, week, team), NOT (week, team): when a fallback
    mixes two seasons' rows into one list, a week-only key would pool a 2025
    week 5 with a 2026 week 5 and corrupt every share. Rows from a single
    season are unaffected -- the season component is constant -- so the normal
    path produces the same bases it always did.
    """
    pool = defaultdict(float)
    for r in rows:
        pool[(r["season"], r["week"], r["team"])] += r["opportunity"]
    for r in rows:
        d = pool[(r["season"], r["week"], r["team"])]
        r["basis"] = (r["opportunity"] / d if d > 0 else 0.0) if outcome.share_based \
            else r["opportunity"]
        r["team_pool"] = d if outcome.share_based else 0.0


def _current_coach(season, team):
    """This team's head coach this season, same precedence pack() uses.

    staff_table is the source of truth; the games.csv column is the fallback
    when no staff file exists. Returns None if neither names a coach.
    """
    st = B.staff_table(season).get(team)
    if st and st.get("head_coach"):
        return st["head_coach"]
    path = F.CACHE / "games.csv"
    coach, best_week = None, -1
    for r in csv.DictReader(path.open()):
        if r.get("game_type") != "REG" or int(F.num(r["season"])) != season:
            continue
        for side in ("home", "away"):
            if r.get(f"{side}_team") == team:
                wk = int(F.num(r["week"]))
                name = r[f"{side}_coach"].strip()
                if name and wk > best_week:
                    coach, best_week = name, wk
    return coach


def _lead_passers(season):
    """team -> player_id of the QB with the most pass attempts that season."""
    totals = defaultdict(lambda: defaultdict(float))
    path = F.CACHE / f"stats_player_week_{season}.csv"
    if not path.exists():
        return {}
    for r in csv.DictReader(path.open()):
        if r.get("season_type") != "REG" or r.get("position") != "QB":
            continue
        team = r.get("team") or r.get("recent_team", "")
        pid = r.get("player_id", "")
        totals[team][pid] += F.num(r.get(_PASS_ATTEMPTS_FIELD))
    return {team: max(by_pid, key=by_pid.get) for team, by_pid in totals.items() if by_pid}


def _fallback_eligible(outcome, season, team, player_id, prior_rows):
    """Whether prior-season games may be blended in, and why not if not.

    Continuity, so a blended baseline describes the same job the player holds
    now rather than a different one under different coaching:

      - The head coach must be unchanged from last season to this one. Always
        required.
      - A QB evaluated on his own stats carries his own history tautologically,
        so nothing more is asked. A non-QB skill player additionally needs the
        team's lead passer to be the same person both seasons AND at least one
        of his own rows on that team last season -- a share of a target pool is
        only comparable if the man throwing into it, and the player competing
        for it, are the same.

    Returns (ok, reason). reason is "" when ok.
    """
    cur = _current_coach(season, team)
    prior_coaches = B.prior_season_coaches(season).get(team)
    if not prior_coaches or cur is None or cur not in prior_coaches:
        return False, (f"head coach changed from {season - 1} to {season} "
                       f"(now {cur or 'unknown'})")

    if outcome.positions == {"QB"}:
        return True, ""

    lead_now = _lead_passers(season).get(team)
    lead_prev = _lead_passers(season - 1).get(team)
    if lead_now is None or lead_prev is None or lead_now != lead_prev:
        return False, f"team's lead passer changed from {season - 1} to {season}"

    if not any(r["player"] == player_id and r["team"] == team for r in prior_rows):
        return False, f"player has no {season - 1} games for {team}"

    return True, ""


def _weighted_stats(seq):
    """prior_stats, but each game carries a weight.

    `seq` is a chronologically ordered list of (row, weight). The baseline is a
    weight-weighted mean of basis and output; the trend is the weighted mean of
    basis over the last TREND_WINDOW games minus the overall weighted baseline,
    mirroring F.prior_stats with statistics.mean replaced by a weighted mean.
    """
    def wmean(rows_w, key):
        tw = sum(w for _, w in rows_w)
        return sum(r[key] * w for r, w in rows_w) / tw if tw else 0.0

    baseline = wmean(seq, "basis")
    return {
        "baseline_share": baseline,
        "baseline_output": wmean(seq, "yards"),
        "trend": wmean(seq[-F.TREND_WINDOW:], "basis") - baseline,
    }


def prior_state(rows, outcome, player_id, week, season):
    """Baseline and trend from games BEFORE `week`.

    The normal path mirrors build() exactly. When the current season holds
    fewer than MIN_PRIOR_GAMES prior games, up to FALLBACK_MAX_PRIOR_GAMES of
    the player's prior-season games are blended in at FALLBACK_WEIGHT -- but
    only when coaching/role continuity holds, and only when the combined sample
    then clears the floor. Otherwise it refuses, as it always did.
    """
    _apply_basis(rows, outcome)
    prior = sorted([r for r in rows if r["player"] == player_id and r["week"] < week],
                   key=lambda x: x["week"])

    if len(prior) >= F.MIN_PRIOR_GAMES:
        # The same function the fit uses, not a second copy of it.
        return dict(F.prior_stats(prior), games=len(prior), team=prior[-1]["team"],
                    phrase=f"from {len(prior)} prior games")

    # Below the floor on the current season alone. Try the prior-season blend.
    # The team is this player's current-season team: his latest prior game, or
    # his latest current-season game of any week if every one falls after it.
    own = [r for r in rows if r["player"] == player_id]
    team = (prior[-1]["team"] if prior
            else max(own, key=lambda x: x["week"])["team"] if own else None)
    prev_rows = _rows(outcome, season - 1)
    _apply_basis(prev_rows, outcome)
    ok, why = _fallback_eligible(outcome, season, team, player_id, prev_rows) \
        if team else (False, "no current-season games to anchor a team")

    fb = sorted([r for r in prev_rows if r["player"] == player_id and r["team"] == team],
                key=lambda x: x["week"], reverse=True)[:FALLBACK_MAX_PRIOR_GAMES]
    fb.sort(key=lambda x: x["week"])

    if not ok:
        extra = (f" A {season - 1} fallback was ineligible: {why}."
                 if fb else f" No {season - 1} fallback was possible ({why}).")
        raise SystemExit(
            f"only {len(prior)} prior game(s) this season; the grid needs "
            f"{F.MIN_PRIOR_GAMES} before it will price anything.{extra}")

    if len(prior) + len(fb) < F.MIN_PRIOR_GAMES:
        raise SystemExit(
            f"only {len(prior)} prior game(s) this season and {len(fb)} eligible "
            f"from {season - 1}; the grid needs {F.MIN_PRIOR_GAMES} combined "
            f"before it will price anything. The fallback was tried and is still "
            f"short.")

    seq = [(r, FALLBACK_WEIGHT) for r in fb] + [(r, 1.0) for r in prior]
    return dict(_weighted_stats(seq), games=len(prior) + len(fb), team=team,
                phrase=(f"from {len(prior)} prior games ({season}) + {len(fb)} "
                        f"from {season - 1}, weighted {FALLBACK_WEIGHT:g}x"))


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

    s = prior_state(rows, outcome, pid, args.week, args.season)
    unit = outcome.unit
    print(f"{name} ({s['team']})  {args.season} week {args.week}, {s['phrase']}")
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

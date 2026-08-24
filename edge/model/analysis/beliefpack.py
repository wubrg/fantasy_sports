"""Packs for the belief probe: what was known before, and what happened after.

Two subcommands, one file, because both need the same
(season, week, team) <-> game_id join and that join must not drift between
them. A prediction that cannot be matched to its outcome is a prediction that
never gets scored.

  pack      what a forecaster is shown before kickoff
  outcomes  whether each scenario actually occurred

The scenario predicates are NOT restated here. This imports
fit_conditionals.SCENARIOS and calls the same objects the grid was fitted on,
so "success rate above 0.46" has exactly one definition in the repository. A
second copy would eventually disagree with the first by one row, and nothing
would say which was right.
"""
import argparse
import csv
import hashlib
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F
import proe
import signals as signals_mod

# Scenarios on the game TOTAL describe the game; everything else describes one
# team's own afternoon. Derived from the basis rather than listed, so a new
# scenario cannot arrive with the wrong unit.
GAME_LEVEL_BASES = {"total"}


def unit_of(definition) -> str:
    return "game" if definition.basis in GAME_LEVEL_BASES else "team"


def game_id(season: int, week: int, away: str, home: str) -> str:
    """The same shape the line board uses, e.g. 2026_01_ARI_LAC."""
    return f"{season}_{week:02d}_{away}_{home}"


def schedule(season: int, week: int) -> list[dict]:
    """Every REG game scheduled that week, played or not.

    The join is against the SCHEDULE, not against the cache. Iterating whatever
    the cache happens to hold cannot tell "not played yet" apart from "missing",
    and a prediction whose outcome is merely absent would sit open forever with
    nothing able to say why.
    """
    path = F.CACHE / "games.csv"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run `make data`")
    out = []
    for r in csv.DictReader(path.open()):
        if r["game_type"] != "REG":
            continue
        if int(F.num(r["season"])) != season or int(F.num(r["week"])) != week:
            continue
        out.append(r)
    if not out:
        raise SystemExit(
            f"no REG games scheduled for {season} week {week}; the schedule in "
            f"games.csv may predate that season")
    return out


def definitions_block() -> dict:
    """The predicates, so a reader can be refused when they change.

    edgectl cross-checks this against its embedded copy and refuses a pack whose
    thresholds disagree. A threshold change then becomes an error rather than a
    silently different measurement.
    """
    return {
        name: {
            "basis": d.basis,
            "op": d.op,
            "threshold": d.threshold,
            "unit": unit_of(d),
        }
        for name, d in F.SCENARIOS.items()
    }


def cache_provenance(names: list[str]) -> dict:
    """The sha256 of every cache file the pack was built from.

    nflverse restates play-by-play. Pinning the inputs means a re-run producing
    a different answer shows up as a diff rather than being absorbed.
    """
    if not F.MANIFEST.exists():
        return {}
    m = json.loads(F.MANIFEST.read_text())
    return {n: m[n] for n in names if n in m}


def outcomes(season: int, week: int) -> dict:
    """Did each scenario occur? One row per scheduled (game, team, scenario)."""
    games = schedule(season, week)
    pbp = F.CACHE / f"play_by_play_{season}.csv.gz"
    have_pbp = pbp.exists()
    proe_tw = proe.team_weeks(season) if have_pbp else {}
    sig_tw = signals_mod.team_weeks(season) if have_pbp else {}

    rows = []
    for g in games:
        away, home = g["away_team"], g["home_team"]
        gid = game_id(season, week, away, home)
        played = bool(g["result"].strip() and g["total"].strip())
        total = F.num(g["total"]) if played else None
        result = F.num(g["result"]) if played else None  # home - away

        for name, d in F.SCENARIOS.items():
            if unit_of(d) == "game":
                rows.append(_row(gid, None, name, d, {"game_total": total},
                                 value=total,
                                 pending=None if played else "the game has no final score yet"))
                continue

            for team in (away, home):
                # Margin is signed to the team whose row this is.
                margin = None
                if played:
                    margin = result if team == home else -result

                if d.basis == "margin":
                    rows.append(_row(gid, team, name, d, {"margin": margin}, value=margin,
                                     pending=None if played else "the game has no final score yet"))
                    continue

                key = (season, week, team)
                if not have_pbp:
                    rows.append(_row(gid, team, name, d, {}, value=None,
                                     pending=f"play_by_play_{season} is not in the cache yet"))
                    continue

                if d.basis == "offense_proe":
                    rec, field, src = proe_tw.get(key), "offense", "proe"
                else:
                    rec, field, src = sig_tw.get(key), "success_rate", "success_rate"

                if rec is None:
                    # The pbp exists and this team-week is not in it: the game
                    # had too few plays carrying the quantity. Unmeasurable is
                    # not the same as did-not-happen, and scoring it as a miss
                    # would bias the base rate downward on a decidedly
                    # non-random sample -- weather games and short blowouts.
                    rows.append(_row(gid, team, name, d, {}, value=None,
                                     unavailable=(
                                         f"fewer than {proe.MIN_PLAYS} plays carrying the "
                                         f"quantity; below the floor both signals.py and "
                                         f"proe.py apply")))
                    continue
                rows.append(_row(gid, team, name, d, {src: rec[field]}, value=rec[field]))

    return {
        "season": season,
        "week": week,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "generated_by": "edge/model/analysis/beliefpack.py outcomes",
        "cache": cache_provenance(["games.csv", f"play_by_play_{season}.csv.gz"]),
        "definitions": definitions_block(),
        "rows": rows,
    }


def _row(gid, team, name, definition, obs, value=None, pending=None, unavailable=None) -> dict:
    r = {"game_id": gid, "team": team, "scenario": name}
    if pending:
        return r | {"status": "pending", "reason": pending}
    if unavailable:
        return r | {"status": "unavailable", "reason": unavailable}
    # The predicate itself, not a copy of it.
    occurred = definition.occurred(obs)
    if occurred is None:
        return r | {"status": "unavailable",
                    "reason": f"{definition.basis} is not available for this row"}
    return r | {"status": "settled", "occurred": occurred,
                "value": round(value, 4) if value is not None else None}


def main(argv) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)

    o = sub.add_parser("outcomes", help="did each scenario occur?")
    o.add_argument("--season", type=int, required=True)
    o.add_argument("--week", type=int, required=True)
    o.add_argument("--out", type=Path, help="write here instead of stdout")

    args = ap.parse_args(argv)

    if args.cmd == "outcomes":
        pack = outcomes(args.season, args.week)
        blob = json.dumps(pack, indent=1) + "\n"
        if args.out:
            args.out.parent.mkdir(parents=True, exist_ok=True)
            args.out.write_text(blob)
            print(f"wrote {args.out}  sha256 {hashlib.sha256(blob.encode()).hexdigest()[:16]}...")
        else:
            sys.stdout.write(blob)

        counts: dict = {}
        for r in pack["rows"]:
            counts[r["status"]] = counts.get(r["status"], 0) + 1
        print(f"\n{len(pack['rows'])} rows: "
              + ", ".join(f"{v} {k}" for k, v in sorted(counts.items())), file=sys.stderr)
        # The void rate is per scenario because the games it happens to are not
        # a random sample of games.
        by = {}
        for r in pack["rows"]:
            if r["status"] == "unavailable":
                by[r["scenario"]] = by.get(r["scenario"], 0) + 1
        if by:
            print("unavailable by scenario: "
                  + ", ".join(f"{k} {v}" for k, v in sorted(by.items())), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

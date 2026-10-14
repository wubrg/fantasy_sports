#!/usr/bin/env python3
"""Realized values for every projection logged for one season/week.

Reads beliefs/projections.jsonl for every (player, outcome) pair projected
for the given season/week, looks up what that player actually did in
stats_player_week_<season>.csv -- the same file player.py's own baseline/
trend lookup reads -- and emits one row per unique (player, outcome) pair.
A projection logged under several scenarios in the same week (the same
player's receiving yards projected under both `shootout` and
`efficient_offense`, say) settles against the one real number either way, so
this computes each pair once rather than once per logged row.

Reuses fit_conditionals.OUTCOMES for the column mapping (yards_field, a
single column or several to sum) so this can never drift from what the grid
itself was fit on, and reuses player.py's own substring-match-with-ambiguity-
refusal rule for matching a logged player name against the CSV's own names,
so a player this script can't uniquely identify is reported as unavailable
rather than guessed at.

Usage:
  python3 model/analysis/projections_outcomes.py --season 2026 --week 6 \
      --projections ../beliefs/projections.jsonl \
      > ../beliefs/2026/week06.projections-outcomes.json
"""
import argparse
import csv
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import fit_conditionals as F


def _load_projections(path, season, week):
    """(player, outcome) pairs needing settlement, from the raw JSONL log.

    Only KindProjection ("projection") entries are read; settle/other kinds
    are skipped. This does not check what is already settled -- the Go
    settle command is what skips already-settled rows, the same division of
    responsibility beliefpack.py's outcomes() has with `edgectl beliefs
    settle` (the Python side always recomputes everything scheduled; the Go
    side is what is idempotent).
    """
    pairs = set()
    p = Path(path)
    if not p.exists():
        return pairs
    for line in p.read_text().splitlines():
        line = line.strip()
        if not line:
            continue
        entry = json.loads(line)
        if entry.get("kind") != "projection":
            continue
        proj = entry.get("projection") or {}
        if proj.get("season") != season or proj.get("week") != week:
            continue
        pairs.add((proj["player"], proj["outcome"]))
    return pairs


def _realized_value(player, outcome_name, season, week):
    """The player's realized stat for this outcome/week, or (None, reason)."""
    outcome = F.OUTCOMES.get(outcome_name)
    if outcome is None:
        return None, f"unknown outcome {outcome_name!r}"

    path = F.CACHE / f"stats_player_week_{season}.csv"
    if not path.exists():
        return None, f"{path} not found -- run `make data`"

    matches = {}
    rows_for_week = []
    for r in csv.DictReader(path.open()):
        if r.get("season_type") != "REG":
            continue
        if r.get("position") not in outcome.positions:
            continue
        name = r.get("player_display_name") or r.get("player_name") or ""
        if player.lower() in name.lower():
            matches[r.get("player_id", "")] = name
        if int(F.num(r.get("week", 0))) == week:
            rows_for_week.append(r)

    if not matches:
        return None, f"no {outcome_name} player matching {player!r} in {season}"
    if len(matches) > 1:
        names = ", ".join(sorted(set(matches.values())))
        return None, f"{player!r} matches {len(matches)} players: {names}"
    pid = next(iter(matches))

    for r in rows_for_week:
        if r.get("player_id", "") == pid and r.get("week") and int(F.num(r["week"])) == week:
            return F.col_sum(r, outcome.yards_field), None

    return None, f"no {outcome_name} row for {player} in week {week} (inactive, bye, or name mismatch)"


def outcomes(projections_path, season, week):
    rows = []
    for player, outcome_name in sorted(_load_projections(projections_path, season, week)):
        value, reason = _realized_value(player, outcome_name, season, week)
        if value is None:
            rows.append({"player": player, "outcome": outcome_name, "status": "unavailable",
                         "actual": None, "reason": reason})
        else:
            rows.append({"player": player, "outcome": outcome_name, "status": "settled",
                         "actual": value, "reason": ""})
    return {"season": season, "week": week, "rows": rows}


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--season", type=int, required=True)
    ap.add_argument("--week", type=int, required=True, help="the week just played")
    ap.add_argument("--projections", required=True, help="path to beliefs/projections.jsonl")
    args = ap.parse_args(argv)

    out = outcomes(args.projections, args.season, args.week)
    print(json.dumps(out, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

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
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F
import proe
import signals as signals_mod

# Scenarios on the game TOTAL describe the game; everything else describes one
# team's own afternoon. Derived from the basis rather than listed, so a new
# scenario cannot arrive with the wrong unit.
GAME_LEVEL_BASES = {"total"}

# nflverse writes gameday and gametime as US Eastern wall-clock with no zone on
# either. Concatenating them into a naive string -- which is what the line board
# does for display -- and comparing it against a real instant is wrong by hours
# or a day depending on the reader's own zone, AND IT FAILS PERMISSIVELY: the
# error direction is accepting a forecast made after kickoff, which is the one
# thing the gate exists to prevent. January games are -05:00 and September games
# -04:00, so a fixed offset is not good enough either.
EASTERN = ZoneInfo("America/New_York")

BELIEF_ARTIFACT = F.ARTIFACT.parent / "belief.json"


def kickoff_instant(row: dict) -> datetime:
    """The resolved moment a game starts, as an aware datetime.

    Fails hard on a missing time rather than defaulting to midnight. A game
    silently starting at 00:00 would make every forecast for it look late.
    """
    day, tm = row["gameday"].strip(), row["gametime"].strip()
    if not day or not tm:
        raise SystemExit(
            f"{row.get('game_id', '?')}: games.csv has gameday={day!r} gametime={tm!r}. "
            f"A kickoff cannot be resolved, and guessing one would decide whether a "
            f"forecast counts as made before the game.")
    return datetime.fromisoformat(f"{day}T{tm}").replace(tzinfo=EASTERN)


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


def base_rates() -> dict:
    """How often each scenario happens to anyone, the number a forecast must beat.

    The two PROE/success-rate scenarios are read from the fitted belief artifact
    rather than recomputed -- recomputing would mean a second pass over every
    season's play-by-play to reproduce a number that is already committed. The
    two market-derived ones come straight from games.csv, which is cheap.
    """
    out = {}
    if BELIEF_ARTIFACT.exists():
        b = json.loads(BELIEF_ARTIFACT.read_text())
        for name, m in b.get("scenarios", {}).items():
            out[name] = m["base_rate"]

    tot = {"shootout": [0, 0], "blowout_loss": [0, 0]}
    for r in csv.DictReader((F.CACHE / "games.csv").open()):
        if r["game_type"] != "REG" or not (r["result"].strip() and r["total"].strip()):
            continue
        season = int(F.num(r["season"]))
        if not (F.FIRST <= season <= F.LAST):
            continue
        total, result = F.num(r["total"]), F.num(r["result"])
        tot["shootout"][1] += 1
        if F.SCENARIOS["shootout"].occurred({"game_total": total}):
            tot["shootout"][0] += 1
        # Both sides of every game, because blowout_loss is a team property.
        for margin in (result, -result):
            tot["blowout_loss"][1] += 1
            if F.SCENARIOS["blowout_loss"].occurred({"margin": margin}):
                tot["blowout_loss"][0] += 1
    for k, (hit, n) in tot.items():
        if n:
            out[k] = round(hit / n, 4)
    return out


def prior_form(season: int, week: int) -> tuple[dict, str]:
    """Each team's form coming into this week, or why there is none.

    Returns ({} , reason) rather than zeros. A team with no measured form and a
    team measured at zero are different states, and emitting the second for the
    first would be handing a forecaster a fact that is not true.
    """
    if week <= F.MIN_PRIOR_GAMES - 1:
        return {}, (f"week {week}: prior form needs {proe.MIN_PRIOR if hasattr(proe, 'MIN_PRIOR') else 3} "
                    f"earlier games, so none exists before week 4")
    pbp = F.CACHE / f"play_by_play_{season}.csv.gz"
    if not pbp.exists():
        return {}, f"play_by_play_{season} is not in the cache yet"

    pf_p = proe.prior_form(proe.team_weeks(season))
    pf_s = signals_mod.prior_form(signals_mod.team_weeks(season))
    out = {}
    for (s, w, team), v in pf_s.items():
        if s != season or w != week:
            continue
        p = pf_p.get((s, w, team))
        if p is None:
            continue
        out[team] = {
            "success_rate_prior": round(v["success_rate_prior"], 4),
            "offense_prior": round(p["offense_prior"], 4),
            "prior_games": v["prior_games"],
        }
    return out, "" if out else "no team has enough prior games yet"


def pack(season: int, week: int) -> dict:
    """What a forecaster is shown before kickoff. Facts only.

    Three things are deliberately absent.

    The incumbent model's own s. Showing a forecaster the answer it is being
    measured against tests anchoring rather than judgement, and destroys the
    head-to-head the exercise exists for. It sees the same FACTS the model sees,
    never the model's inference.

    Any derived probability. The posted total and spread are here because they
    are public and hiding them would make the exercise artificial -- "can you
    beat a number you can see" is the real question -- but converting a line
    into P(scenario) is the tool's job, done once, in Go, by the same code the
    CLI uses.

    Any player, line or price. This asks for a game script and nothing else.
    """
    games = schedule(season, week)
    form, form_reason = prior_form(season, week)

    out_games = []
    for g in games:
        away, home = g["away_team"], g["home_team"]
        gid = game_id(season, week, away, home)
        entry = {
            "game_id": gid,
            "away": away,
            "home": home,
            "kickoff": kickoff_instant(g).isoformat(),
            # spread_line is the HOME team's expected margin: positive means the
            # home side is favoured. Passed through raw, with the convention
            # named, so the one place that converts it can be checked.
            "total_line": F.num(g["total_line"]) if g["total_line"].strip() else None,
            "spread_line": F.num(g["spread_line"]) if g["spread_line"].strip() else None,
            "teams": {},
        }
        for team in (away, home):
            t = {}
            if team in form:
                t["prior_form"] = form[team]
            entry["teams"][team] = t
        out_games.append(entry)

    return {
        "season": season,
        "week": week,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "generated_by": "edge/model/analysis/beliefpack.py pack",
        "cache": cache_provenance(["games.csv", f"play_by_play_{season}.csv.gz"]),
        "definitions": definitions_block(),
        "base_rates": base_rates(),
        "spread_convention": "spread_line is the home team's expected margin; "
                             "positive means the home side is favoured",
        "form_available": bool(form),
        "form_absent_because": form_reason,
        "games": out_games,
    }


SPEC = Path(__file__).parent.parent.parent / "docs" / "frameworks" / "belief-probe.md"
OPERATIVE_BEGIN = "<!-- BEGIN OPERATIVE PROMPT"
OPERATIVE_END = "<!-- END OPERATIVE PROMPT -->"


def operative_prompt() -> str:
    """The instruction block, lifted verbatim from the operative spec.

    The pasteable prompt and belief-probe.md must not drift -- so there is one
    copy, in the document, and this reads it. Missing markers are a hard error
    rather than a silent fallback: emitting a pack with no instructions is
    exactly the bug this exists to prevent, and a rename should shout.
    """
    text = SPEC.read_text()
    try:
        after = text.split(OPERATIVE_BEGIN, 1)[1]
        body = after.split("-->", 1)[1]
        block = body.split(OPERATIVE_END, 1)[0]
    except IndexError:
        raise SystemExit(
            f"belief-probe.md is missing the operative markers "
            f"({OPERATIVE_BEGIN} ... {OPERATIVE_END}); the pasteable prompt "
            f"cannot be built without them. Did the spec get renamed or edited?"
        )
    block = block.strip()
    if "OUTPUT CONTRACT" not in block or "predictions" not in block:
        raise SystemExit(
            "the operative block was found but does not contain the output "
            "contract; refusing to emit a prompt a forecaster cannot answer."
        )
    return block


def render(p: dict, sha: str) -> str:
    """The pack as text to paste into a model: instructions, then this week's facts.

    The instructions are the operative block from belief-probe.md; the facts are
    the tables below, bound to the pack's own sha. A forecaster needs both, and
    for a long time this emitted only the second half.
    """
    L = [f"# BELIEF PACK — {p['season']} week {p['week']}", ""]
    L.append(operative_prompt())
    L.append("")
    L.append("---")
    L.append("")
    L.append("## THE PACK — the facts your forecast is bound to")
    L.append("")
    L.append(f"pack_sha256: {sha}")
    L.append("Echo this sha back in your output. It binds your forecast to exactly "
             "these facts.")
    L.append("")
    L.append("## BASE — how often each scenario happens to anyone")
    L.append("")
    L.append("| scenario | definition | base rate |")
    L.append("|---|---|---|")
    for name, d in p["definitions"].items():
        br = p["base_rates"].get(name)
        L.append(f"| {name} | {d['basis']} {d['op']} {d['threshold']:g} (per {d['unit']}) | "
                 f"{br if br is not None else '—'} |")
    L.append("")
    L.append("## SLATE and MARKET")
    L.append("")
    L.append(f"{p['spread_convention']}.")
    L.append("")
    L.append("| game | away | home | kickoff | total | spread |")
    L.append("|---|---|---|---|---|---|")
    for g in p["games"]:
        L.append(f"| {g['game_id']} | {g['away']} | {g['home']} | {g['kickoff']} | "
                 f"{g['total_line'] if g['total_line'] is not None else '—'} | "
                 f"{g['spread_line'] if g['spread_line'] is not None else '—'} |")
    L.append("")
    L.append("## FORM — each team coming into this week, from earlier games only")
    L.append("")
    if not p["form_available"]:
        L.append(f"**No prior form this week:** {p['form_absent_because']}.")
        L.append("")
        L.append("This is not a gap you should fill by guessing. It is a real absence, and "
                 "the weeks it happens in are the ones where nobody — you, the model, or "
                 "the market — has much to go on.")
    else:
        L.append("| team | success rate | offence PROE | games |")
        L.append("|---|---|---|---|")
        for g in p["games"]:
            for team, t in g["teams"].items():
                f = t.get("prior_form")
                if f:
                    L.append(f"| {team} | {f['success_rate_prior']:.4f} | "
                             f"{f['offense_prior']:+.4f} | {f['prior_games']} |")
    L.append("")
    return "\n".join(L)


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

    k = sub.add_parser("pack", help="what a forecaster is shown before kickoff")
    k.add_argument("--season", type=int, required=True)
    k.add_argument("--week", type=int, required=True)
    k.add_argument("--out-dir", type=Path, required=True)

    args = ap.parse_args(argv)

    if args.cmd == "pack":
        p = pack(args.season, args.week)
        blob = json.dumps(p, indent=1) + "\n"
        sha = hashlib.sha256(blob.encode()).hexdigest()
        args.out_dir.mkdir(parents=True, exist_ok=True)
        stem = f"week{args.week:02d}"
        (args.out_dir / f"{stem}.input.json").write_text(blob)
        # The prompt-facing text is DERIVED from the json and bound to the
        # json's sha rather than its own -- a file cannot contain its own hash,
        # and binding to the source of truth is what matters anyway.
        (args.out_dir / f"{stem}.prompt.md").write_text(render(p, sha))
        print(f"wrote {args.out_dir / (stem + '.input.json')}")
        print(f"wrote {args.out_dir / (stem + '.prompt.md')}   <- paste this")
        print(f"pack_sha256 {sha}")
        if not p["form_available"]:
            print(f"NOTE no prior form: {p['form_absent_because']}", file=sys.stderr)
        return 0

    if args.cmd == "outcomes":
        result = outcomes(args.season, args.week)
        blob = json.dumps(result, indent=1) + "\n"
        if args.out:
            args.out.parent.mkdir(parents=True, exist_ok=True)
            args.out.write_text(blob)
            print(f"wrote {args.out}  sha256 {hashlib.sha256(blob.encode()).hexdigest()[:16]}...")
        else:
            sys.stdout.write(blob)

        counts: dict = {}
        for r in result["rows"]:
            counts[r["status"]] = counts.get(r["status"], 0) + 1
        print(f"\n{len(result['rows'])} rows: "
              + ", ".join(f"{v} {k}" for k, v in sorted(counts.items())), file=sys.stderr)
        # The void rate is per scenario because the games it happens to are not
        # a random sample of games.
        by = {}
        for r in result["rows"]:
            if r["status"] == "unavailable":
                by[r["scenario"]] = by.get(r["scenario"], 0) + 1
        if by:
            print("unavailable by scenario: "
                  + ", ".join(f"{k} {v}" for k, v in sorted(by.items())), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

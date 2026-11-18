#!/usr/bin/env python3
"""Build the week's mechanical belief forecast from its pack.

Not a model and not a judgement: every engaged row is a number a tool already
derives, engaged only where it sits clearly off the base rate (MIN_GAP); every
other row abstains at the base rate.

  efficient_offense, pass_heavy  `edgectl belief` from the team's prior form
                                 (the fitted `s` half)
  shootout                       market-implied P(total > 50) from the posted
                                 total (`edgectl scenario`)
  blowout_loss                   market-implied P(team loses by > 7) from the
                                 team's own spread (`edgectl scenario -basis
                                 margin`, empirical residuals)

blowout_loss replaced the Week 10-11 spread ladder (0.36 + 0.02 per point past
7 for the dog, 0.14 - 0.008 for the favourite) on 2026-11-18. Scored on every
settled team-game of Weeks 8-10 (86 rows), the ladder's Brier was 0.187 and the
market's 0.151 (0.162 vs 0.097 at |spread| >= 7): 7+-point dogs lost by 7+ in 9
of 11, where the ladder said 0.41 and the market 0.61. The ladder was right
about direction and wrong about level.

Excluded games (the Chiefs rule) abstain wholesale.

Usage (from edge/):
  python3 beliefs/mechanical_forecast.py --week 12 --exclude 2026_12_KC_XXX \
      --generated-at 2026-11-25T16:00:00Z
  make belief-ingest SEASON=2026 WEEK=12 FILE=beliefs/2026/week12.forecast.json
"""
import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

EDGE = Path(__file__).resolve().parent.parent
APP = EDGE / "app"
EM = "—"  # the ingest falsifier's claim separator
MIN_GAP = 0.10
SCENARIOS = ("blowout_loss", "pass_heavy", "efficient_offense")


def run(cmd):
    r = subprocess.run(cmd, cwd=APP, capture_output=True, text=True)
    return r.stdout + r.stderr


def tool_s(name, prior):
    return float(re.search(r"s = ([\d.]+)", run(["./edgectl", "belief", "-name", name, "-prior", str(prior)])).group(1))


def market_pct(out):
    m = re.search(r"market says ([\d.]+)%", out)
    if not m:
        raise SystemExit(f"edgectl gave no market probability:\n{out}")
    return float(m.group(1)) / 100


def shootout_s(total):
    return market_pct(run(["./edgectl", "scenario", "-outcome", "receiving_yards", "-name", "shootout",
                           "-total", str(total), "-threshold", "50", "-baseline", "50", "-trend", "0",
                           "-line", "39.5", "-price", "-110", "-belief", "0.5"]))


def blowout_s(own_spread, total):
    """P(this team loses by more than 7); own_spread > 0 means it is the underdog."""
    return market_pct(run(["./edgectl", "scenario", "-outcome", "rushing_yards", "-name", "blowout_loss",
                           "-basis", "margin", "-spread", str(own_spread), "-threshold", "-7",
                           "-total", str(total), "-baseline", "60", "-trend", "0",
                           "-line", "49.5", "-price", "-200", "-belief", "0.3"]))


def build(season, week, exclude, generated_at, model):
    pack_rel = f"beliefs/{season}/week{week:02d}.input.json"
    raw = (EDGE / pack_rel).read_bytes()
    pack = json.loads(raw)
    base = pack["base_rates"]
    engaged = {}
    for g in pack["games"]:
        gid, away, home = g["game_id"], g["away"], g["home"]
        if gid in exclude:
            continue
        form = {tm: g["teams"][tm]["prior_form"] for tm in (away, home)}
        for tm in (away, home):
            srate, proe = form[tm]["success_rate_prior"], form[tm].get("offense_prior")
            s = tool_s("efficient_offense", srate)
            if abs(s - base["efficient_offense"]) >= MIN_GAP:
                where = "above" if srate >= 0.46 else "below"
                engaged[(gid, tm, "efficient_offense")] = (round(s, 2), 0.46, [
                    f"form: {tm} {EM} prior success rate {srate:.4f}, {where} the .46 efficient-offense threshold"])
            s = tool_s("pass_heavy", proe)
            if abs(s - base["pass_heavy"]) >= MIN_GAP:
                engaged[(gid, tm, "pass_heavy")] = (round(s, 2), 0.46, [
                    f"form: {tm} {EM} prior offensive PROE {proe:+.4f}, set against the +3.0 pass_heavy threshold"])
        total, spread = g["total_line"], g["spread_line"]  # spread_line: home's expected margin
        s = shootout_s(total)
        if abs(s - base["shootout"]) >= MIN_GAP:
            tm = home if form[home]["success_rate_prior"] >= form[away]["success_rate_prior"] else away
            engaged[(gid, None, "shootout")] = (round(s, 2), 0.46, [
                f"market: {away}@{home} {EM} posted total {total}",
                f"form: {tm} {EM} prior success rate {form[tm]['success_rate_prior']:.4f}"])
        for tm, own in ((away, spread), (home, -spread)):
            s = blowout_s(own, total)
            if abs(s - base["blowout_loss"]) >= MIN_GAP:
                role = "the underdog" if own > 0 else "favoured"
                engaged[(gid, tm, "blowout_loss")] = (round(s, 2), 0.48, [
                    f"market: {away}@{home} {EM} posted spread {abs(spread)} with {tm} {role}",
                    f"form: {tm} {EM} prior success rate {form[tm]['success_rate_prior']:.4f}"])

    def row(gid, team, scen):
        if (gid, team, scen) in engaged:
            b, conf, claims = engaged[(gid, team, scen)]
            return {"game_id": gid, "team": team, "scenario": scen, "belief": b,
                    "confidence": conf, "abstained": False, "claims": claims}
        return {"game_id": gid, "team": team, "scenario": scen, "belief": base[scen],
                "confidence": 0.0, "abstained": True, "claims": []}

    preds = []
    for g in pack["games"]:
        preds.append(row(g["game_id"], None, "shootout"))
        for scen in SCENARIOS:
            for tm in (g["away"], g["home"]):
                preds.append(row(g["game_id"], tm, scen))
    n = sum(not p["abstained"] for p in preds)
    sys.stderr.write(f"rows={len(preds)} engaged={n} abstained={len(preds) - n}\n")
    return {"season": season, "week": week, "input_pack": pack_rel,
            "input_pack_sha256": hashlib.sha256(raw).hexdigest(),
            "model": model, "prompt": "belief-v1", "generated_at": generated_at,
            "predictions": preds, "flagged": []}


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--season", type=int, default=2026)
    ap.add_argument("--week", type=int, required=True)
    ap.add_argument("--exclude", action="append", default=[], help="game_id to abstain wholesale (repeatable)")
    ap.add_argument("--generated-at", required=True, help="ISO-8601 UTC, before the first kickoff")
    ap.add_argument("--model", default="mechanical/belief-v1")
    ap.add_argument("--out", type=Path, help="default beliefs/<season>/weekNN.forecast.json")
    a = ap.parse_args(argv)
    out = a.out or EDGE / f"beliefs/{a.season}/week{a.week:02d}.forecast.json"
    out.write_text(json.dumps(build(a.season, a.week, set(a.exclude), a.generated_at, a.model), indent=2))
    print(f"wrote {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

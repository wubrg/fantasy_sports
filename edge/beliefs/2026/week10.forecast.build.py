#!/usr/bin/env python3
"""Serialize the week-10 2026 hand-reasoned belief forecast.

Not a model. Efficient-offense and pass-heavy beliefs are the P(scenario) that
`edgectl belief` derives from each team's prior form (the fitted `s` half), and a
row is engaged only where that sits clearly off the base rate. Shootout beliefs
are the market-implied value `edgectl scenario` reads off the posted total, engaged
only at the extremes. Blowout beliefs follow the week-9 spread ladder (7.0 -> .36,
10.0 -> .42, interpolated). Every row not engaged abstains at the base rate. KC@ATL
is abstained wholesale: no read is offered on that game.

Run from anywhere; paths are absolute against the edge/ tree.
"""
import json
import re
import subprocess
import sys

EDGE = "/Users/adamwieberg/adori/fantasy_sports/edge/"
PACK = "beliefs/2026/week10.input.json"
PACK_SHA = "49c045d512bd2115fa282e4ec5d18e6aa2c82475df707aa6d513e7583f47e87d"
APP = EDGE + "app"
EM = "—"
EXCLUDED = {"2026_10_KC_ATL"}
MIN_GAP = 0.10

BASE = {"shootout": 0.3378, "blowout_loss": 0.2617, "pass_heavy": 0.2648, "efficient_offense": 0.3721}

pack = json.load(open(EDGE + PACK))
GAMES = [(g["game_id"], g["away"], g["home"], g["total_line"], g["spread_line"]) for g in pack["games"]]
FORM = {}
for g in pack["games"]:
    for tm, t in g["teams"].items():
        pf = t["prior_form"]
        FORM[tm] = (pf["success_rate_prior"], pf.get("offense_prior"))
game_of = {}
for gid, a, h, _, _ in GAMES:
    game_of[a] = gid
    game_of[h] = gid


def run(cmd):
    r = subprocess.run(cmd, cwd=APP, capture_output=True, text=True)
    return r.stdout + r.stderr


def tool_s(name, prior):
    m = re.search(r"s = ([\d.]+)", run(["./edgectl", "belief", "-name", name, "-prior", str(prior)]))
    return float(m.group(1))


def market_s(total):
    out = run(["./edgectl", "scenario", "-outcome", "receiving_yards", "-name", "shootout",
               "-total", str(total), "-threshold", "50", "-baseline", "50", "-trend", "0",
               "-line", "39.5", "-price", "-110", "-belief", "0.5"])
    return float(re.search(r"market says ([\d.]+)%", out).group(1)) / 100


ENGAGED = {}
for tm, (srate, proe) in FORM.items():
    gid = game_of[tm]
    if gid in EXCLUDED:
        continue
    s = tool_s("efficient_offense", srate)
    if abs(s - BASE["efficient_offense"]) >= MIN_GAP:
        where = "above" if srate >= 0.46 else "below"
        ENGAGED[(gid, tm, "efficient_offense")] = (
            round(s, 2), 0.46,
            [f"form: {tm} {EM} prior success rate {srate:.4f}, {where} the .46 efficient-offense threshold"])
    s = tool_s("pass_heavy", proe)
    if abs(s - BASE["pass_heavy"]) >= MIN_GAP:
        ENGAGED[(gid, tm, "pass_heavy")] = (
            round(s, 2), 0.46,
            [f"form: {tm} {EM} prior offensive PROE {proe:+.4f}, set against the +3.0 pass_heavy threshold"])

for gid, away, home, total, spread in GAMES:
    if gid in EXCLUDED:
        continue
    s = market_s(total)
    if abs(s - BASE["shootout"]) >= MIN_GAP:
        tm = home if FORM[home][0] >= FORM[away][0] else away
        ENGAGED[(gid, None, "shootout")] = (
            round(s, 2), 0.46,
            [f"market: {away}@{home} {EM} posted total {total}",
             f"form: {tm} {EM} prior success rate {FORM[tm][0]:.4f}"])
    if abs(spread) >= 7.0:
        fav = home if spread > 0 else away
        dog = away if spread > 0 else home
        dog_b = round(0.36 + (abs(spread) - 7.0) * 0.02, 2)
        fav_b = round(0.14 - (abs(spread) - 7.0) * 0.008, 2)
        for tm, b in ((dog, dog_b), (fav, fav_b)):
            role = "favoured" if tm == fav else "the underdog"
            ENGAGED[(gid, tm, "blowout_loss")] = (
                b, 0.48,
                [f"market: {away}@{home} {EM} posted spread {abs(spread)} with {tm} {role}",
                 f"form: {tm} {EM} prior success rate {FORM[tm][0]:.4f}"])


def row(game_id, team, scenario):
    key = (game_id, team, scenario)
    if key in ENGAGED:
        belief, conf, claims = ENGAGED[key]
        return {"game_id": game_id, "team": team, "scenario": scenario,
                "belief": belief, "confidence": conf, "abstained": False, "claims": claims}
    return {"game_id": game_id, "team": team, "scenario": scenario,
            "belief": BASE[scenario], "confidence": 0.0, "abstained": True, "claims": []}


def build():
    preds = []
    for gid, away, home, _, _ in GAMES:
        preds.append(row(gid, None, "shootout"))
        for scen in ("blowout_loss", "pass_heavy", "efficient_offense"):
            for team in (away, home):
                preds.append(row(gid, team, scen))
    engaged = sum(1 for p in preds if not p["abstained"])
    assert len(preds) == 7 * len(GAMES), f"got {len(preds)}"
    sys.stderr.write(f"rows={len(preds)} engaged={engaged} abstained={len(preds)-engaged}\n")
    return {"season": 2026, "week": 10, "input_pack": PACK, "input_pack_sha256": PACK_SHA,
            "model": "claude-sonnet-5/belief-v1", "prompt": "belief-v1",
            "generated_at": "2026-11-11T18:00:00Z",
            "predictions": preds, "flagged": []}


if __name__ == "__main__":
    json.dump(build(), open(EDGE + "beliefs/2026/week10.forecast.json", "w"), indent=2)
    print("wrote week10.forecast.json")

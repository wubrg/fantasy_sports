#!/usr/bin/env python3
"""Serialize the week-8 2026 hand-reasoned belief forecast.

Not a model. Every engaged row is a deliberate judgement made by reading the
pack (FORM vs each scenario threshold, cross-checked against the market line and
the STAFF table). Rows not listed abstain at the base rate, per the prompt's rule
that every row must be forecast and a no-read is an explicit abstention, not an
omission. FORM/market claim numbers are read straight from the pack so the ingest
falsifier passes them at the pack's own precision.
"""
import json

PACK = "beliefs/2026/week08.input.json"
PACK_SHA = "a146b3bac80d49cb1fd7007313d2e690afcf1d9e02c4adb3dc12b58d6ebbb1ef"
EM = "—"  # em dash — the falsifier's claim separator

BASE = {
    "shootout": 0.3378,
    "blowout_loss": 0.2617,
    "pass_heavy": 0.2648,
    "efficient_offense": 0.3721,
}

pack = json.load(open(PACK))
GAMES = [(g["game_id"], g["away"], g["home"], g["total_line"], g["spread_line"]) for g in pack["games"]]
FORM = {}   # team -> (success_rate, proe)
for g in pack["games"]:
    for tm, t in g["teams"].items():
        pf = t["prior_form"]
        FORM[tm] = (pf["success_rate_prior"], pf.get("offense_prior"))

def sr(t):   return FORM[t][0]
def proe(t): return FORM[t][1]

# --- engaged rows: (game_id, team_or_None, scenario) -> (belief, confidence, [claims]) ---
ENGAGED = {}

def eff(team, belief, conf):
    above = sr(team) >= 0.46
    where = "above" if above else "below"
    ENGAGED[(game_of[team], team, "efficient_offense")] = (
        belief, conf,
        [f"form: {team} {EM} prior success rate {sr(team):.4f}, {where} the .46 efficient-offense threshold"],
    )

def passh(team, belief, conf, note=""):
    extra = f", {note}" if note else ""
    ENGAGED[(game_of[team], team, "pass_heavy")] = (
        belief, conf,
        [f"form: {team} {EM} prior offensive PROE {proe(team):+.4f}{extra}"],
    )

def blow(game_id, away, home, team, belief, conf, spread):
    favhome = spread > 0
    fav = home if favhome else away
    role = "favoured" if team == fav else "the underdog"
    ENGAGED[(game_id, team, "blowout_loss")] = (
        belief, conf,
        [f"market: {away}@{home} {EM} posted spread {abs(spread)} with {team} {role}",
         f"form: {team} {EM} prior success rate {sr(team):.4f}"],
    )

def shoot(game_id, away, home, belief, conf, formteam):
    tot = next(g[3] for g in GAMES if g[0] == game_id)
    ENGAGED[(game_id, None, "shootout")] = (
        belief, conf,
        [f"market: {away}@{home} {EM} posted total {tot}",
         f"form: {formteam} {EM} prior success rate {sr(formteam):.4f}"],
    )

game_of = {}
for gid, a, h, _, _ in GAMES:
    game_of[a] = gid; game_of[h] = gid

# efficient_offense — FORM success rate clearly off the .46 threshold
eff("DET", 0.54, 0.56); eff("LA", 0.54, 0.55); eff("CIN", 0.53, 0.55)
eff("BUF", 0.52, 0.52); eff("JAX", 0.52, 0.50); eff("DAL", 0.51, 0.52)
eff("BAL", 0.49, 0.50); eff("KC", 0.49, 0.50)
eff("MIN", 0.25, 0.48); eff("LAC", 0.26, 0.48); eff("PHI", 0.27, 0.47)
eff("DEN", 0.29, 0.45); eff("GB", 0.31, 0.45)

# pass_heavy — FORM offence PROE clearly off the +3.0 threshold
passh("CIN", 0.55, 0.58, "the highest pass-rate-over-expected mark in this slate")
passh("GB", 0.49, 0.54, "above the +3.0 pass_heavy threshold")
passh("CAR", 0.47, 0.52, "above the +3.0 pass_heavy threshold")
passh("PIT", 0.46, 0.52, "above the +3.0 pass_heavy threshold")
passh("KC", 0.45, 0.50, "above the +3.0 pass_heavy threshold")
passh("MIA", 0.07, 0.55, "by far the most run-leaning mark in this slate")
passh("LAC", 0.09, 0.52, "far below the +3.0 pass_heavy threshold")
passh("ATL", 0.12, 0.50, "well below the +3.0 pass_heavy threshold")
passh("TEN", 0.13, 0.48, "well below the +3.0 pass_heavy threshold")
passh("SEA", 0.13, 0.48, "well below the +3.0 pass_heavy threshold")
passh("IND", 0.14, 0.48, "well below the +3.0 pass_heavy threshold")
passh("TB", 0.14, 0.48, "well below the +3.0 pass_heavy threshold")

# shootout — only the clear extremes of the posted total
shoot("2026_08_BAL_BUF", "BAL", "BUF", 0.51, 0.50, "BUF")
shoot("2026_08_ARI_DAL", "ARI", "DAL", 0.48, 0.48, "DAL")
shoot("2026_08_CLE_PIT", "CLE", "PIT", 0.13, 0.50, "PIT")
shoot("2026_08_PHI_WAS", "PHI", "WAS", 0.14, 0.48, "PHI")
shoot("2026_08_KC_DEN", "KC", "DEN", 0.16, 0.46, "DEN")

# blowout_loss — only the wide spreads (>= 6.5)
blow("2026_08_TEN_CIN", "TEN", "CIN", "TEN", 0.42, 0.50, 10.0)
blow("2026_08_TEN_CIN", "TEN", "CIN", "CIN", 0.12, 0.50, 10.0)
blow("2026_08_IND_JAX", "IND", "JAX", "IND", 0.41, 0.50, 9.5)
blow("2026_08_IND_JAX", "IND", "JAX", "JAX", 0.12, 0.50, 9.5)
blow("2026_08_NE_MIA", "NE", "MIA", "MIA", 0.36, 0.48, -7.0)
blow("2026_08_NE_MIA", "NE", "MIA", "NE", 0.14, 0.48, -7.0)
blow("2026_08_ARI_DAL", "ARI", "DAL", "ARI", 0.34, 0.45, 6.5)
blow("2026_08_ARI_DAL", "ARI", "DAL", "DAL", 0.15, 0.45, 6.5)
blow("2026_08_LAC_LA", "LAC", "LA", "LAC", 0.34, 0.45, 6.5)
blow("2026_08_LAC_LA", "LAC", "LA", "LA", 0.15, 0.45, 6.5)

FLAGGED = [
    "2026_08_TEN_CIN/CIN/pass_heavy",
    "2026_08_TEN_CIN/CIN/efficient_offense",
    "2026_08_MIN_DET/DET/efficient_offense",
    "2026_08_LAC_LA/LA/efficient_offense",
    "2026_08_BAL_BUF/BUF/efficient_offense",
    "2026_08_ARI_DAL/DAL/efficient_offense",
    "2026_08_CAR_GB/GB/pass_heavy",
    "2026_08_NE_MIA/MIA/pass_heavy",
    "2026_08_LAC_LA/LAC/pass_heavy",
    "2026_08_MIN_DET/MIN/efficient_offense",
]


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
        for team in (away, home):
            preds.append(row(gid, team, "blowout_loss"))
        for team in (away, home):
            preds.append(row(gid, team, "pass_heavy"))
        for team in (away, home):
            preds.append(row(gid, team, "efficient_offense"))
    engaged = sum(1 for p in preds if not p["abstained"])
    assert len(preds) == 98, f"expected 98 rows, got {len(preds)}"
    for f in FLAGGED:
        gid, team, scen = f.split("/")
        t = None if team == "None" else team
        assert (gid, t, scen) in ENGAGED, f"flagged row not engaged: {f}"
    import sys
    sys.stderr.write(f"rows={len(preds)} engaged={engaged} abstained={len(preds)-engaged}\n")
    return {
        "season": 2026, "week": 8,
        "input_pack": PACK, "input_pack_sha256": PACK_SHA,
        "model": "claude-sonnet-5/belief-v1", "prompt": "belief-v1",
        "generated_at": "2026-10-28T20:00:00Z",
        "predictions": preds, "flagged": FLAGGED,
    }


if __name__ == "__main__":
    print(json.dumps(build(), indent=2))

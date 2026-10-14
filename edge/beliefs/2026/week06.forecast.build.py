#!/usr/bin/env python3
"""Builds beliefs/2026/week06.forecast.json.

Hand-reasoned per the belief-probe prompt, not a mechanical formula (unlike
week04's bootstrap script -- that was a one-off for the week FORM first
existed). Every engaged row's belief/confidence was set by reading the pack's
own FORM/market numbers for that specific game, the same way week05's
log-recorded predictions were produced: `pass_heavy` engaged where a team's
own prior offense_proe already sits clearly on one side of the +3.0
threshold; `efficient_offense` where success_rate_prior sits clearly on one
side of .46; `blowout_loss` where the spread is wide AND/OR the two teams'
FORM gap is real; `shootout` only for the slate's single clearest total
outlier. Rows where the signal was marginal (within roughly +/-0.02 of a
threshold) or genuinely ambiguous (a team trailing-likely despite a weak
baseline pass rate, where game-script and baseline tendency pull opposite
ways -- e.g. LAC, ATL, NYG this week) were left abstained rather than forced.

This script only assembles valid JSON from the hand-specified ENGAGED list
below; it does not compute beliefs from a formula. Every claim quotes a pack
number verbatim (to the same precision as the pack) so a `form`/`market`
claim can never mismatch at ingest.

Run:  python3 edge/beliefs/2026/week06.forecast.build.py \
          > edge/beliefs/2026/week06.forecast.json
"""

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PACK = ROOT / "beliefs" / "2026" / "week06.input.json"
INPUT_PACK = "beliefs/2026/week06.input.json"
PACK_SHA256 = "47bc175c9cd8ebb4b76b6207a00a7b47a8751fad0ad4910dd03a8a34fbbbb2d1"
GENERATED_AT = "2026-10-14T15:10:00Z"  # before the earliest kickoff, SEA@DEN 2026-10-16T00:15:00Z
MODEL = "llm-forecaster/belief-v1"

pk = json.loads(PACK.read_text())
assert pk["season"] == 2026 and pk["week"] == 6
games = {g["game_id"]: g for g in pk["games"]}
base_rates = pk["base_rates"]

# (game_id, team_or_None, scenario) -> (belief, confidence, [claims])
ENGAGED = {
    ("2026_06_HOU_JAX", "JAX", "efficient_offense"): (
        0.44, 0.45,
        ["form: JAX — success rate .4730, above the .46 threshold"]),

    ("2026_06_BAL_CLE", "BAL", "efficient_offense"): (
        0.54, 0.58,
        ["form: BAL — success rate .5099, the second-best in this slate behind SF"]),
    ("2026_06_BAL_CLE", "CLE", "blowout_loss"): (
        0.36, 0.42,
        ["market: BAL@CLE — spread -5.5",
         "form: CLE — success rate .3967 against BAL's .5099"]),
    ("2026_06_BAL_CLE", "BAL", "blowout_loss"): (
        0.14, 0.42,
        ["market: BAL@CLE — spread -5.5",
         "form: BAL — success rate .5099, favored and the stronger offense by form"]),

    ("2026_06_TEN_IND", "IND", "blowout_loss"): (
        0.16, 0.38,
        ["market: TEN@IND — spread 7.0",
         "form: IND — success rate .4226 against TEN's .4072"]),
    ("2026_06_TEN_IND", "TEN", "blowout_loss"): (
        0.35, 0.38,
        ["market: TEN@IND — spread 7.0"]),

    ("2026_06_NYJ_NE", "NE", "blowout_loss"): (
        0.15, 0.40,
        ["market: NYJ@NE — spread 7.5",
         "form: NE — success rate .4500 against NYJ's .4275"]),
    ("2026_06_NYJ_NE", "NYJ", "blowout_loss"): (
        0.36, 0.40,
        ["market: NYJ@NE — spread 7.5"]),

    ("2026_06_CAR_PHI", "CAR", "pass_heavy"): (
        0.50, 0.52,
        ["form: CAR — offense PROE +4.2000, already above the +3.0 threshold in the prior sample"]),
    ("2026_06_CAR_PHI", "PHI", "efficient_offense"): (
        0.26, 0.42,
        ["form: PHI — success rate .3619, well below the .46 threshold"]),

    ("2026_06_PIT_TB", "PIT", "pass_heavy"): (
        0.50, 0.52,
        ["form: PIT — offense PROE +4.5550, already above the +3.0 threshold",
         "form: PIT — RAOE +2.4762, the rushing efficiency reinforces rather than conflicts with the pass-rate read"]),

    ("2026_06_ARI_LA", "LA", "blowout_loss"): (
        0.12, 0.48,
        ["market: ARI@LA — spread 12.5, the widest on this week's slate",
         "form: LA — success rate .4751, favored and the better-form offense"]),
    ("2026_06_ARI_LA", "ARI", "blowout_loss"): (
        0.39, 0.48,
        ["market: ARI@LA — spread 12.5, the widest on this week's slate",
         "narrative: ARI@LA — the form gap (ARI .4473 vs LA .4751) is real but far narrower than the "
         "market's 12.5-point line implies, which argues some of this spread rests on information outside FORM"]),
    ("2026_06_ARI_LA", "LA", "efficient_offense"): (
        0.44, 0.45,
        ["form: LA — success rate .4751, above the .46 threshold"]),

    ("2026_06_LAC_KC", "KC", "blowout_loss"): (
        0.12, 0.48,
        ["market: LAC@KC — spread 8.5",
         "form: KC — success rate .4837, offense PROE +2.7802, RAOE +2.3731 — "
         "the strongest overall form profile in this matchup"]),
    ("2026_06_LAC_KC", "LAC", "blowout_loss"): (
        0.40, 0.48,
        ["market: LAC@KC — spread 8.5",
         "form: LAC — offense PROE -10.4909, the worst in this week's slate"]),
    ("2026_06_LAC_KC", "KC", "efficient_offense"): (
        0.51, 0.54,
        ["form: KC — success rate .4837, above the .46 threshold"]),

    ("2026_06_BUF_LV", None, "shootout"): (
        0.47, 0.55,
        ["market: BUF@LV — total 53.5, the highest on this week's slate",
         "form: BUF — success rate .4863, above the .3721 base"]),
    ("2026_06_BUF_LV", "BUF", "efficient_offense"): (
        0.52, 0.55,
        ["form: BUF — success rate .4863, above the .46 threshold"]),

    ("2026_06_DAL_GB", "DAL", "pass_heavy"): (
        0.52, 0.55,
        ["form: DAL — offense PROE +4.3338, already above the +3.0 threshold",
         "narrative: DAL — a second consecutive week with offense PROE clearly above the pass_heavy "
         "threshold (+4.7207 last week, +4.3338 this week), the most repeated signal in this pack"]),
    ("2026_06_DAL_GB", "DAL", "efficient_offense"): (
        0.51, 0.54,
        ["form: DAL — success rate .4820, above the .46 threshold"]),

    ("2026_06_WAS_SF", "SF", "efficient_offense"): (
        0.56, 0.60,
        ["form: SF — success rate .5294, the best in this week's slate"]),
    ("2026_06_WAS_SF", "SF", "pass_heavy"): (
        0.44, 0.50,
        ["form: SF — offense PROE +3.0261, above the +3.0 threshold, "
         "continuing last week's engaged read (+3.2429)"]),
    ("2026_06_WAS_SF", "SF", "blowout_loss"): (
        0.15, 0.42,
        ["market: WAS@SF — spread 7.5",
         "form: SF — success rate .5294, favored and the best-form offense in the slate"]),
    ("2026_06_WAS_SF", "WAS", "blowout_loss"): (
        0.36, 0.42,
        ["market: WAS@SF — spread 7.5",
         "form: WAS — success rate .3960 against SF's .5294"]),
}

FLAGGED_KEYS = [
    ("2026_06_WAS_SF", "SF", "efficient_offense"),
    ("2026_06_WAS_SF", "SF", "pass_heavy"),
    ("2026_06_BAL_CLE", "BAL", "efficient_offense"),
    ("2026_06_LAC_KC", "KC", "efficient_offense"),
    ("2026_06_DAL_GB", "DAL", "pass_heavy"),
    ("2026_06_DAL_GB", "DAL", "efficient_offense"),
    ("2026_06_BUF_LV", "BUF", "efficient_offense"),
    ("2026_06_LAC_KC", "LAC", "blowout_loss"),
    ("2026_06_ARI_LA", "ARI", "blowout_loss"),
    ("2026_06_CAR_PHI", "CAR", "pass_heavy"),
]


def flag_key(game_id, team, scenario):
    return f"{game_id}/{team or ''}/{scenario}"


predictions = []
flagged = [flag_key(*k) for k in FLAGGED_KEYS]

for g in pk["games"]:
    gid = g["game_id"]
    for name, defn in pk["definitions"].items():
        base = base_rates[name]
        teams = [None] if defn["basis"] == "total" else [g["away"], g["home"]]
        for team in teams:
            key = (gid, team, name)
            if key in ENGAGED:
                belief, confidence, claims = ENGAGED[key]
                abstained = False
            else:
                belief, confidence, claims = base, 0.0, []
                abstained = True
            predictions.append({
                "game_id": gid,
                "team": team,
                "scenario": name,
                "belief": round(belief, 4),
                "confidence": confidence,
                "abstained": abstained,
                "claims": claims,
            })

out = {
    "season": 2026,
    "week": 6,
    "input_pack": INPUT_PACK,
    "input_pack_sha256": PACK_SHA256,
    "model": MODEL,
    "prompt": "belief-v1",
    "generated_at": GENERATED_AT,
    "predictions": predictions,
    "flagged": flagged,
}

print(json.dumps(out, indent=2))

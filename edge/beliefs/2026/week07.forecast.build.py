#!/usr/bin/env python3
"""Serialize the week-7 2026 hand-reasoned belief forecast.

This is NOT a model. Every engaged row below is a deliberate judgement made by
reading the pack (FORM vs each scenario threshold, cross-checked against the
market line and the STAFF table). Rows not listed here abstain at the base rate,
per the prompt's rule that every row must be forecast and a no-read is an
explicit abstention, not an omission. The script only turns those decisions
into the strict JSON the ingest contract wants, with form/market claim numbers
copied at the pack's exact precision so the ingest falsifier passes them.
"""
import json

PACK_SHA = "aa739c298e3439d39f55a01d7c99d9eeba4aa44c51d95483313ae70d6b82b5f3"

BASE = {
    "shootout": 0.3378,
    "blowout_loss": 0.2617,
    "pass_heavy": 0.2648,
    "efficient_offense": 0.3721,
}

# away, home for each game, in slate order.
GAMES = [
    ("2026_07_NE_CHI", "NE", "CHI"),
    ("2026_07_PIT_NO", "PIT", "NO"),
    ("2026_07_SF_ATL", "SF", "ATL"),
    ("2026_07_CIN_BAL", "CIN", "BAL"),
    ("2026_07_TB_CAR", "TB", "CAR"),
    ("2026_07_NYG_HOU", "NYG", "HOU"),
    ("2026_07_IND_MIN", "IND", "MIN"),
    ("2026_07_MIA_NYJ", "MIA", "NYJ"),
    ("2026_07_CLE_TEN", "CLE", "TEN"),
    ("2026_07_DEN_ARI", "DEN", "ARI"),
    ("2026_07_GB_DET", "GB", "DET"),
    ("2026_07_LA_LV", "LA", "LV"),
    ("2026_07_KC_SEA", "KC", "SEA"),
    ("2026_07_DAL_PHI", "DAL", "PHI"),
]

# Engaged rows: (game_id, team_or_None, scenario) -> (belief, confidence, [claims]).
# Everything else abstains at the base rate.
EM = "—"  # em dash — the falsifier's claim separator
ENGAGED = {
    # --- PIT@NO: Pittsburgh's play-caller offence is clearly pass-first ---
    ("2026_07_PIT_NO", "PIT", "pass_heavy"): (
        0.52, 0.56,
        [f"form: PIT {EM} prior offensive PROE +4.9876, the highest in this week's slate",
         f"narrative: PIT {EM} a new head-coaching staff whose play-caller has called a markedly pass-first offence through the prior sample"],
    ),
    # --- SF@ATL: SF the slate's best-form offence, favoured ---
    ("2026_07_SF_ATL", "SF", "blowout_loss"): (
        0.17, 0.50,
        [f"market: SF@ATL {EM} posted spread 3.0 with SF favoured",
         f"form: SF {EM} prior success rate .5300, the best in this week's slate"],
    ),
    ("2026_07_SF_ATL", "SF", "efficient_offense"): (
        0.57, 0.60,
        [f"form: SF {EM} prior success rate .5300, well above the .46 efficient-offense threshold"],
    ),
    # --- CIN@BAL: two of the strongest offences by form, divisional, total 50.5 ---
    ("2026_07_CIN_BAL", None, "shootout"): (
        0.44, 0.45,
        [f"market: CIN@BAL {EM} posted total 50.5",
         f"form: CIN {EM} prior offensive PROE +5.4367, the highest in this week's slate",
         f"form: BAL {EM} prior success rate .4804"],
    ),
    ("2026_07_CIN_BAL", "CIN", "pass_heavy"): (
        0.53, 0.58,
        [f"form: CIN {EM} prior offensive PROE +5.4367, the highest pass-rate-over-expected mark in this slate"],
    ),
    ("2026_07_CIN_BAL", "CIN", "efficient_offense"): (
        0.49, 0.47,
        [f"form: CIN {EM} prior success rate .4708, above the .46 efficient-offense threshold"],
    ),
    ("2026_07_CIN_BAL", "BAL", "efficient_offense"): (
        0.52, 0.52,
        [f"form: BAL {EM} prior success rate .4804, above the .46 efficient-offense threshold"],
    ),
    # --- TB@CAR: Carolina's play-caller offence clearly pass-first ---
    ("2026_07_TB_CAR", "CAR", "pass_heavy"): (
        0.50, 0.54,
        [f"form: CAR {EM} prior offensive PROE +4.8388, well above the +3.0 pass_heavy threshold"],
    ),
    # --- NYG@HOU: widest spread on the slate, and form agrees with it ---
    ("2026_07_NYG_HOU", "NYG", "blowout_loss"): (
        0.42, 0.50,
        [f"market: NYG@HOU {EM} posted spread 10.0, the widest on this week's slate",
         f"form: NYG {EM} prior success rate .4155 against HOU's .4367"],
    ),
    ("2026_07_NYG_HOU", "HOU", "blowout_loss"): (
        0.12, 0.50,
        [f"market: NYG@HOU {EM} posted spread 10.0 with HOU favoured",
         f"form: HOU {EM} prior success rate .4367"],
    ),
    # --- IND@MIN: Minnesota the slate's weakest success-rate offence ---
    ("2026_07_IND_MIN", "MIN", "efficient_offense"): (
        0.25, 0.45,
        [f"form: MIN {EM} prior success rate .3540, the lowest in this week's slate and well below the .46 threshold"],
    ),
    # --- MIA@NYJ: lowest-scoring profile; Miami extreme run-lean ---
    ("2026_07_MIA_NYJ", None, "shootout"): (
        0.14, 0.48,
        [f"market: MIA@NYJ {EM} posted total 39.5, among the lowest on this week's slate",
         f"form: MIA {EM} prior success rate .3739"],
    ),
    ("2026_07_MIA_NYJ", "MIA", "pass_heavy"): (
        0.07, 0.55,
        [f"form: MIA {EM} prior offensive PROE -11.5849, by far the most run-leaning mark in this slate"],
    ),
    ("2026_07_MIA_NYJ", "MIA", "efficient_offense"): (
        0.28, 0.45,
        [f"form: MIA {EM} prior success rate .3739, well below the .46 efficient-offense threshold"],
    ),
    # --- CLE@TEN: the lowest total on the slate, two bottom-tier offences ---
    ("2026_07_CLE_TEN", None, "shootout"): (
        0.13, 0.50,
        [f"market: CLE@TEN {EM} posted total 37.5, the lowest on this week's slate",
         f"form: TEN {EM} prior offensive PROE -4.9111"],
    ),
    # --- DEN@ARI: Denver well below the efficiency threshold ---
    ("2026_07_DEN_ARI", "DEN", "efficient_offense"): (
        0.29, 0.42,
        [f"form: DEN {EM} prior success rate .3789, below the .46 efficient-offense threshold"],
    ),
    # --- GB@DET: highest total on the slate, dome, divisional, DET strong form ---
    ("2026_07_GB_DET", None, "shootout"): (
        0.52, 0.50,
        [f"market: GB@DET {EM} posted total 53.5, the highest on this week's slate",
         f"form: DET {EM} prior success rate .4931"],
    ),
    ("2026_07_GB_DET", "DET", "efficient_offense"): (
        0.54, 0.56,
        [f"form: DET {EM} prior success rate .4931, well above the .46 efficient-offense threshold"],
    ),
    # --- LA@LV: Rams above the efficiency threshold ---
    ("2026_07_LA_LV", "LA", "efficient_offense"): (
        0.50, 0.48,
        [f"form: LA {EM} prior success rate .4749, above the .46 efficient-offense threshold"],
    ),
    # --- KC@SEA: Kansas City pass-first and efficient by form ---
    ("2026_07_KC_SEA", "KC", "pass_heavy"): (
        0.45, 0.50,
        [f"form: KC {EM} prior offensive PROE +3.8549, above the +3.0 pass_heavy threshold"],
    ),
    ("2026_07_KC_SEA", "KC", "efficient_offense"): (
        0.54, 0.55,
        [f"form: KC {EM} prior success rate .4924, well above the .46 efficient-offense threshold"],
    ),
    # --- DAL@PHI: favoured Dallas far better form; Philadelphia weak + home dog ---
    ("2026_07_DAL_PHI", "DAL", "blowout_loss"): (
        0.15, 0.45,
        [f"market: DAL@PHI {EM} posted spread 4.5 with DAL favoured",
         f"form: DAL {EM} prior success rate .4721 against PHI's .3644"],
    ),
    ("2026_07_DAL_PHI", "PHI", "blowout_loss"): (
        0.33, 0.40,
        [f"market: DAL@PHI {EM} posted spread 4.5 with PHI the home underdog",
         f"form: PHI {EM} prior success rate .3644, the weaker offensive form in this matchup"],
    ),
    ("2026_07_DAL_PHI", "DAL", "pass_heavy"): (
        0.50, 0.54,
        [f"form: DAL {EM} prior offensive PROE +4.4290, well above the +3.0 pass_heavy threshold"],
    ),
    ("2026_07_DAL_PHI", "DAL", "efficient_offense"): (
        0.51, 0.52,
        [f"form: DAL {EM} prior success rate .4721, above the .46 efficient-offense threshold"],
    ),
    ("2026_07_DAL_PHI", "PHI", "efficient_offense"): (
        0.27, 0.45,
        [f"form: PHI {EM} prior success rate .3644, well below the .46 efficient-offense threshold"],
    ),
}

FLAGGED = [
    "2026_07_SF_ATL/SF/efficient_offense",
    "2026_07_GB_DET/DET/efficient_offense",
    "2026_07_KC_SEA/KC/efficient_offense",
    "2026_07_CIN_BAL/BAL/efficient_offense",
    "2026_07_CIN_BAL/CIN/pass_heavy",
    "2026_07_PIT_NO/PIT/pass_heavy",
    "2026_07_DAL_PHI/DAL/pass_heavy",
    "2026_07_TB_CAR/CAR/pass_heavy",
    "2026_07_NYG_HOU/NYG/blowout_loss",
    "2026_07_DAL_PHI/DAL/efficient_offense",
]


def row(game_id, team, scenario):
    key = (game_id, team, scenario)
    if key in ENGAGED:
        belief, conf, claims = ENGAGED[key]
        return {
            "game_id": game_id,
            "team": team,
            "scenario": scenario,
            "belief": belief,
            "confidence": conf,
            "abstained": False,
            "claims": claims,
        }
    return {
        "game_id": game_id,
        "team": team,
        "scenario": scenario,
        "belief": BASE[scenario],
        "confidence": 0.0,
        "abstained": True,
        "claims": [],
    }


def build():
    preds = []
    for game_id, away, home in GAMES:
        preds.append(row(game_id, None, "shootout"))
        for team in (away, home):
            preds.append(row(game_id, team, "blowout_loss"))
        for team in (away, home):
            preds.append(row(game_id, team, "pass_heavy"))
        for team in (away, home):
            preds.append(row(game_id, team, "efficient_offense"))

    engaged = sum(1 for p in preds if not p["abstained"])
    assert len(preds) == 98, f"expected 98 rows, got {len(preds)}"
    for f in FLAGGED:
        gid, team, scen = f.split("/")
        assert (gid, team, scen) in ENGAGED, f"flagged row not engaged: {f}"

    out = {
        "season": 2026,
        "week": 7,
        "input_pack": "beliefs/2026/week07.input.json",
        "input_pack_sha256": PACK_SHA,
        "model": "claude-opus-4-8/belief-v1",
        "prompt": "belief-v1",
        "generated_at": "2026-10-21T18:00:00Z",
        "predictions": preds,
        "flagged": FLAGGED,
    }
    import sys
    sys.stderr.write(f"rows={len(preds)} engaged={engaged} abstained={len(preds)-engaged}\n")
    return out


if __name__ == "__main__":
    print(json.dumps(build(), indent=2))

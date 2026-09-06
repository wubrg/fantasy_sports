#!/usr/bin/env python3
"""Builds beliefs/2026/week01.forecast.json.

The forecast is generated, not hand-typed, so a reviewer can zero out any one
deviation and see what it was worth. The derivation is ADR-004; the claim and
abstention policy is ADR-005; the pack-sha discrepancy this file binds through
is ADR-003.

Run:  python3 edge/beliefs/2026/week01.forecast.build.py > edge/beliefs/2026/week01.forecast.json
"""

import json
import math
import sys

# ---------------------------------------------------------------- the pack ---
# Facts as shown to the forecaster. The sha is the PASTED pack's, which differs
# from the committed week01.input.json -- see ADR-003.
PACK_SHA = "42bca9d8d34240219d4c7bbe8c437555f2c45a359478bf8f48cce40a2a985347"
INPUT_PACK = "beliefs/2026/week01.input.json"
GENERATED_AT = "2026-09-06T19:15:00Z"  # before the 2026-09-09T20:20-04:00 opener
MODEL = "llm-forecaster/belief-v1"

BASE = {  # base rates as printed in the pasted pack
    "shootout": 0.3378,
    "blowout_loss": 0.2617,
    "pass_heavy": 0.2648,
    "efficient_offense": 0.3721,
}

# game_id, away, home, total, spread_line (home team's expected margin)
GAMES = [
    ("2026_01_NE_SEA",  "NE",  "SEA", 44.5,  3.5),
    ("2026_01_SF_LA",   "SF",  "LA",  48.5,  3.5),
    ("2026_01_CHI_CAR", "CHI", "CAR", 47.5, -2.5),
    ("2026_01_TB_CIN",  "TB",  "CIN", 51.5,  3.5),
    ("2026_01_NO_DET",  "NO",  "DET", 48.5,  7.0),
    ("2026_01_BUF_HOU", "BUF", "HOU", 44.5, -1.5),
    ("2026_01_BAL_IND", "BAL", "IND", 48.5, -3.5),
    ("2026_01_CLE_JAX", "CLE", "JAX", 40.5,  7.5),
    ("2026_01_ATL_PIT", "ATL", "PIT", 42.5,  3.0),
    ("2026_01_NYJ_TEN", "NYJ", "TEN", 38.5,  3.0),
    ("2026_01_ARI_LAC", "ARI", "LAC", 46.5, 10.5),
    ("2026_01_MIA_LV",  "MIA", "LV",  40.5,  3.5),
    ("2026_01_GB_MIN",  "GB",  "MIN", 45.5,  1.5),
    ("2026_01_WAS_PHI", "WAS", "PHI", 47.5,  4.5),
    ("2026_01_DAL_NYG", "DAL", "NYG", 48.5, -2.5),
    ("2026_01_DEN_KC",  "DEN", "KC",  42.5,  3.0),
]

# ------------------------------------------------------------ the machinery ---

def phi(z):
    return 0.5 * (1.0 + math.erf(z / math.sqrt(2.0)))

def logit(p):
    return math.log(p / (1.0 - p))

def expit(x):
    return 1.0 / (1.0 + math.exp(-x))

def clamp(p, lo=0.02, hi=0.95):
    return max(lo, min(hi, p))

def r2(p):
    return round(p, 3)

# ------------------------------------------------------------- the constants ---
# Every deviation this file makes is one of these numbers. ADR-004 argues each.
SIGMA_TOTAL = 14.0     # combined score, widened from ~13.5 for week-1 uncertainty
SIGMA_MARGIN = 14.5    # game margin, same reason
SHOOTOUT_TILT = -0.02  # week-1 offences open below their in-season efficiency
EFF_TILT = -0.02       # same, applied to success rate
EFF_SLOPE = 0.15       # logit points per implied point of team total
EFF_CENTRE = 22.5      # league-average implied team total

WEEK1_CLAIM = ("narrative: league — week 1 offences open below their in-season efficiency, "
               "with no live-rep continuity and vanilla openers on both sides")

# Per-game shootout deviations beyond the uniform tilt.
SHOOTOUT_ADJ = {
    # An elite defence and a low posted total point at a low-variance script, which
    # cuts the upper tail harder than a symmetric normal does.
    "2026_01_DEN_KC": (-0.02, ["narrative: DEN — defensive front and secondary that finished the "
                               "prior season as the league's most disruptive pass defence"]),
    # The slate's lowest total, two offences with limited explosive-play equity.
    "2026_01_NYJ_TEN": (-0.02, ["narrative: NYJ — run-committed offence with a low explosive-play "
                                "rate caps the combined-score upside"]),
}

# Per-team, per-game blowout deviations. Reads, not line restatements.
BLOWOUT_ADJ = {
    ("2026_01_DEN_KC", "DEN"): (-0.03, ["narrative: DEN — an elite defence compresses margin "
                                        "variance in both directions"]),
    ("2026_01_DEN_KC", "KC"): (-0.03, ["narrative: DEN — an elite defence compresses margin "
                                       "variance in both directions"]),
    ("2026_01_CLE_JAX", "CLE"): (+0.03, ["narrative: CLE — an unsettled quarterback room gives the "
                                         "offence a low floor, which is how a game gets away early"]),
    ("2026_01_NYJ_TEN", "NYJ"): (-0.02, ["narrative: NYJ — a run-committed, clock-shortening "
                                         "offence keeps games close by reducing possessions"]),
    ("2026_01_NYJ_TEN", "TEN"): (-0.02, ["narrative: NYJ — a run-committed, clock-shortening "
                                         "offence keeps games close by reducing possessions"]),
}

# pass_heavy: P(PROE > 3) from prior-season play-calling identity. None of this is
# in the pack, and none of it is visible to a logistic on the total and spread --
# which is the whole reason this scenario carries the flags. ADR-004 §3.
PASS_PRIOR = {
    "PHI": 0.12, "PIT": 0.12, "BAL": 0.13, "LAC": 0.14, "SEA": 0.15, "SF": 0.15,
    "NYJ": 0.16, "CAR": 0.17, "ATL": 0.17, "IND": 0.18, "GB": 0.20, "DET": 0.20,
    "BUF": 0.22, "CHI": 0.28, "LA": 0.28, "CLE": 0.28, "MIA": 0.30, "MIN": 0.30,
    "DEN": 0.30, "WAS": 0.30, "KC": 0.32, "NE": 0.33, "TB": 0.35, "HOU": 0.35,
    "DAL": 0.38, "NO": 0.40, "CIN": 0.45,
    # ARI, JAX, LV, NYG, TEN are absent on purpose: abstained, ADR-005 §4.
}

PASS_CLAIM = {
    "PHI": "narrative: PHI — run-committed identity built on a short-yardage package, the prior "
           "season's lowest pass rate over expectation in the league's bottom tier",
    "PIT": "narrative: PIT — the offensive staff's prior offences called run at well above "
           "expectation on early downs",
    "BAL": "narrative: BAL — heavy-personnel run identity around a mobile quarterback holds pass "
           "rate under expectation",
    "LAC": "narrative: LAC — gap-scheme run identity, among the most run-committed early-down "
           "offences of the prior season",
    "SEA": "narrative: SEA — wide-zone and play-action identity that establishes the run on first "
           "down",
    "SF": "narrative: SF — outside-zone offence that has thrown below expectation every year of "
          "this staff's tenure",
    "NYJ": "narrative: NYJ — the prior season's most run-committed offence, by design rather than "
           "by game script",
    "CAR": "narrative: CAR — run-first staff that leans on early-down carries to protect its "
           "defence",
    "ATL": "narrative: ATL — the offence is organised around a bell-cow back on early downs",
    "IND": "narrative: IND — a run-first identity around a workhorse back",
    "DET": "narrative: DET — two-back rushing identity, and the side expected to lead",
    "CIN": "narrative: CIN — pass-first identity that has sat near the top of the league in pass "
           "rate over expectation for several seasons",
    "NO": "narrative: NO — pass-first play-calling identity, and the side expected to trail",
    "DAL": "narrative: DAL — pass-first offence around a veteran pocket quarterback",
    "TB": "narrative: TB — pass-first identity with three-receiver personnel as the base",
    "HOU": "narrative: HOU — drop-back offence with a limited early-down run game",
    "NE": "narrative: NE — the offence has leaned on its young quarterback's volume rather than "
          "its run game",
    "KC": "narrative: KC — quick-game passing is the base offence, not the change-up",
}

# Game-context nudge for pass_heavy: teams expected to trail by a touchdown or more
# throw a little above expectation, favourites a little below. Small, because PROE
# is already score-and-situation adjusted.
PASS_CONTEXT = 0.03
PASS_CONTEXT_AT = 7.0

# efficient_offense: adjustment to the implied-total baseline for offences whose
# success rate runs above or below what their points suggest. ADR-004 §4.
EFF_ADJ = {
    "BAL": +0.05, "PHI": +0.04, "DET": +0.04, "SEA": +0.03, "SF": +0.03, "LA": +0.03,
    "BUF": +0.03, "CIN": +0.03, "KC": +0.02, "GB": +0.02, "TB": +0.02, "CHI": +0.02,
    "DAL": +0.02, "MIN": +0.01, "WAS": +0.01, "NE": +0.01,
    "MIA": 0.0, "ATL": 0.0, "LAC": 0.0, "IND": 0.0,
    "PIT": -0.02, "CAR": -0.02, "HOU": -0.03, "DEN": -0.04, "NO": -0.04, "NYJ": -0.04,
    # ARI, CLE, JAX, LV, NYG, TEN are absent on purpose: abstained, ADR-005 §4.
}

EFF_CLAIM = {
    "BAL": "narrative: BAL — the offence has led the league in play success rate while scoring "
           "fewer points than that implies, because it shortens games",
    "PHI": "narrative: PHI — a short-yardage conversion package lifts success rate above what the "
           "scoreboard shows",
    "DET": "narrative: DET — high-floor offence: success rate has run ahead of its points in "
           "each of the last two seasons",
    "SEA": "narrative: SEA — play-action and a schemed quick game produced a top-tier success "
           "rate in the prior season",
    "SF": "narrative: SF — the scheme is built for yards-on-schedule, which is what success rate "
          "measures",
    "LA": "narrative: LA — quick-game passing with a high completion floor",
    "BUF": "narrative: BUF — quarterback scrambling converts would-be failures on early downs",
    "CIN": "narrative: CIN — high-volume, high-completion passing keeps the offence on schedule",
    "DEN": "narrative: DEN — the prior-season offence scored more than its play-level efficiency "
           "supported, with points coming off defence and field position",
    "HOU": "narrative: HOU — interior offensive-line issues cap the early-down floor",
    "NO": "narrative: NO — limited early-down run efficiency puts the offence behind the sticks",
    "NYJ": "narrative: NYJ — an offence with little passing-game explosiveness lives on "
           "second-and-long",
    "PIT": "narrative: PIT — run-first early downs at below-average yards per carry",
    "CAR": "narrative: CAR — early-down run rate above what the personnel converts",
}

FLAGGED = [
    "2026_01_WAS_PHI/PHI/pass_heavy",
    "2026_01_ATL_PIT/PIT/pass_heavy",
    "2026_01_BAL_IND/BAL/pass_heavy",
    "2026_01_ARI_LAC/LAC/pass_heavy",
    "2026_01_NE_SEA/SEA/pass_heavy",
    "2026_01_SF_LA/SF/pass_heavy",
    "2026_01_NYJ_TEN/NYJ/pass_heavy",
    "2026_01_TB_CIN/CIN/pass_heavy",
    "2026_01_NO_DET/NO/pass_heavy",
    "2026_01_BAL_IND/BAL/efficient_offense",
    "2026_01_DEN_KC/DEN/efficient_offense",
]

# ----------------------------------------------------------------- the rows ---

def row(game_id, team, scenario, belief, confidence, claims, abstained=False):
    return {
        "game_id": game_id,
        "team": team,
        "scenario": scenario,
        # An abstention is the pack's base rate verbatim, not a rounding of it:
        # the contract calls it a neutral placeholder, and rounding it would make
        # it a (very slightly) different forecast.
        "belief": belief if abstained else r2(belief),
        "confidence": confidence,
        "abstained": abstained,
        "claims": claims,
    }

def build():
    preds = []
    for gid, away, home, total, spread in GAMES:
        # --- shootout: combined > 50, so the threshold is 50.5 on integer scores.
        p = 1.0 - phi((50.5 - total) / SIGMA_TOTAL) + SHOOTOUT_TILT
        adj, extra = SHOOTOUT_ADJ.get(gid, (0.0, []))
        preds.append(row(gid, None, "shootout", clamp(p + adj), 0.45,
                         [WEEK1_CLAIM] + extra))

        for team in (away, home):
            m = spread if team == home else -spread   # this team's expected margin

            # --- blowout_loss: margin < -7, so the threshold is -7.5.
            p = phi((-7.5 - m) / SIGMA_MARGIN)
            adj, claims = BLOWOUT_ADJ.get((gid, team), (0.0, []))
            preds.append(row(gid, team, "blowout_loss", clamp(p + adj),
                             0.5 if claims else 0.4, claims))

            # --- pass_heavy: scheme identity, not the line.
            if team not in PASS_PRIOR:
                preds.append(row(gid, team, "pass_heavy", BASE["pass_heavy"], 0.15, [],
                                 abstained=True))
            else:
                p = PASS_PRIOR[team]
                if m <= -PASS_CONTEXT_AT:
                    p += PASS_CONTEXT
                elif m >= PASS_CONTEXT_AT:
                    p -= PASS_CONTEXT
                claims = [PASS_CLAIM[team]] if team in PASS_CLAIM else []
                conf = 0.65 if abs(p - BASE["pass_heavy"]) >= 0.08 else 0.5
                preds.append(row(gid, team, "pass_heavy", clamp(p), conf, claims))

            # --- efficient_offense: implied team total, then the offence-quality read.
            if team not in EFF_ADJ:
                preds.append(row(gid, team, "efficient_offense", BASE["efficient_offense"],
                                 0.15, [], abstained=True))
            else:
                implied = (total + m) / 2.0
                p = expit(logit(BASE["efficient_offense"])
                          + EFF_SLOPE * (implied - EFF_CENTRE))
                p += EFF_ADJ[team] + EFF_TILT
                claims = [EFF_CLAIM[team]] if team in EFF_CLAIM else []
                conf = 0.6 if claims else 0.45
                preds.append(row(gid, team, "efficient_offense", clamp(p), conf, claims))

    return {
        "season": 2026,
        "week": 1,
        "input_pack": INPUT_PACK,
        "input_pack_sha256": PACK_SHA,
        "model": MODEL,
        "prompt": "belief-v1",
        "generated_at": GENERATED_AT,
        "predictions": preds,
        "flagged": FLAGGED,
    }

# ------------------------------------------------------------ self-checking ---

def selfcheck(doc):
    """Refuses to emit a file the ingest gate would reject for a structural reason."""
    errs = []
    want = set()
    for gid, away, home, _, _ in GAMES:
        want.add((gid, None, "shootout"))
        for t in (away, home):
            for s in ("blowout_loss", "pass_heavy", "efficient_offense"):
                want.add((gid, t, s))
    got = set()
    for p in doc["predictions"]:
        key = (p["game_id"], p["team"], p["scenario"])
        if key in got:
            errs.append(f"duplicate row {key}")
        got.add(key)
        if not (0.0 <= p["belief"] <= 1.0):
            errs.append(f"belief out of range {key}")
        if not (0.0 <= p["confidence"] <= 1.0):
            errs.append(f"confidence out of range {key}")
        if p["scenario"] == "shootout" and p["team"] is not None:
            errs.append(f"shootout carries a team {key}")
        if p["scenario"] != "shootout" and not p["team"]:
            errs.append(f"team scenario without a team {key}")
        for c in p["claims"]:
            kind = c.split(":", 1)[0]
            if kind in ("form", "market", "schedule"):
                errs.append(f"pack-checkable claim type {kind!r} on {key} (ADR-005 forbids it)")
            if kind not in ("form", "market", "schedule", "usage", "injury",
                            "personnel", "narrative"):
                errs.append(f"untyped claim on {key}: {c[:40]!r}")
            if "—" not in c:
                errs.append(f"claim without an em-dash separator on {key}")
    for k in sorted(want - got):
        errs.append(f"missing row {k}")
    for k in sorted(got - want):
        errs.append(f"row not in the pack {k}")
    keys = {f"{p['game_id']}/{p['team'] or ''}/{p['scenario']}" for p in doc["predictions"]}
    for f in doc["flagged"]:
        if f not in keys:
            errs.append(f"flagged row is not in the file: {f}")
    for p in doc["predictions"]:
        if p["abstained"]:
            if abs(p["belief"] - BASE[p["scenario"]]) > 1e-9:
                errs.append(f"abstained row is not at the base rate: {p['game_id']} {p['team']}")
    return errs

if __name__ == "__main__":
    doc = build()
    errs = selfcheck(doc)
    if errs:
        for e in errs:
            print("SELFCHECK:", e, file=sys.stderr)
        sys.exit(1)
    print(json.dumps(doc, indent=1, ensure_ascii=False))

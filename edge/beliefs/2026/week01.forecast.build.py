#!/usr/bin/env python3
"""Builds beliefs/2026/week01.forecast.json.

The forecast is generated, not hand-typed, so a reviewer can zero out any one
deviation and see what it was worth. Derivation: ADR-004. Claim and abstention
policy: ADR-005. Staff context and why it exists: ADR-006.

**Version 2.** The first version was written against a pack with no STAFF block,
and it staked four of its eleven flagged rows on head coaches who had left. It
also predated a six-game line move. This reads the pack from disk -- games, base
rates, coaches and its own sha -- so neither class of error can recur silently.

Run:  python3 edge/beliefs/2026/week01.forecast.build.py \
          > edge/beliefs/2026/week01.forecast.json
"""

import hashlib
import json
import math
import sys
from pathlib import Path

PACK = Path(__file__).resolve().parents[2] / "beliefs" / "2026" / "week01.input.json"
INPUT_PACK = "beliefs/2026/week01.input.json"
GENERATED_AT = "2026-09-06T20:10:00Z"  # before the 2026-09-09T20:20-04:00 opener
MODEL = "llm-forecaster/belief-v1"

# ------------------------------------------------------------ the machinery ---

def phi(z):
    return 0.5 * (1.0 + math.erf(z / math.sqrt(2.0)))

def logit(p):
    return math.log(p / (1.0 - p))

def expit(x):
    return 1.0 / (1.0 + math.exp(-x))

def clamp(p, lo=0.02, hi=0.95):
    return max(lo, min(hi, p))

# ------------------------------------------------------------- the constants ---
# Every deviation this file makes is one of these numbers. ADR-004 argues each.
SIGMA_TOTAL = 14.0     # combined score, widened from ~13.5 for week-1 uncertainty
SIGMA_MARGIN = 14.5    # game margin, same reason
SHOOTOUT_TILT = -0.02  # week-1 offences open below their in-season efficiency
EFF_TILT = -0.02       # same, applied to success rate
EFF_SLOPE = 0.15       # logit points per implied point of team total
EFF_CENTRE = 22.5      # league-average implied team total
PASS_CONTEXT = 0.03    # nudge for a side expected to trail (or lead) by a touchdown
PASS_CONTEXT_AT = 7.0

WEEK1_CLAIM = ("narrative: league — week 1 offences open below their in-season efficiency, "
               "with no live-rep continuity and vanilla openers on both sides")

SHOOTOUT_ADJ = {
    "2026_01_DEN_KC": (-0.02, ["narrative: DEN — defensive front and secondary that finished the "
                               "prior season as the league's most disruptive pass defence"]),
    "2026_01_NYJ_TEN": (-0.02, ["narrative: NYJ — run-committed offence with a low explosive-play "
                                "rate caps the combined-score upside"]),
}

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

# pass_heavy: P(PROE > 3) from play-calling identity. Absent teams are abstained.
#
# Four teams are abstained here BECAUSE of the STAFF block, not despite it: BAL,
# MIA and TEN hired defensive head coaches, and ARI's head coach is defensive, so
# knowing who the head coach is does not tell you who calls the plays. The three
# teams v1 abstained on that the STAFF block RESOLVED -- JAX, LV, NYG -- now
# carry positions, because their new head coach is himself the play-caller.
PASS_PRIOR = {
    "PHI": 0.12, "LAC": 0.14, "SF": 0.15, "LV": 0.16, "NYJ": 0.16, "CAR": 0.17,
    "ATL": 0.17, "DET": 0.20, "IND": 0.18, "NYG": 0.19, "GB": 0.20, "BUF": 0.22,
    "SEA": 0.24, "CHI": 0.28, "LA": 0.28, "MIA_UNUSED": 0.0,
    "MIN": 0.30, "DEN": 0.30, "WAS": 0.30, "JAX": 0.30, "KC": 0.32,
    "NE": 0.33, "PIT": 0.33, "CLE": 0.33, "TB": 0.35, "HOU": 0.35,
    "DAL": 0.38, "NO": 0.40, "CIN": 0.45,
}
PASS_PRIOR.pop("MIA_UNUSED")

# Claims. `coaching` is checked against the pack's STAFF block, so every name
# here has to be the one the pack gives -- which is the point: it ties a scheme
# read to a staff that can be verified, instead of to an assumption.
PASS_CLAIM = {
    "PHI": ["coaching: PHI — Nick Sirianni's staff has called run above expectation every season "
            "of his tenure, built on a short-yardage package no one else runs"],
    "LAC": ["coaching: LAC — Jim Harbaugh's offences are gap-scheme and run-committed on early "
            "downs, at Michigan and since"],
    "SF": ["coaching: SF — Kyle Shanahan's outside-zone offence has thrown below expectation in "
           "every year of his tenure"],
    "LV": ["coaching: LV — Klint Kubiak, the play-caller behind Seattle's run-leaning prior "
           "season, takes over as head coach",
           "narrative: LV — a wide-zone, play-action install throws below expectation on early "
           "downs"],
    "NYJ": ["coaching: NYJ — Aaron Glenn's first season produced the league's most run-committed "
            "offence, by design rather than by game script"],
    "NYG": ["coaching: NYG — John Harbaugh's teams have played complementary, run-first football "
            "for two decades"],
    "CAR": ["coaching: CAR — Dave Canales leans on early-down carries to protect his defence"],
    "ATL": ["narrative: ATL — the offence is organised around a bell-cow back on early downs"],
    "DET": ["coaching: DET — Dan Campbell's two-back rushing identity, and the side expected to "
            "lead"],
    "IND": ["narrative: IND — a run-first identity around a workhorse back"],
    "CIN": ["coaching: CIN — Zac Taylor's offence has sat near the top of the league in pass rate "
            "over expectation for several seasons"],
    "NO": ["coaching: NO — Kellen Moore's play-calling is pass-first, and this is the side "
           "expected to trail"],
    "DAL": ["narrative: DAL — pass-first offence around a veteran pocket quarterback"],
    "TB": ["narrative: TB — pass-first identity with three-receiver personnel as the base"],
    "HOU": ["narrative: HOU — drop-back offence with a limited early-down run game"],
    "NE": ["narrative: NE — the offence has leaned on its young quarterback's volume rather than "
           "its run game"],
    "KC": ["coaching: KC — Andy Reid's quick game is the base offence, not the change-up"],
    "PIT": ["coaching: PIT — Mike McCarthy is new to Pittsburgh, and his offences have been "
            "pass-leaning from under centre, not the run-committed ones this roster last played in"],
    "CLE": ["coaching: CLE — Todd Monken is new as head coach, and his background is as a "
            "passing-game coordinator"],
    "SEA": ["personnel: SEA — the coordinator who set the prior season's run-leaning identity is "
            "now another team's head coach, so that identity is no longer safe to assume"],
    "JAX": ["coaching: JAX — Liam Coen calls the offence himself and his install is pass-leaning"],
}

# efficient_offense: adjustment to the implied-total baseline. Absent teams are
# abstained -- all six for quarterback uncertainty the STAFF block cannot settle.
EFF_ADJ = {
    "PHI": +0.04, "DET": +0.04, "SF": +0.03, "LA": +0.03, "BUF": +0.03, "CIN": +0.03,
    "KC": +0.02, "GB": +0.02, "TB": +0.02, "CHI": +0.02, "DAL": +0.02, "BAL": +0.02,
    "MIN": +0.01, "WAS": +0.01, "NE": +0.01, "SEA": 0.0,
    "MIA": 0.0, "ATL": 0.0, "LAC": 0.0, "IND": 0.0, "PIT": 0.0,
    "CAR": -0.02, "HOU": -0.03, "DEN": -0.04, "NO": -0.04, "NYJ": -0.04,
}

EFF_CLAIM = {
    "PHI": ["narrative: PHI — a short-yardage conversion package lifts success rate above what "
            "the scoreboard shows"],
    "DET": ["narrative: DET — high-floor offence: success rate has run ahead of its points in "
            "each of the last two seasons"],
    "SF": ["narrative: SF — the scheme is built for yards-on-schedule, which is what success rate "
           "measures"],
    "LA": ["narrative: LA — quick-game passing with a high completion floor"],
    "BUF": ["narrative: BUF — quarterback scrambling converts would-be failures on early downs"],
    "CIN": ["narrative: CIN — high-volume, high-completion passing keeps the offence on schedule"],
    "BAL": ["narrative: BAL — the roster that produced the league's best play-level efficiency is "
            "largely intact, but the staff that shaped it is not, so the read is held to a nudge"],
    "DEN": ["narrative: DEN — the prior-season offence scored more than its play-level efficiency "
            "supported, with points coming off defence and field position"],
    "HOU": ["narrative: HOU — interior offensive-line issues cap the early-down floor"],
    "NO": ["narrative: NO — limited early-down run efficiency puts the offence behind the sticks"],
    "NYJ": ["narrative: NYJ — an offence with little passing-game explosiveness lives on "
            "second-and-long"],
    "CAR": ["narrative: CAR — early-down run rate above what the personnel converts"],
}

FLAGGED = [
    "2026_01_WAS_PHI/PHI/pass_heavy",
    "2026_01_ARI_LAC/LAC/pass_heavy",
    "2026_01_SF_LA/SF/pass_heavy",
    "2026_01_MIA_LV/LV/pass_heavy",
    "2026_01_NYJ_TEN/NYJ/pass_heavy",
    "2026_01_DAL_NYG/NYG/pass_heavy",
    "2026_01_CHI_CAR/CAR/pass_heavy",
    "2026_01_NO_DET/DET/pass_heavy",
    "2026_01_TB_CIN/CIN/pass_heavy",
    "2026_01_NO_DET/NO/pass_heavy",
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
        "belief": belief if abstained else round(belief, 3),
        "confidence": confidence,
        "abstained": abstained,
        "claims": claims,
    }

def build(pack, sha):
    base = pack["base_rates"]
    preds = []
    for g in pack["games"]:
        gid, away, home = g["game_id"], g["away"], g["home"]
        total, spread = g["total_line"], g["spread_line"]

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

            # --- pass_heavy: play-calling identity, not the line.
            if team not in PASS_PRIOR:
                preds.append(row(gid, team, "pass_heavy", base["pass_heavy"], 0.15, [],
                                 abstained=True))
            else:
                p = PASS_PRIOR[team]
                if m <= -PASS_CONTEXT_AT:
                    p += PASS_CONTEXT
                elif m >= PASS_CONTEXT_AT:
                    p -= PASS_CONTEXT
                claims = PASS_CLAIM.get(team, [])
                conf = 0.65 if abs(p - base["pass_heavy"]) >= 0.08 else 0.5
                # A staff in its first week is a weaker read than an established
                # one, whatever the prior says about the man.
                if g["teams"][team].get("coach_is_new"):
                    conf = min(conf, 0.45)
                preds.append(row(gid, team, "pass_heavy", clamp(p), conf, claims))

            # --- efficient_offense: implied team total, then the offence-quality read.
            if team not in EFF_ADJ:
                preds.append(row(gid, team, "efficient_offense", base["efficient_offense"],
                                 0.15, [], abstained=True))
            else:
                implied = (total + m) / 2.0
                p = expit(logit(base["efficient_offense"]) + EFF_SLOPE * (implied - EFF_CENTRE))
                p += EFF_ADJ[team] + EFF_TILT
                claims = EFF_CLAIM.get(team, [])
                conf = 0.6 if claims else 0.45
                if g["teams"][team].get("coach_is_new"):
                    conf = min(conf, 0.45)
                preds.append(row(gid, team, "efficient_offense", clamp(p), conf, claims))

    return {
        "season": pack["season"],
        "week": pack["week"],
        "input_pack": INPUT_PACK,
        "input_pack_sha256": sha,
        "model": MODEL,
        "prompt": "belief-v1",
        "generated_at": GENERATED_AT,
        "predictions": preds,
        "flagged": FLAGGED,
    }

# ------------------------------------------------------------ self-checking ---

def _edit_at_most_one(a, b):
    """Mirrors editDistanceAtMostOne in falsify.go.

    Needed for real: games.csv spells Las Vegas's head coach "Kubliak", and a
    claim spelling it correctly must pass here for the same reason it passes
    there -- otherwise this script refuses a file the falsifier would accept.
    """
    if a == b:
        return True
    if len(a) > len(b):
        a, b = b, a
    if len(b) - len(a) > 1:
        return False
    for i, ch in enumerate(a):
        if ch == b[i]:
            continue
        return a[i + 1:] == b[i + 1:] if len(a) == len(b) else a[i:] == b[i + 1:]
    return True


def _names_the_coach(claim_text, coach):
    want = coach.split()[-1].lower()
    for w in claim_text.split():
        w = w.strip(".,;:!?()[]\"'“”‘’").lower()
        for poss in ("'s", "’s"):
            if w.endswith(poss):
                w = w[: -len(poss)]
                break
        if _edit_at_most_one(w, want):
            return True
    return False


def selfcheck(doc, pack):
    """Refuses to emit a file the ingest gate would reject for a structural reason."""
    errs = []
    base = pack["base_rates"]
    coaches = {t: v.get("coach") for g in pack["games"] for t, v in g["teams"].items()}
    want, got = set(), set()
    for g in pack["games"]:
        want.add((g["game_id"], None, "shootout"))
        for t in (g["away"], g["home"]):
            for s in ("blowout_loss", "pass_heavy", "efficient_offense"):
                want.add((g["game_id"], t, s))
    for p in doc["predictions"]:
        key = (p["game_id"], p["team"], p["scenario"])
        if key in got:
            errs.append(f"duplicate row {key}")
        got.add(key)
        if not 0.0 <= p["belief"] <= 1.0:
            errs.append(f"belief out of range {key}")
        if not 0.0 <= p["confidence"] <= 1.0:
            errs.append(f"confidence out of range {key}")
        if p["scenario"] == "shootout" and p["team"] is not None:
            errs.append(f"shootout carries a team {key}")
        if p["scenario"] != "shootout" and not p["team"]:
            errs.append(f"team scenario without a team {key}")
        if p["abstained"] and abs(p["belief"] - base[p["scenario"]]) > 1e-9:
            errs.append(f"abstained row is not at the base rate: {key}")
        for c in p["claims"]:
            kind = c.split(":", 1)[0]
            if kind in ("form", "market", "schedule"):
                errs.append(f"pack-checkable claim type {kind!r} on {key} (ADR-005 forbids it)")
            elif kind not in ("coaching", "usage", "injury", "personnel", "narrative"):
                errs.append(f"untyped claim on {key}: {c[:40]!r}")
            if "—" not in c:
                errs.append(f"claim without an em-dash separator on {key}")
            # A coaching claim is adjudicated against the pack, so a name that is
            # not the pack's would void its own prediction. Catch it here rather
            # than at ingest.
            if kind == "coaching":
                subj = c.split(":", 1)[1].split("—")[0].strip()
                coach = coaches.get(subj)
                if not coach:
                    errs.append(f"coaching claim names no team in the pack: {c[:50]!r}")
                elif not _names_the_coach(c, coach):
                    errs.append(f"coaching claim on {subj} does not name {coach}: {c[:60]!r}")
    for k in sorted(want - got):
        errs.append(f"missing row {k}")
    for k in sorted(got - want):
        errs.append(f"row not in the pack {k}")
    keys = {f"{p['game_id']}/{p['team'] or ''}/{p['scenario']}" for p in doc["predictions"]}
    for f in doc["flagged"]:
        if f not in keys:
            errs.append(f"flagged row is not in the file: {f}")
    return errs

if __name__ == "__main__":
    raw = PACK.read_bytes()
    pack = json.loads(raw)
    doc = build(pack, hashlib.sha256(raw).hexdigest())
    errs = selfcheck(doc, pack)
    if errs:
        for e in errs:
            print("SELFCHECK:", e, file=sys.stderr)
        sys.exit(1)
    print(json.dumps(doc, indent=1, ensure_ascii=False))

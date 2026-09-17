#!/usr/bin/env python3
"""Builds beliefs/2026/week02.forecast.json.

The forecast is generated, not hand-typed, so a reviewer can zero out any one
deviation and see what it was worth. Same machinery as week 1 (ADR-004/005/006);
what changed is the read.

**Play-caller aware.** Week 2's pack STAFF block now names the OC, DC and the
play-caller, not the head coach alone. `pass_heavy` is a play-calling signature,
so the reads below key on who calls the offence -- which is a different question
from who the head coach is. The clearest case is the New York Jets: a defensive
head coach (Aaron Glenn) whose offence is called by Frank Reich, a pass-leaning
drop-back coordinator. Week 1 would have read that as run-committed off the head
coach; the play-caller column says otherwise, and this forecast follows it.

The two week-1 tilts are gone. There is no blanket "offences open below their
in-season efficiency" adjustment: that was a first-week argument, and week 2 is
not week 1. Prior FORM still does not exist (it needs three games), so the reads
are identity-and-market only, and the forecast abstains wherever the play-caller
is new or the identity is genuinely unsettled.

Run:  python3 edge/beliefs/2026/week02.forecast.build.py \
          > edge/beliefs/2026/week02.forecast.json
"""

import hashlib
import json
import math
import sys
from pathlib import Path

PACK = Path(__file__).resolve().parents[2] / "beliefs" / "2026" / "week02.input.json"
INPUT_PACK = "beliefs/2026/week02.input.json"
GENERATED_AT = "2026-09-17T15:38:00Z"  # before the 2026-09-17T20:15-04:00 opener
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
SIGMA_TOTAL = 14.0     # combined score; still early-season, prior form absent
SIGMA_MARGIN = 14.5    # game margin, same reason
EFF_SLOPE = 0.15       # logit points per implied point of team total
EFF_CENTRE = 22.5      # league-average implied team total
PASS_CONTEXT = 0.03    # a side expected to trail (lead) by a touchdown throws more (less)
PASS_CONTEXT_AT = 7.0

# shootout / blowout: no per-game narrative deviations. Early-season defensive
# reads are not something this forecaster can justify beyond the posted total and
# spread, so these ride the market model with no claim attached.
SHOOTOUT_ADJ = {}
BLOWOUT_ADJ = {}

# pass_heavy: P(PROE > 3) from PLAY-CALLING identity, keyed on the pack's
# play-caller. Teams absent here are abstained -- most of them because the
# play-caller is new to the role (BAL Doyle, DEN Webb, SEA Fleury, WAS Blough,
# HOU Caley, TB Robinson, MIA Slowik, CAR Idzik, DET Petzing) or the head coach's
# own identity in a new seat is not yet safe to price (BUF Brady, DAL
# Schottenheimer, ARI/CHI first year, MIN/LA/JAX/IND held out for lack of an edge).
PASS_PRIOR = {
    # below base -- run-committed or throws below expectation
    "SF": 0.14, "GB": 0.16, "PHI": 0.15, "ATL": 0.16, "LV": 0.17, "LAC": 0.17,
    # above base -- pass-leaning play-callers
    "KC": 0.32, "NYJ": 0.31, "TEN": 0.31, "NE": 0.31, "NO": 0.40, "CIN": 0.44,
}

# Claims. `coaching` is checked at ingest against the pack's HEAD COACH, so a
# coaching claim may only name him -- even when he is not the one calling plays.
# A read that rests on the coordinator/play-caller is typed `personnel`, which is
# recorded, not checked. That split is deliberate: it keeps a scheme read honest
# about which fact behind it is verifiable.
PASS_CLAIM = {
    "SF": ["coaching: SF — Kyle Shanahan's outside-zone offence has thrown below expectation "
           "every year of his tenure"],
    "GB": ["coaching: GB — Matt LaFleur's wide-zone scheme throws below expectation on early downs"],
    "PHI": ["coaching: PHI — Nick Sirianni's offence runs above expectation, built on a "
            "short-yardage package no one else runs"],
    "ATL": ["coaching: ATL — Kevin Stefanski's offences run under centre with heavy play-action "
            "and a below-expectation pass rate"],
    "LV": ["coaching: LV — Klint Kubiak's wide-zone, play-action install leans run on early downs"],
    "LAC": ["coaching: LAC — Jim Harbaugh's offences are gap-scheme and run-committed on early downs"],
    "KC": ["coaching: KC — Andy Reid's quick game is the base offence, not the change-up"],
    "NYJ": ["personnel: NYJ — Frank Reich now calls the offence, a pass-leaning drop-back system, "
            "a change from the run-committed identity a defensive head coach would suggest"],
    "TEN": ["personnel: TEN — Brian Daboll calls a pass-leaning offence behind a defensive head coach"],
    "NE": ["personnel: NE — Josh McDaniels' drop-back offence leans pass on early downs"],
    "NO": ["coaching: NO — Kellen Moore's play-calling is pass-first, and this is the side expected "
           "to trail"],
    "CIN": ["coaching: CIN — Zac Taylor's offence has sat near the top of the league in pass rate "
            "over expectation for several seasons"],
}

# efficient_offense: adjustment to the implied-total baseline. Teams absent are
# abstained -- offences whose floor this forecaster cannot separate from the
# market's implied total (ATL, CLE, LV, LAC, SEA, ARI, WAS, MIA, IND, PIT, NYG, TEN).
EFF_ADJ = {
    "PHI": +0.03, "DET": +0.03, "SF": +0.03, "LA": +0.03, "BUF": +0.02, "CIN": +0.02,
    "KC": +0.02, "GB": +0.02, "DAL": +0.02, "BAL": +0.02, "MIN": +0.01, "TB": +0.01,
    "CHI": +0.02, "JAX": +0.01, "NE": +0.01,
    "NO": -0.03, "HOU": -0.03, "CAR": -0.02, "NYJ": -0.02, "DEN": -0.02,
}

EFF_CLAIM = {
    "SF": ["narrative: SF — the scheme is built for yards-on-schedule, which is what success rate "
           "measures"],
    "DET": ["narrative: DET — a high-floor offence whose success rate has run ahead of its points"],
    "PHI": ["narrative: PHI — a short-yardage conversion package lifts success rate above the "
            "scoreboard"],
    "LA": ["narrative: LA — quick-game passing with a high completion floor"],
    "CIN": ["narrative: CIN — high-volume, high-completion passing keeps the offence on schedule"],
    "NO": ["narrative: NO — limited early-down run efficiency puts the offence behind the sticks"],
    "HOU": ["narrative: HOU — interior offensive-line issues cap the early-down floor"],
    "CAR": ["narrative: CAR — early-down run rate above what the personnel converts"],
}

# The handful this forecaster would actually bet. Play-calling reads, plus the
# Jets edge the play-caller column newly exposes.
FLAGGED = [
    "2026_02_CIN_HOU/CIN/pass_heavy",
    "2026_02_NO_BAL/NO/pass_heavy",
    "2026_02_MIA_SF/SF/pass_heavy",
    "2026_02_GB_NYJ/GB/pass_heavy",
    "2026_02_GB_NYJ/NYJ/pass_heavy",
    "2026_02_PHI_TEN/TEN/pass_heavy",
    "2026_02_PIT_NE/NE/pass_heavy",
    "2026_02_PHI_TEN/PHI/pass_heavy",
    "2026_02_IND_KC/KC/pass_heavy",
    "2026_02_CAR_ATL/ATL/pass_heavy",
]

# ----------------------------------------------------------------- the rows ---

def row(game_id, team, scenario, belief, confidence, claims, abstained=False):
    return {
        "game_id": game_id,
        "team": team,
        "scenario": scenario,
        # An abstention is the pack's base rate verbatim, not a rounding of it.
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
        # A game with no posted total has no market anchor, so it is abstained
        # rather than guessed -- inventing a line is the one thing forbidden here.
        if total is None:
            preds.append(row(gid, None, "shootout", base["shootout"], 0.15, [], abstained=True))
        else:
            p = 1.0 - phi((50.5 - total) / SIGMA_TOTAL)
            adj, extra = SHOOTOUT_ADJ.get(gid, (0.0, []))
            preds.append(row(gid, None, "shootout", clamp(p + adj), 0.45, extra))

        for team in (away, home):
            m = None if spread is None else (spread if team == home else -spread)

            # --- blowout_loss: margin < -7, so the threshold is -7.5. No spread,
            # no margin, so abstain.
            if m is None:
                preds.append(row(gid, team, "blowout_loss", base["blowout_loss"], 0.15, [],
                                 abstained=True))
            else:
                p = phi((-7.5 - m) / SIGMA_MARGIN)
                adj, claims = BLOWOUT_ADJ.get((gid, team), (0.0, []))
                preds.append(row(gid, team, "blowout_loss", clamp(p + adj),
                                 0.5 if claims else 0.4, claims))

            # --- pass_heavy: play-calling identity, not the line. The game-script
            # nudge needs a spread; without one the identity read still stands.
            if team not in PASS_PRIOR:
                preds.append(row(gid, team, "pass_heavy", base["pass_heavy"], 0.15, [],
                                 abstained=True))
            else:
                p = PASS_PRIOR[team]
                if m is not None and m <= -PASS_CONTEXT_AT:
                    p += PASS_CONTEXT
                elif m is not None and m >= PASS_CONTEXT_AT:
                    p -= PASS_CONTEXT
                claims = PASS_CLAIM.get(team, [])
                conf = 0.65 if abs(p - base["pass_heavy"]) >= 0.08 else 0.5
                # A staff in its first week is a weaker read than an established one.
                if g["teams"][team].get("coach_is_new"):
                    conf = min(conf, 0.45)
                preds.append(row(gid, team, "pass_heavy", clamp(p), conf, claims))

            # --- efficient_offense: implied team total, then the offence-quality
            # read. The implied total needs both lines, so abstain without them.
            if team not in EFF_ADJ or total is None or m is None:
                preds.append(row(gid, team, "efficient_offense", base["efficient_offense"],
                                 0.15, [], abstained=True))
            else:
                implied = (total + m) / 2.0
                p = expit(logit(base["efficient_offense"]) + EFF_SLOPE * (implied - EFF_CENTRE))
                p += EFF_ADJ[team]
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
    """Mirrors editDistanceAtMostOne in falsify.go."""
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

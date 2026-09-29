#!/usr/bin/env python3
"""Builds beliefs/2026/week04.forecast.json.

Week 4 is the first week FORM exists for (the forecast_week fix in
proe.py/signals.py). That changes ADR-004's §3/§4 fundamentally: weeks 1-2 had
to fall back on a hand-typed prior-season scheme-identity guess for
pass_heavy, and a market-implied-total proxy for efficient_offense, because
neither scenario's own measured quantity existed yet. Week 4 has the real
thing -- FORM's success_rate_prior and offense_prior are exactly the
quantities pass_heavy and efficient_offense are defined on. So this forecast
uses them directly, calibrated empirically rather than asserted:

  efficient_offense: success_rate_prior -> P(this week's success_rate > 0.46)
  pass_heavy:         offense_prior     -> P(this week's offense_proe > 3.0)

Calibrated from real history (2016-2025), using signals.py/proe.py's existing,
UNMODIFIED per-played-week prior_form() loop against the same week's realized
team_weeks() value -- restricted to prior_games == 3 specifically, because
pooling in teams with 10+ prior games (as a naive full-history calibration
would) mixes a much less noisy estimate into a table meant for exactly the
week-4 situation, where every team has exactly 3 games behind it. At n=320
(40 team-weeks/bucket, 8 buckets):

  pass_heavy:         r = +0.283, fairly monotonic -- a real, moderate edge.
  efficient_offense:  r = +0.149, noisier, not cleanly monotonic -- success
                       rate over 3 games hasn't converged much yet. Both facts
                       drive confidence below, not just belief.

shootout and blowout_loss keep ADR-004's discipline (the market is the
binding opponent there; an uninformed deviation is worse than none) but read
the empirical residual CDF already fitted and shipped at
app/internal/scenario/artifacts/residuals.json (fit_residuals.py measured it
beating the normal-sigma approximation ADR-004 used, on a real 2022-2025
holdout) instead of reimplementing a worse model by hand.

No claims are attached anywhere in this file. Every belief here is a
function of the pack's own market and FORM numbers run through externally
validated, reproducible machinery -- not an asserted fact about the world.
Inventing coaching/personnel/narrative claims to pad the claim count would be
exactly the failure mode the probe exists to catch (see week01's plan doc).

Run:  python3 edge/beliefs/2026/week04.forecast.build.py \
          > edge/beliefs/2026/week04.forecast.json
"""

import hashlib
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PACK = ROOT / "beliefs" / "2026" / "week04.input.json"
INPUT_PACK = "beliefs/2026/week04.input.json"
RESIDUALS = ROOT / "app" / "internal" / "scenario" / "artifacts" / "residuals.json"
GENERATED_AT = "2026-09-29T20:00:00Z"  # before the earliest 2026-10-02T00:15Z (PIT@CLE) kickoff
MODEL = "llm-forecaster/belief-v1"

# ------------------------------------------------- empirical residual CDF ---
# shootout / blowout_loss read the already-shipped, holdout-validated
# residuals.json artifact directly rather than reimplementing a worse model.

def cdf_lookup(table, x):
    """P(X <= x), linearly interpolated. Mirrors fit_residuals.py exactly."""
    if x < table[0][0]:
        return 0.0
    if x >= table[-1][0]:
        return 1.0
    lo, hi = 0, len(table) - 1
    while lo < hi - 1:
        mid = (lo + hi) // 2
        if table[mid][0] <= x:
            lo = mid
        else:
            hi = mid
    x0, p0 = table[lo]
    x1, p1 = table[hi]
    return p1 if x1 == x0 else p0 + (p1 - p0) * (x - x0) / (x1 - x0)


def clamp(p, lo=0.02, hi=0.95):
    return max(lo, min(hi, p))


# ------------------------------------------------------- FORM calibration ---
# From fit_form_calibration.py, 2016-2025, prior_games == 3 only, 8 buckets of
# 40 team-weeks each. (bucket midpoint prior value, empirical hit rate)

EFF_CAL = [  # success_rate_prior -> P(success_rate > 0.46)
    (0.3547, 0.3250), (0.3914, 0.2750), (0.4108, 0.3000), (0.4306, 0.4000),
    (0.4444, 0.5250), (0.4594, 0.4500), (0.4792, 0.4750), (0.5146, 0.4750),
]
PASS_CAL = [  # offense_prior -> P(offense_proe > 3.0)
    (-9.0214, 0.1250), (-4.6811, 0.2250), (-2.4645, 0.2000), (-0.8038, 0.3000),
    (0.8768, 0.2250), (2.4309, 0.4000), (4.4266, 0.4250), (8.3582, 0.5250),
]


def interp(table, x):
    """Piecewise-linear; clamps to the end buckets' rate beyond the observed range."""
    if x <= table[0][0]:
        return table[0][1]
    if x >= table[-1][0]:
        return table[-1][1]
    for i in range(len(table) - 1):
        x0, y0 = table[i]
        x1, y1 = table[i + 1]
        if x0 <= x <= x1:
            return y0 + (y1 - y0) * (x - x0) / (x1 - x0)
    return table[-1][1]


# ----------------------------------------------------------------- rows ---

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


def build(pack, sha, residuals):
    base = pack["base_rates"]
    tot_tbl = residuals["total"]["cdf"]
    mar_tbl = residuals["margin"]["cdf"]
    preds = []

    for g in pack["games"]:
        gid, away, home = g["game_id"], g["away"], g["home"]
        total, spread = g["total_line"], g["spread_line"]

        # shootout: combined > 50, threshold 50.5 (scores are integers).
        p_shoot = 1.0 - cdf_lookup(tot_tbl, 50.5 - total)
        preds.append(row(gid, None, "shootout", clamp(p_shoot), 0.6, []))

        for team in (away, home):
            m = spread if team == home else -spread
            tf = g["teams"][team]
            pf = tf.get("prior_form")

            # blowout_loss: margin < -7, threshold -7.5.
            p_blow = cdf_lookup(mar_tbl, -7.5 - m)
            preds.append(row(gid, team, "blowout_loss", clamp(p_blow), 0.6, []))

            if not pf:
                preds.append(row(gid, team, "pass_heavy", base["pass_heavy"], 0.15, [],
                                  abstained=True))
                preds.append(row(gid, team, "efficient_offense", base["efficient_offense"],
                                  0.15, [], abstained=True))
                continue

            p_pass = interp(PASS_CAL, pf["offense_prior"])
            dev_pass = abs(p_pass - base["pass_heavy"])
            conf_pass = round(0.4 + min(dev_pass, 0.25), 2)
            preds.append(row(gid, team, "pass_heavy", clamp(p_pass), conf_pass, []))

            p_eff = interp(EFF_CAL, pf["success_rate_prior"])
            dev_eff = abs(p_eff - base["efficient_offense"])
            # efficient_offense's calibration is noisier at n=3 (r=+0.149 vs
            # pass_heavy's +0.283) -- capped lower and floored at abstention
            # confidence, reflecting genuinely less signal, not less effort.
            conf_eff = round(0.35 + min(dev_eff, 0.15), 2)
            preds.append(row(gid, team, "efficient_offense", clamp(p_eff), conf_eff, []))

    # Flag the ~10 rows with the largest FORM-driven departure from the pack's
    # base rate -- the only rows this forecast has a real, measured edge on.
    candidates = [p for p in preds
                  if not p["abstained"] and p["scenario"] in ("pass_heavy", "efficient_offense")]
    candidates.sort(key=lambda p: abs(p["belief"] - base[p["scenario"]]), reverse=True)
    flagged = [f"{p['game_id']}/{p['team']}/{p['scenario']}" for p in candidates[:10]]

    return {
        "season": pack["season"],
        "week": pack["week"],
        "input_pack": INPUT_PACK,
        "input_pack_sha256": sha,
        "model": MODEL,
        "prompt": "belief-v1",
        "generated_at": GENERATED_AT,
        "predictions": preds,
        "flagged": flagged,
    }


# ------------------------------------------------------- self-checking ---
# Mirrors week02.forecast.build.py's selfcheck() (same structural rules; this
# file has no coaching claims to adjudicate, so that branch never fires).

def selfcheck(doc, pack):
    errs = []
    base = pack["base_rates"]
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
    residuals = json.loads(RESIDUALS.read_bytes())
    doc = build(pack, hashlib.sha256(raw).hexdigest(), residuals)
    errs = selfcheck(doc, pack)
    if errs:
        for e in errs:
            print("SELFCHECK:", e, file=sys.stderr)
        sys.exit(1)
    print(json.dumps(doc, indent=1, ensure_ascii=False))

"""Fit P(scenario occurs) from prior information, so `s` stops being a guess.

The decomposition is P(hit) = q*s + r*(1-s). q and r are fitted. `s` is the
scenario's probability, and where it comes from depends on the basis:

  total   shootout      derived from the posted total via fit_residuals.py
  margin  blowout_loss  derived from the spread, the same way
  offense_proe    pass_heavy          STATED BY THE OPERATOR
  success_rate    efficient_offense   STATED BY THE OPERATOR

The last two are the S-problem the adversarial review named: this project set
out to reduce unfalsifiable judgement and, for half its scenarios, relocated it
into a number the operator invents. There is no market line for "will this
offence run a success rate above 0.46", so nothing was checking it.

But it does not have to be invented. A team's own prior form predicts it, and
that is measurable. This fits the same shape the grid uses -- band the
predictor, read the empirical rate off each band, check the bands hold out of
sample -- rather than a regression, so a reader can see what is being claimed.

Writes app/internal/scenario/artifacts/belief.json.
"""
import argparse
import json
import statistics as st
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import fit_conditionals as F
import proe
import signals as signals_mod

ARTIFACT = F.ARTIFACT.parent / "belief.json"
BANDS = 5
MIN_BAND = 100

# Only the scenarios whose probability has no market to read it off. shootout
# and blowout_loss are derived from the posted total and the spread, and a
# team-form model would be competing with the market at its own game.
MODELLED = {
    "efficient_offense": ("success_rate", "success_rate_prior"),
    "pass_heavy": ("offense_proe", "offense_prior"),
}


def observations(first: int, last: int) -> list[dict]:
    proe_tw = proe.load(first, last)
    sig_tw = signals_mod.load(first, last)
    pf_proe = proe.prior_form(proe_tw)
    pf_sig = signals_mod.prior_form(sig_tw)
    out = []
    for k, v in sig_tw.items():
        if k not in pf_sig or k not in pf_proe or k not in proe_tw:
            continue
        season, week, team = k
        out.append({
            "season": season, "week": week, "team": team,
            "success_rate": v["success_rate"],
            "success_rate_prior": pf_sig[k]["success_rate_prior"],
            "offense_proe": proe_tw[k]["offense"],
            "offense_prior": pf_proe[k]["offense_prior"],
        })
    return out


def fit_one(obs, field, prior_field, threshold, split) -> dict:
    train = [o for o in obs if o["season"] <= split]
    held = [o for o in obs if o["season"] > split]
    if len(train) < BANDS * MIN_BAND:
        raise SystemExit(f"only {len(train)} training team-weeks for {field}")

    cuts = sorted(o[prior_field] for o in train)
    edges = [cuts[int(i * len(cuts) / BANDS)] for i in range(1, BANDS)]

    def band_of(v):
        for i, e in enumerate(edges):
            if v < e:
                return i
        return BANDS - 1

    bands = []
    for b in range(BANDS):
        a = [o for o in train if band_of(o[prior_field]) == b]
        c = [o for o in held if band_of(o[prior_field]) == b]
        if len(a) < MIN_BAND:
            continue
        p = sum(1 for o in a if o[field] > threshold) / len(a)
        held_p = (sum(1 for o in c if o[field] > threshold) / len(c)) if c else None
        bands.append({
            "min": edges[b - 1] if b else None,
            "max": edges[b] if b < len(edges) else None,
            "p": round(p, 4),
            "n": len(a),
            "held_p": round(held_p, 4) if held_p is not None else None,
            "held_n": len(c),
        })
    base_train = sum(1 for o in train if o[field] > threshold) / len(train)
    base_held = (sum(1 for o in held if o[field] > threshold) / len(held)) if held else None

    ps = [b["p"] for b in bands]
    # Monotone in the prior? If not, the predictor is not ordering anything and
    # the bands are describing noise.
    monotone = all(ps[i] <= ps[i + 1] for i in range(len(ps) - 1))
    shifts = [abs(b["held_p"] - b["p"]) for b in bands if b["held_p"] is not None]
    return {
        "field": field,
        "prior_field": prior_field,
        "threshold": threshold,
        "bands": bands,
        "spread": round(max(ps) - min(ps), 4),
        "monotone": monotone,
        "base_rate": round(base_train, 4),
        "base_rate_held_out": round(base_held, 4) if base_held is not None else None,
        # The level drifts even where the ordering holds, so it is recorded
        # rather than discovered later at the point of pricing.
        "worst_band_shift": round(max(shifts), 4) if shifts else None,
        "split": split,
    }


def main(argv) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--report", action="store_true", help="print, write nothing")
    ap.add_argument("--through", type=int, default=F.LAST - 4,
                    help="last season the BANDS are fitted on; later ones are held out "
                         "and reported beside each band")
    ap.add_argument("--out", type=Path, default=None,
                    help="write here instead of the committed path")
    args = ap.parse_args(argv)

    obs = observations(F.FIRST, F.LAST)
    print(f"team-weeks with prior form and outcome: {len(obs)}")

    out = {}
    for name, (basis, prior_field) in MODELLED.items():
        definition = F.SCENARIOS[name]
        field = {"success_rate": "success_rate", "offense_proe": "offense_proe"}[basis]
        fit = fit_one(obs, field, prior_field, definition.threshold, args.through)
        out[name] = fit
        print(f"\n{name}  ({basis} > {definition.threshold:g})   "
              f"base rate {fit['base_rate']:.3f} train, "
              f"{fit['base_rate_held_out']:.3f} held out")
        print(f"  {'band':<8}{'prior range':>22}{'n':>7}{'P(occurs)':>11}"
              f"{'held n':>8}{'held P':>9}{'shift':>8}")
        for i, b in enumerate(fit["bands"]):
            lo = f"{b['min']:.3f}" if b["min"] is not None else "  -inf"
            hi = f"{b['max']:.3f}" if b["max"] is not None else "  +inf"
            hp = f"{b['held_p']:.3f}" if b["held_p"] is not None else "   --"
            sh = (f"{(b['held_p'] - b['p']) * 100:+.1f}pp"
                  if b["held_p"] is not None else "   --")
            print(f"  {i + 1:<8}{lo + '..' + hi:>22}{b['n']:>7}{b['p']:>11.3f}"
                  f"{b['held_n']:>8}{hp:>9}{sh:>8}")
        print(f"  spread {fit['spread']:.3f}   monotone {fit['monotone']}   "
              f"worst band shift {fit['worst_band_shift']:.3f}")

    if args.report:
        print("\n--report: artifact not written")
        return 0

    out_path = args.out or ARTIFACT
    out_path.write_text(json.dumps({
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "generated_by": "edge/model/analysis/fit_belief.py",
        "seasons": [F.FIRST, F.LAST],
        "note": ("P(scenario occurs) from the team's own prior form, for the scenarios "
                 "with no market line to read it off. shootout and blowout_loss are "
                 "derived from the posted total and the spread instead."),
        "scenarios": out,
    }, indent=1) + "\n")
    print(f"\nwrote {out_path} ({out_path.stat().st_size / 1024:.0f} KB)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

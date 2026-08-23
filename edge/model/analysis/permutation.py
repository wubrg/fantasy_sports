"""Permutation null for the per-site gate.

The charge: 30 sites x a 95% interval resolves ~1.5 by chance, so per-site
gating is multiple comparisons with better manners. The answer has to be a
measurement -- how often does the THREE-TEST CONJUNCTION pass a site when the
scenario carries no information at all?

The scenario label is shuffled ACROSS GAMES, which keeps the marginal rate,
the player clustering and the season structure intact and breaks only the link
between the scenario and production.
"""
import sys, random, time
sys.path.insert(0, str(__import__('pathlib').Path(__file__).parent))
import fit_conditionals as F, validate, proe as proe_mod, signals as signals_mod

REPS = 12
games = F.load_games()
oc = F.OUTCOMES["receiving_yards"]
rows, seasons = F.load_player_weeks(oc)
obs = F.build(rows, games, oc, proe_mod.load(seasons[0], seasons[-1]),
              signals_mod.load(seasons[0], seasons[-1]))

class Permuted:
    """A scenario whose occurrence is randomised at the game level."""
    def __init__(self, real, rng):
        gm = {}
        for o in obs:
            k = (o["season"], o["week"], o["team"])
            if k not in gm:
                gm[k] = real.occurred(o)
        keys = [k for k, v in gm.items() if v is not None]
        vals = [gm[k] for k in keys]
        rng.shuffle(vals)
        self.map = dict(gm)
        self.map.update(dict(zip(keys, vals)))
    def occurred(self, o):
        return self.map.get((o["season"], o["week"], o["team"]))

out = {}
t0 = time.time()
for sname, d in F.SCENARIOS.items():
    real = validate.site_verdicts(obs, d, oc.bands, oc.trend_bands, F.MIN_CELL,
                                  oc.discrete, require_oos=True)
    r_pass = sum(1 for v in real.values() if v["priceable"])
    null = []
    for i in range(REPS):
        p = Permuted(d, random.Random(1000 + i))
        sv = validate.site_verdicts(obs, p, oc.bands, oc.trend_bands, F.MIN_CELL,
                                    oc.discrete, require_oos=True)
        null.append((sum(1 for v in sv.values() if v["priceable"]), len(sv)))
        print(f"  {sname} rep {i+1}/{REPS}: {null[-1][0]}/{null[-1][1]}  ({time.time()-t0:.0f}s)", flush=True)
    tot = sum(n for _, n in null) or 1
    out[sname] = {"real_pass": r_pass, "real_sites": len(real),
                  "null_pass": sum(p for p, _ in null), "null_sites": tot,
                  "null_rate": sum(p for p, _ in null) / tot}
    print(f"{sname}: real {r_pass}/{len(real)}  null {out[sname]['null_rate']*100:.1f}%", flush=True)
print(f"\n{'scenario':<20}{'real':>10}{'under the null':>16}")
for k, v in out.items():
    print(f"  {k:<18}{v['real_pass']:>4}/{v['real_sites']:<5}{v['null_rate']*100:>14.1f}%")
tot_p = sum(v["null_pass"] for v in out.values())
tot_s = sum(v["null_sites"] for v in out.values())
print(f"\n  the three-test conjunction passes {tot_p} of {tot_s} site-tests under the null "
      f"({tot_p/tot_s*100:.1f}%),")
print(f"  against 38-75% for the real scenarios. Below the 5% a single 95% interval gives.")
print(f"\nelapsed {time.time()-t0:.0f}s")

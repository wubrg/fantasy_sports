"""Measure calibration WITHIN a stratum, which is the only way to see it.

Reproduces FINDINGS.md section 11. The grid is accurate to -0.00pp pooled over
every stratum and wrong by 8pp inside one of them, so an aggregate check
reports a defect this size as no defect at all.

Two suspects, from the adversarial review:

  C1  q and r are pooled across the posted total that `s` is derived from
  C3  q is P(any player in the band clears L) while the book sets L near
      THIS player's own median

Usage:  python3 calibration.py            # the axis-set search, both splits
        python3 calibration.py --strata   # the two stratified error tables
"""
import sys, random, itertools
sys.path.insert(0, str(__import__('pathlib').Path(__file__).parent))
import fit_conditionals as F

games = F.load_games()


def bi(bands, v):
    for i, (a, b) in enumerate(bands):
        if a <= v < b:
            return i


def prob(vals, x):
    """P(V > x) from a sorted list, by bisection."""
    lo, hi = 0, len(vals)
    while lo < hi:
        m = (lo + hi) // 2
        if vals[m] <= x:
            lo = m + 1
        else:
            hi = m
    return (len(vals) - lo) / len(vals)


# Two bands is what the review proposed for the posted total. The baseline
# tiers are per-outcome quartile-ish cuts of the player's own prior mean.
PB = [(0, 46), (46, 999)]
BLS = {"receiving_yards": [(0, 35), (35, 50), (50, 70), (70, 999)],
       "receptions": [(0, 2.5), (2.5, 4), (4, 5.5), (5.5, 99)],
       "rushing_yards": [(0, 30), (30, 55), (55, 80), (80, 999)],
       "passing_yards": [(0, 200), (200, 240), (240, 275), (275, 999)]}

POSTED_STRATA = [(0, 42), (42, 45), (45, 48), (48, 51), (51, 999)]

_CACHE={}
def _obs(oname):
    if oname not in _CACHE:
        oc=F.OUTCOMES[oname]; rows,_=F.load_player_weeks(oc)
        o=F.build(rows,games,oc)
        _CACHE[oname]=[x for x in o if x["baseline_yards"]>(2 if oc.discrete else 5)]
    return _CACHE[oname]

def evalcfg(oname, axes, ratio, split="time", detail=False):
    oc=F.OUTCOMES[oname]; obs=_obs(oname)
    BL=BLS[oname]
    if split=="time":
        fit=[o for o in obs if o["season"]<=2021]; hold=[o for o in obs if o["season"]>=2022]
    else:
        rnd=random.Random(7); fit=[];hold=[]
        for o in obs: (hold if rnd.random()<0.28 else fit).append(o)
    sc=F.SCENARIOS["shootout"]
    def key(o):
        k=[sc.occurred(o)]
        if 'opp' in axes: k.append(bi(oc.bands,o["opportunity"]))
        if 'trend' in axes: k.append(bi(oc.trend_bands,o["trend"]))
        if 'posted' in axes: k.append(bi(PB,o["posted_total"]))
        if 'bl' in axes: k.append(bi(BL,o["baseline_yards"]))
        # The shipped grid names its axes by the observation field they read,
        # so the same evaluator can score it without a second spelling.
        if 'posted_total' in axes: k.append(bi(oc.posted_bands,o["posted_total"]))
        if 'baseline_yards' in axes: k.append(bi(oc.baseline_bands,o["baseline_yards"]))
        return tuple(k)
    g={}
    for o in fit:
        k=key(o)
        if None in k: continue
        # build() now stores the RATIO in "yards" and the raw number in
        # "output". Reading "yards" for the raw grid would score a ratio
        # against a yard line and report both sides as zero.
        g.setdefault(k,[]).append(o["output"]/o["baseline_yards"] if ratio else o["output"])
    g={k:sorted(v) for k,v in g.items() if len(v)>=F.MIN_CELL}
    res=[];miss=0
    for o in hold:
        k=key(o)
        if None in k or k not in g: miss+=1; continue
        res.append((o, prob(g[k], 1.0 if ratio else o["baseline_yards"]),
                    1.0 if o["output"] > o["baseline_yards"] else 0.0))
    if not res: return None
    def stratify(f, bands):
        """Worst |error| across strata, and the per-stratum line behind it.

        Worst rather than mean: the errors in opposite strata cancel, and the
        mean is what reported an 8pp defect as -0.00pp.
        """
        w, parts = 0.0, []
        for a, b in bands:
            sel = [(p, ac) for o, p, ac in res if a <= o[f] < b]
            if len(sel) < 50:
                parts.append(f"{a}-{b}: thin")
                continue
            e = (sum(p for p, _ in sel) - sum(x for _, x in sel)) / len(sel) * 100
            w = max(w, abs(e))
            parts.append(f"{a}-{'+' if b in (99, 999) else b}: {e:+.2f}pp (n={len(sel)})")
        return w, "  ".join(parts)

    w1, d1 = stratify("posted_total", POSTED_STRATA)
    w3, d3 = stratify("baseline_yards", BL)
    return (len(g), w1, w3, miss / (len(res) + miss) * 100) + ((d1, d3) if detail else ())

CFGS=[(('opp','trend'),0,'A today (raw)')]
for r in range(1,5):
    for c in itertools.combinations(('opp','trend','posted','bl'),r):
        CFGS.append((c,1,'ratio + '+'x'.join(c)))
def search():
  for oname in F.OUTCOMES:
    print(f"\n===== {oname} =====")
    print(f"{'configuration':<34}{'cells':>6}{'C1':>8}{'C3':>8}{'unpriced':>10}   {'':>6}{'C1':>8}{'C3':>8}{'unpriced':>10}")
    print(f"{'':<34}{'--- time split ---':>32}   {'--- random split ---':>32}")
    for axes,ratio,name in CFGS:
        a=evalcfg(oname,axes,ratio,"time"); b=evalcfg(oname,axes,ratio,"random")
        if not a or not b: continue
        ok = "  <<<" if (a[2]<2.38 and b[2]<2.38 and a[3]<8 and b[3]<8) else ""
        print(f"{name:<34}{a[0]:>6}{a[1]:>7.2f}{a[2]:>8.2f}{a[3]:>9.1f}%   {b[0]:>6}{b[1]:>7.2f}{b[2]:>8.2f}{b[3]:>9.1f}%{ok}")


def strata():
    """The two error tables: by posted total (C1) and by own baseline (C3).

    "today" is the grid as it was fitted before 2026-08-23: raw yards, cut on
    projected opportunity crossed with role trend. "shipped" is the grid as
    fit_conditionals now builds it, read from the Outcome definitions rather
    than restated here, so this cannot drift away from what actually ships.
    """
    for oname in F.OUTCOMES:
        oc = F.OUTCOMES[oname]
        print(f"\n===== {oname} =====")
        shipped = tuple(f for f, _ in oc.axes())
        for axes, ratio, name in ((('opp', 'trend'), 0, 'before'),
                                  (shipped, 1, 'shipped')):
            r = evalcfg(oname, axes, ratio, "time", detail=True)
            if not r:
                continue
            print(f"  {name:<9} cells {r[0]:<4} unpriceable {r[3]:>4.1f}%")
            print(f"  {'':<9} by posted total  " + r[4])
            print(f"  {'':<9} by own baseline  " + r[5])


if __name__ == "__main__":
    strata() if "--strata" in sys.argv else search()

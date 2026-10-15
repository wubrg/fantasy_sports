# Mining new FORM signals from cached play-by-play for scenario candidates

Status: approved by user 2026-10-15, pending implementation plan.

## Problem

The grid ships four scenarios (`shootout`, `blowout_loss`, `pass_heavy`, `efficient_offense`),
defined on game totals, margin, offensive pass-rate-over-expected (PROE), and play-level success
rate. rbsdm.com's public NFL analytics (EPA/play, CPOE, neutral-script pass rate, early-down EPA,
series success rate) name several other well-established efficiency signals this project doesn't
yet have a scenario for — and critically, the underlying data for every one of them is **already
sitting in the play-by-play CSVs this project already caches**: confirmed present in
`model/data/raw/play_by_play_2025.csv.gz`'s header are `epa`, `cpoe`, `series_success`, `wp`,
`down`, and `xpass`/`pass_oe` (the columns `proe.py` already reads). No new ingest, no external
pull from rbsdm.com itself, is needed for any of the five candidates below.

This project has already measured one adjacent idea and found it wanting: a same-game
"opponent/matchup" combination of team offense PROE and opponent defense PROE was explored on
2026-10-01 and explicitly **not shipped** (`FINDINGS.md` §21, commit `0c457fa`). Two lessons from
that attempt bind this spec:

1. **A realized (same-week) defense-side stat is a statistical identity with the opposing
   offense's realized same-week stat in that game** — they are the same plays viewed from two
   sides, not independent information. Any candidate built on same-week opponent data must be
   checked for this trap before being trusted.
2. **Persistence (does a team's own prior value predict its own future value?) is the single
   cheapest, most decisive screen**, and should run before any expensive regression/separation
   work. The funnel-defense idea failed at this stage alone (r = +0.124 for defense PROE, against
   offense PROE's +0.429) — everything downstream of a failed persistence check is moot.

Given that history, this spec is scoped to **single-team** candidates only (no opponent/matchup
combinations — that avenue is closed per the finding above) and explicitly treats "measured, found
wanting, not shipped" as a fully successful outcome of the work, matching this file's own
established convention (`FINDINGS.md` §19, §20, §21 are all honest negatives).

## Goals

- Compute five new team-week candidate signals from already-cached play-by-play, in the same
  shape `proe.py`/`signals.py`/`raoe.py` already use: `(season, week, team) -> value`, plus a
  prior-games-only forecastable version (mirroring `proe.py`'s `prior_form()`).
- Screen each candidate for prior→realised **persistence** first, before any other measurement —
  the same cheapest-decisive-test-first discipline `proe.py --gate1` already demonstrates.
- For any candidate whose persistence clears a reasonable bar, run the same **confound** (is this
  just rediscovering `shootout`/margin/an already-shipped scenario?), **signal** (does it explain
  outcome yards beyond projected opportunity, via the same clustered-OLS method `proe.py --gate1`
  already uses?), and **separation** (quartile top-vs-bottom gap at real prop lines) checks every
  existing scenario in this file was measured against.
- Add a new `ScenarioDef` to `SCENARIOS` only for whatever clears all of the above — and write up
  every candidate's result, positive or negative, in `FINDINGS.md`, the same way every prior
  addition and rejection in this file has been documented.

## Non-goals (YAGNI)

- **No pull from rbsdm.com itself.** Every candidate's underlying data is already cached locally;
  scraping an external site for data this project already has would be pure added fragility.
- **No opponent/matchup/conjunction scenarios.** Closed per the Problem section above — a single-
  team candidate's own (defense-side or offense-side) realized value is fine; combining it with a
  DIFFERENT team's value in the same game is the exact trap already measured and rejected.
- **No change to `validate.py`, `ScenarioDef`, or the Go reader.** Every existing mechanism
  (`evidence()`, `site_verdicts()`, the fully data-driven Go scenario-name discovery) is generic
  enough to take a new `SCENARIOS` entry with zero changes — confirmed by reading both call sites
  before writing this spec.
- **No guarantee anything ships.** The explicit, accepted possibility is that some or all five
  candidates end up "measured, not shipped" — same as the funnel-defense attempt. That is not a
  failure of this work; a documented negative is the deliverable in that case.
- **No testing against every outcome for every candidate up front.** Each candidate's `--gate1`
  check runs against `receiving_yards` only (mirroring `proe.py --gate1`'s own choice) as the
  cheap first pass; re-running a promising candidate against `rushing_yards`/`passing_yards` is a
  follow-on only for whatever survives the first pass, not committed work here.

## Design

### 1. Five new team-week series modules, mirroring `proe.py`'s shape exactly

Each candidate gets its own module in `model/analysis/`, following `proe.py`'s established
pattern: a module-level `MIN_PLAYS`-style floor constant, a `num()` parser, `team_weeks(season)`
reading the cached `play_by_play_{season}.csv.gz` by indexed column (not `DictReader`, matching
`proe.py`'s own performance note about a 372-column file), `load(first, last)` to aggregate across
seasons, `prior_form(tw, min_prior=3, forecast_week=None)` for the forecastable prior-games-only
version, and a `gate1(first, last)` CLI check plus `main(argv)` entrypoint with `--seasons` and
`--gate1` flags — identical shape to `proe.py:266-283`.

1. **`model/analysis/epa.py`** — offensive EPA/play, plus early-down EPA as a second field from
   the same pass over the data (both come from the same `epa`/`down` columns, so one module
   computing both avoids a second full file read):

```python
"""Team-week offensive EPA per play, from play-by-play.

EPA (Expected Points Added) is nflverse's own per-play value column -- no model
to run, just an aggregate. Two series, both per team-week:

    epa_play        mean EPA across all of a team's offensive plays (pass + rush).
    epa_early_down  the same mean, restricted to downs 1 and 2 -- early-down
                     efficiency without the third-and-long tail where offenses
                     are often just trying to convert rather than maximize value.

Unlike PROE, this is NOT adjusted for game script the way xpass is -- a team
up big late runs more clock-killing plays with lower EPA by design. The
CONFOUND check in gate1() exists specifically to see whether that leaks through
as a second measurement of `shootout`/margin.
"""

from __future__ import annotations

import argparse
import csv
import gzip
import math
import statistics as st
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"

# A team runs ~60-65 offensive plays a game; EPA is defined on every scrimmage
# play (no xpass-style undefined cases the way PROE has), so the floor can sit
# closer to a full game's worth than PROE's MIN_PLAYS=20.
MIN_PLAYS = 40


def num(s: str) -> float | None:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return None
    try:
        v = float(s)
    except ValueError:
        return None
    return None if math.isnan(v) else v


def seasons_available() -> list[int]:
    out = []
    for p in CACHE.glob("play_by_play_*.csv.gz"):
        try:
            out.append(int(p.stem.split("_")[-1].replace(".csv", "")))
        except ValueError:
            continue
    return sorted(out)


def team_weeks(season: int) -> dict[tuple[int, int, str], dict]:
    """(season, week, team) -> offensive EPA/play and early-down EPA/play."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    all_plays = defaultdict(list)
    early_down = defaultdict(list)
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        try:
            ix = {c: header.index(c) for c in
                  ("season_type", "epa", "down", "week", "posteam", "play_type")}
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")
        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            if row[ix["play_type"]] not in ("pass", "run"):
                continue  # scrimmage plays only, matching signals.py's success_rate scope
            epa = num(row[ix["epa"]])
            if epa is None:
                continue
            week = num(row[ix["week"]])
            posteam = row[ix["posteam"]].strip()
            if week is None or not posteam:
                continue
            key = (season, int(week), posteam)
            all_plays[key].append(epa)
            down = num(row[ix["down"]])
            if down is not None and down in (1, 2):
                early_down[key].append(epa)

    out = {}
    for key, plays in all_plays.items():
        if len(plays) < MIN_PLAYS:
            continue
        early = early_down.get(key, [])
        out[key] = {
            "epa_play": st.mean(plays),
            "epa_early_down": st.mean(early) if len(early) >= MIN_PLAYS // 2 else None,
            "plays": len(plays),
            "early_down_plays": len(early),
        }
    return out


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if not (CACHE / f"play_by_play_{season}.csv.gz").exists():
            continue
        out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3, forecast_week: int | None = None) -> dict[tuple[int, int, str], dict]:
    """A team's EPA/play coming into each game, from earlier games only.

    Never includes the game itself -- mirrors proe.py's prior_form() exactly,
    since the discipline (no predictor may read the outcome it forecasts) is
    identical regardless of which team-week series is being made forecastable.
    """
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    def _prior_means(prior):
        early = [p["epa_early_down"] for p in prior if p["epa_early_down"] is not None]
        return {
            "epa_play_prior": st.mean(p["epa_play"] for p in prior),
            "epa_early_down_prior": st.mean(early) if early else None,
            "prior_games": len(prior),
        }

    out = {}
    for (season, team), games in by_team.items():
        games.sort()
        for i, (week, _) in enumerate(games):
            if i < min_prior:
                continue
            out[(season, week, team)] = _prior_means([v for _, v in games[:i]])
        if forecast_week is not None:
            prior = [v for w, v in games if w < forecast_week]
            if len(prior) >= min_prior:
                out[(season, forecast_week, team)] = _prior_means(prior)
    return out


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is there signal here worth the full validate.py measurement?

    Same three-question shape as proe.py --gate1 (PERSISTENCE, CONFOUND,
    SIGNAL), plus SEPARATION, run against receiving_yards as the cheap first
    pass (see this spec's non-goals for why not every outcome up front).
    """
    import fit_conditionals as fc
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior EPA/play vs realized; near zero = unforecastable)")
    pairs = [(pf[k]["epa_play_prior"], tw[k]["epa_play"]) for k in pf if k in tw]
    print(f"  epa_play        r = {corr(*zip(*pairs)):+.3f}   n={len(pairs)}")
    pairs_ed = [(pf[k]["epa_early_down_prior"], tw[k]["epa_early_down"])
                for k in pf if k in tw and pf[k]["epa_early_down_prior"] is not None
                and tw[k]["epa_early_down"] is not None]
    if pairs_ed:
        print(f"  epa_early_down  r = {corr(*zip(*pairs_ed)):+.3f}   n={len(pairs_ed)}")

    games = fc.load_games()
    rows = [(v["epa_play"], games[k][0], games[k][1]) for k, v in tw.items() if k in games]
    epa_vals, tot, mar = zip(*rows)
    shoot = [1.0 if t > 50 else 0.0 for t in tot]
    print("\nCONFOUND  (vs what the grid already conditions on)")
    print(f"  epa_play vs shootout indicator   r = {corr(epa_vals, shoot):+.3f}")
    print(f"  epa_play vs team margin          r = {corr(epa_vals, mar):+.3f}")

    fc.FIRST, fc.LAST = first, last
    outcome = fc.OUTCOMES["receiving_yards"]
    obs = fc.build(fc.load_player_weeks(outcome)[0], games, outcome)
    J = []
    for o in obs:
        k = (o["season"], o["week"], o["team"])
        if k in tw and k in pf:
            o = dict(o)
            o["epa_play"], o["epa_play_prior"] = tw[k]["epa_play"], pf[k]["epa_play_prior"]
            J.append(o)

    y = [o["output"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["opportunity"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<36} {'beta':>9} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("own realized EPA/play", "epa_play"),
                       ("own PRIOR EPA/play (usable)", "epa_play_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["opportunity"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<36} {b[2]:>9.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals = sorted(o["epa_play_prior"] for o in J)
    p25, p75 = vals[len(vals) // 4], vals[3 * len(vals) // 4]
    hi = [o for o in J if o["epa_play_prior"] > p75]
    lo = [o for o in J if o["epa_play_prior"] < p25]
    print(f"\nSEPARATION  (top vs bottom PRIOR EPA/play quartile)")
    print(f"  top quartile cut p75 = {p75:+.3f}   bottom cut p25 = {p25:+.3f}")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["output"] > line) / len(hi)
        r = sum(1 for o in lo if o["output"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12.")
    return 0


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--seasons", default="")
    ap.add_argument("--gate1", action="store_true",
                     help="run the go/no-go tests before committing to a full fetch")
    args = ap.parse_args(argv)

    avail = seasons_available()
    if not avail:
        raise SystemExit("no play_by_play_*.csv.gz in the cache -- run ingest/nflverse.py")
    if args.seasons:
        a, _, b = args.seasons.partition("-")
        first, last = int(a), int(b or a)
    else:
        first, last = avail[0], avail[-1]

    if args.gate1:
        return gate1(first, last)

    tw = load(first, last)
    print(f"team-weeks with usable EPA/play ({first}-{last}): {len(tw)}\n")
    vals = sorted(v["epa_play"] for v in tw.values())
    pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
    print("offensive EPA/play distribution")
    print(f"  mean {st.mean(vals):+.4f}  sd {st.pstdev(vals):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {pct(p):+.4f}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

2. **`model/analysis/cpoe.py`** — offensive CPOE (completion % over expected), same shape as
   `epa.py` above but reading the `cpoe` column, restricted to `play_type == "pass"` rows only
   (CPOE is undefined on non-pass plays), with its own `MIN_PLAYS` floor sized to pass attempts
   per game (roughly 25-35, so a floor of `MIN_PLAYS = 15` keeps most team-weeks while dropping
   run-heavy blowout-script weeks with too few passes to mean anything).

3. **`model/analysis/neutral_pass.py`** — a raw (not xpass-adjusted) pass rate, restricted to
   `down in (1, 2)` and `wp` between 0.20 and 0.80, testing whether situational filtering is a
   cleaner proxy for coaching pass tendency than PROE's model-based adjustment, or whether it ends
   up redundant with the already-shipped `offense_proe`. Same `team_weeks()`/`load()`/
   `prior_form()`/`gate1()` shape; its `gate1()`'s CONFOUND section additionally checks
   `neutral_pass_rate vs offense_proe` (not just `vs shootout`/`margin`) — if that correlation is
   very high, the candidate is a redundant restatement of `pass_heavy`'s existing axis, not a new
   dimension, and that result goes in the writeup same as any other negative.

4. **`model/analysis/early_down_epa.py` — folded into `epa.py` above** (the `epa_early_down`
   field), not a separate module, since it's the same pass over the same columns.

5. **`model/analysis/series_success.py`** — team-week series-level success rate, reading the
   `series_success` column (already present per-play, carrying the same value for every play in a
   series) and `series` (to dedupe to one row per series rather than double-counting), grouped by
   `posteam`. Same shape as the above, with its own appropriately-sized `MIN_PLAYS`-equivalent
   floor (a team runs roughly 10-12 series a game, so the floor here is `MIN_SERIES = 8`, not a
   play count).

Each module's full code (for `cpoe.py`, `neutral_pass.py`, and `series_success.py`) follows the
exact same `team_weeks()`/`load()`/`prior_form()`/`corr()`/`gate1()`/`main()` shape demonstrated in
full for `epa.py` above — the implementation plan writes each one out completely; this spec
establishes the one shared shape all four non-EPA modules follow rather than repeating ~150 lines
of near-identical code four more times.

### 2. Running the gate1 checks (a research step, not a coded deliverable)

Once the five modules exist, each is run via
`python3 model/analysis/<module>.py --gate1 --seasons <available range>` (same invocation style as
`proe.py --gate1` and the Makefile's existing `targets-gate1` target). The implementation plan's
job is to build working, runnable modules; running them against real cached data and reading the
printed PERSISTENCE/CONFOUND/SIGNAL/SEPARATION numbers is empirical work whose outcome isn't
knowable in advance — the plan's "done" criterion for this phase is "the script runs cleanly and
prints real numbers against the full cached season range," not "the candidate validates."

### 3. Writing up results and (conditionally) shipping

For each candidate: write one `FINDINGS.md` entry in the same style as §19/§20/§21 (methodology,
numbers, verdict), whether the result is positive or negative. Only for a candidate whose
persistence, confound, and signal numbers look genuinely promising — a judgment call made by
reading the actual printed numbers against the bars §21 already established (persistence
meaningfully above the funnel-defense's failing r=0.124; non-redundant with what's already
shipped; a ΔR²/separation that isn't "statistically detectable and practically nil" the way §19's
targets proxy and §21's funnel measure both were) — proceed to the full `validate.py` measurement
(`evidence()`/`site_verdicts()`, same as every shipped scenario) and, if that also clears its
three-test bar, add one new `SCENARIOS` entry in `fit_conditionals.py`. This step is explicitly
out of this spec's committed scope (it depends on results this spec cannot know in advance) and
gets its own short follow-up plan once real numbers exist, same as how `blowout_loss`'s own
gating and any future scenario's addition already works.

## Testing

This codebase's `model/analysis/` layer has no existing pytest-style unit test file for `proe.py`,
`signals.py`, or `raoe.py` — these scripts are validated empirically, by the `--gate1` harness
itself run against real cached data, not by synthetic-fixture unit tests. This spec follows that
established convention rather than introducing a new testing style for only these five modules.
Each new module's own correctness check IS its `--gate1` run: a module with a bug in its column
indexing or aggregation will produce nonsensical PERSISTENCE/SIGNAL numbers immediately visible
against the known-good `proe.py --gate1` baseline (e.g. an r far outside [-1, 1], or a team-week
count wildly different from `proe.py`'s own ~7,200 for the same season range signals something is
wrong in the new module's parsing, not in the underlying data).

## Migration

None. Five new, additive modules; zero changes to `fit_conditionals.py`'s `SCENARIOS`,
`validate.py`, or the Go reader until and unless a specific candidate's measured results warrant
it, which is explicitly a follow-on decision this spec does not make.

# FORM Signal Mining Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build four new team-week FORM signal modules (EPA/play + early-down EPA, CPOE, neutral-script pass rate, series success rate) from already-cached play-by-play, each with a `--gate1` go/no-go check, mirroring the exact shape `proe.py`/`signals.py` already use.

**Architecture:** Each module is a small, self-contained Python file in `edge/model/analysis/`, following the established `team_weeks(season)` / `load(first, last)` / `prior_form(tw, min_prior, forecast_week)` / `corr(xs, ys)` / `gate1(first, last)` / `main(argv)` shape already used by `proe.py` and `signals.py`. No shared abstraction is introduced across the four modules — this codebase's own `model/analysis/` layer duplicates this shape file-to-file rather than factoring it out, and this plan follows that established convention rather than introducing a new one for only these four files.

**Tech Stack:** Python 3, stdlib only (`csv`, `gzip`, `statistics`, `argparse`) — matching every existing file in this directory, no new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-15-form-signal-mining-design.md`

## Global Constraints

- No pull from rbsdm.com or any external network source — every column used (`epa`, `cpoe`,
  `series_success`, `wp`, `down`, `play_type`) is already present in the cached
  `play_by_play_{season}.csv.gz` files.
- No opponent/matchup/conjunction scenarios — every signal in this plan is single-team only.
- No changes to `fit_conditionals.py`'s `SCENARIOS` dict, `validate.py`, or the Go reader
  (`app/internal/scenario/conditionals.go`) — adding a scenario is an explicitly out-of-scope,
  conditional follow-on this plan does not make.
- No pytest-style unit tests — this directory's existing modules (`proe.py`, `signals.py`,
  `raoe.py`) have none; each module's own `--gate1` run against real cached data is its
  correctness check, per this codebase's established convention.
- The house season window for this kind of measurement is `2009-2025` (matching
  `fit_conditionals.py:87`'s `FIRST, LAST = 2009, 2025` and `FINDINGS.md` §21's own reproduce
  command) — every `--gate1` run in this plan uses that range.
- Every module reads `play_by_play_{season}.csv.gz` by indexed column lookup (`header.index(c)`),
  not `csv.DictReader`, matching `proe.py`'s own stated performance rationale (a 372-column file;
  `DictReader` spends most of its time on columns nothing reads).

---

## Discovery notes (facts verified against the real code and cached data before writing this plan)

- `edge/model/analysis/proe.py` (311 lines, read in full) is the primary shape template:
  `MIN_PLAYS = 20`, `num(s)`, `seasons_available()`, `team_weeks(season)`, `load(first, last)`,
  `prior_form(tw, min_prior=3, forecast_week=None)`, `corr(xs, ys)`, `gate1(first, last)`,
  `main(argv)`.
- `edge/model/analysis/signals.py` (402 lines, read in full) is a SECOND, more directly
  analogous template — it already computes THREE named rates (`success_rate`, `chunk_rate`,
  `tempo`) from one pass over play-by-play, generalized via a `NAMES = ("success_rate",
  "chunk_rate", "tempo")` tuple threaded through `prior_form()` and `gate1()`. This plan's
  multi-field modules (`epa.py` has two fields) follow `signals.py`'s generalization pattern, not
  a single-field one.
- **Two existing precedents disagree on two points, and this plan picks one answer for both,
  applied consistently across all four new modules:**
  - **Regression target:** `proe.py`'s `gate1()` regresses against `o["output"]` (raw yards);
    `signals.py`'s `gate1()` regresses against `x["yards"]` (the baseline-normalized ratio
    `fit_conditionals.build()` itself documents as "THE FITTED VALUE" at `fit_conditionals.py`
    around line 794-800: *"a book sets its line near this player's own median, so a grid holding
    raw yards answers... when the question was 'what does HE do'"*). This plan uses `x["yards"]`
    (the ratio) for every new module's SIGNAL section, matching `build()`'s own stated preference
    and `signals.py`'s more recent convention.
  - **Separation basis:** `proe.py`'s SEPARATION tests the PRIOR-form quartiles (the actually
    forecastable, bettable quantity); `signals.py`'s SEPARATION tests REALIZED-form quartiles (a
    simpler best-case sanity check). This plan uses PRIOR-form quartiles for every new module,
    matching `proe.py`'s more rigorous choice — these new signals are candidate scenarios meant to
    be forecast before kickoff, so testing separation on data not available at bet time would
    overstate what is actually achievable.
- `fc.build(rows, games, outcome, proe_tw=None, signals_tw=None)` populates each observation's
  `"proe"`/`"success_rate"` fields from those optional dicts, but neither `proe.py` nor
  `signals.py`'s own `gate1()` actually reads those fields — both instead join their OWN
  `tw`/`pf` dicts onto `obs` AFTER calling `fc.build()`, keyed on `(season, week, team)`. This
  plan's four modules follow that same post-join pattern; none needs to pass its own data into
  `fc.build()`'s `proe_tw`/`signals_tw` parameters at all.
- `fc.OUTCOMES["receiving_yards"]` is the `Outcome` used by both existing `gate1()`s and is what
  every new module's SIGNAL section uses too (matching this plan's Global Constraints and the
  spec's stated non-goal of not testing every outcome up front).
- `fc.load_games()` returns `(season, week, team) -> (realized total, that team's signed margin,
  posted total)` — `games[k][0]` is the total (for the `shootout` CONFOUND check), `games[k][1]`
  is the margin (for the margin CONFOUND check).
- `proe.load(first, last)` is directly importable and is what every new module's CONFOUND section
  uses for the "vs already-shipped offense PROE" non-redundancy check — `signals.py`'s own
  `gate1()` already does exactly this (`proe_tw = proe.load(first, last)`, then
  `both = [(v[n], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]`), and this plan's four
  modules reuse that exact pattern for consistency, checking against ALL THREE of shootout,
  margin, and offense PROE (not just the two `proe.py`'s own `gate1()` checks) — the third check
  specifically guards against a new offense-side signal silently rediscovering `pass_heavy`.
- `from utilization_lag import ols_clustered` — `def ols_clustered(X: list[list[float]], y:
  list[float], groups: list[str])` (confirmed at `utilization_lag.py:179`), returns `(beta, se,
  r2)`. Used identically in both existing `gate1()`s and every new module's SIGNAL section.
- **The cached `play_by_play_2025.csv.gz` header was read directly (not assumed) to confirm every
  column this plan depends on actually exists:** `epa` (col 74), `down` (col 22), `cpoe` (col
  288), `series_success` (col 290), `series_result` (col 291), `wp` (col 93), `air_yards` (col
  39), `play_type` (present, used by `signals.py` already). **There is no bare `series` numeric
  ID column** — only `series_success`/`series_result` (both constant across every play inside one
  series, per nflfastR's standard encoding) and `drive`/`fixed_drive` (drive IDs). The spec's
  Design section assumed a `series` column exists for deduping to one row per series; it does
  not. **Correction applied in Task 4 below:** dedupe using `(season, week, posteam, drive)` as
  the series proxy key instead — a drive and a series are the same thing except for the rare case
  of a penalty resetting a set of downs mid-drive, which this plan accepts as a documented,
  deliberate approximation (the same kind of named, flagged simplification `signals.py`'s own
  `tempo` field already carries: "NAMED BADLY... kept under the name it shipped with, with the
  caveat attached").
- Cached season range confirmed present in this worktree: `play_by_play_2005.csv.gz` through
  `play_by_play_2026.csv.gz` — the `2009-2025` window this plan uses is fully covered.
- `fc.FIRST, fc.LAST = first, last` (module-global reassignment) is how both existing `gate1()`s
  point `fc.load_player_weeks()` at the requested season window before calling it — every new
  module's `gate1()` does the same.

---

## Task 1: `epa.py` — offensive EPA/play and early-down EPA

**Files:**
- Create: `edge/model/analysis/epa.py`

**Interfaces:**
- Consumes: `fit_conditionals.load_games()`, `fit_conditionals.OUTCOMES["receiving_yards"]`,
  `fit_conditionals.load_player_weeks()`, `fit_conditionals.build()`, `fit_conditionals.FIRST`/
  `.LAST`, `proe.load(first, last)` (for the CONFOUND-vs-PROE check), `utilization_lag.ols_clustered`.
- Produces: `team_weeks(season) -> dict[(season,week,team), {"epa_play": float,
  "epa_early_down": float | None, "plays": int, "early_down_plays": int}]`; `load(first, last) ->
  dict`; `prior_form(tw, min_prior=3, forecast_week=None) -> dict[(season,week,team),
  {"epa_play_prior": float, "epa_early_down_prior": float | None, "prior_games": int}]`;
  `gate1(first, last) -> int`; `main(argv) -> int`. Task 2-4's own modules do not call into this
  one — each is independent — but this is the shape every later task's module must match.

- [ ] **Step 1: Write `epa.py`**

```python
"""Team-week offensive EPA per play, from play-by-play.

EPA (Expected Points Added) is nflverse's own per-play value column -- no model
to run, just an aggregate. Two series, both per team-week:

    epa_play        mean EPA across all of a team's offensive scrimmage plays
                     (pass + rush).
    epa_early_down   the same mean, restricted to downs 1 and 2 -- early-down
                     efficiency without the third-and-long tail where offenses
                     are often just trying to convert rather than maximize value.

Unlike PROE, this is NOT adjusted for game script the way xpass is -- a team up
big late runs more clock-killing plays with lower EPA by design. The CONFOUND
check in gate1() exists specifically to see whether that leaks through as a
second measurement of `shootout`/margin, or as a redundant restatement of the
already-shipped `pass_heavy` (offense PROE) axis.

Each also comes in a prior-information-only form -- a team's EPA/play COMING
INTO a game, never including it -- because that is the only form usable to
forecast whether a scenario built on it will occur. Same discipline as
proe.prior_form and fit_conditionals.build.

What this file does NOT do is decide whether this is worth using as a scenario.
That is --gate1 here, then validate.py inside the fit, matching proe.py and
signals.py's own split of responsibility.
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

# A team runs roughly 60-65 offensive plays a game; EPA is defined on every
# scrimmage play (no xpass-style undefined cases the way PROE has kickoffs,
# punts and kneels drop out), so the floor sits closer to a full game's worth
# than PROE's own MIN_PLAYS=20.
MIN_PLAYS = 40

# Below this many early-down plays in a team-week, the early-down mean is
# describing a handful of snaps rather than a tendency -- dropped to None
# rather than reported on a noisy denominator.
MIN_EARLY_DOWN_PLAYS = 20


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
            # Scrimmage plays only, matching signals.py's success_rate scope --
            # kickoffs, punts and kneels are not a team choosing how to attack.
            if row[ix["play_type"]] not in ("pass", "run"):
                continue
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
            if down is not None and down in (1.0, 2.0):
                early_down[key].append(epa)

    out = {}
    for key, plays in all_plays.items():
        if len(plays) < MIN_PLAYS:
            continue
        early = early_down.get(key, [])
        out[key] = {
            "epa_play": st.mean(plays),
            "epa_early_down": st.mean(early) if len(early) >= MIN_EARLY_DOWN_PLAYS else None,
            "plays": len(plays),
            "early_down_plays": len(early),
        }
    return out


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if (CACHE / f"play_by_play_{season}.csv.gz").exists():
            out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3, forecast_week: int | None = None) -> dict[tuple[int, int, str], dict]:
    """A team's EPA/play coming into each game, from earlier games only.

    Never includes the game itself -- a predictor that contains the thing it
    predicts is not one. Mirrors proe.py's prior_form() exactly.
    """
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    def _prior_means(prior: list[dict]) -> dict:
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

    PERSISTENCE, CONFOUND (now against shootout, margin, AND the already-
    shipped offense PROE axis), SIGNAL, and SEPARATION -- the same shape as
    proe.py/signals.py's own gate1(), with this plan's two house choices
    applied: the SIGNAL regression target is x["yards"] (the ratio
    fit_conditionals.build() documents as the fitted quantity, not raw
    output), and SEPARATION uses the PRIOR-form quartiles (the actually
    forecastable quantity, not the realized one).
    """
    import fit_conditionals as fc
    import proe
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
    proe_tw = proe.load(first, last)
    rows = [(k, v) for k, v in tw.items() if k in games]
    print("\nCONFOUND  (against what the grid already conditions on)")
    vals = [v["epa_play"] for _, v in rows]
    shoot = [1.0 if games[k][0] > 50 else 0.0 for k, _ in rows]
    marg = [games[k][1] for k, _ in rows]
    both = [(v["epa_play"], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]
    pr = corr(*zip(*both)) if both else float("nan")
    print(f"  epa_play vs shootout indicator   r = {corr(vals, shoot):+.3f}")
    print(f"  epa_play vs team margin          r = {corr(vals, marg):+.3f}")
    print(f"  epa_play vs offense PROE         r = {pr:+.3f}")

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

    y = [o["yards"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["opportunity"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<32} {'beta':>10} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("own realized EPA/play", "epa_play"),
                       ("own PRIOR EPA/play (usable)", "epa_play_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["opportunity"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<32} {b[2]:>10.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals_sorted = sorted(o["epa_play_prior"] for o in J)
    p25, p75 = vals_sorted[len(vals_sorted) // 4], vals_sorted[3 * len(vals_sorted) // 4]
    hi = [o for o in J if o["epa_play_prior"] > p75]
    lo = [o for o in J if o["epa_play_prior"] < p25]
    print("\nSEPARATION  (top vs bottom PRIOR EPA/play quartile)")
    print(f"  top quartile cut p75 = {p75:+.3f}   bottom cut p25 = {p25:+.3f}")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["output"] > line) / len(hi)
        r = sum(1 for o in lo if o["output"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12; "
          "pass_heavy +0.14.")
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

Note the SEPARATION loop uses `o["output"]` (raw receiving yards) rather than `o["yards"]`
(the ratio) for the q/r threshold check specifically — this matches both existing precedents
(`proe.py` and `signals.py` both threshold against a real yardage LINE like 52.5 for this specific
check, since "does the player clear a real prop line" is the question, not "does his ratio clear
1.0"). Only the SIGNAL regression's own target changed to the ratio per this plan's house choice
above; SEPARATION's real-line framing is unchanged from both precedents.

- [ ] **Step 2: Run it against the full cached range and confirm it executes cleanly**

Run: `cd edge/model/analysis && python3 epa.py --gate1 --seasons 2009-2025`

Expected: no exceptions; prints team-week counts in the same rough order of magnitude as
`proe.py --gate1 --seasons 2009-2025`'s own printed count (both are built from the same
underlying play-by-play, filtered differently, so an order-of-magnitude match is the sanity
check — an exact match is not expected since `MIN_PLAYS` differs); every printed `r =` value
falls within `[-1.000, +1.000]`; the SIGNAL table prints three rows with real (non-NaN, non-
infinite) `beta`/`t`/`dR2` numbers; SEPARATION prints five real `q`/`r`/`q-r` rows.

- [ ] **Step 3: Also run the non-`--gate1` default path**

Run: `cd edge/model/analysis && python3 epa.py --seasons 2009-2025`

Expected: prints `offense EPA/play distribution` with a real mean/sd and five percentile rows,
no exceptions.

- [ ] **Step 4: Record the actual printed output in this task's notes** (for the follow-on
  write-up step this plan's spec defers — not graded here, just captured)

This is not a pass/fail gate — per this plan's Global Constraints, the done-criterion is "runs
cleanly and prints real numbers," not "the candidate validates." Save the full `--gate1` output
(e.g. redirect to `/tmp/epa_gate1.txt` or similar scratch location) so the eventual `FINDINGS.md`
write-up step the spec defers has the real numbers to quote, rather than needing to re-run this
later from memory.

- [ ] **Step 5: Commit**

```bash
git add edge/model/analysis/epa.py
git commit -m "model: add epa.py, offensive EPA/play + early-down EPA team-week series"
```

---

## Task 2: `cpoe.py` — completion percentage over expected

**Files:**
- Create: `edge/model/analysis/cpoe.py`

**Interfaces:**
- Consumes: same as Task 1 (`fit_conditionals`, `proe`, `utilization_lag.ols_clustered`).
- Produces: `team_weeks(season) -> dict[(season,week,team), {"cpoe": float, "attempts": int}]`;
  `load(first, last) -> dict`; `prior_form(tw, min_prior=3, forecast_week=None) ->
  dict[(season,week,team), {"cpoe_prior": float, "prior_games": int}]`; `gate1(first, last) ->
  int`; `main(argv) -> int`.

- [ ] **Step 1: Write `cpoe.py`**

```python
"""Team-week offensive completion percentage over expected, from play-by-play.

CPOE is nflverse's own per-pass-attempt value column: actual completion minus
the modelled completion probability given target depth, receiver separation
and pass-rush pressure. No model to run here, just an aggregate -- same shape
as proe.py's relationship to xpass.

One series, per team-week:

    cpoe   mean CPOE across a team's own pass attempts (sacks, scrambles and
           spikes carry no CPOE value and are excluded by the column itself
           being empty on those rows).

Comes in a prior-information-only form too -- a team's CPOE COMING INTO a
game, never including it -- same discipline as proe.prior_form.

What this file does NOT do is decide whether this is worth using as a
scenario. That is --gate1 here, then validate.py inside the fit.
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

# A team throws roughly 30-35 times a game; CPOE is only defined on pass
# attempts with a target (sacks/scrambles/spikes are blank), so the floor
# sits below a full game's pass volume to avoid dropping low-volume-passing
# team-weeks entirely.
MIN_ATTEMPTS = 15


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
    """(season, week, team) -> offensive CPOE for that team's pass attempts."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    acc = defaultdict(list)
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        try:
            ix = {c: header.index(c) for c in
                  ("season_type", "cpoe", "week", "posteam", "play_type")}
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")
        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            if row[ix["play_type"]] != "pass":
                continue
            cpoe = num(row[ix["cpoe"]])
            if cpoe is None:
                continue  # sacks, scrambles, spikes: no target, no CPOE value
            week = num(row[ix["week"]])
            posteam = row[ix["posteam"]].strip()
            if week is None or not posteam:
                continue
            acc[(season, int(week), posteam)].append(cpoe)

    return {
        k: {"cpoe": st.mean(v), "attempts": len(v)}
        for k, v in acc.items() if len(v) >= MIN_ATTEMPTS
    }


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if (CACHE / f"play_by_play_{season}.csv.gz").exists():
            out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3, forecast_week: int | None = None) -> dict[tuple[int, int, str], dict]:
    """A team's CPOE coming into each game, from earlier games only."""
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    out = {}
    for (season, team), games in by_team.items():
        games.sort()
        for i, (week, _) in enumerate(games):
            if i < min_prior:
                continue
            prior = [v for _, v in games[:i]]
            out[(season, week, team)] = {
                "cpoe_prior": st.mean(p["cpoe"] for p in prior), "prior_games": i,
            }
        if forecast_week is not None:
            prior = [v for w, v in games if w < forecast_week]
            if len(prior) >= min_prior:
                out[(season, forecast_week, team)] = {
                    "cpoe_prior": st.mean(p["cpoe"] for p in prior),
                    "prior_games": len(prior),
                }
    return out


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is there signal here worth the full validate.py measurement?"""
    import fit_conditionals as fc
    import proe
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior CPOE vs realized; near zero = unforecastable)")
    pairs = [(pf[k]["cpoe_prior"], tw[k]["cpoe"]) for k in pf if k in tw]
    print(f"  cpoe  r = {corr(*zip(*pairs)):+.3f}   n={len(pairs)}")

    games = fc.load_games()
    proe_tw = proe.load(first, last)
    rows = [(k, v) for k, v in tw.items() if k in games]
    print("\nCONFOUND  (against what the grid already conditions on)")
    vals = [v["cpoe"] for _, v in rows]
    shoot = [1.0 if games[k][0] > 50 else 0.0 for k, _ in rows]
    marg = [games[k][1] for k, _ in rows]
    both = [(v["cpoe"], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]
    pr = corr(*zip(*both)) if both else float("nan")
    print(f"  cpoe vs shootout indicator   r = {corr(vals, shoot):+.3f}")
    print(f"  cpoe vs team margin          r = {corr(vals, marg):+.3f}")
    print(f"  cpoe vs offense PROE         r = {pr:+.3f}")

    fc.FIRST, fc.LAST = first, last
    outcome = fc.OUTCOMES["receiving_yards"]
    obs = fc.build(fc.load_player_weeks(outcome)[0], games, outcome)
    J = []
    for o in obs:
        k = (o["season"], o["week"], o["team"])
        if k in tw and k in pf:
            o = dict(o)
            o["cpoe"], o["cpoe_prior"] = tw[k]["cpoe"], pf[k]["cpoe_prior"]
            J.append(o)

    y = [o["yards"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["opportunity"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<28} {'beta':>10} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("own realized CPOE", "cpoe"),
                       ("own PRIOR CPOE (usable)", "cpoe_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["opportunity"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<28} {b[2]:>10.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals_sorted = sorted(o["cpoe_prior"] for o in J)
    p25, p75 = vals_sorted[len(vals_sorted) // 4], vals_sorted[3 * len(vals_sorted) // 4]
    hi = [o for o in J if o["cpoe_prior"] > p75]
    lo = [o for o in J if o["cpoe_prior"] < p25]
    print("\nSEPARATION  (top vs bottom PRIOR CPOE quartile)")
    print(f"  top quartile cut p75 = {p75:+.3f}   bottom cut p25 = {p25:+.3f}")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["output"] > line) / len(hi)
        r = sum(1 for o in lo if o["output"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12; "
          "pass_heavy +0.14.")
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
    print(f"team-weeks with usable CPOE ({first}-{last}): {len(tw)}\n")
    vals = sorted(v["cpoe"] for v in tw.values())
    pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
    print("offensive CPOE distribution")
    print(f"  mean {st.mean(vals):+.4f}  sd {st.pstdev(vals):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {pct(p):+.4f}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

- [ ] **Step 2: Run it against the full cached range and confirm it executes cleanly**

Run: `cd edge/model/analysis && python3 cpoe.py --gate1 --seasons 2009-2025`

Expected: no exceptions; every printed `r =` value within `[-1.000, +1.000]`; SIGNAL table prints
three real rows; SEPARATION prints five real rows. Note nflverse's `cpoe` column coverage starts
partway through the 2009-2025 window in some seasons (the completion-probability model was
introduced after some earlier seasons were played) — if the team-week count is noticeably lower
than `epa.py`'s or `proe.py`'s own count for the same range, that is expected and not a bug;
record the actual count rather than assuming parity.

- [ ] **Step 3: Also run the non-`--gate1` default path**

Run: `cd edge/model/analysis && python3 cpoe.py --seasons 2009-2025`

Expected: prints the CPOE distribution with a real mean/sd and five percentile rows, no
exceptions.

- [ ] **Step 4: Record the actual printed output** (same scratch-capture note as Task 1 Step 4)

- [ ] **Step 5: Commit**

```bash
git add edge/model/analysis/cpoe.py
git commit -m "model: add cpoe.py, offensive completion % over expected team-week series"
```

---

## Task 3: `neutral_pass.py` — neutral-script pass rate

**Files:**
- Create: `edge/model/analysis/neutral_pass.py`

**Interfaces:**
- Consumes: same as Task 1.
- Produces: `team_weeks(season) -> dict[(season,week,team), {"neutral_pass_rate": float,
  "plays": int}]`; `load(first, last) -> dict`; `prior_form(tw, min_prior=3, forecast_week=None)
  -> dict[(season,week,team), {"neutral_pass_rate_prior": float, "prior_games": int}]`;
  `gate1(first, last) -> int`; `main(argv) -> int`.

- [ ] **Step 1: Write `neutral_pass.py`**

```python
"""Team-week neutral-script pass rate, from play-by-play.

Where proe.py divides game script out of pass rate with a model (xpass), this
divides it out with a filter instead: restrict to plays where the situation
itself is not yet dictating the decision -- down 1 or 2, win probability
between 20% and 80% -- and take the RAW pass rate inside that slice. If
situational filtering is a cleaner proxy for coaching pass tendency than
PROE's model-based adjustment, this should persist at least as well; if it is
mostly redundant with the already-shipped offense_proe axis, the CONFOUND
check in gate1() against PROE directly says so.

One series, per team-week:

    neutral_pass_rate   fraction of a team's neutral-script, early-down plays
                        that were passes.

Comes in a prior-information-only form too -- a team's rate COMING INTO a
game, never including it -- same discipline as proe.prior_form.

What this file does NOT do is decide whether this is worth using as a
scenario. That is --gate1 here, then validate.py inside the fit.
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

# A neutral-script, early-down slice is a fraction of a team's ~60-65 total
# offensive plays -- most 1st/2nd-down snaps happen inside wp 0.20-0.80 in a
# typical game, but a blowout can shrink this heavily. Floor set lower than
# epa.py's MIN_PLAYS for that reason.
MIN_PLAYS = 15


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
    """(season, week, team) -> raw pass rate on neutral-script, early-down plays."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    acc = defaultdict(lambda: {"n": 0, "pass": 0})
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        try:
            ix = {c: header.index(c) for c in
                  ("season_type", "week", "posteam", "play_type", "down", "wp")}
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")
        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            if row[ix["play_type"]] not in ("pass", "run"):
                continue
            down = num(row[ix["down"]])
            wp = num(row[ix["wp"]])
            if down is None or down not in (1.0, 2.0):
                continue
            if wp is None or not (0.20 <= wp <= 0.80):
                continue
            week = num(row[ix["week"]])
            team = row[ix["posteam"]].strip()
            if week is None or not team:
                continue
            a = acc[(season, int(week), team)]
            a["n"] += 1
            a["pass"] += 1 if row[ix["play_type"]] == "pass" else 0

    return {
        k: {"neutral_pass_rate": v["pass"] / v["n"], "plays": v["n"]}
        for k, v in acc.items() if v["n"] >= MIN_PLAYS
    }


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if (CACHE / f"play_by_play_{season}.csv.gz").exists():
            out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3, forecast_week: int | None = None) -> dict[tuple[int, int, str], dict]:
    """A team's neutral-script pass rate coming into each game, from earlier games only."""
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    out = {}
    for (season, team), games in by_team.items():
        games.sort()
        for i, (week, _) in enumerate(games):
            if i < min_prior:
                continue
            prior = [v for _, v in games[:i]]
            out[(season, week, team)] = {
                "neutral_pass_rate_prior": st.mean(p["neutral_pass_rate"] for p in prior),
                "prior_games": i,
            }
        if forecast_week is not None:
            prior = [v for w, v in games if w < forecast_week]
            if len(prior) >= min_prior:
                out[(season, forecast_week, team)] = {
                    "neutral_pass_rate_prior": st.mean(p["neutral_pass_rate"] for p in prior),
                    "prior_games": len(prior),
                }
    return out


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is there signal here worth the full validate.py measurement?"""
    import fit_conditionals as fc
    import proe
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior neutral pass rate vs realized; near zero = unforecastable)")
    pairs = [(pf[k]["neutral_pass_rate_prior"], tw[k]["neutral_pass_rate"])
             for k in pf if k in tw]
    print(f"  neutral_pass_rate  r = {corr(*zip(*pairs)):+.3f}   n={len(pairs)}")

    games = fc.load_games()
    proe_tw = proe.load(first, last)
    rows = [(k, v) for k, v in tw.items() if k in games]
    print("\nCONFOUND  (against what the grid already conditions on)")
    vals = [v["neutral_pass_rate"] for _, v in rows]
    shoot = [1.0 if games[k][0] > 50 else 0.0 for k, _ in rows]
    marg = [games[k][1] for k, _ in rows]
    both = [(v["neutral_pass_rate"], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]
    pr = corr(*zip(*both)) if both else float("nan")
    print(f"  neutral_pass_rate vs shootout indicator   r = {corr(vals, shoot):+.3f}")
    print(f"  neutral_pass_rate vs team margin          r = {corr(vals, marg):+.3f}")
    print(f"  neutral_pass_rate vs offense PROE         r = {pr:+.3f}   "
          f"(high here means this is mostly a restatement of pass_heavy, not a new axis)")

    fc.FIRST, fc.LAST = first, last
    outcome = fc.OUTCOMES["receiving_yards"]
    obs = fc.build(fc.load_player_weeks(outcome)[0], games, outcome)
    J = []
    for o in obs:
        k = (o["season"], o["week"], o["team"])
        if k in tw and k in pf:
            o = dict(o)
            o["neutral_pass_rate"] = tw[k]["neutral_pass_rate"]
            o["neutral_pass_rate_prior"] = pf[k]["neutral_pass_rate_prior"]
            J.append(o)

    y = [o["yards"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["opportunity"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<36} {'beta':>10} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("own realized neutral pass rate", "neutral_pass_rate"),
                       ("own PRIOR neutral pass rate (usable)", "neutral_pass_rate_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["opportunity"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<36} {b[2]:>10.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals_sorted = sorted(o["neutral_pass_rate_prior"] for o in J)
    p25, p75 = vals_sorted[len(vals_sorted) // 4], vals_sorted[3 * len(vals_sorted) // 4]
    hi = [o for o in J if o["neutral_pass_rate_prior"] > p75]
    lo = [o for o in J if o["neutral_pass_rate_prior"] < p25]
    print("\nSEPARATION  (top vs bottom PRIOR neutral pass rate quartile)")
    print(f"  top quartile cut p75 = {p75:+.3f}   bottom cut p25 = {p25:+.3f}")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["output"] > line) / len(hi)
        r = sum(1 for o in lo if o["output"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12; "
          "pass_heavy +0.14.")
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
    print(f"team-weeks with usable neutral pass rate ({first}-{last}): {len(tw)}\n")
    vals = sorted(v["neutral_pass_rate"] for v in tw.values())
    pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
    print("neutral-script pass rate distribution")
    print(f"  mean {st.mean(vals):.4f}  sd {st.pstdev(vals):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {pct(p):.4f}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

- [ ] **Step 2: Run it against the full cached range and confirm it executes cleanly**

Run: `cd edge/model/analysis && python3 neutral_pass.py --gate1 --seasons 2009-2025`

Expected: no exceptions; every printed `r =` value within `[-1.000, +1.000]` (including the
`vs offense PROE` line, which is the specific non-redundancy check this candidate exists to
answer); SIGNAL table prints three real rows; SEPARATION prints five real rows.

- [ ] **Step 3: Also run the non-`--gate1` default path**

Run: `cd edge/model/analysis && python3 neutral_pass.py --seasons 2009-2025`

Expected: prints the neutral-script pass rate distribution with a real mean/sd and five
percentile rows, no exceptions.

- [ ] **Step 4: Record the actual printed output** (same scratch-capture note as Task 1 Step 4)

- [ ] **Step 5: Commit**

```bash
git add edge/model/analysis/neutral_pass.py
git commit -m "model: add neutral_pass.py, neutral-script early-down pass rate team-week series"
```

---

## Task 4: `series_success.py` — series-level success rate

**Files:**
- Create: `edge/model/analysis/series_success.py`

**Interfaces:**
- Consumes: same as Task 1.
- Produces: `team_weeks(season) -> dict[(season,week,team), {"series_success_rate": float,
  "series": int}]`; `load(first, last) -> dict`; `prior_form(tw, min_prior=3,
  forecast_week=None) -> dict[(season,week,team), {"series_success_rate_prior": float,
  "prior_games": int}]`; `gate1(first, last) -> int`; `main(argv) -> int`.

**Note on a spec correction** (see this plan's Discovery Notes): the cached play-by-play has no
bare `series` ID column, only `series_success`/`series_result` (constant across every play inside
one series) and `drive`/`fixed_drive`. This task dedupes using `(season, week, posteam, drive)` as
the series proxy key instead of a dedicated series ID, accepting the known, documented
approximation that a drive and a series are usually but not always the same thing (a mid-drive
penalty can reset a set of downs into a new series without starting a new drive). This is called
out explicitly in the module's own docstring, matching `signals.py`'s own precedent for a
similarly-approximate field (`tempo`).

- [ ] **Step 1: Write `series_success.py`**

```python
"""Team-week series-level success rate, from play-by-play.

A "series" in nflverse's sense is a set of downs -- did the offense get a new
set (first down or touchdown) before the set ran out. `series_success` is
already a per-play column (constant across every play inside one series);
this file reduces that to one row per series and aggregates to a team-week
rate. Different unit than signals.py's `success_rate` (which is a PLAY-level
rate, EPA > 0 on each individual play) -- this measures whether a team
SUSTAINS drives, not whether any one play was positive value.

Known approximation: nflverse's cached play-by-play carries no dedicated
series ID column, only `series_success`/`series_result` (both constant across
a series) and `drive`/`fixed_drive` (drive IDs). This file dedupes on
(season, week, team, drive) as a proxy for series -- a drive and a series are
the same thing except for the rare case of a penalty resetting a set of downs
mid-drive, which this measure cannot distinguish. Kept as a documented,
accepted simplification rather than blocking on finding/deriving a true series
ID, the same way signals.py's `tempo` field is kept under an acknowledged
imprecise name rather than redesigned.

One series (pun unavoidable), per team-week:

    series_success_rate   fraction of a team's own series/drives that ended
                          in a first down or touchdown.

Comes in a prior-information-only form too -- a team's rate COMING INTO a
game, never including it -- same discipline as proe.prior_form.

What this file does NOT do is decide whether this is worth using as a
scenario. That is --gate1 here, then validate.py inside the fit.
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

# A team runs roughly 10-12 drives a game -- this floor is a SERIES count, not
# a play count, unlike every other module in this directory.
MIN_SERIES = 8


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
    """(season, week, team) -> fraction of drives ending in a first down or TD."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    # (season, week, team, drive) -> the series_success value seen for it.
    # Every play in a drive carries the same value, so the first one seen is
    # as good as any -- this is a dict precisely to dedupe without needing to
    # sort or track "have I seen this drive" separately.
    by_drive: dict[tuple[int, int, str, str], float] = {}
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        try:
            ix = {c: header.index(c) for c in
                  ("season_type", "week", "posteam", "play_type",
                   "series_success", "drive")}
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")
        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            if row[ix["play_type"]] not in ("pass", "run"):
                continue
            succ = num(row[ix["series_success"]])
            if succ is None:
                continue
            week = num(row[ix["week"]])
            team = row[ix["posteam"]].strip()
            drive = row[ix["drive"]].strip()
            if week is None or not team or not drive:
                continue
            by_drive.setdefault((season, int(week), team, drive), succ)

    acc = defaultdict(lambda: {"n": 0, "success": 0})
    for (season, week, team, _drive), succ in by_drive.items():
        a = acc[(season, week, team)]
        a["n"] += 1
        a["success"] += 1 if succ > 0 else 0

    return {
        k: {"series_success_rate": v["success"] / v["n"], "series": v["n"]}
        for k, v in acc.items() if v["n"] >= MIN_SERIES
    }


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if (CACHE / f"play_by_play_{season}.csv.gz").exists():
            out.update(team_weeks(season))
    return out


def prior_form(tw: dict, min_prior: int = 3, forecast_week: int | None = None) -> dict[tuple[int, int, str], dict]:
    """A team's series success rate coming into each game, from earlier games only."""
    by_team = defaultdict(list)
    for (season, week, team), v in tw.items():
        by_team[(season, team)].append((week, v))

    out = {}
    for (season, team), games in by_team.items():
        games.sort()
        for i, (week, _) in enumerate(games):
            if i < min_prior:
                continue
            prior = [v for _, v in games[:i]]
            out[(season, week, team)] = {
                "series_success_rate_prior": st.mean(p["series_success_rate"] for p in prior),
                "prior_games": i,
            }
        if forecast_week is not None:
            prior = [v for w, v in games if w < forecast_week]
            if len(prior) >= min_prior:
                out[(season, forecast_week, team)] = {
                    "series_success_rate_prior": st.mean(
                        p["series_success_rate"] for p in prior
                    ),
                    "prior_games": len(prior),
                }
    return out


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is there signal here worth the full validate.py measurement?"""
    import fit_conditionals as fc
    import proe
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior series success rate vs realized; near zero = unforecastable)")
    pairs = [(pf[k]["series_success_rate_prior"], tw[k]["series_success_rate"])
             for k in pf if k in tw]
    print(f"  series_success_rate  r = {corr(*zip(*pairs)):+.3f}   n={len(pairs)}")

    games = fc.load_games()
    proe_tw = proe.load(first, last)
    rows = [(k, v) for k, v in tw.items() if k in games]
    print("\nCONFOUND  (against what the grid already conditions on)")
    vals = [v["series_success_rate"] for _, v in rows]
    shoot = [1.0 if games[k][0] > 50 else 0.0 for k, _ in rows]
    marg = [games[k][1] for k, _ in rows]
    both = [(v["series_success_rate"], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]
    pr = corr(*zip(*both)) if both else float("nan")
    print(f"  series_success_rate vs shootout indicator   r = {corr(vals, shoot):+.3f}")
    print(f"  series_success_rate vs team margin          r = {corr(vals, marg):+.3f}")
    print(f"  series_success_rate vs offense PROE         r = {pr:+.3f}")

    fc.FIRST, fc.LAST = first, last
    outcome = fc.OUTCOMES["receiving_yards"]
    obs = fc.build(fc.load_player_weeks(outcome)[0], games, outcome)
    J = []
    for o in obs:
        k = (o["season"], o["week"], o["team"])
        if k in tw and k in pf:
            o = dict(o)
            o["series_success_rate"] = tw[k]["series_success_rate"]
            o["series_success_rate_prior"] = pf[k]["series_success_rate_prior"]
            J.append(o)

    y = [o["yards"] for o in J]
    g = [o["player"] for o in J]
    _, _, r2b = ols_clustered([[1.0, o["opportunity"]] for o in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<40} {'beta':>10} {'t':>7} {'dR2':>10}   verdict")
    for label, key in (("own realized series success rate", "series_success_rate"),
                       ("own PRIOR series success rate (usable)", "series_success_rate_prior"),
                       ("role trend, for comparison", "trend")):
        b, se, r2 = ols_clustered([[1.0, o["opportunity"], o[key]] for o in J], y, g)
        t = b[2] / se[2] if se[2] > 0 else 0.0
        print(f"  {label:<40} {b[2]:>10.3f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
              f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")

    vals_sorted = sorted(o["series_success_rate_prior"] for o in J)
    p25, p75 = vals_sorted[len(vals_sorted) // 4], vals_sorted[3 * len(vals_sorted) // 4]
    hi = [o for o in J if o["series_success_rate_prior"] > p75]
    lo = [o for o in J if o["series_success_rate_prior"] < p25]
    print("\nSEPARATION  (top vs bottom PRIOR series success rate quartile)")
    print(f"  top quartile cut p75 = {p75:.3f}   bottom cut p25 = {p25:.3f}")
    print(f"  {'line':>6} {'q':>8} {'r':>8} {'q-r':>8}")
    for line in (24.5, 40.5, 52.5, 75.5, 100.5):
        q = sum(1 for o in hi if o["output"] > line) / len(hi)
        r = sum(1 for o in lo if o["output"] > line) / len(lo)
        print(f"  {line:>6} {q:>8.3f} {r:>8.3f} {q - r:>+8.3f}")
    print("  shootout's q-r at 52.5 in the shipped grid runs about +0.09 to +0.12; "
          "pass_heavy +0.14.")
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
    print(f"team-weeks with usable series success rate ({first}-{last}): {len(tw)}\n")
    vals = sorted(v["series_success_rate"] for v in tw.values())
    pct = lambda p: vals[min(int(p * len(vals)), len(vals) - 1)]
    print("series success rate distribution")
    print(f"  mean {st.mean(vals):.4f}  sd {st.pstdev(vals):.4f}")
    for p in (0.10, 0.25, 0.50, 0.75, 0.90):
        print(f"  p{int(p*100):<3} {pct(p):.4f}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

- [ ] **Step 2: Run it against the full cached range and confirm it executes cleanly**

Run: `cd edge/model/analysis && python3 series_success.py --gate1 --seasons 2009-2025`

Expected: no exceptions; team-week count in the same rough order of magnitude as the other three
new modules (it floors on series count rather than play count, so an exact match is not expected);
every printed `r =` value within `[-1.000, +1.000]`; SIGNAL table prints three real rows;
SEPARATION prints five real rows.

- [ ] **Step 3: Also run the non-`--gate1` default path**

Run: `cd edge/model/analysis && python3 series_success.py --seasons 2009-2025`

Expected: prints the series success rate distribution with a real mean/sd and five percentile
rows, no exceptions.

- [ ] **Step 4: Record the actual printed output** (same scratch-capture note as Task 1 Step 4)

- [ ] **Step 5: Commit**

```bash
git add edge/model/analysis/series_success.py
git commit -m "model: add series_success.py, series-level success rate team-week series"
```

---

## Final verification

- [ ] **Spec coverage check** — cross-reference
  `docs/superpowers/specs/2026-10-15-form-signal-mining-design.md` section by section:
  - Design §1, modules 1-5 (EPA/early-down-EPA folded together, CPOE, neutral pass rate, series
    success) — Tasks 1-4, one module each (early-down EPA folded into Task 1's `epa.py` exactly
    as the spec itself directs).
  - Design §2 (running gate1 as a research step, not a coded deliverable) — every task's Step 2/3
    runs the real script against real cached data; no task claims a candidate "validates" or
    "ships," matching the spec's explicit framing.
  - Design §3 (writing up results, conditional shipping) — explicitly out of this plan's scope,
    same as the spec states; Step 4 of every task captures the real output so that follow-on step
    has real numbers to draw from.
  - Testing section (no pytest, `--gate1` against real data is the correctness check) — every
    task's Step 2/3 is exactly that; no synthetic fixtures anywhere in this plan.
  - Migration (none) — confirmed; no task touches `SCENARIOS`, `validate.py`, or the Go reader.
  All covered.

- [ ] **Placeholder scan** — every task's code block is complete, runnable Python with no
  `TBD`/`TODO`/`pass  # implement later` anywhere; confirmed by re-reading all four full module
  listings above.

- [ ] **Type/shape consistency** — every module's `team_weeks()`/`load()`/`prior_form()`/
  `gate1()`/`main()` signatures match the Interfaces block stated at the top of its own task; the
  two house choices (ratio-based SIGNAL target via `o["yards"]`, PRIOR-form SEPARATION) are
  applied identically across all four modules' `gate1()` functions; every module's CONFOUND
  section checks the same three things (shootout, margin, offense PROE) for consistency across
  candidates.

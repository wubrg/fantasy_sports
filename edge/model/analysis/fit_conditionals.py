#!/usr/bin/env python3
"""Fit P(receiving yards > L | opportunity, role trend, game script).

This produces the q and r that the scenario decomposition needs:

    q = P(hit | scenario)      r = P(hit | no scenario)

Until now both were operator guesses. A per-player estimate cannot supply them
-- 17 games is nowhere near enough -- but pooling across player-games with
similar opportunity gives cells with hundreds of observations each, which is
exactly the sample size the whole exercise has been short of.

# Why these three axes

    projected targets   The Late-Round thesis is volume over efficiency, and
                        every measurement in this project has agreed: targets
                        drive yards far more than per-target skill does.

    role trend          utilization_lag.py established that a two-game share
                        trend carries information season-long share does not,
                        but that only a LARGE change is worth acting on. A grid
                        that averaged over trend levels would wash that out, so
                        it gets its own axis with the boundary at the measured
                        threshold (+6 share points).

    game script         The axis that separates q from r. Without it there is
                        no scenario decomposition, only a base rate.

# Counting, not regression

Each cell stores the empirical distribution of yards as a quantile table, so
P(yards > L) is a lookup at any line rather than a fit at one. A thin cell
reports itself through its n, and the Go side turns that into a Wilson interval
at query time using the same code the hit-rate layer already uses. No
distributional assumption anywhere.

Dependency-free: stdlib only.

Usage:
    python3 fit_conditionals.py
    python3 fit_conditionals.py --report      # cell table, write nothing
"""

from __future__ import annotations

import argparse
import csv
import itertools
import json
import statistics as st
import sys
import time

import validate
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "data" / "raw"
MANIFEST = CACHE / "manifest.json"
ARTIFACT = ROOT.parent / "app" / "internal" / "scenario" / "artifacts" / "conditionals.json"

# stats_player_week carries target_share only from 2009 and air yards from 2006,
# but targets reach back to 2005. (This fit computes share from raw targets
# rather than reading the column, so 2009 is not a hard floor for it.)
#
# Was 2014, chosen to match the residual fit's recency preference rather than to
# maximise n. Extended to 2009 to publish the cells that matter most and were
# missing: 8-11 projected targets with a rising role -- high volume plus climbing
# usage, which is the corpus's "usage vacuum", the strongest edge it claims, and
# the one situation the grid could not price. That cell held 97 observations
# against a floor of 100.
#
# The trade is era dilution, and it was measured rather than assumed before being
# taken. Extending keeps the effect: shootout goes from 15/15 to 16/16 cells
# consistent and 14/14 to 15/15 out of sample, with resolution improving from
# 11/15 to 12/16. Cells that already published move by at most 2.5 points of
# P(>52.5), most by under one. validate.py re-checks all of this on every fit and
# fails if it stops holding.
#
# What was NOT done, and why: merging 8-11 with 11+ would have published the same
# cell without new data, but the bands do not behave alike -- at a 100.5 line the
# 11+ band's NO-shootout rate (0.289) exceeds 8-11's WITH-shootout rate (0.277),
# so pooling understates a 12-target projection by ~11 points on q. Lowering
# MIN_CELL would have cleared a three-observation shortfall by weakening the floor
# everywhere.
FIRST, LAST = 2009, 2025
POSITIONS = {"WR", "TE", "RB"}


class Outcome:
    """What is being predicted, and the opportunity axis it is conditioned on.

    The axis is not interchangeable between outcomes, which is why this exists
    rather than a `stat` argument. A pass-catcher's opportunity is a SHARE: he
    competes for a fixed pool of team targets, so his baseline is a share of it
    and a change in that share is the role change the trend axis measures. A
    quarterback has no share -- he takes essentially all of his team's attempts
    -- so his opportunity is his own prior attempt volume and his trend is a
    change in that volume.

    Treating the two the same would put a QB's raw attempts through a
    share-shaped model and produce bands that mean nothing.
    """

    def __init__(self, name, yards_field, opp_field, positions, share_based,
                 bands, trend_bands, min_baseline, baseline_bands, min_output,
                 posted_bands=None, discrete=False, unit="yds"):
        self.name = name
        self.yards_field = yards_field
        self.opp_field = opp_field
        self.positions = positions
        self.share_based = share_based
        self.bands = bands
        self.trend_bands = trend_bands
        self.min_baseline = min_baseline
        # Bands over the player's own prior mean OUTPUT -- the quantity a book
        # sets its line near. Conditioning on it is what makes q a probability
        # about THIS player rather than about the cohort he was pooled with;
        # see FINDINGS.md 11.
        self.baseline_bands = baseline_bands
        # Below this the ratio is not a stable quantity: a 3-yard baseline
        # turns an ordinary game into a ratio of 8.
        self.min_output = min_output
        # The POSTED total, which `s` is derived from. An outcome whose
        # population cannot fill the split uses one band covering everything,
        # which is the same grid with the axis switched off.
        self.posted_bands = posted_bands or POSTED_BANDS
        # A count rather than a measurement. Changes how the cell's
        # distribution is stored and how a half-integer line is read off it.
        self.discrete = discrete
        # What the cell medians are counted in. Printing receptions as "yds"
        # is a small lie of exactly the kind this grid keeps finding.
        self.unit = unit

    def axes(self) -> list:
        """The grid's conditioning axes, in cell-key order.

        Projected opportunity is NOT among them any more. It was the original
        axis, and once the value being fitted is a ratio to the player's own
        baseline, that baseline carries most of what projected opportunity was
        standing in for -- while costing a third of the density it used to.
        """
        return [
            ("posted_total", self.posted_bands),
            ("baseline_yards", self.baseline_bands),
            ("trend", self.trend_bands),
        ]

    def as_json(self) -> dict:
        return {
            "yards_field": self.yards_field,
            "opportunity": self.opp_field,
            "share_based": self.share_based,
            "positions": sorted(self.positions),
            "bands": [list(b) for b in self.bands],
            "trend_bands": [list(b) for b in self.trend_bands],
            "baseline_bands": [list(b) for b in self.baseline_bands],
            "posted_bands": [list(b) for b in self.posted_bands],
            "min_baseline": self.min_baseline,
            "min_output": self.min_output,
            "discrete": self.discrete,
            "unit": self.unit,
        }

# The posted total, split at the median game. Two bands rather than three:
# measured, three publishes more cells but the third is thin enough that its
# sites fail resolution anyway. See FINDINGS.md 11.
POSTED_BANDS = [(0, 46), (46, 999)]

MIN_PRIOR_GAMES = 4
TREND_WINDOW = 2
MIN_BASELINE_SHARE = 0.05

# Bands over the player's own prior mean output. Cut near the quartiles of each
# outcome's population, so a tier holds players a book would price similarly.
BASELINE_YARD_BANDS = [(0, 35), (35, 50), (50, 70), (70, 999)]
BASELINE_RECEPTION_BANDS = [(0, 2.5), (2.5, 4), (4, 5.5), (5.5, 99)]
BASELINE_RUSH_BANDS = [(0, 30), (30, 55), (55, 80), (80, 999)]
BASELINE_PASS_BANDS = [(0, 200), (200, 240), (240, 275), (275, 999)]
MIN_CELL = 100  # below this a cell is dropped rather than published thin

# Projected targets. Boundaries follow the natural break points of usage --
# rotational, complementary, starter, focal, alpha.
TARGET_BANDS = [(0, 4), (4, 6), (6, 8), (8, 11), (11, 999)]

# Attempt bands for passing. Coarser than the target bands on purpose: there
# are ~32 starting quarterbacks in a week against ~237 pass-catchers, a
# structural ceiling no amount of fetching moves, so the same five-band grid
# would drop most cells. Measured at 4x3, 18 of 24 cells publish.
#
# The sub-28 band is expected to drop and that is the right outcome: those are
# backup quarterbacks and injury exits, not a band anyone prices a prop in.
ATTEMPT_BANDS = [(0, 28), (28, 33), (33, 38), (38, 999)]

# Attempt trend, in attempts rather than share points. The +/-2 boundary is not
# the +6-share-point actionability threshold borrowed from utilization_lag --
# that measurement is about target share and says nothing about QB volume. It
# is a starting split from the distribution, and it needs its own measurement
# before anything leans on it.
ATTEMPT_TREND_BANDS = [(-99.0, -2.0), (-2.0, 2.0), (2.0, 99.0)]

# Role trend. The +0.06 boundary is the measured actionability threshold from
# utilization_lag.py: below it the effect cannot clear the vig.
TREND_BANDS = [(-99.0, -0.03), (-0.03, 0.03), (0.03, 0.06), (0.06, 99.0)]

# Rushing bands, measured rather than borrowed. Projected carries run p25=4.2,
# p50=8.4, p75=13.7, so these are roughly quartiles of the real distribution.
CARRY_BANDS = [(0, 5), (5, 10), (10, 15), (15, 999)]

# Carry-share trend. The +6-share-point target threshold does NOT transfer:
# carry-share trend has an sd of 0.131 against target share's ~0.05, because
# backfields consolidate and split far more sharply than target trees do. These
# sit at the same multiples of their own sd that the target bands sit at in
# theirs (+/-0.6 sd and +1.2 sd), which puts +8 points at 3.9 rushing yards and
# +16 at 7.8.
#
# Measured, not assumed: RB rushing yards on baseline carry share plus trend
# gives the trend beta = 48.74 (t = 18.25, dR2 = +0.0268) with errors clustered
# by player -- an order of magnitude more explanatory power than the target-share
# trend manages for receiving yards (+0.0032). On carries themselves it is
# stronger still (dR2 = +0.0418), which is the volume-over-efficiency thesis
# showing up where it should.
CARRY_TREND_BANDS = [(-99.0, -0.08), (-0.08, 0.08), (0.08, 0.16), (0.16, 99.0)]

OUTCOMES = {
    "receiving_yards": Outcome(
        "receiving_yards", "receiving_yards", "targets", {"WR", "TE", "RB"},
        share_based=True, bands=TARGET_BANDS, trend_bands=TREND_BANDS,
        min_baseline=MIN_BASELINE_SHARE,
        baseline_bands=BASELINE_YARD_BANDS, min_output=5.0,
    ),
    "receptions": Outcome(
        "receptions", "receptions", "targets", {"WR", "TE", "RB"},
        share_based=True, bands=TARGET_BANDS, trend_bands=TREND_BANDS,
        min_baseline=MIN_BASELINE_SHARE,
        baseline_bands=BASELINE_RECEPTION_BANDS, min_output=1.0,
        # The same rows and the same opportunity axis as receiving yards --
        # only the outcome column differs. What changes is that this one is a
        # count in single digits, so its distribution is stored exactly.
        discrete=True,
        unit="rec",
    ),
    "rushing_yards": Outcome(
        "rushing_yards", "rushing_yards", "carries", {"RB"},
        share_based=True, bands=CARRY_BANDS, trend_bands=CARRY_TREND_BANDS,
        min_baseline=MIN_BASELINE_SHARE,
        baseline_bands=BASELINE_RUSH_BANDS, min_output=5.0,
        # One band, on the same rule that gives the other outcomes two: the
        # posted-total split earns its place by improving calibration across
        # posted totals, and here it does not (worst stratum 5.94pp against
        # 6.07pp without it) while costing 11 points of coverage. That is the
        # same result FINDINGS 9 reached from the other direction -- a high
        # total says a game will be scored in, not how -- and rushing yards
        # stay miscalibrated across posted totals either way, which is recorded
        # rather than papered over.
        posted_bands=[(0, 999)],
        # RB only. A quarterback's carries are scrambles and kneels and a
        # receiver's are jet sweeps -- median 3 and 1 against an RB's 8 -- so
        # they are not a share of the same designed-run pool. Pooling them
        # would be the same error as running QB attempts through a share axis.
        # The corpus's "Konami Code" mobile-QB angle is a different model and
        # is not this one.
    ),
    "passing_yards": Outcome(
        "passing_yards", "passing_yards", "attempts", {"QB"},
        share_based=False, bands=ATTEMPT_BANDS, trend_bands=ATTEMPT_TREND_BANDS,
        # Attempts, not a share: below ten a game is a relief appearance and its
        # prior mean describes a different job.
        min_baseline=10.0,
        baseline_bands=BASELINE_PASS_BANDS, min_output=100.0,
        # ~32 quarterbacks against ~150 pass-catchers. Splitting the posted
        # total on top of four baseline tiers leaves 39-51% of real wagers
        # unpriceable, so this outcome keeps one band and says so rather than
        # publishing sites too thin to resolve.
        posted_bands=[(0, 999)],
    ),
}

# Scenarios, defined on the FINAL game state. That is an end-state proxy for
# what are really path properties, and the naming here reflects what is actually
# measured rather than the narrative it is meant to serve.
#
# In particular: this is "blowout_loss", NOT "trailing". The corpus predicts a
# garbage-time volume boost -- a team down 14 must throw, so its receivers see
# more work. Measured on final margin the effect comes out negative in 14 of 15
# cells, because losing by more than a touchdown mostly identifies offenses that
# did not function, and that swamps the late-game volume.
#
# The measurement does not refute the garbage-time mechanism. It refutes final
# margin as a proxy for it.
class ScenarioDef:
    """What a named scenario MEANS, and the test for whether it happened.

    The predicate is derived from the recorded fields rather than written
    alongside them. That is the whole point: the definition used to be a bare
    lambda, so the artifact could record a scenario's NAME but never what the
    name stood for -- and the CLI takes its own -threshold at query time.
    Asking for `-name shootout -threshold 65` produced s = P(total > 65) blended
    against a q measured on total > 50, which is not a probability of anything.
    Both halves now read the same three fields.
    """

    # Which observation field each basis measures. Adding a basis means adding
    # a row here and populating that field in build(); nothing else changes.
    FIELD = {"total": "game_total", "margin": "margin", "offense_proe": "proe",
             "success_rate": "success_rate"}

    def __init__(self, basis: str, op: str, threshold: float):
        assert basis in self.FIELD, basis
        assert op in (">", "<"), op
        self.basis, self.op, self.threshold = basis, op, threshold

    def occurred(self, o: dict) -> bool | None:
        """Did this scenario happen in this observation?

        None means "cannot say" -- the quantity is missing for this game, which
        for PROE means no play-by-play. Callers must exclude those observations
        from this scenario's cells rather than guessing a side; a missing value
        assigned to `not occurred` would quietly inflate the baseline with games
        that might well have been scenario games.
        """
        v = o.get(self.FIELD[self.basis])
        if v is None:
            return None
        return v > self.threshold if self.op == ">" else v < self.threshold

    def as_json(self) -> dict:
        return {"basis": self.basis, "op": self.op, "threshold": self.threshold}


SCENARIOS = {
    "shootout": ScenarioDef("total", ">", 50),
    "blowout_loss": ScenarioDef("margin", "<", -7),
    # Pass rate over expected: the team threw more than down, distance, score
    # and time called for.
    #
    # HOW MUCH OF IT IS VOLUME. Holding realised opportunity fixed, the share
    # of the separation that survives differs sharply by outcome:
    #
    #   receiving_yards   t 14.25 -> 1.97   90% volume   WITHDRAWN, identity
    #   receptions        t 22.01 -> 9.17   78% volume   kept
    #   passing_yards     t 16.56 -> 5.05   77% volume   kept
    #   rushing_yards     t-18.69 ->-9.80   72% volume   kept
    #
    # Receiving yards is targets x catch rate x yards-per-catch, and this
    # scenario acts only on the first term, so fixing realised targets leaves
    # nothing. The others retain a real effect beyond volume -- plausibly a
    # shorter, safer throw distribution for receptions, and game script beyond
    # carry count for rushing. Coach and scheme tendency with game script divided
    # out -- which is why it needs play-by-play rather than the weekly table,
    # and why it is near-independent of `shootout` (r = +0.098) instead of a
    # second measurement of it.
    #
    # +3.0 rather than a percentile: PROE is in percentage points of pass rate,
    # so the threshold reads as "threw three points more than the situation
    # called for". It puts the base rate at 0.321 against shootout's 0.335,
    # which keeps cell density comparable. Separation barely moves with the
    # threshold anyway -- q-r at a 52.5 line runs +0.090 to +0.095 across
    # everything from 0.0 to +6.0 -- so this is chosen for interpretability and
    # balance, not to maximise an effect.
    "pass_heavy": ScenarioDef("offense_proe", ">", 3.0),
    # Team offensive success rate -- the fraction of plays with positive EPA.
    # The efficiency staple, and the honest substitute for DVOA, which is
    # proprietary to FTN and not obtainable.
    #
    # 0.46 sits at roughly the 66th percentile of team-weeks and puts the base
    # rate near shootout's, keeping cell density comparable. It is also a round,
    # readable number in the units the rate is measured in.
    #
    # It is more entangled with game script than PROE is -- r = +0.302 against
    # the shootout indicator and +0.389 against margin, where PROE manages
    # +0.098 and +0.020 -- so it was tested for redundancy rather than assumed
    # independent. Conditioning on shootout AND blowout together explains
    # dR2 = +0.0127 of receiving yards; adding success rate on top takes it to
    # +0.0196, an extra +0.0068 at t = 17.23. Its coefficient falls from 54.25
    # to 41.47 under that control, so about a quarter of the raw effect is
    # script overlap and three quarters is not.
    "efficient_offense": ScenarioDef("success_rate", ">", 0.46),
}

# Whether a scenario is fit to bet on. Cells are still emitted for unvalidated
# scenarios -- the fit stays reproducible and the data stays available for the
# work that would validate them -- but the Go lookup refuses to price a wager
# from one.
#
# blowout_loss failed validation on three independent counts, any one of which
# would be disqualifying:
#
#   1. The direction inverts at ordinary lines. At 7 projected targets, q > r at
#      6.5, 20.5 and 24.5 receiving yards -- mainstream prop lines, in a
#      volume-bearing band, contradicting the negative direction the medians
#      show. A wager priced there would carry the opposite belief requirement
#      from the one the finding implies.
#   2. The sign is unresolved almost everywhere. A player-level cluster
#      bootstrap of the median delta clears zero in only 3 of 15 cells; the one
#      positive cell has a CI of [0.0, 4.0]. Twelve of fifteen signs are noise.
#   3. It does not survive out of sample. Refit on 2014-2021 and evaluated on
#      2022-2025, the sign holds in 10 of 13 cells against shootout's 14 of 14,
#      with roughly 2.7 points of non-noise error on q -- larger than the
#      2.4-point vig cushion at -110.
#
# shootout passes all three: positive in 15 of 15 cells, resolved in 10, and
# 14 of 14 out of sample.
#
# What would un-gate blowout_loss: defining it on play-by-play (time remaining
# crossed with score differential) rather than final margin, which is the
# measurement this whole result argues for.
# Whether a scenario may be priced. The VERDICT is a human judgement and stays
# one; the EVIDENCE behind it is measured every run by validate.py and written
# into the artifact's note, so it can no longer drift away from the data while
# continuing to read as authoritative.
#
# The rule below is the bar the verdict is held to. It is written out rather
# than inferred, and the fit fails if a recorded verdict and the measured
# evidence disagree -- which is the whole mechanism: a scenario cannot quietly
# stop qualifying and keep its flag.
#
# It is deliberately not a computed threshold. Two scenarios is not enough to
# calibrate one on, and a bar reverse-engineered to reproduce the answer already
# written down would look derived while being fitted to its conclusion.
# The statistical gate is now MEASURED PER SITE (validate.site_verdicts) and is
# not recorded in this file, so there is nothing here for the data to drift
# away from. What remains is the part measurement cannot do.
#
# A VETO is an operator removing a pairing for a reason no test can see -- the
# effect is real and still not something to bet on. It can only ever subtract.
# A scenario absent from this table is decided entirely by its sites.
#
# The purely statistical verdicts that used to live here ("holds in only 13 of
# 16 out of sample", "misses each criterion by a single cell") are gone,
# because that is exactly the judgement the per-site gate now makes -- and
# makes at the granularity a wager is actually priced at. See FINDINGS.md 12.
SCENARIO_VETO = {
    ("receiving_yards", "pass_heavy"): (
        "WITHDRAWN 2026-08-23 as a volume identity, not on the out-of-sample cell it was "
        "previously overridden for. Holding REALISED targets fixed, its coefficient collapses "
        "from t = 14.25 to t = 1.97 -- 90% of the separation is explained by volume the "
        "projection failed to anticipate. It measures the inadequacy of our own projected "
        "targets (prior share x prior 3-game team pool, no market input), not a market "
        "inefficiency. The earlier defence -- that separation holds within projected-target "
        "bands and widens with volume -- is the identity's signature rather than a "
        "refutation: a higher-share player converts extra team attempts into proportionally "
        "more targets, so share x extra attempts IS the widening. Receiving yards is the one "
        "outcome where this is fatal, because it is targets x catch rate x yards-per-catch "
        "and pass_heavy acts only on the first term. It survives volume conditioning for "
        "receptions (t=9.17), rushing (t=-9.80) and passing (t=5.05), and is kept there. See "
        "docs/reviews/2026-08-23-adversarial.md finding C2. "
    ),
    ("receiving_yards", "blowout_loss"): (
        "Needs a play-by-play definition (time remaining crossed with score differential) "
        "rather than final margin, which is the measurement this result argues for. "
    ),
}

# An operator accepting ONE named site's failure. The key IS the site, so an
# override can no longer quietly cover a whole scenario: it names the four
# coordinates it applies to and is checked against the measured verdict for
# exactly that site. If the site stops failing, the override is reported stale.
# WITHDRAWN 2026-08-23 with the axis change. The override below named a site in
# the OLD grid -- 6-8 projected targets crossed with a role trend -- and the
# grid is no longer cut on projected targets at all. There is no way to carry an
# override across a re-cut: the failure it accepted was measured on a cell that
# does not exist any more, and silently re-pointing it at the nearest new cell
# would be accepting a failure nobody measured. If the equivalent site fails
# again on the new grid, it can be accepted again, on new evidence.
#
# The fit enforces this: an override naming a site the grid does not have is a
# hard error, not a warning.
ACCEPTED_FAILURES: dict = {}

_WITHDRAWN_ACCEPTED_FAILURES = {
    ("receptions", "pass_heavy", (6, 8, 0.03, 0.06)): {
        "cell": '6-8 projected targets, +0.03..+0.06 role trend',
        "measured": (
            "15/16 out of sample; consistent 17/17; resolved 17/17 -- the strongest bootstrap of "
            "any pairing in the grid "
        ),
        "why": (
            "The failing cell shows +14.5 yards and +1.22 receptions of separation over 2009-2021 "
            "and -0.5 / -0.02 across 2022-2025, on 65 held-out games. Half a yard is not a "
            "reversal, and the cluster bootstrap cannot distinguish it from zero. The rule still "
            "says no, and is not being softened: a magnitude-aware version was tested and "
            "rejected because it makes every scenario pass (FINDINGS.md 4). This is an operator "
            "accepting one named failure instead. Receptions and receiving yards fail the SAME "
            "cell on the SAME player-games, so this is one failure seen twice, not two. Revisit "
            "when the held-out half has enough seasons to resolve it -- roughly 15-20 games a "
            "year accrue to this cell. "
        ),
        "accepted_by": 'operator, 2026-08-22',
    },
}


def qualifies(ev: dict) -> bool:
    """The stated rule. All three must hold.

    1. The direction is consistent in every cell -- whichever direction it is.
       An effect that reverses from band to band is a description of noise.
    2. It survives out of sample, in every cell the late seasons can speak to.
       A separation that exists only where it was found is not a separation.

    Two things are measured and reported but deliberately NOT gated on:

    Inversions at ordinary prop lines. This was going to be the first
    criterion, on the strength of the note it replaces. Measured against
    sampling error, not one crossing clears 2 SE -- for either scenario. They
    are noise, and gating on a noise count would have rejected shootout on the
    strength of a single 1.3-point wobble at 6.5 yards, a line at which ~88% of
    player-games clear either way.

    Resolution. A player-clustered bootstrap over a few hundred correlated
    games is demanding, and shootout clears only 11 of 15. Requiring all of them
    would reject a real effect; reporting it keeps the reader honest about how
    much of the grid is firmly held.
    """
    return ev["consistent"] == ev["cells"] and ev["oos_agree"] == ev["oos_cells"]

QUANTILE_STEPS = 51  # p0..p100 in 2% steps


def num(s) -> float:
    s = (s or "").strip()
    if s in ("", "NA", "NaN"):
        return 0.0
    try:
        return float(s)
    except ValueError:
        return 0.0


def load_games() -> dict:
    """(season, week, team) -> (realized total, that team's margin, posted total).

    Three quantities, and confusing any two of them is a real hazard. `total`
    is the points actually scored and `result` the margin actually achieved --
    both OUTCOMES, and what a scenario predicate tests. `total_line` is the
    number the market posted before kickoff: PRIOR information, never an
    outcome, and the variable `s` is derived from downstream.

    A game with no posted total is dropped rather than defaulted. Conditioning
    on a total we invented would be worse than not conditioning at all.
    """
    path = CACHE / "games.csv"
    if not path.exists():
        raise SystemExit(f"{path} not found — run ingest/nflverse.py")
    out = {}
    for r in csv.DictReader(path.open()):
        if not (r["result"].strip() and r["total"].strip()
                and r["total_line"].strip()):
            continue
        season, week = int(num(r["season"])), int(num(r["week"]))
        total, result = num(r["total"]), num(r["result"])  # result = home - away
        posted = num(r["total_line"])
        out[(season, week, r["home_team"])] = (total, result, posted)
        out[(season, week, r["away_team"])] = (total, -result, posted)
    return out


def load_player_weeks(outcome: Outcome) -> tuple[list[dict], list[int]]:
    """Every regular-season game-week in the fit window, for one outcome.

    A missing season is an error, not a skip. It used to `continue`, which
    made a partial cache produce a quietly smaller grid -- and since the
    artifact stamped the seasons it *asked* for rather than the ones it read,
    the result claimed 2014-2025 either way. A four-season grid labelled as
    twelve is worse than no grid: every q and r it serves is defensible-looking
    and wrong, and nothing downstream can tell.

    Returns the rows and the seasons they actually came from, so the caller can
    stamp what was read instead of what was requested.
    """
    rows = []
    seasons = []
    for season in range(FIRST, LAST + 1):
        path = CACHE / f"stats_player_week_{season}.csv"
        if not path.exists():
            raise SystemExit(
                f"{path} not found -- run ingest/nflverse.py --seasons {FIRST}-{LAST}.\n"
                f"  Refusing to fit {FIRST}-{LAST} from a partial cache: the result would "
                f"be a thinner grid carrying the full window's provenance."
            )
        seasons.append(season)
        for r in csv.DictReader(path.open()):
            if r.get("season_type") != "REG" or r.get("position") not in outcome.positions:
                continue
            rows.append(
                {
                    "season": int(num(r["season"])),
                    "week": int(num(r["week"])),
                    "player": r.get("player_id", ""),
                    "team": r.get("team") or r.get("recent_team", ""),
                    "opportunity": num(r.get(outcome.opp_field)),
                    "yards": num(r.get(outcome.yards_field)),
                }
            )
    return rows, seasons


def build(rows, games, outcome: Outcome, proe_tw: dict | None = None,
          signals_tw: dict | None = None) -> list[dict]:
    """One observation per player-game, using only prior information as inputs.

    proe_tw is the team-week PROE series from analysis/proe.py, keyed
    (season, week, team). Optional: without it the `proe` field is None, every
    PROE scenario reports "cannot say" for every game, and its cells drop out
    rather than being fitted against a silently absent quantity.
    """
    if outcome.share_based:
        # A pass-catcher competes for a fixed pool, so his opportunity is a
        # share of it and his baseline must be measured that way.
        team_pool = defaultdict(float)
        for r in rows:
            team_pool[(r["season"], r["week"], r["team"])] += r["opportunity"]
        for r in rows:
            d = team_pool[(r["season"], r["week"], r["team"])]
            r["basis"] = r["opportunity"] / d if d > 0 else 0.0
            r["team_pool"] = d
    else:
        # A quarterback takes essentially all of his team's attempts. There is
        # no share to hold, so the opportunity IS the volume.
        for r in rows:
            r["basis"] = r["opportunity"]
            r["team_pool"] = 0.0

    by_player = defaultdict(list)
    for r in rows:
        by_player[(r["player"], r["season"])].append(r)

    obs = []
    for (player, _season), g in by_player.items():
        g.sort(key=lambda x: x["week"])
        for i, x in enumerate(g):
            if i < MIN_PRIOR_GAMES:
                continue
            prior = g[:i]
            stats = prior_stats(prior)
            baseline = stats["baseline_share"]
            if baseline < outcome.min_baseline:
                continue
            recent = stats["trend"] + baseline
            # The player's own prior mean OUTPUT, as opposed to his prior mean
            # opportunity. Prior information only, and the quantity a book's
            # line is actually set near -- see docs/reviews/2026-08-23-adversarial.md
            # finding C3.
            baseline_yards = stats["baseline_output"]
            if baseline_yards < outcome.min_output:
                # Below this the ratio stops being a stable quantity: on a
                # 3-yard baseline an ordinary game is a ratio of 8, and the
                # tail of the cell would be made of arithmetic rather than
                # football.
                continue
            # Projected opportunity uses only prior information. For a share
            # outcome that is the baseline share against the team's recent
            # volume; for a volume outcome the baseline already IS the
            # projection.
            if outcome.share_based:
                projected = baseline * st.mean(p["team_pool"] for p in prior[-3:])
            else:
                projected = baseline
            ctx = games.get((x["season"], x["week"], x["team"]))
            if ctx is None:
                continue
            game_total, margin, posted_total = ctx
            obs.append(
                {
                    "player": player,
                    # Carried for the out-of-sample split in validate.py. The
                    # fit itself pools across seasons and does not use it.
                    "season": x["season"],
                    # Carried so team-week series (PROE, and anything else keyed
                    # that way) can be joined on. build() used to drop both,
                    # which made such a join impossible without reimplementing
                    # this function.
                    "week": x["week"],
                    "team": x["team"],
                    # None when play-by-play is not cached for this season, or
                    # the game had too few plays with a defined xpass.
                    "proe": (proe_tw or {}).get(
                        (x["season"], x["week"], x["team"]), {}
                    ).get("offense"),
                    # None when play-by-play is not cached for the season, or
                    # the team-week had too few scrimmage plays to be a rate.
                    "success_rate": (signals_tw or {}).get(
                        (x["season"], x["week"], x["team"]), {}
                    ).get("success_rate"),
                    "opportunity": projected,
                    "baseline_yards": baseline_yards,
                    "trend": recent - baseline,
                    # THE FITTED VALUE IS A RATIO, not the raw number. A book
                    # sets its line near this player's own median, so a grid
                    # holding raw yards answers "what does the cohort do at
                    # this line" when the question was "what does HE do". That
                    # mismatch measured 8pp at the top tier -- four times the
                    # vig cushion. See FINDINGS.md 11.
                    "yards": x["yards"] / baseline_yards,
                    "output": x["yards"],
                    "game_total": game_total,
                    "margin": margin,
                    # The POSTED total: prior information, and the variable the
                    # operator's `s` is derived from. Distinct from game_total,
                    # which is what was actually scored.
                    "posted_total": posted_total,
                }
            )
    return obs


def prior_stats(prior: list[dict]) -> dict:
    """Baseline and trend from a player's PRIOR games only.

    Shared by the fit and by analysis/player.py rather than written twice.
    player.py has to produce the same two numbers for an UPCOMING week, and a
    wrong one does not fail -- it lands the operator in a neighbouring cell and
    prices a different population with nothing to show that it happened. Two
    copies of this arithmetic would eventually disagree, and the disagreement
    would be silent.

    Expects rows carrying `basis` (share of the team pool, or raw volume) and
    `yards` (the raw output, before the fit turns it into a ratio).
    """
    baseline = st.mean(p["basis"] for p in prior)
    return {
        "baseline_share": baseline,
        "baseline_output": st.mean(p["yards"] for p in prior),
        "trend": st.mean(p["basis"] for p in prior[-TREND_WINDOW:]) - baseline,
    }


def band_index(bands, v):
    for i, (lo, hi) in enumerate(bands):
        if lo <= v < hi:
            return i
    return None


def quantiles(values: list[float], discrete: bool = False) -> list[list[float]]:
    """[[cumulative probability, value], ...].

    For a CONTINUOUS outcome this samples the quantile function at evenly
    spaced probabilities, and the lookup interpolates between neighbouring
    points. Yardage is dense enough that the error is negligible.

    For a DISCRETE outcome it stores the exact CDF at every observed value
    instead. Receptions live on the integers 0-21 and their lines are
    half-integers, so P(X > 3.5) is exactly P(X > 3) -- there is no
    probability mass between 3 and 4 to interpolate across. Sampling that
    distribution at 2% steps and interpolating produced errors up to 1.44
    percentage points, bounded by half a step exactly as theory says. Small,
    but it is method error rather than sampling noise, and it is invisible.
    Storing the real CDF costs about twenty points per cell and removes it.
    """
    s = sorted(values)
    n = len(s)
    if discrete:
        out = []
        seen = 0
        for v in sorted(set(s)):
            seen += s.count(v)
            out.append([round(seen / n, 6), round(v, 1)])
        return out
    out = []
    for i in range(QUANTILE_STEPS):
        p = i / (QUANTILE_STEPS - 1)
        idx = min(int(p * (n - 1)), n - 1)
        out.append([round(p, 4), round(s[idx], 1)])
    return out


def effective_n(values: list[float], players: list[str]) -> tuple[float, float]:
    """Effective sample size after accounting for repeat players.

    A cell pools many games from the same player, and those observations are not
    independent -- a target hog contributes twenty correlated rows. Feeding the
    raw count into a Wilson interval therefore claims more precision than the
    data supports.

    The correction is the standard design effect, deff = 1 + (m0 - 1) * ICC,
    with ICC estimated by one-way ANOVA over players within the cell. Measured
    across the published grid the design effect runs 1.0 to about 1.4, so this
    widens intervals by roughly 9% typically rather than transforming them --
    cells pool 42 to 847 distinct players, which is what keeps the problem small.

    Returns (n_eff, icc).
    """
    n = len(values)
    groups: dict[str, list[float]] = defaultdict(list)
    for v, p in zip(values, players):
        groups[p].append(v)
    k = len(groups)
    if k < 2 or n <= k:
        return float(n), 0.0

    grand = st.mean(values)
    # Between- and within-group mean squares.
    ss_between = sum(len(g) * (st.mean(g) - grand) ** 2 for g in groups.values())
    ss_within = sum((v - st.mean(g)) ** 2 for g in groups.values() for v in g)
    ms_between = ss_between / (k - 1)
    ms_within = ss_within / (n - k)
    if ms_within <= 0:
        return float(n), 0.0

    # m0 is the size-corrected mean cluster size, not the plain mean.
    sum_sq = sum(len(g) ** 2 for g in groups.values())
    m0 = (n - sum_sq / n) / (k - 1)
    if m0 <= 1:
        return float(n), 0.0

    icc = (ms_between - ms_within) / (ms_between + (m0 - 1) * ms_within)
    icc = max(0.0, min(icc, 1.0))
    deff = 1 + (m0 - 1) * icc
    return n / deff if deff > 0 else float(n), icc


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--report", action="store_true", help="print cells, write nothing")
    args = ap.parse_args(argv)

    games = load_games()

    # PROE comes from play-by-play, which is a separate and much larger table.
    # A scenario whose quantity is missing everywhere still fits -- every cell
    # simply falls below MIN_CELL and is dropped, which is the honest outcome
    # and is visible in the dropped count rather than silently wrong.
    import proe as proe_mod
    import signals as signals_mod

    cells, dropped = [], 0
    status: dict[str, dict] = {}
    # (outcome, scenario) -> {site key: verdict}. Attached to each cell below,
    # so the gate travels with the numbers it gates rather than beside them.
    sites_by: dict[str, dict] = {}
    seasons_read = None
    proe_tw = None
    signals_tw = None

    for oname, outcome in OUTCOMES.items():
        rows, seasons = load_player_weeks(outcome)
        if seasons_read is None:
            seasons_read = seasons
            proe_tw = proe_mod.load(seasons[0], seasons[-1])
            signals_tw = signals_mod.load(seasons[0], seasons[-1])
            print(f"team-weeks with PROE: {len(proe_tw)}   with rates: {len(signals_tw)}")
        obs = build(rows, games, outcome, proe_tw, signals_tw)
        print(f"\n=== {oname} ===")
        print(f"player-weeks {FIRST}-{LAST}: {len(rows)}   usable observations: {len(obs)}")

        # Measure the evidence before building cells, so a fit can never emit an
        # artifact whose validation note disagrees with the data in the same file.
        #
        # Per OUTCOME, not once globally: a scenario that separates receiving
        # yards need not separate passing yards, and carrying one verdict across
        # both would be exactly the unvalidated leap this pipeline exists to
        # stop.
        print("VALIDATION")
        status[oname] = {}
        sites_by[oname] = {}
        for scenario, definition in SCENARIOS.items():
            # The scenario-level evidence is still measured, but it is now a
            # SUMMARY rather than the gate -- reported so the note in the
            # artifact stays a statement about the data.
            ev = validate.evidence(obs, definition, outcome.axes(),
                                   MIN_CELL, False)
            # The sweep runs FIRST and hands back the bootstrap it computed at
            # the loosest threshold, so the expensive test runs once rather
            # than twice over overlapping site sets.
            stab = validate.verdict_stability(obs, definition, outcome.axes(), False)
            sv = validate.site_verdicts(obs, definition, outcome.axes(),
                                        MIN_CELL, False, require_oos=True,
                                        resolved=stab["resolved"])
            # How much of each verdict is the data and how much is the two
            # knobs. Stamped onto the cell so it travels with the number it
            # qualifies, rather than living in a report nobody reads at the
            # point of pricing.
            for key, v in sv.items():
                v["stability"] = stab["sites"].get(key, {}).get("share")
            sites_by[oname][scenario] = sv
            note = validate.note(ev)
            # .strip(): these are written as wrapped implicit-concat literals,
            # which leaves a trailing space on the last fragment.
            veto = (SCENARIO_VETO.get((oname, scenario)) or "").strip() or None
            msgs = []

            # Does this scenario have a direction at all? A site is judged by
            # whether it agrees with the scenario's dominant sign, so if that
            # sign is a coin flip the agreeing sites are the lucky half.
            p_sign = validate.sign_coherence(ev["consistent"], ev["cells"])
            incoherent = p_sign >= validate.COHERENCE_ALPHA

            # An override names ONE site and is checked against that site's
            # measured verdict. It cannot cover a scenario, and it cannot name
            # a site the grid does not have.
            for (o2, s2, key), acc in ACCEPTED_FAILURES.items():
                if (o2, s2) != (oname, scenario):
                    continue
                if incoherent:
                    raise SystemExit(
                        f"\n{oname}/{scenario}: accepted_failure on a scenario with no "
                        f"established direction ({ev['consistent']}/{ev['cells']}, "
                        f"p={p_sign:.3f}). Accepting one named failure presumes the rest of "
                        f"the pairing is sound; here there is nothing to except it from."
                    )
                if veto:
                    raise SystemExit(
                        f"\n{oname}/{scenario}: has both a veto and an accepted failure. "
                        f"A veto removes the pairing outright; an override that survives it "
                        f"would make the veto decorative."
                    )
                v = sv.get(key)
                if v is None:
                    raise SystemExit(
                        f"\n{oname}/{scenario}: accepted_failure names site {key}, which is "
                        f"not in the grid. An override for a cell that does not exist accepts "
                        f"nothing and hides the fact."
                    )
                for field in ("cell", "measured", "why", "accepted_by"):
                    if not acc.get(field):
                        raise SystemExit(
                            f"\n{oname}/{scenario}: accepted_failure is missing {field!r}. "
                            f"An override that does not say WHICH failure was accepted, by "
                            f"whom, is indistinguishable from turning the gate off."
                        )
                if v["priceable"]:
                    msgs.append(f"NOTE: accepted_failure for {key} is stale -- that site "
                                f"now passes on its own and the override can be removed")
                else:
                    v["priceable"] = True
                    # The site bounds travel with the override so a reader --
                    # and the Go struct -- has the whole record in one place,
                    # rather than the coordinates living only in the dict key.
                    acc = {k: (v2.strip() if isinstance(v2, str) else v2)
                           for k, v2 in acc.items()}
                    v["override"] = dict(acc, opportunity_min=key[0],
                                         opportunity_max=key[1],
                                         trend_min=key[2], trend_max=key[3])
                    msgs.append(f"OVERRIDE: {key} priced despite {'; '.join(v['why'])}")
                    msgs.append(f"  accepted {acc['accepted_by']}")

            if incoherent:
                for v in sv.values():
                    v["priceable"] = False
                    v["why"] = [f"the scenario has no established direction: it leans one way "
                                f"in only {ev['consistent']} of {ev['cells']} cells (p={p_sign:.3f})"]
            if veto:
                for v in sv.values():
                    v["priceable"] = False
                    v["vetoed"] = True
                    v["why"] = ["the operator vetoed this pairing"]

            ok = sum(1 for v in sv.values() if v["priceable"])
            print(f"  {scenario:18} {note}")
            # FIRM means every one of the 25 MIN_CELL x OOS_SPLIT settings
            # agrees. Anything short of that is a verdict the knobs can move,
            # and is labelled rather than quietly counted as a pass.
            firm = sum(1 for v in sv.values()
                       if v["priceable"] and (v.get("stability") or 0) >= 1.0)
            print(f"  {'':18} {ok}/{len(sv)} sites priceable ({firm} firm at every "
                  f"knob setting)   sign p={p_sign:.4f}"
                  + ("  [VETOED]" if veto else "")
                  + ("  [NO DIRECTION]" if incoherent else ""))
            if qualifies(ev) != (ok == len(sv)):
                print(f"  {'':18} (the old scenario-level rule said {qualifies(ev)})")
            for m in msgs:
                print(f"  {'':18} {m}")

            status[oname][scenario] = {
                # Kept under the old key so a reader of the artifact is not
                # silently handed a different meaning: it now says whether ANY
                # site of this pairing may be priced, and the per-cell flags say
                # which. A pairing with no priceable site is as unbettable as a
                # scenario that failed the old gate.
                "validated": ok > 0,
                "sites": len(sv),
                "sites_priceable": ok,
                "note": note,
                "evidence": ev,
                "rule_says": qualifies(ev),
                "sign_p": round(p_sign, 5),
            }
            if veto:
                status[oname][scenario]["vetoed"] = True
                status[oname][scenario]["why"] = veto

        axes = outcome.axes()
        fields = [f for f, _ in axes]
        for scenario, definition in SCENARIOS.items():
            for occurred in (True, False):
                for combo in itertools.product(*[b for _, b in axes]):
                    if True:
                        sel = [
                            o
                            for o in obs
                            if all(lo <= o[f] < hi
                                   for f, (lo, hi) in zip(fields, combo))
                            and definition.occurred(o) == occurred
                        ]
                        if len(sel) < MIN_CELL:
                            dropped += 1
                            continue
                        ys = [o["yards"] for o in sel]
                        n_eff, icc = effective_n(ys, [o["player"] for o in sel])
                        # The verdict for the SITE this cell belongs to. Both
                        # halves of a site carry the same flag, because q and r
                        # are only meaningful together -- pricing one against a
                        # cell that failed is the mismatch this gate exists to
                        # stop. A cell with no site verdict was never paired
                        # (its opposite half fell below MIN_CELL) and cannot be
                        # priced from at all.
                        sv = sites_by[oname][scenario].get(
                            tuple(x for band in combo for x in band))
                        cells.append(
                            {
                                "outcome": oname,
                                "scenario": scenario,
                                "occurred": occurred,
                                "validated": bool(sv and sv["priceable"]),
                                "why": (sv["why"] if sv else
                                        ["this cell has no opposite half above "
                                         f"n={MIN_CELL}, so no q/r pair exists here"]),
                                "override": (sv or {}).get("override"),
                                # The share of MIN_CELL x OOS_SPLIT settings
                                # under which this site is priceable. None
                                # where no setting publishes the site at all.
                                "stability": (sv or {}).get("stability"),
                                "posted_min": combo[0][0],
                                "posted_max": combo[0][1],
                                "baseline_min": combo[1][0],
                                "baseline_max": combo[1][1],
                                "trend_min": combo[2][0],
                                "trend_max": combo[2][1],
                                "n": len(sel),
                                "n_eff": round(n_eff, 1),
                                "players": len({o["player"] for o in sel}),
                                "icc": round(icc, 4),
                                # The RATIO median -- 1.0 means a typical game
                                # for this player. median_output is the same
                                # cell in the units a person reads.
                                "median": round(st.median(ys), 4),
                                "median_output": round(
                                    st.median([o["output"] for o in sel]), 1),
                                # Ratios are continuous even when the outcome
                                # is a count: 3 receptions against a 4.2
                                # baseline is 0.714, and there is no integer
                                # lattice left to store exactly.
                                "quantiles": quantiles(ys, False),
                            }
                        )

    print(f"\ncells: {len(cells)} published, {dropped} dropped for n < {MIN_CELL}\n")

    # Show the thing the decomposition is built on: does the scenario move the
    # distribution at all? If q and r are equal the scenario carries no
    # information, which RequiredScenarioProb rejects outright.
    # Ratios, so 1.00 is a typical game for the player in question and the
    # delta reads as a percentage of his own baseline rather than in yards.
    print("SCENARIO SEPARATION (median ratio to own baseline, occurred vs not)")
    hdr = (f"  {'outcome':>15} {'scenario':>18} {'posted':>9} {'baseline':>11} "
           f"{'trend':>14} {'occ':>6} {'not':>6} {'delta':>7} {'':>3}")
    print(hdr)
    for oname, outcome in OUTCOMES.items():
        for scenario in SCENARIOS:
            for combo in itertools.product(*[b for _, b in outcome.axes()]):
                got = {
                    c["occurred"]: c
                    for c in cells
                    if c["outcome"] == oname
                    and c["scenario"] == scenario
                    and c["posted_min"] == combo[0][0]
                    and c["baseline_min"] == combo[1][0]
                    and c["trend_min"] == combo[2][0]
                }
                if len(got) != 2:
                    continue
                a, b = got[True]["median"], got[False]["median"]
                mark = "" if got[True]["validated"] else "gated"
                print(
                    f"  {oname:>15} {scenario:>18} "
                    f"{f'{combo[0][0]:g}-{combo[0][1]:g}':>9} "
                    f"{f'{combo[1][0]:g}-{combo[1][1]:g}':>11} "
                    f"{f'{combo[2][0]:+.2f}..{combo[2][1]:+.2f}':>14} "
                    f"{a:>6.3f} {b:>6.3f} {a - b:>+7.3f} {mark:>5}"
                )

    if args.report:
        print("\n--report: artifact not written")
        return 0

    # Which files this fit actually read, hashed. fit_residuals.py has recorded
    # this since it shipped; conditionals never did, and the asymmetry cost a
    # full refit-and-diff to answer "does the committed grid still reproduce?"
    # -- a question the manifest could have answered in a second. Upstream
    # revises historical seasons (a 2014-2021 correction moved five cell
    # medians after this artifact was generated), so "same seasons" is not the
    # same claim as "same data".
    source = {}
    if MANIFEST.exists():
        m = json.loads(MANIFEST.read_text())
        for season in seasons_read:
            name = f"stats_player_week_{season}.csv"
            if name in m:
                source[name] = m[name]

    ARTIFACT.parent.mkdir(parents=True, exist_ok=True)
    ARTIFACT.write_text(
        json.dumps(
            {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "generated_by": "edge/model/analysis/fit_conditionals.py",
                # The outcomes fitted, and the opportunity axis each is
                # conditioned on. Was a single top-level string, which could
                # only ever describe one grid.
                "outcomes": {k: v.as_json() for k, v in OUTCOMES.items()},
                # The seasons READ, not the seasons requested. Identical while
                # load_player_weeks refuses a partial cache, which is the point:
                # the stamp is now derived rather than asserted.
                "seasons": [seasons_read[0], seasons_read[-1]],
                "source": source,
                "min_cell": MIN_CELL,
                # What each name means. Without this the artifact recorded that
                # a cell belonged to "shootout" but not what "shootout" was, so
                # nothing could check the caller's threshold against it.
                "scenario_definitions": {k: v.as_json() for k, v in SCENARIOS.items()},
                "scenario_status": status,
                "note": (
                    "quantiles are [[probability, yards], ...]; P(yards > L) is "
                    "1 minus the interpolated CDF. n supports a Wilson interval "
                    "computed at query time."
                ),
                "cells": cells,
            },
            indent=1,
        )
        + "\n"
    )
    print(f"\nwrote {ARTIFACT} ({ARTIFACT.stat().st_size / 1024:.0f} KB)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

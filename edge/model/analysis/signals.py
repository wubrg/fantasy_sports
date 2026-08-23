"""Team-week belief signals from cached play-by-play.

Each is a candidate SCENARIO: a realized condition about a game that a prop's
outcome might depend on. That is the shape the grid already fits -- `shootout`
is "the total went over 50", `pass_heavy` is "the offense threw more than
expected" -- and it costs no cell density to add another, because a scenario
gets its own set of cells rather than multiplying an existing axis.

Three signals, all derivable from play-by-play already on disk:

    success_rate   fraction of a team's plays that succeeded, nflfastR's
                   definition (EPA > 0). The efficiency staple, and the honest
                   substitute for DVOA, which is proprietary to FTN.
    chunk_rate     fraction of plays gaining 20+ yards. Explosiveness, and the
                   thing an alternate-line Over is really betting on.
    tempo          fraction of plays from shotgun or no-huddle. NAMED BADLY:
                   this is a formation choice, not pace. Its correlation with
                   plays actually run is +0.064, and shotgun is near-universal
                   now. Kept under the name it shipped with, with the caveat
                   attached, because renaming it would quietly disconnect it
                   from FINDINGS section 9.

                   Real pace -- plays run -- was measured after the fact and
                   rejected too, on persistence rather than signal: it explains
                   dR2 +0.0143 of receiving yards beyond projected targets but
                   a team's prior play volume predicts this week's at only
                   r = +0.138. The `plays` count is returned alongside the rates
                   so that is reproducible.

Each also comes in a prior-information-only form -- a team's rate COMING INTO a
game, never including it -- because that is the only form usable to forecast
whether the scenario will occur. Same discipline as proe.prior_form and
fit_conditionals.build.

What this file does NOT do is decide whether any of them is worth using. That
is `--gate1` here, then validate.py inside the fit.
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

# Below this a team-week rate is describing a handful of snaps, not a tendency.
MIN_PLAYS = 20

# A "chunk" play. Twenty yards is the common explosive-pass threshold and is
# used here for all plays rather than splitting pass and rush, because the
# signal is meant to describe a team's game, not a play type.
CHUNK_YARDS = 20.0


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
    """(season, week, team) -> the three rates for that team's own plays."""
    path = CACHE / f"play_by_play_{season}.csv.gz"
    if not path.exists():
        raise SystemExit(f"{path} not found -- run ingest/nflverse.py --seasons {season}")

    acc: dict[tuple[int, int, str], dict] = defaultdict(
        lambda: {"n": 0, "success": 0, "chunk": 0, "tempo": 0}
    )
    with gzip.open(path, "rt", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader)
        try:
            ix = {
                c: header.index(c)
                for c in ("season_type", "week", "posteam", "play_type",
                          "success", "yards_gained", "shotgun", "no_huddle")
            }
        except ValueError as e:
            raise SystemExit(f"{path}: expected column missing ({e})")

        for row in reader:
            if row[ix["season_type"]] != "REG":
                continue
            # Scrimmage plays only: kickoffs, punts and kneels are not a team
            # choosing how to attack, and including them would let special
            # teams volume move an offensive tendency.
            if row[ix["play_type"]] not in ("pass", "run"):
                continue
            team = row[ix["posteam"]].strip()
            week = num(row[ix["week"]])
            if not team or week is None:
                continue
            success = num(row[ix["success"]])
            if success is None:
                continue
            gained = num(row[ix["yards_gained"]])
            shotgun = num(row[ix["shotgun"]]) or 0.0
            nohuddle = num(row[ix["no_huddle"]]) or 0.0

            a = acc[(season, int(week), team)]
            a["n"] += 1
            a["success"] += 1 if success > 0 else 0
            a["chunk"] += 1 if (gained is not None and gained >= CHUNK_YARDS) else 0
            a["tempo"] += 1 if (shotgun > 0 or nohuddle > 0) else 0

    return {
        k: {
            "success_rate": v["success"] / v["n"],
            "chunk_rate": v["chunk"] / v["n"],
            "tempo": v["tempo"] / v["n"],
            "plays": v["n"],
        }
        for k, v in acc.items()
        if v["n"] >= MIN_PLAYS
    }


def load(first: int, last: int) -> dict[tuple[int, int, str], dict]:
    out = {}
    for season in range(first, last + 1):
        if (CACHE / f"play_by_play_{season}.csv.gz").exists():
            out.update(team_weeks(season))
    return out


NAMES = ("success_rate", "chunk_rate", "tempo")


def prior_form(tw: dict, min_prior: int = 3) -> dict[tuple[int, int, str], dict]:
    """Each team's rates coming into a game, from earlier games only.

    Never includes the game itself. A predictor that contains the thing it
    predicts is not one.
    """
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
                f"{n}_prior": st.mean(p[n] for p in prior) for n in NAMES
            } | {"prior_games": i}
    return out


def injuries_out(first: int, last: int) -> dict[tuple[int, int, str], set[str]]:
    """(season, week, team) -> the gsis_ids listed OUT that week.

    Only "Out". Questionable and Doubtful are forecasts of availability, not
    facts about it, and a player listed Questionable plays most of the time --
    folding them in would put maybes into a signal whose whole value is that it
    is a checked fact.

    Schema note: every season carries `game_type`; only 2025 also carries
    `season_type`. Reading the newer name alone would silently return nothing
    for sixteen of seventeen seasons, so the older one is what is used.
    """
    out: dict[tuple[int, int, str], set[str]] = defaultdict(set)
    for season in range(first, last + 1):
        path = CACHE / f"injuries_{season}.csv"
        if not path.exists():
            continue
        for r in csv.DictReader(path.open()):
            if r.get("game_type") != "REG":
                continue
            if (r.get("report_status") or "").strip() != "Out":
                continue
            gsis = (r.get("gsis_id") or "").strip()
            week = num(r.get("week"))
            team = (r.get("team") or "").strip()
            if gsis and week is not None and team:
                out[(season, int(week), team)].add(gsis)
    return dict(out)


def baseline_shares(rows: list[dict], min_prior: int = 4) -> dict:
    """(player, season) -> {week: baseline share from games BEFORE that week}.

    Defined for every week in the season after the player's fourth game, not
    only the weeks he played. That distinction is the whole point: a usage
    vacuum asks what an ABSENT player would have commanded, and an absent
    player has no row in the week he missed. Keying the baseline on weeks
    played makes exactly the players the signal is about unfindable -- which,
    measured, left 9 non-zero values in 83,183.
    """
    by_player = defaultdict(list)
    for r in rows:
        by_player[(r["player"], r["season"])].append(r)

    out = {}
    for key, games in by_player.items():
        games.sort(key=lambda x: x["week"])
        weeks = {}
        for i, x in enumerate(games):
            if i < min_prior:
                continue
            weeks[x["week"]] = st.mean(p["share"] for p in games[:i])
        if not weeks:
            continue
        # Carry forward across missed weeks: the baseline on the week he sat
        # out is what he had established by then.
        last_week = max(g["week"] for g in games)
        running = None
        filled = {}
        for w in range(1, last_week + 2):
            if w in weeks:
                running = weeks[w]
            if running is not None:
                filled[w] = running
        out[key] = filled
    return out


def usage_vacuum(rows: list[dict], outs: dict, baseline: dict) -> dict:
    """(season, week, player) -> the baseline share sitting OUT around him.

    The framework's Tier 3: "the WR1 just got ruled out, the WR2 priced at 30
    yards will now see 10 targets, the math is simply broken." What makes that
    bettable is not that someone is hurt but HOW MUCH OPPORTUNITY was vacated,
    so the signal is the sum of the absent players' own baseline shares --
    computed from their prior games, never from the game being predicted.

    A star missing is worth several times a rotational player missing, and this
    says so rather than counting bodies.
    """
    by_team_week = defaultdict(list)
    for r in rows:
        by_team_week[(r["season"], r["week"], r["team"])].append(r)

    def share_of(player: str, season: int, week: int) -> float:
        return baseline.get((player, season), {}).get(week, 0.0)

    vac = {}
    for (season, week, team), teammates in by_team_week.items():
        absent = outs.get((season, week, team), set())
        gone = sum(share_of(p, season, week) for p in absent)
        for r in teammates:
            vac[(season, week, r["player"])] = gone
    return vac


def corr(xs, ys) -> float:
    mx, my = st.mean(xs), st.mean(ys)
    n = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    d = (sum((x - mx) ** 2 for x in xs) * sum((y - my) ** 2 for y in ys)) ** 0.5
    return n / d if d else float("nan")


def gate1(first: int, last: int) -> int:
    """Is any of this worth fitting? Three questions, none needing a grid."""
    import fit_conditionals as fc
    import proe
    from utilization_lag import ols_clustered

    tw = load(first, last)
    pf = prior_form(tw)
    print(f"team-weeks {first}-{last}: {len(tw)}   with >=3 prior games: {len(pf)}\n")

    print("PERSISTENCE  (prior vs realized; near zero means unforecastable)")
    for n in NAMES:
        pairs = [(pf[k][f"{n}_prior"], tw[k][n]) for k in pf if k in tw]
        print(f"  {n:<14} r = {corr(*zip(*pairs)):+.3f}")

    games = fc.load_games()
    proe_tw = proe.load(first, last)
    rows = [(k, v) for k, v in tw.items() if k in games]
    print("\nCONFOUND  (against what the grid already conditions on)")
    print(f"  {'signal':<14} {'vs shootout':>12} {'vs margin':>10} {'vs PROE':>9}")
    for n in NAMES:
        vals = [v[n] for _, v in rows]
        shoot = [1.0 if games[k][0] > 50 else 0.0 for k, _ in rows]
        marg = [games[k][1] for k, _ in rows]
        both = [(v[n], proe_tw[k]["offense"]) for k, v in rows if k in proe_tw]
        pr = corr(*zip(*both)) if both else float("nan")
        print(f"  {n:<14} {corr(vals, shoot):>+12.3f} {corr(vals, marg):>+10.3f} {pr:>+9.3f}")

    fc.FIRST, fc.LAST = first, last
    o = fc.OUTCOMES["receiving_yards"]
    obs = fc.build(fc.load_player_weeks(o)[0], games, o, proe_tw)
    J = []
    for x in obs:
        k = (x["season"], x["week"], x["team"])
        if k in tw and k in pf:
            x = dict(x) | {n: tw[k][n] for n in NAMES} | {f"{n}_prior": pf[k][f"{n}_prior"] for n in NAMES}
            J.append(x)

    y = [x["yards"] for x in J]
    g = [x["player"] for x in J]
    _, _, r2b = ols_clustered([[1.0, x["opportunity"]] for x in J], y, g)
    print(f"\nSIGNAL  ({len(J)} player-games; baseline = projected targets, R2 {r2b:.5f})")
    print(f"  {'term':<28} {'beta':>10} {'t':>7} {'dR2':>10}   verdict")
    for n in NAMES:
        for form in (n, f"{n}_prior"):
            b, se, r2 = ols_clustered([[1.0, x["opportunity"], x[form]] for x in J], y, g)
            t = b[2] / se[2] if se[2] > 0 else 0.0
            print(f"  {form:<28} {b[2]:>10.2f} {t:>7.2f} {r2 - r2b:>+10.5f}   "
                  f"{'SIGNIFICANT' if abs(t) > 1.96 else 'null'}")
    b, se, r2 = ols_clustered([[1.0, x["opportunity"], x["trend"]] for x in J], y, g)
    print(f"  {'role trend, for comparison':<28} {b[2]:>10.2f} {b[2]/se[2]:>7.2f} "
          f"{r2 - r2b:>+10.5f}")

    # The usage vacuum, tested where the framework actually makes its claim.
    pool = defaultdict(float)
    raw, _ = fc.load_player_weeks(o)
    for r in raw:
        pool[(r["season"], r["week"], r["team"])] += r["opportunity"]
    for r in raw:
        dd = pool[(r["season"], r["week"], r["team"])]
        r["share"] = r["opportunity"] / dd if dd > 0 else 0.0
    base = baseline_shares(raw)
    outs = injuries_out(first, last)
    bytw = defaultdict(list)
    for r in raw:
        bytw[(r["season"], r["week"], r["team"])].append(r)
    vac, is_top = {}, {}
    for (ss, ww, tt), mates in bytw.items():
        gone = sum(base.get((p, ss), {}).get(ww, 0.0) for p in outs.get((ss, ww, tt), set()))
        ranked = sorted(mates, key=lambda r: -base.get((r["player"], ss), {}).get(ww, 0.0))
        for i, r in enumerate(ranked):
            vac[(ss, ww, r["player"])] = gone
            is_top[(ss, ww, r["player"])] = i == 0
    V = [dict(x, vacuum=vac[k], is_top=is_top[k])
         for x in J if (k := (x["season"], x["week"], x["player"])) in vac]
    print("\nUSAGE VACUUM  (baseline target share of the teammates listed OUT)")
    for subset, label in ((V, "every pass-catcher"),
                          ([x for x in V if x["is_top"]], "the TOP remaining receiver")):
        gg = [x["player"] for x in subset]
        yy = [x["yards"] for x in subset]
        _, _, a = ols_clustered([[1.0, x["opportunity"]] for x in subset], yy, gg)
        b, se, r2 = ols_clustered(
            [[1.0, x["opportunity"], x["vacuum"]] for x in subset], yy, gg)
        hi = [x for x in subset if x["vacuum"] > 0.15]
        lo = [x for x in subset if x["vacuum"] <= 0.001]
        q = sum(1 for x in hi if x["yards"] > 52.5) / len(hi)
        r = sum(1 for x in lo if x["yards"] > 52.5) / len(lo)
        print(f"  {label:<28} beta {b[2]:>7.2f}  t {b[2]/se[2]:>5.2f}  "
              f"dR2 {r2-a:>+8.5f}   q-r {q-r:+.3f}")
    print("  The framework's Tier 3 is a claim about the SECOND line: WR1 out, WR2 eats.")

    print("\nSEPARATION  (top vs bottom quartile of the REALIZED rate, at 52.5)")
    for n in NAMES:
        vals = sorted(x[n] for x in J)
        p25, p75 = vals[len(vals) // 4], vals[3 * len(vals) // 4]
        hi = [x for x in J if x[n] > p75]
        lo = [x for x in J if x[n] < p25]
        q = sum(1 for x in hi if x["yards"] > 52.5) / len(hi)
        r = sum(1 for x in lo if x["yards"] > 52.5) / len(lo)
        print(f"  {n:<14} q {q:.3f}  r {r:.3f}  q-r {q - r:+.3f}")
    print("  shootout manages about +0.09 to +0.12 here; pass_heavy +0.14.")
    return 0


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--gate1", action="store_true")
    ap.add_argument("--seasons", default="2009-2025")
    a = ap.parse_args()
    f, _, l = a.seasons.partition("-")
    sys.exit(gate1(int(f), int(l or f)) if a.gate1 else 0)

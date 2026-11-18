#!/usr/bin/env python3
"""Grade the URPS reports' structured wagers against the box score, and write
the running scorecard into the Obsidian vault.

Weeks 7+ of the 2026 URPS reports carry a `wagers:` and a `ladders:` block in
their YAML frontmatter -- every priced row, per scenario, with the line, the
price, the model's predicted probability, EV, verdict and whether the report
recommended it. That frontmatter exists so the recommendations can be reviewed
after the fact; this is the review.

What it does, per run:

  * grades every `wagers:` and `ladders:` row against nflverse player-week stats
    (over/under the line; an integer over line is an "N+" threshold, so a count
    equal to N wins; anytime TD = rushing + receiving TDs >= 1; no stat row that
    week = void; no line/price = refused);
  * optionally writes `actual` and `result` back into each report's `wagers:`
    rows (--writeback; idempotent: rows already carrying `result:` are left alone);
  * scores the scenario forecast (beliefs/log.jsonl vs weekNN.outcomes.json) for
    every selected week that has an outcomes file;
  * joins recommended wagers to the real bet log (~/fanatics-bonus.jsonl) by
    player + stat + line, same Tuesday-to-Tuesday week;
  * writes one markdown scorecard (default: <vault>/URPS-Scorecard-2026.md).

Stdlib only (PyYAML is not a dependency here; the frontmatter rows are parsed
by hand -- both flow `- {k: v, ...}` and block `- k: v` styles occur).

Usage:
    python3 urps_scorecard.py --weeks 7-10
    python3 urps_scorecard.py --weeks 7-10 --writeback
"""

from __future__ import annotations

import argparse
import collections
import csv
import datetime as dt
import glob
import json
import os
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
EDGE = HERE.parent.parent
RAW = HERE.parent / "data" / "raw"
BELIEFS = EDGE / "beliefs"
VAULT = Path.home() / "Documents" / "adori" / "Edge"
BETLOG = Path.home() / "fanatics-bonus.jsonl"
SEASON = 2026
# Week 1 of 2026 opens Tuesday 2026-09-08; a week runs Tuesday to Tuesday.
WEEK1_TUESDAY = dt.date(2026, 9, 8)

SCENARIOS = ("efficient_offense", "shootout", "pass_heavy", "blowout_loss")
STATS = ("passing_yards", "rushing_yards", "receiving_yards", "receptions", "anytime_td", "passing_tds")
LEAN_VERDICTS = ("market-alone", "scenario-dependent")


# --------------------------------------------------------------- frontmatter

def val(x: str):
    x = x.strip()
    if x in ("null", "~", ""):
        return None
    if x in ("true", "false"):
        return x == "true"
    if len(x) > 1 and x[0] == x[-1] and x[0] in "\"'":
        return x[1:-1]
    try:
        return float(x) if "." in x else int(x)
    except ValueError:
        return x


def split_flow(s: str) -> list[str]:
    parts, cur, q = [], "", None
    for ch in s:
        if q:
            cur += ch
            if ch == q:
                q = None
        elif ch in "\"'":
            q = ch
            cur += ch
        elif ch == ",":
            parts.append(cur)
            cur = ""
        else:
            cur += ch
    if cur.strip():
        parts.append(cur)
    return parts


def frontmatter(path: str) -> tuple[list[str], int]:
    lines = open(path, encoding="utf-8").read().split("\n")
    if not lines or lines[0] != "---":
        raise ValueError("no frontmatter")
    return lines, lines.index("---", 1)


def parse_block(lines: list[str], end: int, key: str) -> list[tuple[dict, int, int]]:
    """Rows of a top-level list `key:` as (row, first_line, last_line)."""
    rows, i = [], 1
    while i < end:
        if lines[i].startswith(key + ":"):
            i += 1
            while i < end and (lines[i].startswith(" ") or lines[i].startswith("-")):
                s = lines[i].strip()
                if s.startswith("- {"):
                    d = {}
                    for p in split_flow(s[3:s.rindex("}")]):
                        k, v = p.split(":", 1)
                        d[k.strip()] = val(v)
                    rows.append((d, i, i))
                    i += 1
                elif s.startswith("- "):
                    first = i
                    d = {}
                    k, v = s[2:].split(":", 1)
                    d[k.strip()] = val(v)
                    i += 1
                    while i < end and lines[i].startswith("    "):
                        k, v = lines[i].strip().split(":", 1)
                        d[k.strip()] = val(v)
                        i += 1
                    rows.append((d, first, i - 1))
                else:
                    i += 1
            break
        i += 1
    return rows


def meta_of(lines: list[str], end: int) -> dict:
    meta = {}
    for ln in lines[1:end]:
        m = re.match(r"^(\w+):\s*(.*)$", ln)
        if m and m.group(1) in ("week", "away", "home", "game", "slate", "kickoff"):
            meta[m.group(1)] = val(m.group(2))
    return meta


# --------------------------------------------------------------------- grading

class BoxScore:
    def __init__(self, path: Path):
        self.rows = {}
        for r in csv.DictReader(open(path)):
            if r["season_type"] == "REG":
                self.rows[(r["player_display_name"].lower(), int(r["week"]), r["team"])] = r
        self.max_week = max(k[1] for k in self.rows)

    def actual(self, name, week, teams, outcome):
        for tm in teams:
            r = self.rows.get((str(name).lower(), week, tm))
            if r:
                f = lambda c: float(r[c] or 0)
                if outcome == "anytime_td":
                    return f("rushing_tds") + f("receiving_tds")
                if outcome in r:  # any plain box-score column: passing_tds, carries, ...
                    return f(outcome)
                return None
        return None

    def team(self, name, week, teams):
        for tm in teams:
            if (str(name).lower(), week, tm) in self.rows:
                return tm
        return None


def payout(price: float) -> float:
    return price / 100.0 if price > 0 else 100.0 / -price


def grade(d: dict, a, side="over") -> str:
    line = float(d["line"])
    if d.get("outcome") == "anytime_td" or d.get("stat") == "anytime_td":
        return "win" if a >= 1 else "loss"
    # an integer over line is an "N+" threshold (alt lines / ladders)
    if side == "over" and line == int(line):
        return "win" if a >= line else "loss"
    if a == line:
        return "push"
    return "win" if (a > line) == (side == "over") else "loss"


def grade_row(d: dict, week: int, teams, box: BoxScore) -> dict:
    rec = dict(d)
    if d.get("line") is None or d.get("side") is None or d.get("price") is None:
        rec.update(grade="refused", actual=None)
        return rec
    a = box.actual(d["player"], week, teams, d["outcome"])
    rec["actual"] = a
    if a is None:
        rec["grade"] = "void"
        return rec
    rec["grade"] = grade(d, a, d["side"])
    rec["profit"] = payout(d["price"]) if rec["grade"] == "win" else (-1.0 if rec["grade"] == "loss" else 0.0)
    return rec


def report_paths(vault: Path, week: int) -> list[str]:
    return sorted(glob.glob(str(vault / f"Week {week} {SEASON}" / "URPS-Report-*.md")))


def load_week(vault: Path, week: int, box: BoxScore):
    wagers, ladders = [], []
    for p in report_paths(vault, week):
        lines, end = frontmatter(p)
        meta = meta_of(lines, end)
        teams = [meta.get("away"), meta.get("home")]
        report = os.path.basename(p)[:-3]
        for d, _, _ in parse_block(lines, end, "wagers"):
            r = grade_row(d, week, teams, box)
            r.update(week=week, report=report, game=meta.get("game"),
                     team=box.team(d.get("player"), week, teams), teams=teams)
            wagers.append(r)
        for d, _, _ in parse_block(lines, end, "ladders"):
            if d.get("player") is None or d.get("line") is None:
                continue
            d = dict(d, outcome=d.get("stat") or d.get("outcome"), side="over")
            r = grade_row(d, week, teams, box) if d.get("price") is not None else dict(d, grade="void")
            if d.get("price") is None:
                a = box.actual(d["player"], week, teams, d["outcome"])
                r["actual"] = a
                if a is not None:
                    r["grade"] = grade(d, a)
            r.update(week=week, report=report)
            ladders.append(r)
    return wagers, ladders


def fmt_num(a):
    if a is None:
        return "null"
    return str(int(a)) if a == int(a) else str(a)


def writeback(vault: Path, week: int, box: BoxScore) -> int:
    """Append `actual` and `result` to each `wagers:` row lacking a `result`."""
    n = 0
    for p in report_paths(vault, week):
        lines, end = frontmatter(p)
        meta = meta_of(lines, end)
        teams = [meta.get("away"), meta.get("home")]
        edits = []
        for d, first, last in parse_block(lines, end, "wagers"):
            if "result" in d:
                continue
            g = grade_row(d, week, teams, box)
            edits.append((first, last, fmt_num(g.get("actual")), g["grade"]))
        for first, last, a, res in reversed(edits):
            if first == last and lines[first].rstrip().endswith("}"):
                s = lines[first].rstrip()
                lines[first] = s[:-1].rstrip() + f", actual: {a}, result: {res}}}"
            else:
                lines[last + 1:last + 1] = [f"    actual: {a}", f"    result: {res}"]
            n += 1
        if edits:
            open(p, "w", encoding="utf-8").write("\n".join(lines))
    return n


# --------------------------------------------------------------------- dedupe

def dedupe(wagers: list[dict]) -> list[dict]:
    """One row per (report, player, outcome, side, line); recommended if any
    scenario row recommended it; best-EV scenario carries predicted/price."""
    seen = {}
    for r in wagers:
        k = (r["report"], r.get("player"), r.get("outcome"), r.get("side"), r.get("line"))
        s = seen.setdefault(k, dict(r, rec_any=False, best_ev=None, scen=[]))
        s["rec_any"] |= bool(r.get("recommended"))
        ev = r.get("ev")
        if ev is not None and (s["best_ev"] is None or ev > s["best_ev"]):
            s.update(best_ev=ev, pred_best=r.get("predicted"), verdict_best=r.get("verdict"))
        s["scen"].append((r.get("scenario"), r.get("verdict"), ev, bool(r.get("recommended"))))
    U = list(seen.values())
    for u in U:
        u["lean"] = (not u["rec_any"]) and any(
            v in LEAN_VERDICTS and ev is not None and ev > 0 for _, v, ev, _ in u["scen"])
    return U


GRADED = ("win", "loss", "push")


def summary(rs: list[dict], label: str, pred_key="pred_best") -> str:
    gr = [r for r in rs if r["grade"] in GRADED]
    n = len(gr)
    if not n:
        return f"| {label} | 0 | | | | |"
    w = sum(r["grade"] == "win" for r in gr)
    l = sum(r["grade"] == "loss" for r in gr)
    prof = sum(r.get("profit", 0.0) for r in gr)
    pm = [r[pred_key] for r in gr if r.get(pred_key) is not None]
    pred = f"{100 * sum(pm) / len(pm):.0f}%" if pm else "n/a"
    return (f"| {label} | {n} | {w}-{l}-{n - w - l} | {100 * w / max(w + l, 1):.0f}% | {pred} | "
            f"{prof:+.1f}u ({100 * prof / n:+.0f}%) |")


HEAD = ("| Group | Graded | W-L-P | Hit rate | Avg predicted | Flat 1u P&L (ROI) |\n"
        "|---|---|---|---|---|---|")


# ---------------------------------------------------------------- bet log join

STAT_WORDS = {
    "passing_yards": ("pass",), "rushing_yards": ("rush",),
    "receiving_yards": ("rec yd", "receiving", "rec. yd", "rec yds", "yds rec", "rec"),
    "receptions": ("reception", "catches", "rec"), "anytime_td": ("td", "touchdown"),
}


def week_of(ts: str) -> int | None:
    try:
        t = dt.datetime.fromisoformat(ts.replace("Z", "+00:00"))
    except ValueError:
        return None
    t = t.astimezone(dt.timezone(dt.timedelta(hours=-5))).date()
    return (t - WEEK1_TUESDAY).days // 7 + 1


def load_bets(path: Path) -> list[dict]:
    if not path.exists():
        return []
    bets, settles = [], {}
    for ln in open(path):
        try:
            e = json.loads(ln)
        except ValueError:
            continue
        if e.get("kind") == "bet":
            bets.append(dict(e["bet"], id=e["id"], week=week_of(e["time"])))
        elif e.get("kind") == "settle":
            settles[e["id"]] = e.get("result")
    for b in bets:
        b["result"] = settles.get(b["id"])
    return bets


def placed(u: dict, bets: list[dict]) -> str:
    last = str(u.get("player", "")).split()[-1].lower()
    nums = lambda s: [float(x) for x in re.findall(r"\d+(?:\.\d+)?", s)]
    for b in bets:
        sel = str(b.get("selection", "")).lower()
        if b.get("week") not in (u["week"], u["week"] - 1) or last not in sel:
            continue
        if not any(w in sel for w in STAT_WORDS.get(u.get("outcome"), ())):
            continue
        if u.get("line") is None or not any(abs(x - float(u["line"])) <= 0.51 for x in nums(sel)):
            continue
        return f"yes ({b.get('result') or 'open'})"
    return "no"


# ------------------------------------------------------------ named index leans

def index_leans(vault: Path, week: int) -> list[tuple[str, str]]:
    """(report, lean text) from the week index's table column headed 'Lean'."""
    p = vault / f"Week {week} {SEASON}" / f"URPS-Index-{SEASON}-W{week:02d}.md"
    if not p.exists():
        return []
    out, col = [], None
    for ln in open(p, encoding="utf-8"):
        if not ln.startswith("|"):
            col = None
            continue
        cells = [c.strip() for c in ln.strip().strip("|").split("|")]
        if col is None:
            col = next((i for i, c in enumerate(cells) if c.lower() == "lean"), -1)
            continue
        if col < 0 or col >= len(cells) or set(cells[0]) <= set("-: "):
            continue
        m = re.search(r"\[\[(URPS-Report-[^\]|]+)", ln)
        if m and cells[col] and cells[col].lower() not in ("-", "—", "none", "n/a"):
            for piece in re.split(r";|,(?![^()]*\))", cells[col]):
                if piece.strip():
                    out.append((m.group(1), piece.strip()))
    return out


def match_lean(text: str, rows: list[dict]) -> dict | None:
    t = text.lower()
    nums = [float(x) for x in re.findall(r"(?<![+\-\d])(\d+(?:\.\d+)?)", re.sub(r"\([+\-]\d+\)", "", t))]
    for r in rows:
        last = str(r.get("player", "")).split()[-1].lower()
        if last not in t or r.get("line") is None:
            continue
        o = r.get("outcome")
        kw = {"passing_yards": "pass", "rushing_yards": "rush", "anytime_td": "td"}.get(o)
        if kw and kw not in t:
            continue
        if o in ("receiving_yards", "receptions") and not re.search(r"rec|catch|yd", t):
            continue
        if o == "receptions" and float(r["line"]) > 15:
            continue
        if any(abs(x - float(r["line"])) <= 0.51 for x in nums):
            return r
    return None


# --------------------------------------------------------- scenario identification

def scenario_rows(weeks: list[int]) -> list[dict]:
    occ = {}
    for wk in weeks:
        p = BELIEFS / str(SEASON) / f"week{wk:02d}.outcomes.json"
        if p.exists():
            for r in json.load(open(p))["rows"]:
                if r["status"] == "settled":
                    occ[(r["game_id"], r["team"], r["scenario"])] = r["occurred"]
    latest = {}
    for ln in open(BELIEFS / "log.jsonl"):
        e = json.loads(ln)
        if e.get("kind") != "belief":
            continue
        p = e["prediction"]
        if p.get("season") != SEASON or p.get("week") not in weeks:
            continue
        latest[(p["game_id"], p.get("team"), p["scenario"])] = p
    out = []
    for k, p in latest.items():
        if k in occ:
            out.append(dict(week=p["week"], game_id=k[0], team=k[1], scenario=k[2],
                            belief=p["belief"], base=p.get("s_base_rate"), occurred=occ[k]))
    return out, occ


def scen_table(rows, label):
    eng = [r for r in rows if r["base"] is not None and abs(r["belief"] - r["base"]) > 1e-9]
    if not eng:
        return f"| {label} | 0 | | | | |"
    o = sum(r["occurred"] for r in eng)
    right = sum((r["belief"] > r["base"]) == bool(r["occurred"]) for r in eng)
    bri = sum((r["belief"] - r["occurred"]) ** 2 for r in eng) / len(eng)
    brb = sum((r["base"] - r["occurred"]) ** 2 for r in eng) / len(eng)
    return (f"| {label} | {len(eng)} | {o} ({100 * o / len(eng):.0f}%) | "
            f"{sum(r['belief'] for r in eng) / len(eng):.3f} | {right} of {len(eng)} ({100 * right / len(eng):.0f}%) | "
            f"{bri:.3f} vs {brb:.3f} |")


# --------------------------------------------------------------------- report

def build(weeks: list[int], vault: Path, box: BoxScore, bets: list[dict]) -> str:
    W, L = [], []
    for wk in weeks:
        w, l = load_week(vault, wk, box)
        W += w
        L += l
    U = dedupe(W)
    rec = [u for u in U if u["rec_any"]]
    notrec = [u for u in U if not u["rec_any"]]
    leans = [u for u in U if u["lean"]]
    span = f"{weeks[0]}-{weeks[-1]}"
    o = []

    # headline numbers for the abstract
    def wl(rs):
        g = [r for r in rs if r["grade"] in GRADED]
        return sum(r["grade"] == "win" for r in g), sum(r["grade"] == "loss" for r in g)
    rw, rl = wl(rec)
    per_week = ", ".join(f"Wk {wk}: {'-'.join(map(str, wl([u for u in rec if u['week'] == wk])))}" for wk in weeks)
    lw, ll = wl(leans)
    srows, occ = scenario_rows(weeks)
    eng = [r for r in srows if r["base"] is not None and abs(r["belief"] - r["base"]) > 1e-9]
    dright = sum((r["belief"] > r["base"]) == bool(r["occurred"]) for r in eng)

    o.append(f"""---
type: urps-scorecard
season: {SEASON}
weeks: [{', '.join(map(str, weeks))}]
generated: {dt.date.today().isoformat()}
generator: edge/model/analysis/urps_scorecard.py
---

# URPS Scorecard, {SEASON} Weeks {span}

> [!abstract] READ FIRST
> Every priced row in the Week {span} URPS reports, graded against the nflverse box score and regenerated by `make urps-scorecard`. Flat 1-unit stakes at the quoted price: this grades the model's calls, not your bankroll (`edgectl ledger period` has real P&L). **Recommended: {rw}-{rl} ({per_week}). Structural leans: {lw}-{ll}. Scenario forecast direction right {dright} of {len(eng)} engaged rows.** Samples are small and the rows are correlated within a game. Treat this as a trend read, not proof.

## Contents
- [[#Results]]
- [[#By week]]
- [[#By verdict]]
- [[#By outcome type, recommended only]]
- [[#Calibration]]
- [[#Leans]]
- [[#Aces and de-facto lines]]
- [[#Scenario identification]]
- [[#Hit rate within the priced scenario]]
- [[#Every recommended wager]]
- [[#Method and limits]]

## Results

Unique wagers, deduped across scenarios.

{HEAD}
{summary(rec, '**Recommended**')}
{summary(leans, 'Leans (not recommended, +EV somewhere)')}
{summary(notrec, 'All not recommended')}
{summary(U, 'Every priced row')}
""")
    vc = collections.Counter(u["grade"] for u in U)
    o.append(f"Ungraded: {vc['void']} void (no stat row: did not play), {vc['refused']} refused (no line or price).\n")

    o.append("## By week\n\n" + HEAD)
    for wk in weeks:
        wu = [u for u in U if u["week"] == wk]
        o.append(summary([u for u in wu if u["rec_any"]], f"Week {wk} recommended"))
        o.append(summary([u for u in wu if u["lean"]], f"Week {wk} leans"))
        o.append(summary([u for u in wu if not u["rec_any"]], f"Week {wk} not recommended"))

    o.append("\n## By verdict\n\nVerdict of each wager's best-EV scenario row.\n\n" + HEAD)
    for v in sorted({str(u.get("verdict_best")) for u in U}):
        o.append(summary([u for u in U if str(u.get("verdict_best")) == v], v))

    o.append("\n## By outcome type, recommended only\n\n" + HEAD)
    for st in STATS:
        rs = [u for u in rec if u.get("outcome") == st]
        if rs:
            o.append(summary(rs, st))

    o.append("\n## Calibration\n\nEvery priced unique wager, bucketed by its best-scenario predicted probability.\n\n"
             "| Predicted | Graded | Avg predicted | Hit rate | Gap |\n|---|---|---|---|---|")
    for lo, hi in ((0, .3), (.3, .4), (.4, .5), (.5, .6), (.6, .7), (.7, 1.01)):
        rs = [u for u in U if u["grade"] in ("win", "loss") and u.get("pred_best") is not None and lo <= u["pred_best"] < hi]
        if rs:
            p = sum(u["pred_best"] for u in rs) / len(rs)
            h = sum(u["grade"] == "win" for u in rs) / len(rs)
            o.append(f"| {lo:.0%}-{min(hi, 1):.0%} | {len(rs)} | {p:.0%} | {h:.0%} | {100 * (h - p):+.0f} pts |")

    o.append("\n## Leans\n\nStructural definition: verdict market-alone or scenario-dependent, EV > 0 in at least one "
             "scenario, never recommended. A superset of the leans the reports name.\n\n" + HEAD)
    o.append(summary(leans, "All leans"))
    for wk in weeks:
        o.append(summary([u for u in leans if u["week"] == wk], f"Week {wk}"))
    named = []
    for wk in weeks:
        for report, text in index_leans(vault, wk):
            r = match_lean(text, [u for u in U if u["report"] == report])
            named.append((wk, report, text, r))
    if named:
        o.append("\n### Leans the weekly index names\n\n| Week | Lean (as written) | Matched row | Actual | Result |\n|---|---|---|---|---|")
        for wk, report, text, r in named:
            if r:
                o.append(f"| {wk} | {text} | {r['player']} {r['side']} {r['line']} {r['outcome']} | {fmt_num(r.get('actual'))} | {r['grade']} |")
            else:
                o.append(f"| {wk} | {text} | no priced row matched | | |")
        m = [r for _, _, _, r in named if r and r["grade"] in GRADED]
        if m:
            o.append("\n" + HEAD + "\n" + summary(m, "Index-named leans (matched)"))

    o.append("\n## Aces and de-facto lines\n\nThe `ladders:` block, every line graded as an over at the listed price "
             "(an integer line is an N+ threshold).\n\n" + HEAD.replace("Avg predicted", "n/a"))
    o.append(summary(L, "All ladder lines"))
    for wk in weeks:
        o.append(summary([r for r in L if r["week"] == wk], f"Week {wk}"))
    for st in STATS:
        rs = [r for r in L if r.get("outcome") == st]
        if rs:
            o.append(summary(rs, st))

    sweeks = sorted({r["week"] for r in srows})
    o.append(f"\n## Scenario identification\n\nWeeks with a settled outcomes file: {', '.join(map(str, sweeks)) or 'none'}. "
             "Engaged = the belief differs from the base rate. Direction right = the belief moved above base and the scenario "
             "occurred, or below and it did not. Definitions: shootout total > 50, blowout_loss lost by > 7, "
             "pass_heavy PROE > +3.0, efficient_offense success rate > .46.\n\n"
             "| Group | Engaged rows | Occurred | Avg belief | Direction right | Brier (ours vs base rate) |\n|---|---|---|---|---|---|")
    o.append(scen_table(srows, "**All engaged**"))
    for wk in sweeks:
        o.append(scen_table([r for r in srows if r["week"] == wk], f"Week {wk}"))
    for s in SCENARIOS:
        o.append(scen_table([r for r in srows if r["scenario"] == s], s))
    ab = [r for r in srows if r["base"] is not None and abs(r["belief"] - r["base"]) <= 1e-9]
    if ab:
        o.append(f"\nAbstained (belief = base rate): {len(ab)} rows, occurred {sum(r['occurred'] for r in ab)} "
                 f"({100 * sum(r['occurred'] for r in ab) / len(ab):.0f}%).")
    o.append("\n| Call | Rows | Occurred |\n|---|---|---|")
    for lab, f in (("Called likely (belief 0.45+)", lambda b: b >= .45), ("Middle", lambda b: .20 < b < .45),
                   ("Called unlikely (0.20 or lower)", lambda b: b <= .20)):
        rs = [r for r in eng if f(r["belief"])]
        if rs:
            oc = sum(r["occurred"] for r in rs)
            o.append(f"| {lab} | {len(rs)} | {oc} ({100 * oc / len(rs):.0f}%) |")

    # within the priced scenario
    o.append("\n## Hit rate within the priced scenario\n\nRecommended and lean wagers, by the scenario each row was "
             "priced under (not deduped across scenarios). A row counts once per scenario it was priced in.\n\n"
             "| Scenario | Kind | Rows | Wins | Hit rate |\n|---|---|---|---|---|")
    lean_keys = {(u["report"], u.get("player"), u.get("outcome"), u.get("side"), u.get("line")) for u in leans}
    srow = []
    for r in W:
        if r["grade"] not in ("win", "loss") or not r.get("scenario"):
            continue
        k = (r["report"], r.get("player"), r.get("outcome"), r.get("side"), r.get("line"))
        kind = "recommended" if r.get("recommended") else (
            "lean" if k in lean_keys and r.get("verdict") in LEAN_VERDICTS and (r.get("ev") or 0) > 0 else None)
        if not kind:
            continue
        a, h = r["report"].split(f"W{r['week']:02d}-")[-1].split("-")[:2]
        gid = f"{SEASON}_{r['week']:02d}_{a}_{h}"
        oc = occ.get((gid, None if r["scenario"] == "shootout" else r.get("team"), r["scenario"]))
        srow.append(dict(r, kind=kind, occurred=oc))
    hr = lambda rs: (len(rs), sum(x["grade"] == "win" for x in rs))
    for kind in ("recommended", "lean"):
        for s in SCENARIOS:
            n, w = hr([x for x in srow if x["kind"] == kind and x["scenario"] == s])
            if n:
                o.append(f"| {s} | {kind} | {n} | {w} | {100 * w / n:.0f}% |")
    o.append("\n| Scenario | Stat | Rows | Wins | Hit rate |\n|---|---|---|---|---|")
    for s in SCENARIOS:
        for st in STATS:
            n, w = hr([x for x in srow if x["scenario"] == s and x.get("outcome") == st])
            if n:
                o.append(f"| {s} | {st} | {n} | {w} | {100 * w / n:.0f}% |")
    o.append("\nConditional on whether the priced scenario actually occurred:\n\n"
             "| Scenario | Occurred? | Rows | Wins | Hit rate |\n|---|---|---|---|---|")
    for s in SCENARIOS:
        for oc in (True, False):
            n, w = hr([x for x in srow if x["scenario"] == s and x["occurred"] is oc])
            if n:
                o.append(f"| {s} | {'yes' if oc else 'no'} | {n} | {w} | {100 * w / n:.0f}% |")
    o.append(f"\nRows with no scenario outcome (week not settled, or team unmatched): "
             f"{sum(x['occurred'] is None for x in srow)} of {len(srow)}.")

    o.append("\n## Every recommended wager\n\n| Week | Report | Wager | Price | Predicted | Actual | Result | Placed? |\n"
             "|---|---|---|---|---|---|---|---|")
    for u in sorted(rec, key=lambda u: (u["week"], u["report"], str(u.get("player")))):
        pr = u.get("price")
        price = "" if pr is None else ("%+d" % pr if isinstance(pr, int) else str(pr))
        pred = "" if u.get("pred_best") is None else "%.0f%%" % (100 * u["pred_best"])
        o.append(f"| {u['week']} | [[{u['report']}]] | {u.get('player')} {u.get('side')} {u.get('line')} {u.get('outcome')} | "
                 f"{price} | {pred} | {fmt_num(u.get('actual'))} | {u['grade']} | {placed(u, bets)} |")

    o.append(f"""
## Method and limits

- **Source:** the `wagers:` and `ladders:` frontmatter of `Week N {SEASON}/URPS-Report-*.md`. Weeks 1-6 have no structured rows. Week 8 has only the reports that were written.
- **Grading:** nflverse `stats_player_week_{SEASON}.csv` (box score through Week {box.max_week}). Over/under vs the line. **An integer over line is an "N+" threshold**: a count equal to N wins. Anytime TD = rushing + receiving TDs of at least 1. Push (actual = a .5-less line) returns the stake. **void** = no stat row that week (did not play); **refused** = no line or price. Neither counts.
- **Dedupe:** the same wager appears under several scenarios; tables count each (report, player, outcome, side, line) once, recommended if any scenario row recommended it. The predicted probability shown is the best-EV scenario's.
- **Placed?** matched by last name + stat keyword + a number within 0.5 of the line in `~/fanatics-bonus.jsonl`, same or prior Tuesday-to-Tuesday week. A match can be an SGP leg.
- **Write-back:** `--writeback` adds `actual` and `result` to each `wagers:` row that lacks them; ladders are not written back.
- **Not covered:** SGPs, parlays, anytime-TD and game-line bets written only in prose.
- **Reproduce:** `make urps-scorecard WEEKS={span}` (add `WRITEBACK=1` to annotate the reports).
""")
    return "\n".join(o)


def parse_weeks(spec: str) -> list[int]:
    if "-" in spec:
        a, b = spec.split("-")
        return list(range(int(a), int(b) + 1))
    return [int(x) for x in spec.split(",")]


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--weeks", required=True, help="range or list, e.g. 7-10 or 7,9,10")
    ap.add_argument("--vault", type=Path, default=VAULT)
    ap.add_argument("--stats", type=Path, default=RAW / f"stats_player_week_{SEASON}.csv")
    ap.add_argument("--betlog", type=Path, default=BETLOG)
    ap.add_argument("--out", type=Path, help=f"default <vault>/URPS-Scorecard-{SEASON}.md")
    ap.add_argument("--writeback", action="store_true", help="write actual/result into each report's wagers: rows")
    args = ap.parse_args(argv)

    weeks = parse_weeks(args.weeks)
    box = BoxScore(args.stats)
    late = [w for w in weeks if w > box.max_week]
    if late:
        raise SystemExit(f"box score only reaches Week {box.max_week}; run `make data` (weeks {late} not played/cached)")
    if args.writeback:
        for wk in weeks:
            print(f"week {wk}: wrote actual/result to {writeback(args.vault, wk, box)} rows")
    out = args.out or args.vault / f"URPS-Scorecard-{SEASON}.md"
    out.write_text(build(weeks, args.vault, box, load_bets(args.betlog)), encoding="utf-8")
    print(f"wrote {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

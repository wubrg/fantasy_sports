#!/usr/bin/env python3
"""Extract the DraftSheets workbook into the draft room's normalized CSV.

DraftSheets ships a single macro-free xlsx that recomputes everything from raw
stat projections against a Scoring sheet the owner fills in. That makes it the
second source, after Ciely, that publishes components rather than a bare point
total — so this script does what extract_ciely.py does: it recomputes fantasy
points under the league's own scoring and writes both numbers, keeping the
difference auditable instead of inheriting the vendor's arithmetic on trust.

The workbook's Scoring sheet was already set to this league (12 teams, $200,
0.5 PPR at RB/WR/TE, 25 pass yards per point, 4 pass TD, -1 INT, 6 rush/rec
TD), so the recompute is expected to agree closely. It is still done rather
than assumed: the Scoring sheet is editable and a future export could arrive
with someone else's settings, in which case the mismatch prints instead of
silently poisoning the board.

Two conversions matter and are the reason this file exists rather than a
straight column copy:

  Yards.   DraftSheets expresses yardage as "yards per 1 point" (25, 10) while
           the league's scoring is points per yard (0.04, 0.1). The workbook's
           own formulas divide by that cell; ours multiply by the reciprocal.
           Copying 25 as a coefficient would be off by a factor of 625.

  Fumbles. DraftSheets carries FL -2 and folds it into its own totals. The
           league's reference scoring — the SCORING table in extract_ciely.py
           and Components.Total in sources.go — has no fumble term at all,
           because Ciely's workbook does not project fumbles and the board
           compares sources against each other. So league_points here is
           computed WITHOUT fumbles, deliberately, so a DraftSheets total and
           a Ciely total mean the same thing. The fumble projection is not
           discarded: fumbles_lost and fumble_points ride along as their own
           columns, and workbook_points (the vendor's own half-PPR total, which
           does include fumbles) is written beside them, so the term can be
           folded back in downstream without re-extracting.

Sheets read: Aggregate (the spine — position, positional rank, projection band,
VBD, auction), the four position sheets (raw components), RISK (missed-games
adjustment), Rookies (rookie flag), FLEX (flex composition, reported only).
ECR is skipped — it is FantasyPros consensus, already ingested from the source.
DraftSheet is skipped — it is a print view derived from Aggregate.

One quirk worth knowing before reading proj_avg: the Aggregate band is NOT the
season total. The workbook's formula is `position_sheet_total / 17 * (16 -
missed_games)`, so it is risk-discounted and benchmarked to a 16-game season.
league_points is the undiscounted 17-game total, so proj_avg sits well below it
by design — for a healthy player the ratio is about 16/17, and further below
for anyone RISK marks. Both are written; do not difference them and call it a
scoring error.

Usage:
    python3 extract_draftsheets.py <workbook.xlsx> <out.csv>

Dependency-free on purpose: parses the xlsx zip/XML directly so this runs on
a stock Python 3 with no pip install.
"""

from __future__ import annotations

import csv
import re
import sys
import xml.etree.ElementTree as ET
import zipfile

NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
RNS = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"

# Hit or Miss scoring, from the league's Sleeper scoring_settings, stated as
# points per unit. Deliberately identical to the SCORING table in
# extract_ciely.py: the two sources are only comparable if they are scored by
# the same rulebook. Fumbles are absent on purpose — see the module docstring.
SCORING = {
    "pass_yards": 0.04,
    "pass_td": 4.0,
    "interceptions": -1.0,
    "rush_yards": 0.1,
    "rush_td": 6.0,
    "receptions": 0.5,
    "recv_yards": 0.1,
    "recv_td": 6.0,
}

# What the workbook's Scoring sheet should say if it is still set to this
# league. Keyed by the label in column A. Yard rows are the divisor form
# (yards per point) and are inverted before comparison.
EXPECTED_SETTINGS = {
    "PassYDS": ("pass_yards", "per_point"),
    "PassTDs": ("pass_td", "direct"),
    "INTS": ("interceptions", "direct"),
    "RushYDS": ("rush_yards", "per_point"),
    "RushTDS": ("rush_td", "direct"),
    "RB PPR": ("receptions", "direct"),
    "WR PPR": ("receptions", "direct"),
    "TE PPR": ("receptions", "direct"),
    "RecYDS": ("recv_yards", "per_point"),
    "RecTDS": ("recv_td", "direct"),
}

# The league's own basis, from HitOrMiss() in sources.go. Compared against the
# workbook's roster settings so a rescaling need is visible rather than silent:
# auction dollars and VBD are only meaningful relative to the roster they were
# computed for.
LEAGUE_BASIS = {"#TEAMS:": 12, "QB:": 1, "RB:": 2, "WR:": 3, "TE:": 1, "FLEX:": 1}

# Aggregate lays the four positions out side by side. Each block starts one
# column left of its PLAYER header, at the positional-rank label (QB1, RB2...),
# which is the only place position and positional rank are stated.
AGG_LABEL, AGG_PLAYER, AGG_TEAM, AGG_BYE = 0, 1, 2, 3
AGG_LOW, AGG_AVG, AGG_HIGH = 5, 6, 7
AGG_ZSCORE, AGG_VBD, AGG_AUCTION, AGG_TIER, AGG_MISSED = 8, 9, 10, 11, 12
AGG_WIDTH = 13

# Position-sheet column order. Several headers repeat within a sheet ("ATT",
# "YDS", "TDS" appear under both rushing and receiving), so columns are taken
# positionally and the header is verified against this list rather than looked
# up by name.
LAYOUTS = {
    "QB": ["Player", "Team", "pass_att", "pass_cmp", "pass_yards", "pass_td",
           "interceptions", "rush_att", "rush_yards", "rush_td", "fumbles_lost",
           "FPTS", "pass_1d", "rush_1d", "LOW", "AVG", "HIGH"],
    "RB": ["Player", "Team", "rush_att", "rush_yards", "rush_td", "receptions",
           "recv_yards", "recv_td", "fumbles_lost", "FPTS", "rush_1d", "rec_1d",
           "LOW", "AVG", "HIGH"],
    "WR": ["Player", "Team", "receptions", "recv_yards", "recv_td", "rush_att",
           "rush_yards", "rush_td", "fumbles_lost", "FPTS", "rush_1d", "rec_1d",
           "LOW", "AVG", "HIGH"],
    "TE": ["Player", "Team", "receptions", "recv_yards", "recv_td",
           "fumbles_lost", "FPTS", "rec_1d", "LOW", "AVG", "HIGH"],
}

# Header text as it actually appears, position by position. Checked so a
# reshuffled export fails loudly instead of scoring the wrong column.
HEADERS = {
    "QB": ["Player", "Team", "ATT", "CMP", "YDS", "TDS", "INTS", "ATT", "YDS",
           "TDS", "FL", "FPTS", "Pa1D", "Ru1D", "LOW", "AVG", "HIGH"],
    "RB": ["Player", "Team", "ATT", "YDS", "TDS", "REC", "YDS", "TDS", "FL",
           "FPTS", "Ru1D", "Rec1D", "LOW", "AVG", "HIGH"],
    "WR": ["Player", "Team", "REC", "YDS", "TDS", "ATT", "YDS", "TDS", "FL",
           "FPTS", "Ru1D", "Rec1D", "LOW", "AVG", "HIGH"],
    "TE": ["Player", "Team", "REC", "YDS", "TDS", "FL", "FPTS", "Rec1D", "LOW",
           "AVG", "HIGH"],
}

POSITIONS = ["QB", "RB", "WR", "TE"]

# DraftSheets' published FPTS column is full PPR, not the workbook's own
# half-PPR total. Kept as the vendor's published number so points_delta shows
# the scoring gap the board is correcting for, exactly as the Ciely extractor
# shows his interception gap.
VENDOR_PPR = 1.0

# Fumble value from the workbook's Scoring sheet, held only to price the
# fumble_points column that league_points excludes.
VENDOR_FUMBLE = -2.0

# The label DraftSheets uses for the blank filler row under each header.
FILLER = "Â "


class Book:
    """Minimal read-only xlsx reader for cached cell values."""

    def __init__(self, path):
        self.z = zipfile.ZipFile(path)
        self.shared = []
        try:
            sst = ET.fromstring(self.z.read("xl/sharedStrings.xml"))
            for si in sst:
                self.shared.append("".join(t.text or "" for t in si.iter(NS + "t")))
        except KeyError:
            pass

        wb = ET.fromstring(self.z.read("xl/workbook.xml"))
        rels = ET.fromstring(self.z.read("xl/_rels/workbook.xml.rels"))
        targets = {r.get("Id"): r.get("Target") for r in rels}
        self.sheets = {}
        for s in wb.find(NS + "sheets"):
            target = targets[s.get(RNS + "id")]
            self.sheets[s.get("name")] = target if target.startswith("xl/") else "xl/" + target

    def grid(self, name):
        """Return the sheet as a list of row-lists, column-aligned."""
        root = ET.fromstring(self.z.read(self.sheets[name]))
        rows = []
        for row in root.iter(NS + "row"):
            cells = {}
            for c in row.iter(NS + "c"):
                col = re.match(r"[A-Z]+", c.get("r")).group(0)
                cells[col] = self._value(c)
            rows.append(cells)

        def order(col):
            n = 0
            for ch in col:
                n = n * 26 + ord(ch) - 64
            return n

        cols = sorted({c for r in rows for c in r}, key=order)
        return [[r.get(c) for c in cols] for r in rows]

    def _value(self, c):
        kind = c.get("t")
        inline = c.find(NS + "is")
        if inline is not None:
            return "".join(x.text or "" for x in inline.iter(NS + "t"))
        v = c.find(NS + "v")
        if v is None:
            return None
        if kind == "s":
            return self.shared[int(v.text)]
        if kind == "b":
            return v.text == "1"
        try:
            return float(v.text)
        except (TypeError, ValueError):
            return v.text


def num(v):
    """Cells arrive as float, str or None; anything unparseable is a zero."""
    if isinstance(v, bool):
        return 0.0
    if isinstance(v, (int, float)):
        return float(v)
    try:
        return float(str(v).strip())
    except (TypeError, ValueError):
        return 0.0


def text(v):
    return "" if v is None else str(v).strip()


def read_scoring(book):
    """Read the workbook's Scoring sheet and restate it in league units.

    Returns (converted, mismatches). The conversion is the point: the sheet
    stores yardage as a divisor, so a 25 in PassYDS means 0.04 points per yard.
    """
    grid = book.grid("Scoring")
    raw = {}
    for row in grid:
        label = text(row[0] if row else None)
        if label in EXPECTED_SETTINGS and len(row) > 1 and row[1] is not None:
            raw[label] = num(row[1])

    converted, mismatches = {}, []
    for label, (key, form) in EXPECTED_SETTINGS.items():
        if label not in raw:
            mismatches.append(f"{label}: missing from Scoring sheet")
            continue
        value = raw[label]
        if form == "per_point":
            # Guard the divisor before inverting: a zeroed cell would otherwise
            # blow up rather than report itself.
            if value == 0:
                mismatches.append(f"{label}: zero yards-per-point, cannot invert")
                continue
            value = 1.0 / value
        converted[label] = value
        if abs(value - SCORING[key]) > 1e-9:
            mismatches.append(f"{label}: workbook {value:g} vs league {SCORING[key]:g}")

    # Roster settings live in the same sheet, laid out as a label row above a
    # value row rather than as label/value pairs.
    basis = {}
    for i, row in enumerate(grid[:-1]):
        for j, cell in enumerate(row):
            if text(cell) in LEAGUE_BASIS and grid[i + 1][j] is not None:
                basis.setdefault(text(cell), num(grid[i + 1][j]))
    for label, want in LEAGUE_BASIS.items():
        got = basis.get(label)
        if got is not None and int(got) != want:
            mismatches.append(f"{label} workbook {int(got)} vs league {want}")

    return converted, mismatches


def read_position_sheet(book, position):
    """Return {player name: component dict} for one position sheet.

    Each player occupies three rows — the mean projection, then a `high` and a
    `low` variant that carry no name in column A. Only the named row is kept;
    the band the board reads comes from Aggregate, which has already applied
    the risk discount to those same high/low numbers.
    """
    grid = book.grid(position)
    layout, header = LAYOUTS[position], HEADERS[position]
    got = [text(v) for v in grid[0][:len(header)]]
    if got != header:
        sys.exit(f"extract_draftsheets: {position} sheet header changed\n"
                 f"  expected {header}\n  found    {got}")

    players = {}
    for row in grid[1:]:
        name = text(row[0] if row else None)
        if not name or name == FILLER:
            continue
        cells = dict(zip(layout, row))
        players[name] = {
            "team": text(cells.get("Team")),
            "vendor_points": num(cells.get("FPTS")),
            "workbook_points": num(cells.get("AVG")),
            "fumbles_lost": num(cells.get("fumbles_lost")),
            **{k: num(cells.get(k)) for k in SCORING},
        }
    if not players:
        sys.exit(f"extract_draftsheets: no players on the {position} sheet")
    return players


def read_risk(book):
    """RISK is keyed by positional-rank label (QB1, RB2...), not by name.

    Its single column is a missed-games adjustment, which is what Aggregate
    subtracts from a 16-game season when it discounts a projection.
    """
    risk = {}
    for row in book.grid("RISK")[1:]:
        label = text(row[0] if row else None)
        if re.fullmatch(r"[A-Z]+\d+", label) and len(row) > 1 and row[1] is not None:
            risk[label] = num(row[1])
    return risk


def read_rookies(book):
    """Rookies are listed by name with a rookie-class rank (rookie RB1, WR1...).

    That rank is within the rookie class only and has nothing to do with the
    overall positional rank on Aggregate, so it is joined by name and kept in
    its own column rather than merged into pos_rank.
    """
    rookies = {}
    for row in book.grid("Rookies")[1:]:
        name = text(row[0] if row else None)
        if name and name != FILLER:
            rookies[name] = text(row[1] if len(row) > 1 else None)
    return rookies


def read_flex(book):
    """FLEX names no players — it reports how the 12 flex slots fill.

    Reported in the summary only, because the split (how many flex slots go to
    RB vs WR vs TE) is what sets the positional baselines the VBD and auction
    columns are measured from.
    """
    grid = book.grid("FLEX")
    if not grid:
        return {}
    head = grid[0]
    counts = {}
    for j, cell in enumerate(head[:-1]):
        label = text(cell).rstrip(":")
        if label in POSITIONS and head[j + 1] is not None:
            counts[label] = int(num(head[j + 1]))
    return counts


def aggregate_blocks(header):
    """Locate each side-by-side position block by its PLAYER header cell."""
    return [i - 1 for i, v in enumerate(header) if text(v) == "PLAYER" and i > 0]


def league_points(stats):
    return sum(weight * stats.get(key, 0.0) for key, weight in SCORING.items())


def extract(path):
    book = Book(path)
    scoring, mismatches = read_scoring(book)
    components = {pos: read_position_sheet(book, pos) for pos in POSITIONS}
    risk = read_risk(book)
    rookies = read_rookies(book)

    grid = book.grid("Aggregate")
    starts = aggregate_blocks(grid[1])
    if len(starts) != len(POSITIONS):
        sys.exit(f"extract_draftsheets: expected {len(POSITIONS)} Aggregate blocks, "
                 f"found {len(starts)}")

    rows, unmatched, risk_conflicts = [], [], 0
    for start in starts:
        for raw in grid[2:]:
            block = raw[start:start + AGG_WIDTH]
            if len(block) < AGG_WIDTH:
                continue
            label = text(block[AGG_LABEL])
            name = text(block[AGG_PLAYER])
            m = re.fullmatch(r"([A-Z]+)(\d+)", label)
            if not m or not name:
                continue
            position, pos_rank = m.group(1), int(m.group(2))
            if position not in components:
                continue

            stats = components[position].get(name)
            if stats is None:
                # Aggregate pulls its names through ECR, so a name it carries
                # that the position sheet does not is a join failure worth
                # counting rather than a row worth inventing numbers for.
                unmatched.append(f"{label} {name}")
                continue

            ours = league_points(stats)
            vendor = stats["vendor_points"]

            # Aggregate already carries the missed-games figure it used; RISK is
            # the table it read it from. Prefer Aggregate's (it is what actually
            # priced this row) and count any disagreement.
            agg_missed = num(block[AGG_MISSED])
            risk_missed = risk.get(label)
            if risk_missed is not None and abs(risk_missed - agg_missed) > 0.01:
                risk_conflicts += 1
            missed = agg_missed if agg_missed else (risk_missed or 0.0)

            rows.append({
                "source": "draftsheets",
                "position": position,
                "pos_rank": pos_rank,
                "player": name,
                "team": text(block[AGG_TEAM]),
                "bye": int(num(block[AGG_BYE])),
                "rookie": int(name in rookies),
                "rookie_rank": rookies.get(name, ""),
                "proj_low": round(num(block[AGG_LOW]), 2),
                "proj_avg": round(num(block[AGG_AVG]), 2),
                "proj_high": round(num(block[AGG_HIGH]), 2),
                "draftsheets_points": round(vendor, 2),
                "league_points": round(ours, 2),
                "points_delta": round(ours - vendor, 2),
                # The workbook's own total under its Scoring sheet. Equals
                # league_points plus the fumble term, which is the whole audit:
                # if these two differ by anything other than fumble_points, the
                # scoring conversion above is wrong.
                "workbook_points": round(stats["workbook_points"], 2),
                "zscore_points": round(num(block[AGG_ZSCORE]), 2),
                "vbd": round(num(block[AGG_VBD]), 2),
                "auction_value": round(num(block[AGG_AUCTION]), 2),
                "tier": int(num(block[AGG_TIER])),
                "missed_games": round(missed, 2),
                # Aggregate discounts against a 16-game season, not 17 — its
                # formula is total/17*(16-missed). Reported the same way so
                # projected_games and proj_avg tell one consistent story.
                "projected_games": round(16.0 - missed, 2),
                "fumbles_lost": round(stats["fumbles_lost"], 2),
                "fumble_points": round(stats["fumbles_lost"] * VENDOR_FUMBLE, 2),
                "pass_yards": round(stats["pass_yards"], 1),
                "pass_td": round(stats["pass_td"], 2),
                "interceptions": round(stats["interceptions"], 2),
                "rush_yards": round(stats["rush_yards"], 1),
                "rush_td": round(stats["rush_td"], 2),
                "receptions": round(stats["receptions"], 1),
                "recv_yards": round(stats["recv_yards"], 1),
                "recv_td": round(stats["recv_td"], 2),
            })

    return rows, scoring, mismatches, unmatched, risk_conflicts, read_flex(book)


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    rows, scoring, mismatches, unmatched, risk_conflicts, flex = extract(sys.argv[1])
    if not rows:
        sys.exit("extract_draftsheets: no rows found — is this the DraftSheets workbook?")

    with open(sys.argv[2], "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)

    banded = [r for r in rows if r["proj_low"] and r["proj_high"]]
    rookie_rows = [r for r in rows if r["rookie"]]
    fumbled = [r for r in rows if r["fumbles_lost"]]
    # The recompute check: workbook_points is the vendor's own total under the
    # same settings, so it should exceed league_points by exactly the fumble
    # term. Anything else means the yards-per-point conversion is wrong.
    drifted = [r for r in rows
               if abs(r["league_points"] + r["fumble_points"] - r["workbook_points"]) > 0.05]

    print(f"wrote {len(rows)} rows to {sys.argv[2]}")
    for position in POSITIONS:
        n = sum(1 for r in rows if r["position"] == position)
        print(f"  {position:3} {n:4} players")
    print(f"  auction values total ${sum(r['auction_value'] for r in rows):,.0f}")
    print(f"  {len(banded)} rows carry a LOW/HIGH band, {len(rookie_rows)} rookies flagged")
    print(f"  flex slots fill as {', '.join(f'{k} {flex[k]}' for k in POSITIONS if k in flex)}")
    print(f"  re-scored under league rules ({VENDOR_PPR:g} PPR published -> "
          f"{SCORING['receptions']:g} PPR league)")
    print(f"  fumbles excluded from league_points on {len(fumbled)} projections "
          f"carrying one (worth {sum(r['fumble_points'] for r in fumbled):,.0f} pts "
          f"at the workbook's {VENDOR_FUMBLE:g}/FL, kept in fumble_points)")
    if drifted:
        print(f"  WARNING: {len(drifted)} rows do not reconcile with the workbook's "
              f"own total — the scoring conversion is suspect")
    else:
        print(f"  all {len(rows)} rows reconcile with the workbook's own total")
    if mismatches:
        print("  WARNING: workbook settings differ from the league:")
        for line in mismatches:
            print(f"    {line}")
    if unmatched:
        print(f"  WARNING: {len(unmatched)} Aggregate names missing from their "
              f"position sheet, e.g. {', '.join(unmatched[:3])}")
    if risk_conflicts:
        print(f"  note: {risk_conflicts} rows where RISK and Aggregate disagree "
              f"on missed games; Aggregate's figure was used")


if __name__ == "__main__":
    main()

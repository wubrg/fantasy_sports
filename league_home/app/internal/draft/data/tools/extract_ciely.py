#!/usr/bin/env python3
"""Extract Jake Ciely's projection workbook into the draft room's normalized CSV.

Ciely's workbook is a subscriber product from The Athletic, so it is never
fetched — it is exported by hand and read from a local path. See
internal/draft/data/README.md for the ingest policy.

The workbook's default Settings sheet happens to match Hit or Miss almost
exactly (12 teams, 1QB/2RB/3WR/1TE/1FLEX/1DST, $200 budget, 0.5 PPR at every
position, 0.04 pass yards, 4 pass TD, 6 rush/rec TD). The single mismatch is
interceptions: Ciely uses -2, the league uses -1. Because the workbook also
carries the raw stat projections, this script recomputes fantasy points under
the league's own scoring rather than inheriting his, and writes both so the
difference stays auditable.

Usage:
    python3 extract_ciely.py <workbook.xlsx> <out.csv>

Dependency-free on purpose: parses the xlsx zip/XML directly so this runs on
a stock Python 3 with no pip install.
"""

import csv
import re
import sys
import xml.etree.ElementTree as ET
import zipfile

NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
RNS = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"

# Hit or Miss scoring, from the league's Sleeper scoring_settings. Only the
# categories Ciely projects are listed; the rest are zero for skill players.
SCORING = {
    "PASS YARDS": 0.04,
    "PASS TD": 4.0,
    "INT": -1.0,
    "RUSH YARDS": 0.1,
    "RUSH TD": 6.0,
    "REC": 0.5,
    "RECV YARDS": 0.1,
    "RECV TD": 6.0,
}

# Ciely's own interception value, kept only to explain the delta.
CIELY_INT = -2.0

POSITION_BLOCKS = ["QB", "RB", "WR", "TE", "DST"]


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


def split_blocks(header):
    """Ciely lays positions out side by side, each starting with an RK column."""
    starts = [i for i, v in enumerate(header) if v == "RK"]
    return [(starts[i], starts[i + 1] if i + 1 < len(starts) else len(header))
            for i in range(len(starts))]


def league_points(stats):
    return sum(SCORING[k] * (stats.get(k) or 0) for k in SCORING)


def extract(path):
    book = Book(path)
    grid = book.grid("Ranks w Proj")
    header = grid[0]
    out = []

    for block_index, (start, end) in enumerate(split_blocks(header)):
        if block_index >= len(POSITION_BLOCKS):
            break
        position = POSITION_BLOCKS[block_index]
        columns = header[start:end]
        for row in grid[1:]:
            stats = dict(zip(columns, row[start:end]))
            name = stats.get("Player")
            if not name:
                continue

            ciely_fps = stats.get("FPS") or 0.0
            ours = league_points(stats)
            # DST rows carry no stat components to recompute from, so his
            # number stands and the delta is zero by definition.
            if position == "DST":
                ours = ciely_fps

            out.append({
                "source": "ciely",
                "position": position,
                "pos_rank": int(stats.get("RK") or 0),
                "player": name.strip(),
                "team": (stats.get("TM") or "").strip(),
                "bye": int(stats.get("BYE") or 0),
                "ciely_points": round(ciely_fps, 2),
                "league_points": round(ours, 2),
                "points_delta": round(ours - ciely_fps, 2),
                "auction_value": round(stats.get("AUC$") or 0.0, 2),
                "pass_yards": round(stats.get("PASS YARDS") or 0, 1),
                "pass_td": round(stats.get("PASS TD") or 0, 2),
                "interceptions": round(stats.get("INT") or 0, 2),
                "rush_yards": round(stats.get("RUSH YARDS") or 0, 1),
                "rush_td": round(stats.get("RUSH TD") or 0, 2),
                "targets": round(stats.get("TGTS") or 0, 1),
                "receptions": round(stats.get("REC") or 0, 1),
                "recv_yards": round(stats.get("RECV YARDS") or 0, 1),
                "recv_td": round(stats.get("RECV TD") or 0, 2),
            })
    return out


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    rows = extract(sys.argv[1])
    if not rows:
        sys.exit("extract_ciely: no rows found — is this the projections workbook?")

    with open(sys.argv[2], "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)

    changed = [r for r in rows if abs(r["points_delta"]) > 0.05]
    print(f"wrote {len(rows)} rows to {sys.argv[2]}")
    print(f"  auction values total ${sum(r['auction_value'] for r in rows):,.0f}")
    print(f"  {len(changed)} players re-scored under league rules "
          f"(interceptions {CIELY_INT:+.0f} -> {SCORING['INT']:+.0f})")


if __name__ == "__main__":
    main()

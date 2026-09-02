#!/usr/bin/env python3
"""Extract Subvertadown's VBD draft sheets into the draft room's normalized CSV.

Subvertadown's tool renders entirely client-side, so there is no export and
no API — these are saved-page captures of the rendered DOM, taken by hand.
See the private data repo's README for the ingest policy.

Three baselines are read (BEER, BEER+, VOLS) from the "stock" QB sheets. The
"qbstream" variants are deliberately skipped: they were verified byte-identical
to the stock sheets, differing in 0 of 218 rows across all three baselines.

Each row yields four independent quantities:

  aav      average auction value from *actual human drafts* (the market)
  value    the VBD price under that baseline (the model)
  ps_pct   positional scarcity — the fraction of positive value remaining at
           the position after this player is drafted. Not a point total, and
           never a loss figure.
  ecr_up / ecr_down
           whether any FantasyPros expert thinks the player scores
           significantly more / less than consensus. These are INDEPENDENT:
           a genuinely contested player carries both.

Parsing order matters. The value cell contains both a <template> tooltip body
(which corrupts the number if left in) and the ECR arrow icons (which are
signal). Icons are read from the raw HTML first, then templates are dropped,
then text is taken.

Usage:
    python3 extract_subvertadown.py <sheets-dir> <out.csv>

Dependency-free: stdlib only.
"""

import csv
import html
import os
import re
import sys

# Baseline label -> filename stem, in the order Subvertadown presents them.
#
# A baseline whose sheet is absent is skipped rather than fatal. Each is a
# separate page save, and which ones get saved is a judgement about what the
# board needs rather than a fixed set: beerplus is the default the board prices
# on and vols is what rosters are scored against, so beer is the one that can
# be left out without anything downstream noticing. Every consumer of the
# per-baseline map already guards its lookup.
#
# At least one is still required. An empty extract would leave the board with
# no market price at all, which is a different and much quieter failure than a
# missing file.
BASELINES = {"beer": "stock-beer", "beerplus": "stock-beerplus", "vols": "stock-vols"}

# Column indexes in the board table. The header row is:
#   0 position rank | 1 player | 2 Q | 3 R | 4 W | 5 T | 6 Tm | 7 B | 8 AAV | 9 PS | 10 $
COL_POS_RANK, COL_PLAYER, COL_TEAM, COL_BYE, COL_AAV, COL_PS, COL_VALUE = 0, 1, 6, 7, 8, 9, 10
MIN_CELLS = 11

TAG_RE = re.compile(r"<[^>]+>")
TEMPLATE_RE = re.compile(r"<template.*?</template>", re.S)
TABLE_RE = re.compile(r"<table.*?</table>", re.S)
ROW_RE = re.compile(r"<tr.*?</tr>", re.S)
CELL_RE = re.compile(r"<t[dh][^>]*>(.*?)</t[dh]>", re.S)
# Player cells end with the team and that team's positional rank, e.g.
# "Jahmyr Gibbs DET-1". Strip it so the name matches Sleeper's dictionary.
TEAM_SUFFIX_RE = re.compile(r"\s+[A-Z]{2,3}-\d+\s*$")


def cell_text(raw):
    """Visible text of a cell, with tooltip bodies removed."""
    return re.sub(r"\s+", " ", html.unescape(TAG_RE.sub(" ", TEMPLATE_RE.sub("", raw)))).strip()


def first_int(text, pattern=r"-?\d+"):
    m = re.search(pattern, text or "")
    return int(m.group(0)) if m else 0


def parse_sheet(path, baseline):
    with open(path, encoding="utf-8", errors="replace") as f:
        markup = f.read()

    tables = TABLE_RE.findall(markup)
    if not tables:
        raise SystemExit(f"extract_subvertadown: no table found in {path}")

    rows = []
    for raw_row in ROW_RE.findall(tables[0]):
        cells = CELL_RE.findall(raw_row)
        if len(cells) < MIN_CELLS:
            continue

        name = TEAM_SUFFIX_RE.sub("", cell_text(cells[COL_PLAYER])).strip()
        if not name or name.lower() == "player":
            continue

        pos_rank_text = cell_text(cells[COL_POS_RANK])
        m = re.match(r"([A-Z]+)(\d+)", pos_rank_text)
        if not m:
            continue
        position, pos_rank = m.group(1), int(m.group(2))

        # Read the arrows from raw markup BEFORE the tooltip body is
        # dropped, then read the number from the cleaned text.
        value_raw = cells[COL_VALUE]
        ecr_up = "ri-arrow-up-line" in value_raw
        ecr_down = "ri-arrow-down-line" in value_raw

        rows.append({
            "source": "subvertadown",
            "baseline": baseline,
            "position": position,
            "pos_rank": pos_rank,
            "player": name,
            "team": cell_text(cells[COL_TEAM]).split("-")[0].strip(),
            "bye": first_int(cell_text(cells[COL_BYE])),
            "aav": first_int(cell_text(cells[COL_AAV]), r"\d+"),
            "ps_pct": first_int(cell_text(cells[COL_PS]), r"\d+"),
            "value": first_int(cell_text(cells[COL_VALUE]), r"\d+"),
            "ecr_up": int(ecr_up),
            "ecr_down": int(ecr_down),
        })
    return rows


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    sheets_dir, out_path = sys.argv[1], sys.argv[2]

    all_rows = []
    for baseline, stem in BASELINES.items():
        path = os.path.join(sheets_dir, f"{stem}.html")
        if not os.path.exists(path):
            print(f"  {baseline:9} skipped, no {stem}.html in this snapshot")
            continue
        rows = parse_sheet(path, baseline)
        if not rows:
            sys.exit(f"extract_subvertadown: parsed 0 rows from {path}")
        all_rows.extend(rows)
        print(f"  {baseline:9} {len(rows):3} players from {os.path.basename(path)}")

    # A sheet that is present but unparseable is fatal above. Nothing at all is
    # fatal here, because writing an empty CSV would take the market off the
    # board without saying so.
    if not all_rows:
        sys.exit(f"extract_subvertadown: no baseline sheets found in {sheets_dir}; "
                 f"expected any of {', '.join(s + '.html' for s in BASELINES.values())}")

    with open(out_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(all_rows[0].keys()))
        writer.writeheader()
        writer.writerows(all_rows)

    # Count the baselines actually read, not the ones this script knows about:
    # one may have been skipped, and a summary reporting the wrong divisor is
    # how a half-loaded market gets mistaken for a full one.
    read = sorted({r["baseline"] for r in all_rows})
    per_baseline = len(all_rows) // len(read)
    one = [r for r in all_rows if r["baseline"] == "beerplus"]
    up = sum(r["ecr_up"] for r in one)
    down = sum(r["ecr_down"] for r in one)
    both = sum(1 for r in one if r["ecr_up"] and r["ecr_down"])
    print(f"wrote {len(all_rows)} rows ({per_baseline} players x {len(read)} baselines: "
          f"{', '.join(read)}) to {out_path}")
    print(f"  ECR flags (beerplus): {up} up, {down} down, {both} carrying both")


if __name__ == "__main__":
    main()

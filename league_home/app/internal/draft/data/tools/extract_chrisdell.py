#!/usr/bin/env python3
"""Extract Chris Dell's expert rankings into the draft room's normalized CSV.

Chris Dell is a single trusted expert whose per-position boards are exported
from FantasyPros, saved under raw/ as chris[s]dell-<pos>-expert-rankings.csv
(the RB file is misspelled "chrissdell" in one export; both spellings are
accepted). Each file has a few banner lines, then:

    Rank, Player Name, Team, Position, ECR, vs. ECR

where Rank is Dell's own positional rank, ECR is the consensus positional
rank, and `vs. ECR` = ECR - Rank. Positive means Dell rates the player ABOVE
consensus (a sharp lean up), which matches the sign the FantasyPros
sharp-expert signal already uses in the board.

Output columns: source, position, pos_rank, player, team, ecr, vs_ecr.

Usage:
    python3 extract_chrisdell.py <raw_dir> <out_csv>
"""
import csv
import glob
import os
import sys

HEADER_KEY = "rank"  # the real header row starts with a "Rank" column


def parse_file(path):
    rows = []
    with open(path, encoding="utf-8-sig", errors="replace") as f:
        reader = csv.reader(f)
        cols = None
        for raw in reader:
            if not raw or not any(c.strip() for c in raw):
                continue
            first = raw[0].strip().lower()
            if cols is None:
                # Skip banner lines until the real header.
                if first == HEADER_KEY:
                    cols = {c.strip().lower().rstrip("."): i for i, c in enumerate(raw)}
                continue
            if not raw[cols["rank"]].strip().isdigit():
                continue

            def cell(name):
                i = cols.get(name)
                return raw[i].strip() if i is not None and i < len(raw) else ""

            player = cell("player name") or cell("player")
            if not player:
                continue
            # vs_ecr = consensus positional rank - Dell's positional rank.
            # Computed rather than read: the source's own "vs. ECR" column is
            # the same value but carries an awkward header. Positive means Dell
            # rates him above consensus (a sharp lean up).
            pos_rank, ecr = cell("rank"), cell("ecr")
            vs_ecr = ""
            if pos_rank.isdigit() and ecr.lstrip("-").isdigit():
                vs_ecr = str(int(ecr) - int(pos_rank))
            rows.append({
                "source": "chrisdell",
                "position": cell("position"),
                "pos_rank": pos_rank,
                "player": player,
                "team": cell("team"),
                "ecr": ecr,
                "vs_ecr": vs_ecr or "0",
            })
    if not rows:
        sys.exit(f"extract_chrisdell: parsed 0 rows from {path}")
    return rows


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    raw_dir, out_path = sys.argv[1], sys.argv[2]

    paths = sorted(glob.glob(os.path.join(raw_dir, "chris*dell-*-expert-rankings.csv")))
    if not paths:
        sys.exit(f"extract_chrisdell: no chris*dell-*-expert-rankings.csv in {raw_dir}")

    all_rows = []
    for path in paths:
        rows = parse_file(path)
        all_rows.extend(rows)
        print(f"  {os.path.basename(path):45} {len(rows):3} players")

    with open(out_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(all_rows[0].keys()))
        writer.writeheader()
        writer.writerows(all_rows)

    up = sum(1 for r in all_rows if int(r["vs_ecr"]) > 0)
    down = sum(1 for r in all_rows if int(r["vs_ecr"]) < 0)
    print(f"wrote {len(all_rows)} players to {out_path} ({up} above consensus, {down} below)")


if __name__ == "__main__":
    main()

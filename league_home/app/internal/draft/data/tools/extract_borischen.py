#!/usr/bin/env python3
"""Extract Boris Chen's tier lists into the draft room's normalized CSV.

Boris Chen groups players into tiers with a Gaussian mixture model over expert
consensus ranks, and publishes the result as plain text files on a public S3
bucket (https://s3-us-west-1.amazonaws.com/fftiers/out). Because they are free
and public, they are fetched rather than hand-exported — a dated snapshot lands
under raw/borischen/<date>/ for provenance, and this script normalizes it.

The half-PPR files are text_<POS>-HALF.txt for RB/WR/TE; QB is scoring-agnostic
and ships as text_QB.txt. Each line is one tier:

    Tier 1: Ja'Marr Chase, Puka Nacua, Jaxon Smith-Njigba

so the tier number is the line's rank and the players are comma-separated. The
position comes from the filename, and the tier is within that position — which
is what the board wants beside its own gap-based tiering.

Output columns: source, position, tier, player.

Usage:
    python3 extract_borischen.py <raw_dir> <out_csv>
"""
import csv
import glob
import os
import re
import sys

TIER_RE = re.compile(r"^\s*Tier\s+(\d+)\s*:\s*(.+)$", re.IGNORECASE)


def position_of(filename):
    """Derive the position from a Boris Chen filename: text_RB-HALF.txt -> RB."""
    stem = os.path.basename(filename)
    stem = re.sub(r"^text_", "", stem)
    stem = re.sub(r"\.txt$", "", stem)
    # Drop the scoring suffix (-HALF / -PPR / -STD); QB has none.
    stem = re.sub(r"-(HALF|PPR|STD)$", "", stem, flags=re.IGNORECASE)
    return stem.upper()


def parse_file(path):
    position = position_of(path)
    rows = []
    with open(path, encoding="utf-8-sig", errors="replace") as f:
        for line in f:
            m = TIER_RE.match(line)
            if not m:
                continue
            tier = m.group(1)
            for name in m.group(2).split(","):
                name = name.strip()
                if not name:
                    continue
                rows.append({
                    "source": "borischen",
                    "position": position,
                    "tier": tier,
                    "player": name,
                })
    if not rows:
        sys.exit(f"extract_borischen: parsed 0 players from {path}")
    return rows


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    raw_dir, out_path = sys.argv[1], sys.argv[2]

    paths = sorted(glob.glob(os.path.join(raw_dir, "text_*.txt")))
    if not paths:
        sys.exit(f"extract_borischen: no text_*.txt in {raw_dir}")

    all_rows = []
    for path in paths:
        rows = parse_file(path)
        all_rows.extend(rows)
        tiers = max(int(r["tier"]) for r in rows)
        print(f"  {os.path.basename(path):22} {position_of(path):3}  {len(rows):3} players, {tiers} tiers")

    with open(out_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(all_rows[0].keys()))
        writer.writeheader()
        writer.writerows(all_rows)

    print(f"wrote {len(all_rows)} players to {out_path}")


if __name__ == "__main__":
    main()

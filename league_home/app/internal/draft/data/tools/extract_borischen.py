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

The text files carry tier membership and nothing else. When the site pages are
hand-saved instead of fetched, the bundle also includes weekly-ALL.csv, which
carries the rank dispersion behind the tiering — best/worst/average rank and
standard deviation across the expert panel. That is not derivable from the text
files at any effort, and it is the whole reason a manual save is worth keeping:
it promotes Boris Chen from a tier-only source into a rank source. So the CSV is
joined in when present, by player name within position, and is simply absent on
the fetched path — `make refresh` produces a dated dir of text files alone, and
that must keep working with the dispersion columns left empty.

Two things about weekly-ALL.csv are easy to misread. Its Rank is an OVERALL rank
across positions (Gibbs 1, Chase 2), not a positional rank. And its Tier is the
tier from Chen's combined ALL board, which spans every position at once — DST
lands in the twenties there — so it is NOT the same quantity as the per-position
tier in the text files and the two cannot be compared. The text files stay
authoritative for `tier`; the CSV's tier rides along as `overall_tier`.

Output columns: source, position, tier, player, overall_rank, overall_tier,
best, worst, avg_rank, stddev.

Usage:
    python3 extract_borischen.py <raw_dir> <out_csv>
"""
import csv
import glob
import os
import re
import sys

TIER_RE = re.compile(r"^\s*Tier\s+(\d+)\s*:\s*(.+)$", re.IGNORECASE)

# The dispersion fields, in output order. Kept as one list so the empty-string
# fallback on the fetched path cannot drift out of sync with the join.
DISPERSION_FIELDS = ["overall_rank", "overall_tier", "best", "worst", "avg_rank", "stddev"]

FIELDS = ["source", "position", "tier", "player"] + DISPERSION_FIELDS


def position_of(filename):
    """Derive the position from a Boris Chen filename: text_RB-HALF.txt -> RB."""
    stem = os.path.basename(filename)
    stem = re.sub(r"^text_", "", stem)
    stem = re.sub(r"\.txt$", "", stem)
    # Drop the scoring suffix (-HALF / -PPR / -STD); QB has none.
    stem = re.sub(r"-(HALF|PPR|STD)$", "", stem, flags=re.IGNORECASE)
    return stem.upper()


def name_key(position, name):
    """Join key for the CSV lookup.

    Position is part of the key so a name shared across positions cannot pull in
    the wrong player's dispersion. Whitespace is collapsed because the saved page
    and the S3 text file are two different renderings of the same string.
    """
    return (position.upper(), " ".join(name.split()).lower())


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
                row = {
                    "source": "borischen",
                    "position": position,
                    "tier": tier,
                    "player": name,
                }
                row.update({k: "" for k in DISPERSION_FIELDS})
                rows.append(row)
    if not rows:
        sys.exit(f"extract_borischen: parsed 0 players from {path}")
    return rows


def find_weekly_csv(raw_dir):
    """Locate weekly-ALL.csv, or None on the fetched path where it never exists.

    The hand-saved bundle drops it inside as-provided/ alongside the page HTML,
    but an operator who pulls the CSV out on its own is not wrong either, so the
    raw dir itself is checked first.
    """
    for candidate in (
        os.path.join(raw_dir, "weekly-ALL.csv"),
        os.path.join(raw_dir, "as-provided", "weekly-ALL.csv"),
    ):
        if os.path.isfile(candidate):
            return candidate
    return None


def load_dispersion(path):
    """Read weekly-ALL.csv into {(position, name): {dispersion fields}}."""
    by_player = {}
    with open(path, encoding="utf-8-sig", newline="") as f:
        for rec in csv.DictReader(f):
            position = (rec.get("Position") or "").strip().upper()
            name = (rec.get("Player.Name") or "").strip()
            if not position or not name:
                continue
            by_player[name_key(position, name)] = {
                # Carried alongside the fields for reporting only, so warnings can
                # name the player as Chen spells him rather than as the join key.
                "display_name": name,
                "overall_rank": (rec.get("Rank") or "").strip(),
                "overall_tier": (rec.get("Tier") or "").strip(),
                "best": (rec.get("Best.Rank") or "").strip(),
                "worst": (rec.get("Worst.Rank") or "").strip(),
                "avg_rank": (rec.get("Avg.Rank") or "").strip(),
                "stddev": (rec.get("Std.Dev") or "").strip(),
            }
    if not by_player:
        sys.exit(f"extract_borischen: parsed 0 players from {path}")
    return by_player


def stale_join_warnings(dispersion, matched):
    """Flag ranked players the tier files should have covered but do not.

    Neither artifact strictly contains the other: the CSV stops at 200 overall,
    while the tier files stop at their own per-position depth — which for RB/WR
    is shallower than the CSV. So an unjoined CSV player is usually just depth,
    not a problem, and counting all of them would cry wolf every run.

    What is not explained by depth is a player ranked ABOVE the deepest player
    the tier file did cover: that one should have been in the file and wasn't.
    Expect zero or one at the ragged edge, where the two artifacts order the
    boundary slightly differently. A larger number means the tier files and the
    CSV were captured on different days and the join is stale.
    """
    def rank_of(key):
        raw = dispersion[key]["overall_rank"]
        return int(raw) if raw.isdigit() else None

    gaps = []
    for pos in sorted({p for p, _ in dispersion}):
        keys = [k for k in dispersion if k[0] == pos]
        covered = [r for r in (rank_of(k) for k in keys if k in matched) if r is not None]
        if not covered:
            continue
        depth = max(covered)
        for key in keys:
            rank = rank_of(key)
            if key not in matched and rank is not None and rank < depth:
                gaps.append((rank, pos, dispersion[key]["display_name"]))

    if not gaps:
        return []
    plural = "player" if len(gaps) == 1 else "players"
    lines = [f"  {len(gaps)} ranked {plural} missing from the tier files"
             " within the depth those files cover"]
    # One or two is the ragged edge; a run of them is a stale capture.
    if len(gaps) > 3:
        lines.append("  WARNING: more than a boundary artifact — the tier files and"
                     " the CSV were likely captured on different days")
    for rank, pos, name in sorted(gaps)[:10]:
        lines.append(f"    {pos} #{rank} {name} is ranked but untiered")
    return lines


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

    csv_path = find_weekly_csv(raw_dir)
    if csv_path is None:
        print("  weekly-ALL.csv       absent — tiers only, no rank dispersion")
    else:
        dispersion = load_dispersion(csv_path)
        matched = set()
        for row in all_rows:
            key = name_key(row["position"], row["player"])
            found = dispersion.get(key)
            if found:
                row.update({k: found[k] for k in DISPERSION_FIELDS})
                matched.add(key)
        print(f"  {os.path.basename(csv_path):22}     {len(dispersion)} ranked, {len(matched)} joined to a tier")
        for line in stale_join_warnings(dispersion, matched):
            print(line)

    with open(out_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=FIELDS)
        writer.writeheader()
        writer.writerows(all_rows)

    with_dispersion = sum(1 for r in all_rows if r["avg_rank"])
    print(f"wrote {len(all_rows)} players to {out_path} ({with_dispersion} with rank dispersion)")


if __name__ == "__main__":
    main()

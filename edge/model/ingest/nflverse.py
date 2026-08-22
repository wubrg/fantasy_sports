#!/usr/bin/env python3
"""Fetch the nflverse tables this project models from, into a local cache.

nflverse is an open data project that publishes NFL data as flat files on
GitHub releases. Nothing here scrapes anything; these are files distributed to
be downloaded.

Three tables are pulled:

    games.csv                 schedules, and crucially the CLOSING SPREAD AND
                              TOTAL for every game back to 1999. This is what
                              lets the scenario model be validated against
                              reality rather than assumed.
    stats_player_week_<yr>    weekly player production, with target_share and
                              air_yards_share already computed.
    snap_counts_<yr>          snap share. Note the coverage gap below.
    play_by_play_<yr>         every play, 372 columns. Fetched for two of them:
                              xpass (the modelled probability a play is a pass,
                              given down, distance, score and time) and pass_oe
                              (actual minus xpass). Their team-week mean is PASS
                              RATE OVER EXPECTED -- tendency with game script
                              already divided out, which raw pass rate cannot
                              give you. Gzipped, ~19 MB a season.

Coverage, established by probing the releases rather than assumed:

    games.csv               1999-2026 (2026 lines only, no results yet)
    stats_player_week       2005-2025
    snap_counts             2012-2025  <- starts in 2012, not 2005
    play_by_play            1999-2025 as files. Whether pass_oe is POPULATED
                            that far back is a separate question from whether
                            the file exists, and is checked by analysis/proe.py
                            rather than assumed here.

That snap-counts gap is real and load-bearing. Pro-Football-Reference's snap
data begins in 2012, so any analysis depending on snap share is limited to
2012 forward, while target share reaches back to 2005. A missing snap file for
2005-2011 is expected and is not an error; a missing one for 2012+ is.

There is an older `player_stats_<yr>.csv` release still up. It stops at 2024
and is superseded by stats_player_week, which covers the full range under one
name. Do not switch back to it.

Dependency-free: stdlib only, matching the other Python in this repo.

Usage:
    python3 nflverse.py                      # everything, 2005-2025
    python3 nflverse.py --seasons 2022-2025  # a range
    python3 nflverse.py --force              # re-fetch even if cached
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, asdict
from pathlib import Path

RELEASES = "https://github.com/nflverse/nflverse-data/releases/download"
GAMES_URL = "https://raw.githubusercontent.com/nflverse/nfldata/master/data/games.csv"

# Earliest season each table covers. Requesting earlier is not an error; the
# season is skipped with a note.
FIRST_SEASON = {"stats_player_week": 2005, "snap_counts": 2012, "play_by_play": 1999}

DEFAULT_FIRST, DEFAULT_LAST = 2005, 2025

# Where the cache lives, relative to this file: edge/model/data/raw/.
# Gitignored — it is large and fully regenerable from this script.
CACHE = Path(__file__).resolve().parent.parent / "data" / "raw"
MANIFEST = CACHE / "manifest.json"


@dataclass
class Entry:
    """What was fetched, from where, and when — recorded per file.

    Provenance is not decoration here. Every downstream number traces back to
    one of these files, and a silent change in the source would otherwise be
    indistinguishable from a change in the NFL.
    """

    url: str
    fetched_at: str
    bytes: int
    sha256: str


def load_manifest() -> dict[str, dict]:
    if not MANIFEST.exists():
        return {}
    try:
        return json.loads(MANIFEST.read_text())
    except json.JSONDecodeError as e:
        raise SystemExit(f"manifest at {MANIFEST} is corrupt: {e}")


def save_manifest(m: dict[str, dict]) -> None:
    MANIFEST.parent.mkdir(parents=True, exist_ok=True)
    MANIFEST.write_text(json.dumps(m, indent=2, sort_keys=True) + "\n")


def fetch(url: str, dest: Path, *, retries: int = 3) -> Entry:
    """Download url to dest, atomically.

    Writes to a temporary file and renames on success, so an interrupted run
    never leaves a truncated CSV that would parse as valid but short.
    """
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".tmp")

    last: Exception | None = None
    for attempt in range(1, retries + 1):
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "edge-model/1.0"})
            with urllib.request.urlopen(req, timeout=120) as resp, tmp.open("wb") as out:
                digest = hashlib.sha256()
                total = 0
                while chunk := resp.read(1 << 20):
                    out.write(chunk)
                    digest.update(chunk)
                    total += len(chunk)
            tmp.replace(dest)
            return Entry(
                url=url,
                fetched_at=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                bytes=total,
                sha256=digest.hexdigest(),
            )
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            last = e
            tmp.unlink(missing_ok=True)
            if attempt < retries:
                time.sleep(2 * attempt)
    raise SystemExit(f"failed to fetch {url} after {retries} attempts: {last}")


def want(name: str, url: str, manifest: dict, force: bool) -> bool:
    """Whether this file needs fetching."""
    if force:
        return True
    path = CACHE / name
    if not path.exists():
        return True
    prior = manifest.get(name)
    if not prior or prior.get("url") != url:
        return True
    # Size drift means the file changed underneath us.
    return path.stat().st_size != prior.get("bytes")


def targets(first: int, last: int) -> list[tuple[str, str]]:
    """(cache name, url) pairs for the requested seasons."""
    out = [("games.csv", GAMES_URL)]
    for season in range(first, last + 1):
        for table in ("stats_player_week", "snap_counts", "play_by_play"):
            if season < FIRST_SEASON[table]:
                continue
            # The release TAG is not always the table name: stats_player_week
            # lives under "stats_player", play_by_play under "pbp". Getting this
            # wrong reports "not published" for a file that is right there.
            release = {"stats_player_week": "stats_player", "play_by_play": "pbp"}.get(
                table, table
            )
            # play-by-play is served gzipped and is the only table large enough
            # for that to matter: 19 MB a season against 99 MB uncompressed.
            ext = "csv.gz" if table == "play_by_play" else "csv"
            out.append(
                (f"{table}_{season}.{ext}", f"{RELEASES}/{release}/{table}_{season}.{ext}")
            )
    return out


def parse_seasons(spec: str) -> tuple[int, int]:
    if "-" in spec:
        a, _, b = spec.partition("-")
        first, last = int(a), int(b)
    else:
        first = last = int(spec)
    if first > last:
        raise SystemExit(f"--seasons {spec}: first season is after the last")
    return first, last


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument(
        "--seasons",
        default=f"{DEFAULT_FIRST}-{DEFAULT_LAST}",
        help=f"season or range, default {DEFAULT_FIRST}-{DEFAULT_LAST}",
    )
    ap.add_argument("--force", action="store_true", help="re-fetch cached files")
    ap.add_argument("--dry-run", action="store_true", help="show what would be fetched")
    args = ap.parse_args(argv)

    first, last = parse_seasons(args.seasons)
    manifest = load_manifest()
    todo = [(n, u) for n, u in targets(first, last) if want(n, u, manifest, args.force)]

    if args.dry_run:
        for name, url in todo:
            print(f"would fetch {name}")
        print(f"\n{len(todo)} file(s); cache at {CACHE}")
        return 0

    if not todo:
        print(f"cache is current ({CACHE})")
        return 0

    skipped = []
    for i, (name, url) in enumerate(todo, 1):
        print(f"[{i}/{len(todo)}] {name} ", end="", flush=True)
        try:
            entry = fetch(url, CACHE / name)
        except SystemExit as e:
            # A 404 on a season the release genuinely lacks is worth noting but
            # is not fatal — coverage differs per table and is documented above.
            if "404" in str(e):
                print("not published, skipped")
                skipped.append(name)
                continue
            raise
        manifest[name] = asdict(entry)
        save_manifest(manifest)
        print(f"{entry.bytes / 1e6:.1f} MB")

    print(f"\n{len(todo) - len(skipped)} fetched, {len(skipped)} unavailable")
    print(f"cache: {CACHE}")
    print(f"manifest: {MANIFEST}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

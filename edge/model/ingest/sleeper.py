#!/usr/bin/env python3
"""Pull NFL rosters and player injury status from Sleeper, into a local cache.

Sleeper (api.sleeper.app) is this project's trusted source of record for
who's on which team and who's hurt -- established the hard way: a web-search
summary once put three cornerbacks on the wrong teams entirely, and Sleeper's
own data was what caught it. `/v1/players/nfl` is a single unauthenticated,
keyless endpoint returning EVERY NFL player, keyed by player_id -- not
filterable server-side, so this script downloads the whole ~5 MB blob and
filters locally.

Unlike ingest/nflverse.py's tables, this is not a versioned historical
archive -- it's a live, always-current snapshot with no season/week axis, so
there's nothing here to keep a SHA256 manifest of many immutable files for.
Sleeper's own guidance (documented, not enforced by them: see the sibling Go
client at league_home/app/internal/sleeper/draft.go, a read-only reference,
not a dependency of this script) is to fetch this at most once a day and
cache to disk. So freshness here is just the cache file's own mtime: reuse it
unless it's older than --max-age-hours (default 20, under Sleeper's
once-a-day ceiling) or --force is passed.

Fields captured mirror that Go client's proven set (player_id, name fields,
position, team, fantasy_positions, years_exp, status, injury_status, active)
plus two it does NOT carry -- depth_chart_position and depth_chart_order --
which matter here: this project has repeatedly needed a real depth chart
(who's CB1 vs CB2, which back the market actually treats as the lead back),
not just a flat roster list.

Dependency-free: stdlib only, matching the other Python in this repo.

Usage:
    python3 sleeper.py --team GB                      # roster + depth chart
    python3 sleeper.py --team GB --position CB         # one position group
    python3 sleeper.py --team ATL --team GB            # both sides of a matchup
    python3 sleeper.py --player "Micah Parsons"        # name search, all matches shown
    python3 sleeper.py --force                         # just refresh the cache
    python3 sleeper.py --team GB --json                # machine-readable
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path

PLAYERS_URL = "https://api.sleeper.app/v1/players/nfl"

# Same cache directory ingest/nflverse.py already uses -- already gitignored
# there (open, regenerable data), so no new .gitignore entry is needed.
CACHE = Path(__file__).resolve().parent.parent / "data" / "raw"
CACHE_FILE = CACHE / "sleeper_players.json"

# Sleeper asks for at most one fetch a day; this sits comfortably under that.
DEFAULT_MAX_AGE_HOURS = 20.0


@dataclass
class Player:
    player_id: str
    first_name: str
    last_name: str
    full_name: str
    search_full_name: str
    position: str | None
    team: str | None
    fantasy_positions: list[str]
    years_exp: int | None
    status: str | None
    injury_status: str | None
    active: bool
    depth_chart_position: str | None
    depth_chart_order: int | None


def cache_age_hours(path: Path = CACHE_FILE) -> float | None:
    """None if the cache does not exist yet."""
    if not path.exists():
        return None
    return (time.time() - path.stat().st_mtime) / 3600.0


def fetch(dest: Path = CACHE_FILE, *, retries: int = 3) -> int:
    """Download the full Sleeper player list to dest, atomically.

    Writes to a temporary file and renames on success -- same pattern as
    ingest/nflverse.py's fetch -- so an interrupted run never leaves a
    truncated JSON file that a later run would try to parse. Returns the
    byte count fetched.
    """
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".tmp")

    last: Exception | None = None
    for attempt in range(1, retries + 1):
        try:
            req = urllib.request.Request(PLAYERS_URL, headers={"User-Agent": "edge-model/1.0"})
            with urllib.request.urlopen(req, timeout=120) as resp, tmp.open("wb") as out:
                total = 0
                while chunk := resp.read(1 << 20):
                    out.write(chunk)
                    total += len(chunk)
            tmp.replace(dest)
            return total
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            last = e
            tmp.unlink(missing_ok=True)
            if attempt < retries:
                time.sleep(2 * attempt)
    raise SystemExit(f"failed to fetch {PLAYERS_URL} after {retries} attempts: {last}")


def _parse(raw: dict) -> dict[str, Player]:
    """Build Player records defensively -- years_exp, depth_chart_order and
    even team are null for a real share of entries (retired players, practice
    squad, D/ST units), so every field is pulled with a safe default rather
    than assumed present.
    """
    out: dict[str, Player] = {}
    for pid, p in raw.items():
        if not isinstance(p, dict):
            continue
        years_exp = p.get("years_exp")
        depth_order = p.get("depth_chart_order")
        full_name = p.get("full_name") or (
            f"{p.get('first_name', '')} {p.get('last_name', '')}".strip()
        ) or pid
        out[pid] = Player(
            player_id=p.get("player_id") or pid,
            first_name=p.get("first_name") or "",
            last_name=p.get("last_name") or "",
            full_name=full_name,
            search_full_name=p.get("search_full_name") or "",
            position=p.get("position"),
            team=p.get("team"),
            fantasy_positions=list(p.get("fantasy_positions") or []),
            years_exp=int(years_exp) if years_exp is not None else None,
            status=p.get("status"),
            injury_status=p.get("injury_status") or None,
            active=bool(p.get("active")),
            depth_chart_position=p.get("depth_chart_position"),
            depth_chart_order=int(depth_order) if depth_order is not None else None,
        )
    return out


def load_players(
    *, force: bool = False, max_age_hours: float = DEFAULT_MAX_AGE_HOURS
) -> dict[str, Player]:
    """Current Sleeper players -- the importable entry point.

    Fetches only when the cache is missing, older than max_age_hours, or
    force is set; otherwise reuses the cached file. Prints nothing, so it's
    safe to import from another script (e.g. analysis/beliefpack.py, the same
    way it already imports proe/raoe via a sys.path.insert). All
    operator-facing status reporting belongs to main() below.
    """
    age = cache_age_hours()
    if force or age is None or age > max_age_hours:
        fetch()
    raw = json.loads(CACHE_FILE.read_text())
    return _parse(raw)


# Sleeper's `position` field is inconsistent for defensive backs -- on a
# single real roster (GB, verified below), one corner is tagged "CB" while
# the other three are tagged generically "DB", with the actual slot only
# distinguishable via depth_chart_position (LCB/RCB/SS/FS/NB). A naive
# --position CB filter against .position alone silently drops most of a
# team's real corners -- caught by running this against GB and finding only
# 1 of 4 rostered corners. This is a small, explicit alias list for the
# roles this project actually queries, not an attempt to solve Sleeper's
# position/depth_chart_position mismatch in general.
_POSITION_ALIASES: dict[str, set[str]] = {
    "CB": {"LCB", "RCB"},
    "S": {"SS", "FS"},
    "SAF": {"SS", "FS"},
}


def filter_team(
    players: dict[str, Player], team: str, *, position: str | None = None
) -> list[Player]:
    """Exact (not substring) case-insensitive match on team code.

    Substring matching would be ambiguous here -- Sleeper's short codes mean
    e.g. "LA" would match both LAC and LAR.
    """
    want = team.strip().upper()
    out = [p for p in players.values() if (p.team or "").upper() == want]
    if position:
        pos = position.strip().upper()
        aliases = _POSITION_ALIASES.get(pos, set())
        out = [
            p
            for p in out
            if (p.position or "").upper() == pos
            or (p.depth_chart_position or "").upper() == pos
            or (p.depth_chart_position or "").upper() in aliases
        ]
    return out


def filter_player(players: dict[str, Player], query: str) -> list[Player]:
    """Case-insensitive substring match on name. Always returns every match --
    never silently picks one. That's the direct fix for the mistake that
    motivated this script: trusting a single, possibly-wrong name lookup.
    """
    q = query.strip().lower()
    if not q:
        return []
    return [
        p
        for p in players.values()
        if q in p.full_name.lower() or q in p.search_full_name.lower()
    ]


def _slot_label(p: Player) -> str:
    if not p.depth_chart_position:
        return "-"
    if p.depth_chart_order is None:
        return p.depth_chart_position
    return f"{p.depth_chart_position}{p.depth_chart_order}"


def _print_table(rows: list[Player]) -> None:
    if not rows:
        print("  (no matches)")
        return
    # TEAM is shown even in a --team roster view (redundant with the section
    # header there) because --player search spans every team, and a name
    # search that doesn't say WHICH team a match is on defeats its own
    # purpose -- the exact thing this script exists to get right, caught in
    # verification when a first draft of this table omitted it.
    widths = (10, 24, 6, 5, 12, 10)
    header = ("SLOT", "NAME", "TEAM", "POS", "STATUS", "INJURY")
    print("  " + "".join(h.ljust(w) for h, w in zip(header, widths)))
    for p in rows:
        cells = (
            _slot_label(p),
            p.full_name,
            p.team or "-",
            p.position or "-",
            p.status or "-",
            p.injury_status or "-",
        )
        print("  " + "".join(str(c).ljust(w) for c, w in zip(cells, widths)))


def _print_roster(players: list[Player]) -> None:
    """Grouped by depth-chart slot, ordered within it -- a real depth chart,
    not just a flat list. Anyone with no depth-chart slot (bench/practice
    squad entries Sleeper hasn't slotted) prints in a trailing section
    instead of being dropped.
    """
    has_slot = sorted(
        (p for p in players if p.depth_chart_position),
        key=lambda p: (p.depth_chart_position, p.depth_chart_order if p.depth_chart_order is not None else 999),
    )
    no_slot = sorted(
        (p for p in players if not p.depth_chart_position),
        key=lambda p: (p.position or "", p.full_name),
    )
    _print_table(has_slot)
    if no_slot:
        print("  -- no depth chart slot --")
        _print_table(no_slot)


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--team", action="append", default=[], help="team code, e.g. GB (repeatable)")
    ap.add_argument("--player", default="", help="name substring, case-insensitive")
    ap.add_argument("--position", default="", help="position filter; only valid combined with --team")
    ap.add_argument("--max-age-hours", type=float, default=DEFAULT_MAX_AGE_HOURS)
    ap.add_argument("--force", action="store_true", help="re-fetch even if the cache is fresh")
    ap.add_argument("--json", action="store_true", help="machine-readable output on stdout")
    args = ap.parse_args(argv)

    if args.position and not args.team:
        ap.error("--position only makes sense combined with --team")
    if not args.team and not args.player and not args.force:
        ap.error("give --team and/or --player, or pass --force alone to just refresh the cache")

    # Status reporting is deliberately on stderr, unconditional, and BEFORE
    # the fetch decision is hidden inside load_players -- staleness must
    # never be silent.
    age_before = cache_age_hours()
    if args.force:
        print(f"cache: {CACHE_FILE}  FORCED refetch", file=sys.stderr)
    elif age_before is None:
        print(f"cache: {CACHE_FILE}  MISSING, fetching now", file=sys.stderr)
    elif age_before > args.max_age_hours:
        print(
            f"cache: {CACHE_FILE}  age {age_before:.1f}h (max {args.max_age_hours:.1f}h), refetching",
            file=sys.stderr,
        )
    else:
        print(
            f"cache: {CACHE_FILE}  age {age_before:.1f}h (max {args.max_age_hours:.1f}h), reused",
            file=sys.stderr,
        )

    players = load_players(force=args.force, max_age_hours=args.max_age_hours)
    print(f"cache: {len(players)} players", file=sys.stderr)

    if not args.team and not args.player:
        return 0  # standalone --force refresh: nothing more to show

    json_out: list[Player] = []
    for team in args.team:
        rows = filter_team(players, team, position=args.position or None)
        json_out.extend(rows)
        if not args.json:
            label = team.upper() + (f" ({args.position.upper()})" if args.position else "")
            print(f"\n{label}")
            _print_roster(rows)

    if args.player:
        matches = filter_player(players, args.player)
        json_out.extend(matches)
        if not matches:
            print(f'no matches for "{args.player}"', file=sys.stderr)
            return 1
        if not args.json:
            if len(matches) > 1:
                print(f'\n{len(matches)} matches for "{args.player}" -- verify which one you mean:')
            else:
                print(f'\nplayer search: "{args.player}"')
            _print_table(sorted(matches, key=lambda p: p.full_name))

    if args.json:
        print(json.dumps([asdict(p) for p in json_out], indent=2))

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

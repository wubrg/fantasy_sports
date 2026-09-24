#!/usr/bin/env python3
"""Pull JJ Zacharison's weekly Late Round matchup-notes newsletter, into a
local cache, for citation during URPS report generation.

Late Round's weekly "Matchup Notes" newsletter (high-level game notes, pass-
game matchups, run-game matchups -- EPA rankings, route rates, target shares,
usage-based read on the week's slate) is published as a public Mailchimp
campaign-archive page, one per week:

    https://mailchi.mp/lateround/week-{week}-matchup-notes-{season}

Verified directly (not assumed): no paywall, no login, no bot-blocking -- a
plain fetch with an honest User-Agent returns the same ~76 KB either way as a
browser-spoofed one. The URL pattern was confirmed against two different real
weeks (2 and 3, 2026) before being hardcoded here.

This is prose, not a data table -- there's nothing here to parse into typed
fields the way nflverse.py's CSVs or sleeper.py's player records are. Both the
raw HTML and a cleaned, readable text extraction are cached per week: the HTML
so a bad cleaning heuristic is recoverable without re-fetching, the text
because that's what actually gets read and quoted from when writing a report.
The cleaner is a light stdlib-only tag-strip (script/style removed, block tags
turned into line breaks, entities unescaped, blank lines collaped) -- good
enough to read and cite from, not an attempt at faithful Markdown.

A published week's content doesn't change underneath us the way a live
Sleeper snapshot does, so there's no freshness window here: once a week is
fetched, it's kept until --force asks for it again.

Dependency-free: stdlib only, matching the other Python in this repo.

Usage:
    python3 lateround.py --week 3                 # this season's week 3
    python3 lateround.py --week 3 --season 2025    # a different season
    python3 lateround.py --week 3 --force          # re-fetch even if cached
"""

from __future__ import annotations

import argparse
import html as html_lib
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

URL_TEMPLATE = "https://mailchi.mp/lateround/week-{week}-matchup-notes-{season}"

# This project is anchored to the 2026 season everywhere else (belief-pack,
# board scaffolding) -- same default here, overridable with --season.
DEFAULT_SEASON = 2026

# Same cache root ingest/nflverse.py and ingest/sleeper.py already use --
# already gitignored there, so no new .gitignore entry is needed.
CACHE = Path(__file__).resolve().parent.parent / "data" / "raw" / "lateround"


def cache_paths(week: int, season: int) -> tuple[Path, Path]:
    """(raw HTML path, cleaned text path) for a given week/season."""
    stem = CACHE / f"{season}-week{week:02d}"
    return stem.with_suffix(".html"), stem.with_suffix(".txt")


_BLOCK_TAGS = re.compile(r"<(?:p|div|br|h[1-6]|li|tr)\b[^>]*>", re.IGNORECASE)
_SCRIPT_STYLE = re.compile(r"<(script|style)\b.*?</\1>", re.IGNORECASE | re.DOTALL)
# Mailchimp templates lean heavily on MSO conditional comments
# (<!--[if gte mso 15]>...<![endif]-->) for Outlook-only markup -- without
# stripping these, the ">"/"-->" edges of a conditional block leak through as
# stray punctuation in the cleaned text.
_HTML_COMMENT = re.compile(r"<!--.*?-->", re.DOTALL)
_ANY_TAG = re.compile(r"<[^>]+>")
_BLANK_RUN = re.compile(r"\n{3,}")
_INLINE_SPACE = re.compile(r"[ \t]+")


def clean(raw_html: str) -> str:
    """Strip a newsletter HTML page down to readable prose.

    Not a general HTML-to-Markdown converter -- just enough to turn a
    Mailchimp campaign page into something worth reading and quoting from:
    scripts/styles gone, block-level tags become line breaks, remaining tags
    stripped, entities unescaped, and every whitespace-only line dropped
    before collapsing the rest.
    """
    text = _SCRIPT_STYLE.sub("", raw_html)
    text = _HTML_COMMENT.sub("", text)
    text = _BLOCK_TAGS.sub("\n", text)
    text = _ANY_TAG.sub("", text)
    text = html_lib.unescape(text)
    text = _INLINE_SPACE.sub(" ", text)
    lines = [ln.strip() for ln in text.split("\n")]
    lines = [ln for ln in lines if ln]
    text = "\n".join(lines)
    return _BLANK_RUN.sub("\n\n", text).strip()


def fetch(week: int, season: int = DEFAULT_SEASON, *, retries: int = 3) -> tuple[Path, Path]:
    """Download and clean one week's newsletter, atomically. Returns
    (html_path, text_path).
    """
    url = URL_TEMPLATE.format(week=week, season=season)
    html_path, text_path = cache_paths(week, season)
    html_path.parent.mkdir(parents=True, exist_ok=True)
    tmp_html = html_path.with_suffix(html_path.suffix + ".tmp")

    last: Exception | None = None
    for attempt in range(1, retries + 1):
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "edge-model/1.0"})
            with urllib.request.urlopen(req, timeout=60) as resp:
                raw = resp.read().decode("utf-8", errors="replace")
            tmp_html.write_text(raw, encoding="utf-8")
            tmp_html.replace(html_path)
            text_path.write_text(clean(raw) + "\n", encoding="utf-8")
            return html_path, text_path
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            last = e
            tmp_html.unlink(missing_ok=True)
            if attempt < retries:
                time.sleep(2 * attempt)
    raise SystemExit(f"failed to fetch {url} after {retries} attempts: {last}")


def load(week: int, season: int = DEFAULT_SEASON, *, force: bool = False) -> str:
    """Cleaned newsletter text for a week -- the importable entry point.

    Fetches only when the cached text is missing or force is set; otherwise
    reuses the cached file. Prints nothing, so it's safe to import from
    another script the same way sleeper.py's load_players is.
    """
    _, text_path = cache_paths(week, season)
    if force or not text_path.exists():
        fetch(week, season)
    return text_path.read_text(encoding="utf-8")


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--week", type=int, required=True, help="NFL week, e.g. 3")
    ap.add_argument("--season", type=int, default=DEFAULT_SEASON)
    ap.add_argument("--force", action="store_true", help="re-fetch even if already cached")
    args = ap.parse_args(argv)

    html_path, text_path = cache_paths(args.week, args.season)
    if args.force or not text_path.exists():
        print(
            f"fetching {URL_TEMPLATE.format(week=args.week, season=args.season)} ...",
            file=sys.stderr,
        )
        fetch(args.week, args.season)
    else:
        print(f"cache: {text_path}  already fetched, reused (use --force to re-fetch)", file=sys.stderr)

    print(f"html: {html_path} ({html_path.stat().st_size} bytes)", file=sys.stderr)
    print(f"text: {text_path} ({text_path.stat().st_size} bytes)", file=sys.stderr)
    print(text_path.read_text(encoding="utf-8"))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

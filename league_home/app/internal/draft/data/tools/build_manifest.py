#!/usr/bin/env python3
"""Record what is in the private data root, where it came from, and when.

`manifest.json` has been documented in both READMEs and returned by
`DataRoot.Manifest()` since the draft room was written, and until this script
nothing ever wrote one. That gap is the whole reason this file exists.
Provenance is not decoration: every dollar value on the board traces back to a
saved export, and without a recorded digest a source that silently changed
underneath us is indistinguishable from the NFL changing.

What is recorded, per snapshot, matching the schema the READMEs publish:

    source      the directory under raw/
    origin      where it came from — a URL where one exists, otherwise an
                honest short string ("hand-exported", "saved page"). Most of
                these are subscriber content exported by hand from a copy we
                already have rights to; there is no URL to record and saying
                so is better than inventing one.
    fetched_at  when the DATA was captured, taken from the newest file mtime
                in the snapshot rather than from the clock at manifest time.
                The question this answers is "how stale is this export", and
                "now" answers that question wrong every time.
    sha256      digest of the NORMALIZED CSV the source produces, plus a
                per-file digest for every raw file in `files`.
    rows        data rows in that normalized CSV.
    basis       the league assumptions the source's numbers were produced
                under — the Python mirror of the `Basis` struct in
                internal/draft/sources.go.

Two deliberate asymmetries in how absence is written:

  * `rows` is OMITTED where a row count is meaningless — an .xlsx, a .webp, a
    saved .pdf. A zero would be indistinguishable from "the extractor produced
    nothing", which is exactly the failure this manifest exists to catch.
  * `basis` is written as an explicit `null` where the source's own basis is
    not known. Absent would read as an oversight; null is a claim. Never
    assume a source shares Hit or Miss's basis — a wrong basis silently
    corrupts blended dollar values, and a silent corruption is the one thing
    this field is here to prevent.

Exactly one snapshot per source is marked `current`, and only that one carries
`sha256`/`rows` — attaching a normalized CSV to a snapshot it was not extracted
from would assert a lineage that is not true. Which one is current follows what
`make refresh` actually reads: the newest raw/<source>/<date>/ for most sources
(`ls -d .../*/ | tail -1`), but the loose undated files for Fantasy Points,
whose stanza globs raw/fantasypoints/*.html at the source root.

Those loose files are undated by design, not broken, and are recorded under a
snapshot with `date: null`. Only directories NAMED as dates count as snapshots:
a saved page leaves a `<name>_files/` sidecar of scripts and tracking images
beside itself, and a hundred digests of those is not provenance.

Idempotent: run twice on unchanged data and the bytes are identical. There is
deliberately no generated-at stamp, since one would make every run a diff.

Usage:
    python3 build_manifest.py <data-root>      # writes <data-root>/manifest.json

Dependency-free: stdlib only, matching the extractors beside it.
"""

from __future__ import annotations

import csv
import hashlib
import json
import re
import sys
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

MANIFEST_NAME = "manifest.json"

# Hit or Miss's own starting lineup, mirroring HitOrMiss() in sources.go.
LEAGUE_STARTERS = {"QB": 1, "RB": 2, "WR": 3, "TE": 1, "DEF": 1}


def basis(
    *,
    reception=None,
    interception=None,
    teams=None,
    budget=None,
    starters=None,
    flex=None,
) -> dict:
    """A source's league basis, with every unknown left explicitly null.

    Field names mirror the Basis struct in internal/draft/sources.go. A null
    means "this source does not publish it", not "same as ours".
    """
    return {
        "reception": reception,
        "interception": interception,
        "teams": teams,
        "budget": budget,
        "starters": starters,
        "flex": flex,
    }


@dataclass
class Source:
    """What is known about a source, independent of any one snapshot."""

    origin: str
    # The normalized CSV its extractor produces, or None where the source is
    # read by hand rather than extracted (the Athletic articles, Peaked's
    # cheat sheets).
    normalized: str | None = None
    basis: dict | None = None
    # Which bucket `make refresh` actually extracts from: "dated" for the
    # latest raw/<source>/<date>/, or "loose" for the undated files at the
    # source root. Fantasy Points is the loose one — its stanza globs
    # raw/fantasypoints/*.html directly — and getting this wrong marks a
    # superseded snapshot as the live one.
    reads: str = "dated"


SOURCES: dict[str, Source] = {
    # The only openly published source here, and so the only one with a real
    # URL: make refresh curls the half-PPR tier files straight from S3.
    "borischen": Source(
        origin="https://s3-us-west-1.amazonaws.com/fftiers/out",
        normalized="borischen-2026.csv",
        # Tiers, not dollars: the -HALF files fix scoring at 0.5 per
        # reception and the format publishes nothing else.
        basis=basis(reception=0.5),
    ),
    "chrisdell": Source(
        origin="hand-exported from FantasyPros (single expert board)",
        normalized="chrisdell-2026.csv",
        # Ranks only. Whatever scoring Dell ranks under is not published.
        basis=None,
    ),
    "ciely": Source(
        origin="hand-exported (subscriber spreadsheet)",
        normalized="ciely-2026.csv",
        # The one source that states its basis outright, and it is ours:
        # 12 teams, 1QB/2RB/3WR/1TE/1FLEX/1DST, $200, 0.5 PPR. It says
        # nothing about interceptions, so that stays null rather than
        # inheriting our -1.
        basis=basis(
            reception=0.5,
            teams=12,
            budget=200,
            starters=dict(LEAGUE_STARTERS),
            flex=1,
        ),
    ),
    "draftsheets": Source(
        origin="hand-exported (subscriber workbook)",
        normalized="draftsheets-2026.csv",
        basis=None,
    ),
    "fantasypoints": Source(
        origin="saved page (subscriber article)",
        normalized="fantasypoints-big3-2026.csv",
        reads="loose",
        # The Big-3 article is explicitly full-PPR; extract_fantasypoints.py
        # restates receiving into half-PPR precisely because of this.
        basis=basis(reception=1.0),
    ),
    "fantasypros": Source(
        origin="hand-exported from a logged-in session (renders client-side)",
        normalized="fantasypros-2026.csv",
        basis=None,
    ),
    "peaked": Source(
        origin="saved page (subscriber cheat sheets)",
        normalized=None,
        # Taken from the export's own filename, which names its settings:
        # "…-half-PPR-12-Team-…". Budget and lineup are not stated.
        basis=basis(reception=0.5, teams=12),
    ),
    "subvertadown": Source(
        origin="saved page (client-side tool, no export and no API)",
        normalized="subvertadown-2026.csv",
        basis=None,
    ),
    "theathletic": Source(
        origin="saved article PDF (subscriber)",
        normalized=None,
        basis=None,
    ),
}

# Extensions whose rows can be counted honestly. Everything else omits `rows`.
TABULAR = {".csv", ".txt"}

# A snapshot directory is a DATE, per the layout both READMEs publish. The rule
# has to be this strict: a saved page drops a `<name>_files/` sidecar beside
# itself, and treating that as a snapshot made it sort last and steal `current`
# from the loose .html the extractor actually reads.
DATE_DIR = re.compile(r"^\d{4}-\d{2}-\d{2}$")


@dataclass
class FileEntry:
    """One raw file inside a snapshot."""

    name: str
    bytes: int
    sha256: str
    rows: int | None = None


@dataclass
class Entry:
    """One snapshot of one source."""

    source: str
    origin: str
    fetched_at: str
    # The snapshot directory name, or null for the loose undated files some
    # sources keep at their root by design.
    date: str | None
    basis: dict | None
    # Whether this is the snapshot `make refresh` currently extracts from.
    current: bool
    normalized: str | None = None
    sha256: str | None = None
    rows: int | None = None
    files: list[FileEntry] = field(default_factory=list)


def sha256_of(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as f:
        while chunk := f.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def count_rows(path: Path) -> int | None:
    """Data rows in a tabular file, or None where the count would be a lie.

    CSVs are counted through the csv module so an embedded newline inside a
    quoted cell does not read as an extra player, and the header is not
    counted as one. Plain text is counted as non-blank lines.
    """
    if path.suffix.lower() == ".csv":
        try:
            with path.open(encoding="utf-8-sig", errors="replace", newline="") as f:
                n = sum(1 for row in csv.reader(f) if any(c.strip() for c in row))
        except OSError:
            return None
        return max(n - 1, 0)
    try:
        with path.open(encoding="utf-8", errors="replace") as f:
            return sum(1 for line in f if line.strip())
    except OSError:
        return None


def is_noise(path: Path) -> bool:
    """Dotfiles and Finder droppings are not provenance."""
    return path.name.startswith(".")


def iso_utc(epoch: float) -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(epoch))


def snapshot_files(root: Path, *, recurse: bool) -> list[Path]:
    """Files belonging to a snapshot, in stable order.

    Dated snapshots are walked in full — Subvertadown and Boris Chen both keep
    an `as-provided/` subdirectory beside the parsed one. Undated roots are NOT
    walked, which is what keeps a saved page's `<name>_files/` sidecar of
    scripts and tracking images out of the manifest.
    """
    it = root.rglob("*") if recurse else root.iterdir()
    # Sorted on the full path, not the basename: rglob order is filesystem
    # order, and the manifest has to come out byte-identical every run.
    return sorted(p for p in it if p.is_file() and not is_noise(p))


def build_entry(
    source: str,
    spec: Source,
    snapshot: Path,
    *,
    date: str | None,
    current: bool,
    normalized_dir: Path,
    warn,
) -> Entry | None:
    files = snapshot_files(snapshot, recurse=date is not None)
    if not files:
        return None

    recorded = []
    newest = 0.0
    for path in files:
        stat = path.stat()
        newest = max(newest, stat.st_mtime)
        rel = path.relative_to(snapshot).as_posix()
        rows = count_rows(path) if path.suffix.lower() in TABULAR else None
        recorded.append(
            FileEntry(name=rel, bytes=stat.st_size, sha256=sha256_of(path), rows=rows)
        )

    entry = Entry(
        source=source,
        origin=spec.origin,
        fetched_at=iso_utc(newest),
        date=date,
        basis=spec.basis,
        current=current,
        files=recorded,
    )

    # The normalized CSV describes the snapshot it was extracted from, which is
    # the latest one. Attaching it to a superseded snapshot would assert a
    # lineage that is not true.
    if current and spec.normalized:
        entry.normalized = spec.normalized
        out = normalized_dir / spec.normalized
        if out.exists():
            entry.sha256 = sha256_of(out)
            entry.rows = count_rows(out)
        else:
            warn(f"{source}: normalized/{spec.normalized} does not exist — never extracted?")
    return entry


def prune(value):
    """Drop null-valued keys, keeping the ones whose null is a statement.

    `basis: null` means "this source's basis is not known" and `date: null`
    means "undated by design". Both are findings. A null `rows` or `sha256`
    means the quantity does not apply, and is better left out entirely.
    """
    keep_null = {"basis", "date"}
    if isinstance(value, dict):
        return {
            k: prune(v) for k, v in value.items() if v is not None or k in keep_null
        }
    if isinstance(value, list):
        return [prune(v) for v in value]
    return value


def build(data_root: Path, warn) -> dict:
    raw = data_root / "raw"
    if not raw.is_dir():
        raise SystemExit(f"{raw} does not exist — is {data_root} the private data root?")
    normalized_dir = data_root / "normalized"

    manifest: dict[str, dict] = {}
    for source_dir in sorted(p for p in raw.iterdir() if p.is_dir() and not is_noise(p)):
        source = source_dir.name
        spec = SOURCES.get(source)
        if spec is None:
            warn(f"{source}: unknown source, origin and basis recorded as unknown")
            spec = Source(origin="unknown")

        dated = sorted(
            p for p in source_dir.iterdir() if p.is_dir() and DATE_DIR.match(p.name)
        )
        # Dates are YYYY-MM-DD, so lexicographic order is chronological — the
        # same assumption `ls -d .../*/ | tail -1` makes in the Makefile.
        latest = dated[-1].name if dated else None

        for snap in dated:
            entry = build_entry(
                source,
                spec,
                snap,
                date=snap.name,
                current=spec.reads == "dated" and snap.name == latest,
                normalized_dir=normalized_dir,
                warn=warn,
            )
            if entry is None:
                warn(f"{source}/{snap.name}: empty snapshot directory")
                continue
            manifest[f"{source}/{snap.name}"] = prune(asdict(entry))

        # Loose files at the source root: undated on purpose. Current when the
        # source's extractor reads them, or when there is no dated snapshot at
        # all to supersede them.
        loose = build_entry(
            source,
            spec,
            source_dir,
            date=None,
            current=spec.reads == "loose" or latest is None,
            normalized_dir=normalized_dir,
            warn=warn,
        )
        if loose is not None:
            manifest[source] = prune(asdict(loose))

    return manifest


def main(argv: list[str]) -> int:
    if len(argv) != 1:
        print("usage: build_manifest.py <data-root>", file=sys.stderr)
        print("writes <data-root>/manifest.json", file=sys.stderr)
        return 2

    data_root = Path(argv[0]).expanduser().resolve()
    warnings: list[str] = []
    manifest = build(data_root, warnings.append)

    out = data_root / MANIFEST_NAME
    out.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")

    sources = sorted({e["source"] for e in manifest.values()})
    with_norm = sum(1 for e in manifest.values() if "sha256" in e)
    files = sum(len(e["files"]) for e in manifest.values())
    print(
        f"{len(manifest)} snapshot(s) across {len(sources)} source(s), "
        f"{files} raw file(s), {with_norm} normalized CSV(s) digested"
    )
    for w in warnings:
        print(f"  WARN: {w}")
    print(f"manifest: {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

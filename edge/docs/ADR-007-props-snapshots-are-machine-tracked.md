# ADR-007: Props snapshots are tracked in git, and that is a different decision from ADR-001

**Status:** Accepted
**Date:** 2026-09-30
**Deciders:** wubrg

---

## Context

`edgectl board props-report -save` writes a small JSON array under
`edge/props/<season>/week<NN>-<game-slug>-<timestamp>.json`. Each file is the priced output of one
DraftKings capture: one row per outcome, carrying the id, game, market, selection, book, American
price, raw implied probability, de-vigged fair value, breakeven and hold. A typical snapshot is a
few hundred rows and a few tens of kilobytes.

The capture it is built from is a browser `.har` — one to nine megabytes of a browsing session,
mostly images and scripts, that lives in the ingest folder *outside* the repo (`~/adori/Edge/ingest/`)
and is overwritten the next time the same game is re-fetched. Nothing in the repo has ever recorded
what a props capture said.

That gap bit a real report this week: two captures of the same game were taken hours apart, the
lines moved between them, and there was no record of which capture a given scouting number was
built against. The numbers were defensible; which snapshot produced them was not recoverable.

The obvious precedent is ADR-001, which tracks the hand-typed moneyline board in git. But its
reasoning does not transfer, and pretending it does would get the decision right for the wrong
reason.

---

## Decision

**Track `edge/props/` in git. Do not track the raw `.har` captures.**

The snapshots are committed as written. The captures stay in the ingest folder outside the repo and
are not added to it.

---

## Rationale

**This is not ADR-001's reason, and the file should not imply it is.** ADR-001 tracks the moneyline
board because its content is *irreplaceable hand-entry*: a person reads a price off a sportsbook app
and types it in, and no generator can reproduce it, so losing the file loses a season of
observations. None of that applies here. A props snapshot is machine-generated — `props-report`
rebuilds it from the capture in a second — so it is not irreplaceable in ADR-001's sense. If the
reasoning were only "keep the unreproducible thing," props would be gitignored like
`model/data/raw/`.

**The reason to track it is that a snapshot is dated evidence, citable by commit.** A capture is
ephemeral and lives outside the repo; the price it showed exists nowhere once it is re-fetched. The
snapshot freezes that one reading, and a git commit proves *when* it was frozen — the same guarantee
ADR-001 leans on for prices and ADR-002 leans on for belief packs. This is what lets a scouting
report say "built against the week04 PIT@CLE snapshot at commit abc123" and have the citation still
resolve months later, exactly the way belief-pack shas are cited today. The value is provenance and
reference, not rescue of hand-labour.

**The snapshot, not the `.har`, is the right unit to track.** The capture is large, binary-ish, full
of irrelevant session traffic, and would produce enormous diffs that say nothing. The snapshot is
small, structured, diffable, and is the actual thing a report cites. Tracking the derived artifact
and discarding the raw input is the opposite of what a build cache would do, and correct here for
the same reason: the evidence is in the reading, not in the megabytes it was read from.

**The timestamp in the filename is the capture's, not the save's.** `props-report` names the file
with the mtime of the newest capture `readIngest` actually used, so two snapshots of one game taken
hours apart sort and name distinctly — which is the precise failure that motivated this.

---

## Alternatives Considered

### A: Gitignore `edge/props/`, matching `model/data/raw/`

- **Pro:** Consistent with the cache rule; no files accrue in the repo.
- **Con:** There is then no durable record of what any capture said, and no commit to cite. This is
  the exact gap being closed — a report's numbers would again be unattributable to a moment in time.

### B: Track the raw `.har` captures instead

- **Pro:** The literal source is preserved, and a snapshot could be regenerated from it.
- **Con:** Multi-megabyte files of mostly-irrelevant session traffic, with diffs that carry no
  meaning, for a repo whose other tracked data is small and hand-curated. The snapshot already
  captures everything a report needs, so the `.har` buys preservation of bytes nobody reads at a
  cost nobody wants.

### C: Append snapshots to a single log file per season

- **Pro:** One file, matching the betlog/belief-log pattern.
- **Con:** A capture is naturally one file (one game, one moment), and a per-capture file names
  itself by game and time without any merge. A single log would need its own keying for no gain,
  and would make "which snapshot" a line range rather than a path.

---

## Consequences

- A report can cite a specific snapshot path and commit, and the citation keeps resolving.
- Snapshots accumulate over a season. They are small, so this is cheap; a season of captures is a
  few megabytes of JSON.
- Saving is opt-in (`-save`), because a snapshot is a deliberate act of recording evidence, not a
  side effect of reading a capture. A `props-report` run without `-save` writes nothing.
- The raw `.har` remains outside the repo and is still overwritten on re-fetch. If a capture ever
  needs preserving in full, that is a separate decision; this ADR deliberately does not make it.

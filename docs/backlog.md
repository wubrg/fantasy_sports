# Backlog

Written 2026-08-12. **Draft is Thu 2026-09-03 — 22 days out.**

Items are ordered against that date. Above the cutline is work that changes *this year's*
draft outcome; below it is research platform and repo hygiene, which are real but do not
need to happen before September 3rd.

Every claim below carries a `file:line` so it can be checked rather than believed. Sizes are
**S** (hours), **M** (a day or two), **L** (longer, or unbounded until scoped further).

Companion documents: [`league_home/app/internal/draft/data/OPEN-QUESTIONS.md`](../league_home/app/internal/draft/data/OPEN-QUESTIONS.md)
is the decision log for source acquisition and league-vs-model questions. The
`draftroom_2026.md` notes file in the same directory is the user's strategy scratchpad,
symlinked to an Obsidian vault and read by no code.

---

## Above the cutline — before 2026-09-03

### Tier 0 — the phone-editing workflow

`data/leans/wubrg-lean.csv` and `data/draftroom_2026.md` are symlinks into an Obsidian vault
so leans can be edited from a phone. The failure mode of that workflow is silence: an edit
that looks applied but is not.

- **`D1` — Blank leans mean "undecided", not "reject the file".** ✅ **Done 2026-08-12.**
  `ParseLeans` errored on any unrecognized lean, which failed the *whole file* over a single
  blank row and discarded every finished read in it. A blank lean now records no read and is
  reported by name; a *misspelled* lean is still an error, because that one hides a read you
  think you have. `internal/draft/leans.go`, `leansets.go` (`LeanSet.Undecided`),
  `cmd/draftroom/leans.go`.

- **`D2` — Report leans that match no player.** ✅ **Done 2026-08-12.** A lean is applied by
  name — `WalkAway` (`internal/draft/leans.go:95-96`) does a plain `normalizeName` map lookup —
  so a name typed wrong was not an error, not a warning, and simply absent from the board.
  `draftroom leans` now checks each set against the projection source and names what missed,
  with the nearest real player as a suggestion. Caught two live typos in `wubrg-lean.csv`:
  `isaiah lilely` → Isaiah Likely, and `jacorey croskey-merritt` → Jacory Croskey-Merritt.
  `internal/draft/leancheck.go`, `cmd/draftroom/leans.go` `reportUnmatched`.
  The check is offline: the board names players from the projection CSV's own spelling
  (`cmd/draftroom/static.go:163`), so no Sleeper fetch is needed. It degrades to a printed note
  when the private data repo is absent.

- **`D2b` — Let leans use the alias and suffix machinery.** ✅ **Done 2026-08-12.** A lean was
  applied only on an exact spelling match against the projection source, while vendor rows got
  `aliases.csv` and `stripSuffix` through `PlayerIndex.Resolve`. So the pool said
  `Kenneth Walker` and a read saying `Kenneth Walker III` — Sleeper's spelling, and the natural
  one to type — reached nothing. `PoolMatcher` (`internal/draft/leancheck.go`) resolves exact,
  then suffix-stripped, then through names the alias file puts on one Sleeper id; two pool
  players sharing a stem resolve to neither and stay in the unmatched report. Built offline from
  the pool and alias file rather than from `PlayerIndex`, so `draftroom leans` and the board
  agree exactly. `playerIDByName` goes through it too.

- **`D3` — The vault → draftroom loop.** ✅ **Done 2026-08-12.** Lean sets were read once at
  startup (`loadStatic` runs a single time), so a read edited in the vault reached the board
  only after a restart, with nothing on screen to say the file and the board had diverged.
  A **reload leans** button in the board footer re-reads the files and rebuilds — it is a button
  on the page, so it works from the phone the edit was made on. A file that does not parse keeps
  the reads already loaded and reports the error. Deliberately on demand, not a timer: nothing
  about lean sets moves during a draft. Loop documented in `cmd/draftroom/README.md`
  ("Editing leans away from the machine").

- **`D2c` — `stripSuffix` over-strips a surname ending in `i`.** ✅ **Done 2026-08-12.**
  Normalizing before stripping glued the suffix to the surname, so `Kyle Monangai II` became
  `kylemonangaiii`, matched `iii`, and stripped to a player who does not exist. `stemName`
  (`internal/draft/sources.go`) drops a trailing suffix *token* before normalizing, which cannot
  make that mistake. Same fix covers `Rasheen Ali II` and `Mike Gesicki II`.

### Tier 1 — trust the data before doing research on it

- **`D4` — `draftroom sources` diagnostic.** ✅ **Done 2026-08-12.** Rows that failed to resolve
  to a Sleeper id were counted, never named, so `aliases.csv` could only be filled by accident.
  `draftroom sources -unmatched` names each one and prints the `aliases.csv` line that would fix
  it. Two ways of finding a candidate, because failures come in two kinds: edit distance for a
  misspelling, and surname + position + team for a nickname, which is not close as a string —
  the latter only when exactly one player fits. Needs no `-league`: resolution wants names,
  positions and teams, not the keeper ledger, so it is one Sleeper call.
  `cmd/draftroom/sources.go`, `internal/draft/leancheck.go` `ClosestPlayer`.
  Found the live case immediately: **Hollywood Brown → Marquise Brown, id 5848**, in both
  sources. Note this supersedes `OPEN-QUESTIONS.md:118`, which recorded him as absent from
  Sleeper's dictionary — he is there now.

- **`D5` — Validate required columns on source CSVs.** ✅ **Done 2026-08-12.** Only the player
  column had to exist; everything else fell through `pick()` → `num()` → `0`, so a renamed vendor
  column became a column of zeros and the board still rendered. Each source now declares the
  columns whose absence would corrupt a number (`SourceSchema`, `CielyColumns`,
  `SubvertadownColumns` in `internal/draft/sources.go`) and loading fails naming the missing one
  and the header actually read. Accepted spellings mirror `pick()`, so the check cannot reject a
  header the parser would have read; optional columns stay optional.

### Tier 2 — draft-day decision support

Realistically two or three of these land before the draft. `D6` and `D9` are the best value
per day spent; `D11` is the one most likely to miss the date.

- **`D6` — FantasyPros ECR ingest as the base ranking anchor.** Spike **S**, extractor **M**.
  `OPEN-QUESTIONS.md:16-22` records four of five wanted experts as paywalled or JS-rendered,
  but that survey predates a closer look at FantasyPros, which may expose per-expert rankings
  for each. **Timebox a spike to confirm what is actually exportable for free before building
  anything.** Do not scope the extractor against data nobody has confirmed exists.

- **`D7` — Keeper price export with owner and team names.** **S.** So the league can be told
  what their keepers cost. Most of this exists — `internal/draft/project.go` `WriteProjection`
  and `WriteKeeperOptions` already render per-team keeper budgets. This is an output-format
  ticket, not new logic.

- **`D8` — Effective positional scarcity, v1.** **M.** From the notes: taking Gibbs also removes
  Amon-Ra, LaPorta, and Jameson Williams from *your* board, given a no-handcuff and
  one-per-offense preference — so your effective scarcity differs from the league's. Scarcity
  today is league-wide only (`internal/draft/pivots.go`, `tiers.go`). The narrow version is a
  declared preference file plus a board annotation — a filter, not a solver.

- **`D9` — Decide what a lean means: "I like him" or "I like him at this price".** Decision
  **S**, implementation **M**. Raised in the user's own notes, and it changes the meaning of
  every lean already written. Conviction is currently price-blind: `WalkAway`
  (`internal/draft/leans.go:95-127`) scales the model value by ±15% and never reads market
  cost. The cheapest expression is an optional price bound — "up under $30" — and `cap` already
  does half of this for must-haves.

- **`D10` — Keeper simulation into leftover-pool assessment.** **M.** Pseudo-randomly guess the
  league's keepers, then assess what is left and what team shapes it implies. Greenfield.

- **`D11` — Team-composition search, v1.** **L.** From the notes: rather than maximizing value,
  enumerate rosters that fill every starting slot plus 0–3 bench under budget while clearing a
  minimum projected PPG. Explicitly different from climbing ECR, since rank does not price PPG.
  `internal/draft/roster.go:145-244` `Score()` is a greedy lineup filler, not a search. This is
  the most elaborated idea in the notes and the least likely to be finishable in 22 days —
  worth starting only if Tier 0 and Tier 1 are done.

---

## Below the cutline — after the draft

### Research platform

- **`R1` — Shared extractor abstraction.** Each of `data/tools/extract_{ciely,fantasypoints,subvertadown}.py`
  is a bespoke script with hardcoded sheet names (`extract_ciely.py:123`, `"Ranks w Proj"`),
  hardcoded filenames (`extract_subvertadown.py:42`), and hardcoded column positions
  (`extract_subvertadown.py:44-47`). Adding an expert means writing a new standalone script.
  Prerequisite for the multi-expert ambition in the notes.

- **`R2` — Per-expert accuracy backtesting.** The notes want the most accurate experts
  identified and weighted into a collective lean set. `internal/draft/calibrate.go:140-184`
  backtests *spending shape* against results via Spearman ρ, not expert projections — and found
  no signal (every metric ρ < 0.21). Per-expert accuracy needs historical expert projections
  that are not currently retained, so this is blocked on archiving each season's sources.

- **`R3` — Scenario traits.** Port the Edgectl idea: named per-player flags scored into a lean
  to find underpriced tails. Builds on the six traits already in `internal/draft/traits.go:11-41`
  (floor, redzone, upside, targethog, bellcow, discounted) and the one existing generated set
  (`leangen.go:51-58`, `menton` = ≥2 Big-3 traits up, 0 down).

- **`R4` — League-vs-industry positional spend correlation.** Did this league follow industry
  consensus on where to spend? Extends `calibrate.go`. Note that `OPEN-QUESTIONS.md:53-80`
  already did a version of this by hand and concluded the league is normal and Ciely is the
  outlier — so this ticket is about automating a check that has been run once, not an open
  question.

### Repo hygiene

The repo is now three Go modules (`canton/app`, `edge/app`, `league_home/app`) with a
delegating root `Makefile`. The docs did not keep up.

- **`H1` — Root docs describe a repo that no longer exists.** `README.md` omits both `edge/`
  and `draftroom` — the tool under active development is undocumented at the top level.
  `CONTRIBUTING.md:3` says "two independent Go modules" where the root `Makefile:13-19` correctly
  says three. **S**, and the cheapest credibility win in the list.

- **`H2` — Go versions disagree.** `league_home/app/go.mod:3` is `1.24.0`; `canton/app/go.mod:3`
  and `edge/app/go.mod:3` are `1.25.0`. Any single-version CI cannot validate all three. **S.**

- **`H3` — plist templates hardcode absolute paths and an owner ID.**
  `league_home/app/com.draftroom.serve.plist.template:10,25,27,31` embeds
  `/Users/adamwieberg/adori/fantasy_sports/...` and owner ID `243501760939814912`; the canton
  and leagueweb templates embed paths too. Acknowledged as a deliberate trade-off in
  `docs/maintenance/2026-07-08-main-recovery/ADR-0001-plist-conflict-resolution.md:35`, so this
  ticket is to revisit that decision, not to report a surprise.

- **`H4` — Install docs reference placeholders that no longer exist.**
  `canton/app/README.md:123` and `league_home/README.md:350-352` tell the reader to fill in
  `/REPLACE/WITH/...` placeholders. Per `H3` those templates now carry hardcoded paths and have
  no placeholders, so anyone following the documented setup finds nothing to edit. **S.**

- **`H5` — `.gitignore` gaps.** `*-go-tmp-umask` (two present in `canton/app/`), `.fuse_hidden*`
  (six present in `canton/data/`), and `.DS_Store` are untracked and un-ignored, so
  `git status` is permanently noisy. Verify with
  `git check-ignore -v canton/app/canton-go-tmp-umask` — currently no match. **S.**
  *(For the record: `/*.pdf` at `.gitignore:92` already covers the root PDFs, and `git ls-files`
  confirms no built binaries are tracked. Both are fine.)*

- **`H6` — Small duplications across modules.** JSON response writing exists as a helper in
  `league_home/app/cmd/leagueweb/main.go:96-104` and inline in `canton/app/main.go:40-48`.
  Data-root resolution exists only in `league_home/app/internal/draft/dataroot.go` and is
  tangled with draftroom's assumptions. Recorded, not scoped — three modules sharing a little
  duplication is cheaper than a premature shared package.

- **`H7` — Three different service-configuration strategies.** draftroom's plist uses three
  environment variables, leagueweb's uses one commented-out optional variable, canton's uses a
  command-line flag. No convention. **S** to pick one, **M** to converge.

- **`H8` — Directory sprawl outside the repo.** Siblings of the checkout:
  `fantasy_sports-nfl-betting/` (192M, not a git repo), `fantasy_sports-draftroom/` (4M, not a
  git repo), `bak.fantasy_sports/` (a real clone, last commit 2021), `fantasy_football.bak/`.
  Decide keep, fold in, or delete for each. Also one stale agent worktree under
  `.claude/worktrees/`. Not a repo problem strictly, but it is where the confusion about "which
  copy is real" comes from.

---

## Known-good, for the record

Checked 2026-08-12 so they are not re-investigated:

- The private data root is healthy: `../fantasy_sports_data` (37M, no git remote by design,
  snapshotted 2026-08-06) holds all three normalized CSVs — `ciely-2026.csv`,
  `subvertadown-2026.csv`, `fantasypoints-big3-2026.csv`.
- The lean symlink mechanism works. `LoadLeanSet` (`internal/draft/leansets.go:41`) uses
  `os.Open`, which follows symlinks, and `ParseLeans` maps columns by header name, so a set may
  order its columns however is easiest to type. `data/leans/.gitignore` already keeps everything
  but `mine.csv` out of this public repo.

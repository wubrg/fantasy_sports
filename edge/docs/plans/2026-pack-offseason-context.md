---
title: "Putting offseason context in the pack (and what is not available)"
doc_version: 1.0.0
status: PROPOSED — needs the decisions in §6 before any code changes
date: 2026-09-06
owner: wubrg
relates_to:
  - ./2026-w01-belief-forecast.md
  - ../frameworks/belief-probe.md
  - ../ADR-006-pack-carries-offseason-context.md
---

# Offseason context in the pack

## 1. The request

> "Can we include preseason / offseason info into the prompt / report?"

Asked after the 2026 week-1 forecast was written, whose stated weakness was that its entire
evidence base — prior-season scheme identity — is unauditable, and that its reads assume staff
continuity the forecaster could not verify.

## 2. What is actually available — probed, not assumed

Every row below was checked by fetching the file on 2026-09-06, three days before the week-1
opener. Nothing here is inferred from what nflverse "usually" publishes.

| source | pre-kickoff status | what it carries |
|---|---|---|
| `games.csv` `away_coach` / `home_coach` | **populated, 16/16 week-1 games** | the head coach of every team, for every game back to 1999 |
| `games.csv` `away_rest` / `home_rest`, `div_game`, `roof`, `surface`, `stadium` | **populated, 16/16** | rest days, divisional flag, dome vs outdoors, surface |
| `roster_weekly_2026.csv` | **published**, 2946 rows, week 1, `game_type` REG | per player: team, position, `depth_chart_position`, `status` (ACT 1693 / DEV 528 / CUT 421 / RES 277 / RET 23 / EXE 4), experience, college |
| `depth_charts_2026.csv` | **published**, snapshot dated 2026-09-06T11:29:30Z | ordered depth chart, `pos_rank` / `pos_slot`. 47 MB — it is daily snapshots keyed on `dt`, with no week column |
| `players.csv`, `draft_picks.csv` | published | player biography; every draft pick, so a rookie's team and round are checkable |
| `injuries_2026.csv` | **404 — not published yet** | the weekly injury report starts once the season does |
| **preseason games** | **do not exist in this pipeline** | `games.csv` has 7548 rows and *zero* preseason ones: `game_type` is only REG / WC / DIV / CON / SB |
| **preseason play-by-play, snap counts** | **absent** | nflverse's pbp and snap releases exclude preseason; the 2026 files 404 until week 1 is played |

**So the two halves of the question have opposite answers.**

- **Preseason: no.** Not "not yet fetched" — not published anywhere this project draws from. No
  preseason box score, snap count or play exists in nflverse. A `preseason:` claim type would be
  a checker that can never check anything, which `falsify.go` already argues is worse than no
  checker.
- **Offseason: yes, and more than expected.** Head coach, roster status, depth chart and draft
  class are all sitting there, published, before kickoff.

### 2a. A statement in the framework doc is wrong

[`belief-probe.md`](../frameworks/belief-probe.md) and `falsify.go` both say `personnel` claims
cannot be checked because *"there is no depth-chart or coaching table in this repository."*

There is a coaching table. It is two columns of `games.csv`, which is already cached, already
parsed by `beliefpack.py`, and already the source of two of the four base rates. A depth chart is
one HTTP fetch away. That sentence should be corrected whatever else is decided.

## 3. The finding that cannot wait

Head coaches changed at **seven** teams between 2025 and 2026, from `games.csv`:

| team | 2025 | 2026 |
|---|---|---|
| BAL | John Harbaugh | Jesse Minter |
| CLE | Kevin Stefanski | Todd Monken |
| LV | Pete Carroll | Klint Kubiak |
| MIA | Mike McDaniel | Jeff Hafley |
| NYG | Brian Daboll | John Harbaugh |
| PIT | Mike Tomlin | Mike McCarthy |
| TEN | Brian Callahan | Robert Saleh |

The week-1 forecast did not know this. Its `pass_heavy` reads are scheme-identity reads, and
scheme identity is a property of the staff:

| row | belief | flagged | the claim it rests on | status |
|---|---|---|---|---|
| `PIT/pass_heavy` | 0.12 | **yes** | "the offensive staff's prior offences called run at well above expectation" — that was Arthur Smith's, under Tomlin | **premise gone** |
| `BAL/pass_heavy` | 0.13 | **yes** | "heavy-personnel run identity around a mobile quarterback" — Harbaugh's | **premise gone** |
| `BAL/efficient_offense` | 0.53 | **yes** | same staff assumption | **premise gone** |
| `SEA/pass_heavy` | 0.15 | **yes** | "wide-zone and play-action identity" — that was Klint Kubiak's, who is now Las Vegas's head coach | **premise weakened** |
| `MIA/pass_heavy` | 0.30 | no | McDaniel's offence | premise gone |
| `CLE/pass_heavy` | 0.31 | no | Stefanski's run-lean identity; Monken's are not | premise gone |
| `LV`, `NYG`, `TEN` | abstained | no | — | the abstentions happen to be right |

**Four of eleven flagged rows rest on a staff that no longer exists.** Nothing has been ingested
yet, so nothing is corrupted — but the file as committed is wrong in a way that two columns of an
already-cached CSV would have caught. That is the argument for this change, made concrete.

## 4. Proposal

Three tiers, cheapest first. They compose; each is useful alone.

### Tier 1 — head coach and schedule context (recommended)

`beliefpack.py` adds to each game: `home_coach`, `away_coach`, `home_coach_is_new`,
`away_coach_is_new` (compared against the prior season's rows in the same file), `home_rest`,
`away_rest`, `div_game`, `roof`, `surface`.

- **Cost: near zero.** All of it is in `games.csv`, which the pack already opens.
- Renders into the SLATE table of `weekN.prompt.md` as extra columns.
- `falsify.go` gains a `coaching` claim type (or extends `personnel` to be checkable when it names
  a coach), adjudicated against the pack's coach fields — so "PIT — run-committed play-caller" is
  no longer unauditable when it names the wrong coach.

### Tier 2 — roster status

Per team, from `roster_weekly_<season>.csv`: counts by status, and the named players at QB / RB /
WR / TE whose `status` is not ACT. This is the closest pre-week-1 substitute for the injury report,
which does not exist until the season starts.

- **Cost: one 933 KB fetch a season.**
- Makes `usage`-adjacent claims checkable earlier than `injuries_*.csv` can.

### Tier 3 — depth chart

`pos_rank` 1 at each offensive position, from the latest `dt` snapshot at or before kickoff.

- **Cost: 47 MB a season**, and a snapshot-nearest join. `nflverse.py` already declines this
  table, in a comment, for exactly those two reasons. Tier 3 means overturning that decision, and
  it should not be done casually.

## 5. Arguments against, which are real

1. **It is not free information.** Every fact added to the pack is a fact the *incumbent and the
   line model do not get*, but also one the forecaster can no longer claim as an outside read. The
   probe measures `s_you − s_reference`; handing the forecaster the coach narrows what "outside
   knowledge" means. That is fine — it converts unauditable claims into checkable ones, which is
   the stated goal — but it is a change to what the experiment measures, not just a data addition.
2. **The coach column is head coaches only.** PROE is called by the offensive coordinator, and no
   OC table exists anywhere in nflverse. Tier 1 would have caught PIT and BAL; it would *not* have
   caught Seattle losing its play-caller while keeping its head coach. Tier 1 is a large
   improvement and not a solution.
3. **More checkable claims means more rejected predictions.** That is the point, but it will make
   the first few weeks' survivor counts look worse, and that has to be expected in advance rather
   than explained afterward.
4. **Scope creep against a three-day clock.** Week 1 kicks off 2026-09-09 20:20 ET. Tier 1 is a
   small change; Tiers 2 and 3 are not week-1 work.

## 6. OPEN DECISIONS

**Q3. The week-1 forecast — revise or freeze?**
- **(a) Revise before kickoff.** Fold the seven coach changes in, re-emit, re-commit. Legitimate:
  nothing is ingested, and the deadline is the kickoff, not the first draft. The forecast gets
  better and four bad flags are withdrawn.
- **(b) Freeze it.** Keep it as the honest record of what a forecaster *without* offseason data
  produces, and let it settle. Its errors then become the measured argument for Tier 1 rather than
  a hypothetical one — at the cost of knowingly logging four flags built on a false premise.

**Q4. Which tier, and when?** Tier 1 before week 1, or all of it after week 1 as one considered
change to the pack format?

**Q5. Does the `report` half of the question mean the ingest/score output too?** `beliefs ingest`
currently reports `checked / deferred / unverifiable / untyped`. If coaching becomes checkable it
should report that separately, so the "0 checked" line that made this run unauditable is visibly
fixed rather than quietly absorbed.

## 7. Tracker

- 2026-09-06 — availability probed, findings recorded, nothing implemented. Blocked on Q3/Q4/Q5.

## 8. Changelog

| version | date | change |
|---|---|---|
| 1.0.0 | 2026-09-06 | First issue. Probes what preseason and offseason data exists, records that preseason does not exist at all while offseason does, documents the seven 2026 head-coach changes and the four flagged week-1 rows they undermine, and proposes a three-tier pack extension. |

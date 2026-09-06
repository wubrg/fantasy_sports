# ADR-006: The pack carries offseason context; preseason does not exist to carry

**Status:** Accepted 2026-09-06. Tier 1 implemented; Tiers 2 and 3 deferred until after week 1
**Date:** 2026-09-06
**Deciders:** wubrg

---

## Context

The 2026 week-1 forecast produced 55 claims of which **zero** were checkable, because the only
claim types the pack can adjudicate are `form`, `market` and `schedule`, and the forecaster's
actual evidence was prior-season scheme identity — a `narrative` claim by the taxonomy's own
definition. `falsify.go` says that is unauditable *"because there is no depth-chart or coaching
table in this repository."*

That statement is false. `games.csv`, already cached and already parsed by `beliefpack.py`, carries
`away_coach` and `home_coach` for every game since 1999, populated for all sixteen 2026 week-1
games three days before kickoff. Seven teams changed head coach between 2025 and 2026, and four of
the eleven rows the week-1 forecast flagged rest on staffs that no longer exist.

Separately, and unlike the offseason: **preseason data does not exist in this pipeline at all.**
`games.csv` holds 7548 rows and none of them are preseason (`game_type` ∈ REG/WC/DIV/CON/SB), and
nflverse publishes neither preseason play-by-play nor preseason snap counts.

## Decision (proposed)

1. **Add offseason context to the pack, starting with head coach and schedule context** — the Tier
   1 set in the plan: coach per side, a `coach_is_new` flag computed from the prior season's rows,
   rest days, divisional flag, roof and surface. All from `games.csv`; no new fetch.
2. **Make coaching claims checkable.** A claim naming a coach is adjudicated against the pack's
   coach fields, on the same conservative rule as everything else in `falsify.go`: falsified only
   when it names a coach the pack says is not there, never merely for being vague.
3. **Do not add a `preseason` claim type, and say why in the doc.** A declared type with no
   possible data is precisely the checker-that-passes-everything the framework already rejects.
4. **Correct the "no coaching table" sentence** in `belief-probe.md` and `falsify.go` regardless of
   whether 1–3 are adopted, because it is currently a false statement about the repository's own
   contents that has shaped what forecasters are told they may not be held to.

## Consequences

- The claim tally stops reading `0 checked` for a forecaster whose real evidence is scheme
  identity, which is the single largest gap in the week-1 run.
- Rejected-prediction counts will rise. That is the mechanism working, and it should be expected in
  writing beforehand — the survivors-versus-all comparison exists for exactly this.
- The pack format changes, so packs generated before and after are not byte-comparable. Any
  outstanding forecast bound to an old sha (see
  [ADR-003](./ADR-003-week01-binds-to-the-pack-as-pasted.md)) must be resolved first, or it will be
  invalidated twice over.
- **This narrows what "outside knowledge" means.** A fact in the pack is a fact the forecaster can
  no longer be credited for knowing. The probe measures `s_you − s_reference`, and moving coaching
  from the forecaster's side of that line to the pack's is a change in what is being measured, not
  only in what is being checked. It is worth it — an unauditable read is worth little — but it is
  not a free addition and should not be presented as one.

## Limits, stated so they are not discovered later

- **Head coaches only.** Pass rate over expectation is a coordinator's signature, and no
  coordinator table exists in nflverse. This would have caught Pittsburgh and Baltimore; it would
  not have caught Seattle losing its play-caller while keeping its head coach.
- **No injury data before week 1.** `injuries_2026.csv` returns 404 until the season starts, so the
  `injury` claim type stays deferred-and-unchecked for week 1 no matter what is adopted. Roster
  `status` (ACT/CUT/RES/DEV) is the nearest available substitute and is Tier 2, not this ADR.

---

## As built, 2026-09-06

- `beliefpack.py` writes `coach`, `coach_is_new` and `rest_days` per team, and `div_game`, `roof`
  and `surface` per game. `coach_is_new` compares against the **set** of coaches that team had in
  the prior season, so a mid-season replacement is not miscounted as new the following year, and it
  is `null` — not `false` — when the prior season is absent.
- The pasteable prompt gains a `venue` column and a **STAFF** table, which states in the pack
  itself that these are head coaches only.
- `falsify.go` adjudicates a new `coaching` claim type. Clearing is deliberately more generous than
  convicting: any mention of the pack's coach clears a claim, including a bare surname, while only
  a full name can convict — so *"McCarthy replaces Tomlin"* passes. Surnames match with one edit
  allowed, because `games.csv` currently spells Las Vegas's head coach "Kubliak" and a forecaster
  who spells it correctly must not be convicted by a typo in the fact it is checked against.
- Not done, deliberately: no new line in the ingest report. Coaching claims flow into the existing
  `checked` counter, which is where a reader already looks.

Measured effect on the week-1 file: **0 checked claims → 14**, with 0 rejected.

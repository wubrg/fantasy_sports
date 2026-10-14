# Projection log: is our median a better fit than the market's line?

Status: approved by user 2026-10-14, pending implementation plan.

## Problem

Every URPS report prices player props against `conditionals.json` (the fitted grid) and the live
DK capture, but nothing records what the grid actually *said* once the price is typed into a
report's markdown. Two different questions get conflated as a result:

1. **Did a recommended wager hit?** — already answered. `edgectl bet place -predicted <p>` plus
   `betlog.Score` already measures this, for every wager that actually got placed.
2. **Was our median a better point-estimate of what the player would do than the market's
   posted line?** — not answered by anything. This is the question that actually matters for
   judging the grid's skill: a report can type "Puka Nacua's grid median is 52 receiving yards,
   DK's line is 60" and correctly decline to recommend an Over, because the market and the grid
   disagree and the market usually wins that argument — but whether the grid's 52 or the book's
   60 ends up closer to what Nacua actually does is a real, checkable fact, and today nothing
   checks it. It only exists as prose in an Obsidian markdown file, for the one week until the
   file is superseded by the next one.

The distinction matters because recommendation count is small and selection-biased (we only
recommend when we think we disagree with the market *and are right*), while every priced
player/outcome this season — recommended or not — is a data point for "does the grid's median
track reality better than the book's own number." That is a much larger, much less biased sample,
and it is the sample `model/FINDINGS.md`'s existing backtest methodology would want if it existed
at the per-line level instead of only the per-bet level.

## Goals

- Record, at report-writing time, every player/outcome/scenario the grid actually prices: our
  cell median, the market's posted line, and enough context to settle it later (player, outcome,
  season, week, scenario).
- After the week is played, settle each recorded projection against the player's realized stat
  line.
- Score the settled population in aggregate: is the grid's median closer to reality than the
  market's line, how often, and by how much — with the comparison broken out by outcome
  (`receiving_yards` is not `passing_yards`) the same way existing calibration is broken out by
  source, because a pooled number would hide which outcome actually carries skill.
- A new, fresh-context `review-week` skill (Feature 1, spec'd separately after this one) consumes
  this log plus the existing belief-log and betlog scoring machinery to produce a per-week retro.

## Non-goals (YAGNI)

- **No change to recommendation logic.** What makes a report recommend a wager is unchanged;
  this only adds a record of what was priced, independent of whether it became a recommendation.
- **No replacement for `betlog.Score` or `beliefs score`.** Those already measure "did the
  wager/scenario calibrate correctly" and keep doing exactly that. This is a third, independent
  layer, not a replacement for either existing one.
- **No retrofit of old reports.** Week 1–6 reports are not re-parsed to backfill this log. The
  record starts whenever this ships; everything before it is out of reach (the same stance the
  Week 5 "Late round sleepers" cross-reference and every other retroactive-data request this
  season has taken — reports are prose, not a data source to mine).
- **No logging of REFUSED cells.** A refusal has no median to compare; nothing to record.
- **No new database engine.** Same append-only JSONL convention as the betlog, ledger, and belief
  log — confirmed with the user as the right fit over a queryable store like SQLite, since the
  data volume is small (one season's worth of priced props) and the review skill can hold it all
  in memory.

## Design

### 1. Data model — `Projection` / `SettledProjection`, a third record type in `internal/betlog`

The `betlog` package already hosts two parallel record kinds with their own files
(`betlog.go`'s `Bet`/`Settled`, `belief.go`'s `Prediction`/`SettledPrediction`), sharing one
JSONL stream format and one `scan()`/`Append()` substrate. A `Projection` is a third kind,
following the same shape, in a new `app/internal/betlog/projection.go`:

```go
// Projection is what the grid said about a player/outcome, with nothing
// staked and no recommendation implied — present whenever the grid prices a
// cell, whether or not the report ends up recommending a wager on it.
type Projection struct {
    Season   int    `json:"season"`
    Week     int    `json:"week"`
    Player   string `json:"player"`
    Outcome  string `json:"outcome"`  // receiving_yards, passing_yards, ...
    Scenario string `json:"scenario"` // shootout, pass_heavy, ...

    OurMedian  float64 `json:"our_median"`  // conditionals.CellMedian, the grid's own units
    MarketLine float64 `json:"market_line"` // the operator-supplied -line, same units

    Baseline float64 `json:"baseline"` // the player's own prior mean, for context
    Trend    float64 `json:"trend"`

    InputPack    string `json:"input_pack,omitempty"`     // same binding discipline as Prediction
    InputPackSHA string `json:"input_pack_sha256,omitempty"`

    GeneratedAt time.Time `json:"generated_at"`
    Kickoff     time.Time `json:"kickoff"`
}

// SettledProjection is a projection joined to what the player actually did.
type SettledProjection struct {
    ID         string
    Recorded   time.Time
    Projection Projection
    Actual     *float64 // nil until settled; a pointer so "0 yards" and "not yet known" differ
    SettleAt   time.Time
    Note       string
}
```

Same predates-kickoff discipline as `Prediction.Record` (`GeneratedAt` and the wall clock both
checked against `Kickoff`), for the same reason: a projection recorded after the game started is
not a projection, and `beliefs/` is git-tracked specifically so the commit timestamp is
independent proof of timing (ADR-002) — this new log lives in the same directory
(`beliefs/projections.jsonl`) and inherits that proof for the same reason.

### 2. Write path — a new `-projections <path>` flag on `edgectl scenario`

`edgectl scenario` already computes everything a `Projection` needs in one invocation: the
grid's `CellMedian` (already in the pricing result — `conditionals.go:496,705`) and the
operator-supplied `-line` (the market's number), alongside baseline/trend already passed in from
`player.py`'s printed command. This is a **new flag**, not a repurposing of the existing `-log`:
`-log` keeps its current behavior unchanged (writing a real `betlog.Bet`, for the cases where a
wager really is being tracked that way), and `-projections <path>` is new, additive, and fires
independently — a single `edgectl scenario` call can pass both, neither, or just one.

The URPS report-writing workflow changes to make `-projections` **non-optional**: every scenario
pricing call made while writing a report passes it, whether the result becomes a named
recommendation or not (per the user's explicit scope decision — the 52-vs-60 example where no
recommendation was made is exactly the case this must still capture). This is a change to
`.claude/skills/urps/SKILL.md`'s workflow section, not a code change, and ships alongside the Go
work.

### 3. Settle path — mirrors `beliefpack.py outcomes` → `edgectl beliefs settle`

A new script, `model/analysis/projections_outcomes.py`, reads `stats_player_week_<season>.csv` —
the same file `player.py`'s `_rows()` already reads — for the week just played, and for every
unsettled `Projection` in `beliefs/projections.jsonl` matching that season/week, emits the
player's realized value for that outcome as a JSON file. It imports and reuses `fit_conditionals`'s
`OUTCOMES` dict (the same `yards_field`/`opp_field` column mapping `player.py` already uses) so
the realized-value lookup can never drift from what the grid itself was fit on — a second,
independent column mapping would eventually disagree, silently. A new Go command, `edgectl
projections settle -file <that>`, ingests it and appends `SettledProjection` entries, the same
two-step shape as `beliefpack.py outcomes` → `edgectl beliefs settle`.

A player who didn't play (inactive, bye misread, whatever) settles with `Actual: nil` and a note
explaining why — same "pending, not wrong" discipline `outcomes()` already uses for games not yet
played (`beliefpack.py:526`, the `pending` field).

### 4. Score path — new math, not reused

`betlog.Score` and `beliefs score` both measure *probabilistic calibration* (Brier score,
reliability/resolution) — the right tool for "did a stated probability come true at the stated
rate," the wrong one for "was this point-estimate closer to reality than that one." A new
`edgectl projections score -file <path>` computes, per outcome (not pooled — `receiving_yards`
and `passing_yards` scored separately, mirroring `BySource`'s existing reasoning that a pooled
number hides which population actually carries the skill), **over settled rows only** (`Actual !=
nil` — a pending row contributes to nothing until it has a real outcome, same as `beliefs score`
excluding ungraded predictions):

- **Our MAE** = mean(|OurMedian − Actual|), **Market MAE** = mean(|MarketLine − Actual|).
- **Win rate** = the fraction of settled rows where |OurMedian − Actual| < |MarketLine − Actual|.
- **Signed bias** = mean(OurMedian − Actual) and mean(MarketLine − Actual) — do we or the market
  systematically project high or low, not just how far off on average.
- Sample size `n` on every row, since a win-rate on eight settled props means nothing and the
  output must say so rather than imply confidence it doesn't have (same spirit as `betlog.Score`'s
  existing Wilson-interval discipline on small samples).

## Testing

- `internal/betlog`: unit tests for `Projection.Validate`, `Record` (predates-kickoff gate, same
  two-clock check as `Prediction.Record`), `LoadProjections`, and the new score function — pin a
  worked example (3–4 settled rows with known MAE/win-rate) the way `ev_test.go` pins worked
  examples from the source documents.
- CLI: `edgectl scenario -projections <path>` writes a well-formed `Projection`; `edgectl
  projections settle` ingests a realized-outcomes file and produces correct `SettledProjection`
  rows, including the `Actual: nil` pending case; `edgectl projections score` matches the pinned
  worked example.
- Python: the new outcomes-emitting function/script reads a real cached `stats_player_week_*.csv`
  and returns the right realized value for a known real player/week — same "read it off the same
  data" discipline as `player.py`'s own test (mentioned in `FINDINGS.md`) pinning it against
  `fit_conditionals.build()`.

## Migration

None. `beliefs/projections.jsonl` is a new, empty file; no existing data moves or changes shape.

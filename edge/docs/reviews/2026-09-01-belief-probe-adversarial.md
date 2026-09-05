# Adversarial review of the belief probe, 2026-09-01 — findings and triage

Three reviewers, all read-only, all against `wubrg/belief-probe`: a **red team** attacking the
endpoint as an adversary who wants to pass it, a **hobbyist** using the thing as documented for one
real week, and a **statistical adversary** attacking the measurement. `make -C edge check` and
`go test ./...` pass. Nothing below is a broken build. Everything below is a measurement defect, a
documentation defect, or a hole an adversary can walk through.

## The short version

**The harness is sound and the endpoint is not.** The log, the hashing, the frozen references, the
dual-clock kickoff gate, the abstention plumbing, the clustering machinery, the ingest refusals —
all verified, all worth keeping. But the two legs that carry the experiment are broken:

- **E1's opponent is a stale constant, and the pack hands the forecaster the exploit.** A
  four-parameter logistic on the total and spread — the numbers the pack itself supplies — beats the
  base rate by **+0.029**, three times the effect the plan exists to detect, and passes E1 in **7 of
  8** single-season windows. No football knowledge is involved.
- **The falsifier is instructed by its own specification not to emit checkable claims**, and its one
  reliable behaviour is rejecting true ones.
- **The power table overstates by 25 points** at the size the decision rests on, and the pre-registered
  week-8 decision point does not have the power it was chosen for.

And the file we tell the operator to paste contains no instructions.

## Reconciling the reviewers

The hobbyist and the red team returned findings that look like flat contradictions. Both are correct,
and the reconciliation is itself the most important result of the review.

| | hobbyist | red team | statistician |
|---|---|---|---|
| null forecaster | 22 runs, **zero** passes — "exemplary" | constant `0.4343` passes **both** endpoints | line-only logistic passes E1 in 7/8 windows |

The hobbyist's null was **noise** — random beliefs, which the endpoint correctly rejects. The red
team's was **systematic shading toward a reference known to be biased**, and the statistician
produced the practical version of the same attack. The endpoint resists noise and does not resist
exploitation, because the exploit does not require being right about football — only about which
direction the reference is stale in, a fact the repository publishes in the artifact.

The second apparent contradiction resolves the same way. The red team measured a **39%** false-positive
rate at one committed game against a nominal 2.5%; the hobbyist measured that a *genuinely useful*
forecaster passes only about **10%** of the time at the abstention rate the contract asks for. These
are not in tension:

> **The endpoint is simultaneously too easy to pass by gaming and too hard to pass honestly.**

That is what a test measured on the wrong population looks like. The hobbyist's conclusion follows
directly and is the sharpest sentence any reviewer wrote: week 8 will most likely print NEITHER, and
that will not mean "no edge" — it will mean "not enough positions." For a project whose identity is
recording real negatives, that is the worst available outcome, because it is a negative that means
nothing.

---

## Findings

Grades are mine after verification, not always the reviewer's. Where I raised or lowered one, I say so.

### C1 — the reference is beatable with no football knowledge, and the pack supplies the exploit · critical · verified

Merges the statistician's CRITICAL 1 and the red team's constant-forecaster attack; they are the same
defect at two levels of sophistication.

**The base-rate reference is stale by construction.** `beliefpack.py base_rates()` reads `base_rate`
from `belief.json`. That artifact *also* stores `base_rate_held_out`, and I verified the two disagree
in the file:

```
"base_rate": 0.3243   "base_rate_held_out": 0.3721     efficient_offense   +0.048
"base_rate": 0.3353   "base_rate_held_out": 0.2648     pass_heavy          -0.071
```

The drift is concentrated in exactly the two decision-weighted scenarios; `shootout` and
`blowout_loss`, which carry a market reference, are clean. A forecaster that says nothing but "0.38
everywhere" collects `(0.056)² = +0.0031` — about **30% of the gain the plan's target edge
corresponds to** — for free.

**The incumbent reference is biased too, band by band.** `freezeReferences` uses the in-sample
`band.P`; every band also stores `held_p`, and correcting nothing but that bias earns +0.00175 and
+0.00151 on the weeks-4-onward rows.

**The practical exploit.** The statistician fitted a four-parameter logistic on `1`, `(total−45)/5`,
`expected_margin/7`, `|expected_margin|/7`, leave-one-season-out on 2018–2025, scored against the
pack's base rate with the shipped game-clustered bootstrap:

```
efficient_offense  commit every row              n 4222  gain +0.02934  lo +0.02438  PASS
efficient_offense  abstain unless |p-base|>0.10  n 2463  gain +0.04957  lo +0.04101  PASS
efficient_offense  abstain unless total>=48/<=41 n 1945  gain +0.04230  lo +0.03381  PASS
pass_heavy         abstain unless |p-base|>0.10  n 1434  gain +0.02402  lo +0.01787  PASS

one 8-week window, scored as the pre-registered command would:
  efficient_offense  n~241  mean gain +0.02546  E1 PASSES in 7/8 windows
```

**Abstention makes the exploit stronger, not weaker** — +0.029 committing everywhere, +0.050
committing only where the line-derived number sits >0.10 from the base rate. The hand-codable version
is *"abstain unless the posted total is ≥48 or ≤41."*

This is the finding that most damages the plan's own defence of abstention. The plan argues that
conditioning on abstention is legitimate because *"it conditions on a pre-outcome property of the
forecast."* True, and insufficient: the pre-outcome property here identifies **where the reference is
predictably wrong**, not where the forecaster is skilful. That distinction is not in the plan, and it
needs to be, because it governs every filter in the design.

The red team's degenerate version — one constant, zero claims, AUC 0.500, resolution 0.0000 — passes
both endpoints:

```
E1 accuracy  paired Brier gain +0.06584  [+0.03441, +0.09727]  PASS
E2 profit    realised edge     +0.3348   [+0.1920,  +0.4777]   on 28 wagers  PASS
VERDICT      BOTH PASS
```

**Fix.** Use a rolling recent-seasons rate, re-derived each season, not the in-sample constant. Freeze
`band.Held_p` rather than `band.P`. Add a **third mandatory reference**: the line-only logistic,
fitted once and versioned — it is the honest null for "does outside knowledge add anything," and E1
against it is the only version of E1 that means what the plan says. Report E1 per
(scenario × reference-type); never pool across reference types.

### C2 — the falsifier cannot fire on a compliant forecaster, and its one reliable behaviour is false accusation · critical · verified

The statistician extracted `falsify.go` verbatim into a harness with the spec's own example pack:

```
the spec's own example (prose, no numbers)          survives  checked=0
a forecaster obeying "do not restate a pack number" survives  checked=0
TRUE  "form: KC — averaged 27 points over last three"   FALSIFIED
TRUE  "form: KC — 4 giveaways in three games"           FALSIFIED
FALSE "form: KC — success rate 0.45 is a mirage..."     survives
"market: DEN@KC — total 47.5, and it has moved 6"       survives
"schedule: KC - at home"  (ASCII hyphen)                survives, untyped
TRUE  "schedule: DEN — away from home"                  FALSIFIED
```

Five distinct defects:

1. **The specification instructs the forecaster to avoid the only checkable claims.**
   `belief-probe.md` says *"Do not restate a pack number as a claim."* A compliant forecaster emits
   `injury`/`personnel`/`narrative` only, and `Checked` is **zero**. Test A is not weak; on a
   compliant forecaster it is structurally empty. The spec's own worked example falsifies nothing.
2. **`usage` and `injury` are permanently disabled, not deferred.** I verified
   `falsify.go:61`: `var deferred = map[string]bool{"usage": true, "injury": true}` — an
   unconditional constant. `injuries_2026.csv` will exist by week 2 and nothing will notice. These are
   the two types the specification's own preamble names as the historical failure mode, so coverage
   is empty precisely where the documented risk is.
3. **`checkNumbers` inverts the rule.** It falsifies a claim whose numbers match *nothing* in a
   three-number fact set — a test for "is this number in the pack," not "is this number false." The
   code's comment says it "errs toward letting things through"; the measured behaviour for `form` is
   the exact opposite.
4. **One true number immunises a claim.** The loop returns on the first match, so an invented figure
   travels free beside a real one.
5. **The `schedule` branch is buggy** — `strings.Contains(low, "home")` fires on "away from home" and
   "not at home" — and `claimRE` requires an em or en dash, so an LLM's default ASCII hyphen makes
   every claim untyped and unchecked.

**Consequence for Test B, which is worse than the finding itself.** Rejected predictions are excluded
from the edge score. Since the reliable firing mode is false accusation on concrete-and-true claims,
the survivor set is biased by removing the predictions whose reasoning was *most specific*. Survivors
would then score **worse** than the full set, and that reads as "the falsifier is worth nothing" — a
correct-looking conclusion drawn from a broken instrument.

The hobbyist found the complementary hole: a fabricated `form: CHI — prior success rate .451` passes
in week 1, because there is no form to check against for three weeks. The falsifier is blindest
exactly where the plan says the interesting weeks are.

**Fix.** Default `form` and `market` to unsupported-not-contradicted; falsify only on direct
contradiction of a *named* quantity, parsed from the assertion. Require all stated numbers on a
matched quantity to agree. Accept `-`, `--`, `:`. Word-boundary the home/away test with negation
handling. Wire `injury` to `injuries_*.csv` before week 1 — **or state plainly in the plan that Test A
measures nothing and remove it from the framing.** Half-fixing this is worse than deleting it.

### C3 — the endpoint's arithmetic does not survive contact with its own assumptions · critical · verified

Four independent errors, all in the same direction, compounding:

**The coded test is one-sided at 2.5%, not 5%.** I verified `calib.go:519`:
`loIdx := int(alpha / 2 * float64(len(vals)))`. Passing `alpha = 0.05` and reading the lower bound is
a one-sided test at α = 0.025. Reproduced against the shipped code, 600 trials:

| n | edge | as coded (2.5%) | genuine 1-sided 5% | **plan's table** |
|---|---|---|---|---|
| 180 | +0.10 | 29% | 39% | **52%** |
| 360 | +0.10 | 52% | 64% | **80%** |
| 480 | +0.10 | **64%** | 75% | **90%** |
| 1080 | +0.05 | 43% | 54% | **68%** |

The table matches at small n and diverges upward as n grows — systematically optimistic in exactly the
regime the decision rests on. Week 8 was chosen as *"the first point the power table puts above 80%
for the +0.10 edge that matters."* **At the rule as coded it is ~64%.**

**Clustering is on the wrong axis.** The statistician *refutes* the seeded doubt about cross-scenario
correlation — `pass_heavy` and `efficient_offense` for the same team in the same game already share a
`game_id`. The uncaptured axis is **a team across weeks**:

| scenario | within-game DEFF | same-team ICC | DEFF over 8 weeks |
|---|---|---|---|
| `efficient_offense` | 1.12 | +0.106 | **1.74** |
| `pass_heavy` | 1.03 | +0.142 | **2.00** |

Measured coverage of the shipped `BootstrapCI` at nominal 95%: **86.6%** and **84.6%**. The one-sided
false-positive rate roughly triples. And this is a floor — an LLM's read of a team persists across
weeks too, so the ICC of the *gain* exceeds the ICC of the outcome.

This also breaks the plan's arithmetic. The "60 effective per week" figure is **correct for one week**
(`32/1.109 + 32/1.022 = 60.2` — the statistician explicitly refutes any objection to it). The error is
multiplying it. At the measured team ICCs, week 8 gives `256/1.74 + 256/2.00 ≈ 275`, i.e. **~34
effective per week, not 60.**

**The population is smaller again than that.** The hobbyist's honest week 1 produced 93 abstentions and
19 positions. Eight weeks of that is ~152 positions, not 480 — at which a *useful* forecaster passes
about **10%** of the time.

**The table is not reproducible from anything in the repo.** There is no simulation script under
`edge/model/analysis/`. A pre-registered decision point derived from an unversioned simulation is not
pre-registration in the sense the document claims. The scratchpad harness that appears to back it
parameterises "edge" as a shrinkage coefficient rather than a probability-point edge, gives every row
a conditionally-unbiased reference from week 1 (so the base-rate regime is never exercised), and draws
outcomes independently despite assigning shared `game_id`s (so clustering does nothing).

The hobbyist adds the operational consequence: the VERDICT and POWER lines **contradict each other on
screen**, printing a decision beside a note saying the decision is not supported.

**Fix.** Pass `alpha = 0.10`, or state the endpoint honestly as α = 0.025. Cluster on `team-season`.
Regenerate the table with the shipped code, commit the script, and move the decision point to the week
the corrected table actually supports.

### C4 — the file we tell the operator to paste contains no instructions · critical · verified

`make belief-pack` prints `<- paste this` against `week01.prompt.md`. I verified: **42 lines, zero
occurrences of "json", zero of "abstain"** — no schema, no scenario definitions, no output contract.
Paste it and you get prose back. This is the first action, on the first Friday, of an eight-week
commitment, and it fails silently.

---

### S1 — E2 does not measure the wager the plan describes · serious · reviewer-verified

The statistician **refutes** the seeded doubt that E2 is underpowered: at 150 wagers it has 56% power
at a +0.10 `s`-edge and 91% at +0.15. It is not decoration. The defects are elsewhere, and they are
worse, *because an underpowered test returns "undecided" and a mis-specified one returns "PASS."*

1. **The units are mislabelled.** `RealisedEdge` returns `mean(y − ref·(1+hold))` — per unit of
   *maximum payout*. The doc-comment and CLI call it "per unit staked," which is
   `(y − P_book)/P_book`, smaller by ~3–10×. FINDINGS §16's oracle bound (+7% to +18%) is in the
   second unit. **E2's number and the bound this project is calibrated against cannot be compared** —
   which is the one comparison a reader will make.
2. **The market does not exist.** You cannot bet `efficient_offense`. E2 prices a fictional direct
   market at `s_ref·(1+hold)` — it asks "would you beat the incumbent if the incumbent were a
   bookmaker." For weeks 1–3 that bookmaker is a constant.
3. **The hold is levied in the wrong space.** The plan's own inequality is
   `s_you − s_book > P_book·hold/(q−r)` ≈ **+0.042**. `RealisedEdge` charges `s_ref·hold` ≈ 0.018, in
   `P`-space, never dividing by `(q−r)`. At `s_ref = 0.30` and true skill `+0.02`, E2 reports a
   profit on a wager that is negative-EV. **E2's pass region strictly contains losing prop
   strategies**, and `-bar` does not save it — the bar thresholds disagreement, not whether the edge
   clears the requirement.

**Fix.** Either rename it to what it is and stop calling E2 "the wagers it implies would have won," or
make it real: take `q` and `r` from the validated site, wager only when the inequality clears, and
report `(y − P_book(1+hold))/(P_book(1+hold))`. The second is a small change and will cut the
over-bar count sharply — which is itself the finding.

### S2 — the forecaster is scored against a number it was told not to compute · serious · verified

The pack says converting a line into a probability *"is the tool's job."* Then `auto` scores
`shootout` and `blowout_loss` against `s_market`. The hobbyist's honest gut mapping differed from the
tool's by up to **0.082** on totals and **0.46 vs 0.638** on a spread. Of 17 rows clearing the bar,
three were rows they had marked `abstained`. The measured "edge" on those scenarios is tail-model
mismatch, not judgement.

Compounding it: **instruction 3 guarantees a loss.** *"Where you have no read, give the base rate"* —
but for `shootout` the reference is the market, so on a 38.5 total that is a −0.146 error **by
instruction**.

### S3 — `Report.Slope` and `Report.Intercept` are swapped · serious · verified

I verified both ends. `calib.go:359` declares
`func CalibrationSlope(pts []Point) (a, b, se float64, converged bool)` with `a` the intercept;
`calib.go:158` assigns `r.Slope, r.Intercept, r.SlopeSE, r.Converged = CalibrationSlope(live)`.

On 40,000 perfectly calibrated synthetic forecasts the CLI prints `CALIBRATION SLOPE 0.004` under a
line of prose reading *"1.0 is honest; below 1 means over-confident."* On a forecaster with true slope
0.554 it prints 0.006. **The reported slope never moves with the slope**, and `SlopeSE` — correctly the
variance of `b` — is attached to the wrong number. The package tests destructure `CalibrationSlope`
directly and so test around the bug; nothing asserts on `Report.Slope`.

### S4 — anti-anchoring is illusory, and cannot be fixed by patching · serious · verified

The plan's A3 states the pack *"deliberately excludes `belief.json`'s own `s`"* because showing the
incumbent's answer would test anchoring rather than judgement. But `s_incumbent` reconstructs exactly
from the pack plus the committed `belief.json`, which is in the repository and which also publishes
`held_p`. The withholding is real in the pack and void in practice.

This one is not patchable — you cannot un-publish a committed artifact. **The claim should be
withdrawn from the documents** and replaced with an honest statement that the incumbent is
reconstructible and the design relies on the forecaster not doing so.

### S5 — the forecaster controls its own exclusion, and incompleteness is silent · serious · reviewer-verified

`rejected` is forecaster-settable. There is no completeness check, so a truncated forecast ingests
silently. Combined with C1's finding that abstention *strengthens* the exploit, a forecaster that
emits 20 rows instead of 112 and marks the inconvenient ones rejected controls the endpoint's own
false-positive rate — which the red team measured at **39%** at one committed game and **14%** at two,
against a nominal 2.5%.

**Fix.** Require the row count to match the pack. Refuse a forecaster-set `rejected`. Floor the
committed count before a verdict is printed at all.

### S6 — the pre-registered command decides on scenarios the plan says cannot be spent · serious

Graded minor by the reviewer; I am raising it, because it is the registered command. `jointVerdict`
receives every scored position and there is no scenario filter anywhere in `beliefsScore`. So
`edgectl beliefs score -from-week 1 -to-week 8 -bar 0.10 -hold 0.06` pools all four scenarios, while
the pre-registration says the decision weights `shootout` and `efficient_offense`. I verified
`scenariosNotWagerable` (`beliefs_score.go:35`) is read at exactly one place — line 443, to print a
footer.

`blowout_loss` alone contributes ~256 of ~900 rows, and its **negative** within-game correlation
(−0.345, DEFF 0.66) *inflates* effective n for a claim that cannot be wagered. FINDINGS §16's own
lesson applies verbatim: every time an average is taken across sites, the thing being measured
disappears into it.

### S7 — ADR-002 documents a field that is never written · serious · verified

ADR-002 argues the git commit is the only external evidence a forecast predates kickoff, and states
each record carries `commit_sha`. I verified `CommitSHA` appears at exactly one line —
`internal/betlog/belief.go:50` — and is written nowhere. The attestation the ADR rests on does not
exist.

---

### Minor

- **M1 — the Murphy decomposition is printed against a Brier it does not sum to.** `BinnedBrier`
  exists, is documented as the honest way to show the residual, and is never called outside the tests.
  Worse for a realistic forecaster: `Decompose` uses **equal-width** bins, so 480 rows spread over a
  plausible 0.28–0.44 occupy **2 of 10 bins**, and reliability cannot see miscalibration finer than
  that. `TestBrierEqualsItsDecomposition` uses forecasts spanning 0.1–0.8 and never enters the regime.
- **M2 — the `blowout_loss` market reference includes the −7 atom.** `freezeReferences` derives
  `P(margin ≤ −7)`; the outcome pack uses `margin < −7`. `P(margin == −7)` is **4.26%** — the modal
  NFL margin — leaving the reference ~2pp high on ~28% of pooled rows.
- **M3 — `res.Checked++` fires before `adjudicate` returns early** for a team with no `PriorForm`, so
  the reported "N checked" overstates.
- **M4 — `scenariosNotWagerable`'s comment names `pass_heavy`; the map contains only `blowout_loss`.**

---

## What the review cleared

Recorded because negatives are the point.

- **The within-game correlations in FINDINGS reproduce.** Claimed +0.109 / +0.022 / −0.353; measured
  **+0.120 / +0.0295 / −0.345** over 2016–2025. Sound.
- **The "60 effective per week" arithmetic is correct** — for one week. The error is extrapolating it.
- **The cross-scenario clustering doubt is refuted.** Same-team-same-game pairs already share a
  `game_id`, including the −0.257 `blowout_loss`/`efficient_offense` and +0.240
  `shootout`/`efficient_offense` pairs.
- **The E2-is-underpowered doubt is refuted.** 56% power at +0.10, 91% at +0.15.
- **"Form does not exist before week 4" is correct.** The pack's guard is a season-week proxy that
  lets week 3 do wasted work, but the downstream filter is right and byes only make it more
  conservative.
- **The abstention plumbing is correct.** `Positions()` is applied consistently to estimate and
  interval; `MeanAbsDisagreement`, `OverBar` and `RealisedEdge` all exclude abstentions; tests pin it.
  The doubt is about *which rows the forecaster chooses*, not leakage.
- **`RealisedEdge`'s side-taking is correct** — direction picks the side, breakeven flips with it, all
  four cases pinned.
- **Freezing references at ingest is right and load-bearing**, as are the `pack_sha256` binding, the
  dual-clock kickoff gate, and `kickoff_instant`'s Eastern resolution — verified including London and
  Munich games.
- **Settlement is idempotent.** Nine of twelve ingest gates hold; the hobbyist called the refusals the
  best part of the project.
- **`player.py`'s quantified refusals do real work** — five of ten players never made it past the gate.
- **The pack correctly withholds the incumbent's `s`** and all derived probabilities, subject to S4.

---

## Recommended order

**Nothing here is a patch to the endpoint. The endpoint is redesigned or the experiment does not run.**

**Before week 1 — blocking.**

1. **C4** — put the instructions in the pasteable file. Minutes, and everything else is moot without it.
2. **S3** — swap the assignment, add `TestScoreReportsTheSlopeNotTheIntercept`. One line; ships today.
3. **C1** — the references. Rolling base rate, `Held_p` for the incumbent, and the versioned line-only
   logistic as a mandatory third opponent. This is the substantial work and it is the difference
   between an experiment and a formality.
4. **C2** — fix the falsifier or withdraw Test A. Decide which; do not half-fix it.
5. **S1** — either rename E2 honestly or specify the real wager. Prefer the real wager.

**Before the endpoint is re-registered.**

6. **C3** — recompute the power table with the shipped code, `team-season` clustering, the honest
   α, and the hobbyist's measured abstention rate. Commit the script. Move the decision week to
   whatever the corrected table supports, and accept that it may be later than week 8 or may require
   dropping to a larger detectable effect.
7. **S6** — filter the verdict population to the decision scenarios.
8. **S5** — completeness check, refuse forecaster-set `rejected`, floor the committed count.
9. **S2** — stop scoring the forecaster against `s_market` on scenarios where it was told not to
   compute it, and fix instruction 3.

**Housekeeping.**

10. **S4** — withdraw the anti-anchoring claim from the documents; state the truth instead.
11. **S7** — write `commit_sha` or correct ADR-002. Writing it is easy and the ADR's argument needs it.
12. **M1–M4.**

## Standing back

The previous review found that pooling destroys the thing being measured. This one found the same
disease in a new organ: **every defect above is a place where a number was compared against a
reference that was not what it claimed to be** — an in-sample base rate labelled as the base rate, an
in-sample band probability labelled as the incumbent, a payout-denominated edge labelled as ROI, an
intercept labelled as a slope, a fictional bookmaker labelled as the market, a 2.5% test labelled as
5%, a per-week effective n multiplied as if independent.

The harness was built carefully and it works. What was not built carefully is the set of things the
harness measures against. That is the encouraging reading, because references are cheap to fix and
harnesses are not.

The uncomfortable one: had this run unreviewed, the most likely outcome was **week 8 printing BOTH
PASS on a forecaster with zero football knowledge**, and this project would have believed it.

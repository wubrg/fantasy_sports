# Review of the belief-probe remediation, 2026-09-04 — findings and triage

The three "before week 1" fixes from the [2026-09-01 review](2026-09-01-belief-probe-adversarial.md)
(C1 references, C2 falsifier, S1 real E2, plus C4 prompt and S3 slope) were sent back through the
same three personas: a **red team** trying to pass the remediated endpoint cheaply, an **analytical
hobbyist** running the documented week-1 flow, and a **statistical adversary** attacking the
measurement. All three read-only, all against `wubrg/belief-probe`, all reproduced against the real
2009–2025 nflverse cache. `go test ./...` and `go vet` pass; nothing below is a broken build.

## The short version

**Two of the five fixes are clean; the two hard ones relocated their defect rather than removing it.**

- **C4 (prompt) and S3 (slope) are correct and pinned** — all three reviewers independently confirmed.
- **The zero-knowledge exploit that passed BOTH endpoints last time is genuinely dead.** The constant
  forecaster now scores negative in all 8 seasons; the line model reproduces bit-for-bit; the E2
  arithmetic is right and its units really are per-unit-staked.
- **But C1's "mandatory line opponent" is not the opponent for 5 of 8 weeks** — `auto` scores the two
  PROE scenarios against the *incumbent* (which the line model beats with no football knowledge), and
  the per-reference breakdown that was supposed to fix this is printed but never wired into the
  verdict. Red team and statistician found this independently.
- **C2's falsifier stopped missing false claims by starting to convict true ones** — "coming off a
  road win", "over their last 3 games", "the line has moved and total sits at 47.5" are all falsely
  falsified, biasing the survivor set toward vague reasoning: the exact C2 consequence, moved into the
  regex layer.
- **E2 was rebuilt into a rescaling of E1, not the independent second claim the design needs**, and
  its confidence interval is ~5× too tight for what its label promises.

The red team's one-line diagnosis is the right frame: *a number is still being compared against a
reference that is not what it claims to be* — the same disease the last two reviews named, in the code
that was written to cure it.

## Reconciling the reviewers

Two convergences and one apparent contradiction.

**Convergence 1 — the verdict still pools references.** The red team (S-A) found `auto` prefers the
incumbent over the line and the breakdown is display-only; the statistician (SE2) found `jointVerdict`
runs on `auto`-pooled points while `referenceBreakdown` scores unpooled but never feeds the decision.
Same defect from two directions. I verified it: `reference()` orders market → **incumbent → line** →
base-rate (`beliefs_score.go:485,488`), and `jointVerdict` receives the `auto` points
(`beliefs_score.go:157`).

**Convergence 2 — E2 is not independent evidence.** The red team (S-B) and statistician (minor) both
found that with `q,r` frozen once per scenario, `h = q if Y else r` makes E2's per-row outcome a
deterministic function of the same `(Y, sign(s_you−s_ref))` that drives E1. "Both must pass" no longer
buys what the pre-registration claims it does.

**Apparent contradiction — did the falsifier get fixed?** The hobbyist ran real claims through it and
saw it work: it caught false form values, correctly left "4 giveaways in three games" alone, and
handled ASCII hyphens and "away from home". The red team ran *different* claims and saw it convict true
ones. Both are right — the hobbyist's cases didn't hit the incidental-keyword collisions the red team
targeted ("road", "last N games", "line"). The parser mechanics are genuinely fixed; the **keyword
sets over-fire**. Not a contradiction — a gap in the hobbyist's sample that the red team filled.

---

## Findings

Grades are mine after reconciliation. "Relocated" marks a prior fix that moved its own defect.

### C-A — the falsifier convicts honest, correct claims · critical · verified (red team) · relocated

The C2 rewrite falsifies "only a named quantity the pack contradicts". Three keyword collisions make it
falsify **true** claims a real forecaster would write, reproduced against a faithful replica of the
shipped regexes:

1. **`reAway` contains `\broad\b` and `saysAway` overrides `saysHome`** (`falsify.go:228,284`). Any
   incidental "road" — *"KC — hosting, coming off a road win"* — flips a correct, explicit **home**
   claim to "away" and falsifies it. The prior home/away fix narrowed the bug to
   `"from home"/"not at home"` and left the commoner `road` in, with override priority.
2. **`reGames` = `\bgames?\b` + `numberNear`** (`falsify.go:226,214`) reads ordinary prose *"over their
   last 3 games the offense has clicked"* as a claim that `prior_games == 3` and convicts it against the
   pack's cumulative count (6). "Last N games" is the normal way to phrase recent form.
3. **`reSpread` claims the word `\bline\b`** (`falsify.go:223`) and `numberNear` then binds the *total*
   to it: *"the line has moved and total sits at 47.5"* checks 47.5 against the spread (±3) and convicts,
   though the total was stated correctly and no spread was stated. `getting`/`laying`/`dog` collide the
   same way.

Falsified predictions drop from the survivor score, and these fire on exactly the *concrete* claims —
the ones naming a side, a count, a line. So the survivor set is biased toward vague reasoning, which is
the C2 consequence the last review called "worse than the finding itself," now driven by the
replacement code. **This is the finding that most damages the remediation**: my #40 commit claimed to
fix the false-accusation problem and instead moved it.

### C-B — the "mandatory" line opponent is bypassed for 5 of 8 weeks, and the verdict never sees the breakdown · critical · verified (red team S-A + statistician SE2)

The pre-registered command runs with no `-vs`, so the verdict scores against `auto`. I verified the
`auto` chain is market → **incumbent → line** → base-rate (`beliefs_score.go:485` before `:488`), and
`SIncumbent` is populated from week 4 (`beliefs.go` freeze). So on the two PROE scenarios the line model
was built to defend — the only ones it covers — **the line is the scored reference only in weeks 1–3;
weeks 4–8 revert to the incumbent band.** `referenceBreakdown` reports each opponent separately but is
*printed only*; `jointVerdict` runs on the pooled `auto` points.

That matters because the incumbent (prior-form band) is **beatable by the committed line model with zero
football knowledge**: read `line_model.json`, read the pack's total/spread, emit `s_line`. The red team's
LOSO reproduction over 2018–2025, efficient_offense, weeks 4–8, clustered by game:

```
pooled 8 seasons  n=1168  gain +0.00923  CI[+0.00178, +0.01696]  PASS   (positive in 7 of 8 seasons)
```

The mercy is that at one-season scale (~145 rows) the CI does not clear zero, so it does not *reliably*
flip a single-year verdict — but it biases the pooled gain optimistically in a way the harness cannot
see, and it defeats the stated purpose of C1. I built the per-reference separation the last review asked
for **as a display and never wired it to the decision**, and worse, ordered `auto` so the mandatory line
sits *below* the beatable incumbent. This is the C1 fix relocating its own defect.

### S-A — E2 is a deterministic rescaling of E1, so "both must pass" no longer buys independence · serious · verified (red team S-B + statistician)

`BestWagerableSite` returns one `(q,r)` per scenario, frozen for every row (`conditionals.go:796`). With
`h = p.R; if p.Y { h = p.Q }` (`calib.go:600`), E2's per-row outcome uses **no independent prop data** —
it is a deterministic function of the scenario outcome `Y`, the sign of `s_you−s_ref`, and two constants.
A row wins its over-bet iff the scenario occurred, which is the same `Y`-vs-reference signal E1's paired
Brier already scores. The pre-registration's whole justification for both halves is that they "can point
in opposite directions"; after the S1 rewrite they are strongly correlated for the line/incumbent
scenarios. E2 also inherits C-B's exploit surface exactly. The independent-second-claim safeguard is
largely gone.

### S-B — E2's confidence interval is ~5× too tight for a "realised ROI on N wagers" · serious · verified by simulation (statistician SE1 + hobbyist)

`h = q|r` is the conditional *mean*, never a 0/1 prop draw. As a point estimate of expected ROI it is
unbiased (confirmed). But the bootstrap resamples these plug-in returns, whose per-wager sd is **5.2×
smaller** than the same wager settled by an actual `Bernoulli(h)` prop (0.207 vs 1.077 in the
statistician's probe). And because `q,r` are one constant per scenario, the row/game bootstrap **cannot
see the grid's own fitting error** — every replicate uses identical constants. So E2's CI is conditional
on the frozen site being exactly the true prop probabilities, with zero propagation of estimation error,
and the `elo > 0` verdict declares significance on far thinner evidence than "realised ROI on N wagers"
implies. This is the mechanism behind the hobbyist's concrete "`E2 … PASS on 2 wagers`" with a tight CI.

### S-C — a raw `+NaN` reaches the terminal · serious (trivial fix) · verified (hobbyist)

`flaggedReport` computes `calib.PairedBrierGain` on the flagged group and prints it with a bare
`%+.5f` and no `math.IsNaN` guard (`beliefs_score.go:~365`). On a 1-row flagged group the gain is NaN,
so the CLI prints `flagged n=1 Brier 0.3025 gain +NaN` — while every other section substitutes an em
dash for an undefined ratio. The spec asks a forecaster to flag "around ten" candidates, and a flagged
group of one is entirely plausible early season. A one-line fix, but user-facing.

### S-D — Test A is still structurally empty for a compliant forecaster, and the spec steers to vagueness · serious · verified (red team S-D)

The operative prompt (shipped in the pasteable file, inside the markers) still says *"Do not restate a
pack number as a claim … getting it wrong is the one way a form or market claim can hurt you"*
(`belief-probe.md:191`). A forecaster that obeys emits only un-numbered `narrative`/`personnel` claims →
`Checked = 0`. Combined with C-A, the spec **actively steers forecasters to vagueness and then convicts
the few who are concrete**. Separately, `injury`/`usage` remain deferred (`falsify.go:65`), so the two
types the spec names as the historical failure mode are never checked — a fabricated `injury: KC —
starting QB is OUT` passes uncounted. The last remediation reframed this honestly but did not close it.

### S-E — the population is still forecaster-steerable, which is what makes C-B's edge worth chasing · serious · known (deferred #43), reprioritized · verified (red team S-C + hobbyist)

`Rejected` is copied straight from the forecaster's file (`beliefs.go` ingest) and the falsifier can only
*add* a rejection, never clear a self-declared one. There is no check that the row count matches the
pack's scenario×game cross-product — the hobbyist dropped 8 of 98 rows and got `ready 90` with no
warning. So a forecaster can submit efficient_offense-only, weeks 4–8, and steer the scored population
onto exactly the rows where C-B's incumbent reference is beatable. This was already logged as S5/S6 and
routed to task #43; both reviewers show it is the **enabler** that turns C-B's modest directional edge
into a cherry-pickable one, which argues for doing it in the same pass as C-B rather than after.

---

### Minor

- **E2's label still overstates.** "realised ROI … per unit STAKED (comparable to §16's +7%..+18%)" and
  "a real prop wager" (`beliefs_score.go:196`, `belief-probe.md:230`) — no real prop line or outcome is
  touched; `h` is the model-implied rate. It is a model-internal expected ROI, and §16's bound is on
  real prop outcomes, so the invited comparison is still not apples-to-apples. (Ties to S-A/S-B.)
- **The slope caption cannot describe a negative slope.** "1.0 is honest; below 1 means over-confident"
  prints unconditionally; a *negative* slope (belief anti-correlated with outcome) is materially
  different and more alarming, and the caption reads the same. (`beliefs_score.go:115`.)
- **`BestWagerableSite`'s max-separation carries a winner's curse** — selecting the max-`(q−r)` validated
  cell biases `q−r` upward, which inflates the wager count and narrows the CI further (compounds S-B).
- **Two different "over bar" numbers share one label** — `referenceBreakdown` prints `g.OverBar`
  (`|p−ref|>bar`) while the E2 line prints `OverBarCount` (vig-cleared wagers). Same words, different
  populations.
- **`scenariosNotWagerable`'s comment still names `pass_heavy`; the map holds only `blowout_loss`** (M4,
  carried from the last review) — routed to #44.
- **Rehearsal friction:** the dual-clock kickoff gate (correctly) makes every past-season game
  un-ingestable through the CLI, so a user cannot rehearse the pipeline on last season's results without
  a kickoff-shifted pack. Worth one line in the docs if rehearsal is ever a supported use.
- **Deferred, confirmed still present:** the one-sided 2.5% α and the game-level (not team-season)
  clustering both persist and the E2/per-reference CIs inherit them → task #42.

---

## What the review cleared

Recorded because negatives are the point, and this list is long — most of the remediation is sound.

- **C4 works** — the pasteable prompt is a real 192-line single-source lift with schema, abstention,
  scenarios, and a hard `SystemExit` if the markers vanish. All three reviewers confirmed.
- **S3 is correct and pinned** — `Report.Slope` is the slope, `SlopeSE` is `Var(b)`, and the new test
  asserts on the field the CLI prints. Verified on calibrated and over-confident synthetic input.
- **The zero-knowledge exploit is dead** — the constant-0.4343 forecaster now scores negative in all 8
  seasons; `auto` never uses the base rate as the reference for a scored scenario; the incumbent freezes
  `HeldP`, removing the low bias.
- **The line model is sound and untampered** — refit reproduces `line_model.json` bit-for-bit (β, n,
  base, held-LL identical; games.csv sha matches), the Newton solver hits the MLE (‖grad‖ ~1e-13), LOSO
  logloss 0.595/0.618 beats the null 0.643/0.633 with **no leakage**, training labels use the correct
  observation field, home/away sign is correct, and Go `Predict` reproduces Python to 6 decimals.
- **E2's arithmetic is right** — `(h−be)/be` with `be = P_book·(1+hold)` is genuinely per-unit-staked
  (re-derived), the vig gate reduces algebraically to the plan's `s_you−s_ref > P_book·hold/(q−r)`, and
  all four side/outcome cases are pinned. E2 is **not** fooled by a null forecaster against an honest
  reference. The prior payout-vs-stake mislabel is fixed.
- **The line reference itself is not a new exploit surface** — reproducing `s_line` and "sitting just
  off it" does not beat it on E1, and the knowable-`q,r` manufacture attack is −EV against it. The
  exploitable reference is the *incumbent* (C-B), not the line.
- **The falsifier's parser fixes are real** — spaced hyphen / double-hyphen / colon separators parse,
  `St-Brown` keeps its intra-word hyphen, and requiring every stated number on a matched quantity to
  agree removes the one-true-number-immunises hole. The residual is over-firing (C-A), not the parser.
- **`auto` prefers `line` over `base-rate`** — which does neutralize the weeks-1–3 stale-base-rate
  exploit; the gap is only that it prefers the *incumbent* over the line (C-B).
- All documented `make` targets and CLI flags run and match their descriptions; ingest correctly refuses
  out-of-range beliefs, unknown JSON fields, and (via the dual-clock gate) late forecasts.

---

## Recommended order

The two hard fixes need finishing, not patching, and two of them are cheap. This slots into the #42–44
plan rather than replacing it.

**Cheap and unambiguous — do first.**

1. **S-C** — guard the `flaggedReport` NaN (em dash like every other section). One line.
2. **C-A** — narrow the falsifier keyword sets: drop bare `road` from `reAway` (keep the explicit away
   phrases) or require it to co-occur with a side word; stop `reGames` binding "last N games" to
   `prior_games` (require a possessive/total phrasing, or drop the games check); stop `reSpread` claiming
   `line` when the only number is a total. Re-run the review's four false-conviction cases as tests.

**The substance — finish C1 and E2 together (these are the headline).**

3. **C-B** — wire the decision to the per-reference breakdown: score E1 per (scenario × reference-type)
   in the *verdict*, and for the two PROE scenarios require beating the **line**, not the incumbent —
   either reorder `auto` to prefer `line` over `incumbent` on those scenarios, or make the verdict take
   the *max* over available references. This is the fix the last review actually asked for; I shipped its
   display half only.
4. **S-A + S-B** — decide what E2 is. If it stays a `(q,r)`-plug-in, (a) stop calling it a "realised"
   wager track record and price the CI honestly — propagate `q,r` uncertainty (bootstrap the grid, or a
   Bayesian interval on the site) and/or settle each wager as a `Bernoulli(h)` draw so the interval
   reflects real prop variance; and (b) confront that it is not independent of E1 for the line/incumbent
   scenarios — either accept E2 as a same-signal robustness check and say so, or give it genuinely
   independent information. This is a design decision, not a patch.

**Fold into the already-planned tasks.**

5. **S-E** → **#43** (was S5/S6): completeness check, refuse forecaster-set `rejected`, floor the
   committed count — do it *with* C-B, since it is C-B's enabler.
6. **S-D** — resolve the Test A double-bind: either loosen "do not restate a pack number" so a compliant
   forecaster can make checkable claims, or accept Test A measures little and say so; wire injury/usage
   via the pack when it carries those facts.
7. **Minors** (E2 label, slope caption, over-bar naming, winner's curse, `scenariosNotWagerable`
   comment, rehearsal doc) → **#44**. The α and clustering remain **#42**.

## Standing back

The last review found the endpoint compared numbers against references that were not what they claimed.
This one finds that **the two fixes aimed at that disease reproduced it one layer in**: C1 added the
right opponent and then scored against the wrong one; C2 fixed the parser and then let the keyword sets
convict the honest. The encouraging read is that both are now small, well-located changes — a reorder and
a display-to-decision wiring for C-B, four keyword narrowings for C-A — and everything underneath them
(the line fit, the E2 arithmetic, the slope, the prompt) is verified sound. The uncomfortable read is
that "add a per-reference table" and "rewrite the falsifier" *felt* like closing the findings while
leaving the decision that actually matters — which reference the verdict beats, and which claims it
drops — pointing the same way as before.

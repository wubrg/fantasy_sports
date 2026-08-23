# Adversarial review, 2026-08-23 — findings and triage

Two independent reviewers, read-only, given the documentation and the code: one in the persona of
the analytical hobbyist the corpus describes, one attacking the methods in
[`../../model/FINDINGS.md`](../../model/FINDINGS.md). Both were seeded with the author's own doubts
and told to find their own.

Every finding below was **re-verified by running it** before being recorded here. Where a reviewer's
number differs from the re-run, the re-run is what appears.

## The short version

The statistical reviewer rebuilt the pipeline independently and reproduced **all sixteen published
verdicts exactly**, along with §9's `t = 17.23` and §4's ICC figures to the digit. The arithmetic
is right and the reporting is honest.

The problem is not that the survivors are noise — a permutation test settles that decisively. The
problem is that **the gate answers "is this effect real?" and has been read as "is this
priceable?"** Under that misreading, one of two shipped receiving scenarios is 90% restated volume,
and both are priced from a `q`,`r` pair pooled over the exact variable used to form `s`, at a cost
above the vig.

## The unifying diagnosis

Three of the most serious findings are the same defect wearing different clothes.

**`q` and `r` are pooled over everything except the two grid axes. Any information the operator
brings that is not one of those axes creates a mismatch.**

- Bring the **posted total** (to form `s`): `q`,`r` were averaged across posted totals → up to
  −9.5pp of miscalibration, monotone, against a 2.38pp cushion.
- Bring the **player** (whose line the book set): `q`,`r` were averaged across players → a
  player-specific line judged against a cohort median, so every good player reads as a fade.
- Bring **PROE** (to form `s` for `pass_heavy`): same shape, 1.4pp.

Seeing these as one defect matters, because it says the fix direction is shared: either condition
`q`,`r` on what the operator brings, or recentre them on the specific case. Fixing them one at a
time will keep producing the next instance.

---

## Findings

Severity: **critical** = a priced number is wrong; **serious** = a claim is unsupported or a
capability is unusable; **minor** = wrong but harmless.

### C1 — `q`,`r` pooled over the variable used to form `s` · critical · verified

The decomposition is `P(hit) = q·s + r·(1−s)`. `s` is derived from the posted total; `q` and `r`
were fitted pooled across posted totals. Measured on the 6–8 target, flat-trend cell:

| posted total | n | s | model P | actual P | error |
|---|---|---|---|---|---|
| 0–42 | 656 | 0.238 | 0.393 | 0.384 | +0.86pp |
| 42–45 | 738 | 0.320 | 0.402 | 0.375 | +2.63pp |
| 45–48 | 705 | 0.369 | 0.407 | 0.396 | +1.12pp |
| 48–51 | 474 | 0.460 | 0.417 | 0.424 | −0.72pp |
| **51+** | 312 | 0.561 | 0.428 | 0.522 | **−9.46pp** |

Monotone, and worst where the market is dearest. The vig cushion at −110 is 2.38pp.

**Neither gate can see this.** Both test *sign*; neither tests calibration.

**Fix is affordable.** `total_line` is already cached in `games.csv` and read by nothing outside
`fit_residuals.py`. Measured cost of adding it as an axis:

| line bands | cells published | survive |
|---|---|---|
| 1 (today) | 34 | 85% |
| **2** | **61** | 76% |
| 3 | 84 | 70% |

At two bands the grid publishes *more* cells than it does today.

### C2 — `pass_heavy` is 90% a volume identity · critical · verified

Holding **realized** targets fixed:

| scenario | β before | β after | t after | explained by volume |
|---|---|---|---|---|
| `pass_heavy` | 6.079 (t 14.25) | 0.611 | **1.97** | **90%** |
| `shootout` | 7.448 (t 17.58) | 5.354 | 16.50 | 28% |

`pass_heavy` measures the inadequacy of this project's own target projection (prior share × prior
3-game team pool, no market input), not a market inefficiency.

**The defence in §4 was the identity's fingerprint, not a refutation.** "It holds within
projected-target bands and *widens* with volume" — the realized-target gap widens across the same
bands (+0.48, +0.75, +1.17, +1.43), because a higher-share player converts extra team attempts into
proportionally more targets. Share × extra attempts *is* the widening. A duplicate of the
*projection* would collapse; a consequence of the projection's *inadequacy* widens exactly so.

**This inverts the author's stated confidence.** `pass_heavy` was trusted enough to override a gate
for; `shootout` is the cleaner effect and deserved that confidence instead.

**It is not worthless — it is filed under the wrong outcome.** A signal that predicts realized
targets above projection is a signal about *opportunity*. That is task #7, targets as an outcome.

> **Measured 2026-08-23. C1 and C3 were correctly triaged as one defect, and this review named
> the weaker half as the cure.** C3 is the larger error by a factor of four (8.01pp against C1's
> 3.12pp, on a 2.38pp vig cushion) and the posted-total axis does not touch it. Normalizing to the
> player's own prior mean fixes C3 *and* takes C1 from 3.12pp to 2.68pp with no axis added.
> The fix is blocked, not by its own measurement, but because it re-cuts the grid into more cells
> and `qualifies()` is not scale-invariant — `shootout` for receiving yards then fails a gate it
> passes today, on 20/24 out-of-sample cells rather than 15/15, with its direction *improving*
> from 16/16 to 29/29. Per-cell gating is now a blocker for this item rather than an improvement.
> See [FINDINGS.md §11](../../model/FINDINGS.md) and `make calibration`.

### C3 — the grid judges a player-specific line against a cohort median · critical · verified

`scenario` takes no player identity. `q` is `P(any player in this band clears L)`; the book sets `L`
near *this* player's median. Swept at −115, 7 projected targets, flat trend:

| line | verdict |
|---|---|
| 34.5, 39.5 | MARKET-ALONE |
| 44.5 and above | BEYOND-YOUR-READ (even at s = 100%) |

Every standard −110/−115 over is structurally unreachable, and the only approvals occur where the
book's line sits *below* the cohort median — which in the real world means the book knows something.
The tool reads the trap population as free money.

Sharpest form of the charge: `CheckDefinition` refuses when `s` and `q`,`r` measure a different
*event*, and permits them measuring a different *population*.

### S1 — two unexplained constants determine the verdicts · serious · reviewer-verified

`MIN_CELL = 100` and `OOS_SPLIT = 2021` are justified nowhere. Sweeping them:

- `shootout`/receiving — the flagship clean case — **fails at 2 of 5 plausible splits**.
- `receptions`/`pass_heavy` **passes outright at 2018, 2019 and 2020**, failing only at the shipped
  2021. The `accepted_failure` override therefore exists because of a knob setting, not a data
  shortage — so "revisit when the held-out half has more seasons" misdiagnoses the cause.
- `rushing`/`blowout_loss` and `rushing`/`efficient_offense` both flip to PASS at `MIN_CELL` 150.

"The rule was written before this scenario existed" loses force when the rule's *constants* were
set afterwards.

### S2 — §6's self-service check has gone stale · serious · reviewer-verified

> **Confirmed and fixed 2026-08-23.** Re-run across the current grid the location choice moves
> **33 site verdicts, all permissive**. The shipped grid uses the median everywhere. The reason the
> mean was adopted has also gone: receptions now have 0 of 30 cells at a median delta of exactly
> zero, against 12 of 16 on the raw-count grid. `make recheck`; FINDINGS §6.


The median→mean switch was checked against settled verdicts and moved none — true when written.
Re-run across the whole grid now, **three verdicts flip FAIL→PASS, all permissive**:
`receiving`/`efficient_offense`, `rushing`/`blowout_loss`, `rushing`/`efficient_offense`. Both
rushing pairings were added *after* the check.

Consequence: §7's "`blowout_loss` is the closest miss in the grid" is a fact about the *estimator*,
not the effect. It passes under the mean.

### S3 — §8's statistic contains its own outcome · serious · reviewer-verified

> **Confirmed; the finding survives re-derivation, smaller.** On `|train-only effect|` pooled over
> all sixteen pairings: real 2.37×, null 1.04×. Not the 5× reported, and not present in every
> pairing. Per-cell gating (§12) was decided on the gate's scale-dependence and its own permutation
> null, not on this statistic. `make recheck`; FINDINGS §8.


The "median |fit effect|" is a full-sample delta while agreement is train-vs-test, so the test half
is inside the predictor. Under a **pure null** this manufactures a 3.0× ratio by itself; the
reported 5× is therefore weak evidence.

**The finding survives a clean statistic and is stronger**: on train-only |effect|, real data gives
5.0 vs 1.0 while the null gives 1.0 vs 1.0 — no pattern at all. Since §8 is the argument for
per-cell gating (task #9), it must be re-derived before anyone acts on it.

### S4 — §8 is not reproducible · serious

> **Fixed 2026-08-23.** `_compare_oos` had been raising `TypeError` since the grid went to four
> outcomes. Stated per-outcome now, and the claim survives stronger: 16 of 16 pairings pass the
> magnitude-aware criterion, where §8 reported 4 of 4.


No script in `analysis/` computes its table, and the cell count could not be reproduced (192 vs the
reported 141). `FINDINGS.md` promises every claim comes with the script that produced it.

### S5 — `efficient_offense` is unreachable · serious · verified

```
$ edgectl scenario -name efficient_offense -basis success_rate -threshold 0.46 ...
edgectl: "efficient_offense" is fitted as success_rate > 0.5, but you asked for
a threshold of 0.5. ... Use -threshold 0.5
```

The same number twice, and the remedy fails forever. The fitted value is **0.46**;
`Definition.String()` formats with `%.1f`. A scenario the README lists as usable is reachable only
by reading the JSON artifact.

### S6 — `scenario` cannot price an under · serious · verified

> **Fixed 2026-08-23.** `-side over|under`. The grid fits one direction and reads the other off the
> same cell, mirroring the interval rather than recomputing it.


There is no `-side`. `hitrate` has one. Given C3 pushes every estimate below the line, unders are
where this grid's implied value actually sits, and it cannot express one.

### S7 — the parlay header contradicts its own table · serious · verified

`PER-BOOK ALLOCATION` shows `$12.50`; the header beneath announces `4 shot(s) at $25.00`; every
return is computed on $12.50. The header is the number a bettor types into a slip.

### S8 — `make demo` sells a verdict its own grid contradicts · serious · verified

The ladder step passes hand-written `q=0.72`; the fitted grid gives **0.55** at the same line.
Seventeen points more optimistic, producing a `+5.5636 EV` buy signal that does not survive
substituting the tool's own measurement — in a repo whose founding grievance is a confident number
from a wrong formula.

### M1–M4 — minor

> **All four addressed 2026-08-23.** M1: the capability map is rewritten against the current grid.
> M2: `THIN`/`MEASURED` was one threshold, so 13 effective observations and 400 printed the same
> word at ±28% and ±5%; there is a `SPARSE` band between them now and every estimate prints its
> relative error. M3 and M4's wording fix landed in the first commit.
>
> **M4's second half was not minor and is now §14.** `shootout` measured on the opponent's points
> alone — the half of the total the player cannot cause — still separates (+0.086 median delta,
> 7 sites priceable), so it does not collapse the way `pass_heavy` did. But the shipped definition
> reads about **1.8× the exogenous effect**. Recorded rather than acted on: removing the
> circularity would cut receiving yards from 13 priceable sites to 7.


- `capability-map.md` is a version behind and contradicts the README on what is priceable.
- `THIN` fires at 7 effective observations; 13 still prints `MEASURED`.
- `ledger expiring` prints a 41-character lot id into a 20-character column.
- §7 says "a high **posted** total"; `shootout` is defined on the **realized** total. The reasoning
  survives, the wording does not. Related and unmentioned: a receiver's own yards feed his team's
  points, so `shootout` is mildly self-referential (own-team >27 pts gives +7.52 yards, opponent
  >27 only +3.44).

---

## What the review cleared

Recorded because negative results count here too, and two of these were the author's own worries.

- **Multiple comparisons: refuted.** 1,600 null replicates with permuted labels, **zero passes**.
  Expected spurious passes across sixteen pairings: **< 0.08**. §4 *understates* the gate — "zero
  failures in 15" is ~2⁻¹⁵, not the p ≈ 0.1 claimed.
- **Player clustering is correct and conservative** in every test (design effect 1.132 against
  1.001 team-week). The most load-bearing methodological decision in the project, and it is right.
- **The bootstrap of a median is well-behaved here**, and errs conservative.
- **Prior-information discipline holds** everywhere it could be checked — no leakage found.
- **The override is principled machinery**, not an escape hatch: `rule_says` is stored separately,
  the failing cell is named at the point of pricing, and a test prevents decay. Its gap is
  governance (no budget, no aggregate view), not honesty.
- **`effective_n` on yards ICC applied to a hit-rate interval** is a genuine statistic mismatch,
  measured immaterial and in the conservative direction.

---

## Recommended order

Grouped by what unblocks what, not by severity alone.

**1. Stop the bleeding (hours, no analysis).** S5, S7, M1, M3, M4 — the `%.1f` rounding, the parlay
stake, the stale capability map, the column width, the "posted" wording. All are the tool or its
docs lying about themselves, all are cheap, and leaving them in undermines the credibility of every
number beside them.

**2. Settle `pass_heavy` (C2).** The measurement is already done; this is a decision, not a project.
It determines what remains to be calibrated, so it comes before the calibration work. The likely
answer is to withdraw it as a production scenario and re-file it against targets-as-an-outcome
(task #7), where a signal about opportunity belongs.

**3. Fix the calibration (C1), and take C3 with it.** They are one defect. Add the posted total as a
conditioning axis at two bands — measured to publish 61 cells against today's 34 — and decide at the
same time how the player's own baseline enters, since a cohort `q` against a player-specific line is
the same mismatch. This is the largest piece of work here and the one that decides whether any
priced number can be trusted.

**4. Nail down the constants (S1).** Sweep `MIN_CELL` and `OOS_SPLIT`, justify them, and **report
verdict stability alongside every verdict**. A verdict that holds at one split and fails at another
should say so. Do this after 3, since re-fitting will move things anyway.

**5. Correct the record (S2, S3, S4).** Re-run §6's check now that rushing and `efficient_offense`
exist; re-derive §8 on the train-only statistic; make §8 reproducible or delete its numbers. This is
bookkeeping on claims, and it gates task #9 — do not decide per-cell gating on a contaminated
statistic.

**6. Capability gaps (S6 and the hobbyist's list).** `-side under` is cheap and doubles the
addressable market. Then `-trend`/`-targets` extracted from the cache — currently typed from memory,
and a wrong cell fails silently. Then prop prices on the board, then a candidate screen.

## Standing back

The hobbyist's verdict was that the board, bonus and ledger half is worth keeping and better than a
spreadsheet, and that the prop half would not survive to week 4. The statistical reviewer's was that
the reporting is honest, the gate is a much stronger filter than its author credits, and the
corpus's claims are being genuinely tested and mostly rejected — which is the hard part, done well.

Both then found the same shape of defect from opposite directions. That agreement is the most
useful thing in this document.

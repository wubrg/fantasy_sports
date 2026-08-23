# Why this tool refuses things

Most of `edge` is arithmetic anyone could write. What took the work was deciding when **not** to
answer, and that is what this document is about. Every rule below exists because something went
wrong without it, and each one names the measurement in [`../model/FINDINGS.md`](../model/FINDINGS.md)
rather than restating numbers that will drift.

---

## The two invariants

**1. The CLI computes; the model never does.** The source frameworks contain four arithmetic errors
and one process failure. The most instructive is a "No Sweat" worked example reporting a **$127.55
loss as an $872.45 profit** — the losing cash stake is simply never subtracted, and both branches
therefore look profitable. It survived review because precise arithmetic on the wrong formula reads
as far more authoritative than obvious nonsense.

So the arithmetic lives in tested Go, and each source error is pinned by a test asserting the
corrected figure *and* that the printed one is not reproduced.

**2. Missing data fails loudly.** No function returns a zero for an input it could not interpret. A
zeroed EV reads as "no edge"; the truth is "no data", and those must not look alike. `edgectl market`
refuses to report vig from one side of a market, because vig cannot be measured from one side.

---

## Why a scenario, and not another axis

The grid conditions on projected opportunity and role trend. Adding a third conditioning axis
multiplies the cells and thins every one of them; adding a **scenario** costs nothing, because a
scenario gets its own set of cells rather than subdividing an existing one.

That is not a stylistic preference. A binary fourth axis was measured to keep 30 of 34 cells — and
the four it killed were the high-volume, rising-role cells, including the "usage vacuum" cell that
had just been published at real cost. See §4 and §7.

## Why the question is "what would you have to believe?"

A hit rate asks whether a player's *baseline* rate beats the price. At a season's sample size it
essentially never does: clearing a −110 hurdle at 95% confidence needs 13 of 17. Fitting a
distribution and simulating does not rescue it — measured, it narrows the interval about 10% at a
standard line while losing calibration in the tail, where nominal 95% coverage falls to 86% even
when the fitted family is exactly right (§1).

But a narrative wager was never a claim about baseline. It claims *this week differs*. So the
question changes from "is my edge real?", which is unanswerable, to "what would I have to believe?",
which is a single number, is arguable, and can be checked against what the game line already implies.

## Why a verdict is recorded against a stated rule

`SCENARIO_STATUS` used to carry three numbers as prose describing an analysis that existed only as
comments. Nothing recomputed them, so they rode into every artifact unverified while the data
beneath them changed (§4).

Now the evidence is measured on every fit and **the fit fails if the recorded verdict and the rule
disagree**. The verdict remains a human judgement — two scenarios is not enough to calibrate a
threshold on — but the rule it is held to is written down.

## Why an override is by name, and never by softening the rule

`pass_heavy` fails on a single out-of-sample cell whose held-out delta is −0.5 yards on 65 games.
The obvious fix was a magnitude-aware test that stops counting a near-zero flip as disagreement.

It was tried, tested against the two verdicts already settled, and **rejected**: it makes every
scenario pass, including both that are gated. A gate that cannot fail is not a gate (§4).

The cost of softening a rule is paid by every scenario measured afterwards. An exception costs only
the scenario it names. So `accepted_failure` records which cell failed, what was measured, why it
was accepted and by whom — and it is loud: the fit prints it, the artifact carries it, and
`edgectl` prints it before the numbers and says whether *your* wager falls inside the failing cell.

## Why the instrument gets checked before the result

Twice, a result that looked like an absent effect was a blind instrument.

**Receptions.** The first fit found `shootout` consistent in 4 of 16 cells, against 16/16 for
receiving yards *on identical games*. The cause was measuring location with a median: receptions
run 0–21 with a cell median of 2 to 5, so **twelve of sixteen cells had a median delta of exactly
zero** while every one of their means was positive (§6).

**Tempo.** A signal was rejected for having no effect. It did have no effect — but the metric was
the shotgun rate, which correlates **+0.064** with plays actually run. It was measuring formation,
not pace. Real pace matters 26× more and fails for a different reason (§9).

The lesson both times: a null result deserves the same suspicion as a positive one.

## Why changes to method are tested against settled verdicts first

Every methodological change here was checked by applying it to the cases whose answers were already
fixed, **before** applying it to the case it would change. The magnitude-aware test was run on
`shootout` and `blowout_loss` first, and that ordering is the only reason its failure was visible.
The mean-for-discrete switch was checked the same way and moved no settled verdict.

Run on the case it would benefit, either change would have looked like a principled refinement that
happened to admit the thing being worked on.

## What the grid still cannot do, stated plainly

- **It cannot find candidates.** You bring the player, the price and the belief.
- **It cannot obtain a price.** There is no network client in this module and none intended.
- **It has no projections.** The operative template's Filter 1 requires a consensus of independent
  projections, and nothing here can produce one.
- **`p_true` by simulation does not exist for any stat**, so a prop with only a mean projection
  must be dropped — which today is most of them.
- **Two scenarios need you to supply their probability.** Books do not price a team's pass rate over
  expected, so there is no line to derive it from. The judgement the tool exists to reduce is, for
  those, relocated rather than removed.

## What the corpus got wrong, and what it got right

Wrong: the arithmetic in three places; no de-vigging anywhere in its tables; calibration never
addressed; and its **strongest claim measured backwards** — the "usage vacuum" is worth +15
percentage points in the text and is *negative* on the receiver it describes (§10).

Right, and worth keeping: variance and portfolio construction; tracking EV rather than a win/loss
record; treating hold as the determinant of conversion efficiency; distinguishing a theoretical
optimum from a behaviourally sustainable one; and the observation that a gap has two possible
causes — the line is stale, or *your projection* is. A large gap raises the probability of the trap,
not the edge.

---

Further reading: [`capability-map.md`](capability-map.md) for what is implemented against what the
frameworks ask, [`../model/FINDINGS.md`](../model/FINDINGS.md) for every measured claim with the
script that produced it, and [`frameworks/`](frameworks/) for the source material with its errors
annotated inline.

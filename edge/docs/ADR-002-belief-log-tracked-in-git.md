# ADR-002: The belief log and its packs are tracked in git

**Status:** Accepted
**Date:** 2026-08-24
**Deciders:** wubrg

---

## Context

The belief probe tests one claim: that a read on a game — from a prompt, or from a person — beats
the base rate by enough to make a prop wager +EV. The bar is `+0.03 to +0.16` on `s`
([FINDINGS §16](../model/FINDINGS.md)).

**The test is forward-only.** A model has already seen every historical NFL season, so asking one
whether a played game was likely to be a shootout is not a forecast and no prompting makes it one.
The measurement therefore rests entirely on one property:

> the forecast existed, in the form scored, **before kickoff**.

`edgectl beliefs ingest` checks two clocks — the `generated_at` the forecaster wrote, and the wall
clock at ingest — and refuses the whole file if either is late. That closes the honest failures: a
file written late, or written on time and ingested days after.

**It closes none of the dishonest ones.** Nothing in the process stops a forecast being written
after the games and stamped with an earlier time. The tool cannot tell the difference, and any
claim that it can would be false.

This matters more here than it would elsewhere. This project's stated position is that an
unfalsifiable judgement is worth less than a measured one — it withdrew `pass_heavy`, rejected six
belief signals, and recorded the negative results. A calibration record that quietly depends on the
author's honesty, without saying so, would be exactly the thing it criticises.

## Decision

**`edge/beliefs/` is tracked in git**, and the commit is the evidence.

That covers three kinds of file:

| path | what it is |
|---|---|
| `beliefs/log.jsonl` | the append-only prediction stream |
| `beliefs/<season>/week<NN>.input.json` and `.prompt.md` | the facts a forecaster was shown |
| `beliefs/<season>/week<NN>.outcomes.json` | what happened, and how it was determined |

The argument is [ADR-001](./ADR-001-line-board-tracked-in-git.md)'s, and it is stronger here. There,
a commit timestamp is evidence of *when a price was observed*, which matters because prices move.
Here a commit **pushed to a remote before kickoff** is the only externally checkable evidence that a
forecast predates the game it forecasts. Without it there is no test, only an assertion.

The record does **not** store the commit that carries it — it cannot, because that commit does not
exist until after the record is written and the file is committed. The attestation is the git history
of `beliefs/` itself: `git log --follow beliefs/<week>.jsonl` recovers when each line was committed and
pushed, and that commit's timestamp is compared against `generated_at` and kickoff. The evidence lives
in the tree, not in a self-referential field.

### And it is attestation, not proof

A commit pushed before kickoff is good evidence. A commit made afterwards, or never pushed, is
none — git timestamps are writable, and a local repository proves nothing about when anything
happened.

**This is stated in the package documentation, in the specification, and here**, rather than left
for a reader to work out. The honest description of the guarantee is:

> The tool refuses forecasts it can see are late. It cannot detect a backdated one. The evidence
> that a forecast predates its game is a push to a remote before kickoff, and nothing weaker.

## Consequences

**Good.**

- The measurement has external evidence rather than resting on self-report.
- The packs are pinned, so "which facts did it see" is answerable months later — reinforced by the
  `pack_sha256` on every record, since a path is mutable and content is not.
- The outcome packs carry the sha of the cache files they were built from. nflverse restates
  play-by-play; a re-run producing a different answer shows up as a diff rather than being absorbed.

**Costs, accepted.**

- **Volume.** A week is ~112 predictions and three files. A season is roughly 2,000 log lines and
  54 packs. That is smaller than `moneylines/`, which ADR-001 already accepted at ~6,000 lines.
- **The log is append-only and must never be rewritten.** `git rebase` over it would be exactly the
  hindsight the format exists to prevent. Correcting a mis-settled belief means voiding it and
  recording a new one, which the loader already enforces by refusing a second settlement.
- **A forecast must be committed and pushed before kickoff to count as evidence.** That is a
  discipline on the operator, not something the tool can impose.

**Rejected alternatives.**

- *Gitignore it like `model/data/raw/`.* That cache is regenerable; this is not. A lost belief log
  is a lost season of measurement.
- *Timestamp with an external service.* Genuine cryptographic timestamping would be stronger than a
  push, and is disproportionate for a single-operator experiment whose result is a decision about
  whether to keep going.
- *Say nothing about the limit.* The cheapest option and the one this project exists not to take.

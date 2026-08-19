# draftroom

Auction draft companion for the Hit or Miss league. Two jobs: keeper
accounting before the draft, and a live board during it.

Draft is **Thu 2026-09-03, 8:00 PM** — 12 teams, $200, 14 roster spots,
half-PPR, up to 2 keepers.

---

## Setup

Proprietary source data lives in a **separate private repo** — this one is
public and holds no vendor data. See `../../internal/draft/data/README.md`
for why.

```sh
cd league_home/app
make install
```

That bakes the config and data directory paths into the binary, so it works
from any directory with **no environment variables**. Make sure
`$(go env GOPATH)/bin` is on your `PATH`. The CSVs are still read at
runtime, so editing `leans/mine.yaml` takes effect immediately — no rebuild.

Re-run `make install` if you move either repo.

Working from the checkout instead? Every command has a `make` shortcut and
needs no setup at all.

| Variable | What it points at | Default |
|---|---|---|
| `DRAFTROOM_DATA_DIR` | private data repo | `../fantasy_sports_data` from the repo root |
| `DRAFTROOM_CONFIG_DIR` | rulings/aliases/leans | `league_home/app/internal/draft/data` |
| `DRAFTROOM_OWNER_ID` | your Sleeper owner ID | none (board shows a full $200) |
| `LEAGUE_ID` | Sleeper league | Hit or Miss |

---

## Commands

Everything has a `make` shortcut; the binary takes the same flags.

### `make keepers`

Keeper reconciliation, true per-team budgets, and next season's keeper
prices for every rostered player.

Sleeper does **not** enforce the league's escalating keeper ladder, so the
amount it records is only as right as whoever typed it in. This recomputes
every keeper from `leagues/hit_or_miss/draft.md` and reports disagreements.
Currently **67/67 (100%)** across usable seasons, with two LM rulings
applied.

Use it to decide which two players to keep, and to see what every rival
brings to the auction — Sleeper shows all twelve teams a flat $200, which
is wrong and would corrupt any "who can outbid me" judgment.

### `draftroom keepers -share`

Keeper prices for the league, without your valuations.

```sh
draftroom keepers -share
```

`make keepers` cannot be sent anywhere. It carries what a keeper would cost
to win back in the auction, what he is worth on median projections, which one
to keep, and which past seasons the tool distrusts — the model, in other
words. Handing that to eleven opponents gives away the reasoning the board
exists to have.

`-share` prints only the half they are owed and cannot work out themselves:
each owner's eligible players and what the league's rules charge for each,
cheapest first. Sleeper shows everyone a flat budget and does not apply the
escalating ladder, so without this nobody knows what their own keepers cost
until the auction.

About 6 kB for a twelve-team league. Each team's block is self-contained, so
it splits cleanly across chat messages if you are pasting it somewhere with a
length limit.

### `make board`

The draft board. Re-solves prices against the pool that actually remains.

```sh
make board                          # full $200, default BEER+ curve
make board ME=243501760939814912    # your keepers deducted
make board BASELINE=vols LIMIT=25
```

```
BOARD — beerplus baseline, $2055 over 153 slots

$130 left  12 slots  max bid $119
still starting: 1 QB, 1 RB, 2 WR, 1 TE

must-haves: Ashton Jeanty $48, Kenneth Walker $40 = $88,
            leaving $42 for 10 slots (~$4 each)

>> BARBELL: 2 tier-one QB left — take one now or wait out the flat middle

PLAYER               POS  COST  VALUE  EDGE  MY MAX  PS%  FLAGS
Jahmyr Gibbs         RB   $68   $63    -5    $63     92%  questionable
Jaxon Smith-Njigba   WR   $58   $66    +8    $66     78%  ecr- swing$12
```

Reading a row — three quantities, deliberately not blended:

| Column | Question it answers |
|---|---|
| `COST` | **what will it take to win him?** National AAV restated against this room's money and slots |
| `VALUE` | **what is he worth?** Median projections re-solved against the same pool. Pay this and you should get this production |
| `EDGE` | `VALUE − COST`. Positive is a bargain; negative means you are paying for a range of outcomes a median cannot see |
| `MY MAX` | the most you will bid. `*` = a must-have, i.e. a deliberate overpay. `!` = clipped to what you can afford. `—` = do-not-draft |
| `PS%` | fraction of positive value left at the position after he goes |

### Positional bias

An edge is only as good as the value source, and ours has a known skew.
Ciely prices **29 tight ends** above the floor against **13** in the market
and **12** starting spots — his tight end baseline sits far deeper than the
market's, which lifts every TE's value and makes the whole position look
cheap. Left alone, nine of the top twelve "bargains" were tight ends, and
none of it was about any individual player.

So the board reports the **median edge at each position** and subtracts it:

```
POS  MEDIAN EDGE  READING
QB   +5           value source runs high here
RB   -4           market runs high here
WR   +1           value and cost broadly agree here
TE   +8           value source prices this position far above the market
```

The `vs POS` column is the edge with that common component removed — how
much better a player looks than the rest of his own position. **That is the
number to act on.** A raw +$15 on a tight end when every tight end reads
+$8 is a baseline artifact; the same +$15 on a position sitting at +$1 is a
real disagreement about the player.

The median is taken over contested players only. Hundreds sit at the $1
floor on both boards with an edge of zero, and including them drags every
position's median to nothing.

Cost and value must stay separate because they answer different questions
and diverge exactly where it matters. Ashton Jeanty is worth $24 on a median
projection and costs $44 in real drafts — that $20 is the market pricing the
Kubiak scheme change, which a median cannot see. Collapsing them into one
"price" hides the only decision worth making.

### Price lines

What a bid buys at each position, in the sidebar:

```
PRICE LINES   top-3  top-5  top-8  top-12
QB              $13    $10     $5     $5
                $26    $17    $12     $2
RB              $58    $49    $43    $36
WR              $58    $52    $34    $26
                       $40
TE              $17     $6     $6     $3
                       $15
```

The question during an auction is never really "what is he worth" — it is
"is $40 on this back top-five money". The board answers in dollars and leaves
you to translate at the moment you have the least time to do it.

Worth showing because the same dollar means completely different things by
position. **$36 buys you TE1 or RB12.** The ladder at running back runs $68 down to
$36 across twelve players; at tight end it runs $36 down to $3.

**The muted second figure is what this league has actually paid** for that
finish, median of 2023-2025 — 2022 is out because a third of its money never
reached the table, and 2021 because one keeper against twenty-odd since is a
different auction, and it only appears where the room diverges from
the board by a third or more. Above, that is quarterbacks across the board —
this league pays $26 for QB3 where the cost model says $13 — and tight end at
the top-five line. Those gaps are the room's habits, and they are the reason
the reference comes from league history rather than from national AAV.

The lines are **live**. A player already gone counts at the price he actually
went for, so once four backs sell the top-three line moves to reflect them
rather than describing whoever is left over.

#### What it does not say

It says what the money buys, not how a player will do. Measured against this
league's own drafts, price rank and end-of-season finish rank correlate at
**+0.46** across twelve position-seasons — real, and nowhere near a promise.
The five dearest at a position finished in its top five **28 times in 60**.
2024's five most expensive backs finished 45th, 4th, 1st, 3rd and 31st.

`draftroom calibrate` re-derives all of that, so it stays a measurement rather
than a number somebody wrote down once:

```
DID PRICE RANK PREDICT FINISH RANK?  (1.0 = perfectly, 0 = not at all)
2023 QB   n=18  rho +0.49  top-5 money finished top-5: 3 of 5
2023 RB   n=55  rho +0.44  top-5 money finished top-5: 1 of 5
...
mean            rho +0.46  28 of 60
```

Positions nobody bids on are left out of that table. Defenses go for a dollar
or four here — eleven of them across a $3 spread — so their price rank is
mostly the order the picks happened in, and correlating it against a finish
produces a confident number about nothing. Sleeper does score defenses, so
the exclusion is about whether a price ladder exists, not about missing data.

### Position scarcity

```
POS  STARTABLE  STARTERS STILL NEEDED  COVER    PS% AT TOP  NEXT CLIFF
QB   8          11                     0.73x !  83%         34 pts
RB   26         21                     1.24x    92%         35 pts
WR   45         37                     1.22x    78%         31 pts
TE   14         12                     1.17x    52%         15 pts
```

**`STARTABLE` counts players good enough to start, not bodies.** A raw count
is worse than no count: 182 receivers remain against 43 starting spots, which
reads as the deepest position on the board, while the players anyone would
actually start are far fewer.

The bar is **the median projection of the tier the last starting slot falls
in** — not the projection of the player sitting in that slot. Those differ,
and the difference is the whole measure.

A single player is a knife edge: whoever happens to land at rank 42 sets the
line, and he is one projection revision away from being someone else. A tier
is the cluster he belongs to, and asking what its typical member projects for
asks the real question — how good is the player you end up with in the last
starting slot, and how many players are that good?

**Where the slot falls inside its tier is what makes the answer move:**

| | last starting slot | its tier | tier median | startable | slots |
|---|---|---|---|---|---|
| QB | Dart, 317 (rank 12) | ranks 4–12 | 322 | **8** | 12 |
| WR | Robinson, 152 (rank 42) | ranks 41–50 | 148 | **45** | 42 |

The last starting quarterback sits at the *bottom* of his tier, so the tier's
typical member is better than he is and only eight quarterbacks clear the bar
against twelve slots. The last starting receiver sits in the *middle* of a
long flat band, so forty-five receivers clear that band's median against
forty-two slots. Same league, opposite conclusions, and a point estimate at
the slot itself reports neither.

A tier straddling the slot is one tier — half of it landing on the bench is a
fact about roster rules, not about the players. Where the marginal starter
has a break on both sides he is his own tier, and the measure falls back to
the point estimate; that is how tight end behaves this season.

Tiers are read from **pinned pre-draft** projections. Against a bar
recomputed from the pool that remains, the line sinks as the pool empties and
the count above it barely moves — a scarcity number that cannot fall has
nothing to say.

Supply and demand are counted at the same line. Demand is taken at VOLS
depth, which is the starting spots themselves with no bench rounds behind
them.

`GONE` (used by the RB33 pivot, not shown in the table) counts players
already off the board at the position. It is a different question from
`STARTABLE` and rules about draft *progress* need it: Ciely's running back
rule is stated as "before the 33rd comes off the board", and 33 gone is
nothing like 33 remaining.

`COVER` is startable over spots. Below 1 the room cannot field the position
from players worth starting — **QB is at 0.73x before a single pick**, which
means three or four teams are going to start a quarterback nobody wanted.
`NEXT CLIFF` and `PS% AT TOP` remain the quality measures during a run.

Flags: `MUST` `+` `-` `DND` (your leans) · `vs <set>` (another lean set
reads him the opposite way) · `ecr+` `ecr-` `split` (industry deviation;
`split` means contested in both directions) · `sharp+` `sharp-` (the
most-accurate experts — FantasyPros' top-10/20 subset — rank him higher /
lower than the full consensus) · `swing$N` (value moves $N across baselines —
a fragile buy) · injury designation.

`sharp±` and `ecr±` are different sources and never share a code path: `ecr±`
is Subvertadown's industry-vs-market deviation, `sharp±` is FantasyPros'
accurate-expert subset against its own consensus.

**Baselines are a live strategy switch, not config.** VOLS values against
the last starter and concentrates hardest on the top; BEER+ balances
starters against a de-risked bench. VOLS also reorders the board, promoting
receivers over backs. Switch mid-draft if the room's behaviour changes.

### `make serve`

The draft-night view: the same board as a web page, built for a second
monitor beside the Sleeper window.

```sh
make serve ME=243501760939814912      # http://localhost:8083
```

Dark, large type, readable from a couple of feet away. Header strip carries
budget, slots, max bid and the safe ceiling; the pivot banner sits under it;
the sidebar holds positional bias, scarcity, and everything you have
recorded.

**Recording sales is the fast path.** Every row has `me` and `them` buttons.
`me` asks the price, debits your budget, clears the roster spot and
recomputes the ceiling immediately — you know a sale closed well before the
API does. `them` just removes the player and shrinks the pool. `clear all`
undoes everything.

Keyboard: `/` focuses the filter, `Esc` clears it. Typing a position
("rb") filters to it.

**Setting a lean from the board.** Every row carries a lean flag — `MUST`,
`+`, `-`, `DND`, or a `·` when you have no read. Clicking it cycles: none →
must → up → down → dnd → none. `MY MAX` and the must-have budget line move in
the same paint.

It is written straight back to `leans/mine.yaml`, so a read set mid-auction is
still there next time the board starts. The save is a read-modify-write of
that file rather than a dump of what the server is holding: you edit it by
hand too, and may have it open, so anything added since startup normally
survives. `cap` and `note` are carried across untouched for the same reason —
the board cannot set them, so it must not erase them, and that holds all the
way around the cycle including the step that clears the read.

The window between that read and the rename is small but real: driven hard
with an editor writing concurrently, about one hand-typed row in eight was
lost. On a single-user board that is a race worth knowing about rather than
locking against, but do not hand-edit the file mid-auction and expect both
writers to win.

Clearing a read that came from a *generated* set writes `none` rather than
deleting a row that was never there — otherwise the analyst's read simply
returns on restart. `none` is a real value in these files: it means "I looked
and I have no opinion", and it outranks a set that does.

The file is written without the `# generated by` header, deliberately. That
marker is what lets `leans -generate` overwrite a file, and a hand-written
set carrying it would be fair game for the next generation.

**The scratch roster** is the panel on the right, bordered and chipped
`hypothetical` so it can never be mistaken for the live board. `+` on any
row tries that player at his board price; click his price in the panel to
change what you think he goes for; `×` takes him off. The panel shows the
lineup filling slot by slot, the starting slots still empty, spend, budget
left, slots left and live POPR — the same POPR the archetypes report, from
the same scorer, so a roster you build by hand and a shape `draftroom
shapes` produced are directly comparable.

Nothing in the panel touches the draft. Scratch picks do not debit your
budget, do not remove anyone from the board, and are never confused with a
recorded sale — a leak in that direction would corrupt draft-night
arithmetic invisibly, since a scratch pick and a real sale both look like a
player leaving the board. The one way the live board reaches in: a player
who gets drafted for real while sitting in your scratch roster is dropped
from it and named in the note, because planning around someone already gone
is worse than losing the note. The roster lives in server memory and starts
empty on restart.

The page polls every 2s, matched to the server's own Sleeper poll — it
cannot have newer data than that. Both API reads are served from the cached
snapshot, so the page costs no Sleeper traffic at all; the server does the
one live-picks call, because building a full snapshot walks five seasons of
history and the 5 MB player dictionary.

### Running it on your tailnet

Same shape as leagueweb and canton — a launch agent for persistence, and
`tailscale serve` to mount it:

```sh
make draftroom            # build the binary the agent runs
make draftroom-install    # plist into ~/Library/LaunchAgents
make draftroom-load       # start it, and on every login
make draftroom-serve-mount    # https://<your-machine>.<tailnet>/draftroom
```

`draftroom-status` says whether the agent is loaded, `draftroom-serve-status`
shows the current mounts, and `draftroom-unload` / `draftroom-serve-unmount`
undo each half.

**`tailscale serve`, never `tailscale funnel`.** Serve is tailnet-only —
your devices and nobody else's. Funnel would put this on the public
internet, and this board carries your valuations, your leans, your keeper
analysis and what you are willing to pay for whom. That is the whole edge,
readable by anyone who found the URL.

**Safe to leave mounted year-round.** The board polls Sleeper every two
seconds while a draft is running and once a minute when one is not, decided
per tick from the draft's own status. A flat two-second poll left up all
year would be forty-three thousand requests a day for a draft that happens
once; the idle cadence is fourteen hundred, and it snaps to two seconds
within the minute of your commissioner starting.

The plist pre-fills the config and data directories and your owner ID,
because the binary in the checkout is built by `make draftroom`, which does
not bake paths the way `make install` does. Without the owner ID the board
reads every team as a flat $200 with no keepers deducted.

### Rehearsing against a mock draft

The board is a draft-night tool that gets used once a year, which is a bad
time to discover a wiring fault. `-draft` points it at any Sleeper draft by id:

```sh
draftroom serve -addr :8085 -draft <mock-draft-id> -me <your-sleeper-id> -leans rehearsal
```

Discovery normally finds the draft through the league, and Sleeper's standalone
mock drafts belong to no league — so without the flag there is nothing to find,
and the board would sit watching the real league instead.

**Check the mock is an auction first.** Sleeper's mocks default to snake, and
`Metadata.Amount` is empty for anything that is not an auction, so `Dollars()`
returns 0 and every pick sells for nothing. The board will look like it is
working. A rehearsal that never exercised the money is worse than none, because
you will trust it.

Two things to set up so the rehearsal cannot cost you anything:

- **Point `-leans` at a copy.** The default set is often a symlink into a notes
  vault, and reads set on the board are written straight through to it. Copy it
  to `leans/rehearsal.yaml` first; that still exercises the whole lean path on
  your real reads without editing the ones you will draft from.
- **Use a spare port.** `:8083` is the live board and `:8084` is the second one.

What to actually watch, since a mock is only useful if you know what would
count as a failure:

| | What should happen |
|---|---|
| Picks arrive | A pick made in Sleeper leaves the board within about two seconds. Pool dollars and slots drop, and the remaining prices move. You should never need to record a sale by hand. |
| Money tracks | The winning bid appears as the sale price. When the pick is yours, budget, slots, max bid and safe ceiling all move together, and the risk band changes as you spend. |
| Reads survive | Leans set before the draft still show at the end; one set mid-draft is in `rehearsal.yaml` on disk; `reload leans` does not drop board edits; the scratch roster and recorded sales survive the whole thing. |

Expect the **Kept panel and the keeper scenarios to be empty or meaningless**.
`owners.csv`, `keeper-locks.csv` and `rulings.csv` are keyed to the real
league, and a mock shares none of it. That is correct rather than broken, and
it gives you a clean full pool to rehearse against.

### Player types on the board and the roster

Every contended player is labelled with what kind of player he is, and the
scratch roster adds them up as you build it:

```
composition: 2 floor · 2 redzone · 4 targets · 2 3-down · 1 upside · 1 discounted
```

That count is over **starters**, not the whole roster: a bench stacked with
one kind of player changes nothing about how a season goes.

The labels appear on the web board's rows, where colour and space carry
them. They are deliberately absent from `make board` — that table is the
narrow one, six flags on a row reads as none while bidding, and `targets`
sits on nearly every player at the top of it, so it separates nothing where
the money is.

#### The traits

All position-relative — a 27% touchdown share is ordinary for a back and high
for a receiver, so an absolute cut would label whole positions rather than
players. Only the contended range is labelled; a trait on someone nobody will
roster is a label with nothing behind it.

| Trait | What it means |
|---|---|
| **floor** | touchdown share in the bottom 40% for the position, and real reception volume for pass catchers. Points from usage that repeats |
| **redzone** | touchdown share in the top quartile. The least predictable points in football, bought on purpose |
| **upside** | the industry flags him above consensus, **or** he is projected past his own record |
| **targets** | projected targets in the top quartile — the stickiest thing a pass catcher has |
| **3-down** | a back with both the ground work and the passing downs, not a committee share. Both sides need real volume: a passing-down back with a token carry is not one |
| **discounted** | price suppressed by an injury designation; you buy the discount and the risk together |

Floor and red-zone are opposite ends of one axis, and nobody on this board is
both — 0 of 446. That is a property of the data rather than something the
code enforces: the quantile index calculation can collapse the two thresholds
onto the same element when a position's window is very small, and both would
then fire on one player.

#### How the traits are derived

Ciely publishes his projections' components. Recomputing them across the 442
of 447 rows that carry components reconstructs his published league points to
within **0.07 points** — mean absolute error 0.019, mean *signed* error
+7.2e-05. Only 31 rows land at exact float equality and 52 within any
tolerance from 1e-12 to 1e-6; the rest is rounding in the published columns
(touchdowns at 2dp x 6 points, yards at 1dp). The near-zero signed mean is
what rules out a scoring mismatch rather than a rounding one.

**"Projected past his own record"** is measured **per game** against last
season's half-PPR production. Per season would punish injury rather than
measure ambition: a back who missed half a year looks like a huge projected
leap on totals and no leap at all on rate. Under six games counts as unproven
outright — four good games is not a season's evidence, however good it was.
The threshold is 1.4x, where the league's own distribution puts the 90th
percentile at 1.32.

**A signal that didn't survive contact:** value spread across the BEER/BEER+/
VOLS curves looked like a third upside signal and was not. It scales with
price, so it flagged **100% of players over $45** and almost nothing under
$10 — an expensiveness detector wearing the name of an upside one. The curve
spread is still shown per player as `swing$N`, where it means what it says.

### What happened to the roster shapes

There were eleven of them — five ways to divide a budget, six made of player
types — each filled greedily against the live board and compared. They are
gone, and the reasons compound:

- **They ranked nothing.** Three seasons of this league's own drafts say no
  spending shape separates on results. Every correlation between shape and
  points sits at or under about 0.2 at n=36.
- **The fill could not answer the question asked of it.** "Is this shape
  reachable?" needs a search; a greedy fill failing proves only that a greedy
  fill failed. Three verification rounds each found a defect traceable to
  reading that failure as an answer — a keeper falsely blamed for a shape he
  was the best asset of, then true blames silenced, then shapes declared out
  of reach while a roster reaching them existed at the same budget.
- **Keepers had already settled most of it.** Once two keepers fix a third of
  the roster, "how do I divide the budget" is largely answered before the
  auction opens.

What they were reaching for survives without any of the machinery: the
composition of the lineup you are actually building. `draftroom calibrate`
still asks the one question that never needed the shapes — whether how a
manager spends predicts how the season goes.

### `draftroom calibrate`

Measures the archetype thresholds against completed drafts, so the numbers in
`Archetypes()` carry their derivation instead of being numbers somebody liked.

```sh
draftroom calibrate                    # 2023-2025, the usable seasons
draftroom calibrate -seasons ""        # every season whose prices compare
draftroom calibrate -all               # every season with a complete draft,
                                        # including ones whose prices don't compare
```

```
CALIBRATION — 36 rosters across [2023 2024 2025]

SHAPE           BUILT  SHARE  MED RANK  MED PTS  TOP 4
Stars & Scrubs  10     28%    7.0       1458     4 of 10
Balanced        10     28%    9.0       1406     3 of 10
Hero RB         9      25%    8.0       1452     2 of 9
Zero RB         7      19%    6.0       1495     2 of 7
Robust RB       9      25%    4.0       1540     5 of 9
league median   36     100%   6.5       1471

DOES SPENDING SHAPE PREDICT POINTS?  (n=36, |rho| > 0.33 is p<0.05)
top-2 concentration   rho +0.20   no signal
RB total spend        rho +0.13   no signal
best RB price         rho +0.17   no signal
...
```

**Which seasons count.** 2022 is excluded automatically: the median team spent
$157 of $200 because the league was still working out how keeper money came
off the auction budget, so a third of every roster was bought with money that
was never at stake. Including it drags every price threshold down for reasons
unrelated to how anybody drafts now. 2021 is the inaugural Sleeper draft — one
keeper, a full pool — so it is available but structurally a different game.
The thresholds do **not** hold up on it as a holdout: `draftroom calibrate
-seasons 2021` gives Stars & Scrubs 3 (25%), Balanced 0 — never built, Hero RB
4 (33%), Zero RB 1 (8%), Robust RB 5 (42%). One shape describes zero rosters
and two more sit well outside the 19–28% band. That is the expected result of
a different auction rather than a failed calibration: with a full pool and
only one keeper off the board, top prices ran higher and no team was forced
under the $35 ceiling that Balanced requires. The thresholds are specific to
the keeper era, not validated against 2021.

**What the calibration changed.** The old thresholds were national-strategy
numbers. Against this league they described almost nothing: Zero RB at "no
back over $12" matched **one roster in three years**, Robust RB at "three
backs over $25" matched **two**. Every shape now describes 19–28% of rosters.

Two shapes were also reformulated, because per-player thresholds could not
express what the strategies mean:

- **Zero RB** needs both a cap and a total. On a total alone a single $55 back
  passes, and that is Hero RB wearing the name; on a cap alone, three $19
  backs read as both Zero RB *and* Robust RB at once.
- **Robust RB** is a floor to reach rather than a ceiling to stay under, so it
  has no per-pick veto — a veto can only forbid. Its anchors do the pursuing.

**Hero RB had a real bug.** Its finished-roster check counted only the hero,
while the per-pick rule capped the second back. A roster with a $60 back and a
$45 back behind him passed a shape that is defined by not having one.

**Nothing predicts results.** Every correlation between spending shape and
points sits at or under about 0.2 at n=36 (the largest, top-2 concentration,
is +0.2015), and the best-looking bucket is nine rosters.
Robust RB's median rank of 4.0 is the eye-catcher, and at that sample it is
p≈0.1 before correcting for having looked at five shapes. **The archetypes
describe the space; they do not rank it.** Re-run this after 2026 — a fourth
season is a 33% larger sample and the first real chance of a signal.

### `make leans`

Shows the merged lean sets and what they disagree about, before draft night
rather than during it.

```sh
make leans LEANS=mine,menton
make leans-generate                 # rebuild the analyst sets from source
```

A merge you cannot inspect is a merge you cannot trust: precedence decides
which of two contradictory reads reaches the board. See
[Lean sets](#lean-sets) for what the sets are and how precedence works.

It also reports two things a lean file cannot tell you by looking at it:
players listed with no read yet, and reads naming a player the projection
source does not have — with the nearest real player suggested. A lean is
applied by name, so a name typed wrong is not an error, it is simply absent
from the board.

### Editing leans away from the machine

A lean set is often a symlink into a notes vault, so it can be edited from a
phone while doing research. The loop:

1. **Edit** in the vault — add a name under `up:`, or leave one under
   `undecided:` if you have not ruled on him.
2. **Check** with `make leans LEANS=mine,wubrg-lean`. This is where a typo
   surfaces; the board will not tell you, because a read it cannot match
   simply is not there.
3. **Reload** — press **reload leans** in the board's footer. It re-reads
   the files and rebuilds, and it works from the phone you edited on, since
   it is a button on the page rather than something run on the server.

Reload is deliberately a button and not a timer. Lean sets are otherwise
read once, at startup, and nothing else about them moves while a draft runs;
polling the files would rebuild the board every couple of seconds to learn
nothing. A file that does not parse leaves the reads you already had exactly
where they were and reports the error — losing every conviction you hold to
a half-typed file would be worse than the staleness it fixes.

`make draftroom-restart` is only needed after building a new binary.

Precedence is left to right, so `mine` outranks a set listed after it; see
[Lean sets](#lean-sets).

### `draftroom sources`

What each normalized source contributes, and which of its rows reach no
Sleeper player.

```sh
draftroom sources             # counts per source, plus anything unresolved
draftroom sources -unmatched  # only the rows that resolve to nobody
```

The board can only ever say *how many* rows failed. A count tells you
something is wrong without telling you which player is missing, and a player
missing from the pool is invisible until somebody nominates him — he cannot
be leaned on, priced, or bid against.

For each failed row it names the closest Sleeper player and prints the
`aliases.csv` line that would fix it, so a nickname is a paste rather than a
hunt for a player id:

```
ciely: 1 of 447 rows reach no Sleeper player

  Hollywood Brown  WR PHI
    no player with that name
    closest: Marquise Brown (WR, PHI) id=5848
    aliases.csv: Hollywood Brown,5848,ciely name for Marquise Brown
```

Two ways of finding a candidate, because the failures come in two kinds. A
misspelling is close as a string, so edit distance finds it. A nickname is
not close at all — "Hollywood Brown" and "Marquise Brown" share nothing but
a surname — so the second pass uses the surname with the position and team
the source already stated, and only when exactly one player fits. Where
neither is decisive it says so rather than naming a guess: a wrong alias
binds every read on that player to somebody else, silently.

**It needs no league.** Resolving a name wants names, positions and teams,
not the keeper ledger, so this is one Sleeper call and no `-league`.

### `make refresh`

Re-runs every extractor against the newest snapshot in the private repo,
then rebuilds the generated lean sets from it.

```sh
make refresh
```

**Do this ~Sept 1–2.** It matters more than it looks: Ciely projects players
as available until there's an official timetable, so his numbers go stale
for exactly the players whose injury status resolves in the final weeks.
August designations are roster mechanics; September ones are real.

### `make data-status`

Where the private repo is and how old each snapshot is.

---

## Adding a new source snapshot

1. Save the export into `$DRAFTROOM_DATA_DIR/raw/<source>/<date>/`.
2. `make refresh`.
3. `make board` — unmatched player names are reported, never dropped.

Subvertadown needs a **full page save** (the tool renders client-side, so a
plain fetch returns an empty shell). Save the three `stock-*` sheets into
`raw/subvertadown/<date>/sheets/`; the `qbstream-*` variants are identical
and skipped.

---

## The files you maintain by hand

All in `internal/draft/data/`, tracked in this repo because they are yours,
not a vendor's.

### `leans/mine.yaml`

```yaml
must:
  - Ashton Jeanty
up:
  - Chase Brown
dnd:
  - Kyle Pitts

caps:
  Ashton Jeanty: 48
notes:
  Ashton Jeanty: Kubiak scheme + OL fix; model has him at $24
```

Players group under the read rather than carrying it as a field, so adding
one is a single line. `caps` and `notes` are optional and sit apart because
both are rare. An `undecided:` group holds names you have written down but
not ruled on. The older `player,lean,cap,note` CSV is still read;
`draftroom leans -convert` rewrites a set as YAML.

| Lean | Effect |
|---|---|
| `must` | walk-away = the computed risk ceiling. `cap` optional, and only ever tightens it |
| `up` / `down` | ±15% on the model's value |
| `dnd` | $0 at any price, absolute |

`up`/`down` and `must` answer different questions. Conviction says the
model's median understates a player's range; a must-have says you will pay
above your own estimate to secure him, which is the point of getting your
guys. When you disagree with the model by a lot, use a capped must-have —
no percentage of a number you think is wrong produces the number you would
actually bid.

### Lean sets

`mine.yaml` is one **lean set**. Others live beside it in
`internal/draft/data/leans/`, and `-leans` says which to apply:

```sh
make board LEANS=mine,menton
make leans LEANS=mine,menton      # inspect the merge before draft night
```

**Precedence is the order you list them** — the first set to name a player
owns him. `mine,menton` means your reads win and Menton fills in everyone
you have no opinion about.

The losing read is not discarded. A player two sets read opposite ways is
marked `vs menton` on the board and listed as contested by `make leans`:

```
!Kenneth Walker        must   mine     menton says down — you: Betting the situation…

1 contested: your read stands, but another set argues the other way.
```

That is the reason for splitting the files at all. Menton's Big-3 traits
say Walker is a zero-trait back; you have him as a must-have on situation.
Both are true statements about him, and seeing them side by side beats
either one winning quietly. Agreement is not flagged — `must` and `up` are
a difference of degree between people on the same side, not a conflict.

| Set | Source | What a lean means |
|---|---|---|
| `mine` | you | what you will and will not do |
| `menton` | `fantasypoints-big3-2026.csv` | two of the Big-3 traits leans up, none leans down |

Generated sets are rebuilt from the private source repo:

```sh
make leans-generate     # also runs at the end of make refresh
```

They carry a `# generated by draftroom leans -generate` header, and that
marker is the only thing that makes a file eligible to be overwritten — a
set without it is treated as hand-written and left alone, so a regeneration
can never eat `mine.yaml`. They are also **not committed**: they derive from
the private repo and this one is public, so `leans/.gitignore` allows
`mine.yaml` and excludes the rest.

Menton's threshold is two of three rather than one because one is the
common case — most backs are good at something — and a lean that fires on
almost everyone says nothing. On the 2026 field that is one back up and
three down, which is the article's actual claim rather than a softened
version of it.

### How the must-have ceiling is calculated

You do not pick it. Choosing a number yourself is a guess with a decimal
point; the tool works out how far you can go before the rest of your roster
suffers.

The measure is positional, because an auction is. Spending $113 of $130 on
one player is not risky because $113 is large — it is risky because it
leaves you buying starters at a dollar while everyone else buys them at
fifteen. So each bid is scored as:

```
your $ per remaining starting spot  ÷  the league's $ per remaining starting spot
```

Both sides reserve $1 for every bench spot, and both come from the live
pool, so the ceiling **moves with the draft**: when the room overspends
early, the league figure drops and your recommendation rises on its own.

| Ratio | Band |
|---|---|
| ≥ 1.00 | comfortable |
| ≥ 0.75 | stretched ← the default ceiling |
| ≥ 0.50 | risky |
| < 0.50 | dangerous |

The board prints it up top:

```
max on one player before the rest of your roster suffers: $49
  (stretched — $18 per remaining starter vs the league's $24 (76%))
```

That is a per-player ceiling, so it does not stop you committing to several.
The must-have line does that job — two players at $49 leaves $32 for ten
slots, and the board says so and warns when it gets too thin to field a
roster.

### `rulings.csv`

Commissioner decisions the written rules don't settle. A ruling replaces
the computed price *and* becomes the basis the next season chains off, so
one entry fixes everything downstream.

```csv
season,player_id,price,keep_count,reason
2025,8151,31,,"Walker: charged $31 where the ladder said $36; LM ruled it stands"
```

### `aliases.csv`

Nicknames no normalization will reconcile — sources say "Kenneth Gainwell"
where Sleeper says "Kenny Gainwell".

---

## What the numbers are and are not

Four sources measure genuinely different things, and the board keeps them
apart rather than blending them:

- **FantasyPros ECR** is a second projection and an expert-consensus ranking.
  Its `league_points` are recomputed under this league's scoring like Ciely's,
  then re-solved into dollars as the **FP** column on the web board — a
  like-for-like second opinion beside **Value**, so where the two projection
  sources diverge is visible rather than blended away. The **ECR** column is
  its positional consensus rank. Because its top-10/20 most-accurate-expert
  subsets ship alongside consensus, a `sharp±` flag marks where the sharps and
  the full field disagree. Alone among the four it publishes a **high and a low**
  as well as a middle, recomputed under league scoring the same way, which is the
  only place on the board a projection says how much it does not know. Where that
  range comes out wider than the FP value it brackets, the row carries a `range`
  pill.

  The projections come from FantasyPros' own projection exports, not from the
  `…-ecr-stats-…` view that sits beside its rankings: that view is **last
  season's actuals**, and scoring it floored every 2026 rookie at nothing while
  reading as a second projection. Interceptions and fumbles are both published
  in the projection exports; interceptions are scored at the league's -1, and
  fumbles are deliberately **not** scored, because `SCORING` has to stay
  key-for-key with Ciely's or FP stops being comparable to **Value** at all.
- **Ciely** produces *median* projections, which explicitly removes the
  range of outcomes. His dollars are a linear map of those medians
  (r = 1.000), so they cannot see keeper inflation and they systematically
  underprice upside. Use his ordering; do not read his dollars as prices.
  Values under ~$15 carry no information — his board floors there while a
  real market has a long $1–3 tail.
- **Subvertadown AAV** is what humans actually pay, so it prices the range.
  Measured against Ciely, the market pays about **$5 more** for players
  carrying an industry upside flag and **$9 less** for contested ones.
- **Big 3** (Fantasy Points) ranks upside *at cost*, not absolute quality.
  Gibbs and Bijan are absent from that list because they are priced for it
  already, not because they failed to qualify.

There is **no inflation multiplier** anywhere in this tool. Prices are
re-solved from the money and players actually left in the room; keeper
inflation falls out of that automatically. See `internal/draft/value.go`.

---

## Testing

```sh
make check          # gofmt + vet + Go tests + extractor tests
make extractor-test # just the Python extractors
```

No test touches the live API.

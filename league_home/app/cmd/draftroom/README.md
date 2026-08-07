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
runtime, so editing `leans/mine.csv` takes effect immediately — no rebuild.

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
`split` means contested in both directions) · `swing$N` (value moves $N
across baselines — a fragile buy) · injury designation.

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

To reach it from another device on your tailnet, the leagueweb pattern
applies — see the `leagueweb-serve-*` targets for the shape.

### Shapes by player type

The money shapes ask how to divide a budget. That stops being the interesting
question once keepers hand you a surplus — the distribution is half-settled by
what you kept, and what's left to choose is *what kind of players* the rest
buys. So `shapes` reports two families:

```
BY MONEY — how to divide the budget
Stars & Scrubs  543  $200  $1   $36   $156  $7
Hero RB         521  $200  $4   $103  $80   $13  only reached without De'Von Achane
Balanced        506  $199  $13  $53   $106  $27
Zero RB         506  $199  $13  $53   $106  $27
Robust RB       502  $199  $1   $152  $42   $4

BY PLAYER TYPE — what kind of players the money buys
Target Volume    543  $200  $1  $36   $156  $7
Red Zone Corner  530  $200  $1  $37   $145  $17
Bell Cows        521  $200  $4  $103  $80   $13
Buy the Injuries 507  $199  $1  $103  $85   $10
Upside Swing     492  $200  $1  $90   $102  $7
Floor Build      422  $200  $1  $98   $44   $57
```

Every shape's detail also lists what its lineup actually is, so you can read
one against another:

```
Red Zone Corner — corner the touchdowns
  starters: 2 high floor, 4 red-zone ace, 2 upside, 7 target hog, 2 bell cow
```

#### The traits

All position-relative — a 27% touchdown share is ordinary for a back and high
for a receiver, so an absolute cut would label whole positions rather than
players. Only the contended range is labelled; a trait on someone nobody will
roster is a label with nothing behind it.

| Trait | What it means |
|---|---|
| **high floor** | touchdown share in the bottom 40% for the position, and real reception volume for pass catchers. Points from usage that repeats |
| **red-zone ace** | touchdown share in the top quartile. The least predictable points in football, bought on purpose |
| **upside** | the industry flags him above consensus, **or** he is projected past his own record |
| **target hog** | projected targets in the top quartile — the stickiest thing a pass catcher has |
| **bell cow** | a back with both the ground work and the passing downs, not a committee share |
| **injury discount** | price suppressed by a designation; you buy the discount and the risk together |

Floor and red-zone are opposite ends of one axis, and nobody on this board is
both — 0 of 446. That's a property of the data, not something the code
enforces: the quantile index calculation can collapse the p40 and p75
thresholds onto the same element when a position's window is very small, and
both traits then fire on the same player.

#### How the traits are derived

Ciely publishes his projections' components. Of the 447 rows in
`ciely-2026.csv`, 442 carry components; recomputing them reconstructs his
published league points, over those 442 rows, to within **0.07 points** —
mean absolute error 0.019, mean *signed* error +7.2e-05. 31 rows match at
exact float equality and 52 match at any tolerance from 1e-12 to 1e-6; the
rest is entirely consistent with rounding in the published columns
(touchdowns at 2dp × 6 points, yards at 1dp), and the near-zero signed mean
is what rules out a scoring mismatch rather than a rounding one. So every
player with components decomposes into touchdown, reception and yardage
points with no fitting involved.

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

#### Why trait shapes have no per-pick veto

A shape about what you own is a floor to reach, not a ceiling to stay under,
and a veto can only refuse. Vetoing every player without the trait would
forbid the cheap ones needed to field a legal lineup and produce a roster with
four red-zone aces and no quarterback. The anchors do the pursuing instead —
and are immovable once bought, because the greedy upgrade pass will otherwise
trade four red-zone aces for four better players who score nothing at the goal
line, dismantling the shape it was asked to build.

### Shapes and your keepers

A shape is a claim about the finished fourteen, so `-me` builds every shape
**around the players you are keeping**:

```
Every shape is built around your keepers — Puka Nacua WR $35, De'Von Achane RB $35

SHAPE           POPR  SPEND  QB   RB    WR    TE   NOTES
Stars & Scrubs  543   $200   $1   $36   $156  $7
Hero RB         521   $200   $4   $103  $80   $13  only reached without De'Von Achane
Balanced        506   $199   $13  $53   $106  $27
```

Filling only the twelve slots you still have to buy got every RB shape wrong
for anyone holding a back:

| shape | keeper-blind | truth |
|---|---|---|
| Hero RB | achieved | **impossible** — Achane at $35 is already a second back over $20. The note says only "only reached without" him, because the code cannot prove impossibility in general even where it happens to hold |
| Zero RB | achieved at $27 on backs | $62 with the keeper, over the $61 ceiling |
| Robust RB | "did not reach it" at $62 | the keeper had already carried it past the line |

**"only reached without X"** is decided by experiment, not by reading the
constraint: the shape is built again with that keeper set aside — and off the
board, so the fill cannot rebuy him — and if it succeeds then he is in the
way. Two keepers are judged one at a time rather than by leaving each out,
because two that are *each* fatal would otherwise cancel: remove either and
the other still breaks it, so neither looks responsible.

The wording is deliberately weak. What the experiment establishes is that a
roster exists without him and the greedy fill could not find one with him —
which is a fact about the search, not about the board. An earlier version
read that as proof and said "ruled out by keeping X"; three blames in
twenty-eight were false, and one told you to abandon Robust RB while a
roster reaching it with that keeper existed at the same $200. Since this note
outranks every other on the row, overclaiming here is the most expensive
mistake the report can make.

Keepers cannot be sold to make room, so the upgrade pass may not swap one out.
The scratch roster on the web board starts with them for the same reason —
otherwise it understates POPR and disagrees with this report about the same
roster.

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

### `leans/mine.csv`

```csv
player,lean,cap,note
Ashton Jeanty,must,48,"Kubiak scheme + OL fix; model has him at $24"
Chase Brown,up,,"most mispriced RB per Menton"
Kyle Pitts,dnd,,"never again"
```

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

`mine.csv` is one **lean set**. Others live beside it in
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
can never eat `mine.csv`. They are also **not committed**: they derive from
the private repo and this one is public, so `leans/.gitignore` allows
`mine.csv` and excludes the rest.

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

Three sources measure genuinely different things, and the board keeps them
apart rather than blending them:

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

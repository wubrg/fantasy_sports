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
runtime, so editing `my-guys.csv` takes effect immediately — no rebuild.

Re-run `make install` if you move either repo.

Working from the checkout instead? Every command has a `make` shortcut and
needs no setup at all.

| Variable | What it points at | Default |
|---|---|---|
| `DRAFTROOM_DATA_DIR` | private data repo | `../fantasy_sports_data` from the repo root |
| `DRAFTROOM_CONFIG_DIR` | rulings/aliases/my-guys | `league_home/app/internal/draft/data` |
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

Flags: `MUST` `+` `-` `DND` (your leans) · `ecr+` `ecr-` `split` (industry
deviation; `split` means contested in both directions) · `swing$N` (value
moves $N across baselines — a fragile buy) · injury designation.

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

The page polls every 20s and the server caches for the same, because
building a snapshot walks five seasons of history and the 5 MB player
dictionary. Hitting Sleeper per request during a live draft would be a bad
way to get rate-limited.

To reach it from another device on your tailnet, the leagueweb pattern
applies — see the `leagueweb-serve-*` targets for the shape.

### `make refresh`

Re-runs every extractor against the newest snapshot in the private repo.

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

### `my-guys.csv`

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

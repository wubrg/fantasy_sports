# Using the web UI, week to week

`weekly-slate.md` walks the same tool from a terminal. This is the same board, but the operator's
actual week runs almost entirely through the phone UI at `edgectl board serve` — a book's app open
in one tab, the board in the other. A personal cadence file (kept outside this repo, since it's a
week-to-week scratchpad, not documentation) lays out roughly what gets bet and when; this doc is the
tab-by-tab version of the same week, written so it stands on its own without that file.

**What this walkthrough does not do.** It will not tell you which props or games are worth looking
at, and it does not replace the belief probe or the scenario grid for pricing a prop from a game log
— those stay CLI tools, called out below where they show up in the week.

---

## Before week 1 of a slate: get the board running

Two things happen once, from a terminal, before any tab is useful:

```
$ edgectl board scaffold -week 1        # once per week, from the schedule
$ edgectl board serve -addr :8085 -ingest-dir ~/adori/Edge/ingest
```

`scaffold` writes the week's YAML file from the schedule; nothing in the web UI creates a week file,
and `period`/`funds` boost-expiry both depend on that file existing to know when the week starts and
ends. `serve` is the actual web server — leave it running (or behind Tailscale, see the repo's
Tailscale notes) and everything below is a tab in the browser it hosts.

---

## Tuesday — close the books on last week

**funds tab.** Withdraw last week's winnings from each book to the bank first (outside the app, in
the book's own UI), then come back and tap **zero out** next to each book's cash balance. This
appends a withdrawal to the ledger — the same event `edgectl ledger add -kind withdraw` would record
— so the period report sees it as a withdrawal, not a loss. It only zeroes *cash*; bonus and boost
balances aren't touched, and aren't meant to be.

While here, declare whatever fresh budget or boost is in hand for the new week — a deposit, a
matched bonus, a promo credit — using the **declare a boost** / **declare a no-sweat token** forms
lower on the funds tab. Leave the expiry field blank and it auto-expires at the end of whichever week
the banner is currently showing (there's no separate week field to fill in — it just follows the
banner); that's the Tuesday-before-next-kickoff boundary, the same one the period report uses, so a
boost and the report explaining where it went agree on when the week closed. Type an explicit date
into the expiry field instead for the rarer promo that should outlive one week.

## Wednesday — resolve the log, get ready for Thursday

**log tab.** Settle anything still open from the prior slate: **won** / **lost** / **push** / **void**
per wager. If a same-game parlay got repriced by the book after a leg voided (an injury scratch is
the common case), use **repriced…** instead — it asks for the price the book actually paid and
records that without rewriting what was predicted at placement.

**props tab.** Drop the week's DraftKings capture (a `.har`, or its raw JSON) into the ingest folder
and hit **reload**. It groups everything by game — collapsible sections, category under each — and
shows implied and fair (de-vigged) prices. When it looks right, hit **sync to board**: it plans a
diff against what's already on the board and shows it before writing anything. Confirm to write the
game-line odds (moneyline/spread/total) onto the week's board. This is the only way a price reaches
the board now — there's no manual entry form left.

**bets tab.** Once the board has prices, this tab is the report: check the book-pool chips at top
(pick which book's prices you're viewing recommendations against — consensus, the schedule's own
reference line, is deliberately not selectable, since it isn't a price you can actually bet), the
dogs ranked by bonus-bet conversion, and — if a bankroll is funded — the frontier/allocation section
showing how many tickets to build and how much per ticket.

## Thursday and the weekend slates — place wagers

**bets tab.** For a straightforward parlay or dog the board already priced, tap **record** on a
ticket, confirm the stake (it suggests one from the frontier) and the week it's for, and it's logged.

For a prop, an SGP, or anything the board doesn't carry a price for, use the **calculator** at the
bottom of the bets tab:
- **hit rate** — paste a game log, a line, and (optionally) a price; it returns the hit rate with a
  confidence interval and, with a price, a supported/reject verdict against the breakeven.
- **multi-leg wager** — tag each leg with a game. Legs in different games combine automatically into
  a fair price; two or more legs sharing a game tag are refused (a correlated same-game parlay isn't
  a product of independent prices) — enter the book's own combined SGP price as the overall price
  instead, then **log this wager** to record it.

Pricing a prop from scratch — deriving a hit-rate estimate from a scenario grid rather than a raw
game log — is still a terminal step: `edgectl scenario` (see `weekly-slate.md` §3) or the belief
probe below. The calculator only combines and scores numbers you already have.

**funds tab.** Check what's still on the board before a slate locks: the **expiring** section flags
anything with under 48 hours left, and the boosts list is ranked by ceiling (real value) rather than
headline percentage, so a 100%-boost-capped-at-$5 doesn't outrank a 25%-boost-capped-at-$50.

**log tab.** Anything typed by hand rather than recorded from the bets tab (a same-game parlay's book
price, a straight prop) goes in through the entry form: selection, price, stake, an optional win-
probability belief (drives the EV column later), which balance it draws from, and an optional boost
to apply. Tick **deposit stake to fund it** when the stake needs a matching deposit first rather than
coming out of an existing balance.

## After each slate — settle the log

**log tab**, same as Wednesday: work through **won**/**lost**/**push**/**void** (or **repriced…**)
for what just played. Doing this right after a slate rather than saving it all for Monday keeps the
board's "already committed" section (bets tab) accurate — a settled game frees its team back up for
next week's parlay set; an open one stays excluded so it can't get bet twice.

## Monday night — final settle, then check the week

**log tab.** Settle Monday Night Football the same way.

**period tab.** Pick (or leave on the default, which lands on the current week) the week number and
read the week's actual P&L: deposits/withdrawals moved with the bank, realized cash and bonus on
what's graded, what's staked, what's still open, and a reconcile line — net-to-bank against realized
net, with the gap explained by open stake and parked bonus. This is the number a point-in-time funds
balance can't give you once Tuesday's zero-out has already reset it: funds answers "what do I hold
right now," period answers "what did this week cost or make."

---

## Where the belief probe fits

The **beliefs** tab runs a separate, no-money experiment: whether a model's read on a game script
beats the market. It doesn't touch the board, the log, or the bankroll. If you're running it:

1. **Friday**, from a terminal: `make belief-pack SEASON=2026 WEEK=<n>` — needs the data cache and
   Python, so it stays a CLI step. It writes the week's facts and a pasteable prompt.
2. **beliefs tab**: *copy prompt*, paste into a model, paste the JSON it returns back into the tab,
   *preview* (it runs the same gates the CLI would — a bad hash, a late file, a contradicted claim
   are all refused with the reason shown), then *apply to log* — before kickoff.
3. **Tuesday**, after settling from data on the machine: *load score* in the tab. The verdict is
   accuracy against the hardest opponent (market, incumbent model, a line-only null); a second
   profit-flavored number is a diagnostic, not a second gate.

## The honest summary of a week

Every tab above reads or writes the same week file and the same ledger the CLI commands in
`weekly-slate.md` use — nothing here is a second source of truth. What the web UI adds is that a
normal week's actual work (reading prices, placing a wager, settling one, zeroing a book, checking
the week's P&L) never needs a terminal. Pricing a prop from a game log, generating a belief pack, and
scaffolding a new week's board still do.

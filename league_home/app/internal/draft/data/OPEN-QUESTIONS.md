# Draft room — open questions

Written 2026-08-05. Draft is **Thu 2026-09-03, 8:00 PM** (29 days out).

Items are ordered by how much they change the board. Nothing here blocks
the next chunk of work; they need your call, not mine.

---

## 1. Sources I could not get, and what I need from you

Everything except Ciely is either paywalled or rendered client-side with no
export. None of it is scrapeable, and none of it should be — the ingest
design takes paid content from your own copy only.

| Source | Status | What would unblock it |
|---|---|---|
| **Jake Ciely** | **Ingested.** 447 players, $2,400 in auction values | nothing |
| **JJ Zachariason** | Blocked. Late-Round Draft Guide is **$29.99**; no free rankings anywhere. FantasyPros' "JJ Zachariason vs Consensus" page renders in JS and returned "No rankings found" | Buy it and export/copy the rankings, or say skip him |
| **Peaked in High Skool** | Blocked. Cheat sheets moved to **Patreon** ($10/mo tier). Their latest published sheet is **2025 V2.01**, not 2026 | Your Patreon copy of the 12-team half-PPR PDF, or skip |
| **Subvertadown** | Blocked. Tool is free but 100% client-side — no CSV export, no API, no data in the HTML | Run the tool with our settings and copy the table out |
| **FantasyPros ECR** | **Ingested 2026-08-13.** 787 consensus players + top10/top20 sharp subsets | nothing — see `docs/backlog.md` `D6` |

**FantasyPros re-checked 2026-08-12.** The JS-rendering still holds, and
`?export=xls` without a session returns the page shell rather than data. Two
things are now settled that were not before: the consensus CSV export needs
only a **free** account, and **per-expert rankings are a paid feature** — the
rankings nav reads "Pick Experts — Upgrade". So Kluge, Menton, Zacharison and
Barrett cannot be had individually without Premium ($6.99–$39.99/mo, MVP
$53.94 per six months). The consensus anchor is free and hand-exported; the
multi-expert ambition has a price on it. See `docs/backlog.md` `D6`/`D6b`.

**Question:** which of these are worth buying/exporting, and which do we
drop? My honest read: Ciely alone may be enough. He already matches our
league's exact settings, and a second source mainly helps as a sanity
check on outliers. **Subvertadown is the cheapest add** — it's free, and
copying one table gives us an independent second opinion.

---

## 2. Ciely's numbers land almost exactly on our league

His workbook's default Settings are our league, essentially line for line:
12 teams, 1QB/2RB/3WR/1TE/1FLEX/1DST, $200 budget, 0.5 PPR at every
position, 0.04 pass yards, 4 pass TD, 6 rush/rec TD, 0.1 rush/rec yards.
His auction values total **$2,400** — our exact pool.

**One mismatch: interceptions are −2 in his sheet, −1 in our league.**
Because the workbook carries raw stat projections, the extractor recomputes
points under our scoring instead of inheriting his. 67 QBs shift, mean
**+5.6 pts**, up to **+15.4** (Brock Purdy), **+15.2** (Geno Smith).

His DST scoring also differs from ours — he has no yardage-allowed tiers,
and his points-allowed buckets don't line up. Given DST is ~1% of spend in
our league, I left DST alone rather than modeling it.

**Question:** anything else in his Settings you'd want changed before we
lean on these numbers?

---

## 3. CORRECTED — our league is normal; Ciely is the outlier

**This supersedes the section below, which was written from Ciely alone.**
With Subvertadown ingested we now have two more independent references: `AAV`
(what humans actually pay in real drafts) and their VBD model. Both land on
our league, not on Ciely:

| Source | WR | RB | TE | QB |
|---|---|---|---|---|
| National AAV (real drafts) | 41.8% | 44.3% | 7.3% | 6.7% |
| Subvertadown VBD (BEER+) | 45.2% | 42.1% | 6.0% | 6.6% |
| **Hit or Miss 2025 actual** | **46.6%** | **40.3%** | **5.3%** | **6.7%** |
| Ciely model | 50.7% | **30.3%** | **13.9%** | 5.1% |

The earlier claim that this league "underpays TE by ~2.6× and overpays RB by
10 points of budget share" does not survive corroboration. Three of four
sources agree RB is worth ~40–44% and TE ~5–7%; **Ciely is alone** in saying
RB 30% / TE 14%.

Verified not a parsing artifact: he prices 29 TEs with positive value and has
George Kittle at **$17.50** where AAV says **$3** and Subvertadown VBD says
**$6**. His TE replacement baseline simply runs much deeper than everyone
else's.

**Practical effect:** do not chase a TE "market inefficiency" that only one
source believes in. Our league's positional spend looks efficient. This is
also independent support for treating Ciely's dollars as signal rather than
price.

## 3b. Superseded — the original single-source read

| Position | Ciely's model | Our league, 2025 actual |
|---|---|---|
| WR | 50.4% | 46.6% |
| **RB** | **30.1%** | **40.3%** |
| **TE** | **13.8%** | **5.3%** |
| QB | 5.1% | 6.7% |
| DST | 0.5% | 1.1% |

Two exploitable gaps, in opposite directions:

- **Our league underpays TE by roughly 2.6×** relative to what the model
  says the position is worth. Ciely's own advice reinforces it — he's
  "Ricky Bobby-ing" TE the same way he does QB.
- **Our league overpays RB by ~10 points of budget share.** But this
  collides with the Menton "league-winning RB" article and Ciely's own RB
  piece, which says to lock up backs **before the top-33 come off the
  board**.

Also worth noting: his most expensive player is **$47** (Jaxon
Smith-Njigba), while our league paid **$78 for Bijan** last year. Our price
curve is far steeper at the top than any VORP model justifies.

**Question:** do you want the board to show model value, our league's
market-adjusted value, or both side by side? I'd default to both — the gap
*is* the edge, and hiding it behind one blended number wastes it.

---

## 4. Name matching is at 99.8%, one name left

446 of 447 Ciely rows resolve to Sleeper IDs. Three nicknames are now in
`aliases.csv` (Kenneth→Kenny Gainwell, Mitch/Mitchell Tinsley, the
"Dermarcus"→Demarcus Robinson typo in his sheet).

**Unresolved: "Hollywood Brown" (WR, PHI).** ~~No Marquise Brown in Sleeper's
dictionary under either name.~~

**Corrected 2026-08-12:** he *is* in Sleeper — **Marquise Brown, id 5848, WR
PHI**, matching the source row's position and team exactly. `draftroom
sources -unmatched` finds him and prints the alias line. Adding
`Hollywood Brown,5848,ciely name for Marquise Brown` (and the subvertadown
equivalent) resolves the only unmatched row in either source. His Ciely
value is still $0.00, so it changes nothing on the board — but the reason
recorded here was wrong, not just stale.

---

## 5. RESOLVED — data moved to a private repo

All vendor data now lives in `../fantasy_sports_data`, a separate local git
repo with no remote. The public repo keeps only the extractors, `aliases.csv`,
`rulings.csv`, and these docs. Resolution order is `-data` →
`DRAFTROOM_DATA_DIR` → `../fantasy_sports_data` (anchored to the repo root,
not the working directory), and a missing directory errors with the exact
`git init` command rather than silently building an empty board.

Moved out: Ciely's workbook, the three Athletic PDFs, the FantasyPoints
article, the Subvertadown sheets and articles, and the Peaked cheat sheet.

---

## 6. Still outstanding

- **Mock draft `draft_id`** — the one open technical risk, unchanged. Start
  any free Sleeper mock, send the ID, and I can confirm whether picks are
  readable mid-draft.
- **League docs: DONE.** `scoring.md` now states half-PPR with −1 INT;
  `draft.md` documents the trade reset, the fourth-keep +$15, the FAAB
  no-carry rule, self-pickup churn, and that Sleeper enforces none of it;
  `rosters.md` notes the enforced position limits. The doc's keeper table and
  the code's ladder are locked together by test.

## 7. New, from the Subvertadown ingest

- **Peaked cheat sheet is full PPR.** The file provided is
  `1-QB-PPR-12-Team-peaked-base.webp` — PPR, not half-PPR, and it's an image
  rather than text. Want the half-PPR version, or drop Peaked?
- **`qbstream` sheets are redundant.** Verified identical to `stock` in 0 of
  218 rows across all three baselines; skipping them per your call.
- **Baseline spread is positional, and that's useful.** VOLS inflates elite
  RBs (Gibbs $73 → $91) while BEER+ inflates elite WRs (Chase $54 → $67). A
  player whose value swings hard across baselines is baseline-sensitive —
  a fragile buy — which is a risk signal worth surfacing on the board.
- **Six players are ECR-contested** (both arrows): Justin Jefferson, DJ Moore,
  Jadarian Price, George Kittle, Chig Okonkwo, Ricky Pearsall. Jefferson is
  the one that matters at $47 AAV.

---

## Strategy notes pulled from the three articles

Not questions, just what's worth encoding into the board later.

**Ciely on QB** — "Ricky Bobby-ing QB: if I'm not first, I'm last." Tier 1
(top 6) or wait to double-digit rounds. Tier 3 is flat enough that upside,
floor, and expectation converge, so there's no reason to pay there. If you
take a Tier 4 QB, pair him with a Tier 3 for two shots at a top-10 finish.
He likes Bo Nix, Jaxson Dart, and a healthy Daniel Jones as breakout
gambles. Jayden Daniels is his QB4.

**In auction terms for us:** this is a barbell. Either ~$32 for an elite
arm or $1–3 for two lottery tickets. Our league's history supports the
cheap end — QB was 6.7% of spend and topped out at $32.

**Ciely on RB** — target backs **before RB33 comes off the board**. After
that the WR/RB value gap widens, so every WR you take while opponents chase
RBs builds an edge. Replacement level in his 2026 numbers is **RB44 vs
WR57** — RBs run out sooner, which is what justifies paying up early.

**Hume on method** — rank position by *marginal advantage over the
replacement starter*, not raw points. Use "best worst" around 30 for RB/WR
rather than 24, to account for the flex.

**Menton (FantasyPoints) on RBs** — the Big 3 traits, re-weighted for our
half-PPR: receiving matters less than his full-PPR framing implies,
goal-line share matters more.

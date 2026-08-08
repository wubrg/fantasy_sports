---
title: "Data Sources for Positive EV Player Prop Research + The Free-Only Analytical Hobbyist Algorithm"
source_pdf: positive-ev-data-sources.pdf
source_url: https://gemini.google.com/share/ad9913351027
alt_url: https://share.gemini.google/3rtEwoqBMuiI
source_pages: all 45
created: 2025-11-13
captured: 2026-08-06
transcription: |
  Transcribed visually from a rasterized render; the PDF text layer is unusable.
  Wide tables are clipped at the right margin; clipped cells are marked.
status: ARCHIVE — source text with verification notes
---

# Researching Positive EV Player Props

> **Archive.** Source document, transcribed. **Methodologically the most rigorous of the four
> documents** — it is the only one that addresses de-vigging, mean-vs-median, calibration, and
> account-limitation risk together. It also contains, in its final five pages, a **live
> demonstration of the workflow that fails its own first filter**. See
> [Verification](#verification).

The document has two halves, from two separate prompts:

- **Part A (pp.2–17)** — a survey of professional (mostly paid) data sources and tools.
- **Part B (pp.18–45)** — a rewrite under hard constraints the user supplied: **NFL only, free
  data only, bonus bets plus max $25/month cash, $1–$5 wagers.** This is the operative half.

---

# Part A: An Analysis of Data Sources for +EV Player Prop Research

## I. Foundational Methodologies

**Positive Expected Value is a mathematical construct, not a predictive power.** A +EV wager is not
a "guaranteed win" but a bet that, if placed an infinite number of times under identical
conditions, would yield a net profit.

The core principle is identification of a mispriced line. The "edge" is the quantifiable gap
between the odds offered by a sportsbook and the "true" probability of that event occurring:

```
EV = (Probability of Winning × Payout) − (Probability of Losing × Stake)
```

The entire challenge is not "predicting who will win" but developing a superior process for
determining the **Probability of Winning**. There are only two ways to do this.

### Methodology 1: Market-Based ("Top-Down") Analysis

The "true" probability of an event is best determined by the efficient market itself — the "wisdom
of the crowd" of professional syndicates and market-maker sportsbooks.

- **Concept:** A small handful of "sharp" books (Pinnacle, Circa, Bookmaker) are the arbiters of
  truth — defined by high limits, rapid line movement in response to sharp action, and a business
  model that tolerates professional clientele. **The consensus, vig-free line from these market
  makers is considered the "true" price.**
- **Execution:** A +EV opportunity exists when a "soft" book (FanDuel, DraftKings) is slow to
  update, creating a temporary "outlier" price mispriced relative to sharp consensus.
- **The Causal Chain of Account Limitations:** Mathematically sound, but its execution is
  self-defeating at scale:
  1. Recreational books do not aim to have the sharpest line; their model manages a portfolio of
     *losing* customers.
  2. Market-based +EV tools instruct a user to *only* bet stale lines that favor the bettor.
  3. Risk-management algorithms are designed precisely to detect this pattern.
  4. Accounts that exclusively "pick off" stale lines are quickly flagged and "limited" — max wager
     reduced to trivial amounts (e.g., $5.00).
  5. Therefore this is a "high-burn" strategy, operationally unsustainable without a constant
     supply of fresh accounts.

### Methodology 2: Projection-Based ("Bottom-Up") Analysis

Rejects market consensus as the only source of truth.

- **Concept:** A research team, through proprietary data and superior modeling, can create a
  statistical projection of player performance *more accurate* than the one the sportsbook used.
- **Execution:** If the model projects a mean of 25.5 receiving yards and the line is 22.5, the
  perceived edge is the +EV.
- **The Path to Sustainability:** Far more sustainable. Because the bet is based on a *proprietary,
  private* model, it is not easily detectable by risk management — it looks like a unique,
  opinion-based wager rather than systematic scraping of public market errors. This is the strategy
  of choice for long-term professional syndicates.
- **Critical Nuance (Mean vs. Median):** A vital, expert-level distinction. **Sportsbook prop lines
  represent a *median* outcome** (the line where the event goes over or under 50% of the time).
  However, player statistical distributions are *not* normal; they are highly skewed by a few
  explosive, high-yardage games. **This means a player's *mean* performance is often significantly
  higher than their *median* performance.**
  - Simply betting "Over" because a mean projection is higher than the median line is a common and
    costly mistake.
  - The *only* correct way to compare a mean projection to a median line is through **simulation**.
    The best professional tools allow a user to input a mean projection and standard deviation,
    then simulate that player's performance 10,000 times to determine the *true* median outcome and
    thus the *true* win probability against the sportsbook's line.

> 💡 **This is the single most sophisticated point in the entire corpus**, and it is absent from
> `edge-of-vigor.md`, whose entire Profitability Matrix assumes you can simply supply a `p_true`.
> This section explains why obtaining that number is hard, and that the naive route is
> systematically biased toward the Over.

## II. Market-Based (+EV) Prop Betting Tools

### 1. OddsJam

- **Utility:** High-speed odds comparison and arbitrage platform, scanning a claimed 100+
  sportsbooks to identify "big line discrepancies." Explicitly compares soft books (FD, DK) to
  sharp books (Pinnacle). Its "Positive EV" feed provides real-time bets, calculated EV percentage,
  and a recommended bet size based on the **Kelly Criterion**. Also offers arbitrage, "middles," and
  bonus conversion tools.
- **Reliability:** The API is among the fastest and most reliable in the industry. Its ability to
  *find* discrepancies is not in question. However, its *methodology* for calculating final EV has
  been criticised: analyses suggest OddsJam may use a **"best case" scenario for vig removal, which
  can artificially inflate calculated EV.** Competitors use a "worst case" (more conservative)
  model, a more rigorous standard.
- **Confidence:** **High** in technical ability to identify discrepancies. **Moderate** in utility
  for a *sustainable* operation. The tool works *too well*: a user who "smashes" every bet on the
  +EV feed is engaging in the exact behaviour risk-management algorithms are built to detect. It is
  a "burn-and-churn" tool.

### 2. Unabated

- **Utility:** Professional-grade toolkit built by and for professional bettors. Its core feature is
  the **"Unabated Line"** — a finely tuned, **blended, vig-free consensus price** derived from
  multiple market-making books. This is methodologically superior to using a single book as "the
  truth." Extensive odds screens including player props, and best-in-class calculators for pricing
  alternate lines, derivatives, and in-game odds.
- **Reliability:** Maximal. Transparent about both its "top-down" and "bottom-up" methodologies.
  Crucially, its **"Prop Simulators"** are an industry-leading tool that bridges the two strategies
  — they correctly address the mean-vs-median problem by allowing a user to input a projection and
  simulate the distribution of outcomes 10,000 times, yielding a true median and win probability.
- **Confidence:** **Very High.** Designed for professionals, not casual gamblers.

### 3. Outlier.bet

- **Utility:** Hybrid tool combining a market-based +EV feed with a visually driven research
  platform. Scans thousands of player props on FD, DK and other recreational books. Its "Pro" plan
  provides a standard +EV feed; its primary strength is a research dashboard presenting historical
  performance trends, "hit rates" against prop lines, deep matchup data, and line movement charts.
- **Reliability:** Highly regarded for UI, data consolidation and speed. Aggregates from enterprise
  sources like **Sportradar** and **Rotowire**. Critiques focus on subscription price, not data
  quality.
- **Confidence:** **High.** An excellent all-in-one "command center."

### 4. OddsShopper (from Stokastic)

- **Utility:** The market-based ("top-down") tool from Stokastic, a company renowned for
  projection-based models. Classic "scraper" model. Its key concept is **"Portfolio EV"** — a
  branded term for the sound principle of placing a high volume of small, diversified +EV bets to
  leverage the Law of Large Numbers.
- **Reliability:** Backed by Stokastic's data science infrastructure. Reported as a highly effective
  and in some cases more affordable alternative to OddsJam.
- **Confidence:** **High.**

## III. Projection-Based (Modeling) Data & Research Sources

### Tier 1: Specialized Analytics Providers (The "Alpha")

**5. Pro Football Focus (PFF)** — Employs analysts to manually review game film for *every player on
every play*, generating proprietary predictive metrics unavailable elsewhere: "PFF passing grade vs.
blitz," "pressure-to-sack rate," "yards per route run," "adjusted completion percentage." Packaged
into a "Player Prop Tool" comparing internal projections against book odds. **Reliability: Maximal**
— its primary clientele consists of NFL teams and agents. **Confidence: Maximal.** Not a "source"
but an essential building block.

**6. FTN Fantasy (Home of DVOA)** — New home of Football Outsiders and its signature metric, DVOA
(Defense-adjusted Value Over Average), a bottom-up metric analyzing every play for "success" based
on situational context (down, distance, opponent quality). Allows granular matchup analysis far
beyond team ranks — "DVOA vs. WR1s," "DVOA vs. deep passes," "DVOA vs. slot receivers."
**Reliability:** One of the most respected, academically vetted advanced metrics in football;
independent accuracy tracking has ranked FTN's fantasy projections #1 in recent years.
**Confidence: Very High.** *If PFF tells you how good a player is, DVOA tells you how effective he
will be in his specific matchup.*

### Tier 2: Historical Data & Trend Analysis (The "Database")

**7. Props.Cash** — Not a pick service or projection model; a high-speed research database. Core
function is instantly visualizing a player's historical "hit rate" against any given prop line
(e.g., "how many times has Player X gone OVER 25.5 Points + Rebounds in his last 20 games"),
filterable by road games, games without Player Y, etc. Saves hundreds of hours of manual database
queries. **Confidence: Very High** — essential for idea generation and model validation.

**8. Sports Reference (Pro-Football-Reference et al.)** — The foundational "gold standard" library
for historical sports data. Decades of box scores, play-by-play, and advanced analytics that serve
as the raw material for any bottom-up strategy. **Confidence: Maximal.** *A quantitative research
team not using Sports Reference (or an enterprise equivalent like Sportradar) is not a serious team.*

### Tier 3: DFS-Centric Projection Services (The "Model Sellers")

**9. Stokastic (formerly Awesemo)** — One of the world's most successful DFS projection providers.
Its "Bet Pro" tool compares the Stokastic projection (e.g., 8.1 rebounds) to the sportsbook line
(e.g., 7.5) and calculates an "xWin" (Expected Win %) and EV. **Confidence: Very High** — the purest
direct application of the projection-based methodology, and a *non-detectable* strategy that avoids
account limitation.

**10. RotoWire** — Long-standing, highly respected source for fantasy news, analysis, and
projections, applied to the betting market. Also a primary source of *fast, accurate* player news
(injuries, lineup changes), which is a source of +EV in itself for those who can act before lines
move. Projections are sourced by major platforms (e.g., Sleeper). **Confidence: High.**

**11. BettingPros / FantasyPros** — Primary value is *consensus*: FantasyPros aggregates projections
and rankings of over 150 experts, and the "Prop Bet Analyzer" compares that consensus projection to
sportsbook lines. **Reliability requires a nuanced assessment:**
- *Consensus Data:* **High.** An Expert Consensus Ranking (ECR) is a statistically robust metric
  that filters out individual biases.
- *"PRO Systems":* **Very Low.** A public support article from April 2025 *admits* that a data
  update "caused some win rates to change from positive to negative" for approximately **13%** of
  their systems. This is a catastrophic admission of data integrity failure.
- **Confidence: Moderate (Conditional).** High confidence in the consensus projections as model
  input; **no confidence** in their "PRO" betting systems.

**12. Action Network** — Media-centric hub for odds comparison, bet tracking, news and picks.
**Reliability: Extremely Low — the most significant negative finding of the report.** Two factors:
1. **Conflicting Business Model:** The CEO is on public record stating their "billion-dollar
   business" comes from *affiliate fees* paid by sportsbooks for new signups. Sportsbooks only pay
   these fees for long-term *losing* customers. The company's primary financial incentive is to
   refer losing players, not create winning ones.
2. **Data Integrity Issues:** Public complaints allege "losses magically disappear from a system's
   history" and that "unverified bets" are used as a "dumping ground for loses [sic]" to keep
   public-facing records clean.

**Confidence: None.** Should be viewed as a media and affiliate company, not a quantitative analysis
firm. Its odds screen and bet tracker are functional; **its "picks" should be strictly avoided.**

## IV. Strategic Synthesis (Part A)

### Table 1: Source Assessment Matrix

| Source | Methodology | Primary Use Case |
|---|---|---|
| OddsJam | Market-Based (Top-Down) | Real-time market discrepancy alerts |
| Unabated | Hybrid (Market & Projection) | Market-line origination; Projection simulation |
| Outlier.bet | Hybrid (Market & Projection) | All-in-one research environment; +EV alerts |
| OddsShopper | Market-Based (Top-Down) | High-volume market discrepancy alerts |
| PFF | Projection-Based (Bottom-Up) | "Alpha" data input for NFL projection models |
| FTN Fantasy (DVOA) | Projection-Based (Bottom-Up) | "Alpha" data input for NFL matchup analysis |
| Props.Cash | Projection-Based (Bottom-Up) | High-speed historical trend & "hit rate" research |
| Sports Reference | Projection-Based (Bottom-Up) | Foundational database for building models |
| Stokastic | Projection-Based (Bottom-Up) | "Bottom-up" +EV picks; Model validation |
| RotoWire | Projection-Based (Bottom-Up) | "Bottom-up" +EV picks; Model validation; News |
| BettingPros | Projection-Based (Bottom-Up) | Consensus projection data for model input |
| Action Network | (Media / Affiliate) | Odds screen; Bet tracking (**not for picks**) |

*(A "FD/DK coverage" column is clipped at the right margin; every visible value is "Yes".)*

### Recommended "Tool Stacks"

**Stack 1: The "Market-Based" Offensive (High-Burn, Fast-Profit).** *Not sustainable; requires a
constant supply of new, non-limited accounts.* Unabated (to establish the true vig-free consensus
price) + OddsJam (high-speed alerting on FD/DK deviations) + Outlier.bet (boosts feed, arbitrage).

**Stack 2: The "Projection-Based" Syndicate (Sustainable, Long-Term).** **This is the recommended
path.**
- *Step 1 (Model Inputs):* PFF (player grades), FTN Fantasy DVOA (matchup grades), Sports Reference
  (historical).
- *Step 2 (Model Validation):* Stokastic Bet Pro (benchmark), RotoWire (second stabilizing
  benchmark).
- *Step 3 (Research & Execution):* Props.Cash (trend research) + **Unabated Prop Simulator** (input
  the model's *mean* projection to generate a *true win probability* against any FD/DK median line,
  solving the mean-vs-median problem).

### Analyst's Final Word

+EV player props are a demonstrably exploitable market. The primary challenge is not *finding* value
but *extracting* it at scale without being banned.

Reliance on the top-down strategy is a race to the bottom that will result in a cascade of account
limitations. Any serious operation must adopt a bottom-up strategy as its primary objective.
Market-based tools should be used sparingly and primarily for *information* ("where is the market
moving?") rather than as bet-this-now signals. **The future of professional sports betting is not in
*scraping* the market but in *beating* it with a superior predictive model.**

---

# Part B: A Vetting of Freely Available Data Sources for NFL Player Prop Analysis

> *Prompt: "I do not intend to scale +EV betting to extreme levels. I intend to limit my betting to
> bonus bets and a max of $25 a month with wagers in the $1–$5 range. I also want to exclusively bet
> in the NFL market…"*

## Part 1: The Analytical Hobbyist's Framework

### Section 1.1: Strategic Imperatives

1. **Market Focus:** Exclusively the NFL player prop market.
2. **Budgetary Constraint:** Promotional bonus bets plus a maximum cash outlay of **$25.00 per
   month**.
3. **Wager Sizing:** All wagers in the **$1.00–$5.00** range.
4. **Data Mandate:** The entire workflow must be built using **only freely available data sources**.
   No paid subscriptions, "pro" memberships, or time-limited free trials.

The primary objective is not high-volume profit generation, but the high-efficiency conversion of
bonus bets and the intellectual application of a data-driven +EV process.

### Section 1.2: The "Limit-Avoidance" Insight — Why Low Stakes Are a Strategic Advantage

The user's $1–$5 constraint is, paradoxically, **the single greatest strategic advantage** for
long-term sustainability.

Sportsbooks are risk-management firms. An account that consistently identifies +EV opportunities or
efficiently exploits promotions is flagged not as a "customer" but as a "liability," and the response
is swift: dynamically adjusted betting limits ("limited to pennies per bet"), promotional bans, or
outright account suspension.

The user's stated goal is the *exact* behaviour profile sportsbooks are algorithmically designed to
identify and eliminate. **However, the $1–$5 wager constraint functions as a built-in "cloaking
device."** Risk-management algorithms flag users based on financial *exposure*. A bettor who exploits
a $500 mispriced prop is a clear and present danger. A bettor who exploits the same line for $2.00 is
not. The cost of identifying, tracking and limiting such a micro-stakes player far exceeds any
potential loss they could inflict.

**Therefore the micro-budget is not a limitation on this strategy but its central enabler.**

### Section 1.3: The "Freemium Funnel" and the "Data Arbitrage" Strategy

The business model of virtually every major data provider is identical: **free bait** (live scores,
basic odds display, news) and a **paywalled edge** (proprietary projections, prop value analyzers,
line-history trackers, +EV screeners).

- **Action Network:** "Player Prop projections and values" are behind the PRO subscription. The
  "Edge %" shown on free pages is marketing for that paywalled data.
- **OddsJam:** The entire Positive EV tool is the core premium product.
- **Unabated:** The "Unabated Line" (vig-free consensus) and "Props+" are premium.
- **BettingPros:** "Prop Bet Analyzer" advanced filters marked with a "P" icon require premium.

**A paid +EV tool is not magic. It is a simple algorithm: it compares a Proprietary Statistical
Projection against the Live Market Line and flags significant discrepancies.**

Since the free-only constraint bars every integrated tool, the strategy is to **manually replicate
that function** by building a "data supply chain" — sourcing each component of the +EV algorithm from
disparate industries that monetize data differently:

1. **Source the "Line" (Part 2):** the free odds-viewing tier of the *betting tools* industry.
2. **Source the "Projection" (Part 3):** free, ad-supported data from the *fantasy sports* industry.
3. **Source the "Vetting Data" (Part 4):** free archival data from *media & reference* (PFR) and free
   analytical metrics from fantasy content arms (FantasyLife, FTN).
4. **Source the "Clearance" (Part 5):** free real-time feeds from *news & media* (RotoBaller, X).

## Part 2: Sourcing the "Line" — Free Odds Comparison Tools

**A critical, foundational rule: systematically ignore all "picks," "projections," "edges," "grades"
or "values" displayed within these applications.** They are marketing tools, not free data. Any
projection offered for free by a paywalled service is a teaser — derived from a stale or incomplete
model, or intentionally provided to lure users into the premium funnel. **Use these tools *only* as a
"dumb" odds screen.**

| Source | URL | Free Prop Odds? | Free Prop Market Coverage | Paywalled Features to *Ignore* | Verdict |
|---|---|---|---|---|---|
| **The Action Network** | `actionnetwork.com/nfl/props` | Yes | Excellent. Main lines + many alt lines. | "PRO" Picks, "Edge %", "Player Prop projections and values" | **Primary Recommendation.** Most comprehensive free tool for manual line shopping. |
| **BettingPros** | `bettingpros.com/nfl/props/` | Yes | Very Good. Main prop lines. | "Top-Rated Bets", all features marked with a "P" icon | **Secondary Recommendation.** Strong full-featured alternative. |
| **Unabated** | `unabated.com/nfl/odds` | Yes | Good. Main prop lines. | "Unabated Line", "Props+", "Premium", Line History | **Viable Alternative.** Cleaner professional tool; free tier sufficient for viewing. |
| **RotoWire** | `rotowire.com/betting/nfl/player-props.php` | Yes | Good. Main prop lines. | All "Picks", "Projections", subscription-locked tools | **Viable Alternative.** A simple, no-frills odds viewer. |

## Part 3: Sourcing "Value" — Free Weekly Player Projections

### Section 3.1: The "Hobbyist Consensus"

No single free projection source is authoritative. **A projection from a single site is just one
opinion and is susceptible to model error or stale data.** However, multiple independent, high-quality
free sources exist.

By aggregating them we create a **"Hobbyist Consensus"** projection — *the simple average of 3–4 free,
independent projections* — which serves as *our* "true line."

The entire workflow from this point forward is predicated on this step. A "high-confidence search" is
triggered *only* when a significant discrepancy exists between **the Market Line** (Part 2) and **our
Hobbyist Consensus** (Part 3).

### Section 3.2: Primary Sources

- **Source 1: FantasyPros Consensus — Cornerstone Source.** The entire business model is aggregating
  fantasy advice; free "Projections" pages "combine projections from all major fantasy football sites
  into a consensus." QB pages list consensus "Pass Completions," "Att," "Yds," "TDs," "INTs," plus
  "Rushing: Att; Yds; TDs" — mapping one-to-one with the most popular prop markets.
- **Source 2: ESPN Fantasy — Primary Source.** Free in-house weekly projections for all players; a
  strong independent check and balance against FantasyPros.
- **Source 3: FanDuel Research / numberFire — Primary Source.** FanDuel's content arm publishes free
  DFS projections powered by numberFire, FanDuel's proprietary quantitative engine.
  - **The "In-House Arbitrage" Signal:** We can source a projection from FanDuel Research and compare
    it directly to the prop line offered by FanDuel Sportsbook. The risk/trading team setting the line
    and the content/data team publishing the projection are separate, but part of the same parent
    company. A significant public-facing discrepancy between their two numbers (e.g., Research
    projecting Lamar Jackson for 51.37 rushing yards while the Sportsbook line is O/U 42.5) is an
    extremely high-confidence signal. *We are leveraging one arm of the company against the other
    arm's market line.*
- **Source 4: "Harvested" Projections from Media Picks — Supplemental.** We will not trust a media
  outlet's *pick*, but we can trust one that "shows its work" by publishing the underlying projection
  number.
  - *CBSSports:* "D. Achane. Under 16.5. −122. Carries. **PROJ: 14.5**" — we harvest "14.5".
  - *Covers.com:* "Justin Fields Over 136.5 passing yards (−110). **Projection: 183.73 yards**" — we
    harvest "183.73".
  - *FanDuel Research Articles:* "FanDuel's NFL player projections forecast Dobbins to tally 78.3
    rushing yards in this game" — we harvest "78.3".

## Part 4: Contextual Vetting — Free Advanced Stats & Matchup Analysis

### Section 4.1: Why a "Gap" Isn't Enough

Identifying a statistical gap is only the *first step*. **A gap is a signal, not a conclusion.** It can
exist for two reasons:

1. **A Market Inefficiency (Our Target):** the line is stale or mispriced, and our projection is
   correct.
2. **A Contextual Variable (Our Trap):** the line is *sharp* and our *projection* is stale or wrong,
   because the projection models failed to account for a critical contextual variable (a terrible
   defensive matchup, a declining role, a surprise injury).

### Section 4.2: Historical Player Performance (Game Logs)

**Pro-Football-Reference.** Stathead is paywalled, but individual player pages and "gamelogs" are
completely free and sufficient. Workflow: identify a gap → open the PFR player page → "Gamelogs" →
manually check **Recent Form / Hit Rate** (how many times in the last 5, 8 or 10 games has he exceeded
the line?), **Historical Matchup** (did he play this opponent last year?), and **Splits** (home vs.
away). **Verdict: Primary Vetting Tool** — non-negotiable.

### Section 4.3: Matchup & Defensive Analysis (Defense vs. Position)

- **Source 1: NFL.com (Official) — Primary Recommendation.** The official "Fantasy Points Against"
  tool shows the fantasy points each team allows to each position, filterable by position (QB/RB/WR/TE)
  and timeframe. **Filtering by "Last 4 WKS" is more valuable than season-long data** because it
  reflects current form. Includes raw stats allowed.
- **Source 2: CBSSports — Secondary Recommendation.** "Fantasy Football Stats – Position vs. Defense,"
  ranked 1–32 with fantasy points and raw statistics.
- **Source 3: DraftEdge — Viable Alternative.** Free "Defense vs. Pos" table with points allowed and
  "vsAvg."
- **Discarded:** *Fantasy Alarm* (marketing heavily integrated with the paid "All-Pro" subscription;
  unclear whether truly free) and *RotoWire* (subscription-first; unclear whether the DvP tool is fully
  free).

**Workflow:** *Validation* — opponent ranked #31 against RBs, allowing 135 rushing yards over the last
4 weeks → validates the Over. *Invalidation* — opponent ranked #1 against RBs → this is the likely
*reason* for the gap; the line is sharp and our projection is stale. **We do not make this bet.**

### Section 4.4: Advanced Metrics & Player Role

- **Source 1: FantasyLife Utilization Report — Primary Vetting Tool.** Free under "NFL Stats & Info":
  NFL Inactives, Player Stats, **Air Yards**, and **Snap Counts**. Tracks a player's *role*
  week-to-week — are they on the field (Snap Share), and are they getting the ball (Target Share, Air
  Yards Share)?
  - **The "Utilization" Causal Chain:** *A prop bet is a bet on opportunity.* A player's utilization
    trend is a **leading** indicator of future production, whereas a box score is a **lagging**
    indicator.
  - *Validation:* Snap Share and Target Share have increased for two consecutive weeks (e.g., another
    WR was injured) → the line may be stale, based on his older, smaller role → validates the Over.
  - *Invalidation:* Snap Share is decreasing and a teammate's Air Yards are increasing → the
    *projection* is stale, based on his old, larger role → invalidates the Over.
- **Source 2: FTN Fantasy DVOA — Primary Advanced Vetting Tool.** Most historical and granular DVOA
  tools are paywalled, but FTN lists **"Team Total DVOA" free**, providing current rank for every
  team's TOT / Off / Def DVOA. *Validation example:* the QB's Off DVOA is ranked #4 and the opponent's
  Def DVOA #28 → a massive efficiency mismatch.
- **Source 3: NFL Next Gen Stats (Official) — Supplemental.** The NFL's official player-tracking data
  (RFID tags in shoulder pads and the ball), free to fans. Includes **"Expected Rushing Yards" (XRY)**
  and **"Completion Percentage Over Expectation" (CPOE)**. If a back's actual rushing yards are
  consistently *below* his Expected Rushing Yards, he may be due for positive regression — his
  opportunities are better than his recent box scores suggest.

## Part 5: Real-Time Intelligence — The Free Breaking News Feed

All analysis in Parts 3 and 4 is pre-game. **The entire construct can be rendered useless by a single
piece of information 90 minutes before kickoff.** Therefore the *final step* of any high-confidence
search is a **Real-Time Clearance Check**.

| Tier | Source | Platform | Update Speed | Best For… |
|---|---|---|---|---|
| **Tier 1** | Adam Schefter (@AdamSchefter) | X / Twitter | Instant | Breaking news (trades, signings) & 90-min inactives |
| **Tier 1** | Ian Rapoport (@RapSheet) | X / Twitter | Instant | Breaking news (injuries, contracts) & 90-min inactives |
| **Tier 1** | @Underdog__NFL | X / Twitter | Instant | Raw, filter-free news alerts (e.g., "Player X is practicing") |
| **Tier 2** | RotoBaller | App / Website | Near real-time | **100% free** push alerts, injury updates, news blurbs |
| **Tier 2** | RotoWire | Website | Near real-time | Comprehensive feed of player news, practice reports, transactions |
| **Tier 3** | Local Beat Writers | X / Twitter | Near real-time | Nuanced "on the ground" observations (e.g., practice performance) |

RotoBaller won the FSWA award for "Best Player News Notes," and critically, "all of our news and
alerts are completely free." Local beat writers (e.g., @JourdanRodrigue for the Rams, @jeffzrebiec for
the Ravens) are optional vetting — not required for a basic Active/Inactive clearance check, but a
source of sharper information.

## Part 6: Final Synthesis — The "Analytical Hobbyist" Algorithm

### Section 6.1: The Four-Filter Workflow

Paid +EV tools are simply black-box algorithms that compare a projection to a line and flag the
difference. The free-only constraint requires deconstructing that black box and running the algorithm
manually.

**A prop must pass *all four* filters. Failure at any step disqualifies the prop.**

1. **Filter 1: The Discrepancy Filter** (Projection vs. Line)
2. **Filter 2: The Context Filter** (Historical & Matchup)
3. **Filter 3: The Opportunity Filter** (Role & Efficiency)
4. **Filter 4: The Real-Time Filter** (News & Inactives)

### Section 6.2: The Step-by-Step Search Algorithm

**Step 1 — Identify Projection-Line Discrepancies (Filter 1).** Open a market line source (Action
Network) and the Hobbyist Consensus sources (FantasyPros, FanDuel Research). Manually scan the main NFL
prop markets (Passing Yds, Rushing Yds, Receiving Yds, Receptions) for the largest gaps.
- *Example Target:* Player A, Receiving Yards
- *Market Line (FanDuel):* O/U 52.5 yards
- *Hobbyist Consensus Projection:* 64.1 yards (avg. of FantasyPros [63.5], ESPN [62.0], FanDuel
  Research [66.7])
- *Status:* A significant gap (11.6 yards) is found. **Proceed to Step 2.**

**Step 2 — Contextual Vetting (Filter 2).** *Historical:* PFR gamelogs — pass if he has exceeded 52.5
in 4 of his last 5 games; fail if he has not exceeded it in 6 straight (the projection is likely
wrong). *Matchup:* NFL.com "Fantasy Points Against," filter WR / Last 4 WKS — pass if the opponent is
#29 vs WRs allowing 190.4 yards; fail if #1 (the sportsbook is correct; the bet is invalidated).

**Step 3 — Advanced Vetting (Filter 3).** *Opportunity:* FantasyLife Utilization Report — pass if Snap
Share has increased three straight weeks (60% → 85%) and he dominates team Air Yards; fail if Snap
Share has decreased two straight weeks. *Efficiency:* FTN free DVOA — pass if the player's team Off
DVOA is #5 and the opponent's Def DVOA is #27.

**Step 4 — Real-Time Clearance (Filter 4).** Immediately before the wager: check RotoBaller and
RotoWire for the player *and his quarterback*; check @AdamSchefter and @RapSheet for the official
90-minute inactive reports; check game-day weather (high winds, snow, heavy rain invalidate any
passing/receiving prop). *Fail example:* Player A is active, but his starting QB has been ruled out —
this invalidates the entire analysis; the bet is aborted.

### Section 6.3: Final Report Format

> **High-Confidence Search Result**
> - **Prop:** Player A, Over 52.5 Receiving Yards
> - **Line:** −114 (FanDuel)
> - **Hobbyist Consensus Projection:** 64.1 Yards
> - **Justification:**
>   - *Projection Gap:* free consensus model (avg. of FantasyPros, ESPN, FanDuel Research) projects a
>     significant 11.6-yard gap over the market line.
>   - *Historical Context:* exceeded this 52.5-yard line in 4 of his last 5 games (80% hit rate).
>   - *Matchup:* facing the #29-ranked defense versus wide receivers (4th-most yards allowed) over the
>     last 4 weeks.
>   - *Player Role:* validated by a positive trend in on-field opportunity — snap share up from 60% to
>     85% over three weeks, clear #1 in team Air Yards.
>   - *Status:* player and starting QB both Active. Weather not a factor (Dome).

---

## Appendix: The live demonstration (pp.41–45)

> *Prompt: "Okay let's test it out for tonight's game between the Patriots and the Jets. Find me the
> best three plus EV props for that game."*

**1. Mack Hollins (NE) — Over 50+ Receiving Yards (+142)**
- *Hobbyist Consensus Projection:* "While a specific 'Hobbyist Consensus' number **isn't available**,
  this prop is flagged as a high-confidence 'In-House Arbitrage' signal, as it is a top recommended
  prop from FanDuel's own research arm."
- *Filter 1 (Discrepancy):* "This qualifies as a high-confidence signal, as FanDuel's own analysis team
  is highlighting this alternate line."
- *Filter 2 (Matchup):* Jets secondary "has struggled all season," operating without Sauce Gardner.
- *Filter 3 (Opportunity):* Kayshon Boutte out for a second straight game; in Week 10 with Boutte out,
  Hollins led the Patriots with 10 targets and 106 receiving yards; 60% of snaps in four straight games.
- *Filter 4 (Real-Time):* Boutte confirmed out.

**2. Justin Fields (NYJ) — Over 136.5 Passing Yards (−110)**
- *Hobbyist Consensus Projection:* 183.73 Yards.
- *Filter 1:* "a massive 47-yard gap between the market line (136.5) and our free projection source
  (183.73)."
- *Filter 2:* Patriots defense excels at stopping the run, the Jets' primary strength; combined with a
  high likelihood of the Jets "trailing by a lot," creates a "pass-happy (and garbage time) script."
- *Filter 3:* the low line is likely due to top WR Garrett Wilson being out; the projection model
  accounts for this, anticipating high volume of inefficient passes.
- *Filter 4:* Garrett Wilson confirmed out.

**3. Tyler Johnson (NYJ) — Under 20.5 Receiving Yards (−114)**
- *Hobbyist Consensus Projection:* "This is another 'In-House Arbitrage' signal. FanDuel's own research
  team is recommending the Under."
- *Filter 1:* "a high-confidence signal based on an 'in-house' recommendation to bet the Under."
- *Filters 2 & 3:* a sharp fade based on faulty historical context — Johnson's 60+ yard games when
  Wilson missed time earlier in the year are "a trap," since the Jets have since acquired John Metchie
  III and Adonai Mitchell who, with Allen Lazard, will prevent Johnson from seeing enough targets.
- *Filter 4:* based on the confirmed news that Garrett Wilson is out.

---

## Verification

### What this document gets right

It is the most methodologically serious of the four, and it independently supplies several things
missing from `edge-of-vigor.md`:

- **De-vigging is named and treated as central** — the "Unabated Line" is repeatedly described as a
  "blended, vig-free consensus price," and OddsJam is criticised specifically for a *"best case"
  vig-removal model that can artificially inflate calculated EV.*
- **Mean vs. median.** Prop lines are medians; player distributions are right-skewed, so the mean sits
  above the median. Betting Over because a mean projection exceeds the line is "a common and costly
  mistake." The only correct conversion is simulation. **Nothing in the other documents comes close to
  this.**
- **Calibration.** "A projection from a single site is just one opinion and is susceptible to model
  error or stale data" — hence the 3–4 source Hobbyist Consensus.
- **A gap is a signal, not a conclusion**, with an explicit trap case (the line is sharp, *your*
  projection is stale). This is the discipline `edge-of-vigor.md` lacks entirely.
- **Source integrity assessed independently of utility** — Action Network's affiliate-fee conflict of
  interest and BettingPros' own admission that 13% of its systems flipped from positive to negative
  win rates.
- **The limit-avoidance insight.** Micro-stakes as structural defense rather than limitation is a
  genuinely good observation, and it is the reason this framework is viable at all for the stated
  constraints.

### 🛑 The live demonstration fails its own Filter 1

The final five pages are the only place in the corpus where the methodology is actually *executed*, and
it is the most important evidence in these documents:

**Two of the three recommended props have no projection at all.**

- *Mack Hollins:* "a specific 'Hobbyist Consensus' number **isn't available**." Filter 1 is then marked
  as passing on the grounds that "FanDuel's own analysis team is highlighting this alternate line."
- *Tyler Johnson:* "another 'In-House Arbitrage' signal… FanDuel's own research team is recommending
  the Under."

Filter 1 is the **Discrepancy Filter** — the entire quantitative basis of the method. In both cases no
projection was obtained, no gap was computed, and no EV was calculated. *A recommendation from a
sportsbook's content arm was substituted for the arithmetic*, and the prop was still labelled
"high-confidence." The document states that a prop must pass **all four** filters and that "failure at
any step will disqualify the prop." Two of three should have been disqualified at step one.

**The third prop uses n=1, not a consensus.** Justin Fields' 183.73 is the Covers.com figure harvested
in Part 3 — a single source. Part 3 explicitly requires "the simple average of 3–4 free, independent
projections" and warns that a single site "is just one opinion." A 47-yard gap on a passing prop is
also exactly the magnitude that should have triggered the Filter 2 trap check ("the line is sharp and
*our projection* is stale"); instead it was reported as the strength of the play. A gap that large far
more likely reflects a projection generated before Garrett Wilson was ruled out than a 47-yard market
error.

**Why this matters more than the arithmetic errors.** `edge-of-vigor.md` and `analytical-hobbyist.md`
contain errors of *calculation*. This is an error of *process*: a well-specified, genuinely rigorous
four-filter algorithm was handed to a model, and the model produced confident, structured,
citation-bearing output while silently skipping the one step that made it quantitative. Every heading
was present. Every filter was marked as passing. The math was simply absent.

This is the failure the CLI is meant to make impossible, demonstrated on the user's own slate: the
model must never be the thing that decides whether a discrepancy exists. Filter 1 has to be computed
and supplied, or the prop does not appear in the report at all.

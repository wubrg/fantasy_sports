---
title: "An Analytical Framework for Optimal Strategy: Extracting Value from FanDuel & DraftKings Bonus Bets"
source_pdf: analytical-hobbyist.pdf
source_url: https://gemini.google.com/share/e59c2d8c46d5
alt_url: https://share.gemini.google/nyAPhdQNS6b1
source_pages: all 20
created: 2025-11-14
captured: 2026-08-06
transcription: |
  Transcribed visually from a rasterized render; the PDF text layer is unusable.
status: ARCHIVE — source text with verification notes
---

# Betting Strategy for Bonus Bets

> **Archive.** Source document, transcribed. Arithmetic independently verified — see
> [Verification](#verification). The **conceptual framework is the strongest of the three
> documents**; it is the only one that addresses variance, portfolio construction, and
> operational risk. **Three numeric errors were found**, flagged inline. One of them labels a
> $127.55 loss as an $872.45 profit.

> *Prompt: "I want to understand the optimal strategy for an analytical hobbyist (from personal
> context) as it relates to betting with bonus bets on fan duel and draft Kings."*

## Report Summary

This report provides a quantitative analysis of optimal strategies for an analytical hobbyist
managing "bonus bets" from FanDuel and DraftKings. The analysis establishes that all "bonus
bets" offered by these platforms are fundamentally **Stake Not Returned (SNR)** instruments.
This core mechanic — a winning wager returns only the profit and not the original stake —
fundamentally alters all standard Expected Value (EV) calculations.

This report will mathematically demonstrate that the *optimal* strategy for an SNR bet, in terms
of maximizing theoretical expected value, is to place it on a significant longshot (underdog).
Two operationally distinct strategic frameworks are detailed:

1. **The High-Variance (+EV) Strategy:** A long-term, portfolio-based approach. It embraces the
   high variance of longshot wagers to maximize theoretical EV over a large sample of bets.
2. **The Low-Variance (Conversion) Strategy:** A "matched betting" or "bonus arbitrage" approach
   that eliminates variance. It uses cash hedges on different sportsbooks to lock in an
   immediate, guaranteed, and risk-free profit, **typically converting 60% to 80% of the bonus
   bet's face value into withdrawable cash.**

The report provides a tactical playbook for both, with specific protocols for "No Sweat Bets" and
"Bet & Get" offers. It provides a rigorous analysis of the critical, strategy-altering rule
differences between FanDuel and DraftKings, particularly their divergent policies on "pushes"
(ties) and the ability to split bonus wagers.

Finally, it delivers a framework for long-term operational viability, addressing the primary risk
of any advantage-play strategy: sportsbook account limitations and "promo bans." A methodology of
"mug betting" is presented as a necessary, quantifiable "cost of doing business." The ultimate
recommendation is a hybrid, phased strategy: utilizing risk-free *conversion* to build a
dedicated bankroll, then deploying that capital into a long-term, high-variance *+EV betting*
portfolio.

## Introduction: Deconstructing the "Bonus Bet" as a Quantitative Instrument

### The Core Mechanic: A "Bonus Bet" is a "Stake Not Returned" (SNR) Instrument

To develop an optimal strategy, one must first discard the marketing terminology of "bonus bet"
and adopt the precise financial definition. The bonus bets offered by both FanDuel and DraftKings
are functionally "Stake Not Returned" (SNR) instruments.

Both sportsbooks explicitly confirm this mechanic. FanDuel's terms state: *"you'll get to pocket
all of the winnings but the initial Bonus Bet portion of your wager (your stake) will not be
returned to your wallet"*. DraftKings' terms concur: *"the bonus bet stake never comes back to
you… If you drop a $50 Bonus Bet on a +200 play and win, your return is $100 in profit, not
$150"*.

This SNR mechanic must be rigorously differentiated from **"Site Credit,"** which both platforms
also offer. With site credit, *"the stake will also be returned to you if your bet wins"*. Site
credit is therefore functionally identical to cash. A bonus bet is not.

This distinction is the absolute bedrock of all subsequent strategy. The user has not been given
"$150 in bets"; they have been given a *coupon* (or, in the case of many DraftKings offers, a set
of 12 distinct $25 coupons) that can be redeemed for the *profit* of a single wager. This
reframes the problem from "How do I win this free bet?" to **"What is the mathematically optimal
method for maximizing the cash-equivalent value of this SNR coupon?"**

### The Analytical Shift: How SNR Mechanics Transform Expected Value (EV)

For a normal, stake-returned cash wager, the EV formula is:

```
EV = (P_win × Profit) − (P_loss × Stake)
```

Where `P_win` is the probability of winning, `P_loss` is the probability of losing, `Profit` is
the payout on a win, and `Stake` is the cash amount risked.

With an SNR bonus bet, the user's own cash `Stake` is **0**. The "loss" is the promotional
instrument itself, which has no cash value and disappears if the bet loses. The formula therefore
becomes:

```
EV = (P_win × Profit) − (P_loss × $0)

EV = P_win × Profit
```

This simple change has profound, non-linear consequences.

**Scenario: A $100 SNR Bonus Bet**

- **Example 1: Heavy Favorite**
  - Odds: −1000 (Implied `P_win` ≈ 90.9%)
  - Profit on Win: $10
  - **EV:** `0.909 × $10 = $9.09`
- **Example 2: Even Money**
  - Odds: +100 (Implied `P_win` = 50.0%)
  - Profit on Win: $100
  - **EV:** `0.500 × $100 = $50.00`
- **Example 3: Heavy Underdog (Longshot)**
  - Odds: +1000 (Implied `P_win` ≈ 9.09%)
  - Profit on Win: $1,000
  - **EV:** `0.0909 × $1,000 = $90.90`

✅ All three verified correct.

The conclusion is mathematically unavoidable: the expected value of an SNR bonus bet is *not*
static; it increases directly with the odds of the wager.

This creates a **mathematical imperative to bet on longshots**. The traditional betting goal of
"finding a safe winner" is the *least* optimal strategy for an SNR bet. A $100 bonus bet used on
a −250 favorite, for example, has an EV of only **$30.08**. This inefficient allocation
effectively "wastes" more than 60% of its potential theoretical value compared to a +1000
longshot.

> 🛑 **ERROR 1.** The −250 figure is wrong. Implied probability at −250 is 71.43% and profit on a
> $100 bonus bet is $40.00, so `EV = 0.7143 × $40 = $28.57`, not $30.08. The qualitative point
> stands (and is in fact slightly understated).

> 💡 **The closed form the document misses.** At fair odds, `EV_BB = stake × (1 − p)` — a bonus
> bet is worth its face value times the probability you *lose*. Check: −1000 → `$100 × 0.0909 =
> $9.09`; +100 → `$100 × 0.50 = $50.00`; +1000 → `$100 × 0.909 = $90.90`. All three examples in
> one line, and "longshots convert better" becomes self-evident.

### The Two "Optimal" Paths: Defining Your Objective

1. **Path 1: The +EV (High-Variance) Strategy.** Maximizes *theoretical* long-term value by fully
   embracing the "longshot imperative." Accepts extreme short-term variance, including "large
   swings in your bankroll" and long losing streaks, in exchange for the highest possible EV. It
   treats bonus bets as a portfolio of high-risk, high-reward assets, similar to a venture
   capital or angel investing portfolio.
2. **Path 2: The Conversion (Low-Variance) Strategy.** Seeks to *eliminate* all variance and
   guarantee an *immediate, risk-free* cash profit. Known as "Matched Betting" or "Bonus
   Arbitrage," it uses cash hedges on opposing outcomes at different sportsbooks to "lock in" a
   guaranteed percentage of the bonus bet's face value. It effectively "sells" the high-variance
   asset for a certain cash-equivalent value, typically 60% to 80% of its notional amount.

The choice between these two paths is not a mathematical one but a personal financial one.

---

## Part 1: The High-Variance Strategy (Maximizing Expected Value)

### The Longshot Imperative in Practice

The objective is to place every SNR bonus bet on a wager with long odds to maximize the
`(P_win * Profit)` equation.

It is critical to understand that the strategy is *not* to find "value" in the traditional sense
(i.e., a cash bet that is mispriced by the book). While finding a +EV longshot is ideal, **the
SNR mechanic itself provides the value.** The primary goal is simply to find the longest odds
possible, as the EV of the *SNR instrument* is maximized at these odds.

A conflict arises between pure mathematics and practical application. While the *pure*
mathematical optimum is the longest possible odds available (e.g., +10000), this introduces a
level of variance that is behaviorally and financially untenable for a hobbyist. As noted, while
longshots are theoretically better, *"in the real world of betting you don't want to go too long
between collects… they do not win very often."*

This conflict is resolved by identifying a "sweet spot." Multiple analyses suggest a practical
compromise in the **+300 to +600** odds range. This range is not a *mathematical* peak — the EV
will still be higher at +700 — but rather a *behavioral and bankroll management heuristic*. It
represents a sound, analytical decision to sacrifice a small amount of theoretical EV to reduce
variance to a manageable level. A strategy that is 100% optimal but is abandoned after 50
straight losses (a very real possibility) has a net-zero return. A slightly less-optimal strategy
that is sustainable over the long term is superior.

### A Portfolio Approach to Variance Management

The high-variance +EV strategy is *only* viable when viewed through a high-volume,
portfolio-based lens.

A single SNR bet on a +500 underdog (with a ≈16.7% win probability) is a simple gamble. However,
a *portfolio* of one hundred $25 SNR bets, each placed on a different +500 underdog, is a
statistical model with a predictable long-term return.

To execute this strategy, the analytical hobbyist must undergo a complete psychological shift,
abandoning the "pick accuracy" mindset. The goal is not to "be right." The user must adopt the
mindset of a venture capitalist: they are making 100 small investments, *expecting* 84 of them to
go to zero. The 16 that "hit" at +500 will provide the massive positive return that funds the
entire portfolio.

This strategy requires two things:

1. **Meticulous Record-Keeping:** The user must track their **EV**, not their win/loss record.
2. **Bankroll & Fortitude:** The user must have the bankroll and psychological discipline to
   withstand the "large swings" and "long cold streaks" that are a *guaranteed and expected
   feature* of this strategy. A user who, after a few hundred bets, complains that their
   "bankroll is in a steady, consistent downtrend" is simply observing a small, statistically
   predictable patch of negative variance and is failing to adhere to the long-term, high-volume
   requirements of the model.

---

## Part 2: The Guaranteed Profit Strategy (Bonus Bet Conversion)

### A Framework for "Matched Betting" / "Bonus Arbitrage"

The process is executed in two steps:

1. **The "Back" Bet:** The user places the SNR Bonus Bet on a specific outcome (e.g., "Team A to
   win") at FanDuel or DraftKings.
2. **The "Hedge" Bet:** The user places a precisely calculated *real cash* bet on the *opposing*
   outcome (e.g., "Team B to win") at a *different* sportsbook (e.g., BetMGM, Caesars).

This "covers all eventualities" and "removes chance from the equation."

A crucial point is that the core principle from Part 1 *still applies*. To maximize the
*guaranteed conversion rate*, the user must *still* place the SNR bet on the **longshot**
(underdog) side, ideally with odds of **+300 or higher**.

The Conversion strategy is not a *different* strategy from the +EV strategy; it is the *same*
strategy with an added hedge.

### The Mathematics of Conversion: Calculating the Hedge

This strategy is impossible to "eyeball." It requires a specialized **"Bonus Bet Conversion
Calculator."**

The calculator requires three inputs:

1. **Bonus Bet Size:** The dollar amount of the SNR bet (e.g., $100).
2. **Bonus Bet Line:** The American odds of the bonus bet (e.g., +500 on FanDuel).
3. **Hedge Line:** The American odds of the *opposing* cash bet (e.g., −550 on DraftKings).

The calculator then provides three outputs:

1. **Hedge Bet Amount:** The exact cash amount to wager on the opposing side (e.g., $458.33).
2. **Profit:** The guaranteed profit, regardless of outcome (e.g., $41.67).
3. **% Profit:** The conversion rate (e.g., 41.67%).

> 🛑 **ERROR 2.** These outputs do not correspond to the stated −550 hedge line. Solving
> `500 − H = H × (100/550)` gives **hedge $423.08, guaranteed profit $76.92, conversion 76.92%**.
> The printed $458.33 corresponds to a hedge line of roughly **−1100**, not −550.
>
> The error matters because it makes the worked example fail the document's own standard: 41.67%
> is below the "any conversion below 50% is generally considered highly inefficient" threshold
> stated in the very next paragraph, whereas the correct 76.92% comfortably clears the stated
> "70% or higher" goal.

The analytical hobbyist's goal is to find opportunities that yield a **conversion rate of 70% or
higher**. A 60% rate is acceptable for speed or simplicity, but any conversion below 50% is
generally considered highly inefficient.

### Execution: The Essential Toolkit for Finding Opportunities

The primary variable in the conversion calculation is the *difference* between the two sets of
odds. The efficiency of a conversion is 100% dependent on the **"hold"** (or "vig") of the market
being hedged. "Hold" is the sportsbook's built-in profit margin. A high-hold market (e.g., complex
player props, futures) will result in a very poor conversion rate.

The goal is to find markets with the *lowest possible hold*, ideally at or near 0%. A market with
a negative hold is a true "arbitrage" opportunity.

Manually scanning dozens of sportsbooks across thousands of markets is impossible. Therefore the
analytical hobbyist *must* use specialized, real-time scanning software, marketed as:

- "Free Bet Converter" or "Bonus Bet Converter"
- "Dutch Matcher"
- "Low-Hold Finder"
- "Bet Finders"

Leading providers include **OddsJam**, **ProfitDuel**, **DarkHorse Odds**, and **OddsShopper**.
These platforms scan 100+ sportsbooks in real-time and present a pre-calculated, sortable table of
the most profitable conversion opportunities available. A subscription is a non-negotiable "cost
of doing business," analogous to a financial trader paying for a real-time data feed.

---

## Part 3: Strategic Execution — Critical Divergences Between FanDuel & DraftKings

### 1. The "Push" (Tie) Rule: The Most Critical Difference

- **FanDuel:** If a bonus bet wager results in a push (a tie), the bonus bet is **LOST**. It
  *"will not be returned to your account"*.
- **DraftKings:** If a bonus bet results in a push, the bonus bet is **RETURNED** to the user's
  account.

The implication of FanDuel's rule is severe. For a cash bet, a push is a neutral, non-event. For a
FanDuel bonus bet, **a push is a total loss of the asset.**

This flaw destroys the "guaranteed profit" model of conversion if the user wagers on a market that
can push. For example:

- **Back Bet:** $100 FD Bonus Bet on "Kansas City Chiefs −7"
- **Hedge Bet:** $100 Cash Bet on "Los Angeles Chargers +7"
- **Outcome:** Chiefs win by exactly 7.

In this scenario, the cash hedge bet pushes and the $100 is returned. However, the $100 FanDuel
bonus bet is *lost*. The user has lost their entire bonus asset and gained $0. **This is a 100%
loss.**

Therefore, the optimal strategy on FanDuel *must* involve **avoiding all markets with a non-zero
probability of a push.** This includes NFL spreads (−3, −7), NBA spreads, and any whole-number
totals. The optimal markets for FanDuel conversions are strict 2-way moneylines (e.g., Tennis,
MLB) or 3-way markets (e.g., Soccer) where the "Draw" outcome can be hedged as a separate leg.

DraftKings does not have this flaw, making it the superior platform for converting bonus bets on
spreads and totals.

### 2. The "Splitting" Rule: A Key Bankroll & Risk Management Factor

- **FanDuel: YES**, bonus bets *can* be split. A user can *"adjust the wager… to only use a
  portion of the full Bonus Bet"*.
- **DraftKings: NO**, bonus bets *cannot* be split. They *"must be used in whole"*. Sign-up offers
  are often delivered as multiple, indivisible tokens (e.g., "twelve $25 slips").

Consider a large "No Sweat Bet" that refunds a user with a single $500 bonus bet:

- On **DraftKings**, this must be wagered as one $500 bet. To optimally convert this on a +500
  longshot, the user would need a cash hedge of over **$2,000** at another book. This creates a
  massive liquidity barrier for a hobbyist.
- On **FanDuel**, this $500 bonus can be split. The user can execute ten separate $50 conversions,
  each requiring a much smaller, more manageable cash hedge.

Therefore, **FanDuel is the superior platform for low-bankroll hobbyists executing a conversion
strategy on large-denomination bonuses.**

### 3. Combining with Cash

- **FanDuel: YES**, a user can combine cash and bonus funds in a single wager (e.g., a $50 bet
  using a $25 bonus and $25 cash).
- **DraftKings: NO**, a bonus bet cannot be combined with other rewards or cash in the same wager.

### Valuable Table: FanDuel vs. DraftKings Bonus Bet Rules & Strategic Implications

| Feature | FanDuel | DraftKings | Optimal Strategy Implication |
|---|---|---|---|
| **Stake Returned?** | No (SNR) | No (SNR) | **Baseline:** Use +EV (longshot) or Conversion strategy for both. |
| **Can Bonus Bet be Split?** | Yes | No | **FD Advantage:** Ideal for bankroll management. A large bonus can be split into smaller, low-risk conversions. **DK Risk:** Requires a single, large cash hedge, creating liquidity and bankroll challenges. |
| **Result of a "Push" (Tie)?** | Bonus Bet is **Lost** | Bonus Bet is **Returned** | **FD Risk: CRITICAL.** Never use FD bonus bets on markets that can push (e.g., whole-number spreads/totals). Prioritize 2-way moneylines (tennis, MLB). **DK Advantage:** Pushes are safe; bet can be reused. All markets are viable. |
| **Combine with Cash?** | Yes | No | *FD Advantage:* Offers greater flexibility in wager construction. |
| **Expiration** | 7 Days (typical) | 7 Days (typical) | **Action Required:** Both require quick execution to find the best conversion/EV opportunities. |

---

## Part 4: A Tactical Playbook for Common Promotion Types

### Strategy 1: "No Sweat Bet" / "Second Chance Bet"

**Mechanics:** This promotion (e.g., "$1,000 No Sweat First Bet") is commonly misunderstood. It is
*not* a "risk-free" bet. It is a **cash bet with an insurance policy**. If the initial cash bet
*loses*, the user is refunded their stake in the form of **SNR Bonus Bets**, not cash. This creates
a two-stage problem.

**Optimal Strategy (Conversion), step by step:**

1. Find an optimal conversion opportunity using a "Second Chance Bet Finder" tool.
2. Place the initial "No Sweat" *cash* bet (e.g., $1,000) on the underdog (e.g., FIU +600) at
   FanDuel/DraftKings.
3. Use the tool's calculator to determine the precise cash *hedge* bet (e.g., $857.14) on the
   favorite (e.g., Liberty −700) at a *different* book.
4. **Analyze Outcomes:**
   - **Scenario A (Underdog Wins):** The $1,000 FD/DK bet wins $6,000. The $857.14 hedge bet
     loses. **Net Profit = $5,142.86.**
   - **Scenario B (Favorite Wins):** The $1,000 FD/DK bet loses. The $857.14 hedge bet wins
     $122.45. FD/DK *refunds* the $1,000 loss as bonus bets.
   - **Step 5 (if Scenario B):** The user now has a $1,000 bonus bet asset. They apply the
     Conversion Strategy from Part 2. Assuming a 75% conversion rate, this asset is turned into
     $750 in guaranteed cash.
   - **Final Tally (Scenario B):** $122.45 (hedge win) + $750 (bonus conversion) = **$872.45 Net
     Profit.**

> 🛑 **ERROR 3 — the most serious in any of these documents.** Scenario B is a **loss of
> $127.55**, not a profit of $872.45. The $1,000 cash stake that lost is never subtracted:
>
> ```
>   −$1,000.00   losing cash bet
>   +$  122.45   hedge win
>   +$  750.00   bonus conversion at 75%
>   ───────────
>   −$  127.55   actual net
> ```
>
> $872.45 is the *gross return*, not net profit. Scenario A ($5,142.86) is correct. The document
> does note this is "a high-variance conversion" and that a true guaranteed conversion "locks in
> ~40–50% of the promo's face value" — but as printed, both scenarios look profitable, which
> conceals that the strategy as illustrated *loses money* whenever the favorite wins. Since the
> favorite at −700 wins ~87.5% of the time, that is the overwhelmingly likely outcome.

**Optimal Strategy (+EV):** The high-variance play is simpler.

1. Place the initial $1,000 cash bet on a +EV cash line.
2. If it wins, the promotion is over.
3. If it loses, the user receives $1,000 in bonus bets. They then deploy this asset using the
   High-Variance Strategy from Part 1 (e.g., on a +500 longshot, letting it ride). This is a
   "two-shot" +EV strategy that embraces risk for a higher potential payout.

### Strategy 2: "Bet & Get" (e.g., Bet $5, Get $300 in Bonus Bets *if your bet wins*)

**Mechanics:** Highly common sign-up offers. They require a small *cash* bet (e.g., $5) that *must
win* to unlock a large bonus.

**The Trap:** The common advice is to bet the $5 on a *heavy favorite* to increase the
*probability* of winning.

**The Analytical Flaw:** This is *still gambling*. A −500 favorite (the typical minimum odds) still
loses ≈16.7% of the time. In that scenario, the user loses their $5 and, more importantly,
*receives $0 in bonuses*. This is an unacceptable risk for an analytical hobbyist.

**Optimal Strategy (Qualifying Bet Hedge):** Treat the $5 bet as a *qualifying bet* that must be
secured. Hedge it to *guarantee* it wins, thereby guaranteeing the bonus asset is unlocked.

1. Use an "Opportunity Finder" (from Part 2) to find a low-hold market.
2. Place the $5 *qualifying bet* on one side (e.g., Team A −400) at DraftKings.
3. Place a simultaneous cash *hedge* bet (e.g., $1.30) on the other side (e.g., Team B +380) at
   FanDuel.
4. **Result:** The user will have a small, guaranteed loss (a "qualifying loss") of perhaps $0.20.
   But this *guarantees* that their qualifying bet wins, unlocking the $300 bonus bet asset.

> ⚠️ **Minor discrepancy.** The stated numbers give a qualifying loss of **$0.05–$0.06**, not
> $0.20 (favorite wins: $1.25 − $1.30 = −$0.05; underdog wins: −$5.00 + $4.94 = −$0.06). The
> $0.20 figure is conservative, so the conclusion is unaffected.

This strategy is infinitely superior. The user is *buying* a $300 bonus bet asset for a *fixed
cost* of ~$0.20. This $300 asset can then be converted (per Part 2) into ~$225 in guaranteed cash,
for a net profit of ~$224.80.

---

## Part 5: The Unspoken Risk — Account Limitations and Mitigation Strategies

### The Core Conflict: Advantage Play vs. Bookmaker Profitability

The strategies detailed in this report are forms of "advantage play." Sportsbooks are private
businesses whose model relies on recreational, losing players. They are *not* obligated to accept
wagers from — or offer promotions to — consistently winning or analytical players.

Engaging in bonus arbitrage (conversion) is explicitly frowned upon and will be detected by their
risk-management algorithms. The consequences are predictable and severe:

1. **"Promo Ban":** The account remains open for normal (high-vig) betting, but the user is banned
   from all promotions. This effectively ends the bonus-extraction hobby.
2. **Stake Limiting:** The user's maximum bet size is reduced to trivial amounts (e.g., $5.00),
   making any strategy unprofitable.
3. **Account Closure:** In repeated or extreme cases, the account is banned and closed.

The *true* long-term "optimal strategy" is therefore not just about mathematical extraction; it is
a *meta-game* of maximizing profit while minimizing detection. A strategy that makes $1,000 in one
week and results in a permanent promo ban is *inferior* to a strategy that makes $3,000 over three
months by flying under the radar.

### A Framework for Camouflage: "Mug Betting"

The goal is to mimic the betting patterns of a recreational, unprofitable "mug" punter.

**Actionable Rules for Account Health:**

1. **Round Your Stakes:** *Never* bet exact cents. A hedge calculation may demand a $47.81 wager.
   This is an enormous red flag. Round to $48 or $50. The small loss in conversion efficiency is
   the price of camouflage.
2. **Place "Mug" Bets:** Place non-promotional, −EV bets. This includes small parlays and straight
   bets on high-visibility markets (e.g., betting on a "favorite team" on Monday Night Football).
3. **Bet Popular Markets:** Do not *only* bet on obscure markets (e.g., KBO, table tennis) where
   optimal conversion lines are found. This is a clear sign of an "arber." Mix in "mug" bets on
   popular NFL, NBA, and CFB markets.
4. **Never Hedge at the Same Book:** This is the cardinal sin. Placing the "back" and "lay" (hedge)
   bet at the same sportsbook is explicitly forbidden and will be caught. All hedges must be placed
   at a *different*, non-affiliated sportsbook.
5. **Mimic User Behavior:** Do not just log in, place the single optimal conversion bet, and log
   out. This "hit and run" pattern is a flag. Browse the site, look at odds.
6. **Manage Withdrawals:** Do not withdraw after every successful conversion. Let the balance sit
   and grow, or only withdraw larger, rounded amounts (e.g., $500) periodically.

The analytical hobbyist should treat "mug betting" as a **quantitative cost of doing business** —
a calculated operational expense. Budget a small percentage of profits (e.g., 5–10%) to be "lost"
on these −EV bets. This "fee" is paid to the sportsbook in exchange for continued access to their
highly profitable promotional offers.

---

## Conclusion: An Integrated "Hobbyist Portfolio" Strategy (A Phased Approach)

The optimal path is not one strategy, but a **phased, hybrid model**.

### Phase 1: Bankroll Creation (Low-Risk Conversion)

As a new hobbyist, the primary, non-negotiable goal is to build a "float" or "bankroll" with *zero
risk*. In this phase, the user will *exclusively* use the **Conversion (Matched Betting)
Strategy** from Part 2.

Systematically execute the "Qualifying Bet Hedge" to acquire all "Bet & Get" sign-up offers, then
use "Second Chance Bet Finders" to convert all "No Sweat" offers. Repeat for every recurring
promotion, systematically and risk-free, converting every SNR asset into withdrawable cash. This is
the *only* strategy that builds a substantial bankroll from scratch. **This is the
capital-generation engine.**

### Phase 2: Bankroll Growth (High-Variance +EV)

Once the risk-free conversion engine has built a substantial, dedicated, and *separate* bankroll
(e.g., $3,000–$5,000), the strategy can evolve. This self-funded bankroll now allows the hobbyist
to *afford* to absorb the variance of a true +EV betting portfolio.

1. **For Bonus Bets:** Deploy them using the High-Variance (+EV) Strategy from Part 1. Instead of
   converting a $100 bonus into $75, place it on a +500 longshot and let it ride, accepting the
   variance because the $75 cash is no longer needed.
2. **For Cash Bets:** Use the cash bankroll (built risk-free) to begin a high-volume,
   portfolio-based +EV *cash* betting strategy, using analytical tools to find mispriced lines.

This hybrid model uses the risk-free, arbitrage-based "conversion" method as a capital-generation
engine, which in turn funds the long-term, high-variance "value investing" (+EV) portfolio.

---

## Verification

**Conceptually the strongest of the three documents.** Alone among them it addresses variance
explicitly, insists on tracking EV rather than win/loss record, treats hold correctly as the
determinant of conversion efficiency, distinguishes theoretical optimum from behaviorally
sustainable strategy, and accounts for operational risk (account limiting). Several of the gaps
identified in `edge-of-vigor.md` are answered here.

**Verified correct:** the SNR EV derivation; all three worked bonus-bet examples ($9.09 / $50.00 /
$90.90); the ≈16.7% loss rate at −500; the ~$2,000 hedge requirement for a $500 bonus at +500; the
100-bet portfolio expecting ~84 losses at +500; Scenario A of the No Sweat playbook ($5,142.86).

**Three errors:**

| # | Location | Stated | Correct |
|---|---|---|---|
| 1 | p.6, −250 bonus bet EV | $30.08 | **$28.57** |
| 2 | p.10, conversion hedge at −550 | $458.33 / $41.67 / 41.67% | **$423.08 / $76.92 / 76.92%** (printed values imply a −1100 hedge) |
| 3 | p.15, No Sweat Scenario B | +$872.45 "Net Profit" | **−$127.55** (the $1,000 losing stake is never subtracted) |

Error 3 is the one that matters operationally. As printed, both branches of the No Sweat playbook
appear profitable. In reality the strategy as illustrated loses money whenever the favorite wins —
which, at −700, is about 87.5% of the time. Anyone following the worked example expecting the
printed outcome would be systematically misled about the sign of their return.

Errors 1 and 2 do not change any recommendation. Error 2 is nonetheless notable because it makes
the example fail the document's own stated threshold: 41.67% falls below its "below 50% is highly
inefficient" line, while the correct 76.92% clears its "70% or higher" goal.

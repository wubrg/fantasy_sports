---
title: "The Edge of Vigor — A Layman's Guide to Probability, Pricing, and Profit in the NFL Market"
source_pdf: edge-of-vigor.pdf
source_url: https://gemini.google.com/share/f260eb00b555
alt_url: https://share.gemini.google/iSzFvOpWkCxQ
source_pages: all 36
created: 2025-11-20
captured: 2026-08-06
transcription: |
  Transcribed visually from a rasterized render; the PDF text layer is unusable.
  Wide tables are clipped at the right page margin in the capture — see the
  "Clipped columns" note below.
status: ARCHIVE — source text with verification notes
---

# The Edge of Vigor

> **Archive.** This is the source document, transcribed. Its arithmetic was independently
> verified — see [Verification](#verification) at the bottom. **One real error was found**
> in the bonus-bet tables; it is flagged inline where it occurs.

## Clipped columns

The capture is a print-to-PDF of a page wider than the printed area, so tables lose their
rightmost columns. Where a column header is visible but its values are cut off, the header
is transcribed and the values are marked `[clipped]`. **No clipped value has been invented.**
All of them are reproducible from `EV = p·profit − (1−p)·stake` via
[`edge/app/internal/wager`](../../app/internal/wager/), which is pinned to the visible
values by test.

---

## Part 0: The original explainer (pp.2–9)

> *Prompt: "I want you to put together a report that explains to a Layman how to understand
> positive [EV] when it comes to player prop Wagers for NFL sportsbooks. I want to understand
> what the plus or minus lines imply about the probability…"*

This report breaks down the concept of Positive Expected Value (+EV) specifically for NFL
player props. It is designed to move you from "guessing" to "investing" by understanding the
math behind the lines.

### 1. The Core Concept: What is Positive EV?

In simple terms, **Positive EV (+EV)** means you are making a wager where the *true
probability* of winning is higher than the *implied probability* of the odds the sportsbook
is offering.

Think of it like a weighted coin flip. If you know a coin lands on Heads 60% of the time, but
a casino pays you as if it's a 50/50 toss, you have a "Positive EV" bet. You won't win every
flip, but if you bet on it 100 times, you are mathematically guaranteed to profit.

### 2. Decoding the Lines: What Odds Actually Mean

Sportsbooks express probability through "American Odds" (the plus and minus numbers). To find
value, you must first translate these numbers into percentages, known as **Implied
Probability**.

#### The "Minus" Lines (e.g., −130)

- **What it means:** The outcome is the **Favorite** (more likely to happen).
- **The Math:** You must risk more money to win a smaller amount ($130 to win $100).
- **Implied Probability Formula:** `Odds / (Odds + 100) × 100`
  - *Example:* For a line of **−130**: `130 / (130 + 100) = 130/230 ≈ 56.5%`
  - *Takeaway:* If you bet this, you need to win more than **56.5%** of the time just to
    break even.

#### The "Plus" Lines (e.g., +110)

- **What it means:** The outcome is the **Underdog** (less likely to happen).
- **The Math:** You risk less money to win a larger amount ($100 to win $110).
- **Implied Probability Formula:** `100 / (Odds + 100) × 100`
  - *Example:* For a line of **+110**: `100 / (110 + 100) = 100/210 ≈ 47.6%`
  - *Takeaway:* To break even here, you only need to win more than **47.6%** of the time.

### 3. The "Vig": Why 50/50 Isn't Enough

You might notice that if you look at a typical prop (e.g., *Jalen Hurts Passing Yards:
Over/Under 235.5*), both sides are often priced at **−110**.

- Implied Probability of Over (−110): **52.4%**
- Implied Probability of Under (−110): **52.4%**
- **Total:** 104.8%

That extra **4.8%** is the **Vig** (or "Juice"). It is the sportsbook's fee.

> **Critical Rule:** In a standard −110 market, you must be right **52.4%** of the time just
> to *break even*. To be profitable, your prediction model must strike at a rate higher than
> that (e.g., 55%+).

> ⚠️ **Verification note.** 4.8% is the **overround** in percentage points. The book's actual
> **hold** on balanced action is 4.8 / 104.8 = **4.58%**. The document uses "vig" for both
> throughout. See [Verification](#verification), item 4.

### 4. How to Extract Positive Value (The Strategies)

You extract value by finding discrepancies between the *Sportsbook's Implied Probability* and
the *True Probability*.

#### Strategy A: Line Shopping (The "Free Lunch")

Sportsbooks do not always agree. This is the easiest way to find +EV without doing complex
math.

- **Scenario:**
  - DraftKings has Derrick Henry "Over 80.5 Rushing Yards" at **−140** (Implied Prob: 58.3%).
  - FanDuel has the same prop at **−110** (Implied Prob: 52.4%).
- **The Value:** By betting on FanDuel, you are saving nearly 6% in required win rate. You are
  buying the exact same product for a cheaper price.

#### Strategy B: Market Width (The "Confidence Meter")

"Market Width" is the difference between the odds of the "Over" and the "Under." It tells you
how confident the book is in its own number.

- **Tight Market (High Confidence):** −110 (Over) / −110 (Under). The gap is small (20 cents).
  The book is confident; these lines are hard to beat.
- **Wide Market (Low Confidence):** −135 (Over) / +105 (Under). The gap is larger (40 cents).
  The book is unsure and charging a higher premium (vig) to protect itself.
- **The Play:** Be cautious with wide markets. While they offer potential value, the extra
  "vig" means you have to be even *more* right to cover the cost.

> ⚠️ **Verification note.** This advice reverses in Part II (p.31), where wide markets become
> *"where errors live."* Both framings appear in the same document. See
> [Verification](#verification), item 5.

#### Strategy C: The "Analytical Hobbyist" Approach

You calculate your own "True Probability" using data sources (average yards per game, defense
rank, etc.) and compare it to the line.

**The EV Calculation Formula:**

```
(Probability of Winning × Profit) − (Probability of Losing × Stake)
```

**Real-World Example:**

- **The Bet:** Patrick Mahomes Over 280.5 Passing Yards at **+100** (Even Money).
- **Implied Probability:** 50%.
- **Your Research:** You believe he hits this 55% of the time.
- **The Math (assuming $100 bet):** `(0.55 × $100) − (0.45 × $100)` = `($55) − ($45)` =
  **+$10 EV**
- **Result:** This is a +EV wager. Over time, you will average a $10 profit per bet on this
  specific profile.

### Summary Checklist

1. **Convert Odds to Percentage:** Use the formulas above.
2. **Identify the Breakeven Point:** Remember that −110 means you need >52.4% accuracy.
3. **Shop the Line:** Check if another book offers a lower implied probability for the same
   event.
4. **Compare to Reality:** Does your data suggest the event happens *more often* than the
   implied percentage? If yes, you have found Positive EV.

---

## Part 0b: The cheat sheet (pp.6–9)

> *Prompt: "I want a cheat sheet and a reference table of understanding implied probability
> for various lines. The chart should show implied probability and various EV for percentage
> thresholds of true probability."*

### 1. The "Breakeven" Reference Table

**Implied Probability:** This is your "Hurdle Rate." You must win more often than this
percentage to be profitable.

| American Odds | Implied Prob (Breakeven) | What it Means |
|---|---|---|
| −300 | 75.0% | Heavy Favorite. Very hard to profit long-term. |
| −250 | 71.4% | Strong Favorite. |
| −200 | 66.7% | Moderate Favorite. |
| −175 | 63.6% | Standard "Favorite" Prop Line. |
| −150 | 60.0% | |
| −130 | 56.5% | Slightly Juiced Favorite. |
| **−110** | **52.4%** | **Standard Spread/Total Price.** |
| +100 (Even) | 50.0% | Coin Flip. |
| +110 | 47.6% | Slight Underdog. |
| +130 | 43.5% | Moderate Underdog. |
| +150 | 40.0% | Significant Underdog. |
| +200 | 33.3% | 2-to-1 payout. |
| +300 | 25.0% | Longshot. |

### 2. The "Profitability Matrix" (EV Cheat Sheet)

Find the **Odds** offered by the book on the left, then scan across to see your Expected Value
(ROI) based on your **True Probability**.

- **Red:** Negative EV (Bad Bet — You will lose money long term).
- **Green:** Positive EV (Good Bet — You will profit long term).
- **Bold Green:** Strong Play (+5% EV or higher).

| Book Odds | Implied Prob | If True Prob is 55% | If True Prob is 60% |
|---|---|---|---|
| −200 | 66.7% | −17.5% (Loss) | −10.0% (Loss) |
| −150 | 60.0% | −8.3% (Loss) | 0.0% (Breakeven) |
| −130 | 56.5% | −2.7% (Loss) | **+6.2%** |
| **−110** | **52.4%** | **+5.0%** | **+14.5%** |
| +100 | 50.0% | +10.0% | **+20.0%** |
| +120 | 45.5% | +21.0% | **+32.0%** |
| +150 | 40.0% | +37.5% | **+50.0%** |

✅ **All 14 cells independently verified correct.**

**How to Use This:**

1. **Check the Odds:** You see a Prop at −130.
2. **Check Implied Probability:** The table shows this implies 56.5%.
3. **Consult Your Research:** Your analysis (or model) says this player hits this stat 60% of
   the time.
4. **Find the Intersection:** Go to the −130 row and the 60% True Prob column.
5. **Decision:** You see +6.2%. This is a green (profitable) bet.

### Key Takeaways

- **The −110 Trap:** At standard −110 odds, being "kind of sure" isn't enough. You need to be
  right 52.4% of the time just to have $0.00 profit. You generally need 55% confidence to make
  −110 worth betting.
- **Heavy Favorites Kill ROI:** Look at −200. Even if you are 65% sure (which is very high
  confidence in the NFL), you still **lose money** long-term (−2.5% EV) betting it. You need
  massive confidence (70%+) to bet heavy favorites.
- **Underdogs Scale Fast:** If you find a +120 dog that you think is actually a coin flip (50%
  true probability), your EV is +10%. That is a massive edge.

✅ Both numeric claims verified: −200 @ 65% = −2.5%; +120 @ 50% = +10.0%.

> ⚠️ **Verification note.** "Underdogs Scale Fast" is correct arithmetic, but it points at the
> market segment with the highest vig and the strongest adverse favorite–longshot bias. See
> [Verification](#verification), item 3.

---

## Part 0c: Expanded tables (pp.10–20)

> *Prompt: "I want to enhance this to also show the EV for $1, $3, $5, and $10 wagers. The
> chart should also expand from −500 to +1000."*

### 1. The Expanded Implied Probability "Ruler"

**Multipliers:** How much you win relative to your bet (e.g., 0.5x means you win $5 on a $10
bet).

| American Odds | Implied Probability (Hurdle Rate) | Win Multiplier (Profit) | Difficulty |
|---|---|---|---|
| −500 | 83.3% | 0.20x | Extreme (Requires certainty) |
| −400 | 80.0% | 0.25x | Very High … Needed |
| −300 | 75.0% | 0.33x | High Co… `[clipped]` |
| −200 | 66.7% | 0.50x | Strong F… `[clipped]` |
| −150 | 60.0% | 0.67x | Modera… `[clipped]` |
| **−110** | **52.4%** | 0.91x | Standar… `[clipped]` |
| +100 (Even) | 50.0% | 1.00x | Coin Flip |
| +150 | 40.0% | 1.50x | Modera… `[clipped]` |
| +200 | 33.3% | 2.00x | 2-to-1 D… `[clipped]` |
| +300 | 25.0% | 3.00x | Longsh… `[clipped]` |
| +500 | 16.7% | 5.00x | Deep Lo… `[clipped]` |
| +1000 | 9.1% | 10.00x | "Lottery… `[clipped]` |

### 2. The Micro-Stakes EV Cheat Sheet

*"If I bet small amounts ($1–$10), is it actually worth it?"*

#### Scenario A: Betting Standard Lines (−110)

*Most player props (Over/Under Yards) are priced here.*

| Your Confidence (True Prob) | EV for $1.00 Bet | EV for $3.00 Bet | EV for $5.00 | EV for $10.00 |
|---|---|---|---|---|
| 50% (Coin Flip) | −$0.05 (Loss) | −$0.14 (Loss) | −$0.23 (Loss) | `[clipped]` |
| 53% (Tiny Edge) | +$0.01 | +$0.04 | +$0.06 | `[clipped]` |
| 55% (Solid Edge) | +$0.05 | +$0.15 | +$0.25 | `[clipped]` |
| 60% (Strong Edge) | +$0.15 | +$0.44 | +$0.73 | `[clipped]` |

> **Takeaway:** At −110, if you are only 55% sure, you are grinding out pennies (50 cents on a
> $10 bet). You need high volume for this to add up.

#### Scenario B: Betting "Plus Money" Value (+100 to +150)

*Often found in "Alternate Lines" or Touchdown Scorer props.*

| Market Odds | Your Confidence | EV for $1.00 | EV for $3.00 |
|---|---|---|---|
| +100 | 55% | +$0.10 | +$0.30 |
| +100 | 60% | +$0.20 | +$0.60 |
| +120 | 50% | +$0.10 | +$0.30 |
| +150 | 45% | +$0.13 | +$0.38 |
| +150 | 50% | +$0.25 | +$0.75 |

> **Takeaway:** This is where the value explodes. Finding a +150 underdog that you believe is
> actually a coin flip (50%) pays **5x more** EV than a standard −110 edge.

#### Scenario C: Betting Longshots (+200 to +1000)

*Typically "First Touchdown Scorer" or "2+ Touchdown" bets.*

| Market Odds | Your Confidence | EV for $1.00 | EV for $3.00 |
|---|---|---|---|
| +200 | 35% | +$0.05 | +$0.15 |
| +300 | 30% | +$0.20 | +$0.60 |
| +500 | 20% | +$0.20 | +$0.60 |
| +1000 | 10% | +$0.10 | +$0.30 |
| +1000 | 15% | +$0.65 | +$1.95 |

> **Takeaway:** Notice the volatility. If you bet a +1000 longshot and you are right 15% of the
> time (instead of the implied 9%), you make a massive **$6.50 EV on a $10 bet**. This is why
> "sprinkling" on longshots can be mathematically sound if your research supports it.

✅ All of Scenarios A, B, and C verified correct.

### 3. How to Read "Minus" Odds for Profit (The Safety Tax)

**Example: Betting a "Safe" Prop at −200** *(You risk $10 to win $5)*

| Your Confidence | EV ($10 Bet) | Verdict |
|---|---|---|
| 66.7% (Breakeven) | $0.00 | Waste of time |
| 70% | +$0.50 | **Tiny Value** |
| 75% | +$1.25 | Moderate Value |
| 80% | +$2.00 | Good Value |

**The Trap:** You need to be **80% certain** just to get a $2.00 expected return on a $10
investment. If your confidence is only 70%, you are risking $10 to make 50 cents. **This is
often bad math.**

✅ Verified correct.

---

## Part 0d: The tiered reference document (pp.14–20)

> *Prompt: "I want to synthesize this into a table that has columns for the line, the implied
> probability of the line, the breakeven percentage of your true probability, the positive ev
> for the next three standard deviations of true probability."*

### How to Read These Tables

- **Implied Probability:** This is the "Breakeven" line. You must be more confident than this
  number to profit.
- **Confidence Tiers (The "Standard Deviations"):** Since "True Probability" is subjective, the
  EV is calculated at three tiers of "Edge" (how much *more* likely you think the event is than
  the sportsbook implies).
  - **Tier 1 (Modest Edge):** True Probability is **5% higher** than the implied odds.
  - **Tier 2 (Strong Edge):** True Probability is **10% higher** than the implied odds.
  - **Tier 3 (Elite Edge):** True Probability is **15% higher** than the implied odds.
- **Positive EV:** The dollar amount you expect to profit *on average* per bet at that
  confidence level.

> **Verification note.** "5% higher" means **5 percentage points added** to the implied
> probability, not a 5% relative increase. Confirmed by reproducing every value in Tables A–C.
> Worth stating explicitly, since the wording is ambiguous.

### Section 1: Real Money Wagers

*Formula used:* `(True Prob * Profit) - (False Prob * Stake)`

#### Table A: The $1.00 Wager

| Line | Implied Prob | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) | EV @ Tier 3 (+15%) |
|---|---|---|---|---|
| −500 | 83.3% | +$0.06 | +$0.12 | `[clipped]` |
| −400 | 80.0% | +$0.06 | +$0.13 | `[clipped]` |
| −300 | 75.0% | +$0.07 | +$0.13 | `[clipped]` |
| −200 | 66.7% | +$0.08 | +$0.15 | `[clipped]` |
| −150 | 60.0% | +$0.08 | +$0.17 | `[clipped]` |
| **−110** | **52.4%** | **+$0.10** | **+$0.19** | `[clipped]` |
| +100 | 50.0% | +$0.10 | +$0.20 | `[clipped]` |
| +150 | 40.0% | +$0.13 | +$0.25 | `[clipped]` |
| +200 | 33.3% | +$0.15 | +$0.30 | `[clipped]` |
| +300 | 25.0% | +$0.20 | +$0.40 | `[clipped]` |
| +400 | 20.0% | +$0.25 | +$0.50 | `[clipped]` |
| +500 | 16.7% | +$0.30 | +$0.60 | `[clipped]` |
| +1000 | 9.1% | +$0.55 | +$1.10 | `[clipped]` |
| +2000 | 4.8% | +$1.05 | +$2.10 | `[clipped]` |
| +4000 | 2.4% | +$2.05 | +$4.10 | `[clipped]` |

#### Table B: The $3.00 Wager

*A common unit size for casual hobbyists.*

| Line | Implied Prob | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) |
|---|---|---|---|
| −500 | 83.3% | +$0.18 | +$0.36 |
| −400 | 80.0% | +$0.19 | +$0.38 |
| −300 | 75.0% | +$0.20 | +$0.40 |
| −200 | 66.7% | +$0.23 | +$0.45 |
| −150 | 60.0% | +$0.25 | +$0.50 |
| **−110** | **52.4%** | **+$0.29** | **+$0.57** |
| +100 | 50.0% | +$0.30 | +$0.60 |
| +150 | 40.0% | +$0.38 | +$0.75 |
| +200 | 33.3% | +$0.45 | +$0.90 |
| +300 | 25.0% | +$0.60 | +$1.20 |
| +400 | 20.0% | +$0.75 | +$1.50 |
| +500 | 16.7% | +$0.90 | +$1.80 |
| +1000 | 9.1% | +$1.65 | +$3.30 |
| +2000 | 4.8% | +$3.15 | +$6.30 |
| +4000 | 2.4% | +$6.15 | +$12.30 |

#### Table C: The $5.00 Wager

*Standard "Unit" for many bettors.*

| Line | Implied Prob | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) |
|---|---|---|---|
| −500 | 83.3% | +$0.30 | +$0.60 |
| −400 | 80.0% | +$0.31 | +$0.63 |
| −300 | 75.0% | +$0.33 | +$0.67 |
| −200 | 66.7% | +$0.38 | +$0.75 |
| −150 | 60.0% | +$0.42 | +$0.83 |
| **−110** | **52.4%** | **+$0.48** | **+$0.95** |
| +100 | 50.0% | +$0.50 | +$1.00 |
| +150 | 40.0% | +$0.63 | +$1.25 |
| +200 | 33.3% | +$0.75 | +$1.50 |
| +300 | 25.0% | +$1.00 | +$2.00 |
| +400 | 20.0% | +$1.25 | +$2.50 |
| +500 | 16.7% | +$1.50 | +$3.00 |
| +1000 | 9.1% | +$2.75 | +$5.50 |
| +2000 | 4.8% | +$5.25 | +$10.50 |
| +4000 | 2.4% | +$10.25 | +$20.50 |

✅ Tables A, B, and C verified correct throughout.

### Section 2: Bonus Bets (Stake Not Returned)

*Important:* Bonus Bets work differently. If you win, you only get the profit (the stake
disappears). If you lose, you lose nothing (financially). The EV here is the **Expected Cash
Conversion**.

- *Strategy Note:* You maximize Bonus Bets by betting **Longshots**. Notice how betting a
  Favorite (−200) returns pennies, while a Longshot (+4000) returns massive value.

> 🛑 **ERROR IN SOURCE — the first three rows of Tables D and E are wrong.**
>
> Rows −500, −400, and −300 use the **−200 profit multiplier (0.50x)** instead of their own
> (0.20x, 0.25x, 0.33x). Every other row in both tables is exact. Correct values:
>
> | Line | Table D ($5 BB) Tier 1 | | Table E ($10 BB) Tier 1 | |
> |---|---|---|---|---|
> | | **stated** | **correct** | **stated** | **correct** |
> | −500 | $2.21 | **$0.88** | $4.42 | **$1.77** |
> | −400 | $2.13 | **$1.06** | $4.25 | **$2.13** |
> | −300 | $2.00 | **$1.33** | $4.00 | **$2.67** |
>
> The error also **inverts the document's own thesis.** As printed, a −500 bonus bet ($2.21)
> appears to convert *better* than a −200 one ($1.79), contradicting the Strategy Note directly
> above it. With correct math the sequence is monotonic — $0.88 → $1.06 → $1.33 → $1.79 → … →
> $14.80 — which is what "bonus bets favor longshots" actually predicts.
>
> Note that the consolidated report in Part III (p.34) drops these three rows entirely and is
> correct as printed.

#### Table D: The $5.00 Bonus Bet

Values below are **as printed in the source**; see the correction above.

| Line | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) | EV @ Tier 3 (+15%) |
|---|---|---|---|
| −500 | 🛑 $2.21 | 🛑 $2.33 | 🛑 $2.46 |
| −400 | 🛑 $2.13 | 🛑 $2.25 | 🛑 $2.38 |
| −300 | 🛑 $2.00 | 🛑 $2.13 | 🛑 $2.25 |
| −200 | $1.79 | $1.92 | $2.04 |
| −150 | $2.17 | $2.33 | $2.50 |
| **−110** | **$2.61** | **$2.84** | **$3.06** |
| +100 | $2.75 | $3.00 | $3.25 |
| +150 | $3.38 | $3.75 | $4.13 |
| +200 | $3.83 | $4.33 | $4.83 |
| +300 | $4.50 | $5.25 | $6.00 |
| +500 | $5.42 | $6.67 | $7.92 |
| +1000 | $7.05 | $9.55 | $12.05 |
| +2000 | $9.80 | $14.80 | $19.80 |
| +4000 | $14.80 | $24.80 | $34.80 |

#### Table E: The $10.00 Bonus Bet

| Line | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) | EV @ Tier 3 (+15%) |
|---|---|---|---|
| −500 | 🛑 $4.42 | 🛑 $4.67 | 🛑 $4.92 |
| −400 | 🛑 $4.25 | 🛑 $4.50 | 🛑 $4.75 |
| −300 | 🛑 $4.00 | 🛑 $4.25 | 🛑 $4.50 |
| −200 | $3.58 | $3.83 | $4.08 |
| −150 | $4.33 | $4.67 | $5.00 |
| **−110** | **$5.22** | **$5.67** | **$6.13** |
| +100 | $5.50 | $6.00 | $6.50 |
| +150 | $6.75 | $7.50 | $8.25 |
| +200 | $7.67 | $8.67 | $9.67 |
| +300 | $9.00 | $10.50 | $12.00 |
| +500 | $10.83 | $13.33 | $15.83 |
| +1000 | $14.10 | $19.10 | $24.10 |
| +2000 | $19.60 | $29.60 | $39.60 |
| +4000 | $29.60 | $49.60 | $69.60 |

---

## Part 0e: The "Edge" mental model (pp.21–23)

> *Prompt: "I want to understand how to mentally model and think about the various edges
> respective to their implied odds. What does 5% more confident mean outside of the context of
> numerical analysis."*

Think of the Sportsbook's line as the **"Public Consensus"** — the baseline story everyone
believes.

- **0% Edge:** You agree with the story. (e.g., "The Chiefs are favorites, and they should be.")
- **Your Job:** Find the "Plot Twist" that the public story is missing.

### Tier 1: The 5% Edge (The "Nudge")

**The Feeling:** *"The market is a little lazy here."* You aren't predicting a miracle; you are
noticing a small detail that tilts the scale. The sportsbook is using a general average, but you
found a specific context.

- **Qualitative Evidence:**
  - **The Motivation Spot:** A WR is 10 yards away from a contract bonus in Week 17.
  - **The "Fine Print" Injury:** The starting Left Tackle is active but playing with a cast on
    his hand (holding calls or sacks are more likely).
  - **Weather Nuance:** The forecast says "windy," but you checked the stadium orientation and
    know it blocks North/South winds (meaning the Over is safer than the public thinks).
- **In Plain English:** "This event is slightly more likely than a coin flip because of one
  specific detail the algorithm missed."

### Tier 2: The 10% Edge (The "Angle")

**The Feeling:** *"They are pricing this based on who he WAS, not who he IS."* This is a
significant disagreement. You believe the fundamental role or usage of a player has changed, and
the book hasn't caught up yet.

- **Qualitative Evidence:**
  - **The Role Change:** A RB played 70% of snaps last week (up from 40%) because the coach
    finally trusts him to block, but the line is still set at his "40% snap" average.
  - **The Scheme Mismatch:** A TE is facing a defense that plays "Cover 2" (zone) 60% of the
    time, and this specific TE targets the "turkey hole" in Cover 2 zones exclusively.
  - **The "Next Man Up" Fallacy:** The book assumes the backup QB will be terrible, but you know
    the backup is a "check-down merchant," which actually *boosts* the RB's receiving yards prop.
- **In Plain English:** "The story has changed, but the price is still reflecting the old
  chapters."

### Tier 3: The 15% Edge (The "Error")

**The Feeling:** *"This is a mistake."* These are rare. This happens when you have information
that completely breaks the model. This is usually due to breaking news or a massive overreaction
by the public.

- **Qualitative Evidence:**
  - **The Usage Vacuum:** The WR1 just got ruled out 5 minutes ago. The WR2 (who is priced at 30
    yards) will now see 10 targets. The math is simply broken.
  - **The "Trash Time" Correlation:** You are betting on a QB to throw an interception, and you
    know his team is a 14-point underdog. He will be forced to throw deep passes while trailing
    in the 4th quarter against a "Prevent Defense" that baits picks. The game script *guarantees*
    risky behavior.
- **In Plain English:** "The conditions for this bet winning are almost guaranteed to occur given
  the game script."

### Visualizing the "Confidence Ladder"

*[Image of a stepped pyramid chart titled 'The Confidence Ladder'.*

- **Base Layer (Wide & Green):** "The Longshots (+200 to +500)". Text: *Low Confidence
  Required.* "Narrative: 'It's possible.'"
- **Middle Layer (Medium & Yellow):** "The Coin Flips (−110)". Text: *Medium Confidence
  Required.* "Narrative: 'It's Probable.'"
- **Top Layer (Narrow & Red):** "The Safety Tax (−200)". Text: *Extreme Confidence Required.*
  "Narrative: 'It's Inevitable.'"
- **Side Arrow pointing up:** Label "Required Evidence Strength" increasing as you go up.*]

### Summary Table: The "Evidence" Test

*Before placing a bet, ask yourself: Which row does my evidence fit into?*

| Edge Tier | Percentage | The "Evidence" Required | The "Vibe" |
|---|---|---|---|
| None | 0% | "I think he's a good player." | **Fan** (Do… `[clipped]`) |
| Tier 1 | 5% | "The wind is 15mph, hindering deep passes." | **Observ…** `[clipped]` |
| Tier 2 | 10% | "The defense plays Man Coverage; he excels vs Man." | **Analyst** `[clipped]` |
| Tier 3 | 15% | "WR1 is out; he is the only healthy target left." | **Insider**, aggress… `[clipped]` |

---

## Part I–IV: The consolidated report (pp.30–36)

> *Prompt: "I want to synthesize all of this material into a single report called 'The Edge of
> Vigor'"*, then *"I want to substitute the mental model examples from the new report with the
> original examples from the existing response."*
>
> This final version supersedes the near-identical draft on pp.24–29.

### THE EDGE OF VIGOR
#### A Layman's Guide to Probability, Pricing, and Profit in the NFL Market

### Part I: The Invisible Tax

**"Vigorish" (n.):** *From Yiddish 'vigrish', meaning "winnings" or "profit".* The fee charged by
a bookmaker for accepting a wager. It is the mathematical certainty that ensures the house wins
if you break even.

#### 1. The "Minus" Line (−110)

The standard price for an NFL spread or player prop is **−110**.

- **The Transaction:** You pay $110 to win $100.
- **The Implied Probability:** 52.38%
- **The Reality:** In a fair world (a coin flip), the price would be **+100** (bet $100 to win
  $100). The difference between 50.0% and 52.38% is the **Vig**.
- **The Golden Rule:** To be profitable at −110, you must be right **>52.4%** of the time. If you
  hit exactly 50% of your bets, you will go bankrupt due to the vig.

#### 2. The "Plus" Line (+140)

These are "Underdog" or "Alternate" lines.

- **The Transaction:** You pay $100 to win $140.
- **The Implied Probability:** 41.6%
- **The Reality:** The bookmaker likely believes the event has a ~38% chance of happening. They
  sell it to you at a price implying 41.6%. You profit if the **True Probability** is actually
  42% or higher.

> **Verification note.** This passage is the one place the document acknowledges that the book's
> implied probability sits *above* its own true estimate — i.e. de-vigging. It never carries the
> idea into the tables. See [Verification](#verification), item 1.

### Part II: The Strategies of Value

You cannot beat the vig by guessing. You beat it by finding pricing errors.

#### Strategy A: The "Market Width" Test

**Concept:** Market Width is the gap between the "Over" price and the "Under" price. It reveals
the Bookmaker's confidence.

- **Tight Market (High Confidence):**
  - *Over 22.5 (−110) / Under 22.5 (−110)*
  - Gap: 20 cents.
  - *Meaning:* The book is confident. It is hard to find an edge here.
- **Wide Market (Low Confidence):**
  - *Over 22.5 (−135) / Under 22.5 (+105)*
  - Gap: 40 cents.
  - *Meaning:* The book is unsure. They have increased the vig (fee) to protect themselves from
    volatility. **This is where errors live.**

#### Strategy B: The "Analytical Hobbyist" Method

1. **Find the Line:** e.g., *Derrick Henry Over 80.5 Rush Yards (−115)*.
2. **Convert to Implied Prob:** 53.5%.
3. **Consult Reality:** Your data (average yards, defense rank, etc.) suggests he hits this
   **60%** of the time.
4. **Calculate Edge:** 60% (Your Prob) − 53.5% (Implied Prob) = **6.5% Edge**.
5. **Action:** This is a Positive EV wager.

### Part III: The Reference Tables

#### Table 1: The Breakeven "Hurdle Rate"

| Line | Implied Prob (The Hurdle) | Difficulty Rating |
|---|---|---|
| −300 | 75.0% | Extreme |
| −200 | 66.7% | High |
| −150 | 60.0% | Moderate |
| −110 | 52.4% | Standard |
| +100 | 50.0% | Coin Flip |
| +150 | 40.0% | Underdog |
| +200 | 33.3% | 2-to-1 |
| +400 | 20.0% | Longshot |
| +1000 | 9.1% | Lottery |

#### Table 2: Expected Value (EV) Matrix

- **Tier 1 (Nudge):** You are 5% more confident than the line.
- **Tier 2 (Angle):** You are 10% more confident than the line.
- **Tier 3 (Error):** You are 15% more confident than the line.

**For a $5.00 Wager**

| Line | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) | EV @ Tier 3 (+15%) |
|---|---|---|---|
| −200 | +$0.38 | +$0.75 | +$1.13 |
| −150 | +$0.42 | +$0.83 | +$1.25 |
| **−110** | **+$0.48** | **+$0.95** | +$1.43 |
| +100 | +$0.50 | +$1.00 | +$1.50 |
| +200 | +$0.75 | +$1.50 | +$2.25 |
| +400 | +$1.25 | +$2.50 | +$3.75 |
| +1000 | +$2.75 | +$5.50 | +$8.25 |

**For a $10 Bonus Bet (Stake Not Returned)**

*Note: Bonus bets favor Longshots mathematically.*

| Line | EV @ Tier 1 (+5%) | EV @ Tier 2 (+10%) | EV @ Tier 3 (+15%) |
|---|---|---|---|
| −200 | $3.58 | $3.83 | $4.08 |
| **−110** | **$5.22** | **$5.67** | $6.13 |
| **+200** | **$7.67** | **$8.67** | $9.67 |
| **+400** | **$10.83** | **$13.33** | $15.83 |

✅ Both Part III tables verified correct. This version omits the −500/−400/−300 rows that are
wrong in Tables D and E.

### Part IV: The Mental Model

**How to Justify Your "Edge."** When you say, "I have a 10% edge," you must have evidence to back
it up. Use the **Confidence Ladder**:

#### 1. The Nudge (Tier 1: +5% Edge)

- **The Feeling:** *"The market is a little lazy here."*
- **The Evidence Needed:** The Motivation Spot; The "Fine Print" Injury; Weather Nuance. *(Same
  three examples as Part 0e above.)*
- **Verdict:** Bet Small.

#### 2. The Angle (Tier 2: +10% Edge)

- **The Feeling:** *"They are pricing this based on who he WAS, not who he IS."*
- **The Evidence Needed:** The Role Change; The Scheme Mismatch; The "Next Man Up" Fallacy.
  *(Same three examples as Part 0e above.)*
- **Verdict:** Bet Standard.

#### 3. The Error (Tier 3: +15% Edge)

- **The Feeling:** *"This is a mistake."*
- **The Evidence Needed:** The Usage Vacuum; The "Trash Time" Correlation. *(Same two examples as
  Part 0e above.)*
- **Verdict:** Bet Aggressively.

**Final Directive:**
Always compare your **True Probability** (derived from research) against the **Implied
Probability** (derived from the odds). If `True > Implied`, you have found the Edge of Vigor.

---

## Verification

Every numeric claim in this document was independently recomputed. Findings:

**Correct:** the implied-probability formulas; the Breakeven tables; all 14 Profitability Matrix
cells; the Key Takeaways; Scenarios A/B/C; the Safety Tax table; Tables A, B, C in full; Tables D
and E except as noted; both Part III tables. The document is unusually consistent arithmetically.

**One error:** rows −500 / −400 / −300 of bonus-bet Tables D and E use the −200 profit multiplier.
Flagged inline above with corrected values.

**Four methodology gaps**, carried into
[`edge/app/internal/wager`](../../app/internal/wager/) as tests:

1. **No de-vigging.** The Final Directive — *"compare True Probability against Implied
   Probability. If `True > Implied`, you have found the Edge of Vigor"* — uses the book's **raw**
   implied probability. That is correct as a *breakeven hurdle*, and the document's "Hurdle Rate"
   naming is right. But raw implied is not the market's estimate of true probability; both sides
   sum to >100%. At −110/−110 the market's view is 50/50, not 52.4/52.4. Beating the hurdle by 0.6
   points and disagreeing with the market by 3 points are different claims, and only the first is
   ever surfaced. Part I p.31 shows the author knows this (+140 priced at 41.6% vs a believed
   ~38%) but the tables never apply it.
2. **Calibration is never addressed.** Every output is exactly as good as `p_true`. At +150 a
   5-point estimation error swings EV by 12.5 points — larger than most edges these tables exist
   to detect. There is no guidance on validating an estimate, tracking hit rate against predicted
   rate, or sizing down under uncertainty. The tables' precision invites more confidence than the
   inputs support.
3. **"Underdogs Scale Fast"** is valid arithmetic aimed at the worst segment to be wrong in:
   longshot markets carry the highest vig, and the historical favorite–longshot bias means
   longshots are chronically *overbet*.
4. **Overround vs. hold.** "That extra 4.8% is the Vig" conflates two numbers: 4.8 points is the
   overround; hold on balanced action is 4.8 / 104.8 = **4.58%**.
5. **Market Width is self-contradictory.** Part 0 says wide markets warrant caution ("you have to
   be even *more* right"); Part II says wide markets are "where errors live." Both are defensible
   — wider does mean more juice *and* less book confidence — but the document never reconciles
   them, and they imply opposite actions.

**Bonus bets:** the source's `EV = p × profit` is correct. The closed form it misses is that at
fair odds `EV_BB = stake × (1 − p)` — bonus-bet value equals stake times the *loss* probability.
This reproduces every correct row in Tables D and E and makes "longshots convert better" a
one-liner. Two caveats the document does not state: its implied ~90% conversion at +1000 is a
**ceiling at fair odds** (real longshot markets are juiced, so realized conversion is typically
60–75%), and a longshot bonus-bet strategy wins <10% of the time, so with a handful of bonus bets
per season the *median* outcome is zero regardless of EV.

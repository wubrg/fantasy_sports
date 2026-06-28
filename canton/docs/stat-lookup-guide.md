# NFL Player Stat Lookup Guide
**Version:** 1.0  
**Created:** 2026-06-13  
**Purpose:** Authoritative reference for NFL player season stat lookups. All player stat responses must follow this guide.

---

## Lookup Format

When a player is requested using the format `<player name> <position> <year>`, always return:

1. **Full season-long stats** for that player at that position for that year (regular season)
2. **Any awards earned** that season (see Award Types below)
3. **Position group context** — list other players at the same position that year who:
   - Won any award (MVP, OPOY, DPOY, ROTY, etc.)
   - Were named to the Pro Bowl
   - Were named All-Pro (1st or 2nd team)
   - Led the league in any stat category relevant to that position

> **Sources must be used for all lookups.** Do not answer from memory alone. Always verify stats and awards against the Tier 1 sources below.

---

## Source Priority

### Tier 1 — Always Check First

#### 1. Pro-Football-Reference.com (PFR)
**URL:** https://www.pro-football-reference.com  
**Use for:** Season stats (1920–present), awards, Pro Bowl, All-Pro, league leaders, stat exports

- **Player page pattern:** `pro-football-reference.com/players/[LAST_INITIAL]/[LastnameFirstname00].htm`
- **Season leaders by year:** `pro-football-reference.com/years/[YEAR]/[CATEGORY].htm`
  - Categories: `passing`, `rushing`, `receiving`, `defense`, `kicking`, `scoring`
- **Awards index:** `pro-football-reference.com/awards/`
- **Pro Bowl by year:** `pro-football-reference.com/pro-bowl/[YEAR].htm`
- **All-Pro by year:** `pro-football-reference.com/years/[YEAR]/allpro.htm`
- Data is exportable via the Share/Export → CSV option on any table
- Free tier covers all standard lookups; Stathead needed for custom cross-filters

#### 2. NFL.com — Official Stats
**URL:** https://www.nfl.com/stats/player-stats/  
**Use for:** Official verification, current season stats, player index  
- Best for recent seasons (approx. 2010+); limited historical depth
- Use to cross-check PFR on current/recent seasons

---

### Tier 2 — Backup / Cross-Check

#### 3. The Football Database (FootballDB)
**URL:** https://www.footballdb.com  
**Use for:** Quick category leaders, single-season records, clean historical stat tables  
- Good for rushing/receiving/passing single-season leaders by year
- Less granular than PFR but fast and clean

#### 4. ESPN NFL Stats
**URL:** https://www.espn.com/nfl/stats  
**Use for:** Current season, position-filtered quick lookups, sanity checks  
- Limited historical depth; use PFR for anything pre-2010

---

## Source Priority Summary

| Need | Primary | Backup |
|---|---|---|
| Season stats (any year) | PFR | FootballDB |
| Pro Bowl selections | PFR Awards Index | NFL.com |
| All-Pro (1st/2nd team) | PFR Awards Index | Wikipedia (year's All-Pro article) |
| League stat leaders by position | PFR season leaders | FootballDB leaders |
| MVP / OPOY / DPOY / ROTY | PFR Awards Index | NFL.com |
| Current season verification | NFL.com | ESPN |

---

## Award Types to Check

For every player lookup, check PFR's awards index and player page for:

| Award | Org | Notes |
|---|---|---|
| AP MVP | Associated Press | League's most prestigious individual award |
| Offensive Player of the Year | AP | |
| Defensive Player of the Year | AP | |
| Offensive Rookie of the Year | AP | |
| Defensive Rookie of the Year | AP | |
| Comeback Player of the Year | AP | |
| All-Pro 1st Team | AP | Most recognized All-Pro designation |
| All-Pro 2nd Team | AP | |
| Pro Bowl | NFL | Note: replaced by Pro Bowl Games format in 2023 |
| Super Bowl MVP | NFL | If applicable to that season |

---

## Position-Specific Stat Categories

Pull all relevant stats for the player's position. Use these as the baseline:

### QB (Quarterback)
Games, Games Started, Completions, Attempts, Completion %, Passing Yards, Yards/Attempt, TD, INT, Passer Rating, Sacks, Rushing Att, Rushing Yards, Rushing TD

### RB (Running Back)
Games, Games Started, Rush Att, Rush Yards, Yards/Carry, Rush TD, Targets, Receptions, Rec Yards, Rec TD, Fumbles

### WR (Wide Receiver)
Games, Games Started, Targets, Receptions, Rec Yards, Yards/Reception, Yards/Target, Rec TD, Longest Reception, Fumbles

### TE (Tight End)
Games, Games Started, Targets, Receptions, Rec Yards, Yards/Reception, Rec TD, Fumbles

### OL (Offensive Line — G/C/T)
Games, Games Started, Sacks Allowed (if available via PFR advanced OL stats)  
*Note: PFR has limited individual OL stats; use awards/Pro Bowl context heavily for OL lookups*

### DE / DL (Defensive Line)
Games, Games Started, Sacks, Tackles (Solo + Assisted), TFL, QB Hits, Forced Fumbles, Fumble Recoveries

### LB (Linebacker)
Games, Games Started, Tackles, Sacks, TFL, INT, PD, Forced Fumbles

### CB (Cornerback)
Games, Games Started, Tackles, INT, INT Return Yards, PD, Forced Fumbles, Targets Allowed (if available via PFR)

### S (Safety)
Games, Games Started, Tackles, INT, INT Return Yards, PD, Sacks, Forced Fumbles

### K (Kicker)
FG Made, FG Att, FG %, FG Long, XP Made, XP Att, XP %, Points

### P (Punter)
Punts, Punt Yards, Yards/Punt, Net Yards/Punt, TB, Inside 20, Longest Punt, Blocked

---

## Position Group Context Rules

When returning a player's stats, also list the following for the **same year, same position group**:

1. **Award winners** at that position (Pro Bowl, All-Pro, AP awards)
2. **Statistical leaders** in the top 1–3 relevant categories for that position
3. **Notable peers** if the target player ranked highly — e.g., "ranked 3rd in the NFL in receiving yards"

This provides context for evaluating the player's season relative to their peers.

---

## Change Log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-06-13 | Initial document created. Sources, lookup format, position stat categories, award types defined. |

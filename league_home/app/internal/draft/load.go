package draft

import (
	"fmt"

	"leaguehome/internal/sleeper"
)

// regularSeasonWeeks is how many weeks of transactions to pull per season.
// Sleeper returns an empty list for weeks that never happened, so
// overshooting is harmless and avoids missing late-season waiver claims.
const regularSeasonWeeks = 18

// LoadSeasons walks the league's previous_league_id chain back from
// leagueID and assembles one SeasonData per season that has a completed
// draft, newest league first but returned in ascending year order.
//
// Seasons whose recorded data fails DetectAnomalies are marked Corrupt so
// the ledger can flag anything chained through them rather than trusting
// numbers that are known wrong.
func LoadSeasons(c *sleeper.Client, leagueID string, rules Rules) ([]SeasonData, error) {
	var out []SeasonData
	seen := map[string]bool{}

	for id := leagueID; id != "" && id != "0"; {
		if seen[id] {
			return nil, fmt.Errorf("draft: league chain loops at %s", id)
		}
		seen[id] = true

		league, err := c.League(id)
		if err != nil {
			return nil, fmt.Errorf("draft: loading league %s: %w", id, err)
		}
		drafts, err := c.Drafts(id)
		if err != nil {
			return nil, fmt.Errorf("draft: loading drafts for %s: %w", id, err)
		}

		for _, d := range drafts {
			// Only completed drafts contribute history. An abandoned
			// pre_draft shell (the 2021 league has one) carries no picks
			// and would otherwise look like an empty season.
			if d.Status != "complete" {
				continue
			}
			s, err := loadSeason(c, league, d)
			if err != nil {
				return nil, err
			}
			s.Corrupt, s.CorruptReason = DetectAnomalies(s, rules)
			out = append(out, s)
		}
		id = league.PreviousLeagueID
	}

	// BuildLedger sorts too, but callers reading these directly should
	// get them chronologically.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// loadSeason fetches one season's picks, rosters, and transactions.
func loadSeason(c *sleeper.Client, league sleeper.League, d sleeper.Draft) (SeasonData, error) {
	s := SeasonData{Year: d.Season, LeagueID: league.LeagueID}

	picks, err := c.DraftPicks(d.DraftID)
	if err != nil {
		return s, fmt.Errorf("draft: loading picks for %s draft %s: %w", d.Season, d.DraftID, err)
	}
	s.Picks = picks

	rosters, err := c.Rosters(league.LeagueID)
	if err != nil {
		return s, fmt.Errorf("draft: loading rosters for %s: %w", d.Season, err)
	}
	s.Rosters = rosters

	for week := 1; week <= regularSeasonWeeks; week++ {
		txns, err := c.Transactions(league.LeagueID, week)
		if err != nil {
			return s, fmt.Errorf("draft: loading %s week %d transactions: %w", d.Season, week, err)
		}
		s.Transactions = append(s.Transactions, txns...)
	}
	return s, nil
}

// DetectAnomalies reports whether a season's recorded values are
// trustworthy, and why not if they aren't.
//
// This is deliberately rule-derived rather than a hardcoded list of bad
// years. The 2022 Hit or Miss season recorded all 24 keepers at $1 against
// a $10 league floor, which is impossible under the league's own rules —
// so the same check that catches it will catch a future season that breaks
// the same way.
func DetectAnomalies(s SeasonData, rules Rules) (bool, string) {
	var keepers, belowMinimum int
	for _, p := range s.Picks {
		if !p.IsKeeper {
			continue
		}
		keepers++
		if p.Metadata.Dollars() < rules.Minimum {
			belowMinimum++
		}
	}
	if keepers > 0 && belowMinimum == keepers {
		return true, fmt.Sprintf(
			"all %d keepers recorded below the $%d league minimum; keeper values were not set correctly this season",
			keepers, rules.Minimum)
	}
	// A partial breach is worth reporting but doesn't invalidate the
	// season's auction prices, which are what the market model uses.
	if belowMinimum > 0 {
		return false, fmt.Sprintf("%d of %d keepers recorded below the $%d league minimum",
			belowMinimum, keepers, rules.Minimum)
	}
	return false, ""
}

// TotalSpend sums what was charged in a season, split between keepers
// priced under league rules and live auction dollars. Keeper spend uses
// the ledger's computed prices, not Sleeper's recorded amounts.
func TotalSpend(s SeasonData, entries []Entry) (keeperSpend, auctionSpend int) {
	priced := make(map[string]int, len(entries))
	for _, e := range entries {
		priced[e.PlayerID] = e.LeaguePrice
	}
	for _, p := range s.Picks {
		if p.IsKeeper {
			keeperSpend += priced[p.PlayerID]
			continue
		}
		auctionSpend += p.Metadata.Dollars()
	}
	return keeperSpend, auctionSpend
}

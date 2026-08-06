package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"leaguehome/internal/draft"
	"leaguehome/internal/sleeper"
)

// minSpendForUsableSeason is the median team spend below which a season's
// prices cannot be compared with any other.
//
// 2022 sits at $157 against a $200 budget: the league was still working out
// how keepers came off the auction budget, so a third of every roster was
// bought with money that was never really at stake. Including it would drag
// every price threshold down by a quarter for reasons that have nothing to
// do with how anybody drafts now.
const minSpendForUsableSeason = 180

// runCalibrate measures the archetype thresholds against completed drafts.
//
// Exists so the numbers in Archetypes() carry their derivation. A threshold
// nobody can re-derive is a number somebody liked, and the previous set
// described Zero RB rosters this league had built once in three years.
func runCalibrate(leagueID, configDir, dataDir string, seasons []string, includeAll bool) error {
	c := sleeper.New()
	c.HTTPClient = &http.Client{Timeout: 180 * time.Second}

	history, skipped, err := loadTeamSeasons(c, leagueID, seasons, includeAll)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return fmt.Errorf("no usable completed drafts found for league %s", leagueID)
	}
	for _, s := range skipped {
		fmt.Fprintf(os.Stderr, "note: %s\n", s)
	}
	return draft.WriteCalibration(os.Stdout, history, draft.Archetypes())
}

// loadTeamSeasons walks the league chain and pairs each completed draft with
// how that season finished.
func loadTeamSeasons(c *sleeper.Client, leagueID string, want []string, includeAll bool) ([]draft.TeamSeason, []string, error) {
	players, err := c.Players()
	if err != nil {
		return nil, nil, fmt.Errorf("loading player dictionary: %w", err)
	}
	keep := map[string]bool{}
	for _, s := range want {
		keep[s] = true
	}

	var out []draft.TeamSeason
	var notes []string
	seen := map[string]bool{}

	for id := leagueID; id != "" && id != "0"; {
		if seen[id] {
			return nil, nil, fmt.Errorf("league chain loops at %s", id)
		}
		seen[id] = true

		league, err := c.League(id)
		if err != nil {
			return nil, nil, fmt.Errorf("loading league %s: %w", id, err)
		}
		next := league.PreviousLeagueID

		season, teams, err := seasonRosters(c, league, players)
		if err != nil {
			return nil, nil, err
		}
		id = next
		if len(teams) == 0 {
			continue
		}
		if len(keep) > 0 && !keep[season] {
			notes = append(notes, fmt.Sprintf("%s skipped (not requested)", season))
			continue
		}
		if spend := medianSpend(teams); spend < minSpendForUsableSeason && !includeAll {
			notes = append(notes, fmt.Sprintf(
				"%s skipped: median team spent $%d of $200, too little for its prices to compare (-all to include)",
				season, spend))
			continue
		}
		out = append(out, rankBySeasonPoints(teams)...)
	}
	return out, notes, nil
}

// seasonRosters pairs one league year's draft with its final standings.
func seasonRosters(c *sleeper.Client, league sleeper.League, players map[string]sleeper.Player) (string, []draft.TeamSeason, error) {
	drafts, err := c.Drafts(league.LeagueID)
	if err != nil {
		return "", nil, fmt.Errorf("loading drafts for %s: %w", league.Season, err)
	}
	rosters, err := c.Rosters(league.LeagueID)
	if err != nil {
		return "", nil, fmt.Errorf("loading rosters for %s: %w", league.Season, err)
	}

	byOwner := map[string]*draft.TeamSeason{}
	for _, r := range rosters {
		if r.OwnerID == "" {
			continue
		}
		byOwner[r.OwnerID] = &draft.TeamSeason{
			Season: league.Season, OwnerID: r.OwnerID, Points: r.PointsFor(),
		}
	}

	for _, d := range drafts {
		if d.Status != "complete" {
			continue
		}
		picks, err := c.DraftPicks(d.DraftID)
		if err != nil {
			return "", nil, fmt.Errorf("loading picks for draft %s: %w", d.DraftID, err)
		}
		for _, p := range picks {
			ts, ok := byOwner[p.PickedBy]
			if !ok {
				continue
			}
			price := 0
			fmt.Sscanf(p.Metadata.Amount, "%d", &price)
			ts.Picks = append(ts.Picks, draft.DraftedPlayer{
				Name:     players[p.PlayerID].FullName,
				Position: players[p.PlayerID].Position,
				Price:    price,
				Keeper:   p.IsKeeper,
			})
		}
	}

	var out []draft.TeamSeason
	for _, ts := range byOwner {
		// A roster with no picks belongs to an owner who was not in the
		// league that year; the roster row survives, the draft does not.
		if len(ts.Picks) > 0 {
			out = append(out, *ts)
		}
	}
	return league.Season, out, nil
}

// rankBySeasonPoints ranks within the season, best first.
func rankBySeasonPoints(teams []draft.TeamSeason) []draft.TeamSeason {
	sort.SliceStable(teams, func(i, j int) bool { return teams[i].Points > teams[j].Points })
	for i := range teams {
		teams[i].Rank = i + 1
	}
	return teams
}

func medianSpend(teams []draft.TeamSeason) int {
	spends := make([]int, 0, len(teams))
	for _, t := range teams {
		spends = append(spends, t.Spend(""))
	}
	sort.Ints(spends)
	if len(spends) == 0 {
		return 0
	}
	return spends[len(spends)/2]
}

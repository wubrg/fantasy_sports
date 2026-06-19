// Command leaguectl is a CLI front end onto the league_home core data
// layer: live Sleeper standings/matchups/FAAB, plus locally-curated league
// history. It exists to validate the core package against the real league
// before the Discord bot and web UI (which will call the same core
// operations) are built on top of it.
package main

import (
	"flag"
	"fmt"
	"os"

	"leaguehome/internal/core"
)

// defaultLeagueID is the "Hit or Miss" league's current Sleeper league ID
// (see football/readme.md). Override with -league for a different season
// or league.
const defaultLeagueID = "698583839592771584"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "standings":
		cmdStandings(args)
	case "faab":
		cmdFaab(args)
	case "matchups":
		cmdMatchups(args)
	case "history":
		cmdHistory(args)
	case "rules":
		cmdRules(args)
	case "state":
		cmdState(args)
	case "-h", "-help", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `leaguectl - query the Hit or Miss league home data layer

Usage:
  leaguectl standings [-league ID]          Current standings
  leaguectl faab [-league ID]               FAAB (waiver budget) balances
  leaguectl matchups [-league ID] -week N    Matchups for a given week
  leaguectl history                         Award and league-role history
  leaguectl rules                           Current roster/keeper/waiver/draft rules
  leaguectl state                           Current NFL season/week

Flags default -league to the Hit or Miss league.
`)
}

func cmdStandings(args []string) {
	fs := flag.NewFlagSet("standings", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Standings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "standings failed: %v\n", err)
		os.Exit(1)
	}
	for i, r := range rows {
		fmt.Printf("%2d. %-25s %d-%d-%d  PF %.2f  PA %.2f\n",
			i+1, r.Team, r.Wins, r.Losses, r.Ties, r.PointsFor, r.PointsAgainst)
	}
}

func cmdFaab(args []string) {
	fs := flag.NewFlagSet("faab", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Faab()
	if err != nil {
		fmt.Fprintf(os.Stderr, "faab failed: %v\n", err)
		os.Exit(1)
	}
	for _, r := range rows {
		fmt.Printf("%-25s remaining %3d / %3d (used %d)\n", r.Team, r.Remaining, r.Budget, r.Used)
	}
}

func cmdMatchups(args []string) {
	fs := flag.NewFlagSet("matchups", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	week := fs.Int("week", 0, "week number")
	fs.Parse(args)

	if *week == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -week")
		os.Exit(1)
	}

	rows, err := core.New(*leagueID).Matchups(*week)
	if err != nil {
		fmt.Fprintf(os.Stderr, "matchups failed: %v\n", err)
		os.Exit(1)
	}
	for _, m := range rows {
		fmt.Printf("%-25s %6.2f  vs  %6.2f  %s\n", m.Home, m.HomePoints, m.AwayPoints, m.Away)
	}
}

func cmdHistory(args []string) {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	h, err := core.New(*leagueID).History()
	if err != nil {
		fmt.Fprintf(os.Stderr, "history failed: %v\n", err)
		os.Exit(1)
	}
	for _, a := range h.Awards {
		fmt.Printf("%d  Grand Champion: %-30s Sack-O: %s\n", a.Season, a.GrandChampion, a.SackO)
	}
}

func cmdRules(args []string) {
	fs := flag.NewFlagSet("rules", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	r, err := core.New(*leagueID).Rules()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rules failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting lineup:")
	for _, slot := range r.Roster.StartingSlots {
		fmt.Printf("  %-10s starters=%d max_on_roster=%d\n", slot.Position, slot.Starters, slot.MaxOnRoster)
	}
	fmt.Printf("Bench: %d  IR: %d\n\n", r.Roster.BenchSlots, r.Roster.IRSlots)

	fmt.Printf("Keepers: max %d, minimum value $%d, +$%d per keep count\n",
		r.Keepers.MaxKeepers, r.Keepers.MinimumValue, r.Keepers.IncrementPerKeepCount)
	fmt.Printf("Waivers: $%d budget, $%d minimum bid, %s\n",
		r.Waivers.YearlyBudget, r.Waivers.MinimumBid, r.Waivers.ProcessingSchedule)
	fmt.Printf("Draft: %s, $%d base budget\n", r.Draft.Format, r.Draft.BaseBudget)
	fmt.Printf("Trade deadline: start of week %d\n\n", r.TradeDeadlineWeek)

	fmt.Println("Playoffs:")
	for _, p := range r.Playoffs {
		fmt.Printf("  %d-team league: weeks %d-%d, %d teams (%d byes)\n",
			p.LeagueSize, p.StartWeek, p.EndWeek, p.PlayoffTeams, p.ByeTeams)
	}
}

func cmdState(args []string) {
	fs := flag.NewFlagSet("state", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	st, err := core.New(*leagueID).State()
	if err != nil {
		fmt.Fprintf(os.Stderr, "state failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("season %s (%s), week %d\n", st.Season, st.SeasonType, st.Week)
}

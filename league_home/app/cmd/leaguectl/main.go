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
// (see leagues/hit_or_miss/readme.md). Override with -league for a different season
// or league.
const defaultLeagueID = "1368649414419189760"

// defaultESPNLeagueID is the "Hit or Miss" league's ESPN league ID, from
// before it migrated to Sleeper. Override with -espn-league for a
// different league. Unlike Sleeper's API, reading this league's history
// requires auth: set ESPN_S2 and ESPN_SWID (the espn_s2/SWID cookie values
// from a league member's logged-in browser session) before running any
// espn-* command, or every one of them will fail.
const defaultESPNLeagueID = "56226"

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
	case "scoring":
		cmdScoring(args)
	case "managers":
		cmdManagers(args)
	case "announcements":
		cmdAnnouncements(args)
	case "schedule":
		cmdSchedule(args)
	case "rivalries":
		cmdRivalries(args)
	case "state":
		cmdState(args)
	case "seasons":
		cmdSeasons(args)
	case "espn-seasons":
		cmdEspnSeasons(args)
	case "espn-standings":
		cmdEspnStandings(args)
	case "espn-matchups":
		cmdEspnMatchups(args)
	case "espn-draft":
		cmdEspnDraft(args)
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
  leaguectl scoring [-league ID]            Live scoring settings (from Sleeper)
  leaguectl managers                        All managers, past and present
  leaguectl announcements                   League announcements (placeholder data)
  leaguectl schedule                        Season calendar events
  leaguectl rivalries                       Manager head-to-head records (not yet populated)
  leaguectl state                           Current NFL season/week
  leaguectl seasons [-league ID]            This league's seasons on Sleeper, most recent first

  leaguectl espn-seasons [-espn-league ID]                 This league's seasons on ESPN, most recent first
  leaguectl espn-standings [-espn-league ID] -year Y        Final standings for an ESPN-era season
  leaguectl espn-matchups [-espn-league ID] -year Y -week N Matchups for a week of an ESPN-era season
  leaguectl espn-draft [-espn-league ID] -year Y             Draft results for an ESPN-era season

Flags default -league to the Hit or Miss league. Pass a season's league ID
(see "seasons") to any -league flag above to query that season instead.

espn-* commands cover the league's pre-Sleeper history and need ESPN auth:
set ESPN_S2 and ESPN_SWID (the espn_s2/SWID cookie values from a league
member's logged-in browser session at fantasy.espn.com) before running
them, since ESPN has no keyless public read access the way Sleeper does.
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

func cmdScoring(args []string) {
	fs := flag.NewFlagSet("scoring", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	categories, err := core.New(*leagueID).Scoring()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scoring failed: %v\n", err)
		os.Exit(1)
	}
	for _, c := range categories {
		fmt.Printf("%s:\n", c.Name)
		for _, e := range c.Entries {
			fmt.Printf("  %-40s %+g\n", e.Label, e.Points)
		}
	}
}

func cmdManagers(args []string) {
	fs := flag.NewFlagSet("managers", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	managers, err := core.New(*leagueID).Managers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "managers failed: %v\n", err)
		os.Exit(1)
	}
	for _, m := range managers {
		status := "active"
		if !m.Active {
			status = "inactive"
		}
		aliases := ""
		if len(m.Aliases) > 0 {
			aliases = fmt.Sprintf(" (aka %v)", m.Aliases)
		}
		fmt.Printf("%-20s %-9s%s\n", m.Name, status, aliases)
	}
}

func cmdAnnouncements(args []string) {
	fs := flag.NewFlagSet("announcements", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Announcements()
	if err != nil {
		fmt.Fprintf(os.Stderr, "announcements failed: %v\n", err)
		os.Exit(1)
	}
	for _, a := range rows {
		fmt.Printf("[%s] %s - %s\n  %s\n", a.PostedAt, a.Title, a.Author, a.Body)
	}
}

func cmdSchedule(args []string) {
	fs := flag.NewFlagSet("schedule", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Schedule()
	if err != nil {
		fmt.Fprintf(os.Stderr, "schedule failed: %v\n", err)
		os.Exit(1)
	}
	for _, e := range rows {
		switch {
		case e.Recurring:
			fmt.Printf("%-20s (recurring)  %s\n", e.Label, e.Detail)
		case e.Week > 0:
			fmt.Printf("%-20s (week %d)    %s\n", e.Label, e.Week, e.Detail)
		default:
			fmt.Printf("%-20s              %s\n", e.Label, e.Detail)
		}
	}
}

func cmdRivalries(args []string) {
	fs := flag.NewFlagSet("rivalries", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Rivalries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rivalries failed: %v\n", err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("No rivalry data yet (needs live Sleeper history to compute).")
		return
	}
	for _, r := range rows {
		fmt.Printf("%s vs %s: %d-%d-%d  (PF %.2f vs %.2f)\n",
			r.ManagerAID, r.ManagerBID, r.WinsA, r.WinsB, r.Ties, r.PointsForA, r.PointsForB)
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

func cmdSeasons(args []string) {
	fs := flag.NewFlagSet("seasons", flag.ExitOnError)
	leagueID := fs.String("league", defaultLeagueID, "sleeper league id")
	fs.Parse(args)

	rows, err := core.New(*leagueID).Seasons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "seasons failed: %v\n", err)
		os.Exit(1)
	}
	for _, s := range rows {
		fmt.Printf("%s  %-10s  %s\n", s.Season, s.Status, s.LeagueID)
	}
}

// espnService builds a core.Service configured for ESPN history, reading
// the espn_s2/SWID auth cookies from the environment rather than flags so
// they don't end up in shell history or process listings.
func espnService(espnLeagueID string) *core.Service {
	return core.New(defaultLeagueID).WithESPN(espnLeagueID, os.Getenv("ESPN_S2"), os.Getenv("ESPN_SWID"))
}

func cmdEspnSeasons(args []string) {
	fs := flag.NewFlagSet("espn-seasons", flag.ExitOnError)
	espnLeagueID := fs.String("espn-league", defaultESPNLeagueID, "ESPN league id")
	fs.Parse(args)

	years, err := espnService(*espnLeagueID).HistoricalSeasons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "espn-seasons failed: %v\n", err)
		os.Exit(1)
	}
	for _, y := range years {
		fmt.Println(y)
	}
}

func cmdEspnStandings(args []string) {
	fs := flag.NewFlagSet("espn-standings", flag.ExitOnError)
	espnLeagueID := fs.String("espn-league", defaultESPNLeagueID, "ESPN league id")
	year := fs.Int("year", 0, "season year")
	fs.Parse(args)

	if *year == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -year")
		os.Exit(1)
	}

	rows, err := espnService(*espnLeagueID).HistoricalStandings(*year)
	if err != nil {
		fmt.Fprintf(os.Stderr, "espn-standings failed: %v\n", err)
		os.Exit(1)
	}
	for i, r := range rows {
		fmt.Printf("%2d. %-25s %d-%d-%d  PF %.2f  PA %.2f\n",
			i+1, r.Team, r.Wins, r.Losses, r.Ties, r.PointsFor, r.PointsAgainst)
	}
}

func cmdEspnMatchups(args []string) {
	fs := flag.NewFlagSet("espn-matchups", flag.ExitOnError)
	espnLeagueID := fs.String("espn-league", defaultESPNLeagueID, "ESPN league id")
	year := fs.Int("year", 0, "season year")
	week := fs.Int("week", 0, "week number")
	fs.Parse(args)

	if *year == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -year")
		os.Exit(1)
	}
	if *week == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -week")
		os.Exit(1)
	}

	rows, err := espnService(*espnLeagueID).HistoricalMatchups(*year, *week)
	if err != nil {
		fmt.Fprintf(os.Stderr, "espn-matchups failed: %v\n", err)
		os.Exit(1)
	}
	for _, m := range rows {
		fmt.Printf("%-25s %6.2f  vs  %6.2f  %s\n", m.Home, m.HomePoints, m.AwayPoints, m.Away)
	}
}

func cmdEspnDraft(args []string) {
	fs := flag.NewFlagSet("espn-draft", flag.ExitOnError)
	espnLeagueID := fs.String("espn-league", defaultESPNLeagueID, "ESPN league id")
	year := fs.Int("year", 0, "season year")
	fs.Parse(args)

	if *year == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -year")
		os.Exit(1)
	}

	picks, err := espnService(*espnLeagueID).HistoricalDraft(*year)
	if err != nil {
		fmt.Fprintf(os.Stderr, "espn-draft failed: %v\n", err)
		os.Exit(1)
	}
	for _, p := range picks {
		fmt.Printf("%3d. (round %d, pick %d) %-25s player #%d\n", p.Overall, p.Round, p.Pick, p.Team, p.PlayerID)
	}
}

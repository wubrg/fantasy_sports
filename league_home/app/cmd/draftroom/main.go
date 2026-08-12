// Command draftroom is the Hit or Miss auction draft companion: keeper
// accounting before the draft, and a live board during it.
//
// It shares internal/sleeper and the league's own rules with the rest of
// League Home, and owns no league-data logic of its own beyond what lives
// in internal/draft.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"leaguehome/internal/draft"
	"leaguehome/internal/sleeper"
)

// defaultLeagueID is the "Hit or Miss" league's current Sleeper league ID
// (see leagues/hit_or_miss/readme.md). Override with the LEAGUE_ID
// environment variable or -league.
const defaultLeagueID = "1368649414419189760"

// teams and fullBudget describe the league's auction. Both are read back
// from Sleeper when a draft is available; these are only fallbacks.
const (
	defaultTeams  = 12
	defaultBudget = 200
)

// Hand-maintained files that ship with this repo, resolved against the
// config directory rather than the working directory.
const (
	rulingsFile = "rulings.csv"
	aliasesFile = "aliases.csv"
	// myGuysFile is the pre-lean-set filename, still read as a fallback so
	// an older config directory does not silently lose every read.
	myGuysFile = "my-guys.csv"
)

// defaultLeanSets is what -leans holds when you say nothing: your own reads
// alone. An analyst set only applies when you ask for it by name.
const defaultLeanSets = "mine"

const ()

// maxKeepers is how many players a team may keep, and rosterSize how many
// roster spots each team fills, per leagues/hit_or_miss/rosters.md.
const (
	maxKeepers = 2
	rosterSize = 14
)

// builtinConfigDir and builtinDataDir are baked in by `make install` so an
// installed binary works from any directory without environment variables.
// They are only fallbacks: an explicit flag or env var still wins, and the
// files themselves are read at runtime, so editing leans/mine.csv takes effect
// without rebuilding.
var (
	builtinConfigDir string
	builtinDataDir   string
)

func orBuiltin(explicit, builtin string) string {
	if explicit != "" {
		return explicit
	}
	return builtin
}

func main() {
	log := func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	fs := flag.NewFlagSet("draftroom", flag.ExitOnError)
	leagueID := fs.String("league", envOr("LEAGUE_ID", defaultLeagueID), "Sleeper league ID")
	configDir := fs.String("config", "", "directory holding rulings.csv, aliases.csv and leans/ (default: the repo's copy)")
	dataDir := fs.String("data", "", "private data directory (default: $DRAFTROOM_DATA_DIR or ../fantasy_sports_data)")
	baseline := fs.String("baseline", string(draft.BaselineBEERPlus), "valuation curve: beer, beerplus, or vols")
	limit := fs.Int("limit", 40, "rows to show on the board")
	addr := fs.String("addr", ":8083", "address for the serve command (leagueweb owns :8081)")
	me := fs.String("me", envOr("DRAFTROOM_OWNER_ID", ""), "your Sleeper owner ID, so the board knows your budget")
	leans := fs.String("leans", defaultLeanSets, "lean sets to apply, in precedence order: the first to name a player owns him")
	generate := fs.Bool("generate", false, "leans: rebuild the generated sets from source data")
	convert := fs.Bool("convert", false, "leans: rewrite the named sets as YAML, leaving the originals in place")
	seasons := fs.String("seasons", "2023,2024,2025", "calibrate: seasons to measure, comma separated (empty for every usable one)")
	includeAll := fs.Bool("all", false, "calibrate: measure every completed season, including ones whose prices are not comparable")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: draftroom <command> [flags]\n\ncommands:\n")
		fmt.Fprintf(os.Stderr, "  keepers   reconcile keeper prices against Sleeper and show budgets\n")
		fmt.Fprintf(os.Stderr, "  board     price the live pool and print the draft board\n")
		fmt.Fprintf(os.Stderr, "  serve     serve the board as a web page for a second monitor\n")
		fmt.Fprintf(os.Stderr, "  leans     show the merged lean sets, or -generate to rebuild them\n")
		fmt.Fprintf(os.Stderr, "  calibrate ask whether spending shape predicted results, from past drafts\n\n")
		fs.PrintDefaults()
	}

	args := os.Args[1:]
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	command := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		os.Exit(2)
	}

	switch command {
	case "keepers":
		if err := runKeepers(*leagueID, orBuiltin(*configDir, builtinConfigDir), orBuiltin(*dataDir, builtinDataDir)); err != nil {
			log("draftroom: %v", err)
			os.Exit(1)
		}
	case "board":
		if err := runBoard(*leagueID, orBuiltin(*configDir, builtinConfigDir), orBuiltin(*dataDir, builtinDataDir),
			*me, draft.Baseline(*baseline), *limit, draft.SetNames(*leans)); err != nil {
			log("draftroom: %v", err)
			os.Exit(1)
		}
	case "leans":
		if err := runLeans(orBuiltin(*configDir, builtinConfigDir),
			orBuiltin(*dataDir, builtinDataDir), draft.SetNames(*leans), *generate, *convert); err != nil {
			log("draftroom: %v", err)
			os.Exit(1)
		}
	case "calibrate":
		// Whether -seasons was typed, not just what it holds. -all has to
		// widen the default season list or it does nothing, but widening one
		// the user chose themselves would silently discard their choice.
		named := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "seasons" {
				named = true
			}
		})
		if err := runCalibrate(*leagueID, orBuiltin(*configDir, builtinConfigDir),
			orBuiltin(*dataDir, builtinDataDir), draft.SetNames(*seasons), *includeAll, named); err != nil {
			log("draftroom: %v", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServe(*addr, *leagueID, orBuiltin(*configDir, builtinConfigDir),
			orBuiltin(*dataDir, builtinDataDir), *me, draft.Baseline(*baseline), draft.SetNames(*leans)); err != nil {
			log("draftroom: %v", err)
			os.Exit(1)
		}
	default:
		log("draftroom: unknown command %q", command)
		fs.Usage()
		os.Exit(2)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// runKeepers rebuilds the keeper ledger from every season Sleeper has and
// prints the reconciliation plus the most recent season's budgets.
func runKeepers(leagueID, configDir, dataDir string) error {
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	rulingsPath := filepath.Join(cfg, rulingsFile)
	// The player dictionary and multi-season history are large; the
	// default 10s client timeout is tuned for single small calls.
	c := sleeper.New()
	c.HTTPClient = &http.Client{Timeout: 120 * time.Second}

	rules := draft.DefaultRules()
	seasons, err := draft.LoadSeasons(c, leagueID, rules)
	if err != nil {
		return err
	}
	if len(seasons) == 0 {
		return fmt.Errorf("no completed drafts found for league %s", leagueID)
	}

	// Rulings are optional; a missing file just means the league hasn't
	// had to decide anything the written rules don't cover.
	overrides, err := draft.LoadOverrides(rulingsPath)
	if err != nil {
		return err
	}
	ledger, err := draft.BuildLedgerWithOverrides(seasons, rules, overrides)
	if err != nil {
		return err
	}
	if len(overrides) > 0 {
		fmt.Fprintf(os.Stderr, "applied %d LM ruling(s) from %s\n\n", len(overrides), rulingsPath)
	}

	teams, budget := auctionShape(c, leagueID)
	names, err := ownerNames(c, seasons)
	if err != nil {
		return err
	}

	rec := draft.Reconcile(seasons, ledger, teams, budget)
	if err := rec.WriteText(os.Stdout, names, teams, budget); err != nil {
		return err
	}

	last := ledger.Seasons[len(ledger.Seasons)-1]
	fmt.Printf("\n\n%s BUDGETS UNDER LEAGUE RULES\n", last)
	fmt.Println("(Sleeper shows every team a flat $" + fmt.Sprint(budget) + ", so these differ.)")
	if err := draft.WriteBudgets(os.Stdout, ledger.EntriesForSeason(last), names, budget); err != nil {
		return err
	}

	// Rosters carry forward between seasons, so before the draft they
	// still hold last year's squads — the pool of keeper candidates.
	upcoming, err := upcomingSeason(c, leagueID, last)
	if err != nil {
		return err
	}
	rosters, err := c.Rosters(leagueID)
	if err != nil {
		return fmt.Errorf("loading current rosters: %w", err)
	}
	info, err := playerInfo(c, last)
	if err != nil {
		return err
	}

	projected, err := draft.Project(ledger, upcoming, rosters, info)
	if err != nil {
		return err
	}

	// A keeper is a purchase, so it needs the same cost and value boards
	// the auction board runs on.
	costs, values, err := sourceBoards(dataDir, cfg, info, teams, budget)
	if err != nil {
		return err
	}
	options := draft.RankKeepers(projected, costs, values, maxKeepers)
	fmt.Printf("\n\n")
	return draft.WriteKeeperOptions(os.Stdout, options, names, upcoming, maxKeepers, budget)
}

// sourceBoards builds the cost and value boards against a full pool, which
// is the right frame for a keeper decision: it asks what a player would go
// for in an auction he is being withheld from.
func sourceBoards(dataDir, cfg string, info map[string]draft.PlayerInfo, teams, budget int) (costs, values map[string]int, err error) {
	root, err := draft.ResolveDataRoot(dataDir)
	if err != nil {
		return nil, nil, err
	}
	ciely, err := draft.LoadSourceCSV(root.Normalized("ciely-2026.csv"))
	if err != nil {
		return nil, nil, err
	}
	sv, err := draft.LoadSourceCSV(root.Normalized("subvertadown-2026.csv"))
	if err != nil {
		return nil, nil, err
	}
	aliases, err := draft.LoadAliases(filepath.Join(cfg, aliasesFile))
	if err != nil {
		return nil, nil, err
	}
	idx := draft.BuildPlayerIndexWithAliases(info, aliases)
	idx.Resolve(ciely)
	idx.Resolve(sv)

	state := draft.HitOrMissPool()
	state.Teams, state.Dollars, state.Slots = teams, teams*budget, teams*14

	var projections []draft.Projection
	for _, r := range ciely {
		if r.PlayerID == "" || r.Position == "DST" {
			continue
		}
		projections = append(projections, draft.Projection{
			PlayerID: r.PlayerID, Name: r.Player, Position: r.Position, Points: r.Points,
		})
	}
	valueBoard, err := draft.Solve(projections, state)
	if err != nil {
		return nil, nil, err
	}
	values = make(map[string]int, len(valueBoard))
	for _, v := range valueBoard {
		values[v.PlayerID] = v.Price
	}

	var market []draft.MarketPrice
	for _, r := range sv {
		if r.Baseline != "beerplus" || r.PlayerID == "" || r.AAV <= 0 {
			continue
		}
		market = append(market, draft.MarketPrice{
			PlayerID: r.PlayerID, Name: r.Player, Position: r.Position, AAV: r.AAV,
		})
	}
	costBoard, err := draft.SolveCost(market, state)
	if err != nil {
		return nil, nil, err
	}
	costs = make(map[string]int, len(costBoard))
	for _, m := range costBoard {
		costs[m.PlayerID] = m.Cost
	}
	return costs, values, nil
}

// upcomingSeason returns the season the next draft belongs to, falling back
// to the season after the last completed one.
func upcomingSeason(c *sleeper.Client, leagueID, lastCompleted string) (string, error) {
	league, err := c.League(leagueID)
	if err != nil {
		return "", fmt.Errorf("loading league: %w", err)
	}
	if league.Season != "" && league.Season != lastCompleted {
		return league.Season, nil
	}
	year, err := strconv.Atoi(lastCompleted)
	if err != nil {
		return "", fmt.Errorf("cannot derive the season after %q", lastCompleted)
	}
	return strconv.Itoa(year + 1), nil
}

// playerInfo joins Sleeper's player dictionary to the prior season's stats,
// so the projection can name players and apply the "must have played last
// season" keeper eligibility rule.
func playerInfo(c *sleeper.Client, priorSeason string) (map[string]draft.PlayerInfo, error) {
	players, err := c.Players()
	if err != nil {
		return nil, fmt.Errorf("loading player dictionary: %w", err)
	}
	stats, err := c.SeasonStats("regular", priorSeason)
	if err != nil {
		return nil, fmt.Errorf("loading %s stats: %w", priorSeason, err)
	}

	out := make(map[string]draft.PlayerInfo, len(players))
	for id, p := range players {
		name := p.FullName
		if name == "" {
			// Team defenses carry no full_name.
			name = strings.TrimSpace(p.FirstName + " " + p.LastName)
		}
		if name == "" {
			name = id
		}
		gp, prior := -1, 0.0
		if s, ok := stats[id]; ok {
			gp, prior = int(s["gp"]), s["pts_half_ppr"]
		}
		out[id] = draft.PlayerInfo{
			Name: name, Position: p.Position, Team: p.Team,
			GamesPlayed: gp, PriorPoints: prior, Injury: p.InjuryStatus,
		}
	}
	return out, nil
}

// auctionShape reads team count and budget off the league's current draft,
// falling back to the league's documented values if it can't.
func auctionShape(c *sleeper.Client, leagueID string) (teams, budget int) {
	teams, budget = defaultTeams, defaultBudget
	drafts, err := c.Drafts(leagueID)
	if err != nil || len(drafts) == 0 {
		return teams, budget
	}
	if d := drafts[0]; d.Settings.Teams > 0 && d.Settings.Budget > 0 {
		return d.Settings.Teams, d.Settings.Budget
	}
	return teams, budget
}

// ownerNames maps owner IDs to team names across every season, so managers
// who have since left the league still render readably.
func ownerNames(c *sleeper.Client, seasons []draft.SeasonData) (draft.Names, error) {
	names := draft.Names{}
	seen := map[string]bool{}
	for _, s := range seasons {
		if seen[s.LeagueID] {
			continue
		}
		seen[s.LeagueID] = true

		users, err := c.Users(s.LeagueID)
		if err != nil {
			return nil, fmt.Errorf("loading users for %s: %w", s.Year, err)
		}
		for _, u := range users {
			// Later seasons are processed first, so only fill gaps to
			// keep the most recent team name for each manager.
			if _, ok := names[u.UserID]; !ok {
				names[u.UserID] = u.TeamName()
			}
		}
	}
	return names, nil
}

// buildSnapshot assembles the whole draft picture for a one-shot command.
//
// Delegates to the same loader and builder the server uses, so the printed
// board and the web board cannot disagree about anything.
func buildSnapshot(leagueID, configDir, dataDir, ownerID string, baseline draft.Baseline, leanSets []string) (draft.Snapshot, error) {
	static, err := loadStatic(leagueID, configDir, dataDir, ownerID, baseline, leanSets)
	if err != nil {
		return draft.Snapshot{}, err
	}
	// A one-shot board reflects whatever has already been drafted.
	taken := map[string]gone{}
	if picks, err := static.Picks(); err == nil {
		for _, p := range picks {
			if p.PlayerID == "" {
				continue
			}
			taken[p.PlayerID] = gone{
				price: p.Metadata.Dollars(),
				mine:  p.PickedBy != "" && p.PickedBy == ownerID,
			}
		}
	}
	return static.Build(taken, nil)
}

// runBoard prints the draft board.
func runBoard(leagueID, configDir, dataDir, ownerID string, baseline draft.Baseline, limit int, leanSets []string) error {
	snap, err := buildSnapshot(leagueID, configDir, dataDir, ownerID, baseline, leanSets)
	if err != nil {
		return err
	}
	for _, w := range snap.Warnings {
		fmt.Fprintf(os.Stderr, "note: %s\n", w)
	}

	fmt.Printf("BOARD — %s baseline, $%d over %d slots\n", snap.Baseline, snap.Dollars, snap.Slots)
	fmt.Printf("max on one player before the rest of your roster suffers: $%d (%s)\n\n",
		snap.Recommended, snap.Risk.Describe())
	if err := draft.WriteBoard(os.Stdout, snap.Players, snap.Me, snap.MustHaves,
		snap.Pivot, snap.HasPivot, limit); err != nil {
		return err
	}

	fmt.Printf("\n\nPOSITION SCARCITY\n")
	if err := draft.WriteScarcity(os.Stdout, snap.Scarcity); err != nil {
		return err
	}
	fmt.Printf("\n\nPOSITIONAL BIAS — how much of every edge is about the position, not the player\n")
	if err := draft.WritePositionalBias(os.Stdout, snap.Bias); err != nil {
		return err
	}

	ranked := snap.AdjustedEdges(5)
	fmt.Printf("\n\nBEST BUYS — better than their own position, after the bias is removed\n")
	if err := draft.WriteEdges(os.Stdout, ranked, snap.Bias, 12); err != nil {
		return err
	}
	fmt.Printf("\n\nPAYING UP — the market likes them more than their position's baseline explains\n")
	return draft.WriteEdges(os.Stdout, tail(ranked, 10), snap.Bias, 10)
}

// tail returns the last n entries, which for an edge list sorted best-first
// are the players the market prices furthest above their median value.
func tail(edges []draft.PlayerSignals, n int) []draft.PlayerSignals {
	if len(edges) <= n {
		return edges
	}
	out := make([]draft.PlayerSignals, n)
	copy(out, edges[len(edges)-n:])
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// poolAfterKeepers estimates the live auction pool from projected keepers,
// since keepers do not lock until a week before the draft.
//
// Teams keep the players worth most relative to their keeper price, not the
// cheapest ones — an elite back at $74 against a $68 market is a worse keep
// than a $10 receiver worth $30. Surplus is measured against AAV because it
// is the only per-player market reference that does not depend on the pool
// we are trying to compute.
func poolAfterKeepers(projected []draft.Entry, aav map[string]float64, teams, budget int) (dollars, slots int, filled map[string]int) {
	byOwner := map[string][]draft.Entry{}
	for _, e := range projected {
		byOwner[e.OwnerID] = append(byOwner[e.OwnerID], e)
	}

	dollars, slots, filled = teams*budget, teams*rosterSize, map[string]int{}
	for _, list := range byOwner {
		surplus := func(e draft.Entry) float64 { return aav[e.PlayerID] - float64(e.LeaguePrice) }
		sort.Slice(list, func(i, j int) bool { return surplus(list[i]) > surplus(list[j]) })
		for i := 0; i < maxKeepers && i < len(list); i++ {
			// A keeper worth less than he costs is not kept.
			if surplus(list[i]) <= 0 {
				break
			}
			dollars -= list[i].LeaguePrice
			slots--
			filled[list[i].Position]++
		}
	}
	return dollars, slots, filled
}

// myState figures out what you personally bring to the auction, given the
// keepers projected for your team.
func myState(projected []draft.Entry, aav map[string]float64, ownerID string, budget int) draft.MyState {
	me := draft.MyState{
		Budget: budget, OpenSlots: rosterSize,
		StartersNeeded: map[string]int{"QB": 1, "RB": 2, "WR": 3, "TE": 1},
	}
	for _, e := range projectedKeepers(projected, aav, ownerID) {
		me.Budget -= e.LeaguePrice
		me.OpenSlots--
		if n, ok := me.StartersNeeded[e.Position]; ok && n > 0 {
			me.StartersNeeded[e.Position] = n - 1
		}
	}
	return me
}

// projectedKeepers is who an owner is expected to keep: the players whose
// market cost most exceeds what the league will charge, up to the limit.
//
// Split out from myState because the roster shapes need to know *who* is
// kept, not just what it costs. A keeper is part of the finished roster and
// the shape constraints have to see him.
func projectedKeepers(projected []draft.Entry, aav map[string]float64, ownerID string) []draft.Entry {
	if ownerID == "" {
		return nil
	}
	var mine []draft.Entry
	for _, e := range projected {
		if e.OwnerID == ownerID {
			mine = append(mine, e)
		}
	}
	surplus := func(e draft.Entry) float64 { return aav[e.PlayerID] - float64(e.LeaguePrice) }
	sort.Slice(mine, func(i, j int) bool { return surplus(mine[i]) > surplus(mine[j]) })

	var out []draft.Entry
	for i := 0; i < maxKeepers && i < len(mine); i++ {
		if surplus(mine[i]) <= 0 {
			break
		}
		out = append(out, mine[i])
	}
	return out
}

package draft

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"leaguehome/internal/sleeper"
)

// FlagDidNotPlay marks a player with no recorded games in the prior season.
// leagues/hit_or_miss/rosters.md makes those players ineligible to keep, so
// this is a hard rules problem rather than a pricing note.
const FlagDidNotPlay = "ineligible-did-not-play"

// PlayerInfo is the display detail a projection needs about a player.
type PlayerInfo struct {
	Name     string
	Position string
	Team     string
	// GamesPlayed is the prior season's game count, used for the
	// keeper-eligibility rule. Negative means unknown.
	GamesPlayed int
	// PriorPoints is last season's half-PPR total. Sleeper's own scoring
	// rather than this league's, which is close enough to ask whether a
	// projection outruns a player's record and not close enough to price
	// anybody with.
	PriorPoints float64
	// Injury is Sleeper's designation (PUP, IR, Questionable, Out).
	//
	// It carries almost no information in August, when PUP is a roster
	// mechanism rather than a prognosis, and real information by draft
	// day once cutdowns and Week 1 practice reports land. That is why the
	// dictionary is fetched fresh rather than read from a cache.
	Injury string
}

// Project prices every player currently rostered, so each manager can see
// what their keeper options would cost before locking a choice.
//
// Rosters carry forward in Sleeper between seasons, so before the draft
// they still hold last year's squads — which is exactly the eligible pool.
func Project(l *Ledger, season string, rosters []sleeper.Roster, info map[string]PlayerInfo) ([]Entry, error) {
	var declared []Declared
	for _, r := range rosters {
		for _, playerID := range r.Players {
			pi := info[playerID]
			declared = append(declared, Declared{
				OwnerID:  r.OwnerID,
				PlayerID: playerID,
				Name:     pi.Name,
				Position: pi.Position,
			})
		}
	}
	entries, err := l.PriceDeclared(season, declared)
	if err != nil {
		return nil, err
	}

	for i := range entries {
		if pi, ok := info[entries[i].PlayerID]; ok && pi.GamesPlayed == 0 {
			entries[i].Flags = append(entries[i].Flags, FlagDidNotPlay)
		}
	}
	return entries, nil
}

// WriteProjection renders keeper options by team, cheapest first within
// each team, so a manager can scan their own row block.
//
// maxKeepers is how many a team may keep; teams are ordered by the cost of
// their two cheapest viable options, which is a rough proxy for who has the
// most budget flexibility.
func WriteProjection(w io.Writer, entries []Entry, names Names, season string, maxKeepers, fullBudget int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	byOwner := map[string][]Entry{}
	for _, e := range entries {
		// A player who didn't play can't be kept, so showing a price
		// for him would be misleading.
		if hasFlag(e, FlagDidNotPlay) {
			continue
		}
		byOwner[e.OwnerID] = append(byOwner[e.OwnerID], e)
	}
	for owner := range byOwner {
		sort.Slice(byOwner[owner], func(i, j int) bool {
			return byOwner[owner][i].LeaguePrice < byOwner[owner][j].LeaguePrice
		})
	}

	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Slice(owners, func(i, j int) bool { return names.Of(owners[i]) < names.Of(owners[j]) })

	fmt.Fprintf(tw, "%s KEEPER PRICES BY TEAM\n", season)
	fmt.Fprintln(tw, strings.Repeat("=", 78))
	fmt.Fprintf(tw, "Each team may keep up to %d. Budget shown is $%d less the two\n", maxKeepers, fullBudget)
	fmt.Fprintf(tw, "cheapest options, as a floor on what that team could bring to the auction.\n")
	fmt.Fprintf(tw, "Players with no games last season are omitted as ineligible.\n\n")

	for _, o := range owners {
		list := byOwner[o]
		floor := fullBudget
		for i := 0; i < maxKeepers && i < len(list); i++ {
			floor -= list[i].LeaguePrice
		}
		fmt.Fprintf(tw, "%s\t(max budget if keeping the %d cheapest: $%d)\n", names.Of(o), maxKeepers, floor)
		fmt.Fprintln(tw, "  PLAYER\tPOS\tKEEP#\tBASIS\t PRICE\tNOTES")
		for _, e := range list {
			fmt.Fprintf(tw, "  %s\t%s\t%d\t$%d\t $%d\t%s\n",
				e.Name, e.Position, e.KeepCount, e.PriorValue, e.LeaguePrice,
				strings.Join(e.Flags, ","))
		}
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func hasFlag(e Entry, flag string) bool {
	for _, f := range e.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// KeeperOption is one rostered player priced as a keeper decision.
//
// Keeping a player is a purchase at his keeper price, so it gets judged the
// same way as any other buy: what would he cost on the open market, and
// what is he worth. A cheap keeper who is bad at any price is not a bargain.
type KeeperOption struct {
	Entry
	// Cost is what he would take to win in the auction instead.
	Cost int
	// Value is what he is worth on median projections.
	Value int
	// Recommended marks the picks that maximize savings for this team.
	Recommended bool
}

// Saves is the money keeping him frees up versus buying him back.
// This is the primary keeper argument and it uses market data, so it does
// not inherit the value source's positional skew.
func (k KeeperOption) Saves() int { return k.Cost - k.LeaguePrice }

// Edge is value less the keeper price: whether the asset is good at what
// you would actually pay, rather than merely cheaper than the market.
func (k KeeperOption) Edge() int { return k.Value - k.LeaguePrice }

// Trap reports a player who is cheap relative to the market but not worth
// the keeper price on his own merits — the market overprices him and
// keeping him locks that in.
func (k KeeperOption) Trap() bool { return k.Saves() > 0 && k.Edge() < 0 }

// RankKeepers prices every rostered player as a keeper decision and marks
// each team's best options.
//
// Ranking is by savings rather than by value, deliberately. Savings comes
// from the cost board, which is market-derived and carries no positional
// bias; ranking by value would inherit the projection source's skew and
// recommend a whole position at once.
func RankKeepers(entries []Entry, costs, values map[string]int, maxKeepers int) []KeeperOption {
	byOwner := map[string][]int{}
	out := make([]KeeperOption, 0, len(entries))

	for _, e := range entries {
		if hasFlag(e, FlagDidNotPlay) {
			continue
		}
		out = append(out, KeeperOption{
			Entry: e,
			Cost:  costs[e.PlayerID],
			Value: values[e.PlayerID],
		})
	}
	for i := range out {
		byOwner[out[i].OwnerID] = append(byOwner[out[i].OwnerID], i)
	}

	for _, idxs := range byOwner {
		sort.SliceStable(idxs, func(a, b int) bool {
			return out[idxs[a]].Saves() > out[idxs[b]].Saves()
		})
		for n, i := range idxs {
			// A keeper who saves nothing is not worth a roster spot; the
			// same player can be bought back for the same money.
			if n >= maxKeepers || out[i].Saves() <= 0 {
				break
			}
			out[i].Recommended = true
		}
	}
	return out
}

// WriteKeeperOptions renders each team's keeper decision, recommendations
// first.
func WriteKeeperOptions(w io.Writer, options []KeeperOption, names Names, season string, maxKeepers, fullBudget int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	byOwner := map[string][]KeeperOption{}
	for _, o := range options {
		byOwner[o.OwnerID] = append(byOwner[o.OwnerID], o)
	}
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Slice(owners, func(i, j int) bool { return names.Of(owners[i]) < names.Of(owners[j]) })

	fmt.Fprintf(tw, "%s KEEPER DECISIONS\n", season)
	fmt.Fprintln(tw, strings.Repeat("=", 78))
	fmt.Fprintf(tw, "Each team may keep %d. KEEP is what the league charges; COST is what he\n", maxKeepers)
	fmt.Fprintf(tw, "would take to win back in the auction; VALUE is his worth on median\n")
	fmt.Fprintf(tw, "projections. SAVES drives the recommendation because it is market-derived.\n")
	fmt.Fprintf(tw, "Players with no games last season are omitted as ineligible.\n\n")

	for _, o := range owners {
		list := byOwner[o]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Saves() > list[j].Saves() })

		spent, kept := 0, []string{}
		for _, k := range list {
			if k.Recommended {
				spent += k.LeaguePrice
				kept = append(kept, k.Name)
			}
		}
		summary := "keep nobody — nothing on this roster is cheaper than buying it back"
		if len(kept) > 0 {
			summary = fmt.Sprintf("keep %s for $%d, leaving $%d",
				strings.Join(kept, " + "), spent, fullBudget-spent)
		}
		fmt.Fprintf(tw, "%s\t%s\n", names.Of(o), summary)
		fmt.Fprintln(tw, "  PLAYER\tPOS\tKEEP\tCOST\tVALUE\tSAVES\tNOTES")

		for i, k := range list {
			// Everything past the recommendations plus a couple of
			// alternatives is noise on a 15-player roster.
			if i >= maxKeepers+3 && !k.Recommended {
				continue
			}
			mark := ""
			if k.Recommended {
				mark = "KEEP"
			}
			if k.Trap() {
				mark = strings.TrimSpace(mark + " overpriced")
			}
			notes := strings.TrimSpace(mark + " " + strings.Join(k.Flags, ","))
			fmt.Fprintf(tw, "  %s\t%s\t$%d\t$%d\t$%d\t%+d\t%s\n",
				k.Name, k.Position, k.LeaguePrice, k.Cost, k.Value, k.Saves(), notes)
		}
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

// WriteShareableKeepers renders keeper prices for the league to read.
//
// A separate renderer from WriteProjection and WriteKeeperOptions, and
// deliberately poorer than either. Those carry what a keeper would cost to
// win back in the auction, what he is worth on median projections, and
// which one to keep — the model, in other words. Sending that to the league
// would hand eleven opponents the reasoning you built the board to have.
//
// What is left is what they are owed and cannot work out themselves: their
// own players, and what the league's rules charge for each. Sleeper shows
// everyone a flat budget and does not apply the escalating ladder, so
// without this nobody knows what their own keepers cost until the auction.
//
// Cheapest first within a team, because the question an owner opens this
// with is which of his players is a bargain.
func WriteShareableKeepers(w io.Writer, entries []Entry, names Names, season string, maxKeepers, fullBudget int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	byOwner := map[string][]Entry{}
	for _, e := range entries {
		// A player who did not play cannot be kept, so a price for him
		// would be a number nobody can act on.
		if hasFlag(e, FlagDidNotPlay) {
			continue
		}
		byOwner[e.OwnerID] = append(byOwner[e.OwnerID], e)
	}
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
		sort.Slice(byOwner[o], func(i, j int) bool {
			if byOwner[o][i].LeaguePrice != byOwner[o][j].LeaguePrice {
				return byOwner[o][i].LeaguePrice < byOwner[o][j].LeaguePrice
			}
			return byOwner[o][i].Name < byOwner[o][j].Name
		})
	}
	sort.Slice(owners, func(i, j int) bool { return names.Of(owners[i]) < names.Of(owners[j]) })

	fmt.Fprintf(tw, "%s KEEPER PRICES\n\n", season)
	fmt.Fprintf(tw, "What each of your players costs to keep under the league rules in\n")
	fmt.Fprintf(tw, "draft.md. Keep up to %d; your auction budget is $%d less whatever you\n", maxKeepers, fullBudget)
	fmt.Fprintf(tw, "keep. Sleeper shows everyone a flat $%d and does not apply the\n", fullBudget)
	fmt.Fprintf(tw, "escalating ladder, so these are the numbers that count.\n")
	fmt.Fprintf(tw, "Players with no games last season are not eligible and are left out.\n")

	for _, o := range owners {
		fmt.Fprintf(tw, "\n%s\n", names.Of(o))
		for _, e := range byOwner[o] {
			fmt.Fprintf(tw, "  %s\t%s\t$%d\n", e.Name, e.Position, e.LeaguePrice)
		}
	}
	return tw.Flush()
}

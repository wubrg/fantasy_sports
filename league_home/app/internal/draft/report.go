package draft

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Reconciliation compares keeper prices computed from league rules against
// the amounts Sleeper recorded, and reports what needs a human decision.
//
// Agreement is the normal case and is not interesting. Disagreement is:
// Sleeper does not apply the escalating ladder, so its amounts are only as
// correct as whoever typed them in, and each mismatch is either a
// bookkeeping slip or a league ruling that isn't written down.
type Reconciliation struct {
	Seasons []SeasonSummary
	// Mismatches are entries where the computed price differs from what
	// Sleeper recorded, worst first. Corrupt seasons are excluded, since
	// comparing against known-bad records measures nothing.
	Mismatches []Entry
	// Flagged are entries carrying a rules-gap flag, whether or not the
	// price matched.
	Flagged []Entry
	Matched int
	Total   int
}

// SeasonSummary is one season's keeper accounting.
type SeasonSummary struct {
	Year          string
	Keepers       int
	LeagueSpend   int
	SleeperSpend  int
	AuctionSpend  int
	Corrupt       bool
	CorruptReason string
}

// Pool is the money actually available in the live auction: every team's
// budget less what keepers consumed under league rules.
func (s SeasonSummary) Pool(teams, budget int) int {
	return teams*budget - s.LeagueSpend
}

// Reconcile builds the comparison across every season in the ledger.
func Reconcile(seasons []SeasonData, l *Ledger, teams, budget int) Reconciliation {
	byYear := make(map[string]SeasonData, len(seasons))
	for _, s := range seasons {
		byYear[s.Year] = s
	}

	var r Reconciliation
	for _, year := range l.Seasons {
		s := byYear[year]
		entries := l.EntriesForSeason(year)
		keeperSpend, auctionSpend := TotalSpend(s, entries)

		summary := SeasonSummary{
			Year: year, Keepers: len(entries),
			LeagueSpend: keeperSpend, AuctionSpend: auctionSpend,
			Corrupt: s.Corrupt, CorruptReason: s.CorruptReason,
		}
		for _, e := range entries {
			summary.SleeperSpend += e.SleeperAmount
			if len(e.Flags) > 0 {
				r.Flagged = append(r.Flagged, e)
			}
			// A corrupt season's recorded amounts are known wrong, so a
			// disagreement with them is expected rather than actionable.
			if s.Corrupt {
				continue
			}
			r.Total++
			if e.LeaguePrice == e.SleeperAmount {
				r.Matched++
			} else {
				r.Mismatches = append(r.Mismatches, e)
			}
		}
		r.Seasons = append(r.Seasons, summary)
	}

	sort.Slice(r.Mismatches, func(i, j int) bool {
		a, b := abs(r.Mismatches[i].Variance()), abs(r.Mismatches[j].Variance())
		if a != b {
			return a > b
		}
		return r.Mismatches[i].Season < r.Mismatches[j].Season
	})
	return r
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Names resolves owner IDs to display names for reports. A missing entry
// falls back to the raw ID rather than an empty column.
type Names map[string]string

func (n Names) Of(ownerID string) string {
	if name, ok := n[ownerID]; ok && name != "" {
		return name
	}
	return ownerID
}

// WriteText renders the reconciliation as a plain-text report.
func (r Reconciliation) WriteText(w io.Writer, names Names, teams, budget int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "KEEPER RECONCILIATION")
	fmt.Fprintln(tw, strings.Repeat("=", 78))
	fmt.Fprintf(tw, "\nPrices below are computed from leagues/hit_or_miss/draft.md.\n")
	fmt.Fprintf(tw, "Sleeper does not enforce the escalating ladder, so its amounts are\n")
	fmt.Fprintf(tw, "whatever was entered by hand and are shown only for comparison.\n\n")

	fmt.Fprintln(tw, "SEASON\tKEEPERS\tLEAGUE $\tSLEEPER $\tDIFF\tAUCTION $\tLIVE POOL")
	for _, s := range r.Seasons {
		note := ""
		if s.Corrupt {
			note = "  <- EXCLUDED"
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%+d\t%d\t%d%s\n",
			s.Year, s.Keepers, s.LeagueSpend, s.SleeperSpend,
			s.LeagueSpend-s.SleeperSpend, s.AuctionSpend, s.Pool(teams, budget), note)
	}

	for _, s := range r.Seasons {
		if s.Corrupt {
			fmt.Fprintf(tw, "\n! %s excluded: %s\n", s.Year, s.CorruptReason)
		}
	}

	pct := 0.0
	if r.Total > 0 {
		pct = 100 * float64(r.Matched) / float64(r.Total)
	}
	fmt.Fprintf(tw, "\nAgreement with Sleeper: %d/%d (%.0f%%) across usable seasons\n",
		r.Matched, r.Total, pct)

	if len(r.Mismatches) > 0 {
		fmt.Fprintf(tw, "\nNEEDS AN LM RULING (%d)\n", len(r.Mismatches))
		fmt.Fprintln(tw, strings.Repeat("-", 78))
		fmt.Fprintln(tw, "SEASON\tPLAYER\tOWNER\tKEEP#\tPRIOR\tFROM\tLEAGUE $\tSLEEPER $\tDIFF\tNOTES")
		for _, e := range r.Mismatches {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%d\t%d\t%+d\t%s\n",
				e.Season, e.Name, names.Of(e.OwnerID), e.KeepCount, e.PriorValue,
				e.PriorMethod, e.LeaguePrice, e.SleeperAmount, e.Variance(),
				strings.Join(e.Flags, ","))
		}
	}
	return tw.Flush()
}

// WriteBudgets renders each owner's auction budget for a season after
// keeper costs. This is the table the draft room runs on, and the one
// Sleeper cannot produce because it shows every team a flat budget.
func WriteBudgets(w io.Writer, entries []Entry, names Names, fullBudget int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	budgets := Budgets(entries, fullBudget)

	byOwner := map[string][]Entry{}
	for _, e := range entries {
		byOwner[e.OwnerID] = append(byOwner[e.OwnerID], e)
	}
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Slice(owners, func(i, j int) bool { return budgets[owners[i]] < budgets[owners[j]] })

	fmt.Fprintln(tw, "OWNER\tKEEPERS\tKEEPER COST\tBUDGET LEFT")
	total := 0
	for _, o := range owners {
		var parts []string
		spent := 0
		for _, e := range byOwner[o] {
			parts = append(parts, fmt.Sprintf("%s $%d", e.Name, e.LeaguePrice))
			spent += e.LeaguePrice
		}
		total += budgets[o]
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", names.Of(o), strings.Join(parts, ", "), spent, budgets[o])
	}
	fmt.Fprintf(tw, "\nTotal in the live auction pool (keeper-holding teams): $%d\n", total)
	return tw.Flush()
}

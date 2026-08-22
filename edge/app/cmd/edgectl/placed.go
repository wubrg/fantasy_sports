package main

import (
	"os"
	"sort"

	"edge/internal/betlog"
	"edge/internal/board"
)

// Commitment is one open wager and the teams it ties up.
type Commitment struct {
	Selection string
	Teams     []string
}

// PlacedCommitments reads the prediction log and reports which teams on this
// board are already carrying a wager.
//
// This is what retires -exclude as something to remember. A wager riding a
// game makes that game unavailable -- two tickets on one game are not two
// chances, they are one counted twice -- and the log is the record of what is
// riding. Having to retype seven team codes every run was a standing invitation
// to forget one, and forgetting one silently inflates every hit-rate figure in
// the report.
//
// Settled wagers are skipped: a resolved bet no longer ties up anything.
//
// A missing log is not an error. Most boards are read before anything has been
// placed, and refusing to report until a log exists would make the common case
// the broken one.
func PlacedCommitments(logPath string, doc *board.Doc) ([]Commitment, []string, error) {
	bets, err := betlog.Load(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var out []Commitment
	seen := map[string]bool{}
	var teams []string
	for _, b := range bets {
		if b.Result != "" && b.Result != betlog.Open {
			continue
		}
		found := doc.TeamsMentioned(b.Bet.Selection)
		if len(found) == 0 {
			// A wager naming no team on this board belongs to another week.
			// Skipping it is correct and worth not warning about: a season's
			// log will be mostly other weeks.
			continue
		}
		out = append(out, Commitment{Selection: b.Bet.Selection, Teams: found})
		for _, t := range found {
			if !seen[t] {
				seen[t] = true
				teams = append(teams, t)
			}
		}
	}
	sort.Strings(teams)
	return out, teams, nil
}

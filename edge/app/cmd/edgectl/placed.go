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
// week scopes commitments to the board being reported on. A team plays every
// week, so a bet's free-text Selection matching a team code on THIS week's
// board proves nothing on its own -- the same code matches every week that
// team plays. A bet explicitly tagged with a different week (b.Bet.Week != 0)
// is skipped outright regardless of what teams its text mentions; only an
// untagged (legacy) bet falls back to the team-name heuristic below. Pass 0 to
// disable the tag check and fall back to the heuristic for every bet.
//
// A missing log is not an error. Most boards are read before anything has been
// placed, and refusing to report until a log exists would make the common case
// the broken one.
func PlacedCommitments(logPath string, doc *board.Doc, week int) ([]Commitment, []string, error) {
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
		if week > 0 && b.Bet.Week != 0 && b.Bet.Week != week {
			continue // explicitly tagged for a different week
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

package board

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ScheduleGame is one row of games.csv, reduced to what a board needs.
type ScheduleGame struct {
	ID      string
	Season  int
	Week    int
	Away    string
	Home    string
	Kickoff string

	// Consensus prices, empty when games.csv has not published them yet.
	// Roughly 60% of a season's rows carry lines at any given moment.
	AwayML   string
	HomeML   string
	Spread   string // away spread
	Total    string
	SpreadOK bool
	TotalOK  bool
}

// ReadSchedule pulls one season's regular-season games out of games.csv.
//
// The columns are addressed by header name rather than position: nflverse adds
// fields between releases, and a positional read would silently start pulling
// the wrong column rather than failing.
func ReadSchedule(r io.Reader, season int) ([]ScheduleGame, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1

	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("board: reading games.csv header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	for _, need := range []string{"game_id", "season", "game_type", "week", "gameday", "away_team", "home_team"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("board: games.csv has no %q column", need)
		}
	}
	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var out []ScheduleGame
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("board: reading games.csv: %w", err)
		}
		if get(rec, "season") != strconv.Itoa(season) || get(rec, "game_type") != "REG" {
			continue
		}
		wk, err := strconv.Atoi(get(rec, "week"))
		if err != nil {
			continue
		}
		g := ScheduleGame{
			ID:      get(rec, "game_id"),
			Season:  season,
			Week:    wk,
			Away:    get(rec, "away_team"),
			Home:    get(rec, "home_team"),
			Kickoff: kickoff(get(rec, "gameday"), get(rec, "gametime")),
			AwayML:  get(rec, "away_moneyline"),
			HomeML:  get(rec, "home_moneyline"),
		}
		if sl, ao, ho := get(rec, "spread_line"), get(rec, "away_spread_odds"), get(rec, "home_spread_odds"); sl != "" && ao != "" && ho != "" {
			g.Spread, g.SpreadOK = fmt.Sprintf("%s %s/%s", sl, signed(ao), signed(ho)), true
		}
		if tl, ov, un := get(rec, "total_line"), get(rec, "over_odds"), get(rec, "under_odds"); tl != "" && ov != "" && un != "" {
			g.Total, g.TotalOK = fmt.Sprintf("%s %s/%s", tl, signed(ov), signed(un)), true
		}
		out = append(out, g)
	}
	return out, nil
}

// kickoff joins the date and time columns into one sortable field. gametime is
// occasionally blank on unscheduled games, in which case the date alone still
// sorts correctly against other days.
func kickoff(day, t string) string {
	if day == "" {
		return ""
	}
	if t == "" {
		return day
	}
	return day + "T" + t
}

// signed writes a price the way a book shows it. games.csv stores dogs without
// a "+", which parses fine but reads wrong next to hand-typed cells.
func signed(s string) string {
	if s == "" {
		return s
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return "+" + s
	}
	return s
}

// Scaffold merges a week's schedule into an existing document, returning the
// number of games added.
//
// The merge never overwrites a price. Re-running after a schedule reissue must
// be safe at any time, including halfway through typing a board in, so an
// existing cell always wins over a generated one -- even the consensus column,
// which you may have corrected by hand after spotting a bad line.
func (d *Doc) Scaffold(games []ScheduleGame) int {
	if d.Games == nil {
		d.Games = map[string]*Game{}
	}
	added := 0
	for _, sg := range games {
		g, exists := d.Games[sg.ID]
		if !exists {
			g = &Game{Books: map[string]Lines{}}
			d.Games[sg.ID] = g
			added++
		}
		// Schedule facts are authoritative and are refreshed: a game can be
		// flexed to a new kickoff, and the board should follow.
		g.Away, g.Home, g.Kickoff = sg.Away, sg.Home, sg.Kickoff
		if g.Books == nil {
			g.Books = map[string]Lines{}
		}
		for _, bk := range Books {
			cur, had := g.Books[bk]
			if !had {
				cur = Lines{}
			}
			if bk == Consensus {
				if cur.ML == "" && sg.AwayML != "" && sg.HomeML != "" {
					cur.ML = fmt.Sprintf("%s/%s", signed(sg.AwayML), signed(sg.HomeML))
				}
				if cur.Spread == "" && sg.SpreadOK {
					cur.Spread = sg.Spread
				}
				if cur.Total == "" && sg.TotalOK {
					cur.Total = sg.Total
				}
			}
			g.Books[bk] = cur
		}
	}
	return added
}

// SplitByWeek groups a season's schedule into per-week buckets.
func SplitByWeek(games []ScheduleGame) map[int][]ScheduleGame {
	out := map[int][]ScheduleGame{}
	for _, g := range games {
		out[g.Week] = append(out[g.Week], g)
	}
	return out
}

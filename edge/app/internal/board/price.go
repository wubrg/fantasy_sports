package board

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"edge/internal/wager"
)

// The three cell formats, all chosen to be typeable on a phone keypad:
//
//	ml:     "130/-155"
//	spread: "2.5 -110/-110"
//	total:  "44.5 -110/-110"
//
// A leading "+" is accepted and ignored, because that is how books display a
// dog and retyping it without the sign is an invitation to lose the sign that
// mattered. Whitespace around any component is tolerated.

// ParseMarket reads a two-sided price pair such as "130/-155".
//
// ok is false for an empty cell, which is not an error: most cells on a board
// are empty and stay that way. Callers distinguish "no price" from "bad price"
// by checking ok before err.
func ParseMarket(s string) (m wager.Market, ok bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return wager.Market{}, false, nil
	}
	a, b, found := strings.Cut(s, "/")
	if !found {
		return wager.Market{}, false, fmt.Errorf(
			"want two prices separated by '/' (e.g. 130/-155), got %q", s)
	}
	pa, err := parseAmerican(a)
	if err != nil {
		return wager.Market{}, false, err
	}
	pb, err := parseAmerican(b)
	if err != nil {
		return wager.Market{}, false, err
	}
	return wager.Market{A: pa, B: pb}, true, nil
}

// ParseHandicap reads a lined market such as "2.5 -110/-110" or
// "44.5 -110/-110", returning the line and the two-sided price.
//
// For a spread the line is the AWAY team's number, so a negative line means
// the away team is favoured. Storing one signed number rather than a pair
// removes the chance of the two disagreeing.
func ParseHandicap(s string) (line float64, m wager.Market, ok bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, wager.Market{}, false, nil
	}
	lineStr, priceStr, found := strings.Cut(s, " ")
	if !found {
		return 0, wager.Market{}, false, fmt.Errorf(
			"want a line then two prices (e.g. 2.5 -110/-110), got %q", s)
	}
	line, err = strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lineStr), "+")), 64)
	if err != nil {
		return 0, wager.Market{}, false, fmt.Errorf("bad line %q: not a number", lineStr)
	}
	m, ok, err = ParseMarket(priceStr)
	if err != nil {
		return 0, wager.Market{}, false, err
	}
	if !ok {
		return 0, wager.Market{}, false, fmt.Errorf(
			"line %v has no prices; want e.g. %v -110/-110", line, line)
	}
	return line, m, true, nil
}

// parseAmerican reads one American price, tolerating a redundant leading "+".
//
// The price is validated through wager.American rather than accepted as any
// integer: values strictly between -100 and +100 are not real prices, and a
// typo that lands there would otherwise be de-vigged into a confident wrong
// answer instead of being rejected.
func parseAmerican(s string) (wager.American, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, fmt.Errorf("missing price")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad price %q: not an integer", s)
	}
	a := wager.American(n)
	// Decimal() runs American's own validation and is the cheapest way to
	// reach it; the value is discarded.
	if _, err := a.Decimal(); err != nil {
		return 0, fmt.Errorf("bad price %q: %w", s, err)
	}
	return a, nil
}

// FormatMarket renders a price pair back to cell form.
func FormatMarket(m wager.Market) string {
	return fmt.Sprintf("%s/%s", formatAmerican(m.A), formatAmerican(m.B))
}

// FormatHandicap renders a lined market back to cell form.
func FormatHandicap(line float64, m wager.Market) string {
	return fmt.Sprintf("%s %s", strconv.FormatFloat(line, 'f', -1, 64), FormatMarket(m))
}

// formatAmerican writes a price the way a book shows it, with an explicit "+"
// on a dog. Round-tripping through ParseMarket is lossless because the parser
// tolerates the sign.
func formatAmerican(a wager.American) string {
	if a > 0 {
		return "+" + strconv.Itoa(int(a))
	}
	return strconv.Itoa(int(a))
}

// Problem is one malformed cell, located well enough to fix by hand.
type Problem struct {
	GameID string
	Book   string
	Market string // "ml", "spread" or "total"
	Value  string
	Err    error
}

func (p Problem) String() string {
	return fmt.Sprintf("%s %s %s: %q -- %v", p.GameID, p.Book, p.Market, p.Value, p.Err)
}

// Validate parses every non-empty cell and reports the ones that fail.
//
// It returns problems rather than stopping at the first, because a board is
// typed in bulk and fixing transcription errors one round-trip at a time is
// the slowest possible way to do it.
func (d *Doc) Validate() []Problem {
	var out []Problem
	for _, id := range d.GameIDs() {
		g := d.Games[id]
		for _, bk := range sortedBooks(g.Books) {
			l := g.Books[bk]
			if _, _, err := ParseMarket(l.ML); err != nil {
				out = append(out, Problem{id, bk, "ml", l.ML, err})
			}
			if _, _, _, err := ParseHandicap(l.Spread); err != nil {
				out = append(out, Problem{id, bk, "spread", l.Spread, err})
			}
			if _, _, _, err := ParseHandicap(l.Total); err != nil {
				out = append(out, Problem{id, bk, "total", l.Total, err})
			}
		}
	}
	return out
}

// sortedBooks orders a game's books by the canonical Books order, with any
// unrecognised key last so a hand-added book is reported rather than skipped.
func sortedBooks(m map[string]Lines) []string {
	rank := make(map[string]int, len(Books))
	for i, b := range Books {
		rank[b] = i
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ra, oka := rank[a]
		rb, okb := rank[b]
		if oka != okb {
			return oka
		}
		if oka && ra != rb {
			return ra < rb
		}
		return a < b
	})
	return out
}

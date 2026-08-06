package draft

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// FlagOverridden marks a keeper price set by LM ruling rather than
// computed from the ladder.
const FlagOverridden = "lm-ruling"

// Override is a commissioner ruling that fixes a keeper's price, for the
// cases the written rules don't settle: a player whose chain runs through
// the corrupt 2022 season, a rookie with no prior record, or simply a
// price the league agreed on at the time.
type Override struct {
	Season   string
	PlayerID string
	Price    int
	// KeepCount optionally pins the player's ladder position, for cases
	// where the value was established outside Sleeper — an expansion or
	// new-owner draft assigns a keeper price at selection time, and
	// nothing in the API records that it happened. Zero means "leave the
	// computed count alone".
	KeepCount int
	Reason    string
}

// Overrides indexes rulings by season and player.
type Overrides map[string]Override

func overrideKey(season, playerID string) string { return season + "\x00" + playerID }

// Lookup returns the ruling for a player in a season, if one exists.
func (o Overrides) Lookup(season, playerID string) (Override, bool) {
	if o == nil {
		return Override{}, false
	}
	ov, ok := o[overrideKey(season, playerID)]
	return ov, ok
}

// Add records a ruling.
func (o Overrides) Add(ov Override) { o[overrideKey(ov.Season, ov.PlayerID)] = ov }

// LoadOverrides reads rulings from a CSV with the header
// season,player_id,price,reason. A missing file is not an error — most
// seasons need no rulings at all — so callers get an empty set back.
func LoadOverrides(path string) (Overrides, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Overrides{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("draft: opening overrides %s: %w", path, err)
	}
	defer f.Close()
	return ParseOverrides(f)
}

// ParseOverrides reads rulings from any CSV source. Column order is taken
// from the header rather than assumed, so hand-edited files with reordered
// or extra columns still load.
func ParseOverrides(r io.Reader) (Overrides, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("draft: reading overrides: %w", err)
	}
	out := Overrides{}
	if len(rows) == 0 {
		return out, nil
	}

	cols := map[string]int{}
	for i, h := range rows[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, required := range []string{"season", "player_id", "price"} {
		if _, ok := cols[required]; !ok {
			return nil, fmt.Errorf("draft: overrides missing required column %q", required)
		}
	}

	at := func(row []string, name string) string {
		i, ok := cols[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	for n, row := range rows[1:] {
		line := n + 2 // 1-indexed, plus the header
		if len(row) == 0 || strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		season, playerID := at(row, "season"), at(row, "player_id")
		if season == "" || playerID == "" {
			return nil, fmt.Errorf("draft: overrides line %d: season and player_id are required", line)
		}
		price, err := strconv.Atoi(at(row, "price"))
		if err != nil {
			return nil, fmt.Errorf("draft: overrides line %d: bad price %q", line, at(row, "price"))
		}
		if price < 0 {
			return nil, fmt.Errorf("draft: overrides line %d: price %d is negative", line, price)
		}

		// keep_count is optional; blank leaves the computed ladder
		// position untouched.
		keepCount := 0
		if raw := at(row, "keep_count"); raw != "" {
			keepCount, err = strconv.Atoi(raw)
			if err != nil || keepCount < 1 {
				return nil, fmt.Errorf("draft: overrides line %d: bad keep_count %q (must be a positive whole number)", line, raw)
			}
		}
		out.Add(Override{
			Season: season, PlayerID: playerID, Price: price,
			KeepCount: keepCount, Reason: at(row, "reason"),
		})
	}
	return out, nil
}

// apply replaces a computed price with an LM ruling when one exists,
// returning the price to use and the flags to record.
//
// The ruling wins outright: the point of a ruling is that the league
// decided something the rules didn't, so the computed value is history.
func (o Overrides) apply(season, playerID string, price int, flags []string) (int, []string) {
	ov, ok := o.Lookup(season, playerID)
	if !ok {
		return price, flags
	}
	return ov.Price, append(flags, FlagOverridden)
}

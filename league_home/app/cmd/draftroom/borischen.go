package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// loadBorisChenTiers reads Boris Chen's normalized tiers into a per-player
// tier keyed by Sleeper ID. The tier is within-position — his RB tier 3 and
// WR tier 3 are separate ladders — which is what the board wants beside its
// own gap-based tiering. Names are resolved through the same matcher the board
// uses, so a spelling it accepts lands on the same player everywhere else.
//
// Absent or unreadable is not fatal — the board simply shows no Boris tier —
// mirroring how the Chris Dell and FantasyPros sources degrade.
func loadBorisChenTiers(path string, resolve func(string) string) (map[string]int, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "Boris Chen source absent — tier column off"
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, "Boris Chen source unreadable — tier column off"
	}
	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	playerCol, ok1 := cols["player"]
	tierCol, ok2 := cols["tier"]
	if !ok1 || !ok2 {
		return nil, "Boris Chen source missing player/tier columns — tier column off"
	}

	out := map[string]int{}
	unmatched := 0
	for _, r := range records[1:] {
		if playerCol >= len(r) || tierCol >= len(r) {
			continue
		}
		name := strings.TrimSpace(r[playerCol])
		tier, err := strconv.Atoi(strings.TrimSpace(r[tierCol]))
		if err != nil || name == "" {
			continue
		}
		id := resolve(name)
		if id == "" {
			unmatched++
			continue
		}
		// Keep the strongest (lowest-numbered) tier if a name resolves twice.
		if prev, ok := out[id]; !ok || tier < prev {
			out[id] = tier
		}
	}
	if unmatched > 0 {
		return out, fmt.Sprintf("%d Boris Chen rows unmatched", unmatched)
	}
	return out, ""
}

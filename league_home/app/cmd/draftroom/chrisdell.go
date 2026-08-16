package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// loadChrisDellSharp reads Chris Dell's normalized rankings into a per-player
// divergence keyed by Sleeper ID: vs_ecr, the positional-rank gap between
// consensus and his own board, positive where he rates a player above the
// field. Names are resolved through the same matcher the board uses, so a
// spelling it accepts lands on the same player his lean set does.
//
// Absent or unreadable is not fatal — the board simply shows no dell flags —
// mirroring how the FantasyPros source degrades.
func loadChrisDellSharp(path string, resolve func(string) string) (map[string]int, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "Chris Dell source absent — dell flags off"
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, "Chris Dell source unreadable — dell flags off"
	}
	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	playerCol, ok1 := cols["player"]
	deltaCol, ok2 := cols["vs_ecr"]
	if !ok1 || !ok2 {
		return nil, "Chris Dell source missing player/vs_ecr columns — dell flags off"
	}

	out := map[string]int{}
	unmatched := 0
	for _, r := range records[1:] {
		if playerCol >= len(r) || deltaCol >= len(r) {
			continue
		}
		name := strings.TrimSpace(r[playerCol])
		delta, err := strconv.Atoi(strings.TrimSpace(r[deltaCol]))
		if err != nil || name == "" {
			continue
		}
		id := resolve(name)
		if id == "" {
			unmatched++
			continue
		}
		out[id] = delta
	}
	if unmatched > 0 {
		return out, fmt.Sprintf("%d Chris Dell rows unmatched", unmatched)
	}
	return out, ""
}

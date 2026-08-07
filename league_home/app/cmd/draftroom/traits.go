package main

import (
	"leaguehome/internal/draft"
)

// classifyTraits labels every projected player by what kind of player he is.
//
// Assembled here rather than in the draft package because it joins three
// sources: Ciely for the projection and its components, Subvertadown for what
// the industry thinks, and Sleeper for last season and the injury report.
func classifyTraits(ciely, sv []draft.SourceRow, info map[string]draft.PlayerInfo,
	shape draft.PoolState) map[string]draft.TraitSet {

	// Subvertadown ships a row per baseline; the spread between them is the
	// signal, so collect them all before measuring.
	type outside struct {
		up bool
	}
	byID := map[string]*outside{}
	for _, r := range sv {
		if r.PlayerID == "" {
			continue
		}
		o, ok := byID[r.PlayerID]
		if !ok {
			o = &outside{}
			byID[r.PlayerID] = o
		}
		o.up = o.up || r.ECRUp
	}

	var in []draft.TraitInput
	for _, r := range ciely {
		if r.PlayerID == "" || r.Points <= 0 {
			continue
		}
		t := draft.TraitInput{
			PlayerID: r.PlayerID,
			Position: r.Position,
			Points:   r.Points,
			Parts:    r.Components,
		}
		if o, ok := byID[r.PlayerID]; ok {
			t.ECRUpside = o.up
		}
		if pi, ok := info[r.PlayerID]; ok {
			t.Injury = pi.Injury
			t.PriorPoints = pi.PriorPoints
			t.PriorGames = pi.GamesPlayed
		}
		in = append(in, t)
	}
	return draft.ClassifyTraits(in, shape)
}

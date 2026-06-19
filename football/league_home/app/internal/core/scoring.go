package core

import (
	"math"
	"sort"
)

// ScoringEntry is one stat's point value, taken live from Sleeper's
// league.scoring_settings (the actual source of truth points get computed
// from). See football/league_home/README.md Phase 2 for why this is a
// live lookup instead of a hand-transcribed one.
type ScoringEntry struct {
	Key    string
	Label  string
	Points float64
}

// ScoringCategory groups related ScoringEntry rows for display (passing,
// rushing, etc). "Other" holds any scoring_settings key this package
// doesn't have a label for yet, so a newly-added Sleeper stat type still
// shows up instead of silently vanishing.
type ScoringCategory struct {
	Name    string
	Entries []ScoringEntry
}

// statLabel and statCategory translate Sleeper's stat abbreviations
// (https://docs.sleeper.com) into a human label and a display category.
// This is a fixed translation table for Sleeper's own API vocabulary, not
// league-specific data, so it's safe to hard-code here rather than load
// from a JSON file.
var statLabel = map[string]string{
	"pass_yd":  "Passing yard",
	"pass_td":  "Passing TD",
	"pass_int": "Interception thrown",
	"pass_2pt": "2pt passing conversion",

	"rush_yd":  "Rushing yard",
	"rush_td":  "Rushing TD",
	"rush_2pt": "2pt rushing conversion",

	"rec":     "Reception",
	"rec_yd":  "Receiving yard",
	"rec_td":  "Receiving TD",
	"rec_2pt": "2pt receiving conversion",

	"fum":        "Fumble",
	"fum_lost":   "Fumble lost",
	"fum_rec":    "Fumble recovery",
	"fum_rec_td": "Fumble recovery TD",

	"fgm_0_19":  "Field goal made (0-19 yds)",
	"fgm_20_29": "Field goal made (20-29 yds)",
	"fgm_30_39": "Field goal made (30-39 yds)",
	"fgm_40_49": "Field goal made (40-49 yds)",
	"fgm_50p":   "Field goal made (50+ yds)",
	"fgmiss":    "Field goal missed",
	"xpm":       "Extra point made",
	"xpmiss":    "Extra point missed",

	"def_td":         "Defensive TD",
	"def_st_td":      "Defense/special teams TD",
	"def_st_ff":      "Defense/special teams forced fumble",
	"def_st_fum_rec": "Defense/special teams fumble recovery",
	"st_ff":          "Special teams forced fumble",
	"st_fum_rec":     "Special teams fumble recovery",
	"st_td":          "Special teams TD",
	"int":            "Interception (defense)",
	"sack":           "Sack",
	"safe":           "Safety",
	"blk_kick":       "Blocked kick",
	"ff":             "Forced fumble",

	"pts_allow_0":     "Points allowed: 0",
	"pts_allow_1_6":   "Points allowed: 1-6",
	"pts_allow_7_13":  "Points allowed: 7-13",
	"pts_allow_14_20": "Points allowed: 14-20",
	"pts_allow_21_27": "Points allowed: 21-27",
	"pts_allow_28_34": "Points allowed: 28-34",
	"pts_allow_35p":   "Points allowed: 35+",
}

var statCategory = map[string]string{
	"pass_yd": "Passing", "pass_td": "Passing", "pass_int": "Passing", "pass_2pt": "Passing",

	"rush_yd": "Rushing", "rush_td": "Rushing", "rush_2pt": "Rushing",

	"rec": "Receiving", "rec_yd": "Receiving", "rec_td": "Receiving", "rec_2pt": "Receiving",

	"fum": "Fumbles", "fum_lost": "Fumbles", "fum_rec": "Fumbles", "fum_rec_td": "Fumbles",

	"fgm_0_19": "Kicking", "fgm_20_29": "Kicking", "fgm_30_39": "Kicking",
	"fgm_40_49": "Kicking", "fgm_50p": "Kicking", "fgmiss": "Kicking",
	"xpm": "Kicking", "xpmiss": "Kicking",

	"def_td": "Defense/Special Teams", "def_st_td": "Defense/Special Teams",
	"def_st_ff": "Defense/Special Teams", "def_st_fum_rec": "Defense/Special Teams",
	"st_ff": "Defense/Special Teams", "st_fum_rec": "Defense/Special Teams",
	"st_td": "Defense/Special Teams", "int": "Defense/Special Teams",
	"sack": "Defense/Special Teams", "safe": "Defense/Special Teams",
	"blk_kick": "Defense/Special Teams", "ff": "Defense/Special Teams",

	"pts_allow_0": "Points Allowed", "pts_allow_1_6": "Points Allowed",
	"pts_allow_7_13": "Points Allowed", "pts_allow_14_20": "Points Allowed",
	"pts_allow_21_27": "Points Allowed", "pts_allow_28_34": "Points Allowed",
	"pts_allow_35p": "Points Allowed",
}

// categoryOrder fixes the display order of known categories; "Other"
// (unlabeled keys) always sorts last.
var categoryOrder = []string{"Passing", "Rushing", "Receiving", "Fumbles", "Kicking", "Defense/Special Teams", "Points Allowed"}

// roundPoints rounds to 4 decimal places, since Sleeper stores scoring
// values as float32 and re-serializes them as float64 (e.g. 0.04 comes
// back as 0.03999999910593033).
func roundPoints(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// Scoring returns the league's live scoring settings, grouped by category.
// This is pulled straight from Sleeper's league.scoring_settings rather
// than hand-transcribed, since that's the actual source of truth points
// get computed from (see football/league_home/README.md Phase 2).
func (s *Service) Scoring() ([]ScoringCategory, error) {
	league, err := s.sleeper.League(s.leagueID)
	if err != nil {
		return nil, err
	}

	byCategory := make(map[string][]ScoringEntry)
	for key, points := range league.ScoringSettings {
		category := statCategory[key]
		if category == "" {
			category = "Other"
		}
		label := statLabel[key]
		if label == "" {
			label = key
		}
		byCategory[category] = append(byCategory[category], ScoringEntry{
			Key:    key,
			Label:  label,
			Points: roundPoints(points),
		})
	}

	order := categoryOrder
	if _, ok := byCategory["Other"]; ok {
		order = append(append([]string{}, categoryOrder...), "Other")
	}

	categories := make([]ScoringCategory, 0, len(order))
	for _, name := range order {
		entries := byCategory[name]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
		categories = append(categories, ScoringCategory{Name: name, Entries: entries})
	}
	return categories, nil
}

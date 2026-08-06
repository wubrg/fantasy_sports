package sleeper

import (
	"testing"
	"time"
)

func TestDraftsAndDraft(t *testing.T) {
	// Shapes here mirror the real 2026 Hit or Miss draft payload.
	draftBody := map[string]interface{}{
		"draft_id":    "d1",
		"league_id":   "123",
		"season":      "2026",
		"status":      "pre_draft",
		"type":        "auction",
		"start_time":  1788483600000,
		"last_picked": nil,
		"metadata": map[string]interface{}{
			"name":         "Hit or Miss",
			"scoring_type": "half_ppr",
		},
		"settings": map[string]interface{}{
			"budget": 200, "teams": 12, "rounds": 14,
			"slots_qb": 1, "slots_rb": 2, "slots_wr": 3, "slots_te": 1,
			"slots_flex": 1, "slots_def": 1, "slots_bn": 5,
		},
	}
	c := testClient(t, map[string]interface{}{
		"/league/123/drafts": []interface{}{draftBody},
		"/draft/d1":          draftBody,
	})

	drafts, err := c.Drafts("123")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Type != "auction" || drafts[0].Settings.Budget != 200 {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}

	d, err := c.Draft("d1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Settings.Teams != 12 || d.Settings.SlotsBN != 5 || d.Metadata.ScoringType != "half_ppr" {
		t.Errorf("unexpected draft: %+v", d)
	}
	if d.InProgress() {
		t.Error("pre_draft should not report InProgress")
	}
	if got, want := d.StartsAt().UTC(), time.UnixMilli(1788483600000).UTC(); !got.Equal(want) {
		t.Errorf("StartsAt = %v, want %v", got, want)
	}
}

func TestDraftStartsAtZeroWhenUnset(t *testing.T) {
	if got := (Draft{}).StartsAt(); !got.IsZero() {
		t.Errorf("expected zero time for unset start_time, got %v", got)
	}
}

func TestDraftPicksKeeperFlagAndAmount(t *testing.T) {
	// Sleeper sends is_keeper as null for ordinary picks and true for
	// keepers, and encodes the auction amount as a string.
	c := testClient(t, map[string]interface{}{
		"/draft/d1/picks": []interface{}{
			map[string]interface{}{
				"draft_id": "d1", "pick_no": 1, "round": 1, "roster_id": 4,
				"picked_by": "u4", "player_id": "9509", "is_keeper": nil,
				"metadata": map[string]interface{}{
					"amount": "78", "first_name": "Bijan", "last_name": "Robinson",
					"position": "RB", "team": "ATL",
				},
			},
			map[string]interface{}{
				"draft_id": "d1", "pick_no": 2, "round": 1, "roster_id": 7,
				"picked_by": "u7", "player_id": "9224", "is_keeper": true,
				"metadata": map[string]interface{}{
					"amount": "10", "first_name": "Chase", "last_name": "Brown",
					"position": "RB", "team": "CIN",
				},
			},
		},
	})

	picks, err := c.DraftPicks("d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 2 {
		t.Fatalf("expected 2 picks, got %d", len(picks))
	}
	if picks[0].IsKeeper {
		t.Error("null is_keeper should decode to false")
	}
	if !picks[1].IsKeeper {
		t.Error("true is_keeper should decode to true")
	}
	if got := picks[0].Metadata.Dollars(); got != 78 {
		t.Errorf("Dollars() = %d, want 78", got)
	}
	if got := picks[0].Metadata.Name(); got != "Bijan Robinson" {
		t.Errorf("Name() = %q, want %q", got, "Bijan Robinson")
	}
}

func TestDraftPickDollarsHandlesMissingAmount(t *testing.T) {
	// Snake drafts carry no amount; a malformed one must not panic.
	for _, amount := range []string{"", "  ", "n/a"} {
		if got := (DraftPickMetadata{Amount: amount}).Dollars(); got != 0 {
			t.Errorf("Dollars(%q) = %d, want 0", amount, got)
		}
	}
}

func TestDraftPickNameHandlesPartialNames(t *testing.T) {
	// Team defenses come through with only one name component.
	cases := []struct{ first, last, want string }{
		{"Bijan", "Robinson", "Bijan Robinson"},
		{"", "Rams", "Rams"},
		{"Rams", "", "Rams"},
	}
	for _, tc := range cases {
		got := DraftPickMetadata{FirstName: tc.first, LastName: tc.last}.Name()
		if got != tc.want {
			t.Errorf("Name(%q, %q) = %q, want %q", tc.first, tc.last, got, tc.want)
		}
	}
}

func TestTransactionsBidExtraction(t *testing.T) {
	// Waivers carry settings.waiver_bid; free agent adds send settings
	// as null, which must not panic.
	c := testClient(t, map[string]interface{}{
		"/league/123/transactions/2": []interface{}{
			map[string]interface{}{
				"transaction_id": "t1", "type": "waiver", "status": "complete",
				"leg": 2, "roster_ids": []int{3},
				"adds":     map[string]int{"7002": 3},
				"drops":    map[string]int{"1234": 3},
				"settings": map[string]interface{}{"waiver_bid": 26, "seq": 1},
			},
			map[string]interface{}{
				"transaction_id": "t2", "type": "free_agent", "status": "complete",
				"leg": 2, "roster_ids": []int{5},
				"adds": map[string]int{"9999": 5}, "drops": nil, "settings": nil,
			},
			map[string]interface{}{
				"transaction_id": "t3", "type": "waiver", "status": "failed",
				"leg": 2, "roster_ids": []int{6},
				"adds":     map[string]int{"7002": 6},
				"settings": map[string]interface{}{"waiver_bid": 12, "seq": 2},
			},
		},
	})

	txns, err := c.Transactions("123", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(txns))
	}
	if got := txns[0].Bid(); got != 26 {
		t.Errorf("waiver Bid() = %d, want 26", got)
	}
	if got := txns[1].Bid(); got != 0 {
		t.Errorf("free agent Bid() = %d, want 0", got)
	}
	if txns[1].Settings != nil {
		t.Error("expected nil settings for free agent add")
	}
	// Losing bids are still returned by the API and must stay
	// distinguishable, or acquisition costs will be overstated.
	if txns[2].Status != "failed" {
		t.Errorf("expected failed status to survive decoding, got %q", txns[2].Status)
	}
}

func TestSeasonStats(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/stats/nfl/regular/2025": map[string]interface{}{
			"8155": map[string]interface{}{
				"gp": 17.0, "rec": 61.0, "rush_yd": 1456.0, "rush_td": 12.0,
				"pts_half_ppr": 312.6,
			},
		},
	})

	stats, err := c.SeasonStats("regular", "2025")
	if err != nil {
		t.Fatal(err)
	}
	if got := stats["8155"]["rush_yd"]; got != 1456 {
		t.Errorf("rush_yd = %v, want 1456", got)
	}
	// Absent stats must read as zero rather than erroring, since Sleeper
	// omits categories a player never recorded.
	if got := stats["8155"]["pass_td"]; got != 0 {
		t.Errorf("missing stat = %v, want 0", got)
	}
}

func TestPlayers(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/players/nfl": map[string]interface{}{
			"9509": map[string]interface{}{
				"player_id": "9509", "first_name": "Bijan", "last_name": "Robinson",
				"full_name": "Bijan Robinson", "search_full_name": "bijanrobinson",
				"position": "RB", "team": "ATL", "years_exp": 3,
				"fantasy_positions": []string{"RB"}, "active": true,
			},
			// Team defenses are keyed by team abbreviation and carry
			// neither full_name nor search_full_name — verified against
			// the live dictionary, where all 32 entries with a blank
			// search_full_name are DEFs.
			"LAR": map[string]interface{}{
				"player_id": "LAR", "last_name": "Rams", "position": "DEF",
				"team": "LAR", "fantasy_positions": []string{"DEF"}, "active": true,
			},
		},
	})

	players, err := c.Players()
	if err != nil {
		t.Fatal(err)
	}
	if got := players["9509"].SearchFullName; got != "bijanrobinson" {
		t.Errorf("SearchFullName = %q, want %q", got, "bijanrobinson")
	}
	// Name-based matching cannot be the only path to a player_id: DEFs
	// have no usable name and must be resolved by team abbreviation.
	def := players["LAR"]
	if def.Position != "DEF" || def.SearchFullName != "" || def.Team != "LAR" {
		t.Errorf("unexpected DEF entry: %+v", def)
	}
}

func TestLeagueRosterPositions(t *testing.T) {
	c := testClient(t, map[string]interface{}{
		"/league/123": map[string]interface{}{
			"league_id": "123",
			"roster_positions": []string{
				"QB", "RB", "RB", "WR", "WR", "WR", "TE", "FLEX", "DEF",
				"BN", "BN", "BN", "BN", "BN",
			},
			"scoring_settings": map[string]float64{"rec": 0.5, "pass_int": -1},
		},
	})

	l, err := c.League("123")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.RosterPositions) != 14 {
		t.Errorf("expected 14 roster positions, got %d", len(l.RosterPositions))
	}
	// Half-PPR is the whole reason the draft room can't reuse off-the-shelf
	// full-PPR values, so make sure it survives decoding.
	if got := l.ScoringSettings["rec"]; got != 0.5 {
		t.Errorf("rec = %v, want 0.5", got)
	}
}

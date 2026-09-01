package sleeper

import (
	"fmt"
	"strconv"
	"time"
)

// Draft describes one draft attached to a league. Hit or Miss runs auctions,
// so Type is "auction" and Settings.Budget is the per-team dollar budget.
type Draft struct {
	DraftID  string `json:"draft_id"`
	LeagueID string `json:"league_id"`
	Season   string `json:"season"`
	// Status is "pre_draft", "drafting", or "complete".
	Status string `json:"status"`
	// Type is "auction", "snake", or "linear".
	Type string `json:"type"`
	// StartTime and LastPicked are milliseconds since the Unix epoch.
	// LastPicked is 0 until the first pick is made.
	StartTime  int64 `json:"start_time"`
	LastPicked int64 `json:"last_picked"`
	// DraftOrder maps an owner id to the slot he drafts from. Null for this
	// league's own drafts, where picks name their owner outright; populated
	// for a mock, where they do not. See staticData.isMine.
	DraftOrder map[string]int `json:"draft_order"`
	Metadata   struct {
		Name string `json:"name"`
		// ScoringType is Sleeper's coarse label ("half_ppr"), not the
		// league's actual scoring_settings. Use League.ScoringSettings
		// for anything that has to be numerically right.
		ScoringType string `json:"scoring_type"`
		// NominatedPlayerID is the player currently up for bidding.
		//
		// Present only once a draft has started, and — the trap — never
		// cleared afterwards: it holds the last nomination for as long as the
		// draft object exists, so it says who was nominated, never whether
		// they still are. Only the picks feed reports that, by the player
		// turning up sold.
		//
		// timer_end_at sits beside it and is not read, because it cannot
		// answer that question either: Sleeper stamps it once and never
		// advances it, so a player who holds the block for twenty seconds
		// carries a timer that expired fifteen seconds ago. The bid fields
		// (highest_offer, offering_slot, offering_user_id) are not read
		// either — the board names the player and nothing more.
		NominatedPlayerID string `json:"nominated_player_id"`
	} `json:"metadata"`
	Settings DraftSettings `json:"settings"`
}

// DraftSettings holds the roster shape and budget the draft was configured
// with. The Slots* fields are the source of truth for starting lineup
// requirements, since they describe the draft as actually run.
type DraftSettings struct {
	Budget    int `json:"budget"`
	Teams     int `json:"teams"`
	Rounds    int `json:"rounds"`
	SlotsQB   int `json:"slots_qb"`
	SlotsRB   int `json:"slots_rb"`
	SlotsWR   int `json:"slots_wr"`
	SlotsTE   int `json:"slots_te"`
	SlotsFlex int `json:"slots_flex"`
	SlotsDEF  int `json:"slots_def"`
	SlotsK    int `json:"slots_k"`
	SlotsBN   int `json:"slots_bn"`
}

// StartsAt converts the millisecond StartTime to a time.Time. The zero
// time is returned when Sleeper hasn't set a start time.
func (d Draft) StartsAt() time.Time {
	if d.StartTime == 0 {
		return time.Time{}
	}
	return time.UnixMilli(d.StartTime)
}

// InProgress reports whether the draft is actively running.
func (d Draft) InProgress() bool { return d.Status == "drafting" }

// DraftPick is one selection. In an auction every roster spot is a pick,
// including keepers, which are distinguished by IsKeeper.
type DraftPick struct {
	DraftID  string `json:"draft_id"`
	PickNo   int    `json:"pick_no"`
	Round    int    `json:"round"`
	RosterID int    `json:"roster_id"`
	PickedBy string `json:"picked_by"`
	PlayerID string `json:"player_id"`
	// DraftSlot is the seat the pick was made from, 1..teams. It is the only
	// ownership Sleeper stamps on a mock pick, which carries neither
	// picked_by nor roster_id.
	DraftSlot int `json:"draft_slot"`
	// IsKeeper is true only for keeper picks. Sleeper sends null rather
	// than false for ordinary auction picks, which decodes to false —
	// the same meaning, so no pointer is needed here.
	//
	// Note that the recorded keeper Amount is whatever Sleeper carried
	// forward; Sleeper does not enforce Hit or Miss's escalating keeper
	// ladder, so it is not the league's true keeper price. See
	// internal/draft/keepers.go, which recomputes it from league rules.
	IsKeeper bool              `json:"is_keeper"`
	Metadata DraftPickMetadata `json:"metadata"`
}

// DraftPickMetadata is the player snapshot Sleeper embeds in each pick.
// It is denormalized at pick time, so it stays correct for historical
// drafts even after a player changes teams.
type DraftPickMetadata struct {
	// Amount is the auction price in whole dollars, encoded as a string
	// ("78"). It is empty for non-auction drafts. Use Dollars.
	Amount       string `json:"amount"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Position     string `json:"position"`
	Team         string `json:"team"`
	Status       string `json:"status"`
	InjuryStatus string `json:"injury_status"`
	YearsExp     string `json:"years_exp"`
}

// Dollars parses Amount, returning 0 when it is absent or unparseable
// (both of which mean "no auction price recorded").
func (m DraftPickMetadata) Dollars() int {
	n, err := strconv.Atoi(m.Amount)
	if err != nil {
		return 0
	}
	return n
}

// Name returns the player's display name as recorded at pick time.
func (m DraftPickMetadata) Name() string {
	switch {
	case m.FirstName == "":
		return m.LastName
	case m.LastName == "":
		return m.FirstName
	}
	return m.FirstName + " " + m.LastName
}

// Transaction is a waiver claim, free agent add, trade, or commissioner
// action. Waiver claims carry the winning FAAB bid, which is how a
// player's acquisition cost is recovered for keeper pricing.
type Transaction struct {
	TransactionID string `json:"transaction_id"`
	// Type is "waiver", "free_agent", "trade", or "commissioner".
	Type string `json:"type"`
	// Status is "complete" or "failed". Failed waiver claims are losing
	// bids and must be excluded from acquisition-cost lookups.
	Status string `json:"status"`
	// Leg is the week the transaction processed in.
	Leg       int   `json:"leg"`
	RosterIDs []int `json:"roster_ids"`
	// Adds and Drops map player_id to the roster_id gaining or losing
	// the player. Both are nil for transactions that move no players.
	Adds     map[string]int       `json:"adds"`
	Drops    map[string]int       `json:"drops"`
	Created  int64                `json:"created"`
	Settings *TransactionSettings `json:"settings"`
}

// TransactionSettings carries waiver-specific fields. It is null for free
// agent adds and trades, so it is a pointer; use Bid rather than reaching
// into it directly.
type TransactionSettings struct {
	WaiverBid int `json:"waiver_bid"`
	Seq       int `json:"seq"`
}

// Bid returns the winning FAAB bid, or 0 for a transaction that had no
// bid attached (free agent adds and trades). A $0 winning bid and "no bid
// at all" are the same thing for keeper pricing: both floor to the
// league's $10 keeper minimum.
func (t Transaction) Bid() int {
	if t.Settings == nil {
		return 0
	}
	return t.Settings.WaiverBid
}

// Player is the subset of Sleeper's player dictionary this tool needs.
// SearchFullName is Sleeper's own normalized name ("aaronjones") and is
// the join key for matching externally-sourced rankings to player IDs.
type Player struct {
	PlayerID         string   `json:"player_id"`
	FirstName        string   `json:"first_name"`
	LastName         string   `json:"last_name"`
	FullName         string   `json:"full_name"`
	SearchFullName   string   `json:"search_full_name"`
	Position         string   `json:"position"`
	Team             string   `json:"team"`
	FantasyPositions []string `json:"fantasy_positions"`
	YearsExp         int      `json:"years_exp"`
	Status           string   `json:"status"`
	// InjuryStatus is the current designation (PUP, IR, Questionable,
	// Out) and is empty for healthy players. Sleeper refreshes it daily,
	// so it is only as current as the last fetch of this dictionary.
	InjuryStatus string `json:"injury_status"`
	Active       bool   `json:"active"`
}

// Drafts fetches every draft belonging to leagueID, newest first.
func (c *Client) Drafts(leagueID string) ([]Draft, error) {
	var drafts []Draft
	err := c.get("/league/"+leagueID+"/drafts", &drafts)
	return drafts, err
}

// Draft fetches a single draft's settings and status.
func (c *Client) Draft(draftID string) (Draft, error) {
	var d Draft
	err := c.getFresh("/draft/"+draftID, &d)
	return d, err
}

// DraftPicks fetches every pick made in draftID.
func (c *Client) DraftPicks(draftID string) ([]DraftPick, error) {
	var picks []DraftPick
	err := c.getFresh("/draft/"+draftID+"/picks", &picks)
	return picks, err
}

// Transactions fetches every transaction that processed in the given week
// of leagueID, including failed waiver claims.
func (c *Client) Transactions(leagueID string, week int) ([]Transaction, error) {
	var txns []Transaction
	err := c.get(fmt.Sprintf("/league/%s/transactions/%d", leagueID, week), &txns)
	return txns, err
}

// SeasonStats fetches per-player season totals keyed by player ID, with
// each player's stats keyed by Sleeper's stat name ("rec", "rush_td").
// seasonType is typically "regular".
//
// Sleeper includes precomputed pts_ppr/pts_half_ppr/pts_std, but those
// use Sleeper's default scoring rather than the league's, so callers that
// need exact league points should recompute from the raw components.
func (c *Client) SeasonStats(seasonType, season string) (map[string]map[string]float64, error) {
	var stats map[string]map[string]float64
	err := c.get(fmt.Sprintf("/stats/nfl/%s/%s", seasonType, season), &stats)
	return stats, err
}

// Players fetches Sleeper's full player dictionary keyed by player ID.
//
// This response is roughly 5 MB and Sleeper asks that it be fetched at
// most once per day, so callers are expected to cache it; the ingest
// layer snapshots it to disk. Never call this during a live draft.
func (c *Client) Players() (map[string]Player, error) {
	var players map[string]Player
	err := c.get("/players/nfl", &players)
	return players, err
}

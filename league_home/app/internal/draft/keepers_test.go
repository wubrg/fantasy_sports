package draft

import (
	"slices"
	"testing"

	"leaguehome/internal/sleeper"
)

// owners is the roster->owner mapping shared by the fixtures below.
var testRosters = []sleeper.Roster{
	{RosterID: 1, OwnerID: "alice"},
	{RosterID: 2, OwnerID: "bob"},
}

func pick(owner, playerID string, amount string, keeper bool) sleeper.DraftPick {
	rosterID := 1
	if owner == "bob" {
		rosterID = 2
	}
	return sleeper.DraftPick{
		RosterID: rosterID,
		PickedBy: owner,
		PlayerID: playerID,
		IsKeeper: keeper,
		Metadata: sleeper.DraftPickMetadata{
			Amount: amount, FirstName: "Player", LastName: playerID, Position: "RB",
		},
	}
}

func waiver(rosterID int, playerID string, bid int, created int64) sleeper.Transaction {
	return sleeper.Transaction{
		Type: "waiver", Status: "complete", Created: created,
		Adds: map[string]int{playerID: rosterID}, RosterIDs: []int{rosterID},
		Settings: &sleeper.TransactionSettings{WaiverBid: bid},
	}
}

func season(year string, picks []sleeper.DraftPick, txns ...sleeper.Transaction) SeasonData {
	return SeasonData{Year: year, Picks: picks, Transactions: txns, Rosters: testRosters}
}

func findEntry(t *testing.T, l *Ledger, year, playerID string) Entry {
	t.Helper()
	for _, e := range l.Entries {
		if e.Season == year && e.PlayerID == playerID {
			return e
		}
	}
	t.Fatalf("no ledger entry for %s in %s", playerID, year)
	return Entry{}
}

// TestLedgerChainsAcrossFourSeasons is the core case: a player drafted for
// $10 and kept three times must walk 15 -> 25 -> 40, with each year's price
// derived from the previous year's *computed* price rather than whatever
// Sleeper recorded.
func TestLedgerChainsAcrossFourSeasons(t *testing.T) {
	seasons := []SeasonData{
		season("2021", []sleeper.DraftPick{pick("alice", "p1", "10", false)}),
		// Sleeper records a stale $10 every year; the ladder must ignore it.
		season("2022", []sleeper.DraftPick{pick("alice", "p1", "10", true)}),
		season("2023", []sleeper.DraftPick{pick("alice", "p1", "10", true)}),
		season("2024", []sleeper.DraftPick{pick("alice", "p1", "10", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 3 {
		t.Fatalf("expected 3 keeper entries, got %d", len(l.Entries))
	}

	want := []struct {
		year      string
		keepCount int
		prior     int
		price     int
	}{
		{"2022", 1, 10, 15},
		{"2023", 2, 15, 25},
		{"2024", 3, 25, 40},
	}
	for _, w := range want {
		e := findEntry(t, l, w.year, "p1")
		if e.KeepCount != w.keepCount || e.PriorValue != w.prior || e.LeaguePrice != w.price {
			t.Errorf("%s: keepCount=%d prior=%d price=%d, want %d/%d/%d",
				w.year, e.KeepCount, e.PriorValue, e.LeaguePrice, w.keepCount, w.prior, w.price)
		}
	}

	// The whole reason this package exists: by year three the league price
	// is $40 while Sleeper still says $10.
	final := findEntry(t, l, "2024", "p1")
	if final.SleeperAmount != 10 || final.Variance() != 30 {
		t.Errorf("expected variance of 30 against Sleeper's $10, got %d (sleeper=%d)",
			final.Variance(), final.SleeperAmount)
	}
}

// TestLedgerFAABBidDoesNotCarryForward pins the reading of draft.md that
// league practice supports: a waiver pickup is "undrafted" for keeper
// purposes no matter how large the winning bid was.
//
// Measured against five seasons of real keeper records, this reading
// reproduces 49 of 67 recorded prices; carrying the bid forward reproduces
// only 41 and nearly doubles total variance.
func TestLedgerFAABBidDoesNotCarryForward(t *testing.T) {
	seasons := []SeasonData{
		season("2024", nil, waiver(1, "p2", 26, 100)),
		season("2025", []sleeper.DraftPick{pick("alice", "p2", "10", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "p2")
	if e.PriorMethod != MethodWaiver {
		t.Errorf("PriorMethod = %q, want %q", e.PriorMethod, MethodWaiver)
	}
	if e.PriorValue != 0 || e.LeaguePrice != 10 {
		t.Errorf("prior=%d price=%d, want 0/10", e.PriorValue, e.LeaguePrice)
	}
}

// TestLedgerFAABBidCarriesWhenConfigured exercises the other reading, kept
// available so the LM can switch interpretations without a code change.
func TestLedgerFAABBidCarriesWhenConfigured(t *testing.T) {
	rules := DefaultRules()
	rules.WaiverBidCarriesForward = true

	seasons := []SeasonData{
		season("2024", nil, waiver(1, "p2", 26, 100)),
		season("2025", []sleeper.DraftPick{pick("alice", "p2", "31", true)}),
	}
	l, err := BuildLedger(seasons, rules)
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2025", "p2"); e.PriorValue != 26 || e.LeaguePrice != 31 {
		t.Errorf("prior=%d price=%d, want 26/31", e.PriorValue, e.LeaguePrice)
	}
}

// TestLedgerFreeAgentPickupFloorsToMinimum is the other half of that rule:
// an undrafted, unbid-on pickup is kept at the $10 minimum.
func TestLedgerFreeAgentPickupFloorsToMinimum(t *testing.T) {
	freeAdd := sleeper.Transaction{
		Type: "free_agent", Status: "complete", Created: 100,
		Adds: map[string]int{"p3": 1}, RosterIDs: []int{1},
	}
	seasons := []SeasonData{
		season("2024", nil, freeAdd),
		season("2025", []sleeper.DraftPick{pick("alice", "p3", "1", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "p3")
	if e.LeaguePrice != 10 || !slices.Contains(e.Flags, FlagFloored) {
		t.Errorf("price=%d flags=%v, want 10 and a floored flag", e.LeaguePrice, e.Flags)
	}
}

// TestLedgerIgnoresFailedWaiverBids guards a real hazard: Sleeper returns
// losing bids too, and counting them would overstate acquisition cost.
func TestLedgerIgnoresFailedWaiverBids(t *testing.T) {
	// Bid-carrying is enabled here purely so the chosen bid is visible in
	// the resulting price; the point under test is which bid wins.
	rules := DefaultRules()
	rules.WaiverBidCarriesForward = true

	losing := waiver(1, "p4", 99, 50)
	losing.Status = "failed"
	winning := waiver(1, "p4", 5, 60)

	seasons := []SeasonData{
		season("2024", nil, losing, winning),
		season("2025", []sleeper.DraftPick{pick("alice", "p4", "10", true)}),
	}
	l, err := BuildLedger(seasons, rules)
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "p4")
	if e.PriorValue != 5 {
		t.Errorf("PriorValue = %d, want 5 (the winning bid, not the failed $99)", e.PriorValue)
	}
}

// TestLedgerLastAcquisitionWins covers a player added, dropped, and added
// back at a different price within one season.
func TestLedgerLastAcquisitionWins(t *testing.T) {
	rules := DefaultRules()
	rules.WaiverBidCarriesForward = true

	seasons := []SeasonData{
		season("2024", nil, waiver(1, "p5", 3, 10), waiver(1, "p5", 17, 900)),
		season("2025", []sleeper.DraftPick{pick("alice", "p5", "22", true)}),
	}
	l, err := BuildLedger(seasons, rules)
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2025", "p5"); e.PriorValue != 17 {
		t.Errorf("PriorValue = %d, want 17 (the later add)", e.PriorValue)
	}
}

// TestLedgerCarriesBasisThroughTrade encodes observed league practice: a
// traded player keeps his cost basis, so the acquiring manager inherits it.
// Saquon Barkley is the real example — drafted for $51 by one manager in
// 2024 and kept for $56 by a different one in 2025.
func TestLedgerCarriesBasisThroughTrade(t *testing.T) {
	trade := sleeper.Transaction{
		Type: "trade", Status: "complete", Created: 100,
		Adds: map[string]int{"p6": 2}, RosterIDs: []int{1, 2},
	}
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p6", "51", false)}, trade),
		season("2025", []sleeper.DraftPick{pick("bob", "p6", "56", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "p6")
	if e.PriorValue != 51 || e.LeaguePrice != 56 {
		t.Errorf("prior=%d price=%d, want 51/56 — the basis must survive the trade",
			e.PriorValue, e.LeaguePrice)
	}
	if !slices.Contains(e.Flags, FlagChangedHands) {
		t.Errorf("expected %q flag, got %v", FlagChangedHands, e.Flags)
	}
}

// TestLedgerCarriesKeepCountThroughTrade is the Amon-Ra St. Brown case: his
// keep count kept climbing ($10 -> $20 -> $35) even as he changed hands, so
// the ladder position belongs to the player, not the manager.
func TestLedgerCarriesKeepCountThroughTrade(t *testing.T) {
	trade := sleeper.Transaction{
		Type: "trade", Status: "complete", Created: 100,
		Adds: map[string]int{"p10": 2}, RosterIDs: []int{1, 2},
	}
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p10", "5", false)}),
		season("2024", []sleeper.DraftPick{pick("alice", "p10", "10", true)}, trade),
		season("2025", []sleeper.DraftPick{pick("bob", "p10", "20", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2024", "p10"); e.KeepCount != 1 || e.LeaguePrice != 10 {
		t.Errorf("2024: keepCount=%d price=%d, want 1/10", e.KeepCount, e.LeaguePrice)
	}
	e := findEntry(t, l, "2025", "p10")
	if e.KeepCount != 2 || e.LeaguePrice != 20 {
		t.Errorf("2025: keepCount=%d price=%d, want 2/20 after changing hands",
			e.KeepCount, e.LeaguePrice)
	}
}

// TestLedgerPickupResetsBasis is the De'Von Achane case: a big FAAB bid
// does not buy a cost basis. He was drafted for $1, picked up on waivers by
// another manager, and kept at the $10 minimum.
func TestLedgerPickupResetsBasis(t *testing.T) {
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p11", "1", false)}, waiver(2, "p11", 56, 500)),
		season("2024", []sleeper.DraftPick{pick("bob", "p11", "10", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2024", "p11")
	if e.PriorValue != 0 || e.LeaguePrice != 10 {
		t.Errorf("prior=%d price=%d, want 0/10 — a FAAB bid does not carry forward",
			e.PriorValue, e.LeaguePrice)
	}
	if !slices.Contains(e.Flags, FlagResetByPickup) {
		t.Errorf("expected %q flag, got %v", FlagResetByPickup, e.Flags)
	}
}

// TestLedgerSelfPickupDoesNotReset covers ordinary roster churn: a manager
// dropping and re-adding their own player for a bye week must not restart
// the keeper ladder. Amon-Ra St. Brown and Sam LaPorta are the real cases —
// both appear as in-season free agent adds by the manager already holding
// them, and both kept climbing.
func TestLedgerSelfPickupDoesNotReset(t *testing.T) {
	selfAdd := sleeper.Transaction{
		Type: "free_agent", Status: "complete", Created: 500,
		Adds: map[string]int{"p13": 1}, RosterIDs: []int{1},
	}
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p13", "5", false)}),
		season("2024", []sleeper.DraftPick{pick("alice", "p13", "10", true)}, selfAdd),
		season("2025", []sleeper.DraftPick{pick("alice", "p13", "20", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "p13")
	if e.KeepCount != 2 || e.PriorValue != 10 || e.LeaguePrice != 20 {
		t.Errorf("keepCount=%d prior=%d price=%d, want 2/10/20 — a self re-add is not an acquisition",
			e.KeepCount, e.PriorValue, e.LeaguePrice)
	}
}

// TestLedgerPickupByAnotherOwnerStillResets is the other side of that rule:
// churn is only churn when it's your own player.
func TestLedgerPickupByAnotherOwnerStillResets(t *testing.T) {
	rivalAdd := sleeper.Transaction{
		Type: "free_agent", Status: "complete", Created: 500,
		Adds: map[string]int{"p14": 2}, RosterIDs: []int{2},
	}
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p14", "40", false)}, rivalAdd),
		season("2025", []sleeper.DraftPick{pick("bob", "p14", "10", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2025", "p14"); e.PriorValue != 0 || e.LeaguePrice != 10 {
		t.Errorf("prior=%d price=%d, want 0/10", e.PriorValue, e.LeaguePrice)
	}
}

// TestLedgerBasisPersistsAcrossAnUnkeptSeason covers a player who sits on a
// roster for a year without being kept or redrafted: the basis is still his.
func TestLedgerBasisPersistsAcrossAnUnkeptSeason(t *testing.T) {
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p12", "30", false)}),
		season("2024", nil),
		season("2025", []sleeper.DraftPick{pick("alice", "p12", "35", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if e := findEntry(t, l, "2025", "p12"); e.PriorValue != 30 || e.LeaguePrice != 35 {
		t.Errorf("prior=%d price=%d, want 30/35", e.PriorValue, e.LeaguePrice)
	}
}

func TestLedgerFlagsMissingPriorRecord(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "other", "5", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "ghost", "20", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(t, l, "2025", "ghost")
	if !slices.Contains(e.Flags, FlagNoPriorRecord) {
		t.Errorf("expected %q flag, got %v", FlagNoPriorRecord, e.Flags)
	}
	if e.PriorMethod != "unknown" {
		t.Errorf("PriorMethod = %q, want unknown", e.PriorMethod)
	}
}

// TestLedgerPropagatesCorruptSeason is the 2022 case: prices chained out of
// a season whose records are known bad must be marked, not trusted.
func TestLedgerPropagatesCorruptSeason(t *testing.T) {
	bad := season("2022", []sleeper.DraftPick{pick("alice", "p7", "1", true)})
	bad.Corrupt = true
	bad.CorruptReason = "all keepers recorded at $1"

	seasons := []SeasonData{
		season("2021", []sleeper.DraftPick{pick("alice", "p7", "30", false)}),
		bad,
		season("2023", []sleeper.DraftPick{pick("alice", "p7", "1", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	// 2022 itself chains off clean 2021 data, so it is not flagged...
	if e := findEntry(t, l, "2022", "p7"); slices.Contains(e.Flags, FlagPriorSeasonCorrupt) {
		t.Errorf("2022 chains off clean 2021 data and should not be flagged: %v", e.Flags)
	}
	// ...but 2023 reads its prior value out of the corrupt season.
	e := findEntry(t, l, "2023", "p7")
	if !slices.Contains(e.Flags, FlagPriorSeasonCorrupt) {
		t.Errorf("expected %q flag on 2023, got %v", FlagPriorSeasonCorrupt, e.Flags)
	}
}

func TestBudgetsSubtractKeeperPrices(t *testing.T) {
	entries := []Entry{
		{OwnerID: "alice", LeaguePrice: 40},
		{OwnerID: "alice", LeaguePrice: 25},
		{OwnerID: "bob", LeaguePrice: 10},
	}
	got := Budgets(entries, 200)
	if got["alice"] != 135 {
		t.Errorf("alice budget = %d, want 135", got["alice"])
	}
	if got["bob"] != 190 {
		t.Errorf("bob budget = %d, want 190", got["bob"])
	}
	// An owner who keeps nobody is absent, and callers treat that as the
	// full budget rather than $0 — a distinction that would otherwise
	// silently zero out a rival's spending power.
	if _, ok := got["carol"]; ok {
		t.Error("owners with no keepers should not appear in the budget map")
	}
}

// TestPriceDeclaredForUpcomingSeason is the 2026 path: keepers named by
// hand before Sleeper knows about them.
func TestPriceDeclaredForUpcomingSeason(t *testing.T) {
	seasons := []SeasonData{
		season("2024", []sleeper.DraftPick{pick("alice", "p8", "20", false)}),
		season("2025", []sleeper.DraftPick{pick("alice", "p8", "20", true)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}

	entries, err := l.PriceDeclared("2026", []Declared{{OwnerID: "alice", PlayerID: "p8"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	// Kept once in 2025 at $25, so a second keep in 2026 costs $25+$10.
	if e.KeepCount != 2 || e.PriorValue != 25 || e.LeaguePrice != 35 {
		t.Errorf("keepCount=%d prior=%d price=%d, want 2/25/35",
			e.KeepCount, e.PriorValue, e.LeaguePrice)
	}
	if e.SleeperAmount != 0 {
		t.Errorf("SleeperAmount = %d, want 0 for a not-yet-drafted keeper", e.SleeperAmount)
	}
}

func TestPriceDeclaredWithoutHistoryErrors(t *testing.T) {
	l, err := BuildLedger(nil, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.PriceDeclared("2026", []Declared{{OwnerID: "alice", PlayerID: "x"}}); err == nil {
		t.Error("expected an error when the ledger has no completed seasons")
	}
}

// TestBuildLedgerSortsSeasons ensures the chain is walked chronologically
// even when callers hand seasons over out of order.
func TestBuildLedgerSortsSeasons(t *testing.T) {
	seasons := []SeasonData{
		season("2023", []sleeper.DraftPick{pick("alice", "p9", "10", true)}),
		season("2022", []sleeper.DraftPick{pick("alice", "p9", "10", false)}),
	}
	l, err := BuildLedger(seasons, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Seasons; !slices.Equal(got, []string{"2022", "2023"}) {
		t.Fatalf("seasons = %v, want ascending order", got)
	}
	if e := findEntry(t, l, "2023", "p9"); e.PriorValue != 10 || e.LeaguePrice != 15 {
		t.Errorf("prior=%d price=%d, want 10/15", e.PriorValue, e.LeaguePrice)
	}
}

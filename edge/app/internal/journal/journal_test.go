package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
	"edge/internal/wager"
)

func tmp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// grantTime is fixed and earlier than every Place call's "now" in this file, so
// a grant is always visible to the Balances replay Place performs. Using
// time.Now() here would make the tests flaky: whenever the wall clock runs past
// noon UTC on the fixed test date below, a grant timestamped "now" would sort
// after Place's "now" and get truncated out of the balance, wrongly reading as
// zero.
var grantTime = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func grant(t *testing.T, path, id, book, asset string, amount float64) {
	t.Helper()
	ev := ledger.Event{
		Kind: ledger.KindGrant, ID: id, Time: grantTime,
		Creates: &ledger.Lot{ID: id, Book: book, Asset: asset, Amount: amount},
	}
	if err := ledger.AppendFile(path, ev); err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func TestPlace_writesBetlogAndDebitsLedger(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 10)

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "bonus bet", Stake: 4, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if id == "" {
		t.Fatal("expected a wager id")
	}

	// Betlog has the bet.
	bets, err := betlog.Load(bl)
	if err != nil {
		t.Fatalf("betlog.Load: %v", err)
	}
	if len(bets) != 1 || bets[0].Bet.Selection != "Barkley ATD" {
		t.Fatalf("betlog missing the bet: %+v", bets)
	}
	if bets[0].ID != id {
		t.Fatalf("betlog id %q != returned id %q", bets[0].ID, id)
	}

	// Ledger has a place tied to the wager.
	evs, err := ledger.Load(lg)
	if err != nil {
		t.Fatalf("ledger.Load: %v", err)
	}
	var placed float64
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Wager == id {
			placed += e.Amount
		}
	}
	if placed != 4 {
		t.Fatalf("expected 4 placed against %s, got %v", id, placed)
	}
}

func TestPlace_appliesBoost(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-cash", "fanatics", ledger.Cash, 50)
	// A boost lot to be consumed.
	boost := ledger.Event{
		Kind: ledger.KindGrant, ID: "fan-boost", Time: grantTime,
		Creates: &ledger.Lot{ID: "fan-boost", Book: "fanatics", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.3, MaxStake: 50, MinOdds: -200, RequiresCashStake: true}},
	}
	if err := ledger.AppendFile(lg, boost); err != nil {
		t.Fatalf("grant boost: %v", err)
	}

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:        betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "real money", Stake: 50, Week: 1},
		Book:       "fanatics",
		BoostLotID: "fan-boost",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	evs, _ := ledger.Load(lg)
	sawExpire := false
	for _, e := range evs {
		if e.Kind == ledger.KindExpire && e.Lot == "fan-boost" && e.Wager == id {
			sawExpire = true
		}
	}
	if !sawExpire {
		t.Fatal("boost lot was not expired against the wager")
	}
}

func TestPlace_insufficientBalanceWritesNothing(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 1)

	_, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "too big", Price: wager.American(-110), Bankroll: "bonus bet", Stake: 5, Week: 1},
		Book: "draftkings",
	}, time.Now())
	if err == nil {
		t.Fatal("expected an insufficient-balance error")
	}
	if _, statErr := os.Stat(bl); !os.IsNotExist(statErr) {
		if b, _ := os.ReadFile(bl); strings.TrimSpace(string(b)) != "" {
			t.Fatalf("betlog should be empty after a failed debit, got: %s", b)
		}
	}
}

func TestPlace_unknownBoostLotFails(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-cash", "fanatics", ledger.Cash, 50)

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	_, err := Place(bl, lg, PlaceRequest{
		Bet:        betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "real money", Stake: 50, Week: 1},
		Book:       "fanatics",
		BoostLotID: "no-such-boost",
	}, now)
	if err == nil {
		t.Fatal("expected an error for a nonexistent boost lot")
	}

	if _, statErr := os.Stat(bl); !os.IsNotExist(statErr) {
		if b, _ := os.ReadFile(bl); strings.TrimSpace(string(b)) != "" {
			t.Fatalf("betlog should be empty after a bad boost, got: %s", b)
		}
	}
	evs, _ := ledger.Load(lg)
	for _, e := range evs {
		if e.Kind == ledger.KindExpire {
			t.Fatalf("no expire event should have been written for an unknown boost lot: %+v", e)
		}
	}
}

func TestPlace_boostLotWrongBookFails(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-cash", "draftkings", ledger.Cash, 50)
	boost := ledger.Event{
		Kind: ledger.KindGrant, ID: "fan-boost", Time: grantTime,
		Creates: &ledger.Lot{ID: "fan-boost", Book: "fanatics", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.3, MaxStake: 50, MinOdds: -200, RequiresCashStake: true}},
	}
	if err := ledger.AppendFile(lg, boost); err != nil {
		t.Fatalf("grant boost: %v", err)
	}

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	_, err := Place(bl, lg, PlaceRequest{
		Bet:        betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "real money", Stake: 50, Week: 1},
		Book:       "draftkings",
		BoostLotID: "fan-boost",
	}, now)
	if err == nil {
		t.Fatal("expected an error for a boost lot from a different book")
	}

	evs, _ := ledger.Load(lg)
	for _, e := range evs {
		if e.Kind == ledger.KindExpire {
			t.Fatalf("no expire event should have been written for a cross-book boost: %+v", e)
		}
	}
}

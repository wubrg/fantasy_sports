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

func TestPlace_depositFundsTheWager(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	// No prior balance. Without a deposit this would fail (insufficient funds);
	// with Deposit it self-funds: a grant of the stake precedes the debit.
	id, err := Place(bl, lg, PlaceRequest{
		Bet:     betlog.Bet{Selection: "Nabers over", Price: wager.American(-115), Bankroll: "real money", Stake: 25, Week: 2},
		Book:    "fanatics",
		Deposit: true,
	}, now)
	if err != nil {
		t.Fatalf("Place with deposit: %v", err)
	}
	if bets, err := betlog.Load(bl); err != nil || len(bets) != 1 || bets[0].ID != id {
		t.Fatalf("betlog missing the funded bet: %+v (%v)", bets, err)
	}

	// Self-funded: grant +25 then debit -25 nets to zero cash on the book.
	evs, err := ledger.Load(lg)
	if err != nil {
		t.Fatalf("ledger.Load: %v", err)
	}
	pos, err := ledger.Balances(evs, now)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if got := pos.Total("fanatics", ledger.Cash); got != 0 {
		t.Fatalf("self-funded bet should net 0 cash, got %v", got)
	}
	// The deposit is a grant of the stake; the debit is a place tied to the wager.
	var granted, placed float64
	for _, e := range evs {
		if e.Kind == ledger.KindGrant && e.Creates != nil && e.Creates.Asset == ledger.Cash {
			granted += e.Creates.Amount
		}
		if e.Kind == ledger.KindPlace && e.Wager == id {
			placed += e.Amount
		}
	}
	if granted != 25 || placed != 25 {
		t.Fatalf("expected deposit 25 and debit 25, got grant %v / place %v", granted, placed)
	}
}

func TestPlace_depositPreservesPriorBalance(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-bonus", "fanatics", ledger.Bonus, 30) // existing balance
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	// A funded bonus bet: the deposit covers the stake, so the prior $30 stays.
	if _, err := Place(bl, lg, PlaceRequest{
		Bet:     betlog.Bet{Selection: "Chase ATD", Price: wager.American(120), Bankroll: "bonus bet", Stake: 5, Week: 2},
		Book:    "fanatics",
		Deposit: true,
	}, now); err != nil {
		t.Fatalf("Place with deposit: %v", err)
	}
	evs, _ := ledger.Load(lg)
	pos, err := ledger.Balances(evs, now)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if got := pos.Total("fanatics", ledger.Bonus); got != 30 {
		t.Fatalf("prior balance should be untouched at 30, got %v", got)
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

	// A boost applied to a wager is consumed as a KindPlace of the boost lot tied
	// to that wager (a commitment), not a KindExpire ("died unused").
	evs, _ := ledger.Load(lg)
	sawBoostPlace := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Lot == "fan-boost" && e.Wager == id {
			sawBoostPlace = true
		}
		if e.Kind == ledger.KindExpire && e.Lot == "fan-boost" {
			t.Fatalf("boost was expired, not placed: %+v", e)
		}
	}
	if !sawBoostPlace {
		t.Fatal("boost lot was not placed against the wager")
	}
	// The boost lot is consumed: it is no longer a live lot.
	pos, err := ledger.Balances(evs, now)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	for _, l := range pos.Lots {
		if l.ID == "fan-boost" {
			t.Fatal("boost lot should have been consumed by the place")
		}
	}
}

// A RequiresCashStake boost applied to a bonus bet must be refused before any
// write: no betlog entry, and the boost lot is not consumed.
func TestPlace_cashStakeBoostOnBonusBetFails(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-bonus", "fanatics", ledger.Bonus, 50)
	boost := ledger.Event{
		Kind: ledger.KindGrant, ID: "fan-boost", Time: grantTime,
		Creates: &ledger.Lot{ID: "fan-boost", Book: "fanatics", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.5, MaxStake: 50, MinOdds: -200, RequiresCashStake: true}},
	}
	if err := ledger.AppendFile(lg, boost); err != nil {
		t.Fatalf("grant boost: %v", err)
	}

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	_, err := Place(bl, lg, PlaceRequest{
		Bet:        betlog.Bet{Selection: "Barkley ATD", Price: wager.American(-115), Bankroll: "bonus bet", Stake: 50, Week: 1},
		Book:       "fanatics",
		BoostLotID: "fan-boost",
	}, now)
	if err == nil {
		t.Fatal("expected a RequiresCashStake boost on a bonus bet to be refused")
	}

	// No betlog entry written.
	if _, statErr := os.Stat(bl); !os.IsNotExist(statErr) {
		if b, _ := os.ReadFile(bl); strings.TrimSpace(string(b)) != "" {
			t.Fatalf("betlog should be empty after a refused boost, got: %s", b)
		}
	}
	// The boost lot is not consumed: still a live lot.
	evs, _ := ledger.Load(lg)
	for _, e := range evs {
		if (e.Kind == ledger.KindPlace || e.Kind == ledger.KindExpire) && e.Lot == "fan-boost" {
			t.Fatalf("boost lot must not be consumed by a refused place: %+v", e)
		}
	}
	pos, err := ledger.Balances(evs, now)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	live := false
	for _, l := range pos.Lots {
		if l.ID == "fan-boost" {
			live = true
		}
	}
	if !live {
		t.Fatal("boost lot should still be live after a refused place")
	}
}

// A RequiresCashStake boost applied to a CASH bet succeeds and is consumed as a
// place tied to the wager.
func TestPlace_cashStakeBoostOnCashBetSucceeds(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "fan-cash", "fanatics", ledger.Cash, 50)
	boost := ledger.Event{
		Kind: ledger.KindGrant, ID: "fan-boost", Time: grantTime,
		Creates: &ledger.Lot{ID: "fan-boost", Book: "fanatics", Asset: ledger.Boost,
			Boost: &ledger.BoostSpec{Percent: 0.5, MaxStake: 50, MinOdds: -200, RequiresCashStake: true}},
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
	sawBoostPlace := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Lot == "fan-boost" && e.Wager == id {
			sawBoostPlace = true
		}
	}
	if !sawBoostPlace {
		t.Fatal("boost lot was not placed against the cash wager")
	}
	pos, err := ledger.Balances(evs, now)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	for _, l := range pos.Lots {
		if l.ID == "fan-boost" {
			t.Fatal("boost lot should have been consumed")
		}
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

func TestSettle_writesBothAndGuardsDoubleSettle(t *testing.T) {
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

	if err := Settle(bl, lg, id, betlog.Won, nil, "scored", now); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	// Betlog shows settled.
	bets, _ := betlog.Load(bl)
	if bets[0].Result != betlog.Won {
		t.Fatalf("betlog result = %q, want won", bets[0].Result)
	}
	// Ledger has a settle tied to the wager.
	evs, _ := ledger.Load(lg)
	sawSettle := false
	for _, e := range evs {
		if e.Kind == ledger.KindSettle && e.Wager == id {
			sawSettle = true
		}
	}
	if !sawSettle {
		t.Fatal("ledger settle not written")
	}

	// Second settle is refused.
	if err := Settle(bl, lg, id, betlog.Lost, nil, "oops", now); err == nil {
		t.Fatal("expected double-settle to be refused")
	}
}

func TestSettle_predictionWithoutLedgerPlace(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	// A bet recorded with no book -> no ledger place exists.
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet: betlog.Bet{Selection: "pure prediction", Price: wager.American(200), Bankroll: "bonus bet", Stake: 1, Week: 1},
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	// Settling must not error even though there is no ledger place to settle.
	if err := Settle(bl, lg, id, betlog.Lost, nil, "", now); err != nil {
		t.Fatalf("Settle prediction-only: %v", err)
	}
}

// settleReturns returns the Returns lot from the ledger settle event for a
// wager, or nil if there is none.
func settleReturns(t *testing.T, ledgerPath, id string) *ledger.Lot {
	t.Helper()
	evs, err := ledger.Load(ledgerPath)
	if err != nil {
		t.Fatalf("ledger.Load: %v", err)
	}
	for _, e := range evs {
		if e.Kind == ledger.KindSettle && e.Wager == id {
			return e.Returns
		}
	}
	t.Fatalf("no ledger settle for %s", id)
	return nil
}

// A WON cash bet settled with returns=nil (the GUI path) books a returns lot of
// stake + profit, as cash, at the book the stake was placed from.
func TestSettle_computesReturnsForWonCashBet(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-cash", "draftkings", ledger.Cash, 100)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: wager.American(150), Bankroll: "real money", Stake: 50, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	if err := Settle(bl, lg, id, betlog.Won, nil, "scored", now); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	r := settleReturns(t, lg, id)
	if r == nil {
		t.Fatal("expected a computed returns lot on a won cash bet")
	}
	// +150 -> profit multiple 1.5; stake 50 back + 75 profit = 125.
	if r.Amount != 125 {
		t.Fatalf("returns amount = %v, want 125 (stake 50 + profit 75)", r.Amount)
	}
	if r.Asset != ledger.Cash {
		t.Fatalf("returns asset = %q, want cash", r.Asset)
	}
	if r.Book != "draftkings" {
		t.Fatalf("returns book = %q, want draftkings", r.Book)
	}
}

// A WON bonus bet settled with returns=nil books profit only (the bonus stake
// is not returned), as cash.
func TestSettle_computesReturnsForWonBonusBet(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-bonus", "draftkings", ledger.Bonus, 50)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: wager.American(200), Bankroll: "bonus bet", Stake: 50, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	if err := Settle(bl, lg, id, betlog.Won, nil, "scored", now); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	r := settleReturns(t, lg, id)
	if r == nil {
		t.Fatal("expected a computed returns lot on a won bonus bet")
	}
	// +200 -> profit multiple 2.0; profit only = 50 * 2 = 100 (stake not returned).
	if r.Amount != 100 {
		t.Fatalf("returns amount = %v, want 100 (profit only)", r.Amount)
	}
	if r.Asset != ledger.Cash {
		t.Fatalf("returns asset = %q, want cash", r.Asset)
	}
}

// An explicit non-nil returns (the CLI -returns path) is written unchanged, not
// recomputed.
func TestSettle_explicitReturnsUsedUnchanged(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-cash", "draftkings", ledger.Cash, 100)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	id, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "Barkley ATD", Price: wager.American(150), Bankroll: "real money", Stake: 50, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	want := &ledger.Lot{ID: "manual-returns", Book: "draftkings", Asset: ledger.Cash, Amount: 200}
	if err := Settle(bl, lg, id, betlog.Won, want, "manual", now); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	r := settleReturns(t, lg, id)
	if r == nil {
		t.Fatal("expected the explicit returns lot")
	}
	if r.Amount != 200 || r.ID != "manual-returns" {
		t.Fatalf("explicit returns not used unchanged: %+v", r)
	}
}

// A stake drawn from earlier winnings (a lot created by a prior settle's
// Returns, not a grant) must still resolve its book, so a won bet funded from
// winnings books its returns correctly rather than failing on an empty book.
func TestSettle_returnsBookResolvesFromWinningsLot(t *testing.T) {
	bl, lg := tmp(t, "bets.jsonl"), tmp(t, "bank.jsonl")
	grant(t, lg, "dk-seed", "draftkings", ledger.Cash, 10)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	// Bet 1: win a cash bet -> pays out a winnings (Returns) cash lot at draftkings.
	id1, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "seed win", Price: wager.American(100), Bankroll: "real money", Stake: 10, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place bet1: %v", err)
	}
	if err := Settle(bl, lg, id1, betlog.Won, nil, "", now); err != nil {
		t.Fatalf("Settle bet1: %v", err)
	}

	// Bet 2: stake it from that winnings lot (draftkings cash is now the payout,
	// not the consumed grant) and win.
	id2, err := Place(bl, lg, PlaceRequest{
		Bet:  betlog.Bet{Selection: "spend winnings", Price: wager.American(100), Bankroll: "real money", Stake: 10, Week: 1},
		Book: "draftkings",
	}, now)
	if err != nil {
		t.Fatalf("Place bet2: %v", err)
	}
	if err := Settle(bl, lg, id2, betlog.Won, nil, "", now); err != nil {
		t.Fatalf("Settle bet2 (winnings-sourced stake): %v", err)
	}

	r := settleReturns(t, lg, id2)
	if r == nil {
		t.Fatal("expected computed returns on the winnings-sourced won bet")
	}
	if r.Book != "draftkings" {
		t.Fatalf("returns book = %q, want draftkings (resolved from a Returns-created lot)", r.Book)
	}
	if r.Amount != 20 {
		t.Fatalf("returns amount = %v, want 20", r.Amount)
	}
}

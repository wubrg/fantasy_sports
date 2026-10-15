package main

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"edge/internal/betlog"
	"edge/internal/ledger"
	"edge/internal/wager"
)

func TestBetPlace_writesBothLogs(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")
	// Seed a bonus lot.
	_ = ledger.AppendFile(lg, ledger.Event{
		Kind: ledger.KindGrant, ID: "dk-bonus", Time: time.Now(),
		Creates: &ledger.Lot{ID: "dk-bonus", Book: "draftkings", Asset: ledger.Bonus, Amount: 10},
	})

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Barkley ATD", "-price", "-115", "-stake", "4",
		"-book", "draftkings", "-bankroll", "bonus", "-week", "1",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	if len(bets) != 1 {
		t.Fatalf("expected 1 betlog entry, got %d", len(bets))
	}
	evs, _ := ledger.Load(lg)
	placed := false
	for _, e := range evs {
		if e.Kind == ledger.KindPlace && e.Wager == bets[0].ID {
			placed = true
		}
	}
	if !placed {
		t.Fatal("no ledger place tied to the wager")
	}
}

func TestBetPlace_predictedDefaultsToRawImplied(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Barkley ATD", "-price", "-110", "-stake", "4",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	want, _ := wager.American(-110).ImpliedRaw()
	if math.Abs(bets[0].Bet.Predicted-want) > 1e-9 {
		t.Errorf("predicted = %v, want raw implied %v", bets[0].Bet.Predicted, want)
	}
}

func TestBetPlace_predictedFromOpposite(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "SEA -2.5", "-price", "-115", "-opposite", "-105", "-stake", "10",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	m := wager.Market{A: wager.American(-115), B: wager.American(-105)}
	wantA, _, err := m.FairDevig()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(bets[0].Bet.Predicted-wantA) > 1e-9 {
		t.Errorf("predicted = %v, want de-vigged fair %v", bets[0].Bet.Predicted, wantA)
	}
}

func TestBetPlace_predictedFromCorrelation(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "SGP: A + B", "-price", "360", "-stake", "10",
		"-legs", "Leg A:-110,Leg B:150", "-correlation", "0.3",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	pA, _ := wager.American(-110).ImpliedRaw()
	pB, _ := wager.American(150).ImpliedRaw()
	want, err := wager.FrechetBlend(pA, pB, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(bets[0].Bet.Predicted-want) > 1e-9 {
		t.Errorf("predicted = %v, want Frechet floor %v", bets[0].Bet.Predicted, want)
	}
}

func TestBetPlace_predictedFromBeliefQR(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Josh Allen 250+ Pass Yds", "-price", "-110", "-stake", "10",
		"-belief", "0.5", "-q", "0.6", "-r", "0.2",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	want, err := wager.BlendedProb(0.5, 0.6, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(bets[0].Bet.Predicted-want) > 1e-9 {
		t.Errorf("predicted = %v, want blended %v", bets[0].Bet.Predicted, want)
	}
	if bets[0].Bet.SYours != 0.5 || bets[0].Bet.Q != 0.6 || bets[0].Bet.R != 0.2 {
		t.Errorf("provenance not recorded: SYours=%v Q=%v R=%v", bets[0].Bet.SYours, bets[0].Bet.Q, bets[0].Bet.R)
	}
}

func TestBetPlace_explicitPredictedWins(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "SEA -2.5", "-price", "-115", "-opposite", "-105", "-stake", "10",
		"-predicted", "0.75",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	if bets[0].Bet.Predicted != 0.75 {
		t.Errorf("predicted = %v, want explicit 0.75 to win over -opposite's computed value", bets[0].Bet.Predicted)
	}
}

func TestBetPlace_predictedZeroIsTreatedAsUnsetAndComputed(t *testing.T) {
	dir := t.TempDir()
	bl, lg := filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "l.jsonl")

	err := betCmd([]string{
		"place", "-betlog", bl, "-ledger", lg,
		"-selection", "Barkley ATD", "-price", "-110", "-stake", "4",
		"-predicted", "0",
	})
	if err != nil {
		t.Fatalf("bet place: %v", err)
	}

	bets, _ := betlog.Load(bl)
	want, _ := wager.American(-110).ImpliedRaw()
	if math.Abs(bets[0].Bet.Predicted-want) > 1e-9 {
		t.Errorf("predicted = %v, want raw implied %v (explicit 0 should fall through to auto-compute)", bets[0].Bet.Predicted, want)
	}
}

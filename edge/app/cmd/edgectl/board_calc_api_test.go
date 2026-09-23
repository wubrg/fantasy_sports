package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"edge/internal/betlog"
	"edge/internal/wager"
)

func doCalc(t *testing.T, handler func(http.ResponseWriter, *http.Request), body string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	handler(rr, req)
	var m map[string]any
	if rr.Body.Len() > 0 {
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatalf("bad json: %v: %s", err, rr.Body.String())
		}
	}
	return rr.Code, m
}

// TestHandleHitRateMatchesCLI pins the JSON endpoint against the same numbers
// `edgectl hitrate -line 52.5 -side over -values "48,55,60,51,52,49"` prints:
// only 55 and 60 clear 52.5, so 2 of 6 games, a 33.3% point estimate.
func TestHandleHitRateMatchesCLI(t *testing.T) {
	srv := &boardServer{}
	code, m := doCalc(t, srv.handleHitRate, `{"values":"48,55,60,51,52,49","line":52.5,"side":"over"}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, m)
	}
	if m["hits"].(float64) != 2 || m["n"].(float64) != 6 {
		t.Errorf("hits/n = %v/%v, want 2/6", m["hits"], m["n"])
	}
	if math.Abs(m["rate"].(float64)-1.0/3) > 1e-9 {
		t.Errorf("rate = %v, want 0.3333", m["rate"])
	}
	wantLower, wantUpper, err := wager.WilsonInterval(2, 6, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(m["lower"].(float64)-wantLower) > 1e-9 {
		t.Errorf("lower = %v, want %v", m["lower"], wantLower)
	}
	if math.Abs(m["upper"].(float64)-wantUpper) > 1e-9 {
		t.Errorf("upper = %v, want %v", m["upper"], wantUpper)
	}
}

// TestHandleHitRateWithPriceVerdict pins the verdict/breakeven fields that
// only appear when a price is supplied, mirroring -price on the CLI.
func TestHandleHitRateWithPriceVerdict(t *testing.T) {
	srv := &boardServer{}
	code, m := doCalc(t, srv.handleHitRate, `{"values":"1,1,1,1,0","line":0.5,"side":"over","price":-110}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, m)
	}
	if m["verdict"] == nil || m["verdict"] == "" {
		t.Errorf("expected a verdict when price is supplied, got %v", m)
	}
	if m["breakeven"].(float64) < 0.52 || m["breakeven"].(float64) > 0.53 {
		t.Errorf("breakeven = %v, want ~0.5238 (-110)", m["breakeven"])
	}
}

// TestHandleHitRateRequiresLine pins the same "line is required" guard the
// CLI applies via fs.Visit -- an omitted line must not silently default to 0.
func TestHandleHitRateRequiresLine(t *testing.T) {
	srv := &boardServer{}
	code, m := doCalc(t, srv.handleHitRate, `{"values":"1,2,3"}`)
	if code != 400 {
		t.Fatalf("status %d, want 400: %v", code, m)
	}
}

// TestHandleParlayCombineCrossGame pins the combined price against
// wager.CombineParlay directly, and against what `edgectl parlay -legs
// "-110:gameA,+150:gameB" -total 10` would compute (the CLI prints the same
// Combo.Price/Implied via wager.BuildTicket -> wager.RoundRobin ->
// combineIdx, the same math CombineParlay runs for two legs).
func TestHandleParlayCombineCrossGame(t *testing.T) {
	srv := &boardServer{}
	code, m := doCalc(t, srv.handleParlayCombine,
		`{"legs":[{"selection":"A","price":-110,"game":"gameA"},{"selection":"B","price":150,"game":"gameB"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, m)
	}
	combo, err := wager.CombineParlay([]wager.American{-110, 150})
	if err != nil {
		t.Fatal(err)
	}
	if int(m["price"].(float64)) != int(combo.Price) {
		t.Errorf("price = %v, want %v", m["price"], combo.Price)
	}
	if math.Abs(m["implied"].(float64)-combo.Implied) > 1e-9 {
		t.Errorf("implied = %v, want %v", m["implied"], combo.Implied)
	}
}

// TestHandleParlayCombineRefusesSameGame pins the same-game refusal: legs
// sharing a game tag must not be silently multiplied together, matching
// guardSameGame's behavior in the CLI.
func TestHandleParlayCombineRefusesSameGame(t *testing.T) {
	srv := &boardServer{}
	code, m := doCalc(t, srv.handleParlayCombine,
		`{"legs":[{"selection":"A","price":-110,"game":"gameA"},{"selection":"B","price":150,"game":"gameA"}]}`)
	if code != 409 {
		t.Fatalf("status %d, want 409: %v", code, m)
	}
	if !strings.Contains(m["error"].(string), "correlated") {
		t.Errorf("error = %v, want it to explain the correlation refusal", m["error"])
	}
}

// TestHandleParlayCombineRequiresTwoLegs pins the same "needs at least 2 legs"
// floor CombineParlay itself enforces.
func TestHandleParlayCombineRequiresTwoLegs(t *testing.T) {
	srv := &boardServer{}
	code, _ := doCalc(t, srv.handleParlayCombine, `{"legs":[{"selection":"A","price":-110,"game":"gameA"}]}`)
	if code != 400 {
		t.Fatalf("status %d, want 400", code)
	}
}

// TestHandlePlaceWithLegs pins that a placed multi-leg wager carries its legs
// through to the betlog, via the same Legs field the CLI's -legs flag fills.
func TestHandlePlaceWithLegs(t *testing.T) {
	dir := t.TempDir()
	srv := &boardServer{betlogPath: dir + "/log.jsonl", ledgerPath: dir + "/bankroll.jsonl"}
	code, m := doCalc(t, srv.handlePlace, `{
		"selection":"SGP: A + B","price":250,"stake":10,"bankroll":"bonus bet",
		"legs":[{"selection":"A -110","price":-110},{"selection":"B +150","price":150}]
	}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, m)
	}
	bets, err := betlog.Load(srv.betlogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(bets) != 1 {
		t.Fatalf("expected 1 bet, got %d", len(bets))
	}
	if len(bets[0].Bet.Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d: %+v", len(bets[0].Bet.Legs), bets[0].Bet.Legs)
	}
	if bets[0].Bet.Legs[0].Selection != "A -110" || bets[0].Bet.Legs[1].Price != 150 {
		t.Errorf("legs did not round-trip: %+v", bets[0].Bet.Legs)
	}
}

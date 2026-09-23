package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromoGrantListDelete(t *testing.T) {
	led := filepath.Join(t.TempDir(), "bankroll.jsonl")
	srv := &boardServer{ledgerPath: led}

	do := func(handler string, body string) map[string]any {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		switch handler {
		case "boosts":
			srv.handleBoosts(rr, req)
		case "expire":
			srv.handleExpire(rr, req)
		}
		if rr.Code != 200 {
			t.Fatalf("%s: status %d: %s", handler, rr.Code, rr.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatalf("%s: bad json: %v", handler, err)
		}
		return m
	}

	// Grant a boost and a no-sweat through the same endpoint.
	do("boosts", `{"book":"fanatics","percent":0.2,"max_stake":50,"market":"sgp","min_odds":100,"needs_cash":true}`)
	ns := do("boosts", `{"kind":"nosweat","book":"draftkings","max_stake":10,"market":"atd"}`)
	if ns["kind"] != "nosweat" {
		t.Fatalf("no-sweat grant did not report its kind: %v", ns)
	}

	// List: both appear, tagged by kind, each with an id.
	rr := httptest.NewRecorder()
	srv.handleBoosts(rr, httptest.NewRequest("GET", "/api/boosts", nil))
	var list struct {
		Boosts []struct {
			ID, Kind, Market string
			MaxStake         float64 `json:"max_stake"`
		} `json:"boosts"`
	}
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Boosts) != 2 {
		t.Fatalf("expected 2 promos, got %d", len(list.Boosts))
	}
	var boostID, kinds string
	var sawNoSweat bool
	for _, b := range list.Boosts {
		kinds += b.Kind + " "
		if b.Kind == "boost" {
			boostID = b.ID
			if b.Market != "sgp" {
				t.Errorf("boost market = %q, want sgp", b.Market)
			}
		}
		if b.Kind == "nosweat" {
			sawNoSweat = true
			if b.MaxStake != 10 {
				t.Errorf("no-sweat max = %.0f, want 10", b.MaxStake)
			}
		}
	}
	if !sawNoSweat {
		t.Fatalf("no-sweat not listed (kinds seen: %s)", kinds)
	}

	// Delete the boost by id; only the no-sweat remains.
	do("expire", `{"id":"`+boostID+`"}`)
	rr = httptest.NewRecorder()
	srv.handleBoosts(rr, httptest.NewRequest("GET", "/api/boosts", nil))
	var after struct {
		Boosts []struct{ Kind string } `json:"boosts"`
	}
	json.Unmarshal(rr.Body.Bytes(), &after)
	if len(after.Boosts) != 1 || after.Boosts[0].Kind != "nosweat" {
		t.Fatalf("after deleting the boost, expected only the no-sweat, got %+v", after.Boosts)
	}

	// Expiring an unknown lot is refused, not silently absorbed.
	rr = httptest.NewRecorder()
	srv.handleExpire(rr, httptest.NewRequest("POST", "/x", strings.NewReader(`{"id":"nope"}`)))
	if rr.Code == 200 {
		t.Error("expiring a nonexistent lot should fail")
	}
}

// TestBoostWeekAutoExpiry covers the week-scoped promo lifecycle: a boost
// declared with a week tag and no explicit expires date auto-expires at that
// week's own boundary (the same window weekWindow computes for the period
// report), and an explicit expires date still overrides it.
func TestBoostWeekAutoExpiry(t *testing.T) {
	dir := t.TempDir()
	wk(t, dir, 2, "2026-09-14T13:00")
	wk(t, dir, 3, "2026-09-21T13:00")
	led := filepath.Join(t.TempDir(), "bankroll.jsonl")
	srv := &boardServer{dir: dir, ledgerPath: led}

	do := func(body string) map[string]any {
		rr := httptest.NewRecorder()
		srv.handleBoosts(rr, httptest.NewRequest("POST", "/x", strings.NewReader(body)))
		if rr.Code != 200 {
			t.Fatalf("boosts: status %d: %s", rr.Code, rr.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		return m
	}

	do(`{"book":"fanatics","percent":0.2,"max_stake":50,"week":2}`)
	do(`{"kind":"nosweat","book":"fanatics","max_stake":10,"week":2}`)
	// An explicit expires date wins over the week tag.
	do(`{"book":"draftkings","percent":0.5,"max_stake":25,"week":2,"expires":"2026-12-25"}`)

	_, wantEnd, err := weekWindow(dir, 2)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	srv.handleBoosts(rr, httptest.NewRequest("GET", "/api/boosts", nil))
	var list struct {
		Boosts []struct {
			Book    string `json:"book"`
			Expires string `json:"expires"`
		} `json:"boosts"`
	}
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Boosts) != 3 {
		t.Fatalf("expected 3 promos, got %d", len(list.Boosts))
	}
	for _, b := range list.Boosts {
		switch b.Book {
		case "fanatics":
			if b.Expires != wantEnd.Format("2006-01-02") {
				t.Errorf("%s: expires = %q, want week 2's end %q", b.Book, b.Expires, wantEnd.Format("2006-01-02"))
			}
		case "draftkings":
			if b.Expires != "2026-12-25" {
				t.Errorf("draftkings: expires = %q, want the explicit override 2026-12-25", b.Expires)
			}
		}
	}
}

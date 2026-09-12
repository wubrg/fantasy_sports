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

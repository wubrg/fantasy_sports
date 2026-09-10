package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type propsResp struct {
	Groups []struct {
		Category string
		Rows     []struct {
			Selection string
			Implied   float64
			BoostBE   *float64 `json:"boost_be"`
			Fair      *float64
		}
	}
	Sources []map[string]string
	Note    string
}

func getProps(t *testing.T, srv *boardServer) propsResp {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.handleProps(rr, httptest.NewRequest("GET", "/api/props", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var r propsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rr.Body.String())
	}
	return r
}

func TestHandlePropsPricesCapture(t *testing.T) {
	dir := t.TempDir()
	// A DK sportscontent body: a two-sided moneyline (de-vig) + a one-sided prop
	// ladder rung (boost breakeven).
	body := `{"events":[{"id":"E1","name":"NE @ SEA"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Moneyline"},` +
		`{"id":"M2","eventId":"E1","name":"JSN Receiving Yards"}],` +
		`"selections":[{"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"+140"}},` +
		`{"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-166"}},` +
		`{"marketId":"M2","label":"JSN 100+","displayOdds":{"american":"+135"},"points":100}]}`
	esc, _ := json.Marshal(body) // embed as a HAR response text string
	har := `{"log":{"entries":[{"response":{"content":{"text":` + string(esc) + `}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "dk.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}

	r := getProps(t, &boardServer{ingestDir: dir})
	if len(r.Sources) != 1 {
		t.Fatalf("want 1 source, got %d", len(r.Sources))
	}

	var game, recv *struct {
		Selection string
		Implied   float64
		BoostBE   *float64 `json:"boost_be"`
		Fair      *float64
	}
	for gi := range r.Groups {
		for ri := range r.Groups[gi].Rows {
			row := &r.Groups[gi].Rows[ri]
			if r.Groups[gi].Category == "Game" && row.Selection == "NE Patriots" {
				game = row
			}
			if r.Groups[gi].Category == "Receiving" {
				recv = row
			}
		}
	}
	if game == nil || game.Fair == nil {
		t.Fatalf("moneyline should be de-vigged with a fair prob: %+v", game)
	}
	if math.Abs(*game.Fair-0.40) > 0.02 { // NE +140 vs SEA -166 de-vig ~40%
		t.Errorf("NE fair de-vig = %.3f, want ~0.40", *game.Fair)
	}
	if recv == nil || recv.BoostBE == nil || recv.Fair != nil {
		t.Fatalf("one-sided prop should have a boost breakeven and no fair: %+v", recv)
	}
}

func TestHandlePropsEmptyAndUnconfigured(t *testing.T) {
	// Unconfigured ingest dir: a note, no crash.
	if r := getProps(t, &boardServer{ingestDir: ""}); r.Note == "" {
		t.Error("an unconfigured ingest dir should return a note")
	}
	// Empty folder: no groups, no note (nothing wrong, just nothing there).
	if r := getProps(t, &boardServer{ingestDir: t.TempDir()}); len(r.Groups) != 0 {
		t.Errorf("empty folder should yield no groups, got %d", len(r.Groups))
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"leaguehome/internal/draft"
)

// leansPage fetches the merged picture the way the page does.
func leansPage(t *testing.T, srv *server) leansPayload {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleLeansPage(rec, httptest.NewRequest(http.MethodGet, "/api/leans", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out leansPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rowFor(p leansPayload, name string) (leanRow, bool) {
	for _, r := range p.Rows {
		if r.Player == name {
			return r, true
		}
	}
	return leanRow{}, false
}

// TestLeansPageShowsTheMergedPictureNotTheFile — the page has to show what the
// board is applying, which is the file plus anything set since startup. A page
// that read the file alone would disagree with the board it sits beside.
func TestLeansPageShowsTheMergedPictureNotTheFile(t *testing.T) {
	srv, _ := leanServerFile(t, "mine.yaml", "up:\n  - Jahmyr Gibbs\n")

	if r, ok := rowFor(leansPage(t, srv), "Jahmyr Gibbs"); !ok || r.Lean != "up" {
		t.Fatalf("before the edit: %+v ok=%v, want up from the file", r, ok)
	}

	srv.leans.set("Jahmyr Gibbs", draft.LeanDND)
	r, ok := rowFor(leansPage(t, srv), "Jahmyr Gibbs")
	if !ok || r.Lean != "dnd" {
		t.Errorf("after the edit: %+v ok=%v, want the live read", r, ok)
	}
}

// TestLeansPageCarriesTheFavoriteTag — it is the one field the page can change
// that the board cannot, so it has to arrive.
func TestLeansPageCarriesTheFavoriteTag(t *testing.T) {
	srv, _ := leanServerFile(t, "mine.yaml",
		"up:\n  - Jahmyr Gibbs\nfavorites:\n  - Jahmyr Gibbs\n")
	r, ok := rowFor(leansPage(t, srv), "Jahmyr Gibbs")
	if !ok || !r.Favorite {
		t.Errorf("row = %+v ok=%v, want the favorite tag carried through", r, ok)
	}
}

// TestLeansPageKeepsCapAndNote — read-only on the page, but it is the only
// screen that shows them at all; the board has nowhere to put either.
func TestLeansPageKeepsCapAndNote(t *testing.T) {
	srv, _ := leanServer(t,
		"player,lean,cap,note\nJahmyr Gibbs,must,40,\"the ceiling sets the bid\"\n")
	r, ok := rowFor(leansPage(t, srv), "Jahmyr Gibbs")
	if !ok {
		t.Fatal("no row for a player the file names")
	}
	if r.Cap != 40 || r.Note == "" {
		t.Errorf("cap %d note %q, want the file's own values shown", r.Cap, r.Note)
	}
}

// TestLeansPageNamesTheWritableSet — edits land in one file and it may be a
// symlink into a notes vault, so the page has to be able to say which.
func TestLeansPageNamesTheWritableSet(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml", "up:\n  - Jahmyr Gibbs\n")
	got := leansPage(t, srv)
	if got.Writable != path {
		t.Errorf("writable = %q, want %q", got.Writable, path)
	}
	if len(got.Sets) == 0 {
		t.Fatal("no sets reported")
	}
	var writable int
	for _, s := range got.Sets {
		if s.Writable {
			writable++
		}
	}
	if writable != 1 {
		t.Errorf("%d sets marked writable, want exactly one", writable)
	}
}

// TestLeansPageOffersOnlyReadsTheServerAccepts — the cycle is shipped to the
// page so it cannot offer a value handleLean would reject.
func TestLeansPageOffersOnlyReadsTheServerAccepts(t *testing.T) {
	srv, _ := leanServerFile(t, "mine.yaml", "up:\n  - Jahmyr Gibbs\n")
	got := leansPage(t, srv)
	if len(got.Cycle) != len(leanCycle) {
		t.Fatalf("cycle = %v, want the server's own %v", got.Cycle, leanCycle)
	}
	for i, l := range leanCycle {
		if got.Cycle[i] != string(l) {
			t.Errorf("cycle[%d] = %q, want %q", i, got.Cycle[i], l)
		}
	}
}

// TestLeansPageRejectsNonGET — it changes nothing, so a POST to it is a
// caller's mistake worth reporting rather than absorbing.
func TestLeansPageRejectsNonGET(t *testing.T) {
	srv, _ := leanServerFile(t, "mine.yaml", "up:\n  - Jahmyr Gibbs\n")
	rec := httptest.NewRecorder()
	srv.handleLeansPage(rec, httptest.NewRequest(http.MethodPost, "/api/leans", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rec.Code)
	}
}

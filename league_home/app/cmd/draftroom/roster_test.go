package main

import (
	"testing"
)

// rows flattens the panel into one list, since which slot a player lands in
// is the lineup's business and not what these tests are about.
func rows(v ScratchView) map[string]ScratchSpot {
	out := map[string]ScratchSpot{}
	for _, r := range append(append([]ScratchSpot(nil), v.Starters...), v.Bench...) {
		out[r.PlayerID] = r
	}
	return out
}

// TestPanelShowsPlayersYouHaveWon is the point of the panel.
//
// Before this, the board could tell you that you had $47 and eleven slots
// left and never once tell you who you owned: Me carries Budget, OpenSlots
// and StartersNeeded, and nothing that names a player. The panel already drew
// a lineup, scored it and counted its composition — it had simply never been
// told about the players you bought.
func TestPanelShowsPlayersYouHaveWon(t *testing.T) {
	srv := scratchServer(t)
	srv.taken["1"] = gone{price: 83, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())
	got, ok := rows(view)["1"]
	if !ok {
		t.Fatalf("a player you won is not on your own roster: %+v", view)
	}
	if got.Price != 83 {
		t.Errorf("price = $%d, want the $83 actually paid", got.Price)
	}
	if !got.Won {
		t.Error("a player bought at auction should read as won")
	}
	if got.Kept {
		t.Error("a player bought at auction is not a keeper")
	}
	// Empty means "nothing to clear", not "no rows". A player you won is not
	// cleared by the clear-tries button, so a roster made only of won players
	// still reports empty — which is what keeps that button from being live
	// with nothing for it to do.
	if !view.Empty {
		t.Error("a won player is not a try, so there is still nothing to clear")
	}
}

// A pick someone else won is not yours, and must not appear on your roster.
// The board learns this from isMine; the panel must not undo it.
func TestPanelIgnoresPicksThatAreNotYours(t *testing.T) {
	srv := scratchServer(t)
	srv.taken["1"] = gone{price: 83, mine: true}
	srv.taken["2"] = gone{price: 63, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	got := rows(srv.scratchView(srv.snapshot()))
	if _, ok := got["2"]; ok {
		t.Error("someone else's pick landed on your roster")
	}
	if _, ok := got["1"]; !ok {
		t.Error("your own pick is missing")
	}
}

// TestATryYouWinConvertsRatherThanDropping is the sharp edge.
//
// A player penciled onto the panel is resolved against the board, and winning
// him takes him off it — so the one moment he became really yours was the
// moment the panel reported him Dropped and forgot him. He has to convert to
// an owned row instead, at what he actually cost rather than what you guessed.
func TestATryYouWinConvertsRatherThanDropping(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 40) // the guess
	srv.taken["1"] = gone{price: 83, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())
	if len(view.Dropped) != 0 {
		t.Errorf("a player you won was reported as dropped: %v", view.Dropped)
	}
	got := rows(view)
	if len(got) != 1 {
		t.Fatalf("want one row, got %d — he is on the roster twice: %+v", len(got), got)
	}
	if spot := got["1"]; spot.Price != 83 || !spot.Won {
		t.Errorf("want the $83 he cost, marked won, got %+v", spot)
	}
}

// The inverse, which must keep working: a try someone else wins really is
// gone, and planning around him would be worse than losing the note.
func TestATrySomeoneElseWinsStillDrops(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 40)
	srv.taken["1"] = gone{price: 83, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())
	if len(view.Dropped) != 1 {
		t.Errorf("Dropped = %v, want the one player who left", view.Dropped)
	}
	if _, ok := rows(view)["1"]; ok {
		t.Error("a player someone else won is still on your roster")
	}
}

// The live budget is already net of what you have won, so the panel must not
// spend it again. This is the arithmetic that would be wrong in a way nobody
// notices until a bid is a tier too small.
func TestWonPlayersAreNotCountedTwice(t *testing.T) {
	srv := scratchServer(t)
	srv.taken["1"] = gone{price: 83, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	snap := srv.snapshot()
	view := srv.scratchView(snap)

	if view.BudgetLeft != snap.Me.Budget {
		t.Errorf("BudgetLeft = $%d, want the board's $%d: the win was spent twice",
			view.BudgetLeft, snap.Me.Budget)
	}
	if view.SlotsLeft != snap.Me.OpenSlots {
		t.Errorf("SlotsLeft = %d, want the board's %d", view.SlotsLeft, snap.Me.OpenSlots)
	}
}

// "clear tries" clears what you were imagining, not what you own. The two
// live in different places precisely so this cannot go wrong, which is worth
// a test rather than an assumption.
func TestClearingTriesLeavesYourTeamAlone(t *testing.T) {
	srv := scratchServer(t)
	srv.taken["1"] = gone{price: 83, mine: true}
	srv.scratch.add("2", 30)
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	if len(rows(srv.scratchView(srv.snapshot()))) != 2 {
		t.Fatal("expected the try and the won player before clearing")
	}

	srv.scratch.clear()

	got := rows(srv.scratchView(srv.snapshot()))
	if _, ok := got["1"]; !ok {
		t.Error("clearing tries removed a player you own")
	}
	if _, ok := got["2"]; ok {
		t.Error("clearing tries left the try behind")
	}
}

// A sale corrected by hand outranks what the feed said, which is the
// precedence the board itself applies — so the panel has to agree with the
// budget rather than showing a price the board no longer believes.
func TestAHandCorrectedSaleWinsOverTheFeed(t *testing.T) {
	srv := scratchServer(t)
	srv.taken["1"] = gone{price: 83, mine: true}
	srv.manual["1"] = gone{price: 91, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	got := rows(srv.scratchView(srv.snapshot()))
	if len(got) != 1 {
		t.Fatalf("one player, two records, want one row: %+v", got)
	}
	if got["1"].Price != 91 {
		t.Errorf("price = $%d, want the hand-corrected $91: the panel and the "+
			"board's budget must agree about what a player cost", got["1"].Price)
	}
}

package main

import (
	"reflect"
	"testing"

	"leaguehome/internal/draft"
)

func scratchServer(t *testing.T) *server {
	t.Helper()
	srv, err := newServer(testStatic())
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestScratchDoesNotTouchTheLiveBoard is the load-bearing test.
//
// A scratch pick and a real sale both look like a player leaving the board,
// so a leak between them would corrupt draft-night arithmetic invisibly.
func TestScratchDoesNotTouchTheLiveBoard(t *testing.T) {
	srv := scratchServer(t)
	before := srv.snapshot()

	srv.scratch.add("1", 45)
	srv.scratch.add("2", 30)

	after := srv.snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Error("the live snapshot changed when only the scratchpad was touched")
	}
	if len(srv.manual) != 0 {
		t.Errorf("scratch picks leaked into recorded sales: %v", srv.manual)
	}
	if len(srv.taken) != 0 {
		t.Errorf("scratch picks leaked into the live taken set: %v", srv.taken)
	}
	if after.Me.Budget != before.Me.Budget {
		t.Errorf("live budget moved: $%d -> $%d", before.Me.Budget, after.Me.Budget)
	}
	// And the players are still biddable on the real board.
	for _, id := range []string{"1", "2"} {
		found := false
		for _, p := range after.Players {
			if p.PlayerID == id {
				found = true
			}
		}
		if !found {
			t.Errorf("player %s left the live board because of a scratch pick", id)
		}
	}
}

// TestRecordedSalesStillWorkAlongsideAScratchRoster — the inverse: a real
// sale must move the live board while the scratchpad carries on.
func TestRecordedSalesStillWorkAlongsideAScratchRoster(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 45)

	srv.manual["2"] = gone{price: 40, mine: true}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	snap := srv.snapshot()

	if snap.Me.Budget != 160 {
		t.Errorf("live budget = $%d, want $160 after a real $40 buy", snap.Me.Budget)
	}
	// The scratch pick is untouched and still scores.
	view := srv.scratchView(snap)
	if len(view.Starters)+len(view.Bench) != 1 {
		t.Errorf("scratch roster lost its pick: %+v", view)
	}
}

func TestScratchBudgetConservation(t *testing.T) {
	srv := scratchServer(t)
	start := srv.snapshot().Me.Budget

	steps := []struct {
		add    bool
		id     string
		price  int
		expect int
	}{
		{true, "1", 45, 45},
		{true, "2", 30, 75},
		{false, "1", 0, 30},
		{true, "3", 10, 40},
		{true, "1", 20, 60}, // re-added at a different price
	}
	for i, step := range steps {
		if step.add {
			srv.scratch.add(step.id, step.price)
		} else {
			srv.scratch.remove(step.id)
		}
		view := srv.scratchView(srv.snapshot())
		if view.Metrics.Spend != step.expect {
			t.Errorf("step %d: spend $%d, want $%d", i, view.Metrics.Spend, step.expect)
		}
		if view.Metrics.Spend+view.BudgetLeft != start {
			t.Errorf("step %d: $%d spent + $%d left != $%d start",
				i, view.Metrics.Spend, view.BudgetLeft, start)
		}
	}
}

// TestRepricingDoesNotDuplicate — adding a player already held changes his
// price rather than buying him twice.
func TestRepricingDoesNotDuplicate(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 45)
	srv.scratch.add("1", 60)

	view := srv.scratchView(srv.snapshot())
	if n := len(view.Starters) + len(view.Bench); n != 1 {
		t.Fatalf("holding %d copies of one player", n)
	}
	if view.Metrics.Spend != 60 {
		t.Errorf("spend = $%d, want the re-priced $60", view.Metrics.Spend)
	}
}

// TestScratchDropsPlayersTakenForReal — planning around someone already
// drafted is worse than losing the note.
func TestScratchDropsPlayersTakenForReal(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 45)
	srv.scratch.add("2", 30)

	// Someone else buys the first one.
	srv.taken["1"] = gone{price: 52, mine: false}
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}

	view := srv.scratchView(srv.snapshot())
	if n := len(view.Starters) + len(view.Bench); n != 1 {
		t.Errorf("expected one pick left, got %d", n)
	}
	if len(view.Dropped) != 1 {
		t.Errorf("the drop should be reported, got %v", view.Dropped)
	}
	// And his money comes back, since he was never actually bought.
	if view.Metrics.Spend != 30 {
		t.Errorf("spend = $%d, want $30 with the vanished pick excluded", view.Metrics.Spend)
	}
}

func TestScratchClear(t *testing.T) {
	srv := scratchServer(t)
	srv.scratch.add("1", 45)
	srv.scratch.add("2", 30)
	srv.scratch.clear()

	view := srv.scratchView(srv.snapshot())
	if !view.Empty {
		t.Error("cleared scratchpad should read empty")
	}
	if view.Metrics.Spend != 0 {
		t.Errorf("spend = $%d after clearing", view.Metrics.Spend)
	}
	if view.BudgetLeft != srv.snapshot().Me.Budget {
		t.Error("clearing should restore the full budget")
	}
}

// TestScratchAssignsLineupSlots — the panel has to show where a player
// actually plays, and it goes through the same scorer the archetypes use.
func TestScratchAssignsLineupSlots(t *testing.T) {
	srv := scratchServer(t)
	// testStatic holds one RB, one WR and one TE.
	for _, id := range []string{"1", "2", "3"} {
		srv.scratch.add(id, 10)
	}
	view := srv.scratchView(srv.snapshot())

	slots := map[string]int{}
	for _, s := range view.Starters {
		slots[s.Slot]++
	}
	if slots["RB"] != 1 || slots["WR"] != 1 {
		t.Errorf("expected the back and receiver in their own slots, got %v", slots)
	}
	// A three-player roster cannot field a lineup and must say so.
	if len(view.Unfilled) == 0 {
		t.Error("expected unfilled starting slots to be reported")
	}
}

// TestScratchMaxBidReservesForRemainingSlots
func TestScratchMaxBidReservesForRemainingSlots(t *testing.T) {
	srv := scratchServer(t)
	snap := srv.snapshot()
	view := srv.scratchView(snap)

	// Nothing bought yet: the whole budget less a dollar per other slot.
	want := snap.Me.Budget - (snap.Me.OpenSlots - 1)
	if view.MaxBid != want {
		t.Errorf("max bid = $%d, want $%d", view.MaxBid, want)
	}

	srv.scratch.add("1", 50)
	view = srv.scratchView(srv.snapshot())
	want = (snap.Me.Budget - 50) - (snap.Me.OpenSlots - 1 - 1)
	if view.MaxBid != want {
		t.Errorf("after a $50 pick, max bid = $%d, want $%d", view.MaxBid, want)
	}
}

// TestScratchScoresLikeAnArchetype — a roster entered by hand and one a
// shape produced must be measured identically, or the two views cannot be
// compared.
func TestScratchScoresLikeAnArchetype(t *testing.T) {
	srv := scratchServer(t)
	snap := srv.snapshot()

	r := &draft.Roster{}
	for _, p := range snap.Players {
		srv.scratch.add(p.PlayerID, 10)
		r.Add(p, 10)
	}
	direct := draft.Score(r, srv.scoringBaselines(), srv.static.shape)
	view := srv.scratchView(srv.snapshot())

	if view.Metrics.POPR != direct.POPR {
		t.Errorf("scratch POPR %.1f, direct %.1f", view.Metrics.POPR, direct.POPR)
	}
	if view.Metrics.StartingPoints != direct.StartingPoints {
		t.Errorf("scratch points %.1f, direct %.1f",
			view.Metrics.StartingPoints, direct.StartingPoints)
	}
}

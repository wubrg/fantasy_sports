package main

import (
	"reflect"
	"testing"

	"leaguehome/internal/draft"
)

func scratchServer(t *testing.T) *server {
	t.Helper()
	srv, err := newServer(testStatic(), t.TempDir(), "")
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
	// The scratch pick is untouched and still scores — and the player you
	// actually bought is on the roster beside it, because the panel shows
	// what your team is, not only what you are imagining for it.
	view := srv.scratchView(snap)
	rows := append(append([]ScratchSpot(nil), view.Starters...), view.Bench...)
	if len(rows) != 2 {
		t.Fatalf("want the try and the real buy, got %d rows: %+v", len(rows), rows)
	}
	byID := map[string]ScratchSpot{}
	for _, r := range rows {
		byID[r.PlayerID] = r
	}
	if try := byID["1"]; try.Won || try.Kept || try.Price != 45 {
		t.Errorf("the scratch pick should still read as a try at $45: %+v", try)
	}
	if bought := byID["2"]; !bought.Won || bought.Kept || bought.Price != 40 {
		t.Errorf("the real buy should read as won at $40, not kept: %+v", bought)
	}
	// Only the try comes off the panel's remaining budget. The live budget is
	// already net of the real buy, so counting it here would spend it twice.
	if view.BudgetLeft != snap.Me.Budget-45 {
		t.Errorf("BudgetLeft = $%d, want $%d — the real buy was counted twice",
			view.BudgetLeft, snap.Me.Budget-45)
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
// actually plays, and it goes through the same scorer the live board uses.
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

// TestBenchSlotsFollowsTheShape: the bench is the roster less its starting
// lineup, and both are drawn from the same shape. A bench sized by hand would
// drift from the lineup beside it and the panel would show a roster of the
// wrong length.
func TestBenchSlotsFollowsTheShape(t *testing.T) {
	league := draft.HitOrMissPool() // QB1 RB2 WR3 TE1 + 1 flex = 8 starting
	if got := benchSlots(league); got != rosterSize-8 {
		t.Errorf("benchSlots(league) = %d, want %d", got, rosterSize-8)
	}

	// Derived, not constant: widen the lineup and the bench must give way.
	wider := draft.HitOrMissPool()
	wider.FlexSlots = 3
	if got := benchSlots(wider); got != rosterSize-10 {
		t.Errorf("two extra flex slots should cost the bench two: got %d, want %d", got, rosterSize-10)
	}

	// A lineup bigger than the roster leaves no bench rather than a negative
	// one, which would ask the panel to draw a negative number of rows.
	huge := draft.HitOrMissPool()
	huge.Starters = map[string]int{"QB": 20}
	if got := benchSlots(huge); got != 0 {
		t.Errorf("benchSlots(oversized lineup) = %d, want 0", got)
	}
}

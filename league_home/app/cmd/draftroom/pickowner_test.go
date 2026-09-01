package main

import (
	"testing"

	"leaguehome/internal/sleeper"
)

// TestIsMine — whether a pick is yours, given what Sleeper actually stamps.
//
// The load-bearing case is the mock. Sleeper leaves both picked_by and
// roster_id empty on a mock pick and carries ownership on draft_slot alone, so
// a board that only reads picked_by never sees a rehearsal pick as yours — and
// the budget the rehearsal exists to exercise never moves. Measured against the
// real 2026 mock: every pick had picked_by "". Measured against this league's
// last completed draft: none of 168 did.
func TestIsMine(t *testing.T) {
	const me = "243501760939814912"

	// A board that knows all three, the way one following a mock does.
	full := &staticData{ownerID: me, myRosterID: 4, mySlot: 7}

	for _, tc := range []struct {
		name string
		s    *staticData
		pick sleeper.DraftPick
		want bool
	}{
		// picked_by is authoritative wherever Sleeper sets it, which on a
		// real draft night is every pick.
		{"picked_by mine", full, sleeper.DraftPick{PickedBy: me}, true},
		{"picked_by someone else", full, sleeper.DraftPick{PickedBy: "467790106363686912"}, false},
		// A pick Sleeper attributes to another manager is his, whatever the
		// lower rungs would have said. Without this the ladder would fall
		// through to a slot that happens to match and steal his pick.
		{"picked_by wins over a matching slot", full,
			sleeper.DraftPick{PickedBy: "467790106363686912", RosterID: 4, DraftSlot: 7}, false},

		// This league has cpu_autopick on, and Sleeper leaves picked_by empty
		// on an autopick. Getting autopicked while away from the keyboard must
		// not drop the player off your own board.
		{"autopick by roster", full, sleeper.DraftPick{RosterID: 4}, true},
		{"autopick, another roster", full, sleeper.DraftPick{RosterID: 9}, false},

		// The mock: slot only.
		{"mock pick at my slot", full, sleeper.DraftPick{DraftSlot: 7}, true},
		{"mock pick at another slot", full, sleeper.DraftPick{DraftSlot: 3}, false},

		// Nothing to go on is not a guess.
		{"empty pick", full, sleeper.DraftPick{}, false},

		// A board with no slot known — the real draft, whose draft_order is
		// null — must not start attributing by slot. Every seat would
		// otherwise match the zero value.
		{"no draft order, slot ignored", &staticData{ownerID: me, myRosterID: 4},
			sleeper.DraftPick{DraftSlot: 7}, false},
		{"no roster known, roster ignored", &staticData{ownerID: me, mySlot: 7},
			sleeper.DraftPick{RosterID: 4}, false},

		// Jeff's board runs under a name rather than a Sleeper id, so it
		// matches nobody and resolves neither roster nor slot. Nothing on the
		// board is his, which is the generic board he wants.
		{"a board keyed to nobody owns nothing", &staticData{ownerID: "jeff"},
			sleeper.DraftPick{PickedBy: "", RosterID: 4, DraftSlot: 7}, false},
	} {
		if got := tc.s.isMine(tc.pick); got != tc.want {
			t.Errorf("%s: isMine = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The rungs are tried in the order Sleeper's own data is trustworthy, and a
// board must never claim a pick on a zero value: an unset roster_id and an
// unset draft_slot are both 0, and 0 must match nothing.
func TestIsMineNeverMatchesOnAZeroValue(t *testing.T) {
	s := &staticData{ownerID: "me", myRosterID: 0, mySlot: 0}
	if s.isMine(sleeper.DraftPick{RosterID: 0, DraftSlot: 0}) {
		t.Error("a pick with no ownership at all was claimed")
	}
}

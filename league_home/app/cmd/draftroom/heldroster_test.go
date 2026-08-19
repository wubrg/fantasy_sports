package main

import (
	"testing"

	"leaguehome/internal/draft"
)

// TestHeldRosterCarriesTraits guards the exact line the bug lived on.
//
// The draft package has its own test that a keeper's traits reach the shape
// predicate, but it builds its keepers by hand with the traits already set.
// The bug was never there — it was here, in the join: heldRoster assembled a
// roster spot from name, position, points and price and simply never copied
// the traits across. Every shape made of player types then measured the
// keeper as a blank while he sat in the lineup taking a slot and $35.
//
// So this asserts the wiring, not the mechanism. A test that constructs its
// own keeper cannot fail for this reason, which is why one did not.
func TestHeldRosterCarriesTraits(t *testing.T) {
	s := testStatic()
	s.projected = []draft.Entry{{
		OwnerID: "me", PlayerID: "1", Name: "Jahmyr Gibbs",
		Position: "RB", LeaguePrice: 35,
	}}
	s.traits = map[string]draft.TraitSet{
		"1": {draft.TraitFloor, draft.TraitThreeDown},
	}

	held := s.heldRoster("me")
	if len(held) != 1 {
		t.Fatalf("expected one keeper, got %d", len(held))
	}
	got := held[0]
	if got.Price != 35 {
		t.Errorf("price = $%d, want the league charge of $35", got.Price)
	}
	if !got.Player.Traits.Has(draft.TraitFloor) || !got.Player.Traits.Has(draft.TraitThreeDown) {
		t.Errorf("keeper reached the shapes carrying %v, want both of his traits",
			got.Player.Traits)
	}
}

// TestHeldRosterIsEmptyWithoutAnOwner — the generic view holds nothing, and
// must not invent a keeper for a team that was never named.
func TestHeldRosterIsEmptyWithoutAnOwner(t *testing.T) {
	s := testStatic()
	s.projected = []draft.Entry{{
		OwnerID: "someone-else", PlayerID: "1", Name: "Jahmyr Gibbs",
		Position: "RB", LeaguePrice: 35,
	}}
	if held := s.heldRoster(""); len(held) != 0 {
		t.Errorf("no owner given, but %d keepers were held", len(held))
	}
}

package draft

// TraitArchetypes are roster shapes made of kinds of players rather than
// sizes of bid.
//
// The money shapes ask how to divide a budget between few players and many.
// That is the right question when the budget is all you have. It stops being
// the interesting one when keepers hand you a measurable surplus: the
// distribution is already half-decided by what you kept, and what is left to
// choose is what sort of players the rest of the money buys.
//
// Counted over the starting lineup rather than the whole roster. A bench
// full of high-floor players changes nothing about how a season goes, and
// counting fourteen would let depth buy a label the lineup has not earned.
func TraitArchetypes() []Archetype {
	return []Archetype{
		traitShape(TraitFloor, "Floor Build",
			"a lineup that is hard to lose with — points from catches and yardage, not touchdowns", 5),
		traitShape(TraitRedZone, "Red Zone Corner",
			"corner the touchdowns; the least predictable points in football, bought on purpose", 4),
		traitShape(TraitUpside, "Upside Swing",
			"players the industry or their own projection says are underpriced, with the record still to come", 4),
		traitShape(TraitTargetHog, "Target Volume",
			"buy the targets; the stickiest thing a pass catcher has and the last to disappear in a bad week", 4),
		traitShape(TraitBellCow, "Bell Cows",
			"backs who carry both the ground work and the passing downs, rather than a share of a committee", 2),
		traitShape(TraitDiscounted, "Buy the Injuries",
			"players whose price is suppressed by a designation — the discount and the risk together", 3),
	}
}

// traitShape builds one trait archetype: buy this many of this kind.
//
// There is no per-pick veto. A shape about what you own is a floor to reach
// rather than a ceiling to stay under, and a veto can only ever refuse — it
// would forbid every player without the trait, including the cheap ones
// needed to fill out a legal lineup, and produce a roster with four
// red-zone aces and no quarterback.
func traitShape(t Trait, name, why string, need int) Archetype {
	return Archetype{
		Name:      name,
		Why:       why,
		Trait:     t,
		Allows:    func(r Roster, p PlayerSignals, price int) bool { return true },
		Satisfied: func(r Roster) bool { return startersWith(r, t) >= need },
		// A dollar minimum, because the cheapest player carrying a trait is
		// usually carrying it by accident — a backup with four projected
		// targets is not a target hog, he is a rounding error. The anchors
		// go and buy real ones.
		Anchors: []Anchor{{Trait: t, MinPrice: 4, Count: need}},
	}
}

// startersWith counts the lineup players carrying a trait. Score must have
// run first, which Fill guarantees before asking.
func startersWith(r Roster, t Trait) int {
	n := 0
	for _, s := range r.Players {
		if s.Starting && s.Player.Traits.Has(t) {
			n++
		}
	}
	return n
}

// TraitCounts reports how many starters carry each trait, so a shape can be
// read against the others rather than only against its own target.
func TraitCounts(r Roster) map[Trait]int {
	out := map[Trait]int{}
	for _, t := range TraitNames {
		if n := startersWith(r, t); n > 0 {
			out[t] = n
		}
	}
	return out
}

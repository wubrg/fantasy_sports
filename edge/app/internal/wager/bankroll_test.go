package wager

import "testing"

// TestFanaticsPushRules pins the rule that governed an entire promo campaign.
//
// Fanatics' behaviour was unrecorded for a long time, and CheckBonusMarket
// correctly refused to clear it: answering "fine" for a book whose push rule
// is unknown is the exact failure that check exists to prevent. The rule is
// now recorded from the House Rules (2026-08-21) -- a two-way market with no
// winner voids both sides and returns the stake -- so push-capable markets
// are eligible, and the moneylines-only constraint is lifted.
func TestFanaticsPushRules(t *testing.T) {
	if !Fanatics.Known() {
		t.Fatal("Fanatics is not Known, so CheckBonusMarket will refuse every wager on it")
	}
	if Fanatics.BonusLostOnPush() {
		t.Error("Fanatics returns the stake on a push; it must not be flagged as forfeiting")
	}
	if !Fanatics.BonusSplittable() {
		t.Error("Fanatics allows a bonus balance to be split; that is the whole variance argument")
	}

	// The case that matters: a whole-number spread can push, and at Fanatics
	// that is now allowed.
	if err := CheckBonusMarket(Fanatics, true); err != nil {
		t.Errorf("CheckBonusMarket(Fanatics, canPush) = %v, want nil", err)
	}
	// FanDuel must still refuse it, or the rule has been generalised away.
	if err := CheckBonusMarket(FanDuel, true); err == nil {
		t.Error("FanDuel forfeits on a push and must still be refused")
	}
	// Caesars remains unrecorded and must still fail closed.
	if err := CheckBonusMarket(Book("caesars"), true); err == nil {
		t.Error("caesars has no recorded rules and must not be cleared")
	}
	// Capitalisation must not smuggle a book past the check.
	if !Book("Fanatics").Known() {
		t.Error("Book normalisation stopped working for Fanatics")
	}
}

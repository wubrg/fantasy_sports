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
	// An unrecorded book must still fail closed.
	if err := CheckBonusMarket(Book("unrecordedbook"), true); err == nil {
		t.Error("a book with no recorded rules must not be cleared")
	}
	// Capitalisation must not smuggle a book past the check.
	if !Book("Fanatics").Known() {
		t.Error("Book normalisation stopped working for Fanatics")
	}
}

// TestCaesarsPushRules pins Caesars' policy: same as Bet365, confirmed by the
// operator (2026-09-24) rather than read from a house-rules document -- see
// the comment on the Caesars constant. Bet365 has no explicit case in
// BonusLostOnPush/BonusSplittable and relies on their default (false, false);
// Caesars is expected to fall through the same way.
func TestCaesarsPushRules(t *testing.T) {
	if !Caesars.Known() {
		t.Fatal("Caesars is not Known, so CheckBonusMarket will refuse every wager on it")
	}
	if Caesars.BonusLostOnPush() != Bet365.BonusLostOnPush() {
		t.Error("Caesars is documented to share Bet365's push policy but disagrees with it")
	}
	if Caesars.BonusSplittable() != Bet365.BonusSplittable() {
		t.Error("Caesars is documented to share Bet365's split policy but disagrees with it")
	}
	// The case that matters: a whole-number spread can push, and neither
	// Caesars nor Bet365 forfeits the bonus over it.
	if err := CheckBonusMarket(Caesars, true); err != nil {
		t.Errorf("CheckBonusMarket(Caesars, canPush) = %v, want nil", err)
	}
}

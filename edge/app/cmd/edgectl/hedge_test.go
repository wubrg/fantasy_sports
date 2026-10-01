package main

import "testing"

// TestHedgeRefusesPushForfeitBook pins the wiring this file exists for:
// wager.CheckBonusMarket, BonusLostOnPush and BonusSplittable were built and
// tested in internal/wager, but no non-test caller reached them anywhere in
// the tree -- a FanDuel bonus bet on a market that can push would silently
// hedge as if the push-forfeit rule didn't exist, right up until it forfeited
// the whole bonus instead of costing a reduced conversion.
func TestHedgeRefusesPushForfeitBook(t *testing.T) {
	err := hedgeCmd([]string{
		"-face", "50", "-back", "150", "-against", "-170",
		"-book", "fanduel", "-can-push",
	})
	if err == nil {
		t.Fatal("hedge on a pushable market at FanDuel should be refused, forfeits the bonus on a push")
	}
}

// TestHedgeAllowsPushForfeitBookOnNonPushingMarket is the other half: the
// same book is fine once the market itself cannot push (a half-point line or
// a player prop), since BonusLostOnPush only matters when a push is possible.
func TestHedgeAllowsPushForfeitBookOnNonPushingMarket(t *testing.T) {
	if err := hedgeCmd([]string{
		"-face", "50", "-back", "150", "-against", "-170",
		"-book", "fanduel",
	}); err != nil {
		t.Fatalf("hedge on a non-pushable market at FanDuel should be allowed: %v", err)
	}
}

// TestHedgeAllowsPushForfeitMarketAtOtherBooks confirms a book that returns
// the bonus on a push (anything but FanDuel) is unaffected by -can-push.
func TestHedgeAllowsPushForfeitMarketAtOtherBooks(t *testing.T) {
	if err := hedgeCmd([]string{
		"-face", "50", "-back", "150", "-against", "-170",
		"-book", "draftkings", "-can-push",
	}); err != nil {
		t.Fatalf("hedge on a pushable market at DraftKings should be allowed: %v", err)
	}
}

// TestHedgeRequiresBook pins the refusal that makes the push-forfeit check
// possible at all: without knowing which book holds the bonus bet, there is
// nothing to check it against.
func TestHedgeRequiresBook(t *testing.T) {
	err := hedgeCmd([]string{"-face", "50", "-back", "150", "-against", "-170"})
	if err == nil {
		t.Fatal("hedge without -book should be refused")
	}
}

// TestHedgeRefusesUnknownBook mirrors wager.CheckBonusMarket's own guard: a
// book whose bonus-bet rules aren't recorded cannot be cleared as safe.
func TestHedgeRefusesUnknownBook(t *testing.T) {
	err := hedgeCmd([]string{
		"-face", "50", "-back", "150", "-against", "-170",
		"-book", "notarealbook", "-can-push",
	})
	if err == nil {
		t.Fatal("hedge at an unrecorded book should be refused")
	}
}

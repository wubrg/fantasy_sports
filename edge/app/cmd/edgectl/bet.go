package main

import (
	"flag"
	"fmt"
	"time"

	"edge/internal/betlog"
	"edge/internal/journal"
	"edge/internal/ledger"
	"edge/internal/wager"
)

// betCmd is the blessed way to place or settle a wager: one call writes the
// betlog and the ledger together via internal/journal. Raw `ledger add -kind
// place` remains as a low-level escape hatch that writes only the ledger.
func betCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("edgectl bet: want a mode: place or settle")
	}
	switch args[0] {
	case "place":
		return betPlace(args[1:])
	case "settle":
		return betSettle(args[1:])
	default:
		return fmt.Errorf("edgectl bet: unknown mode %q (want place or settle)", args[0])
	}
}

func betPlace(args []string) error {
	fs := flag.NewFlagSet("bet place", flag.ContinueOnError)
	betlogPath := fs.String("betlog", defaultBetlog(), "path to the betlog")
	ledgerPath := fs.String("ledger", defaultLedgerPath(), "path to the bankroll")
	selection := fs.String("selection", "", "what the wager is (required)")
	price := fs.Int("price", 0, "American price of the wager (required)")
	stake := fs.Float64("stake", 0, "stake (required)")
	book := fs.String("book", "", "sportsbook the bet is placed at")
	bankroll := fs.String("bankroll", "bonus", "bankroll: cash, bonus or no-sweat")
	boost := fs.String("boost", "", "boost lot id to apply and consume")
	week := fs.Int("week", 0, "the NFL week this wager is FOR")
	narrative := fs.String("narrative", "", "free-text note")
	predicted := fs.Float64("predicted", 0, "your win-probability belief in [0,1]; drives the log's EV")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bank := "bonus bet"
	switch *bankroll {
	case "cash":
		bank = "real money"
	case "no-sweat", "nosweat", "no sweat":
		bank = "no-sweat"
	}
	id, err := journal.Place(*betlogPath, *ledgerPath, journal.PlaceRequest{
		Bet: betlog.Bet{
			Selection: *selection, Price: wager.American(*price), Bankroll: bank,
			Stake: *stake, Week: *week, Narrative: *narrative, Predicted: *predicted,
		},
		Book: *book, BoostLotID: *boost,
	}, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("placed %s\n", id)
	return nil
}

func betSettle(args []string) error {
	fs := flag.NewFlagSet("bet settle", flag.ContinueOnError)
	betlogPath := fs.String("betlog", defaultBetlog(), "path to the betlog")
	ledgerPath := fs.String("ledger", defaultLedgerPath(), "path to the bankroll")
	id := fs.String("id", "", "wager id to settle (required)")
	result := fs.String("result", "", "won, lost, push or void (required)")
	returns := fs.Float64("returns", 0, "amount handed back by the book")
	returnsAsset := fs.String("returns-asset", "cash", "asset the returns arrive as")
	book := fs.String("book", "", "book the returns land at (required with -returns)")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ret *ledger.Lot
	if *returns > 0 {
		if *book == "" {
			return fmt.Errorf("-book is required with -returns")
		}
		ret = &ledger.Lot{Book: *book, Asset: *returnsAsset, Amount: *returns}
	}
	if err := journal.Settle(*betlogPath, *ledgerPath, *id, betlog.Result(*result), ret, *note, time.Now()); err != nil {
		return err
	}
	fmt.Printf("settled %s %s\n", *id, *result)
	return nil
}

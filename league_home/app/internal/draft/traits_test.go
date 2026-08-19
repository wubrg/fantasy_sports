package draft

import (
	"fmt"
	"math"
	"os"
	"testing"
)

// catcher builds a pass catcher from the pieces a projection is made of.
func catcher(id, pos string, rec, recYds, recTD, targets float64) TraitInput {
	c := Components{
		HasParts: true, Receptions: rec, RecvYards: recYds,
		RecvTD: recTD, Targets: targets,
	}
	return TraitInput{PlayerID: id, Position: pos, Parts: c, Points: c.Total(),
		PriorGames: 17, PriorPoints: c.Total()}
}

func back(id string, rushYds, rushTD, rec, recYds, targets float64) TraitInput {
	c := Components{
		HasParts: true, RushYards: rushYds, RushTD: rushTD,
		Receptions: rec, RecvYards: recYds, Targets: targets,
	}
	return TraitInput{PlayerID: id, Position: "RB", Parts: c, Points: c.Total(),
		PriorGames: 17, PriorPoints: c.Total()}
}

func traitShapePool() PoolState {
	s := HitOrMissPool()
	s.Teams = 1
	return s
}

// TestComponentsScoringIsTheLeagueRulebook restates the league's scoring
// once, by hand, so a change to Total() has to be a deliberate one.
//
// This says nothing about the real projections — see
// TestComponentsRebuildTheRealProjections for that.
func TestComponentsScoringIsTheLeagueRulebook(t *testing.T) {
	c := Components{
		HasParts: true, PassYards: 4000, PassTD: 30, Interceptions: 10,
		RushYards: 400, RushTD: 4, Receptions: 0,
	}
	want := 4000.0/25 + 30*4 + 400.0/10 + 4*6 - 10
	if got := c.Total(); got != want {
		t.Errorf("total = %v, want %v", got, want)
	}
	if got := c.TouchdownPoints(); got != 30*4+4*6 {
		t.Errorf("td points = %v", got)
	}
}

// TestComponentsRebuildTheRealProjections measures the decomposition against
// the file it claims to decompose.
//
// Every trait is a statement about the parts — what share of a player's
// points come from catches, from touchdowns, from yardage. If the parts do
// not add back up to the total the source published, then the traits are
// describing a player the projection never was, and nothing downstream can
// notice: a shape made of player types would keep sorting rosters
// confidently on a scoring rule that had quietly diverged.
//
// The signed mean is the assertion that matters. Both columns are rounded,
// so individual rows are always a little off and the max error alone cannot
// tell rounding from a real mismatch. A systematic difference — a reception
// worth one point instead of half, an interception scored the wrong way —
// pushes the errors to one side and shows up in the mean immediately, while
// rounding cancels out to roughly nothing.
func TestComponentsRebuildTheRealProjections(t *testing.T) {
	root, err := ResolveDataRoot("")
	if err != nil {
		// The projections are subscriber content and live in a private
		// repo; most machines running this suite will not have them.
		t.Skipf("no private data root: %v", err)
	}
	path := root.Normalized("ciely-2026.csv")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no ciely projections at %s", path)
	}
	rows, err := LoadSourceCSV(path, SourceSchema{})
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}

	var n int
	var maxErr, sumAbs, sumSigned float64
	worst := ""
	for _, r := range rows {
		if !r.Components.HasParts {
			continue
		}
		n++
		d := r.Components.Total() - r.Points
		sumSigned += d
		sumAbs += math.Abs(d)
		if math.Abs(d) > maxErr {
			maxErr, worst = math.Abs(d), r.Player
		}
	}
	if n < 400 {
		t.Fatalf("only %d rows had populated components in %s; the decomposition "+
			"looks like it stopped being populated, not that the file shrank", n, path)
	}
	meanAbs, meanSigned := sumAbs/float64(n), sumSigned/float64(n)

	if maxErr >= 0.10 {
		t.Errorf("worst reconstruction is %.4f off (%s) over %d rows; mean abs %.4f, mean signed %.2e",
			maxErr, worst, n, meanAbs, meanSigned)
	}
	if math.Abs(meanSigned) >= 0.01 {
		t.Errorf("mean signed error %.4f over %d rows: the errors lean one way, so this is a scoring mismatch and not rounding (max %.4f on %s, mean abs %.4f)",
			meanSigned, n, maxErr, worst, meanAbs)
	}
	t.Logf("%d rows: max %.4f, mean abs %.4f, mean signed %.2e", n, maxErr, meanAbs, meanSigned)
}

// TestFloorAndRedZoneAreOppositeEnds — one axis, so a player cannot be both,
// and every position should produce some of each.
func TestFloorAndRedZoneAreOppositeEnds(t *testing.T) {
	var in []TraitInput
	// Receivers on a ramp from all-catches to all-touchdowns.
	for i := 0; i < 12; i++ {
		td := float64(i)
		rec := float64(90 - i*6)
		in = append(in, catcher(string(rune('a'+i)), "WR", rec, rec*12, td, rec*1.5))
	}
	got := ClassifyTraits(in, traitShapePool())

	both := 0
	floor, red := 0, 0
	for _, set := range got {
		if set.Has(TraitFloor) {
			floor++
		}
		if set.Has(TraitRedZone) {
			red++
		}
		if set.Has(TraitFloor) && set.Has(TraitRedZone) {
			both++
		}
	}
	if both != 0 {
		t.Errorf("%d players are both a floor and a red-zone play", both)
	}
	if floor == 0 || red == 0 {
		t.Errorf("expected some of each, got %d floor and %d red zone", floor, red)
	}
}

// TestQuarterbacksAreNotAllFloor is the regression.
//
// Quarterbacks have no receptions, so requiring reception share of them
// compared zero against zero and every quarterback qualified.
func TestQuarterbacksAreNotAllFloor(t *testing.T) {
	var in []TraitInput
	for i := 0; i < 12; i++ {
		c := Components{
			HasParts: true, PassYards: 4200, PassTD: float64(18 + i*2),
			RushYards: float64(400 - i*30),
		}
		in = append(in, TraitInput{
			PlayerID: string(rune('a' + i)), Position: "QB", Parts: c,
			Points: c.Total(), PriorGames: 17, PriorPoints: c.Total(),
		})
	}
	got := ClassifyTraits(in, traitShapePool())

	floor := 0
	for _, set := range got {
		if set.Has(TraitFloor) {
			floor++
		}
	}
	if floor == 0 {
		t.Error("no quarterback reads as a floor play; the trait should still apply")
	}
	if floor > len(in)/2 {
		t.Errorf("%d of %d quarterbacks read as floor plays", floor, len(in))
	}
}

// TestUnprovenIsMeasuredPerGame covers both ways a projection can lack
// support: no real sample, and a rate past the one he has posted.
func TestUnprovenIsMeasuredPerGame(t *testing.T) {
	// Four games is not a season's evidence however good it was.
	thin := TraitInput{Points: 170, PriorGames: 4, PriorPoints: 40}
	if !thin.unproven() {
		t.Error("four games is not a solid history")
	}
	// But with a real sample, rate is compared to rate: a player who missed
	// six games and matched his rate is proven, though his total collapsed.
	hurt := TraitInput{Points: 170, PriorGames: 11, PriorPoints: 110} // 10.0 vs 10.0 ppg
	if hurt.unproven() {
		t.Error("matching his rate over eleven games is proven, whatever the total says")
	}

	leap := TraitInput{Points: 170, PriorGames: 16, PriorPoints: 96} // 10.0 vs 6.0 ppg
	if !leap.unproven() {
		t.Error("a projection 1.7x his own per-game rate is unproven")
	}
	steady := TraitInput{Points: 170, PriorGames: 16, PriorPoints: 160} // 10.0 vs 10.0
	if steady.unproven() {
		t.Error("a projection matching his rate is proven")
	}
	rookie := TraitInput{Points: 170, PriorGames: 0}
	if !rookie.unproven() {
		t.Error("no prior season means nothing to check the projection against")
	}
}

// TestThreeDownNeedsRealVolumeOnBothSides — a back with carries alone is a
// committee grinder and one with targets alone is a passing-down specialist.
//
// The specialist case is the one that changed. The rule used to ask only that
// a back rush for something at all, so a receiving back cleared it on a token
// carry and read as an every-down back; the test recorded that as a t.Log
// rather than a failure. Naming the trait "3-down" made the gap between the
// name and the rule impossible to keep.
func TestThreeDownNeedsRealVolumeOnBothSides(t *testing.T) {
	in := []TraitInput{
		back("every-down", 1100, 9, 55, 420, 70),
		back("grinder", 900, 8, 8, 60, 12),
		back("third-down", 120, 1, 60, 480, 80),
	}
	// Pad so the window has something to rank.
	for i := 0; i < 10; i++ {
		in = append(in, back("filler"+string(rune('a'+i)), 400, 3, 20, 150, 25))
	}
	got := ClassifyTraits(in, traitShapePool())

	if !got["every-down"].Has(TraitThreeDown) {
		t.Error("a back with real volume in both jobs is a three-down back")
	}
	if got["grinder"].Has(TraitThreeDown) {
		t.Error("carries without receiving work is not a three-down back")
	}
	if got["third-down"].Has(TraitThreeDown) {
		t.Error("a receiving back with 120 rushing yards is a passing-down specialist, not a three-down back")
	}
}

// TestOnlyContendedPlayersAreLabelled — a trait on somebody nobody will
// roster is a label with nothing behind it.
func TestOnlyContendedPlayersAreLabelled(t *testing.T) {
	var in []TraitInput
	for i := 0; i < 80; i++ {
		rec := float64(90 - i)
		in = append(in, catcher(string(rune('a'+i%26))+string(rune('a'+i/26)), "WR",
			rec, rec*12, 5, rec*1.5))
	}
	got := ClassifyTraits(in, traitShapePool())

	labelled := 0
	for _, set := range got {
		if len(set) > 0 {
			labelled++
		}
	}
	if labelled == 0 {
		t.Fatal("nobody was labelled")
	}
	if labelled == len(in) {
		t.Error("everybody was labelled, including players nobody will roster")
	}
}

// TestTraitCountsReadTheLineupOnly — a bench stacked with one kind of player
// says nothing about how a season goes, so the composition shown against a
// roster has to come from the starters.
func TestTraitCountsReadTheLineupOnly(t *testing.T) {
	var r Roster
	for i := 0; i < 4; i++ {
		r.Players = append(r.Players, RosterSpot{
			Player: PlayerSignals{Position: "WR", Traits: TraitSet{TraitRedZone}},
		})
	}
	if n := TraitCounts(r)[TraitRedZone]; n != 0 {
		t.Errorf("counted %d bench players into the lineup's composition", n)
	}
	r.Players[0].Starting = true
	r.Players[1].Starting = true
	if n := TraitCounts(r)[TraitRedZone]; n != 2 {
		t.Errorf("counted %d, want the 2 who start", n)
	}
	if _, ok := TraitCounts(r)[TraitFloor]; ok {
		t.Error("a trait nobody carries should be absent, not zero")
	}
}

// discountBoard is a board with n healthy players at a known adjusted edge, so
// a percentile taken over them is predictable.
func discountBoard(healthy int, extra ...PlayerSignals) []PlayerSignals {
	out := make([]PlayerSignals, 0, healthy+len(extra))
	for i := 0; i < healthy; i++ {
		// Edge zero: cost equals value, so the positional bias is zero too and
		// the percentile over them is zero.
		out = append(out, PlayerSignals{
			PlayerID: fmt.Sprint("h", i), Name: fmt.Sprint("Healthy ", i),
			Position: "WR", Cost: 20, Value: 20,
		})
	}
	return append(out, extra...)
}

// TestDiscountNeedsTheMarketToAgree is the whole point of the change.
//
// The trait used to fire on any injury designation, which made a claim about a
// price without looking at one. A Questionable tag the market has not moved on
// is not a discount, and 47 of the 53 players carrying the old trait were
// exactly that.
func TestDiscountNeedsTheMarketToAgree(t *testing.T) {
	cheap := PlayerSignals{PlayerID: "cheap", Name: "Marked Down", Position: "WR",
		Cost: 6, Value: 20, Availability: "Questionable"}
	priced := PlayerSignals{PlayerID: "priced", Name: "Still Priced", Position: "WR",
		Cost: 20, Value: 20, Availability: "Questionable"}
	board := discountBoard(30, cheap, priced)

	markDiscounted(board)

	if !board[30].Traits.Has(TraitDiscounted) {
		t.Error("a designated player the market marked down is not flagged")
	}
	if board[31].Traits.Has(TraitDiscounted) {
		t.Error("a designated player at full price is flagged as discounted")
	}
}

// TestDiscountNeedsADesignation — cheap alone is a bargain, which the Edge
// column already says. The trait is for a bargain with a reason attached.
func TestDiscountNeedsADesignation(t *testing.T) {
	bargain := PlayerSignals{PlayerID: "b", Name: "Just Cheap", Position: "WR",
		Cost: 2, Value: 30}
	board := discountBoard(30, bargain)
	markDiscounted(board)
	if board[30].Traits.Has(TraitDiscounted) {
		t.Error("a healthy bargain is flagged as an injury discount")
	}
}

// TestDiscountIgnoresTheDollarFloor — hundreds of players sit at a dollar on
// both boards. Their ratios are noise and they must not be flagged, nor drag
// the percentile.
func TestDiscountIgnoresTheDollarFloor(t *testing.T) {
	floor := PlayerSignals{PlayerID: "f", Name: "Waiver Fodder", Position: "WR",
		Cost: 1, Value: 1, Availability: "IR"}
	board := discountBoard(30, floor)
	markDiscounted(board)
	if board[30].Traits.Has(TraitDiscounted) {
		t.Error("a dollar against a dollar was read as a discount")
	}
}

// TestDiscountHoldsTheBarOnAFlatBoard — a percentile always has a top, so
// without a floor the trait would flag somebody however little the market had
// actually moved.
func TestDiscountHoldsTheBarOnAFlatBoard(t *testing.T) {
	// Barely cheaper than everyone else, and well under discountBar.
	nearly := PlayerSignals{PlayerID: "n", Name: "Barely Cheaper", Position: "WR",
		Cost: 18, Value: 20, Availability: "Questionable"}
	board := discountBoard(30, nearly)
	markDiscounted(board)
	if board[30].Traits.Has(TraitDiscounted) {
		t.Errorf("flagged a $%d edge on a flat board; the bar is $%d",
			nearly.Value-nearly.Cost, discountBar)
	}
}

// TestDiscountFallsBackLateInADraft — once the room has bought almost
// everyone there are too few healthy players left to rank, and a percentile of
// a handful is not a threshold. The bar carries it instead.
func TestDiscountFallsBackLateInADraft(t *testing.T) {
	cheap := PlayerSignals{PlayerID: "c", Name: "Late Bargain", Position: "WR",
		Cost: 4, Value: 20, Availability: "PUP"}
	board := discountBoard(3, cheap) // far below discountSample
	markDiscounted(board)
	if !board[3].Traits.Has(TraitDiscounted) {
		t.Error("a clear discount was missed because the healthy sample was small")
	}
}

// TestDiscountDoesNotLeakThroughTheSharedTraitSet is the trap.
//
// BuildSignals hands every player the same slice out of the traits map, so
// appending in place writes the trait onto everyone who shares that set. The
// offense trait already copies for this reason.
func TestDiscountDoesNotLeakThroughTheSharedTraitSet(t *testing.T) {
	shared := TraitSet{TraitFloor}
	cheap := PlayerSignals{PlayerID: "c", Name: "Marked Down", Position: "WR",
		Cost: 5, Value: 25, Availability: "Questionable", Traits: shared}
	other := PlayerSignals{PlayerID: "o", Name: "Someone Else", Position: "WR",
		Cost: 20, Value: 20, Traits: shared}
	board := discountBoard(30, cheap, other)

	markDiscounted(board)

	if !board[30].Traits.Has(TraitDiscounted) {
		t.Fatal("the discounted player was not flagged")
	}
	if board[31].Traits.Has(TraitDiscounted) {
		t.Error("the trait leaked onto another player through the shared set")
	}
	if shared.Has(TraitDiscounted) {
		t.Error("the trait was written into the shared set itself")
	}
}

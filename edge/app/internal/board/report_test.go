package board

import (
	"fmt"
	"math"
	"testing"

	"edge/internal/wager"
)

// The numbers in this file come from a real board read on 8/17 and were
// computed independently of this package. They are the contract: if the code
// disagrees with them, the code is what changes.
//
// Two of the supplied values could not be reproduced and are documented at the
// assertion that replaces them, with the arithmetic that shows why. Neither
// was adjusted to whatever the code happened to print.

const tol = 0.001

// contractBoard is the 8/17 board: away price, home price, one game per row,
// in kickoff order.
var contractBoard = []struct {
	Away, Home string
	ML         string
}{
	{"ARI", "LAC", "+390/-525"},
	{"NO", "DET", "+265/-340"},
	{"CLE", "JAX", "+290/-375"},
	{"IND", "BAL", "+145/-180"},
	{"NE", "SEA", "+175/-205"},
	{"SF", "LAR", "+150/-180"},
	{"MIA", "LV", "+175/-205"},
	{"WAS", "PHI", "+190/-235"},
	{"TB", "CIN", "+155/-190"},
	{"ATL", "PIT", "+135/-165"},
	{"NYG", "DAL", "+135/-165"},
	{"CAR", "CHI", "+130/-155"},
	{"DEN", "KC", "+125/-150"},
	{"NYJ", "TEN", "+125/-150"},
	{"GB", "MIN", "+105/-125"},
}

// contractDoc builds a one-week Doc with the board loaded into one book.
// Kickoffs are staggered so GameIDs() has a defined order.
func contractDoc(book string) *Doc {
	d := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	for i, row := range contractBoard {
		id := fmt.Sprintf("2026_01_%s_%s", row.Away, row.Home)
		d.Games[id] = &Game{
			Away:    row.Away,
			Home:    row.Home,
			Kickoff: fmt.Sprintf("2026-09-13T%02d:00", 10+i),
			Books: map[string]Lines{
				book: {ML: row.ML},
			},
		}
	}
	return d
}

// line returns the de-vigged line for the game the named away team plays in.
func line(t *testing.T, d *Doc, book, away string) GameLine {
	t.Helper()
	for id, g := range d.Games {
		if g.Away != away {
			continue
		}
		l, ok, err := Devig(id, g, book)
		if err != nil {
			t.Fatalf("Devig(%s): %v", id, err)
		}
		if !ok {
			t.Fatalf("Devig(%s): no price", id)
		}
		return l
	}
	t.Fatalf("no game with away team %s", away)
	return GameLine{}
}

func dog(t *testing.T, d *Doc, book, away string) Side {
	t.Helper()
	return line(t, d, book, away).Dog()
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.4f (tolerance %v)", what, got, want, tol)
	}
}

// TestDevigContract checks the per-game arithmetic against hand-verified
// values: the overround, the de-vigged dog probability, and what a bonus bet
// on that dog actually converts.
func TestDevigContract(t *testing.T) {
	d := contractDoc(Consensus)
	tests := []struct {
		away      string
		overround float64
		fair      float64
		conv      float64
		belowLine bool // fails the 70% bonus-bet floor
	}{
		{away: "ARI", overround: 0.0441, fair: 0.1955, conv: 0.7625},
		// NO is the case that motivates ranking by conversion instead of by
		// price: +265 looks like a comfortable dog and converts at 69.4%,
		// under the floor, because -340 on the other side is heavily juiced.
		{away: "NO", overround: 0.0467, fair: 0.2617, conv: 0.6935, belowLine: true},
		{away: "CLE", overround: 0.0459, fair: 0.2452, conv: 0.7110},
	}
	for _, tc := range tests {
		t.Run(tc.away, func(t *testing.T) {
			l := line(t, d, Consensus, tc.away)
			dg := l.Dog()
			if dg.Team != tc.away {
				t.Fatalf("dog = %s, want %s", dg.Team, tc.away)
			}
			near(t, "overround", l.Overround, tc.overround)
			near(t, "fair", dg.Fair, tc.fair)
			near(t, "conversion", dg.Conversion, tc.conv)
			if l.Suspect {
				t.Errorf("%s flagged suspect at %.4f overround", tc.away, l.Overround)
			}
			if got := !dg.Clears(DefaultTarget); got != tc.belowLine {
				t.Errorf("below floor = %v, want %v (conversion %.4f)", got, tc.belowLine, dg.Conversion)
			}
		})
	}

	// The floor itself comes from the bonus card and is stated here so a
	// change to it cannot pass silently.
	floor, err := wager.MinPriceForConversion(DefaultTarget)
	if err != nil {
		t.Fatal(err)
	}
	if floor != 234 {
		t.Errorf("70%% floor = %+d, want +234", floor)
	}
}

// TestSuspectOverround covers the transcription error this flag exists for.
func TestSuspectOverround(t *testing.T) {
	tests := []struct {
		name        string
		ml          string
		wantSuspect bool
	}{
		// The real board once read SF +150 / LAR -150: a 0.00% overround, the
		// book working for free. The true home price was -180.
		{name: "mirrored dog price", ml: "+150/-150", wantSuspect: true},
		{name: "true price", ml: "+150/-180", wantSuspect: false},
		{name: "ordinary market", ml: "-110/-110", wantSuspect: false},
		// An extra digit on the favourite.
		{name: "extra digit", ml: "+390/-5250", wantSuspect: true},
		// A lost minus sign puts both sides at plus money, which sums to less
		// than 1 -- a market that pays out more than it takes in.
		{name: "lost sign", ml: "+390/525", wantSuspect: true},
		// Just inside the bounds: unusual, not impossible, so not flagged.
		{name: "thin but real", ml: "-102/-102", wantSuspect: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := &Game{Away: "SF", Home: "LAR", Books: map[string]Lines{Consensus: {ML: tc.ml}}}
			l, ok, err := Devig("2026_01_SF_LAR", g, Consensus)
			if err != nil || !ok {
				t.Fatalf("Devig: ok=%v err=%v", ok, err)
			}
			if l.Suspect != tc.wantSuspect {
				t.Fatalf("suspect = %v (overround %.4f), want %v", l.Suspect, l.Overround, tc.wantSuspect)
			}
			if l.Suspect && l.Why == "" {
				t.Error("suspect line carries no explanation")
			}
		})
	}
}

// TestParlayContract checks two-leg pricing and, more importantly, that the
// true probability is the product of the DE-VIGGED legs rather than a de-vig
// of the parlay price.
func TestParlayContract(t *testing.T) {
	d := contractDoc(Consensus)

	t.Run("IND+NE", func(t *testing.T) {
		p, err := MakeParlay(dog(t, d, Consensus, "IND"), dog(t, d, Consensus, "NE"))
		if err != nil {
			t.Fatal(err)
		}
		near(t, "decimal", p.Decimal, 6.7375) // 2.45 x 2.75
		// +573.75 floored. This is one of the two prices actually quoted by
		// Fanatics (the other is SF+MIA at +587), and both confirm the book
		// floors rather than rounding to nearest. The earlier contract's
		// +474 and +429 were computed with round-to-nearest and never
		// observed, so they were never evidence. Conversion uses the exact
		// unrounded decimal either way, so this never moves the money.
		if p.Price != 573 {
			t.Errorf("price = %+d, want +573 (exact +573.75, floored; observed at Fanatics)", p.Price)
		}
		near(t, "true prob", p.TrueProb, 0.1365)
		near(t, "conversion", p.Conversion, 0.782)
	})

	t.Run("SF+MIA", func(t *testing.T) {
		p, err := MakeParlay(dog(t, d, Consensus, "SF"), dog(t, d, Consensus, "MIA"))
		if err != nil {
			t.Fatal(err)
		}
		near(t, "decimal", p.Decimal, 6.875) // 2.50 x 2.75
		if p.Price != 587 {
			t.Errorf("price = %+d, want +587", p.Price)
		}
		// The contract gives 0.3836 x 0.3488 = 0.1338 and a conversion of
		// 0.785. SF's 0.3836 is right, but MIA's 0.3488 is not: +175/-205
		// de-vigs to 0.363636/1.035768 = 0.351079. 0.3488 is 0.363636 divided
		// by 1.042857 -- the overround of the SF/LAR game, applied to the
		// wrong market. The same slip produces the contract's 0.3914 for IND.
		// Correct legs give 0.383562 x 0.351079 = 0.134656 and 0.134656 x
		// 5.875 = 0.791104.
		near(t, "true prob", p.TrueProb, 0.134656)
		near(t, "conversion", p.Conversion, 0.791104)
	})

	// A parlay must never be priced by de-vigging its own price: that leaves
	// every leg's vig compounded inside the answer. The gap is worth about a
	// point of conversion and always in the flattering direction.
	t.Run("not a de-vig of the parlay price", func(t *testing.T) {
		p, err := MakeParlay(dog(t, d, Consensus, "IND"), dog(t, d, Consensus, "NE"))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := p.Price.ImpliedRaw()
		if err != nil {
			t.Fatal(err)
		}
		if raw <= p.TrueProb {
			t.Fatalf("raw implied %.4f should exceed the de-vigged %.4f", raw, p.TrueProb)
		}
		if naive := raw * (p.Decimal - 1); math.Abs(naive-p.Conversion) < 0.005 {
			t.Errorf("naive conversion %.4f is indistinguishable from %.4f; "+
				"the test cannot tell the two methods apart", naive, p.Conversion)
		}
	})
}

// TestParlayPriceRounding pins the rounding rule: FLOOR, always toward the
// price worse for the bettor. Confirmed against the two parlay prices actually
// quoted by Fanatics -- IND+NE at +573 (exact +573.75) and SF+MIA at +587
// (exact +587.50). Round-to-nearest reproduces neither.
func TestParlayPriceRounding(t *testing.T) {
	tests := []struct {
		dec  float64
		want wager.American
	}{
		{dec: 6.7375, want: 573},  // +573.75 -> floor; OBSERVED at Fanatics
		{dec: 6.875, want: 587},   // +587.50 -> floor; OBSERVED at Fanatics
		{dec: 5.945, want: 494},   // +494.50 -> floor
		{dec: 5.7375, want: 473},  // +473.75 -> floor
		{dec: 5.405, want: 440},   // +440.50 -> floor
		{dec: 5.2875, want: 428},  // +428.75 -> floor
		{dec: 5.5, want: 450},     // exact
		{dec: 1.44, want: -228},   // -227.27 -> floor is MORE negative: worse for the bettor
		{dec: 2.0, want: 100},     // the boundary between the two formulas
		{dec: 1.0000001, want: 0}, // no representable price; must error
	}
	for _, tc := range tests {
		got, err := parlayPrice(tc.dec)
		if tc.want == 0 {
			if err == nil {
				t.Errorf("parlayPrice(%v) = %+d, want an error", tc.dec, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parlayPrice(%v): %v", tc.dec, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parlayPrice(%v) = %+d, want %+d", tc.dec, got, tc.want)
		}
	}
}

// TestDisjointSetContract is the whole point of the command: four tickets, no
// team twice, chosen to maximise total conversion rather than picked greedily.
func TestDisjointSetContract(t *testing.T) {
	d := contractDoc(Consensus)
	pool := []Side{}
	for _, team := range []string{"WAS", "TB", "ATL", "NYG", "CAR", "DEN", "NYJ", "GB"} {
		pool = append(pool, dog(t, d, Consensus, team))
	}

	set, err := BuildSet(pool, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Parlays) != 4 {
		t.Fatalf("built %d parlays, want 4", len(set.Parlays))
	}

	want := []struct {
		a, b  string
		price wager.American
		conv  float64
	}{
		{a: "WAS", b: "GB", price: 494, conv: 0.762},
		{a: "TB", b: "DEN", price: 473, conv: 0.755},
		{a: "ATL", b: "CAR", price: 440, conv: 0.746},
		{a: "NYG", b: "NYJ", price: 428, conv: 0.741},
	}
	for i, w := range want {
		got := set.Parlays[i]
		teams := got.Teams()
		if teams[0] != w.a || teams[1] != w.b {
			t.Errorf("parlay %d = %v, want [%s %s]", i, teams, w.a, w.b)
			continue
		}
		if got.Price != w.price {
			t.Errorf("%v price = %+d, want %+d", teams, got.Price, w.price)
		}
		near(t, fmt.Sprintf("%v conversion", teams), got.Conversion, w.conv)
	}
	near(t, "average conversion", set.AvgConversion, 0.751)
	near(t, "P(at least one hits)", set.AnyHit, 0.511)

	// Each ticket clears the 70% floor that none of its legs clears alone --
	// GB converts 0.491 by itself. Pairing dogs is what buys the floor back.
	for _, p := range set.Parlays {
		if p.Conversion < DefaultTarget {
			t.Errorf("%v converts %.3f, below the floor", p.Teams(), p.Conversion)
		}
		for _, l := range p.Legs {
			if l.Conversion >= DefaultTarget {
				t.Errorf("%s already clears the floor alone (%.3f); "+
					"the fixture no longer tests what it claims to", l.Team, l.Conversion)
			}
		}
	}
}

// TestSetIsAlwaysDisjoint is the bookkeeping guarantee, checked over every
// shot count rather than the one the contract names. A repeated team is the
// specific failure that hand-building the set produced.
func TestSetIsAlwaysDisjoint(t *testing.T) {
	d := contractDoc(Consensus)
	var pool []Side
	for _, row := range contractBoard {
		pool = append(pool, dog(t, d, Consensus, row.Away))
	}

	for shots := 0; shots <= 12; shots++ {
		set, err := BuildSet(pool, shots)
		if err != nil {
			t.Fatalf("shots=%d: %v", shots, err)
		}
		// 15 dogs support 7 full pairs; the 8th shot cannot be filled.
		wantN := shots
		if wantN > len(pool)/2 {
			wantN = len(pool) / 2
		}
		if len(set.Parlays) != wantN {
			t.Errorf("shots=%d built %d parlays, want %d", shots, len(set.Parlays), wantN)
		}
		if set.Unfilled != shots-wantN {
			t.Errorf("shots=%d unfilled = %d, want %d", shots, set.Unfilled, shots-wantN)
		}

		seenTeam := map[string]bool{}
		seenGame := map[string]bool{}
		for _, p := range set.Parlays {
			if len(p.Legs) != 2 {
				t.Fatalf("shots=%d: %d legs, want 2", shots, len(p.Legs))
			}
			for _, l := range p.Legs {
				if seenTeam[l.Team] {
					t.Fatalf("shots=%d: %s appears twice across the set", shots, l.Team)
				}
				if seenGame[l.GameID] {
					t.Fatalf("shots=%d: %s used twice across the set", shots, l.GameID)
				}
				seenTeam[l.Team], seenGame[l.GameID] = true, true
			}
		}
	}
}

// TestSameGameLegsRejected covers the correlated case directly: both sides of
// one game multiply to a probability that cannot happen.
func TestSameGameLegsRejected(t *testing.T) {
	d := contractDoc(Consensus)
	l := line(t, d, Consensus, "ARI")
	if _, err := MakeParlay(l.Away, l.Home); err == nil {
		t.Fatal("MakeParlay accepted both sides of one game")
	}
	if _, err := MakeParlay(l.Away, l.Away); err == nil {
		t.Fatal("MakeParlay accepted the same team twice")
	}

	// The pool-level guard: two entries for one game must not become a ticket.
	set, err := BuildSet([]Side{l.Away, l.Home}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Parlays) != 0 {
		t.Fatalf("built %d parlays from one game's two sides, want 0", len(set.Parlays))
	}
	if set.Unfilled != 1 {
		t.Errorf("unfilled = %d, want 1", set.Unfilled)
	}
}

// TestEmptyBoard is the state of every book but consensus today. An empty cell
// must produce no line, no dog and no zero-priced leg -- never a wager at
// odds of 0, which is what a naive parse would hand back.
func TestEmptyBoard(t *testing.T) {
	d := contractDoc(Consensus)

	a, err := Analyze(d, Options{Book: "fanatics", Shots: 4})
	if err != nil {
		t.Fatalf("Analyze on an empty book errored: %v", err)
	}
	if a.Bettable() {
		t.Error("an unpriced book reports as bettable")
	}
	if len(a.Lines) != 0 || len(a.Dogs) != 0 || len(a.Parlays()) != 0 {
		t.Errorf("empty book produced %d lines, %d dogs, %d parlays",
			len(a.Lines), len(a.Dogs), len(a.Parlays()))
	}
	if len(a.Missing) != len(contractBoard) {
		t.Errorf("missing = %d games, want %d", len(a.Missing), len(contractBoard))
	}
	if len(a.Problems) != 0 {
		t.Errorf("empty cells reported as problems: %v", a.Problems)
	}

	// Nothing anywhere may carry a zero price: American(0) is not a price, and
	// it is what an empty cell becomes if it is parsed instead of skipped.
	for _, s := range a.Dogs {
		if s.Price == 0 {
			t.Errorf("%s has a zero price", s.Team)
		}
	}

	// A board with no games at all is still not an error.
	empty := &Doc{Season: 2026, Week: 1, Games: map[string]*Game{}}
	if _, err := Analyze(empty, Options{Book: Consensus, Shots: 4}); err != nil {
		t.Fatalf("Analyze on an empty board errored: %v", err)
	}
}

// TestAnalyze covers the whole-week path: ranking by conversion rather than by
// price, and holding a suspect game out of the parlay pool.
func TestAnalyze(t *testing.T) {
	d := contractDoc(Consensus)
	a, err := Analyze(d, Options{Book: Consensus, Shots: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Lines) != len(contractBoard) {
		t.Fatalf("priced %d games, want %d", len(a.Lines), len(contractBoard))
	}
	if a.Floor != 234 {
		t.Errorf("floor = %+d, want +234", a.Floor)
	}
	for i := 1; i < len(a.Dogs); i++ {
		if a.Dogs[i-1].Conversion < a.Dogs[i].Conversion {
			t.Fatalf("dogs are not ranked by conversion at %d", i)
		}
	}
	// Ranked by conversion, NO (+265) sits below CLE (+290) as expected, but
	// also below nothing else it outprices -- the ordering by price and by
	// conversion agree here. What must not happen is NO clearing the floor.
	if a.Dogs[0].Team != "ARI" {
		t.Errorf("best dog = %s, want ARI", a.Dogs[0].Team)
	}
	for _, s := range a.Dogs {
		if s.Team == "NO" && s.Clears(a.Target) {
			t.Errorf("NO converts %.4f and should fail the floor", s.Conversion)
		}
	}
	if len(a.Parlays()) != 4 {
		t.Errorf("built %d parlays, want 4", len(a.Parlays()))
	}

	// Break one game's price into an implausible market: it stays in Lines and
	// in Dogs, but must not reach the parlay pool.
	broken := contractDoc(Consensus)
	for _, g := range broken.Games {
		if g.Away == "ARI" {
			g.Books[Consensus] = Lines{ML: "+390/-390"}
		}
	}
	b, err := Analyze(broken, Options{Book: Consensus, Shots: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Suspect) != 1 || b.Suspect[0].Away.Team != "ARI" {
		t.Fatalf("suspect = %v, want the ARI game", b.Suspect)
	}
	for _, p := range b.Parlays() {
		for _, l := range p.Legs {
			if l.Team == "ARI" {
				t.Error("a suspect line was used as a parlay leg")
			}
		}
	}
}

// TestShop is the line-shopping path. Consensus is the benchmark and is never
// itself offered as a place to bet.
func TestShop(t *testing.T) {
	d := contractDoc(Consensus)
	// The real case: consensus had ARI +455 while Fanatics showed +390.
	for _, g := range d.Games {
		if g.Away != "ARI" {
			continue
		}
		g.Books[Consensus] = Lines{ML: "+455/-625"}
		g.Books["fanatics"] = Lines{ML: "+390/-525"}
		g.Books["draftkings"] = Lines{ML: "+420/-560"}
	}

	a, err := Analyze(d, Options{Book: "fanatics", Shots: 4})
	if err != nil {
		t.Fatal(err)
	}
	var row *ShopRow
	for i := range a.Shop {
		if a.Shop[i].Team == "ARI" {
			row = &a.Shop[i]
		}
	}
	if row == nil {
		t.Fatal("no shop row for ARI")
	}
	if row.Best.Book != "draftkings" || row.Best.Price != 420 {
		t.Errorf("best = %s %+d, want draftkings +420", row.Best.Book, row.Best.Price)
	}
	if !row.PointsValid || row.Points != -35 {
		t.Errorf("gap to consensus = %+d (valid %v), want -35", row.Points, row.PointsValid)
	}
	// Every other game has only a consensus price, so it can offer nothing to
	// shop: consensus is a reference column, not a book.
	for _, r := range a.Shop {
		if r.Best.Book == Consensus {
			t.Fatalf("%s offered consensus as a bettable price", r.Team)
		}
	}
	if len(a.Shop) != 2 { // ARI and LAC, the two sides of the one shopped game
		t.Errorf("shop rows = %d, want 2", len(a.Shop))
	}
}

package board

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

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

	set, err := BuildSet(pool, 4, MaxConversion, 0)
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
		set, err := BuildSet(pool, shots, MaxConversion, 0)
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
	set, err := BuildSet([]Side{l.Away, l.Home}, 1, MaxConversion, 0)
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

// TestObjectiveTradeoff pins the reason -objective exists.
//
// Maximising conversion drives the set towards the longest parlays available,
// which is the right answer for expected value per dollar and the wrong one
// for why a stake gets split in the first place. On the real Week 1 board that
// produced 82.9% conversion at a 31.9% hit rate -- worse, on the metric that
// motivated splitting, than the hand-built set it replaced.
func TestObjectiveTradeoff(t *testing.T) {
	d := contractDoc(Consensus)
	var pool []Side
	for _, team := range []string{"ARI", "CLE", "NO", "MIA", "WAS", "TB", "SF", "NE", "IND", "ATL", "NYJ", "NYG", "DEN", "CAR", "GB"} {
		pool = append(pool, dog(t, d, Consensus, team))
	}

	conv, err := BuildSet(pool, 4, MaxConversion, DefaultTarget)
	if err != nil {
		t.Fatal(err)
	}
	hit, err := BuildSet(pool, 4, MaxHitRate, DefaultTarget)
	if err != nil {
		t.Fatal(err)
	}

	// Each objective must win on its own metric, or the flag does nothing.
	if hit.AnyHit <= conv.AnyHit {
		t.Errorf("hitrate objective HitRate %.4f, want > conversion's %.4f", hit.AnyHit, conv.AnyHit)
	}
	if conv.AvgConversion <= hit.AvgConversion {
		t.Errorf("conversion objective AvgConversion %.4f, want > hitrate's %.4f",
			conv.AvgConversion, hit.AvgConversion)
	}
	// The trade is real in both directions: this is not a free lunch, and a
	// change that made one objective dominate the other would be a bug.
	if hit.AvgConversion >= conv.AvgConversion {
		t.Error("hitrate should give up conversion; it did not")
	}

	// Under MaxHitRate the objective pulls towards short prices, so without the
	// floor it would run down to near-even-money pairs. Every ticket must still
	// clear it.
	for _, p := range hit.Parlays {
		if p.Conversion < DefaultTarget {
			t.Errorf("%v converts %.4f, below the %.2f floor", p.Teams(), p.Conversion, DefaultTarget)
		}
	}

	// Disjointness is structural and must hold under either objective.
	for _, set := range []ParlaySet{conv, hit} {
		seen := map[string]bool{}
		for _, p := range set.Parlays {
			for _, tm := range p.Teams() {
				if seen[tm] {
					t.Errorf("team %s appears twice in a set", tm)
				}
				seen[tm] = true
			}
		}
	}
}

// TestCoverageAndProvisional pins the distinction that a partly-filled board is
// the normal state, not a broken one.
//
// The trap this guards: consensus is prefilled from the schedule and is
// therefore always complete, so a provisional flag keyed on anything but
// coverage would stay silent for every book that is actually entered by hand
// and fire only on the one that never needs it.
func TestCoverageAndProvisional(t *testing.T) {
	d := contractDoc(Consensus) // consensus filled for every game

	cov := d.Coverage()
	if cov[Consensus] != len(d.Games) {
		t.Fatalf("consensus coverage %d, want %d", cov[Consensus], len(d.Games))
	}
	if cov["fanatics"] != 0 {
		t.Fatalf("fanatics coverage %d, want 0", cov["fanatics"])
	}
	if bb := d.BettableBooks(); len(bb) != 0 {
		t.Fatalf("BettableBooks = %v, want none: consensus cannot be bet", bb)
	}

	full, err := Analyze(d, Options{Book: Consensus, Target: DefaultTarget, Shots: 4})
	if err != nil {
		t.Fatal(err)
	}
	if full.Provisional {
		t.Error("a fully priced book reported provisional")
	}

	// Price one game at one real book. That book is now partly covered, and
	// there is exactly one book worth shopping between -- which is to say,
	// none.
	id := d.GameIDs()[0]
	l := d.Games[id].Books["fanatics"]
	l.ML = "+390/-525"
	d.Games[id].Books["fanatics"] = l

	if got := d.Coverage()["fanatics"]; got != 1 {
		t.Fatalf("fanatics coverage %d, want 1", got)
	}
	bb := d.BettableBooks()
	if len(bb) != 1 || bb[0] != "fanatics" {
		t.Fatalf("BettableBooks = %v, want [fanatics]", bb)
	}

	part, err := Analyze(d, Options{Book: "fanatics", Target: DefaultTarget, Shots: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !part.Provisional {
		t.Error("a board with one of many games priced did not report provisional")
	}
	if len(part.Missing) != len(d.Games)-1 {
		t.Errorf("Missing = %d, want %d", len(part.Missing), len(d.Games)-1)
	}
	if len(part.PricedBooks) != 1 {
		t.Errorf("PricedBooks = %v, want exactly one", part.PricedBooks)
	}
}

// TestLinedLegsStayGameDisjoint guards the correctness property that admitting
// spread and total legs put at risk.
//
// The pairing has always refused two legs from one game WITHIN a parlay. While
// each game offered a single leg -- its moneyline dog -- that was also enough
// to keep games distinct ACROSS the set. Adding spread and total sides breaks
// that equivalence: two different parlays could each take a different leg from
// the same game, and a set riding one game twice is not two independent
// chances. Every hit-rate figure in the report would be inflated by exactly
// the correlation it claims to have excluded.
func TestLinedLegsStayGameDisjoint(t *testing.T) {
	d := contractDoc(Consensus)
	// Give every game a spread and a total, so each one offers five candidate
	// legs (dog, two spread sides, two total sides) instead of one.
	for _, id := range d.GameIDs() {
		l := d.Games[id].Books[Consensus]
		l.Spread = "+3.5 -110/-110"
		l.Total = "44.5 -110/-110"
		d.Games[id].Books[Consensus] = l
	}

	for _, obj := range []Objective{MaxHitRate, MaxConversion} {
		for shots := 1; shots <= 8; shots++ {
			a, err := Analyze(d, Options{
				Book: Consensus, Target: DefaultTarget, Shots: shots,
				Objective: obj, Lined: true,
			})
			if err != nil {
				t.Fatalf("%v shots=%d: %v", obj, shots, err)
			}
			games := map[string]string{}
			for _, p := range a.Set.Parlays {
				for _, leg := range p.Legs {
					if prev, dup := games[leg.GameID]; dup {
						t.Fatalf("%v shots=%d: game %s used twice (%s and %s)",
							obj, shots, leg.GameID, prev, leg.Team)
					}
					games[leg.GameID] = leg.Team
				}
			}
		}
	}
}

// TestLinedLegsWidenThePool pins what the flag actually buys, which is not
// what it first appeared to buy.
//
// Spread and total sides do not beat a long dog at a matched price; they
// convert within a point of each other. Under MaxConversion they change
// nothing, since moneyline dogs already maximise it. Under MaxHitRate they
// give the search shorter pairings that still clear the floor, which raises
// the hit rate at some cost in expected value -- a trade, not a free lunch.
// The assertion here is only that the hit rate never gets WORSE, since the
// lined pool is a superset of the plain one and the objective is free to
// ignore it.
func TestLinedLegsWidenThePool(t *testing.T) {
	d := contractDoc(Consensus)
	for _, id := range d.GameIDs() {
		l := d.Games[id].Books[Consensus]
		l.Spread = "+3.5 -110/-110"
		d.Games[id].Books[Consensus] = l
	}

	plain, err := Analyze(d, Options{Book: Consensus, Target: DefaultTarget, Shots: 8, Objective: MaxHitRate})
	if err != nil {
		t.Fatal(err)
	}
	lined, err := Analyze(d, Options{Book: Consensus, Target: DefaultTarget, Shots: 8, Objective: MaxHitRate, Lined: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(lined.Set.Parlays) < len(plain.Set.Parlays) {
		t.Errorf("lined built %d shots, fewer than %d without it",
			len(lined.Set.Parlays), len(plain.Set.Parlays))
	}
	if lined.Set.AnyHit < plain.Set.AnyHit {
		t.Errorf("lined AnyHit %.4f, worse than %.4f without it",
			lined.Set.AnyHit, plain.Set.AnyHit)
	}
}

// TestExcludeDropsCommittedTeams pins that a team already wagered elsewhere
// cannot reappear in a proposed set.
//
// Five tokens placed on one week tie up seven teams. A ticket riding a team
// you are already on is not another chance, it is more of the same one, and
// the hit-rate figure would count it as independent.
func TestExcludeDropsCommittedTeams(t *testing.T) {
	d := contractDoc(Consensus)
	for _, id := range d.GameIDs() {
		l := d.Games[id].Books[Consensus]
		l.Spread = "+3.5 -110/-110"
		d.Games[id].Books[Consensus] = l
	}
	committed := []string{"ARI", "NO", "CLE", "IND", "NE", "SF", "MIA"}

	// Lined too: a spread on a committed team is exactly as correlated as its
	// moneyline, and an exclusion that only reached moneylines would look like
	// it worked while leaking through the other market.
	for _, lined := range []bool{false, true} {
		a, err := Analyze(d, Options{
			Book: Consensus, Target: DefaultTarget, Shots: 6,
			Objective: MaxHitRate, Lined: lined, Exclude: committed,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range a.Set.Parlays {
			for _, leg := range p.Legs {
				word := strings.Fields(leg.Team)[0]
				for _, c := range committed {
					if strings.EqualFold(word, c) {
						t.Errorf("lined=%v: excluded team %s appeared as %q", lined, c, leg.Team)
					}
				}
			}
		}
		if len(a.Set.Parlays) == 0 {
			t.Errorf("lined=%v: excluding 7 of 32 teams left nothing buildable", lined)
		}
	}

	// Case sensitivity must not be a way past it.
	a, err := Analyze(d, Options{Book: Consensus, Target: DefaultTarget, Shots: 6,
		Objective: MaxHitRate, Exclude: []string{"ari", " Cle "}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range a.Set.Parlays {
		for _, leg := range p.Legs {
			if strings.EqualFold(leg.Team, "ARI") || strings.EqualFold(leg.Team, "CLE") {
				t.Errorf("lowercase/padded exclusion leaked %q", leg.Team)
			}
		}
	}
}

func mkFrontier(t *testing.T, book string, shots int) []Deployment {
	t.Helper()
	d := contractDoc(book)
	var legs []Side
	for _, id := range d.GameIDs() {
		l, ok, err := Devig(id, d.Games[id], book)
		if err != nil || !ok || l.Suspect {
			continue
		}
		legs = append(legs, l.Dog())
	}
	f, err := Frontier(legs, 50, 1, MaxHitRate, DefaultTarget)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestFrontierShowsTheTrade pins the frontier's shape: rows ordered by shots,
// stakes falling, hit rate never worsening, and every row deploying the whole
// bankroll. Conversion is not asserted either way -- see the note below.
func TestFrontierShowsTheTrade(t *testing.T) {
	f := mkFrontier(t, Consensus, 0)
	if len(f) < 2 {
		t.Fatalf("frontier has %d rows, need at least 2 to show a trade", len(f))
	}
	for i := 1; i < len(f); i++ {
		if f[i].Shots <= f[i-1].Shots {
			t.Fatalf("rows not ordered by shots: %d then %d", f[i-1].Shots, f[i].Shots)
		}
		if f[i].Stake >= f[i-1].Stake {
			t.Errorf("%d shots staked %.2f, not less than %.2f at %d",
				f[i].Shots, f[i].Stake, f[i-1].Stake, f[i-1].Shots)
		}
		if f[i].Set.AnyHit < f[i-1].Set.AnyHit {
			t.Errorf("%d shots hit %.3f, worse than %.3f at %d -- more shots must not lower the hit rate",
				f[i].Shots, f[i].Set.AnyHit, f[i-1].Set.AnyHit, f[i-1].Shots)
		}
		// Conversion is deliberately NOT asserted monotonic. Under MaxHitRate
		// it RISES with shots: the objective takes the shortest pairs clearing
		// the floor first, and those convert worst, so reaching further
		// improves conversion too. An earlier version of this test asserted a
		// trade that does not exist under that objective.
		// Every row must be placeable: the whole bankroll, no more.
		if got := f[i].Stake * float64(f[i].Shots); math.Abs(got-50) > 0.01 {
			t.Errorf("%d shots deploys %.2f, not the 50.00 available", f[i].Shots, got)
		}
	}
}

func TestFrontierRespectsMinBet(t *testing.T) {
	d := contractDoc(Consensus)
	var legs []Side
	for _, id := range d.GameIDs() {
		if l, ok, err := Devig(id, d.Games[id], Consensus); err == nil && ok && !l.Suspect {
			legs = append(legs, l.Dog())
		}
	}
	// $50 at a $15 minimum cannot be split more than three ways.
	f, err := Frontier(legs, 50, 15, MaxHitRate, DefaultTarget)
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range f {
		if dep.Shots > 3 {
			t.Errorf("%d shots at a 15.00 minimum needs %.2f", dep.Shots, dep.Stake)
		}
		if dep.Stake < 15 {
			t.Errorf("stake %.2f is below the 15.00 minimum", dep.Stake)
		}
	}
	if _, err := Frontier(legs, 5, 15, MaxHitRate, DefaultTarget); err == nil {
		t.Error("5.00 against a 15.00 minimum should be an error, not an empty plan")
	}
}

// TestAdviseNeverTellsYouToWaitOnExpiringMoney is the case most likely to be
// got backwards, and the most expensive one to get backwards.
//
// A weak window normally means wait. With funds that expire before the games
// are played, waiting forfeits the balance outright -- so the same board
// state has to produce the opposite advice.
func TestAdviseNeverTellsYouToWaitOnExpiringMoney(t *testing.T) {
	f := mkFrontier(t, Consensus, 0)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	lastKick := time.Date(2026, 9, 14, 20, 15, 0, 0, time.UTC)

	expires := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC) // before kickoff
	a := Advise(f, 99, 50, DefaultTarget, expires, lastKick, now)
	if a.CanWait {
		t.Error("funds expiring before kickoff cannot wait for a later board")
	}
	joined := strings.Join(a.Reasons, " ")
	if !strings.Contains(joined, "NOT available") {
		t.Errorf("advice on expiring funds must say waiting is unavailable, got: %v", a.Reasons)
	}

	survives := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) // after the season
	b := Advise(f, 99, 50, DefaultTarget, survives, lastKick, now)
	if !b.CanWait {
		t.Error("funds outlasting the window can wait")
	}
	if strings.Contains(strings.Join(b.Reasons, " "), "NOT available") {
		t.Error("funds that survive must not be told waiting is unavailable")
	}

	// No expiry at all is not an expiry in the past.
	if c := Advise(f, 1, 50, DefaultTarget, time.Time{}, lastKick, now); !c.CanWait {
		t.Error("funds with no recorded expiry should be treated as able to wait")
	}
}

// TestAdviseSeparatesThinFromWeak keeps the two signals distinct: a small
// window means place what works and hold the rest; a bad window means the
// tickets are not worth the face value.
func TestAdviseSeparatesThinFromWeak(t *testing.T) {
	f := mkFrontier(t, Consensus, 0)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	late := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	kick := time.Date(2026, 9, 14, 20, 15, 0, 0, time.UTC)

	a := Advise(f, 99, 50, DefaultTarget, late, kick, now)
	if a.Fillable != f[len(f)-1].Shots {
		t.Errorf("Fillable %d, want %d", a.Fillable, f[len(f)-1].Shots)
	}
	if a.StretchedTo <= 0 {
		t.Error("a thin window must report the stretched stake that still deploys everything")
	}
	if a.HoldBack <= 0 {
		t.Error("funds that outlast the window should be offered the hold-back alternative")
	}

	// The same thin window on EXPIRING funds must NOT offer to hold back:
	// an unplaced balance is worth zero, so stretching is strictly better.
	soon := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	exp := Advise(f, 99, 50, DefaultTarget, soon, kick, now)
	if exp.HoldBack != 0 {
		t.Errorf("HoldBack %.2f advised on funds expiring before kickoff", exp.HoldBack)
	}
	if exp.StretchedTo <= 0 {
		t.Error("expiring funds should still be stretched across what the window supports")
	}

	// Asking for exactly what fits is neither thin nor a hold-back.
	b := Advise(f, f[len(f)-1].Shots, 50, DefaultTarget, late, kick, now)
	if b.HoldBack != 0 {
		t.Errorf("HoldBack %.2f when the window fills the request exactly", b.HoldBack)
	}
}

// TestAdviseJudgesWeaknessOnTheBestRow guards a mistake made twice in this
// file's history: assuming fewer shots convert better.
//
// That holds under MaxConversion and inverts under MaxHitRate, where the
// objective takes the shortest pairs clearing the floor first -- the worst
// converters -- so reaching further improves conversion. Reading the first row
// as "the best split" called a healthy board weak.
func TestAdviseJudgesWeaknessOnTheBestRow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	kick := time.Date(2026, 9, 14, 20, 15, 0, 0, time.UTC)
	late := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	// A frontier whose first row scrapes the floor and whose last is well clear
	// of it. Only a scan finds the best.
	f := []Deployment{
		{Shots: 1, Set: ParlaySet{AvgConversion: 0.701, AnyHit: 0.21}},
		{Shots: 4, Set: ParlaySet{AvgConversion: 0.739, AnyHit: 0.54}},
	}
	if a := Advise(f, 4, 50, DefaultTarget, late, kick, now); a.Weak {
		t.Errorf("called weak on a frontier reaching %.1f%%, %.1f points above the floor",
			f[1].Set.AvgConversion*100, (f[1].Set.AvgConversion-DefaultTarget)*100)
	}

	// Genuinely weak: nothing on the frontier gets clear of the floor.
	g := []Deployment{
		{Shots: 1, Set: ParlaySet{AvgConversion: 0.703, AnyHit: 0.20}},
		{Shots: 4, Set: ParlaySet{AvgConversion: 0.708, AnyHit: 0.52}},
	}
	if a := Advise(g, 4, 50, DefaultTarget, late, kick, now); !a.Weak {
		t.Error("a frontier topping out at 70.8% should be called weak")
	}
}

// multiBookDoc prices every game at two books, with fanatics ALTERNATELY
// better and worse than consensus on the away side.
//
// Making one book uniformly worse is a trap: the optimiser then sends every
// ticket to the other one, and a test that never mixes books cannot detect a
// pairing that spans them or an allocation that misses one. Alternating forces
// a genuinely mixed set.
func multiBookDoc(t *testing.T) *Doc {
	t.Helper()
	d := contractDoc(Consensus)
	for i, id := range d.GameIDs() {
		c := d.Games[id].Books[Consensus]
		m, ok, err := ParseMarket(c.ML)
		if err != nil || !ok {
			continue
		}
		// BOTH sides move. Lengthening the dog alone drops the overround --
		// often below zero, which the de-vig correctly flags as suspect and
		// holds out of the pool, so the second book would silently never
		// appear in a set at all. An earlier version of this fixture did
		// exactly that and the tests passed while proving nothing.
		delta := wager.American(20)
		if i%2 == 1 {
			delta = -20
		}
		aa, bb := m.A+delta, m.B-delta
		if (aa > -100 && aa < 100) || (bb > -100 && bb < 100) {
			aa, bb = m.A, m.B // not representable; leave this game matched
		}
		d.Games[id].Books["fanatics"] = Lines{ML: FormatMarket(wager.Market{A: aa, B: bb})}
	}
	return d
}

// TestParlayLegsShareABook is the invariant multi-book pairing exists to
// protect. There is no ticket spanning two sportsbooks, and with several books
// pooled the best leg for one game and the best for another are routinely at
// different ones -- so an unguarded pairing produces wagers that cannot be
// placed anywhere.
func TestParlayLegsShareABook(t *testing.T) {
	d := multiBookDoc(t)
	for _, lined := range []bool{false, true} {
		for _, obj := range []Objective{MaxHitRate, MaxConversion} {
			a, err := Analyze(d, Options{
				Books: []string{Consensus, "fanatics"}, Target: DefaultTarget,
				Shots: 6, Objective: obj, Lined: lined,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Set.Parlays) == 0 {
				t.Fatalf("lined=%v %v: no set built from a two-book pool", lined, obj)
			}
			for _, p := range a.Set.Parlays {
				first := p.Legs[0].Book
				if first == "" {
					t.Errorf("leg %q carries no book, so it cannot be placed or funded", p.Legs[0].Team)
				}
				for _, l := range p.Legs[1:] {
					if l.Book != first {
						t.Errorf("lined=%v %v: parlay %v spans %s and %s",
							lined, obj, p.Teams(), first, l.Book)
					}
				}
			}
		}
	}
}

// TestAllocateSplitsPerBook pins that each book funds its own tickets from its
// own balance. A single global stake would strand the smaller book's money
// whenever the better prices sat elsewhere, and promotional money cannot move.
func TestAllocateSplitsPerBook(t *testing.T) {
	d := multiBookDoc(t)
	a, err := Analyze(d, Options{
		Books: []string{Consensus, "fanatics"}, Target: DefaultTarget,
		Shots: 4, Objective: MaxHitRate,
	})
	if err != nil {
		t.Fatal(err)
	}
	funds := map[string]float64{Consensus: 30, "fanatics": 20}
	alloc := Allocate(a.Set, funds)

	total := 0
	for _, al := range alloc {
		total += al.Tickets
		if al.Tickets > 0 && al.Funds > 0 {
			if got := al.Stake * float64(al.Tickets); math.Abs(got-al.Funds) > 0.01 {
				t.Errorf("%s deploys %.2f of a %.2f balance", al.Book, got, al.Funds)
			}
		}
	}
	if total != len(a.Set.Parlays) {
		t.Errorf("allocation covers %d tickets, set has %d", total, len(a.Set.Parlays))
	}
	// The fixture alternates which book is better, so a set that never mixes
	// means the pool is not really spanning both and the rest of this test
	// proves nothing.
	seen := map[string]bool{}
	for _, p := range a.Set.Parlays {
		seen[p.Book()] = true
	}
	if len(seen) < 2 {
		t.Fatalf("set used only %v; the fixture must produce a mixed set for this to test anything", seen)
	}

	// A balance the set never touches has to be visible, not silently dropped:
	// it is stranded if it is bonus money and movable if it is cash, and only
	// the operator knows which.
	idle := Allocate(ParlaySet{}, map[string]float64{"bet365": 40})
	if len(idle) != 1 || !idle[0].Idle {
		t.Errorf("an unused balance was not reported idle: %+v", idle)
	}

	// A book the set wants but no balance was declared for cannot be placed.
	one := Allocate(a.Set, map[string]float64{Consensus: 30})
	found := false
	for _, al := range one {
		if al.Book == "fanatics" && al.Unfunded {
			found = true
		}
	}
	if !found {
		t.Error("a book with tickets and no declared balance must be flagged unfunded")
	}
}

package ledger

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"
)

// base is the clock every test hangs off. Fixed, because a package about
// deadlines whose tests use time.Now() would be a package whose tests are a
// deadline.
var base = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return base.Add(d) }

func ptr(t time.Time) *time.Time { return &t }

// ev is a terse constructor so the campaign test below reads as a timeline
// rather than as struct literals.
func ev(id string, when time.Duration, k Kind) Event {
	return Event{Kind: k, ID: id, Time: at(when)}
}

func grant(id string, when time.Duration, l Lot) Event {
	e := ev(id, when, KindGrant)
	e.Creates = &l
	return e
}

func deposit(id string, when time.Duration, l Lot) Event {
	e := ev(id, when, KindDeposit)
	e.Creates = &l
	return e
}

func place(id string, when time.Duration, lot, wagerID string, amount float64) Event {
	e := ev(id, when, KindPlace)
	e.Lot, e.Wager, e.Amount = lot, wagerID, amount
	return e
}

func settle(id string, when time.Duration, wagerID string, r Result, returns *Lot) Event {
	e := ev(id, when, KindSettle)
	e.Wager, e.Result, e.Returns = wagerID, r, returns
	return e
}

func expire(id string, when time.Duration, lot string) Event {
	e := ev(id, when, KindExpire)
	e.Lot = lot
	return e
}

func mustBalances(t *testing.T, events []Event, asOf time.Time) Position {
	t.Helper()
	p, err := Balances(events, asOf)
	if err != nil {
		t.Fatalf("Balances(asOf=%s): unexpected error: %v", asOf.Format(time.RFC3339), err)
	}
	return p
}

func wantTotal(t *testing.T, p Position, book, asset string, want float64) {
	t.Helper()
	if got := p.Total(book, asset); math.Abs(got-want) > 1e-9 {
		t.Errorf("%s %s: got %.2f, want %.2f", book, asset, got, want)
	}
}

// wantErr asserts an error and that its text names the thing that went wrong.
// A replay error nobody can act on is only marginally better than no error.
func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got nil — the impossible state was silently absorbed", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("error %q does not mention %q", err, substr)
	}
}

// TestCampaignReplay walks a whole promo campaign and checks the derived
// position at several points along it. This is the shape the package is for: no
// balance is ever written down, so every one of these numbers is a claim about
// the arithmetic of replay.
func TestCampaignReplay(t *testing.T) {
	// Five $50 bonus tokens land at once, each with its own 24-hour clock, plus
	// $200 of real money to fund anything that needs a cash stake.
	events := []Event{
		deposit("d1", 0, Lot{ID: "cash-fan", Book: "fanatics", Asset: Cash, Amount: 200}),
	}
	for i, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		events = append(events, grant("g-"+id, time.Minute, Lot{
			ID: id, Book: "fanatics", Asset: Bonus, Amount: 50,
			// Staggered deadlines so the ordering test below has something to
			// order. All five arrive in the same minute, as they do in life.
			Expires: ptr(at(time.Duration(24+i*24) * time.Hour)),
		}))
	}

	// After the drop: $200 cash, $250 of bonus, nothing at risk.
	p := mustBalances(t, events, at(2*time.Minute))
	wantTotal(t, p, "fanatics", Cash, 200)
	wantTotal(t, p, "fanatics", Bonus, 250)
	if len(p.Committed) != 0 {
		t.Errorf("nothing has been placed yet, but %d commitments are open", len(p.Committed))
	}

	// Place all five tokens whole, on five wagers.
	for _, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		events = append(events, place("p-"+id, time.Hour, id, "w"+id[1:], 50))
	}

	// Mid-campaign: the tokens are gone from the balance but are not losses yet.
	// Reporting them as either held or spent would be wrong, so they are neither.
	p = mustBalances(t, events, at(2*time.Hour))
	wantTotal(t, p, "fanatics", Bonus, 0)
	wantTotal(t, p, "fanatics", Cash, 200)
	if len(p.Committed) != 5 {
		t.Fatalf("got %d open commitments, want 5", len(p.Committed))
	}
	var committed float64
	for _, c := range p.Committed {
		committed += c.Amount
	}
	if committed != 250 {
		t.Errorf("committed %.2f, want 250", committed)
	}

	// Two win, three lose. A Stake-Not-Returned bonus bet at +150 returns the
	// profit only: $75 cash on a $50 token, not $125.
	events = append(events,
		settle("s1", 3*time.Hour, "w1", Won, &Lot{ID: "r1", Book: "fanatics", Asset: Cash, Amount: 75}),
		settle("s2", 3*time.Hour, "w2", Won, &Lot{ID: "r2", Book: "fanatics", Asset: Cash, Amount: 75}),
		settle("s3", 3*time.Hour, "w3", Lost, nil),
		settle("s4", 3*time.Hour, "w4", Lost, nil),
		settle("s5", 3*time.Hour, "w5", Lost, nil),
	)

	p = mustBalances(t, events, at(4*time.Hour))
	wantTotal(t, p, "fanatics", Cash, 350) // 200 + 75 + 75
	wantTotal(t, p, "fanatics", Bonus, 0)
	if len(p.Committed) != 0 {
		t.Errorf("everything settled, but %d commitments are still open", len(p.Committed))
	}

	// The audit question. Replaying to a point *before* the settlements must
	// still show the money at risk and not the proceeds — a stored balance could
	// never answer this, which is the entire argument for deriving it.
	p = mustBalances(t, events, at(2*time.Hour))
	wantTotal(t, p, "fanatics", Cash, 200)
	if len(p.Committed) != 5 {
		t.Errorf("as of +2h, got %d open commitments, want 5", len(p.Committed))
	}

	// And replaying to before the grant shows only the deposit.
	p = mustBalances(t, events, at(30*time.Second))
	wantTotal(t, p, "fanatics", Cash, 200)
	wantTotal(t, p, "fanatics", Bonus, 0)

	// A zero asOf means the whole log, which here equals the final state.
	whole := mustBalances(t, events, time.Time{})
	wantTotal(t, whole, "fanatics", Cash, 350)
}

// TestReplayIsDeterministic runs the same log twice and also out of file order,
// because "derived" is only useful if it derives the same thing every time.
func TestReplayIsDeterministic(t *testing.T) {
	inOrder := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "betmgm", Asset: Cash, Amount: 100}),
		grant("g1", time.Minute, Lot{ID: "b", Book: "betmgm", Asset: Bonus, Amount: 25, Expires: ptr(at(48 * time.Hour))}),
		place("p1", time.Hour, "c", "w1", 40),
	}
	// The same events written to the file in a different order, which happens
	// whenever an operator records something after the fact. Replay sorts by
	// event time, so the answer must not move.
	shuffled := []Event{inOrder[2], inOrder[0], inOrder[1]}

	a := mustBalances(t, inOrder, time.Time{})
	b := mustBalances(t, shuffled, time.Time{})
	if len(a.Balances()) != len(b.Balances()) {
		t.Fatalf("file order changed the balance set: %v vs %v", a.Balances(), b.Balances())
	}
	for i := range a.Balances() {
		if a.Balances()[i] != b.Balances()[i] {
			t.Errorf("file order changed balance %d: %+v vs %+v", i, a.Balances()[i], b.Balances()[i])
		}
	}
}

// TestExpiringOrder is the function this package exists for. The ordering is the
// product, not a formatting detail.
func TestExpiringOrder(t *testing.T) {
	events := []Event{
		deposit("d1", 0, Lot{ID: "cash", Book: "fanduel", Asset: Cash, Amount: 500}), // no expiry at all
		grant("g1", 0, Lot{ID: "week", Book: "fanduel", Asset: Bonus, Amount: 25, Expires: ptr(at(6 * 24 * time.Hour))}),
		grant("g2", 0, Lot{ID: "today", Book: "fanduel", Asset: Bonus, Amount: 10, Expires: ptr(at(9 * time.Hour))}),
		grant("g3", 0, Lot{ID: "boost", Book: "fanatics", Asset: Boost, Expires: ptr(at(3 * 24 * time.Hour)),
			Boost: &BoostSpec{Percent: 0.5, MaxStake: 25, MinOdds: -500, RequiresCashStake: true}}),
		grant("g4", 0, Lot{ID: "next-month", Book: "caesars", Asset: Bonus, Amount: 100, Expires: ptr(at(30 * 24 * time.Hour))}),
	}

	got, err := Expiring(events, 7*24*time.Hour, base)
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	want := []string{"today", "boost", "week"}
	if len(got) != len(want) {
		t.Fatalf("got %d expiries %v, want %d %v", len(got), ids(got), len(want), want)
	}
	for i := range want {
		if got[i].Lot.ID != want[i] {
			t.Errorf("position %d: got %q, want %q (full order %v)", i, got[i].Lot.ID, want[i], ids(got))
		}
	}
	// The one dying today must report hours, not days.
	if got[0].In != 9*time.Hour {
		t.Errorf("token expiring today reports %v left, want 9h", got[0].In)
	}
	if got[0].Expired() {
		t.Error("a token with 9h left reported as already expired")
	}
	// Cash has no deadline and must not appear at all: padding the list with
	// things that cannot die is how the list stops being read.
	for _, e := range got {
		if e.Lot.ID == "cash" {
			t.Error("cash has no expiry but appeared in the expiring list")
		}
	}

	// A shorter window narrows the list rather than reordering it.
	soon, err := Expiring(events, 24*time.Hour, base)
	if err != nil {
		t.Fatalf("Expiring(24h): %v", err)
	}
	if len(soon) != 1 || soon[0].Lot.ID != "today" {
		t.Errorf("within 24h: got %v, want [today]", ids(soon))
	}
}

// TestExpiringSurfacesOverdueLots covers the case that actually loses money: the
// deadline passed and nobody recorded an expire event, so the ledger still
// thinks the asset is held.
func TestExpiringSurfacesOverdueLots(t *testing.T) {
	events := []Event{
		grant("g1", 0, Lot{ID: "missed", Book: "fanatics", Asset: Bonus, Amount: 50, Expires: ptr(at(-2 * time.Hour))}),
		grant("g2", 0, Lot{ID: "live", Book: "fanatics", Asset: Bonus, Amount: 50, Expires: ptr(at(2 * time.Hour))}),
	}
	got, err := Expiring(events, 24*time.Hour, base)
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want both lots", ids(got))
	}
	if got[0].Lot.ID != "missed" || !got[0].Expired() {
		t.Errorf("the overdue lot should sort first and report as expired, got %+v", got[0])
	}
	if got[1].Expired() {
		t.Errorf("the live lot reported as expired: %+v", got[1])
	}
}

func ids(es []Expiry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Lot.ID
	}
	return out
}

// TestOverspendIsAnError — the headline reason balances are replayed rather than
// stored. A stored number would just have gone negative.
func TestOverspendIsAnError(t *testing.T) {
	events := []Event{
		grant("g1", 0, Lot{ID: "b", Book: "betmgm", Asset: Bonus, Amount: 50}),
		place("p1", time.Hour, "b", "w1", 80),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "only 50.00 remains")
}

func TestPartialSpendThenOverspend(t *testing.T) {
	events := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "betmgm", Asset: Cash, Amount: 100}),
		place("p1", time.Hour, "c", "w1", 60),
		place("p2", 2*time.Hour, "c", "w2", 60),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "only 40.00 remains")

	// The same log stopped before the second placement is perfectly valid, which
	// is what makes asOf an audit tool rather than a filter.
	p := mustBalances(t, events[:2], time.Time{})
	wantTotal(t, p, "betmgm", Cash, 40)
}

// TestExpireAfterSpendIsAnError — the case the brief calls out by name. The
// asset is already gone; an expiry on top of it means the log is describing two
// different histories.
func TestExpireAfterSpendIsAnError(t *testing.T) {
	events := []Event{
		grant("g1", 0, Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: 50, Expires: ptr(at(24 * time.Hour))}),
		place("p1", time.Hour, "b", "w1", 50),
		expire("x1", 25*time.Hour, "b"),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "already placed")
}

func TestExpireTwiceIsAnError(t *testing.T) {
	events := []Event{
		grant("g1", 0, Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: 50, Expires: ptr(at(24 * time.Hour))}),
		expire("x1", 25*time.Hour, "b"),
		expire("x2", 26*time.Hour, "b"),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "already expired")
}

// TestConvertNonexistentAssetIsAnError — converting from nothing would mint
// value out of a typo.
func TestConvertNonexistentAssetIsAnError(t *testing.T) {
	e := ev("c1", time.Hour, KindConvert)
	e.Lot, e.Amount = "fancash-nope", 30
	e.Creates = &Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: 30}
	_, err := Balances([]Event{e}, time.Time{})
	wantErr(t, err, "no such lot")
}

// TestConvertMustConserveValue is the reason convert names both sides.
func TestConvertMustConserveValue(t *testing.T) {
	mk := func(out float64) []Event {
		c := ev("c1", time.Hour, KindConvert)
		c.Lot, c.Amount = "fc", 30
		c.Creates = &Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: out, Expires: ptr(at(7 * 24 * time.Hour))}
		return []Event{
			// "credit" is an asset name this package has never heard of. It is
			// the site-credit type that is deliberately out of scope, and it
			// converts here without a single line of type-specific code.
			grant("g1", 0, Lot{ID: "fc", Book: "fanatics", Asset: "credit", Amount: 30, Expires: ptr(at(7 * 24 * time.Hour))}),
			c,
		}
	}

	_, err := Balances(mk(45), time.Time{})
	wantErr(t, err, "must not create or destroy value")

	// 1:1 is the real rule, and it must work.
	p := mustBalances(t, mk(30), time.Time{})
	wantTotal(t, p, "fanatics", "credit", 0)
	wantTotal(t, p, "fanatics", Bonus, 30)
}

func TestSettleWithoutPlacementIsAnError(t *testing.T) {
	events := []Event{settle("s1", time.Hour, "ghost", Won, nil)}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "no placement was recorded")
}

func TestDoubleSettleIsAnError(t *testing.T) {
	events := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "bet365", Asset: Cash, Amount: 100}),
		place("p1", time.Hour, "c", "w1", 100),
		settle("s1", 2*time.Hour, "w1", Lost, nil),
		settle("s2", 3*time.Hour, "w1", Won, &Lot{ID: "r", Book: "bet365", Asset: Cash, Amount: 250}),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "already settled")
}

func TestDuplicateIDsAreErrors(t *testing.T) {
	dup := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "caesars", Asset: Cash, Amount: 10}),
		deposit("d1", time.Hour, Lot{ID: "c2", Book: "caesars", Asset: Cash, Amount: 10}),
	}
	_, err := Balances(dup, time.Time{})
	wantErr(t, err, "duplicate event id")

	dupLot := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "caesars", Asset: Cash, Amount: 10}),
		deposit("d2", time.Hour, Lot{ID: "c", Book: "caesars", Asset: Cash, Amount: 10}),
	}
	_, err = Balances(dupLot, time.Time{})
	wantErr(t, err, "already exists")
}

// TestBoostRoundTrip proves the fields that made a boost worthless survive the
// file, which is the whole reason a boost is a struct and not a number.
func TestBoostRoundTrip(t *testing.T) {
	expires := at(72 * time.Hour)
	orig := grant("g1", 0, Lot{
		ID: "boost-1", Book: "fanatics", Asset: Boost, Expires: &expires,
		Boost: &BoostSpec{Percent: 0.5, MaxStake: 25, MinOdds: -500, RequiresCashStake: true},
	})
	orig.Note = "fanatics 50% profit boost, cash stake only"

	var buf bytes.Buffer
	if err := Append(&buf, orig); err != nil {
		t.Fatalf("Append: %v", err)
	}
	back, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("got %d events, want 1", len(back))
	}
	got := back[0].Creates
	if got == nil || got.Boost == nil {
		t.Fatalf("boost did not survive the round trip: %+v", back[0])
	}
	if got.Boost.Percent != 0.5 || got.Boost.MaxStake != 25 || got.Boost.MinOdds != -500 {
		t.Errorf("boost fields changed: %+v", *got.Boost)
	}
	if !got.Boost.RequiresCashStake {
		t.Error("RequiresCashStake was lost — this is the field that made four boosts worthless, and dropping it silently is the exact failure the type exists to prevent")
	}
	if !got.Expires.Equal(expires) {
		t.Errorf("expiry changed: got %s, want %s", got.Expires, expires)
	}
	if got.Amount != 0 || !got.Unit() {
		t.Errorf("boost came back carrying amount %v; a boost is not an amount", got.Amount)
	}
	if back[0].Note != orig.Note {
		t.Errorf("note changed: %q", back[0].Note)
	}

	// And it counts as a unit, not as money.
	p := mustBalances(t, back, time.Time{})
	if n := p.Units("fanatics", Boost); n != 1 {
		t.Errorf("got %d boost units, want 1", n)
	}
	wantTotal(t, p, "fanatics", Boost, 0)
}

// TestBoostRejectsAmount guards the other half of the same idea: a boost may not
// be smuggled back into being a number.
func TestBoostRejectsAmount(t *testing.T) {
	e := grant("g1", 0, Lot{ID: "b", Book: "fanatics", Asset: Boost, Amount: 19.03,
		Boost: &BoostSpec{Percent: 0.5}})
	wantErr(t, e.Validate(), "a boost is not an amount")
}

// TestBoostNeedingCashCannotRideABonusBet is the $19.03 lesson, enforced.
func TestBoostNeedingCashCannotRideABonusBet(t *testing.T) {
	boostLot := Lot{ID: "boost-1", Book: "fanatics", Asset: Boost,
		Boost: &BoostSpec{Percent: 0.5, MaxStake: 25, MinOdds: -500, RequiresCashStake: true}}

	bad := []Event{
		grant("g1", 0, Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: 25}),
		grant("g2", 0, boostLot),
		place("p1", time.Hour, "b", "w1", 25),
		place("p2", time.Hour, "boost-1", "w1", 0),
		settle("s1", 2*time.Hour, "w1", Won, &Lot{ID: "r", Book: "fanatics", Asset: Cash, Amount: 50}),
	}
	_, err := Balances(bad, time.Time{})
	wantErr(t, err, "requires a real-money stake")

	// The same boost on a cash-funded wager is fine, and both places belong to
	// one wager id.
	good := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "fanatics", Asset: Cash, Amount: 25}),
		grant("g2", 0, boostLot),
		place("p1", time.Hour, "c", "w1", 25),
		place("p2", time.Hour, "boost-1", "w1", 0),
		settle("s1", 2*time.Hour, "w1", Won, &Lot{ID: "r", Book: "fanatics", Asset: Cash, Amount: 62.50}),
	}
	p := mustBalances(t, good, time.Time{})
	wantTotal(t, p, "fanatics", Cash, 62.50)
	if n := p.Units("fanatics", Boost); n != 0 {
		t.Errorf("the boost was used but %d remain", n)
	}
}

// TestBoostIsSpentWhole — a unit asset cannot be drawn down.
func TestBoostIsSpentWhole(t *testing.T) {
	events := []Event{
		grant("g1", 0, Lot{ID: "boost-1", Book: "fanatics", Asset: Boost, Boost: &BoostSpec{Percent: 0.5}}),
		place("p1", time.Hour, "boost-1", "w1", 10),
	}
	_, err := Balances(events, time.Time{})
	wantErr(t, err, "spent whole or not at all")
}

// TestUnknownAssetIsCarriedFaithfully is the open-string guarantee. "fancash" is
// a type this package has never been told about; it must round-trip, replay,
// expire, and above all leave the known assets alone.
func TestUnknownAssetIsCarriedFaithfully(t *testing.T) {
	events := []Event{
		deposit("d1", 0, Lot{ID: "c", Book: "fanatics", Asset: Cash, Amount: 100}),
		grant("g1", 0, Lot{ID: "b", Book: "fanatics", Asset: Bonus, Amount: 50, Expires: ptr(at(24 * time.Hour))}),
		grant("g2", 0, Lot{ID: "fc", Book: "fanatics", Asset: "fancash", Amount: 40, Expires: ptr(at(7 * 24 * time.Hour))}),
		grant("g3", 0, Lot{ID: "mystery", Book: "fanatics", Asset: "some-future-thing", Amount: 7}),
	}

	// Round trip through the file, since "parses fine" is half the claim.
	var buf bytes.Buffer
	for _, e := range events {
		if err := Append(&buf, e); err != nil {
			t.Fatalf("Append(%s): %v", e.ID, err)
		}
	}
	back, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	p := mustBalances(t, back, time.Time{})
	// The known assets are exactly what they would be without the strangers.
	wantTotal(t, p, "fanatics", Cash, 100)
	wantTotal(t, p, "fanatics", Bonus, 50)
	// And the strangers are held at face value under their own names.
	wantTotal(t, p, "fanatics", "fancash", 40)
	wantTotal(t, p, "fanatics", "some-future-thing", 7)

	// They are not silently folded into cash or bonus.
	for _, b := range p.Balances() {
		if b.Asset == Cash && b.Amount != 100 {
			t.Errorf("cash contaminated by an unknown asset: %.2f", b.Amount)
		}
		if b.Asset == Bonus && b.Amount != 50 {
			t.Errorf("bonus contaminated by an unknown asset: %.2f", b.Amount)
		}
	}
	if len(p.Balances()) != 4 {
		t.Errorf("got %d balance rows, want 4 (one per asset): %+v", len(p.Balances()), p.Balances())
	}

	// The unknown asset participates in expiry like any other, because Expiring
	// reads the deadline off the lot rather than off the asset name.
	exp, err := Expiring(back, 8*24*time.Hour, base)
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if got := ids(exp); len(got) != 2 || got[0] != "b" || got[1] != "fc" {
		t.Errorf("got expiries %v, want [b fc]", got)
	}
}

func TestParseRejectsStructurallyBrokenLines(t *testing.T) {
	cases := []struct {
		name, line, want string
	}{
		{"unknown kind", `{"kind":"transfer","id":"e1","time":"2026-08-20T12:00:00Z"}`, "unknown kind"},
		{"no time", `{"kind":"grant","id":"e1"}`, "no time"},
		{"grant creates nothing", `{"kind":"grant","id":"e1","time":"2026-08-20T12:00:00Z"}`, "creates nothing"},
		{"place with no wager", `{"kind":"place","id":"e1","time":"2026-08-20T12:00:00Z","lot":"b","amount":5}`, "no wager id"},
		{"expire with amount", `{"kind":"expire","id":"e1","time":"2026-08-20T12:00:00Z","lot":"b","amount":5}`, "not a partial spend"},
		{"lot with no book", `{"kind":"grant","id":"e1","time":"2026-08-20T12:00:00Z","creates":{"asset":"bonus","amount":5}}`, "no book"},
		{"settle with bad result", `{"kind":"settle","id":"e1","time":"2026-08-20T12:00:00Z","wager":"w1","result":"maybe"}`, "want won, lost, push or void"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.line))
			wantErr(t, err, tc.want)
		})
	}
}

func TestParseSkipsBlankLines(t *testing.T) {
	in := "\n" + `{"kind":"deposit","id":"d1","time":"2026-08-20T12:00:00Z","creates":{"book":"fanduel","asset":"cash","amount":50}}` + "\n\n"
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	// A lot with no id of its own inherits the event id, so it is still
	// referable by later events.
	p := mustBalances(t, events, time.Time{})
	if len(p.Lots) != 1 || p.Lots[0].ID != "d1" {
		t.Errorf("lot did not inherit the event id: %+v", p.Lots)
	}
}

func TestNewIDsAreDistinctWithinATick(t *testing.T) {
	now := time.Now()
	a, b := NewID(now, "fanatics bonus"), NewID(now, "fanatics bonus")
	if a == b {
		t.Fatalf("NewID collided within one tick: %s", a)
	}
}

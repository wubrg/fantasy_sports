package oddspull

import (
	"encoding/json"
	"strings"
	"testing"
)

// harOf wraps response bodies (JSON or bet365 text) in a HAR.
func harOf(t *testing.T, bodies ...string) []byte {
	t.Helper()
	var entries []map[string]any
	for _, b := range bodies {
		entries = append(entries, map[string]any{"response": map[string]any{"content": map[string]any{"text": b}}})
	}
	data, err := json.Marshal(map[string]any{"log": map[string]any{"entries": entries}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// b365 joins records the way the wire format does.
func b365(recs ...string) string { return "F|CL;ID=12;|" + strings.Join(recs, "|") + "|" }

// header is the tab list every bet365 response repeats; the selected tab's id is
// the cmx record that follows it.
//
// Like the real page, the generic "Lines" and "Props" tabs come BEFORE the
// specific "Game Lines" and "TD Scorers" ones and share their ids; a parser that
// keeps the first name it sees for an id never finds either specific tab.
const b365Header = "MA;NA=Lines;PD=#AC#B12#C1#D48#E1441#F36#;|" +
	"MA;NA=Props;PD=#AC#B12#C1#D47#E120591#F47#;|" +
	"MA;NA=Game Lines;PD=#AC#B12#C1#D48#E1441#F36#N0#;|" +
	"MA;NA=TD Scorers;PD=#AC#B12#C1#D47#E120591#F47#N0#;|" +
	"MA;NA=Pass Yds;PD=#AC#B12#C1#D47#E121155#F47#N0#;"

const passYdsTab = "F|CL;ID=12;|" + b365Header + "|MG;ID=121155;SY=cmx;OI=10;|" +
	"MG;ID=121155;FI=11;SY=sgh;NA=WAS Commanders @ NY Giants;|" +
	"MA;ID=M121155;NA=Player / Last 5;|" +
	"PA;ID=PC111;NA=J.J. McCarthy;N2=NY Giants;|" +
	"PA;ID=PC222;NA=Jayden Daniels;N2=WAS Commanders;|" +
	"MA;ID=121155;|" +
	"CO;ID=C1;NA=150;|PA;ID=111;OD=1/8;|PA;ID=222;OD=2/9;|" +
	"CO;ID=C1;NA=200;|PA;ID=112;OD=1/2;|PA;ID=223;OD=5/6;|" +
	// McCarthy has no price at 225: his slot is an empty placeholder, and
	// Daniels's price (31/20) must still land on Daniels.
	"CO;ID=C1;NA=225;|PA;ID=999;SU=1;OD=;|PA;ID=224;OD=31/20;|" +
	"MG;ID=121155;FI=12;SY=sgh;NA=NE Patriots @ DET Lions;|" +
	"PA;ID=PC333;NA=Drake Maye;N2=NE Patriots;|" +
	"CO;ID=C1;NA=200;|PA;ID=333;OD=1/9;|"

func findLine(outs []Outcome, sel string, line float64) (Outcome, bool) {
	for _, o := range outs {
		if o.Selection == sel && o.Line != nil && *o.Line == line {
			return o, true
		}
	}
	return Outcome{}, false
}

func TestBet365FracToAmerican(t *testing.T) {
	cases := []struct {
		od   string
		want int
		ok   bool
	}{
		{"10/11", -110, true}, {"1/1", 100, true}, {"17/2", 850, true}, {"1/8", -800, true},
		{"100/113", -113, true}, {"31/20", 155, true},
		{"", 0, false}, {"5", 0, false}, {"0/5", 0, false}, {"a/b", 0, false}, {"-1/2", 0, false},
	}
	for _, c := range cases {
		got, ok := b365FracToAmerican(c.od)
		if ok != c.ok || (ok && int(got) != c.want) {
			t.Errorf("%q: got (%d, %v), want (%d, %v)", c.od, got, ok, c.want, c.ok)
		}
	}
}

func TestParseBet365Ladder(t *testing.T) {
	outs, err := Parse(harOf(t, passYdsTab))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		if o.Book != Bet365 || o.BookName() != "bet365" {
			t.Fatalf("every outcome should be stamped bet365, got %+v", o)
		}
		if o.Category != "Passing" || o.Market == "" || o.Event == "" {
			t.Errorf("category/market/event not set: %+v", o)
		}
	}

	mc, ok := findLine(outs, "J.J. McCarthy 150+", 150)
	if !ok || mc.Price != -800 || mc.Event != "WAS Commanders @ NY Giants" || mc.Market != "J.J. McCarthy Passing Yards" {
		t.Errorf("McCarthy 150+: %+v ok=%v", mc, ok)
	}
	if dn, ok := findLine(outs, "Jayden Daniels 200+", 200); !ok || dn.Price != -120 {
		t.Errorf("Daniels 200+: %+v ok=%v", dn, ok)
	}

	// The placeholder at McCarthy's 225 slot must not shift Daniels's price onto
	// McCarthy, and must not itself become an outcome.
	if _, ok := findLine(outs, "J.J. McCarthy 225+", 225); ok {
		t.Error("McCarthy has no 225 price; the placeholder must not produce one")
	}
	if dn, ok := findLine(outs, "Jayden Daniels 225+", 225); !ok || dn.Price != 155 {
		t.Errorf("Daniels 225+ should keep 31/20 = +155 despite the placeholder before it: %+v ok=%v", dn, ok)
	}

	// A second game block starts a fresh player list.
	if mm, ok := findLine(outs, "Drake Maye 200+", 200); !ok || mm.Event != "NE Patriots @ DET Lions" || mm.Price != -900 {
		t.Errorf("Maye: %+v ok=%v", mm, ok)
	}
}

func TestBet365LadderRungsNeverPairAsOverUnder(t *testing.T) {
	// McCarthy shows exactly two rungs. If they shared a market id they would be
	// paired and de-vigged as if one were an Over and the other an Under.
	outs, _ := Parse(harOf(t, passYdsTab))
	ids := map[string]int{}
	for _, o := range outs {
		ids[o.Event+"|"+o.MarketID]++
	}
	for k, n := range ids {
		if n != 1 {
			t.Errorf("market id %q is shared by %d ladder rungs; each rung needs its own", k, n)
		}
	}
}

const linesTab = "F|CL;ID=12;|" + b365Header + "|MG;ID=1441;SY=cmx;OI=10;|MA;ID=M1441;NA= ;|" +
	"PA;ID=PC1;NA=WAS Commanders;N2=NY Giants;FD=WAS Commanders @ NY Giants;FI=10;|PA;PZ=0;|" +
	"PA;ID=PC2;NA=BUF Bills;N2=NY Jets;FD=BUF Bills @ NY Jets;FI=20;|PA;PZ=1;|" +
	"MA;ID=M1441;NA=Spread;FI=10;|" +
	"PA;ID=1;FI=10;HD=+2.5;HA=+2.5;OD=10/11;PZ=0;|PA;ID=2;FI=10;HD=-2.5;HA=-2.5;OD=10/11;PZ=0;|" +
	"PA;ID=3;FI=20;HD=-7.5;HA=-7.5;OD=100/113;PZ=1;|PA;ID=4;FI=20;HD=+7.5;HA=+7.5;OD=100/107;PZ=1;|" +
	"MA;ID=M1442;NA=Total;FI=10;|" +
	"PA;ID=5;FI=11;HD=O 41.5;HA=41.5;OD=100/103;OI=10;PZ=0;|PA;ID=6;FI=11;HD=U 41.5;HA=41.5;OD=100/117;OI=10;PZ=0;|" +
	"PA;ID=7;FI=21;HD=O 50.5;HA=50.5;OD=100/118;OI=20;PZ=1;|PA;ID=8;FI=21;HD=U 50.5;HA=50.5;OD=100/102;OI=20;PZ=1;|" +
	"MA;ID=M936;NA=Money;FI=10;|" +
	"PA;ID=9;FI=12;OD=6/5;PZ=0;|PA;ID=10;FI=12;OD=5/7;PZ=0;|" +
	"PA;ID=11;FI=22;OD=2/9;PZ=1;|PA;ID=12;FI=22;OD=7/2;PZ=1;|"

func TestParseBet365Lines(t *testing.T) {
	outs, err := Parse(harOf(t, linesTab))
	if err != nil {
		t.Fatal(err)
	}
	get := func(event, market, sel string) (Outcome, bool) {
		for _, o := range outs {
			if o.Event == event && o.Market == market && o.Selection == sel {
				return o, true
			}
		}
		return Outcome{}, false
	}
	// First record of a pair is the away side, the second the home side.
	if o, ok := get("WAS Commanders @ NY Giants", "Moneyline", "WAS Commanders"); !ok || o.Price != 120 {
		t.Errorf("WAS ML: %+v ok=%v", o, ok)
	}
	if o, ok := get("WAS Commanders @ NY Giants", "Moneyline", "NY Giants"); !ok || o.Price != -140 {
		t.Errorf("NYG ML: %+v ok=%v", o, ok)
	}
	// Spread line is signed for the side named, away first.
	if o, ok := get("BUF Bills @ NY Jets", "Spread", "BUF Bills"); !ok || o.Line == nil || *o.Line != -7.5 || o.Price != -113 {
		t.Errorf("BUF spread: %+v ok=%v", o, ok)
	}
	if o, ok := get("BUF Bills @ NY Jets", "Spread", "NY Jets"); !ok || o.Line == nil || *o.Line != 7.5 || o.Price != -107 {
		t.Errorf("NYJ spread: %+v ok=%v", o, ok)
	}
	if o, ok := get("WAS Commanders @ NY Giants", "Total", "Over"); !ok || o.Line == nil || *o.Line != 41.5 || o.Price != -103 {
		t.Errorf("total over: %+v ok=%v", o, ok)
	}
	if o, ok := get("WAS Commanders @ NY Giants", "Total", "Under"); !ok || o.Price != -117 {
		t.Errorf("total under: %+v ok=%v", o, ok)
	}
	// Both sides of a market share an id (so the de-vig pairs them), and different
	// markets/games do not.
	a, _ := get("BUF Bills @ NY Jets", "Spread", "BUF Bills")
	b, _ := get("BUF Bills @ NY Jets", "Spread", "NY Jets")
	c, _ := get("BUF Bills @ NY Jets", "Moneyline", "BUF Bills")
	if a.MarketID != b.MarketID || a.MarketID == c.MarketID || a.MarketID == "" {
		t.Errorf("market ids: spread %q/%q, ml %q", a.MarketID, b.MarketID, c.MarketID)
	}
	for _, o := range outs {
		if o.Category != "Game" || o.Book != Bet365 {
			t.Errorf("game line not Game/bet365: %+v", o)
		}
	}
	if len(outs) != 12 {
		t.Errorf("2 games x (ML + spread + total) x 2 sides = 12 outcomes, got %d", len(outs))
	}
}

func TestParseBet365RefusesAMismatchedSpreadPair(t *testing.T) {
	// A spread whose two lines are not each other's negation is not one market;
	// reading it anyway would price a side against the wrong number.
	bad := strings.Replace(linesTab, "HD=-2.5;HA=-2.5;OD=10/11;PZ=0", "HD=-3.5;HA=-3.5;OD=10/11;PZ=0", 1)
	outs, err := Parse(harOf(t, bad))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		if o.Market == "Spread" && o.Event == "WAS Commanders @ NY Giants" {
			t.Errorf("mismatched spread pair should be skipped, got %+v", o)
		}
	}
}

const tdTab = "F|CL;ID=12;|" + b365Header + "|MG;ID=120591;SY=cmx;OI=10;|" +
	"MG;ID=M120591;FI=11;SY=fd;NA=WAS Commanders @ NY Giants;|" +
	"MA;ID=C120591;NA=Player / Last 5;|" +
	"PA;ID=P1;NA=Cam Skattebo;N2=NY Giants;|PA;ID=P2;NA=Rachaad White;N2=WAS Commanders;|" +
	"MA;ID=C120591;NA=First;|PA;ID=1;OD=6/1;|PA;ID=2;OD=8/1;|" +
	"MA;ID=C120591;NA=Anytime;|PA;ID=3;OD=6/5;|PA;ID=4;OD=33/20;|"

func TestParseBet365TD(t *testing.T) {
	outs, err := Parse(harOf(t, tdTab))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"First TD Scorer|Cam Skattebo": 600, "First TD Scorer|Rachaad White": 800,
		"Anytime TD Scorer|Cam Skattebo": 120, "Anytime TD Scorer|Rachaad White": 165,
	}
	if len(outs) != len(want) {
		t.Fatalf("got %d outcomes, want %d: %+v", len(outs), len(want), outs)
	}
	for _, o := range outs {
		p, ok := want[o.Market+"|"+o.Selection]
		if !ok || int(o.Price) != p || o.Category != "Touchdown" || o.Line != nil {
			t.Errorf("unexpected TD outcome %+v (want price %d)", o, p)
		}
	}
}

func TestParseBet365UnknownTabIsSkippedNotGuessed(t *testing.T) {
	other := strings.ReplaceAll(passYdsTab, "MG;ID=121155;SY=cmx", "MG;ID=999999;SY=cmx")
	if _, err := Parse(harOf(t, other)); err == nil {
		t.Error("a tab id the page header does not name carries no recognised market and should yield no outcomes")
	}
}

func TestBet365AndDraftKingsCoexist(t *testing.T) {
	dk := `{"events":[{"id":"E1","name":"WAS Commanders @ NY Giants"}],` +
		`"markets":[{"id":"M1","eventId":"E1","name":"Moneyline"}],` +
		`"selections":[{"marketId":"M1","label":"WAS Commanders","displayOdds":{"american":"+120"}},` +
		`{"marketId":"M1","label":"NY Giants","displayOdds":{"american":"-140"}}]}`
	outs, err := Parse(harOf(t, dk, linesTab))
	if err != nil {
		t.Fatal(err)
	}
	books := map[string]int{}
	for _, o := range outs {
		if o.Event == "WAS Commanders @ NY Giants" && o.Market == "Moneyline" && o.Selection == "WAS Commanders" {
			books[o.BookName()]++
		}
	}
	// Same event, market, selection and price at both books: dedup must not
	// collapse them into one row.
	if books["draftkings"] != 1 || books["bet365"] != 1 {
		t.Errorf("want one WAS ML row per book, got %v", books)
	}
	for _, o := range outs {
		if o.Book == Bet365 {
			continue
		}
		if o.Book != "" || o.BookName() != DraftKings {
			t.Errorf("DraftKings outcome should be untouched (empty Book), got %+v", o)
		}
	}
}

func TestBet365NonBet365TextIsIgnored(t *testing.T) {
	// A response that merely contains ';' or starts with 'F' is not the wire format.
	if isBet365Text("Foo;bar") || isBet365Text("") || isBet365Text("F|") {
		t.Error("only text beginning F| and containing ';' is bet365's format")
	}
}

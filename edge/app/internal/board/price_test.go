package board

import (
	"strings"
	"testing"

	"edge/internal/wager"
)

func TestParseMarket(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantOK  bool
		wantA   wager.American
		wantB   wager.American
		wantErr bool
	}{
		{name: "plain", in: "130/-155", wantOK: true, wantA: 130, wantB: -155},
		{name: "leading plus", in: "+130/-155", wantOK: true, wantA: 130, wantB: -155},
		{name: "spaces", in: "  130 / -155  ", wantOK: true, wantA: 130, wantB: -155},
		{name: "both negative", in: "-118/-102", wantOK: true, wantA: -118, wantB: -102},

		// An empty cell is the normal state of most of the board and must not
		// be an error, or validate would report several thousand problems on a
		// freshly scaffolded season.
		{name: "empty", in: "", wantOK: false},
		{name: "whitespace only", in: "   ", wantOK: false},

		{name: "no slash", in: "130", wantErr: true},
		{name: "missing side", in: "130/", wantErr: true},
		{name: "not a number", in: "abc/-155", wantErr: true},
		// Values strictly between -100 and +100 are not real prices. Accepting
		// one would de-vig into a confident wrong answer rather than failing.
		{name: "impossible price", in: "50/-155", wantErr: true},
		{name: "zero", in: "0/-155", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ok, err := ParseMarket(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseMarket(%q) = no error, want one", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMarket(%q) errored: %v", tc.in, err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ParseMarket(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && (m.A != tc.wantA || m.B != tc.wantB) {
				t.Fatalf("ParseMarket(%q) = %v/%v, want %v/%v", tc.in, m.A, m.B, tc.wantA, tc.wantB)
			}
		})
	}
}

func TestParseHandicap(t *testing.T) {
	line, m, ok, err := ParseHandicap("2.5 -110/-110")
	if err != nil || !ok {
		t.Fatalf("ParseHandicap errored: %v (ok=%v)", err, ok)
	}
	if line != 2.5 || m.A != -110 || m.B != -110 {
		t.Fatalf("got %v %v/%v, want 2.5 -110/-110", line, m.A, m.B)
	}

	// A negative away spread means the away team is favoured; one signed
	// number cannot disagree with itself the way a pair could.
	if line, _, _, err := ParseHandicap("-3.5 -105/-115"); err != nil || line != -3.5 {
		t.Fatalf("negative line: got %v, err %v", line, err)
	}
	if _, _, ok, err := ParseHandicap(""); err != nil || ok {
		t.Fatalf("empty handicap: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	for _, bad := range []string{"2.5", "x -110/-110", "2.5 -110"} {
		if _, _, _, err := ParseHandicap(bad); err == nil {
			t.Fatalf("ParseHandicap(%q) = no error, want one", bad)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	m := wager.Market{A: 390, B: -525}
	s := FormatMarket(m)
	if s != "+390/-525" {
		t.Fatalf("FormatMarket = %q, want %q", s, "+390/-525")
	}
	back, ok, err := ParseMarket(s)
	if err != nil || !ok || back != m {
		t.Fatalf("round trip: got %v ok=%v err=%v", back, ok, err)
	}

	h := FormatHandicap(-3.5, wager.Market{A: -110, B: -110})
	if h != "-3.5 -110/-110" {
		t.Fatalf("FormatHandicap = %q", h)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	// A board is typed in bulk, so validate reports all problems rather than
	// stopping at the first: fixing transcription errors one round trip at a
	// time is the slowest possible way to do it.
	src := `
season: 2026
week: 1
games:
  2026_01_NYJ_TEN:
    away: NYJ
    home: TEN
    kickoff: 2026-09-13T13:00
    books:
      fanatics:
        ml: "nope"
        spread: "2.5"
        total: ""
      draftkings:
        ml: "50/-155"
        spread: ""
        total: ""
`
	doc, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	problems := doc.Validate()
	if len(problems) != 3 {
		t.Fatalf("got %d problems, want 3: %v", len(problems), problems)
	}
	for _, p := range problems {
		if p.GameID != "2026_01_NYJ_TEN" {
			t.Fatalf("problem lost its game id: %+v", p)
		}
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	// A typo'd key would otherwise drop every price beneath it silently.
	_, err := Parse(strings.NewReader("season: 2026\nweek: 1\ngmaes: {}\n"))
	if err == nil {
		t.Fatal("Parse accepted an unknown field, want an error")
	}
}

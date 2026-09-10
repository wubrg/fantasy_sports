package oddspull

import "testing"

// find returns the first outcome whose selection contains sub, for assertions.
func find(t *testing.T, out []Outcome, sub string) Outcome {
	t.Helper()
	for _, o := range out {
		if contains(o.Selection, sub) {
			return o
		}
	}
	t.Fatalf("no outcome with selection containing %q in %d outcomes", sub, len(out))
	return Outcome{}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestParseDKSportscontent(t *testing.T) {
	data := []byte(`{
      "events":[{"id":"E1","name":"NE @ SEA"}],
      "markets":[
        {"id":"M1","eventId":"E1","name":"Moneyline"},
        {"id":"M2","eventId":"E1","name":"Drake Maye Passing Yards"}],
      "selections":[
        {"marketId":"M1","label":"NE Patriots","displayOdds":{"american":"+140"}},
        {"marketId":"M1","label":"SEA Seahawks","displayOdds":{"american":"-166"}},
        {"marketId":"M2","label":"Drake Maye 250+","displayOdds":{"american":"+141"},
         "points":250,"participants":[{"type":"Player","name":"Drake Maye"}]}]}`)
	out, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d outcomes, want 3", len(out))
	}
	ne := find(t, out, "NE Patriots")
	if ne.Price != 140 || ne.Category != "Game" || ne.Market != "Moneyline" || ne.Event != "NE @ SEA" {
		t.Errorf("moneyline row wrong: %+v", ne)
	}
	if ne.MarketID != "M1" {
		t.Errorf("MarketID should let the two ML sides pair, got %q", ne.MarketID)
	}
	maye := find(t, out, "Drake Maye 250+")
	if maye.Price != 141 || maye.Category != "Passing" {
		t.Errorf("prop row wrong: %+v", maye)
	}
	if maye.Line == nil || *maye.Line != 250 {
		t.Errorf("line should be 250, got %v", maye.Line)
	}
	// The player is already in the label — it must not be doubled.
	if maye.Selection != "Drake Maye 250+" {
		t.Errorf("selection should not double the player name: %q", maye.Selection)
	}
}

func TestParseHAR(t *testing.T) {
	// One base64 image body (skipped) and one JSON odds body (kept).
	data := []byte(`{"log":{"entries":[
      {"response":{"content":{"mimeType":"image/png","text":"iVBORw0K","encoding":"base64"}}},
      {"response":{"content":{"text":"{\"offerCategories\":[{\"name\":\"TD Scorer\",\"offers\":[[{\"label\":\"Anytime TD\",\"outcomes\":[{\"label\":\"A.J. Brown\",\"oddsAmerican\":150}]}]]}]}"}}}
    ]}}`)
	out, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d outcomes, want 1 (image body must be skipped)", len(out))
	}
	if o := out[0]; o.Selection != "A.J. Brown" || o.Price != 150 || o.Category != "Touchdown" {
		t.Errorf("HAR outcome wrong: %+v", o)
	}
}

func TestParseGenericOU(t *testing.T) {
	data := []byte(`{"offerCategories":[{"name":"Passing","offers":[[
      {"label":"Maye Pass Yds","outcomes":[
        {"label":"Over","line":249.5,"oddsAmerican":"-115"},
        {"label":"Under","line":249.5,"oddsAmerican":"-105"}]}]]}]}`)
	out, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d, want 2 (over+under)", len(out))
	}
	over := find(t, out, "Over")
	if over.Category != "Passing" || over.Line == nil || *over.Line != 249.5 || over.Price != -115 {
		t.Errorf("over row wrong: %+v", over)
	}
}

func TestParseNoOdds(t *testing.T) {
	if _, err := Parse([]byte(`{"user":{"name":"x"},"total":42}`)); err == nil {
		t.Error("a body with no american odds must be an error, not empty success")
	}
	// A "price" of 42 is a decimal/junk, not an American price, and must not
	// become an outcome.
	if _, err := Parse([]byte(`{"label":"x","price":42}`)); err == nil {
		t.Error("|price| < 100 is not an American price")
	}
}

func TestParseNotJSON(t *testing.T) {
	if _, err := Parse([]byte(`<html>block page</html>`)); err == nil {
		t.Error("non-JSON must error")
	}
}

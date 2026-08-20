package board

import (
	"strings"
	"testing"
)

// testWeek is a three-game week covering both alias cases.
func testWeek() *Doc {
	return &Doc{
		Season: 2026,
		Week:   1,
		Games: map[string]*Game{
			"2026_01_SF_LA": {
				Away: "SF", Home: "LA", Kickoff: "2026-09-13T13:00",
				Books: map[string]Lines{"fanatics": {}},
			},
			"2026_01_GB_MIN": {
				Away: "GB", Home: "MIN", Kickoff: "2026-09-13T16:25",
				Books: map[string]Lines{"fanatics": {ML: "+100/-120"}},
			},
			"2026_01_DAL_NYG": {
				Away: "DAL", Home: "NYG", Kickoff: "2026-09-13T20:20",
				Books: map[string]Lines{"fanatics": {}},
			},
		},
	}
}

func TestCanonicalTeamAliases(t *testing.T) {
	cases := map[string]string{
		"LAR": "LA",
		"lar": "LA",
		"MN":  "MIN",
		"LA":  "LA",
		"MIN": "MIN",
		"SF":  "SF",
		" GB": "GB",
	}
	for in, want := range cases {
		if got := CanonicalTeam(in); got != want {
			t.Errorf("CanonicalTeam(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePastePairsAndSigns(t *testing.T) {
	// Deliberately mixes a signed dog, an unsigned dog and a favourite: books
	// print the "+" but a copy through a spreadsheet can lose it.
	pairs, err := ParsePaste("SF +150, LAR -150, GB 105, MN -125")
	if err != nil {
		t.Fatalf("ParsePaste: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(pairs))
	}
	if pairs[0] != (PastePair{Away: "SF", Home: "LA", AwayPrice: "+150", HomePrice: "-150"}) {
		t.Errorf("pair 0 = %+v", pairs[0])
	}
	// LAR -> LA and MN -> MIN, and the unsigned 105 normalises to +105.
	if pairs[1] != (PastePair{Away: "GB", Home: "MIN", AwayPrice: "+105", HomePrice: "-125"}) {
		t.Errorf("pair 1 = %+v", pairs[1])
	}
}

func TestParsePasteOddEntryCount(t *testing.T) {
	// A dropped entry shifts every pair after it, so the blob must be refused
	// rather than partially applied.
	_, err := ParsePaste("SF +150, LAR -150, GB +105")
	if err == nil {
		t.Fatal("expected an error for an odd number of entries")
	}
	if !strings.Contains(err.Error(), "3 entries") {
		t.Errorf("error should name the count, got: %v", err)
	}
}

func TestParsePasteRejectsBadPrice(t *testing.T) {
	for _, blob := range []string{
		"SF +150, LAR -15",  // -15 is not a real price
		"SF +150, LAR abc",  // not a number
		"SF, LAR -150",      // missing a price
		"SF +150 extra, LA", // malformed entry
	} {
		if _, err := ParsePaste(blob); err == nil {
			t.Errorf("ParsePaste(%q) = nil error, want a rejection", blob)
		}
	}
}

func TestPlanImportPairsAwayHome(t *testing.T) {
	d := testWeek()
	pairs, err := ParsePaste("SF +150, LAR -150, DAL -165, NYG +135")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := d.PlanImport(pairs, "fanatics", "ml")
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2: %+v", len(changes), changes)
	}
	want := map[string]string{
		"2026_01_SF_LA":   "+150/-150",
		"2026_01_DAL_NYG": "-165/+135",
	}
	for _, c := range changes {
		if want[c.GameID] != c.New {
			t.Errorf("%s: got %q, want %q", c.GameID, c.New, want[c.GameID])
		}
	}

	// Nothing is written until ApplyImport runs.
	if got := d.Games["2026_01_SF_LA"].Books["fanatics"].ML; got != "" {
		t.Errorf("PlanImport wrote %q; it must not touch the doc", got)
	}
	if err := d.ApplyImport(changes); err != nil {
		t.Fatalf("ApplyImport: %v", err)
	}
	if got := d.Games["2026_01_SF_LA"].Books["fanatics"].ML; got != "+150/-150" {
		t.Errorf("after apply: %q", got)
	}
}

func TestPlanImportReversedListing(t *testing.T) {
	// A book that lists the home team first is a recognisable shape, not a
	// corrupt one; the prices must follow the teams into away/home order.
	d := testWeek()
	pairs, err := ParsePaste("LAR -150, SF +150")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := d.PlanImport(pairs, "fanatics", "ml")
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	if len(changes) != 1 || changes[0].New != "+150/-150" {
		t.Fatalf("got %+v, want SF/LA stored as +150/-150", changes)
	}
}

func TestPlanImportRejectsUnscheduledMatchup(t *testing.T) {
	// SF does play this week and NYG does play this week, but not each other.
	// Pairing them is the signature of a shifted paste and must not be
	// silently written to whichever game happened to match first.
	d := testWeek()
	pairs, err := ParsePaste("SF +150, NYG -150")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PlanImport(pairs, "fanatics", "ml"); err == nil {
		t.Fatal("expected an error for a matchup that is not on the schedule")
	} else if !strings.Contains(err.Error(), "SF/NYG") {
		t.Errorf("error should name the offending pair, got: %v", err)
	}
}

func TestPlanImportUnaliasedNameFails(t *testing.T) {
	// Guard the alias map from the other direction: if LAR stopped mapping to
	// LA, this blob would stop matching rather than quietly go somewhere else.
	d := testWeek()
	pairs, err := ParsePaste("SF +150, RAM -150")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PlanImport(pairs, "fanatics", "ml"); err == nil {
		t.Fatal("expected an error for an unknown abbreviation")
	}
}

func TestPlanImportSkipsNoOps(t *testing.T) {
	d := testWeek() // GB/MIN already holds +100/-120
	pairs, err := ParsePaste("GB +100, MN -120")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := d.PlanImport(pairs, "fanatics", "ml")
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("re-pasting the same prices should change nothing, got %+v", changes)
	}
}

func TestPlanImportRejectsDuplicateGame(t *testing.T) {
	d := testWeek()
	pairs, err := ParsePaste("SF +150, LAR -150, SF +160, LA -180")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PlanImport(pairs, "fanatics", "ml"); err == nil {
		t.Fatal("expected an error when one game appears twice")
	}
}

func TestPlanImportRejectsConsensusAndOtherMarkets(t *testing.T) {
	d := testWeek()
	pairs, _ := ParsePaste("SF +150, LAR -150")
	if _, err := d.PlanImport(pairs, Consensus, "ml"); err == nil {
		t.Error("consensus must not be importable")
	}
	if _, err := d.PlanImport(pairs, "fanatics", "spread"); err == nil {
		t.Error("spread must not be importable from a flat blob")
	}
	if _, err := d.PlanImport(pairs, "nosuchbook", "ml"); err == nil {
		t.Error("an unknown book must be rejected")
	}
}

func TestSetPrice(t *testing.T) {
	d := testWeek()

	if err := d.SetPrice("2026_01_SF_LA", "fanatics", "ml", "+150/-180"); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	if got := d.Games["2026_01_SF_LA"].Books["fanatics"].ML; got != "+150/-180" {
		t.Errorf("ml = %q", got)
	}

	// An empty value is an erase, not an error.
	if err := d.SetPrice("2026_01_SF_LA", "fanatics", "ml", ""); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if got := d.Games["2026_01_SF_LA"].Books["fanatics"].ML; got != "" {
		t.Errorf("erase left %q", got)
	}

	if err := d.SetPrice("2026_01_SF_LA", "fanatics", "spread", "-2.5 -110/-110"); err != nil {
		t.Fatalf("spread: %v", err)
	}
	if err := d.SetPrice("2026_01_SF_LA", "fanatics", "total", "44.5 -110/-110"); err != nil {
		t.Fatalf("total: %v", err)
	}

	for _, bad := range []struct{ market, value string }{
		{"ml", "150"},             // no second side
		{"ml", "+150/-4"},         // -4 is not a price
		{"spread", "-110/-110"},   // no line
		{"total", "44.5"},         // no prices
		{"nonsense", "+150/-150"}, // unknown market
		{"ml", "+150/-150/-150"},  // three sides
	} {
		if err := d.SetPrice("2026_01_SF_LA", "fanatics", bad.market, bad.value); err == nil {
			t.Errorf("SetPrice(%s, %q) = nil error, want a rejection", bad.market, bad.value)
		}
	}

	if err := d.SetPrice("2026_01_NOPE", "fanatics", "ml", "+150/-150"); err == nil {
		t.Error("unknown game must be rejected")
	}
	if err := d.SetPrice("2026_01_SF_LA", Consensus, "ml", "+150/-150"); err == nil {
		t.Error("consensus must not be editable")
	}
}

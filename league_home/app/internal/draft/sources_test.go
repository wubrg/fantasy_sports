package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testIndex() *PlayerIndex {
	return BuildPlayerIndex(map[string]PlayerInfo{
		"1": {Name: "A.J. Brown", Position: "WR", Team: "PHI"},
		"2": {Name: "Marvin Harrison Jr.", Position: "WR", Team: "ARI"},
		"3": {Name: "Kenneth Walker III", Position: "RB", Team: "KC"},
		"4": {Name: "Kenneth Walker", Position: "WR", Team: "FA"},
		"5": {Name: "Ja'Marr Chase", Position: "WR", Team: "CIN"},
		"6": {Name: "Rams", Position: "DEF", Team: "LAR"},
	})
}

// TestResolveHandlesRealWorldNameNoise covers the hazards that actually
// showed up in Ciely's sheet: punctuation, suffixes, and defenses that
// carry no usable name in Sleeper's dictionary.
func TestResolveHandlesRealWorldNameNoise(t *testing.T) {
	rows := []SourceRow{
		{Player: "AJ Brown", Position: "WR", Team: "PHI"},
		{Player: "A.J. Brown", Position: "WR", Team: "PHI"},
		{Player: "Marvin Harrison", Position: "WR", Team: "ARI"},
		{Player: "JaMarr Chase", Position: "WR", Team: "CIN"},
		{Player: "Los Angeles Rams", Position: "DST", Team: "LAR"},
	}
	if bad := testIndex().Resolve(rows); len(bad) != 0 {
		t.Fatalf("expected all to resolve, got %+v", bad)
	}
	want := []string{"1", "1", "2", "5", "6"}
	for i, w := range want {
		if rows[i].PlayerID != w {
			t.Errorf("%q resolved to %q, want %q", rows[i].Player, rows[i].PlayerID, w)
		}
	}
}

// TestResolveDisambiguatesSameName is the Kenneth Walker case — there are
// two, and only position tells them apart.
func TestResolveDisambiguatesSameName(t *testing.T) {
	rows := []SourceRow{
		{Player: "Kenneth Walker", Position: "RB", Team: "KC"},
		{Player: "Kenneth Walker", Position: "WR", Team: "FA"},
	}
	if bad := testIndex().Resolve(rows); len(bad) != 0 {
		t.Fatalf("expected both to resolve, got %+v", bad)
	}
	if rows[0].PlayerID != "3" || rows[1].PlayerID != "4" {
		t.Errorf("got %q and %q, want 3 and 4", rows[0].PlayerID, rows[1].PlayerID)
	}
}

// TestResolveReportsRatherThanDrops is the whole point of returning
// unmatched rows: a name this misses is a player silently absent from the
// board, which nobody notices until he is nominated.
func TestResolveReportsRatherThanDrops(t *testing.T) {
	rows := []SourceRow{{Player: "Nobody At All", Position: "WR", Team: "SEA", AuctionValue: 40}}
	bad := testIndex().Resolve(rows)
	if len(bad) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(bad))
	}
	if bad[0].Row.AuctionValue != 40 || bad[0].Reason == "" {
		t.Errorf("unmatched row should keep its value and carry a reason: %+v", bad[0])
	}
	if rows[0].PlayerID != "" {
		t.Error("an unmatched row must not be assigned an ID")
	}
}

func TestAliasesResolveNicknames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.csv")
	os.WriteFile(path, []byte("source_name,player_id,note\nHollywood Brown,1,nickname\n"), 0o600)

	aliases, err := LoadAliases(path)
	if err != nil {
		t.Fatal(err)
	}
	idx := BuildPlayerIndexWithAliases(map[string]PlayerInfo{
		"1": {Name: "Marquise Brown", Position: "WR", Team: "PHI"},
	}, aliases)

	rows := []SourceRow{{Player: "Hollywood Brown", Position: "WR", Team: "PHI"}}
	if bad := idx.Resolve(rows); len(bad) != 0 {
		t.Fatalf("alias should resolve, got %+v", bad)
	}
	if rows[0].PlayerID != "1" {
		t.Errorf("PlayerID = %q, want 1", rows[0].PlayerID)
	}
}

func TestLoadAliasesMissingFileIsNotAnError(t *testing.T) {
	got, err := LoadAliases(filepath.Join(t.TempDir(), "none.csv"))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want an empty set and no error", got, err)
	}
}

func TestParseSourceCSVTolerantOfHeaders(t *testing.T) {
	// Different extractors label the same columns differently, and this
	// file is hand-edited between drafts.
	in := "Name,POS,TM,Rank,Value\nJa'Marr Chase,WR,CIN,1,$47.20\n"
	rows, err := ParseSourceCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Player != "Ja'Marr Chase" || r.Position != "WR" || r.PosRank != 1 {
		t.Errorf("unexpected row: %+v", r)
	}
	// The dollar sign must not defeat the parse.
	if r.AuctionValue != 47.20 {
		t.Errorf("AuctionValue = %v, want 47.20", r.AuctionValue)
	}
}

func TestParseSourceCSVRejectsMissingPlayerColumn(t *testing.T) {
	if _, err := ParseSourceCSV(strings.NewReader("pos,value\nWR,10\n")); err == nil {
		t.Error("expected an error when no player/name column exists")
	}
}

// TestRescaleConvertsBetweenPools guards the trap of blending a 10-team
// sheet's dollars with a 12-team league's without adjusting.
func TestRescaleConvertsBetweenPools(t *testing.T) {
	rows := []SourceRow{{Player: "x", AuctionValue: 50}}
	from := Basis{Teams: 10, Budget: 200}
	factor := Rescale(rows, from, HitOrMiss())
	if factor != 1.2 {
		t.Errorf("factor = %v, want 1.2", factor)
	}
	if rows[0].AuctionValue != 60 {
		t.Errorf("value = %v, want 60", rows[0].AuctionValue)
	}
}

func TestRescaleIsANoOpForMatchingPools(t *testing.T) {
	rows := []SourceRow{{Player: "x", AuctionValue: 47.2}}
	if factor := Rescale(rows, HitOrMiss(), HitOrMiss()); factor != 1 {
		t.Errorf("factor = %v, want 1", factor)
	}
	if rows[0].AuctionValue != 47.2 {
		t.Errorf("value changed to %v", rows[0].AuctionValue)
	}
}

// TestBasisMatchesDetectsScoringDifference is why every source declares a
// basis: Ciely's sheet lines up with the league on everything except
// interceptions, and that difference has to be visible.
func TestBasisMatchesDetectsScoringDifference(t *testing.T) {
	ciely := HitOrMiss()
	ciely.Interception = -2

	// Interceptions don't affect auction-pool comparability, so the
	// pool-shape check still matches...
	if !HitOrMiss().Matches(ciely) {
		t.Error("pool shape is identical and should match")
	}
	// ...but the scoring difference must not be lost.
	if HitOrMiss().Interception == ciely.Interception {
		t.Error("the interception difference should be visible on the basis")
	}

	tenTeam := HitOrMiss()
	tenTeam.Teams = 10
	if HitOrMiss().Matches(tenTeam) {
		t.Error("a 10-team basis must not match a 12-team one")
	}
}

// TestECRStateTreatsBothArrowsAsContested is the case that would be lost by
// treating the two flags as one axis: Justin Jefferson carries both in the
// live sheet, and "some say more, some say less" is a volatility signal, not
// a cancellation back to consensus.
func TestECRStateTreatsBothArrowsAsContested(t *testing.T) {
	cases := []struct {
		up, down bool
		want     ECRState
	}{
		{false, false, ECRConsensus},
		{true, false, ECRUpside},
		{false, true, ECRDownside},
		{true, true, ECRContested},
	}
	for _, tc := range cases {
		got := SourceRow{ECRUp: tc.up, ECRDown: tc.down}.ECR()
		if got != tc.want {
			t.Errorf("ECR(up=%v, down=%v) = %q, want %q", tc.up, tc.down, got, tc.want)
		}
	}
}

// TestParseSourceCSVReadsSubvertadownColumns covers the extra quantities the
// Subvertadown sheets carry beyond a plain value: market AAV, positional
// scarcity, the baseline label, and the two ECR flags.
func TestParseSourceCSVReadsSubvertadownColumns(t *testing.T) {
	in := "source,baseline,position,pos_rank,player,team,bye,aav,ps_pct,value,ecr_up,ecr_down\n" +
		"subvertadown,beerplus,WR,6,Justin Jefferson,MIN,6,47,67,44,1,1\n" +
		"subvertadown,beerplus,RB,1,Jahmyr Gibbs,DET,8,68,93,71,0,0\n"
	rows, err := ParseSourceCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	jj := rows[0]
	if jj.Baseline != "beerplus" {
		t.Errorf("Baseline = %q", jj.Baseline)
	}
	if jj.AAV != 47 || jj.AuctionValue != 44 || jj.ScarcityPct != 67 {
		t.Errorf("aav=%v value=%v ps=%v, want 47/44/67", jj.AAV, jj.AuctionValue, jj.ScarcityPct)
	}
	if jj.ECR() != ECRContested {
		t.Errorf("ECR() = %q, want contested", jj.ECR())
	}
	if rows[1].ECR() != ECRConsensus {
		t.Errorf("Gibbs ECR() = %q, want consensus", rows[1].ECR())
	}
}

// TestParseSourceCSVReadsSharpDivergence covers the FantasyPros sharp-expert
// columns: the top-10/top-20 rank moves parse (including a negative), a blank
// reads as no signal, and SharpDelta returns the larger-magnitude signed move.
func TestParseSourceCSVReadsSharpDivergence(t *testing.T) {
	in := "source,baseline,position,pos_rank,player,rank_vs_top10,rank_vs_top20\n" +
		"fantasypros,consensus,RB,1,Bijan Robinson,-6,-4\n" +
		"fantasypros,consensus,RB,2,Jahmyr Gibbs,7,3\n" +
		"fantasypros,consensus,WR,1,Ja'Marr Chase,,\n"
	rows, err := ParseSourceCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].RankVsTop10 != -6 || rows[0].SharpDelta() != -6 {
		t.Errorf("Bijan top10=%d delta=%d, want -6/-6", rows[0].RankVsTop10, rows[0].SharpDelta())
	}
	if rows[1].SharpDelta() != 7 {
		t.Errorf("Gibbs delta=%d, want 7", rows[1].SharpDelta())
	}
	if rows[2].RankVsTop10 != 0 || rows[2].SharpDelta() != 0 {
		t.Errorf("Chase blank should read 0, got top10=%d delta=%d", rows[2].RankVsTop10, rows[2].SharpDelta())
	}
}

// TestRescaleLeavesAAVAlone — AAV is observed market data from real drafts,
// not a model output apportioning a pool, so pool rescaling must not touch it.
func TestRescaleLeavesAAVAlone(t *testing.T) {
	rows := []SourceRow{{Player: "x", AuctionValue: 50, AAV: 50}}
	from := Basis{Teams: 10, Budget: 200}
	Rescale(rows, from, HitOrMiss())
	if rows[0].AuctionValue != 60 {
		t.Errorf("model value = %v, want 60", rows[0].AuctionValue)
	}
	if rows[0].AAV != 50 {
		t.Errorf("AAV = %v, want it untouched at 50", rows[0].AAV)
	}
}

func TestHitOrMissPool(t *testing.T) {
	if got := HitOrMiss().Pool(); got != 2400 {
		t.Errorf("Pool() = %d, want 2400", got)
	}
}

// TestSuffixStrippingDoesNotEatASurname — the suffix is a word, not the
// letters a name happens to end with. "Kyle Monangai II" normalizes to
// kylemonangaiii, which matched the longest suffix checked and stripped to
// "kylemonanga" — so he could not be resolved or leaned on by his full name.
func TestSuffixStrippingDoesNotEatASurname(t *testing.T) {
	idx := BuildPlayerIndex(map[string]PlayerInfo{
		"1": {Name: "Kyle Monangai", Position: "RB", Team: "CHI"},
		"2": {Name: "Rasheen Ali", Position: "RB", Team: "BAL"},
		"3": {Name: "Mike Gesicki", Position: "TE", Team: "CIN"},
	})
	rows := []SourceRow{
		{Player: "Kyle Monangai II", Position: "RB", Team: "CHI"},
		{Player: "Rasheen Ali II", Position: "RB", Team: "BAL"},
		{Player: "Mike Gesicki II", Position: "TE", Team: "CIN"},
	}
	if bad := idx.Resolve(rows); len(bad) != 0 {
		t.Fatalf("expected all to resolve, got %+v", bad)
	}
	for i, want := range []string{"1", "2", "3"} {
		if rows[i].PlayerID != want {
			t.Errorf("%q resolved to %q, want %q", rows[i].Player, rows[i].PlayerID, want)
		}
	}
}

// TestSuffixStrippingLeavesLookalikeSurnamesAlone — a surname that merely
// ends in a suffix's letters is not carrying a suffix. Stripping one would
// invent a match, which is worse than missing one.
func TestSuffixStrippingLeavesLookalikeSurnamesAlone(t *testing.T) {
	for _, name := range []string{"Ricky Popov", "Auden Asiasi", "Kyle Monangai", "Rasheen Ali"} {
		if got := stemName(name); got != normalizeName(name) {
			t.Errorf("stemName(%q) = %q, want it untouched at %q", name, got, normalizeName(name))
		}
	}
}

// TestSuffixStrippingStillDropsRealSuffixes — the behaviour this exists for
// has to survive the fix. Sources disagree about whether to print them.
func TestSuffixStrippingStillDropsRealSuffixes(t *testing.T) {
	for raw, want := range map[string]string{
		"Kenneth Walker III":  "kennethwalker",
		"Marvin Harrison Jr.": "marvinharrison",
		"Michael Pittman Jr":  "michaelpittman",
		"Odell Beckham Jr.":   "odellbeckham",
		"Brian Robinson II":   "brianrobinson",
	} {
		if got := stemName(raw); got != want {
			t.Errorf("stemName(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestSourceSchemaRejectsAMissingColumn — a renamed vendor column used to
// read as a column of zeros, and a board built on zeros still renders. A
// crash naming the column is the cheaper failure by a wide margin.
func TestSourceSchemaRejectsAMissingColumn(t *testing.T) {
	// Ciely's header with auction_value dropped.
	in := "source,position,pos_rank,player,team,league_points\n" +
		"ciely,WR,1,Ja'Marr Chase,CIN,300\n"
	_, err := ParseSourceCSVAs(strings.NewReader(in), CielyColumns)
	if err == nil {
		t.Fatal("expected an error for the missing value column")
	}
	// It has to name the column, or you are left diffing headers by eye.
	if !strings.Contains(err.Error(), "auction_value") {
		t.Errorf("error should name the missing column: %v", err)
	}
	// And name what it did find, so a rename is obvious.
	if !strings.Contains(err.Error(), "league_points") {
		t.Errorf("error should show the header it read: %v", err)
	}
}

// TestSourceSchemaAcceptsAnyKnownSpelling — the required check must not
// reject a header pick() would happily have read, or it breaks setups that
// work.
func TestSourceSchemaAcceptsAnyKnownSpelling(t *testing.T) {
	in := "player,pos,points,value\nJa'Marr Chase,WR,300,47\n"
	rows, err := ParseSourceCSVAs(strings.NewReader(in), CielyColumns)
	if err != nil {
		t.Fatalf("alternate spellings must be accepted: %v", err)
	}
	if len(rows) != 1 || rows[0].AuctionValue != 47 || rows[0].Points != 300 {
		t.Errorf("unexpected row: %+v", rows)
	}
}

// TestSourceSchemaLeavesOptionalColumnsOptional — losing ps_pct or an ECR
// flag costs a signal, not a number that silently reads as zero, so it must
// not stop the file loading.
func TestSourceSchemaLeavesOptionalColumnsOptional(t *testing.T) {
	in := "source,baseline,position,player,team,aav,value\n" +
		"subvertadown,beerplus,WR,Justin Jefferson,MIN,47,44\n"
	rows, err := ParseSourceCSVAs(strings.NewReader(in), SubvertadownColumns)
	if err != nil {
		t.Fatalf("optional columns must stay optional: %v", err)
	}
	if len(rows) != 1 || rows[0].ScarcityPct != 0 || rows[0].ECRUp {
		t.Errorf("unexpected row: %+v", rows)
	}
}

// TestTheRealHeadersSatisfyTheirSchemas — the live extractors' output must
// pass, or this check is a tripwire across the workflow rather than a guard.
func TestTheRealHeadersSatisfyTheirSchemas(t *testing.T) {
	ciely := "source,position,pos_rank,player,team,bye,ciely_points,league_points,points_delta," +
		"auction_value,pass_yards,pass_td,interceptions,rush_yards,rush_td,targets,receptions,recv_yards,recv_td\n"
	if _, err := ParseSourceCSVAs(strings.NewReader(ciely+"ciely,WR,1,X,CIN,6,1,2,3,44,0,0,0,0,0,0,0,0,0\n"), CielyColumns); err != nil {
		t.Errorf("the live ciely header must pass: %v", err)
	}
	sv := "source,baseline,position,pos_rank,player,team,bye,aav,ps_pct,value,ecr_up,ecr_down\n"
	if _, err := ParseSourceCSVAs(strings.NewReader(sv+"subvertadown,beerplus,WR,1,X,CIN,6,47,67,44,0,0\n"), SubvertadownColumns); err != nil {
		t.Errorf("the live subvertadown header must pass: %v", err)
	}
}

// TestSchemaAcceptsEverySpellingPickDoes — the schema promises it can never
// reject a header the parser would have read. Subvertadown's value column
// required only two of the four spellings pick() takes.
func TestSchemaAcceptsEverySpellingPickDoes(t *testing.T) {
	for _, value := range []string{"value", "auction_value", "auc$", "auction"} {
		in := "player,position,baseline,aav," + value + "\nX,WR,beerplus,47,44\n"
		if _, err := ParseSourceCSVAs(strings.NewReader(in), SubvertadownColumns); err != nil {
			t.Errorf("header with %q rejected: %v", value, err)
		}
	}
}

// TestAnEmptySourceFileIsAnError — a header-only or empty file is what a
// broken extractor leaves behind, and it used to sail through as "0 rows".
// A board with no prices renders exactly like a board.
func TestAnEmptySourceFileIsAnError(t *testing.T) {
	if _, err := ParseSourceCSVAs(strings.NewReader(""), CielyColumns); err == nil {
		t.Error("an empty file should not pass a schema")
	}
	// Header only: the columns are checked, so a truncated file that also
	// lost a column still fails on the column.
	in := "source,position,player,team\n"
	if _, err := ParseSourceCSVAs(strings.NewReader(in), CielyColumns); err == nil {
		t.Error("a header-only file missing required columns should fail")
	}
	// And with no schema at all, anything still goes.
	if _, err := ParseSourceCSV(strings.NewReader("")); err != nil {
		t.Errorf("no schema means no opinion: %v", err)
	}
}

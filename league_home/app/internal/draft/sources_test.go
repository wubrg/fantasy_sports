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

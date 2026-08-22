package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"leaguehome/internal/draft"
)

// leanServer is a board with a writable config directory holding a starting
// mine.csv, so saves have a real file to read back and modify.
func leanServer(t *testing.T, mine string) (*server, string) {
	t.Helper()
	return leanServerFile(t, "mine.csv", mine)
}

// leanServerFile is leanServer over a named file, so a test that needs the
// grouped YAML form — the only one with favorites — can have it.
func leanServerFile(t *testing.T, name, mine string) (*server, string) {
	t.Helper()
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	static := testStatic()
	// Production loads staticData.leans from this same file at startup, so a
	// fixture that leaves it empty tests a state the server cannot be in. The
	// set metadata beside it is loaded the same way and for the same reason:
	// the leans page reports which file an edit lands in, and a fixture with
	// no path would test a server that cannot say.
	sets := []draft.LeanSet{}
	loaded, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, s, err := loadLeanSets(cfg, []string{strings.TrimSuffix(name, filepath.Ext(name))}); err == nil {
		sets = s
	}
	static.leans = loaded
	static.leanSetInfo = sets
	static.leanSets = setNames(sets)
	static.minePath = writableSetPath(cfg, sets)

	srv, err := newServer(static, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return srv, path
}

// TestEditsOverrideTheLoadedSets — the point of the overlay.
// leanEdit builds one board edit the way leanEdits.set would, so a test can
// say "he was set to must" without assembling pointers inline.
func leanEdit(player string, lean draft.Lean) boardEdits {
	return boardEdits{draft.NormalizeName(player): {player: player, lean: &lean}}
}

// favEdit builds one favorite tag with no read attached.
func favEdit(player string, fav bool) boardEdits {
	return boardEdits{draft.NormalizeName(player): {player: player, favorite: &fav}}
}

func TestEditsOverrideTheLoadedSets(t *testing.T) {
	s := testStatic()
	s.leans = draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs"): {Player: "Jahmyr Gibbs", Lean: draft.LeanDown},
	}
	edits := leanEdit("Jahmyr Gibbs", draft.LeanMust)

	got := s.effectiveLeans(edits)[draft.NormalizeName("Jahmyr Gibbs")]
	if got.Lean != draft.LeanMust {
		t.Errorf("lean = %q, want the edit to beat the loaded set", got.Lean)
	}
	// And the shared map is untouched, which is what lets staticData stay
	// immutable while the board changes underneath it.
	if s.leans[draft.NormalizeName("Jahmyr Gibbs")].Lean != draft.LeanDown {
		t.Error("the edit leaked into the loaded set")
	}
}

// TestClearingARemovesTheReadEntirely — a blank lean has to delete the row,
// not sit there as an unknown value. WalkAway treats an unrecognised lean as
// "no opinion" but still reports it as a lean, which would show a flag for a
// read you just cleared.
func TestClearingARemovesTheReadEntirely(t *testing.T) {
	s := testStatic()
	key := draft.NormalizeName("Jahmyr Gibbs")
	s.leans = draft.Leans{key: {Player: "Jahmyr Gibbs", Lean: draft.LeanMust}}

	got := s.effectiveLeans(leanEdit("Jahmyr Gibbs", ""))
	if _, still := got[key]; still {
		t.Error("a cleared read is still on the board")
	}
}

func TestNextLeanCycles(t *testing.T) {
	want := []draft.Lean{draft.LeanUp, draft.LeanDown, draft.LeanDND, "", draft.LeanMust}
	got := draft.LeanMust
	for i, expect := range want {
		got = nextLean(got)
		if got != expect {
			t.Fatalf("step %d: %q, want %q", i, got, expect)
		}
	}
}

// TestSavingKeepsHandEditsMadeSinceStartup is why the save is a
// read-modify-write.
//
// mine.csv is a file you edit yourself and may have open. A save that dumped
// server memory over it would silently drop anything added since the board
// came up — and the whole reason to save is that reads survive.
func TestSavingKeepsHandEditsMadeSinceStartup(t *testing.T) {
	srv, path := leanServer(t, "player,lean,cap,note\nOld Guy,up,,from before\n")

	// Somebody edits the file directly while the board is running.
	extra := "player,lean,cap,note\nOld Guy,up,,from before\nHand Added,dnd,,typed in the editor\n"
	if err := os.WriteFile(path, []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}

	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Old Guy", "Hand Added", "Jahmyr Gibbs"} {
		if _, ok := back[draft.NormalizeName(name)]; !ok {
			t.Errorf("%s is not in the saved file", name)
		}
	}
}

// TestSavingKeepsCapAndNote — the board cannot set either, so it must not
// erase them. A must-have with a hand-written cap that came back capless
// would quietly raise your own ceiling.
func TestSavingKeepsCapAndNote(t *testing.T) {
	srv, path := leanServer(t,
		"player,lean,cap,note\nJahmyr Gibbs,up,45,\"only to forty-five\"\n")

	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}
	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	got := back[draft.NormalizeName("Jahmyr Gibbs")]
	if got.Lean != draft.LeanMust {
		t.Errorf("lean = %q, want the edit applied", got.Lean)
	}
	if got.Cap != 45 || !strings.Contains(got.Note, "forty-five") {
		t.Errorf("cap %d note %q — the file's own values were lost", got.Cap, got.Note)
	}
}

// TestClearedReadLeavesTheFile — undoing a read has to remove the row, or the
// board and the file disagree the next time it starts.
func TestClearedReadLeavesTheFile(t *testing.T) {
	srv, path := leanServer(t, "player,lean,cap,note\nJahmyr Gibbs,must,,\n")

	srv.leans.set("Jahmyr Gibbs", "")
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}
	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, still := back[draft.NormalizeName("Jahmyr Gibbs")]; still {
		t.Error("the cleared read is still in the file")
	}
}

// TestSavedFileIsNotMarkedGenerated is the load-bearing one.
//
// If mine.csv came back carrying the "# generated by" marker, the next
// `leans -generate` would consider it its own output and be free to
// overwrite every hand-written read in it. The guard that prevents that
// reads the first line, so what this file starts with matters.
func TestSavedFileIsNotMarkedGenerated(t *testing.T) {
	srv, path := leanServer(t, "player,lean,cap,note\n")

	srv.leans.set("Jahmyr Gibbs", draft.LeanUp)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "#") {
		t.Errorf("saved file opens with a comment, which is how generated sets are recognised:\n%s", body)
	}
	if !strings.HasPrefix(string(body), "player,lean,cap,note") {
		t.Errorf("saved file does not start with the header:\n%s", body)
	}
}

// TestAnEditReachesTheMustHaveLine is the regression.
//
// Build feeds leans to two consumers: BuildSignals, which flags the row, and
// Assemble, which totals what the must-haves commit you to. The first cut
// handed the overlay to one and the loaded set to the other, so a read set
// at the board lit up MUST on the player while the budget line beneath it
// carried on as though he did not exist.
func TestAnEditReachesTheMustHaveLine(t *testing.T) {
	srv, _ := leanServer(t, "player,lean,cap,note\n")
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.rebuild(); err != nil {
		t.Fatal(err)
	}
	snap := srv.snapshot()

	flagged := false
	for _, p := range snap.Players {
		if p.Name == "Jahmyr Gibbs" && p.Lean.Lean == draft.LeanMust {
			flagged = true
		}
	}
	if !flagged {
		t.Error("the row does not carry the read")
	}
	named := false
	for _, pl := range snap.MustHaves.Players {
		if pl.Player == "Jahmyr Gibbs" {
			named = true
		}
	}
	if !named {
		t.Errorf("the must-have line does not know about him: %+v", snap.MustHaves.Players)
	}
}

// TestCyclingBackDoesNotEatTheCap is the defect that mattered most.
//
// The overlay replaced the whole record instead of changing the read, so a
// board click dropped a hand-written cap. It could not be avoided by care:
// every route around the cycle passes through the clear, so four clicks
// ending where they began turned a $20 hard cap into no cap — silently
// doubling the ceiling on a must-have with nothing on screen to say so.
//
// Both halves are checked, because fixing the in-memory overlay alone left
// the file wrong: the clear deletes the row, so by the time the cycle came
// back to a read there was nothing on disk left to preserve.
func TestCyclingBackDoesNotEatTheCap(t *testing.T) {
	srv, path := leanServer(t,
		"player,lean,cap,note\nJahmyr Gibbs,must,20,\"hard cap twenty\"\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	for _, lean := range []draft.Lean{draft.LeanUp, draft.LeanDown, draft.LeanDND, "", draft.LeanMust} {
		srv.leans.set("Jahmyr Gibbs", lean)
		if err := srv.saveLeans(); err != nil {
			t.Fatal(err)
		}
	}

	// On the board.
	live := srv.static.effectiveLeans(srv.leans.snapshot())[key]
	if live.Lean != draft.LeanMust {
		t.Errorf("lean = %q, want must", live.Lean)
	}
	if live.Cap != 20 {
		t.Errorf("cap = %d after a full cycle, want the hand-written 20", live.Cap)
	}

	// And on disk.
	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; got.Cap != 20 || !strings.Contains(got.Note, "hard cap") {
		t.Errorf("saved as cap %d note %q, want the file's own values kept", got.Cap, got.Note)
	}
}

// TestAnEditKeepsTheOtherSetsOpinions — the overlay carried no Others, so
// setting a read wiped the "vs menton" split flag. Worst on a player you set
// against a set that says the opposite, which is exactly when you want it.
func TestAnEditKeepsTheOtherSetsOpinions(t *testing.T) {
	s := testStatic()
	key := draft.NormalizeName("Jahmyr Gibbs")
	s.leans = draft.Leans{key: {
		Player: "Jahmyr Gibbs", Lean: draft.LeanMust, Source: "mine",
		Others: []draft.PlayerLean{{Lean: draft.LeanDown, Source: "menton"}},
	}}

	got := s.effectiveLeans(leanEdit("Jahmyr Gibbs", draft.LeanMust))[key]

	if !got.Contested() {
		t.Error("the split flag went missing when the read was set from the board")
	}
	if got.Source != "board" {
		t.Errorf("source = %q, want the edit to own the read", got.Source)
	}
}

// TestClearingAnInheritedReadSticks — deleting a row that was never in
// mine.csv changes nothing, so a read owned by a generated set came straight
// back on restart. A none row is how the file says "no opinion" loudly
// enough to outrank the set that has one.
func TestClearingAnInheritedReadSticks(t *testing.T) {
	srv, path := leanServer(t, "player,lean,cap,note\n")
	key := draft.NormalizeName("Jahmyr Gibbs")
	// He carries a read from somewhere other than mine.csv.
	srv.static.leans = draft.Leans{key: {
		Player: "Jahmyr Gibbs", Lean: draft.LeanDown, Source: "menton",
	}}

	srv.leans.set("Jahmyr Gibbs", "")
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := back[key]
	if !ok {
		t.Fatal("nothing was written, so the generated set's read returns on restart")
	}
	if got.Lean != draft.LeanNone {
		t.Errorf("wrote %q, want an explicit none to silence the other set", got.Lean)
	}
}

// TestSavingReachesTheSetTheBoardActuallyReads — the board saves to the mine
// set, and the loader picks the format. Writing one format while the loader
// prefers another loses the read without a word: the click appears to work,
// the file on disk is real, and the board comes back without it.
func TestSavingReachesTheSetTheBoardActuallyReads(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A mine set in the format the tool now writes.
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("up:\n  - Old Guy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	static := testStatic()
	set, err := draft.LoadLeanSet(cfg, "mine")
	if err != nil {
		t.Fatal(err)
	}
	static.leans = set.Leans

	srv, err := newServer(static, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	// Re-read the way the board does at startup, not the way the save wrote.
	back, err := draft.LoadLeanSet(cfg, "mine")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Old Guy", "Jahmyr Gibbs"} {
		if _, ok := back.Leans[draft.NormalizeName(name)]; !ok {
			t.Errorf("%s did not survive the save; the board reads %s", name, back.Path)
		}
	}
}

// leanServerAt builds a server over a config directory as production does:
// the reads and the writable path both come from whatever loadLeanSets
// actually resolved, rather than from a filename the test picked.
func leanServerAt(t *testing.T, cfg string) *server {
	t.Helper()
	merged, sets, err := loadLeanSets(cfg, []string{"mine"})
	if err != nil {
		t.Fatal(err)
	}
	static := testStatic()
	static.leans = merged
	static.leanSets = setNames(sets)
	static.minePath = writableSetPath(cfg, sets)
	srv, err := newServer(static, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestSavingAnUnmigratedConfigWritesTheFileItRead — "mine" can resolve to
// my-guys.csv outside the leans directory. A writer that works the path out
// from the config directory instead of using the one startup read would put
// the read somewhere nothing loads, and lose every read already in the
// legacy file the next time the board starts.
func TestSavingAnUnmigratedConfigWritesTheFileItRead(t *testing.T) {
	cfg := t.TempDir()
	legacy := filepath.Join(cfg, myGuysFile)
	if err := os.WriteFile(legacy,
		[]byte("player,lean,cap,note\nBrock Bowers,must,30,keep him\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := leanServerAt(t, cfg)
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatalf("saving an unmigrated config must not fail: %v", err)
	}

	// Nothing may appear in leans/ to shadow the file actually in use.
	if _, err := os.Stat(filepath.Join(cfg, "leans", "mine.yaml")); err == nil {
		t.Error("the save invented leans/mine.yaml, which startup would then prefer")
	}
	back, _, err := loadLeanSets(cfg, []string{"mine"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Brock Bowers", "Jahmyr Gibbs"} {
		if _, ok := back[draft.NormalizeName(name)]; !ok {
			t.Errorf("%s is not there after a restart", name)
		}
	}
	if pl := back[draft.NormalizeName("Brock Bowers")]; pl.Cap != 30 {
		t.Errorf("the hand-written cap did not survive: %+v", pl)
	}
}

// TestSavingDoesNotEatASymlinkedSet — a set is often a symlink into a notes
// vault. Renaming over the link replaces it with a real file, and every
// later edit in the vault stops reaching the board without a word.
func TestSavingDoesNotEatASymlinkedSet(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(t.TempDir(), "mine.yaml")
	if err := os.WriteFile(vault, []byte("up:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "mine.yaml")
	if err := os.Symlink(vault, link); err != nil {
		t.Fatal(err)
	}

	srv := leanServerAt(t, cfg)
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the save replaced the symlink with a real file")
	}
	// The write must have landed in the vault, where the next edit happens.
	body, err := os.ReadFile(vault)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Jahmyr Gibbs") {
		t.Errorf("the read did not reach the vault file:\n%s", body)
	}
}

// TestSavingKeepsTheUndecidedList — undecided names live only in the file,
// not in Leans, so a read-modify-write that forgets them deletes part of
// the file every time you click a lean.
func TestSavingKeepsTheUndecidedList(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("up:\n  - Brock Bowers\nundecided:\n  - Ja'Marr Chase\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := leanServerAt(t, cfg)
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	set, err := draft.LoadLeanSet(cfg, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Undecided) != 1 || set.Undecided[0] != "Ja'Marr Chase" {
		t.Errorf("the undecided list did not survive the save: %v", set.Undecided)
	}
}

// TestDecidingAPlayerTakesHimOutOfUndecided — he cannot be in both, and a
// file saying so does not load at all.
func TestDecidingAPlayerTakesHimOutOfUndecided(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("undecided:\n  - Ja'Marr Chase\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := leanServerAt(t, cfg)
	srv.leans.set("Ja'Marr Chase", draft.LeanMust)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	set, err := draft.LoadLeanSet(cfg, "mine")
	if err != nil {
		t.Fatalf("the saved file must still load: %v", err)
	}
	if len(set.Undecided) != 0 {
		t.Errorf("still listed as undecided after being ruled on: %v", set.Undecided)
	}
	if pl := set.Leans[draft.NormalizeName("Ja'Marr Chase")]; pl.Lean != draft.LeanMust {
		t.Errorf("the read did not land: %+v", pl)
	}
}

// TestReloadPicksUpAFileEditedAfterStartup — lean sets are read once at
// startup, so a read added in a notes vault used to need a restart to reach
// the board. This is the whole point of the reload control.
func TestReloadPicksUpAFileEditedAfterStartup(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mine.yaml")
	if err := os.WriteFile(path, []byte("up:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)

	// Edited elsewhere — a phone, an editor — after the board started.
	if err := os.WriteFile(path,
		[]byte("up:\n  - Brock Bowers\nmust:\n  - Jahmyr Gibbs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not there yet: the board must not pick it up on its own.
	if _, ok := srv.static.leans[draft.NormalizeName("Jahmyr Gibbs")]; ok {
		t.Fatal("the board reloaded without being asked")
	}

	if err := srv.reloadLeans(); err != nil {
		t.Fatal(err)
	}
	if pl, ok := srv.static.leans[draft.NormalizeName("Jahmyr Gibbs")]; !ok || pl.Lean != draft.LeanMust {
		t.Errorf("the new read did not arrive: %+v %v", pl, ok)
	}
	if _, ok := srv.static.leans[draft.NormalizeName("Brock Bowers")]; !ok {
		t.Error("the read that was already there went missing")
	}
}

// TestReloadKeepsThePreviousReadsWhenTheFileIsBroken — a half-typed YAML
// file must not cost you every read you hold. Reporting the error and
// keeping what worked is the whole posture of this workflow.
func TestReloadKeepsThePreviousReadsWhenTheFileIsBroken(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mine.yaml")
	if err := os.WriteFile(path, []byte("must:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)

	if err := os.WriteFile(path, []byte("mustt:\n  - Jahmyr Gibbs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.reloadLeans(); err == nil {
		t.Fatal("a broken lean file must be reported, not swallowed")
	}
	if pl, ok := srv.static.leans[draft.NormalizeName("Brock Bowers")]; !ok || pl.Lean != draft.LeanMust {
		t.Errorf("the previous reads were lost to a typo: %+v %v", pl, ok)
	}
}

// TestReloadKeepsReadsSetOnTheBoard — board edits and file reads are two
// layers, and reloading the lower one must not drop the upper.
func TestReloadKeepsReadsSetOnTheBoard(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("up:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)
	srv.leans.set("Ja'Marr Chase", draft.LeanMust)

	if err := srv.reloadLeans(); err != nil {
		t.Fatal(err)
	}
	if pl, ok := srv.leans.snapshot()[draft.NormalizeName("Ja'Marr Chase")]; !ok || pl.lean == nil || *pl.lean != draft.LeanMust {
		t.Errorf("a read set on the board did not survive the reload: %+v %v", pl, ok)
	}
}

// TestReloadAndSaveDoNotRace — the board writes your lean set and the reload
// replaces the map it reads. Run with -race; without the locking in
// saveLeans and reloadLeans this reports a data race on a live map.
func TestReloadAndSaveDoNotRace(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("up:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)
	srv.leans.set("Jahmyr Gibbs", draft.LeanMust)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = srv.saveLeans() }()
		go func() { defer wg.Done(); _ = srv.reloadLeans() }()
	}
	wg.Wait()

	// Whatever the interleaving, the read must still be on the board.
	if pl, ok := srv.leans.snapshot()[draft.NormalizeName("Jahmyr Gibbs")]; !ok || pl.lean == nil || *pl.lean != draft.LeanMust {
		t.Errorf("the read was lost to concurrency: %+v %v", pl, ok)
	}
}

// TestReloadRefreshesWhatTheStripSaysAboutLeans — a warning that survives
// the edit that fixed it is worse than none: the strip becomes a list of
// things that used to be true, and you stop reading it.
func TestReloadRefreshesWhatTheStripSaysAboutLeans(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mine.yaml")
	// A read naming nobody the board has.
	if err := os.WriteFile(path, []byte("up:\n  - Jahmyr Gibs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)
	srv.static.matcher = draft.NewPoolMatcher(poolNames(srv.static.projections), nil)
	srv.static.refreshLeanWarnings()
	if len(srv.static.leanWarnings) != 1 {
		t.Fatalf("expected the bad read to be reported, got %v", srv.static.leanWarnings)
	}

	// Fix the spelling, the way you would after reading the warning.
	if err := os.WriteFile(path, []byte("up:\n  - Jahmyr Gibbs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.reloadLeans(); err != nil {
		t.Fatal(err)
	}
	if len(srv.static.leanWarnings) != 0 {
		t.Errorf("the warning outlived the fix: %v", srv.static.leanWarnings)
	}
}

// TestPrecedenceDecidesASpellingCollision — two sets spelling one player
// differently used to collide after the merge, where nothing knew which set
// outranked which. Your must-have losing to an analyst's up, on some starts
// and not others, is the worst shape a bug can take here.
func TestPrecedenceDecidesASpellingCollision(t *testing.T) {
	mine := draft.LeanSet{Name: "mine", Leans: draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs"): {
			Player: "Jahmyr Gibbs", Lean: draft.LeanMust, Cap: 20, Note: "hard cap", Source: "mine"},
	}}
	analyst := draft.LeanSet{Name: "menton", Leans: draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs Jr."): {
			Player: "Jahmyr Gibbs Jr.", Lean: draft.LeanDND, Source: "menton"},
	}}
	m := draft.NewPoolMatcher([]string{"Jahmyr Gibbs"}, nil)

	for i := 0; i < 200; i++ {
		got := matchAndMerge([]draft.LeanSet{mine, analyst}, m)[draft.NormalizeName("Jahmyr Gibbs")]
		if got.Lean != draft.LeanMust || got.Cap != 20 || got.Note != "hard cap" {
			t.Fatalf("run %d: precedence lost — %+v", i, got)
		}
		// And the disagreement has to survive, or the board cannot mark it.
		if !got.Contested() {
			t.Fatalf("run %d: the analyst's opposing read vanished: %+v", i, got)
		}
	}
}

// TestLeanEndpointStoresTheBoardsSpelling — validation resolves a name
// through the matcher, so the read must be keyed the same way. Otherwise the
// request succeeds, the file gains the alternate spelling, and the read
// reaches nobody.
func TestLeanEndpointStoresTheBoardsSpelling(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte("up:\n  - Brock Bowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := leanServerAt(t, cfg)
	srv.static.matcher = draft.NewPoolMatcher(poolNames(srv.static.projections), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/lean",
		strings.NewReader(`{"player":"Jahmyr Gibbs Jr.","lean":"must"}`))
	rec := httptest.NewRecorder()
	srv.handleLean(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	// Accepted, so it has to have landed on the player it resolved to.
	found := false
	for _, p := range srv.snapshot().Players {
		if p.Name == "Jahmyr Gibbs" && p.Lean.Lean == draft.LeanMust {
			found = true
		}
	}
	if !found {
		t.Error("the endpoint returned 200 but the read reached no row on the board")
	}
}

// TestCyclingALeanKeepsTheFavoriteTag is the same defect as the cap, one field
// over and found later: the overlay carried no Favorite and the save restored
// only Cap and Note, so every route around the read cycle dropped the star.
//
// It mattered quietly. A favorite lifts the walk-away toward a must-have's
// headroom, so losing the tag lowered what you would pay for a name you had
// deliberately marked as one you want, with nothing on screen to say so.
func TestCyclingALeanKeepsTheFavoriteTag(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml",
		"up:\n  - Jahmyr Gibbs\nfavorites:\n  - Jahmyr Gibbs\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	for _, lean := range []draft.Lean{draft.LeanMust, draft.LeanDown, draft.LeanDND, "", draft.LeanUp} {
		srv.leans.set("Jahmyr Gibbs", lean)
		if err := srv.saveLeans(); err != nil {
			t.Fatal(err)
		}
	}

	if live := srv.static.effectiveLeans(srv.leans.snapshot())[key]; !live.Favorite {
		t.Error("the favorite tag went missing from the board after a full cycle")
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; !got.Favorite || got.Lean != draft.LeanUp {
		t.Errorf("saved as lean %q favorite %v, want up and still favorited", got.Lean, got.Favorite)
	}
}

// TestAFavoriteOutlivesItsRead — the tag is not a read. Clearing the read on a
// player you have marked as one of yours should leave him under favorites
// rather than deleting him from the file, which is a state the format has a
// place for and WalkAway prices correctly.
func TestAFavoriteOutlivesItsRead(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml",
		"up:\n  - Jahmyr Gibbs\nfavorites:\n  - Jahmyr Gibbs\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	srv.leans.set("Jahmyr Gibbs", "")
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	live := srv.static.effectiveLeans(srv.leans.snapshot())[key]
	if !live.Favorite || live.Lean != "" {
		t.Errorf("on the board: lean %q favorite %v, want no read but still favorited", live.Lean, live.Favorite)
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; !got.Favorite || got.Lean != "" {
		t.Errorf("saved as lean %q favorite %v, want him kept under favorites alone", got.Lean, got.Favorite)
	}
}

// TestUntaggingAFavoriteBeatsTheFile — the mirror of the preservation above.
// "Untouched" falls back to the file, but "cleared" has to overwrite it, or
// the leans page could never remove a star.
func TestUntaggingAFavoriteBeatsTheFile(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml",
		"up:\n  - Jahmyr Gibbs\nfavorites:\n  - Jahmyr Gibbs\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	srv.leans.setFavorite("Jahmyr Gibbs", false)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	if live := srv.static.effectiveLeans(srv.leans.snapshot())[key]; live.Favorite {
		t.Error("the board still shows a favorite that was untagged")
	}
	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; got.Favorite {
		t.Error("the file still lists a favorite that was untagged")
	} else if got.Lean != draft.LeanUp {
		t.Errorf("lean = %q, want the read left alone by a favorite-only edit", got.Lean)
	}
}

// TestTaggingAFavoriteLeavesTheRead — a favorite-only edit must not be read as
// "set his lean to nothing", which is what a single non-pointer field would
// have made it.
func TestTaggingAFavoriteLeavesTheRead(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml", "must:\n  - Jahmyr Gibbs\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	srv.leans.setFavorite("Jahmyr Gibbs", true)
	if err := srv.saveLeans(); err != nil {
		t.Fatal(err)
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; got.Lean != draft.LeanMust || !got.Favorite {
		t.Errorf("saved as lean %q favorite %v, want must and favorited", got.Lean, got.Favorite)
	}
}

// TestAFavoriteTagAloneDoesNotClearTheRead is the same guarantee one level
// down, on the overlay rather than the file: a favorite-only edit carries no
// lean, and a non-pointer field would have made that mean "set it to nothing".
func TestAFavoriteTagAloneDoesNotClearTheRead(t *testing.T) {
	s := testStatic()
	key := draft.NormalizeName("Jahmyr Gibbs")
	s.leans = draft.Leans{key: {Player: "Jahmyr Gibbs", Lean: draft.LeanMust, Cap: 40}}

	got := s.effectiveLeans(favEdit("Jahmyr Gibbs", true))[key]
	if got.Lean != draft.LeanMust {
		t.Errorf("lean = %q, want the read left alone", got.Lean)
	}
	if !got.Favorite {
		t.Error("the tag was not applied")
	}
	if got.Cap != 40 {
		t.Errorf("cap = %d, want the loaded set's own value kept", got.Cap)
	}
}

// TestFavoriteOnlyEditLeavesTheRead — the leans page sends setLean:false when
// it is only tagging a star. Without that flag the empty Lean in the body
// would read as "clear his read", so a click on the star would silently throw
// away the conviction beside it.
func TestFavoriteOnlyEditLeavesTheRead(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml", "must:\n  - Jahmyr Gibbs\n")
	key := draft.NormalizeName("Jahmyr Gibbs")

	body := `{"player":"Jahmyr Gibbs","favorite":true,"setLean":false}`
	rec := httptest.NewRecorder()
	srv.handleLean(rec, httptest.NewRequest(http.MethodPost, "/api/lean", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[key]; got.Lean != draft.LeanMust || !got.Favorite {
		t.Errorf("saved lean %q favorite %v, want must and favorited", got.Lean, got.Favorite)
	}
}

// TestAnEmptyEditIsRejected — a body that changes nothing is a bug in the
// caller, and answering 200 to it would hide that.
func TestAnEmptyEditIsRejected(t *testing.T) {
	srv, _ := leanServerFile(t, "mine.yaml", "must:\n  - Jahmyr Gibbs\n")
	body := `{"player":"Jahmyr Gibbs","setLean":false}`
	rec := httptest.NewRecorder()
	srv.handleLean(rec, httptest.NewRequest(http.MethodPost, "/api/lean", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an edit that changes nothing", rec.Code)
	}
}

// TestTheBoardsOwnBodyStillWorks — the board sends {player, lean} and knows
// nothing about favorites or setLean. Extending the endpoint must not have
// changed what it means.
func TestTheBoardsOwnBodyStillWorks(t *testing.T) {
	srv, path := leanServerFile(t, "mine.yaml", "up:\n  - Jahmyr Gibbs\n")
	body := `{"player":"Jahmyr Gibbs","lean":"dnd"}`
	rec := httptest.NewRecorder()
	srv.handleLean(rec, httptest.NewRequest(http.MethodPost, "/api/lean", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	back, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back[draft.NormalizeName("Jahmyr Gibbs")]; got.Lean != draft.LeanDND {
		t.Errorf("lean = %q, want dnd", got.Lean)
	}
}

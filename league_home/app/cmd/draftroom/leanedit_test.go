package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leaguehome/internal/draft"
)

// leanServer is a board with a writable config directory holding a starting
// mine.csv, so saves have a real file to read back and modify.
func leanServer(t *testing.T, mine string) (*server, string) {
	t.Helper()
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "leans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mine.csv")
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	static := testStatic()
	// Production loads staticData.leans from this same file at startup, so a
	// fixture that leaves it empty tests a state the server cannot be in.
	loaded, err := draft.LoadLeans(path)
	if err != nil {
		t.Fatal(err)
	}
	static.leans = loaded

	srv, err := newServer(static, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv, path
}

// TestEditsOverrideTheLoadedSets — the point of the overlay.
func TestEditsOverrideTheLoadedSets(t *testing.T) {
	s := testStatic()
	s.leans = draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs"): {Player: "Jahmyr Gibbs", Lean: draft.LeanDown},
	}
	edits := draft.Leans{
		draft.NormalizeName("Jahmyr Gibbs"): {Player: "Jahmyr Gibbs", Lean: draft.LeanMust},
	}

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

	got := s.effectiveLeans(draft.Leans{key: {Player: "Jahmyr Gibbs", Lean: ""}})
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

	got := s.effectiveLeans(draft.Leans{key: {
		Player: "Jahmyr Gibbs", Lean: draft.LeanMust, Source: "board",
	}})[key]

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

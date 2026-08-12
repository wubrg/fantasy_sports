package draft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A lean set is one source of opinion about players, kept in its own file.
//
// The split exists because the opinions have different standing. Yours are
// convictions you will act on; an analyst's are evidence you asked for. If
// they lived in one file you would have to keep editing someone else's
// reads to hold your own, and a refresh would silently overwrite you.
//
// Separate files also make disagreement visible, which is the part worth
// having. Menton's Big-3 traits say Kenneth Walker is a zero-trait back and
// lean him down; you have him as a must-have on situation. Both are true
// statements about him, and seeing them side by side is more useful than
// either one winning quietly.
type LeanSet struct {
	// Name is the file's stem: "mine" is data/leans/mine.csv.
	Name string
	// Path is where it was read from.
	Path string
	// Leans are the reads in this set alone, before any merge.
	Leans Leans
	// Undecided names the players this set lists with no lean yet, in file
	// order. They are carried rather than dropped so a name you wrote down
	// and have not ruled on stays visible instead of looking like a read
	// that landed.
	Undecided []string
	// Generated marks a set produced from source data rather than typed by
	// hand, which decides whether `draftroom leans -generate` may rewrite
	// it. Nothing regenerates a file you wrote yourself.
	Generated bool
}

// leansDir is where named sets live, under the config directory.
const leansDir = "leans"

// LoadLeanSet reads one named set from the config directory.
func LoadLeanSet(configDir, name string) (LeanSet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return LeanSet{}, fmt.Errorf("draft: a lean set needs a name")
	}
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return LeanSet{}, fmt.Errorf("draft: %q is not a lean set name (use the file stem, like mine)", name)
	}
	path := filepath.Join(configDir, leansDir, name+".csv")
	f, err := os.Open(path)
	if err != nil {
		return LeanSet{}, err
	}
	defer f.Close()

	leans, undecided, err := ParseLeans(f)
	if err != nil {
		return LeanSet{}, fmt.Errorf("draft: %s: %w", path, err)
	}
	set := LeanSet{Name: name, Path: path, Leans: leans, Undecided: undecided, Generated: isGenerated(path)}
	for key, pl := range set.Leans {
		pl.Source = name
		set.Leans[key] = pl
	}
	return set, nil
}

// LoadLeanSets reads the named sets in precedence order and merges them.
//
// Precedence is the order given: the first set to name a player owns him.
// That makes "mine,menton" mean what it reads like — your reads win, and
// the analyst fills in everyone you have no opinion about.
//
// A named set that does not exist is an error rather than an empty set,
// because a typo would otherwise silently drop every read in it.
func LoadLeanSets(configDir string, names []string) (Leans, []LeanSet, error) {
	var sets []LeanSet
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		set, err := LoadLeanSet(configDir, name)
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("draft: no lean set named %q in %s",
				name, filepath.Join(configDir, leansDir))
		}
		if err != nil {
			return nil, nil, err
		}
		sets = append(sets, set)
	}
	return MergeLeans(sets...), sets, nil
}

// MergeLeans combines sets in precedence order, recording what the losing
// sets said rather than discarding it.
//
// Keeping the losers is the point of merging at all. A player two sources
// disagree about is a different proposition from one they agree about, and
// the board can only say so if the disagreement survives the merge.
func MergeLeans(sets ...LeanSet) Leans {
	out := Leans{}
	defer func() {
		// A none that won the merge did its job by winning: it silenced the
		// sets behind it. Leaving it in would put a read on the board for a
		// player you explicitly have no read on.
		for k, pl := range out {
			if pl.Lean == LeanNone {
				delete(out, k)
			}
		}
	}()
	for _, set := range sets {
		for key, pl := range set.Leans {
			pl.Source = set.Name
			held, taken := out[key]
			if !taken {
				out[key] = pl
				continue
			}
			// A later set never displaces an earlier one; it is recorded
			// as a second opinion on the read that already stands.
			held.Others = append(held.Others, PlayerLean{
				Player: pl.Player, Lean: pl.Lean, Cap: pl.Cap,
				Note: pl.Note, Source: set.Name,
			})
			out[key] = held
		}
	}
	return out
}

// direction reduces a lean to the only thing two sources can contradict
// each other about: whether to want the player more or less than his price
// says. must and up both mean more, down and dnd both mean less.
//
// Collapsing to a sign is deliberate. "must" versus "up" is a difference of
// degree between people who agree; "up" versus "down" is a disagreement
// about the player.
func (l Lean) direction() int {
	switch l {
	case LeanMust, LeanUp:
		return 1
	case LeanDown, LeanDND:
		return -1
	}
	return 0
}

// Disagreement reports the sets that read a player the opposite way from
// the one that won, if any.
func (p PlayerLean) Disagreement() []PlayerLean {
	var out []PlayerLean
	for _, o := range p.Others {
		if o.Lean.direction() != 0 && o.Lean.direction() != p.Lean.direction() {
			out = append(out, o)
		}
	}
	return out
}

// Contested reports whether any other set read this player the opposite way.
//
// A contested read is not a reason to change the bid — yours still wins —
// but it is a reason to know you are outvoting someone before the bidding
// starts rather than after.
func (p PlayerLean) Contested() bool { return len(p.Disagreement()) > 0 }

// MarshalJSON adds the contested read to the wire form.
//
// Computed at serialization rather than stored, so the page cannot show a
// disagreement the merge no longer holds — and so the browser does not have
// to reimplement which leans contradict which.
func (p PlayerLean) MarshalJSON() ([]byte, error) {
	type wire PlayerLean
	against := p.Disagreement()
	by := make([]string, 0, len(against))
	for _, o := range against {
		by = append(by, fmt.Sprintf("%s says %s", o.Source, o.Lean))
	}
	return json.Marshal(struct {
		wire
		Contested   bool     `json:"contested"`
		ContestedBy []string `json:"contestedBy,omitempty"`
	}{wire(p), len(against) > 0, by})
}

// Why explains a merged read in one line, naming who said what.
func (p PlayerLean) Why() string {
	if p.Lean == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", p.Source, p.Lean)
	if p.Note != "" {
		fmt.Fprintf(&b, " (%s)", p.Note)
	}
	for _, o := range p.Others {
		fmt.Fprintf(&b, " · %s: %s", o.Source, o.Lean)
		if o.Note != "" {
			fmt.Fprintf(&b, " (%s)", o.Note)
		}
	}
	return b.String()
}

// SetNames splits a comma-separated -leans value.
func SetNames(flagValue string) []string {
	var out []string
	for _, part := range strings.Split(flagValue, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Contested lists every merged read the sets disagree about, sorted by
// player so the report reads the same way twice.
func (l Leans) Contested() []PlayerLean {
	var out []PlayerLean
	for _, pl := range l {
		if pl.Contested() {
			out = append(out, pl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Player < out[j].Player })
	return out
}

package main

import (
	"net/http"
	"sort"

	"leaguehome/internal/draft"
)

// The leans page is the merged picture `draftroom leans` prints, on a screen
// you can act on. The command answers the pre-draft question — did precedence
// land the way I meant, and does every read reach a real player — but it
// answers it in a terminal, and the answer is only useful if you then go and
// change something. This serves the same report and lets you change it.
//
// It reports against the board's own pool rather than the source file, which
// is the smaller and truer thing: a source row that never matched a Sleeper id
// is in the file and not on the board, so only here is the difference knowable.

// leanRow is one player's merged read, flattened for the page.
//
// Deliberately not draft.PlayerLean itself. That type marshals its own
// contested fields and carries the losing reads whole, which is more than the
// page needs and less than it wants: it says nothing about whether the read
// reaches a player who is actually on the board.
type leanRow struct {
	Player   string `json:"player"`
	Lean     string `json:"lean"`
	Source   string `json:"source"`
	Favorite bool   `json:"favorite"`
	Cap      int    `json:"cap,omitempty"`
	Note     string `json:"note,omitempty"`
	Position string `json:"position,omitempty"`
	// Against names the sets that read him the other way, already phrased.
	Against []string `json:"against,omitempty"`
	// OnBoard is false for a read that reaches nobody — a misspelling, or a
	// player the pool does not carry. Suggestion is the nearest real name.
	OnBoard    bool   `json:"onBoard"`
	Suggestion string `json:"suggestion,omitempty"`
}

// leanSetRow is one loaded set, in precedence order.
type leanSetRow struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Reads     int      `json:"reads"`
	Generated bool     `json:"generated"`
	Writable  bool     `json:"writable"`
	Undecided []string `json:"undecided,omitempty"`
}

type leansPayload struct {
	Sets []leanSetRow `json:"sets"`
	Rows []leanRow    `json:"rows"`
	// Writable is the file an edit from this page lands in.
	Writable string `json:"writable"`
	// Leans are the pill values the page may set, so the page never invents
	// one the server would reject.
	Cycle []string `json:"cycle"`
}

func (s *server) handleLeansPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	static := s.static
	s.mu.Unlock()

	// The overlay laid over the file, so the page shows what the board shows
	// rather than what the file said at startup.
	merged := static.effectiveLeans(s.leans.snapshot())

	// Reads that reach nobody, keyed for the row that carries them.
	unmatched := map[string]draft.UnmatchedLean{}
	for _, u := range merged.Unmatched(poolNames(static.projections), static.matcher) {
		unmatched[draft.NormalizeName(u.Lean.Player)] = u
	}

	rows := make([]leanRow, 0, len(merged))
	for key, pl := range merged {
		row := leanRow{
			Player: pl.Player, Lean: string(pl.Lean), Source: pl.Source,
			Favorite: pl.Favorite, Cap: pl.Cap, Note: pl.Note,
			Position: static.positionOf(static.playerIDByName(pl.Player)), OnBoard: true,
		}
		for _, o := range pl.Disagreement() {
			row.Against = append(row.Against, o.Source+" says "+string(o.Lean))
		}
		if u, bad := unmatched[key]; bad {
			row.OnBoard, row.Suggestion = false, u.Suggestion
		}
		rows = append(rows, row)
	}

	// Contested first, then unreachable, then by read and name. Both of those
	// groups are the ones worth acting on before a draft rather than during
	// one, so they lead rather than sitting wherever the alphabet puts them.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (len(a.Against) > 0) != (len(b.Against) > 0) {
			return len(a.Against) > 0
		}
		if a.OnBoard != b.OnBoard {
			return !a.OnBoard
		}
		if a.Lean != b.Lean {
			return a.Lean < b.Lean
		}
		return a.Player < b.Player
	})

	sets := make([]leanSetRow, 0, len(static.leanSetInfo))
	for _, set := range static.leanSetInfo {
		sets = append(sets, leanSetRow{
			Name: set.Name, Path: set.Path, Reads: len(set.Leans),
			Generated: set.Generated, Writable: set.Path == static.minePath,
			Undecided: set.Undecided,
		})
	}

	cycle := make([]string, 0, len(leanCycle))
	for _, l := range leanCycle {
		cycle = append(cycle, string(l))
	}

	writeJSON(w, leansPayload{
		Sets: sets, Rows: rows, Writable: static.minePath, Cycle: cycle,
	})
}

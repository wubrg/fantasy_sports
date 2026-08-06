package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"leaguehome/internal/draft"
)

// loadLeanSets resolves the named sets, falling back to the pre-lean-set
// my-guys.csv when a config directory predates the split.
//
// The fallback exists because a missing lean file is not an error — no
// reads recorded is a legitimate state — so an unmigrated directory would
// otherwise come up silently empty and the board would look right while
// quietly ignoring every conviction you hold.
func loadLeanSets(cfg string, names []string) (draft.Leans, []draft.LeanSet, error) {
	if len(names) == 0 {
		names = draft.SetNames(defaultLeanSets)
	}
	leans, sets, err := draft.LoadLeanSets(cfg, names)
	if err == nil {
		return leans, sets, nil
	}

	legacy := filepath.Join(cfg, myGuysFile)
	onlyMine := len(names) == 1 && names[0] == "mine"
	if !onlyMine {
		return nil, nil, err
	}
	if _, statErr := os.Stat(legacy); statErr != nil {
		return nil, nil, err
	}
	old, loadErr := draft.LoadLeans(legacy)
	if loadErr != nil {
		return nil, nil, loadErr
	}
	fmt.Fprintf(os.Stderr,
		"note: reading %s; move it to %s to use named lean sets\n",
		legacy, filepath.Join(cfg, "leans", "mine.csv"))
	set := draft.LeanSet{Name: "mine", Path: legacy, Leans: old}
	return draft.MergeLeans(set), []draft.LeanSet{set}, nil
}

// runLeans shows the merged lean sets, or rebuilds the generated ones.
//
// Worth having as its own command because a merge you cannot inspect is a
// merge you cannot trust: precedence decides which of two contradictory
// reads reaches the board, and the only way to know it landed the way you
// meant is to look at it before the draft rather than during.
func runLeans(configDir, dataDir string, names []string, generate bool) error {
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	if generate {
		return generateLeanSets(cfg, dataDir)
	}

	merged, sets, err := loadLeanSets(cfg, names)
	if err != nil {
		return err
	}

	fmt.Printf("%-10s %-7s %s\n", "SET", "READS", "FILE")
	for _, set := range sets {
		kind := set.Path
		if set.Generated {
			kind += "  (generated)"
		}
		fmt.Printf("%-10s %-7d %s\n", set.Name, len(set.Leans), kind)
	}
	if len(sets) > 1 {
		fmt.Printf("\nprecedence: %s — the first set to name a player owns him\n",
			strings.Join(names, " > "))
	}

	rows := make([]draft.PlayerLean, 0, len(merged))
	for _, pl := range merged {
		rows = append(rows, pl)
	}
	// Contested reads first: they are the ones worth reading before the
	// draft, since something you believe is being argued with.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Contested() != rows[j].Contested() {
			return rows[i].Contested()
		}
		if rows[i].Lean != rows[j].Lean {
			return rows[i].Lean < rows[j].Lean
		}
		return rows[i].Player < rows[j].Player
	})

	fmt.Printf("\n%-22s %-6s %-8s %s\n", "PLAYER", "LEAN", "SET", "WHY")
	for _, pl := range rows {
		mark := ""
		if pl.Contested() {
			mark = "!"
		}
		why := pl.Note
		if d := pl.Disagreement(); len(d) > 0 {
			var against []string
			for _, o := range d {
				against = append(against, fmt.Sprintf("%s says %s", o.Source, o.Lean))
			}
			why = strings.Join(against, ", ")
			if pl.Note != "" {
				why += " — you: " + pl.Note
			}
		}
		fmt.Printf("%1s%-21s %-6s %-8s %s\n", mark, pl.Player, pl.Lean, pl.Source, truncate(why, 70))
	}

	if contested := merged.Contested(); len(contested) > 0 {
		fmt.Printf("\n%d contested: your read stands, but another set argues the other way.\n",
			len(contested))
	}
	return nil
}

// generateLeanSets rebuilds every generated set from source data.
func generateLeanSets(cfg, dataDir string) error {
	root, err := draft.ResolveDataRoot(dataDir)
	if err != nil {
		return err
	}
	for _, g := range draft.Generators {
		leans, err := g.Generate(root)
		if err != nil {
			return err
		}
		path, err := draft.WriteLeanSet(cfg, g, leans)
		if err != nil {
			return err
		}
		up, down := 0, 0
		for _, pl := range leans {
			switch pl.Lean {
			case draft.LeanUp:
				up++
			case draft.LeanDown:
				down++
			}
		}
		fmt.Printf("%-10s %d reads (%d up, %d down)  %s\n", g.Name, len(leans), up, down, path)
	}
	return nil
}

// setNames lists loaded sets in precedence order.
func setNames(sets []draft.LeanSet) []string {
	out := make([]string, 0, len(sets))
	for _, set := range sets {
		out = append(out, set.Name)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

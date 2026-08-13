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
		legacy, filepath.Join(cfg, "leans", "mine.yaml"))
	set := draft.LeanSet{Name: "mine", Path: legacy, Leans: old}
	return draft.MergeLeans(set), []draft.LeanSet{set}, nil
}

// writableSetPath is the file a read set on the board belongs in: the one
// the highest-precedence set was actually loaded from.
//
// Falls back to where "mine" would be created, for the case where no set
// loaded at all. Never guesses when a real path is available — the reader's
// fallbacks are not reproducible from the config directory alone.
func writableSetPath(cfg string, sets []draft.LeanSet) string {
	if len(sets) > 0 && sets[0].Path != "" {
		return sets[0].Path
	}
	return draft.LeanSetPath(cfg, "mine")
}

// runLeans shows the merged lean sets, or rebuilds the generated ones.
//
// Worth having as its own command because a merge you cannot inspect is a
// merge you cannot trust: precedence decides which of two contradictory
// reads reaches the board, and the only way to know it landed the way you
// meant is to look at it before the draft rather than during.
func runLeans(configDir, dataDir string, names []string, generate, convert bool) error {
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	if generate {
		return generateLeanSets(cfg, dataDir)
	}
	if convert {
		return convertLeanSets(cfg, names)
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
	// Players written down but not ruled on. Worth printing because the
	// alternative is a name sitting in the file looking like a read that
	// landed, which is exactly what you cannot check mid-draft.
	for _, set := range sets {
		if len(set.Undecided) == 0 {
			continue
		}
		fmt.Printf("\n%s: %d listed with no lean yet — %s\n",
			set.Name, len(set.Undecided), strings.Join(set.Undecided, ", "))
	}
	reportUnmatched(sets, cfg, dataDir)

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

// convertLeanSets rewrites the named sets in the grouped YAML form,
// leaving the original where it is.
//
// Nothing is deleted and nothing is overwritten. A lean set may be a
// symlink into a notes vault that other things point at, and the file is
// being edited right up to draft day; the conversion has to be something
// you can look at and walk away from.
func convertLeanSets(cfg string, names []string) error {
	_, sets, err := loadLeanSets(cfg, names)
	if err != nil {
		return err
	}
	for _, set := range sets {
		if strings.EqualFold(filepath.Ext(set.Path), ".yaml") ||
			strings.EqualFold(filepath.Ext(set.Path), ".yml") {
			fmt.Printf("%-10s already YAML  %s\n", set.Name, set.Path)
			continue
		}
		// Sorted by lean then name so the generated file is stable, since
		// a map has no order to preserve in the first place.
		reads := make([]draft.PlayerLean, 0, len(set.Leans))
		for _, pl := range set.Leans {
			reads = append(reads, pl)
		}
		sort.Slice(reads, func(i, j int) bool {
			if reads[i].Lean != reads[j].Lean {
				return reads[i].Lean < reads[j].Lean
			}
			return reads[i].Player < reads[j].Player
		})

		doc, err := draft.FormatLeansYAML(reads, set.Undecided)
		if err != nil {
			return err
		}
		// A set is often a symlink into a notes vault so it can be edited
		// from a phone. Writing the YAML beside the *link* would put a real
		// file in front of it: the loader prefers YAML, so every later edit
		// in the vault would stop reaching the board without saying so.
		// Write beside the target instead, and leave re-pointing to you.
		source, relink := set.Path, ""
		if target, err := filepath.EvalSymlinks(set.Path); err == nil && target != set.Path {
			source = target
			relink = set.Path
		}
		out := strings.TrimSuffix(source, filepath.Ext(source)) + ".yaml"
		if _, err := os.Stat(out); err == nil {
			return fmt.Errorf("%s already exists; move it aside if you meant to replace it", out)
		}
		if err := os.WriteFile(out, doc, 0o644); err != nil {
			return err
		}
		fmt.Printf("%-10s %d reads  %s\n", set.Name, len(reads), out)
		if relink != "" {
			fmt.Printf("%-10s is a symlink; point it at the new file when it looks right:\n"+
				"           ln -sfn %q %s\n",
				set.Name, out, strings.TrimSuffix(relink, filepath.Ext(relink))+".yaml")
		}
	}
	fmt.Printf("\nthe originals are untouched; delete one once its .yaml looks right\n")
	return nil
}

// projectionSource is the normalized CSV whose spellings a lean has to
// match, because the board names players from it — see the Projection built
// in loadStatic.
const projectionSource = "ciely-2026.csv"

// reportUnmatched names the reads that can never fire, per set.
//
// A lean is applied by name, so one typed wrong is not an error and not a
// warning — it is simply absent from the board, indistinguishable from a
// read that landed. That is tolerable when the file is edited beside the
// code and dangerous when it is edited on a phone, which is the whole point
// of checking before the draft rather than discovering it during one.
//
// The check is deliberately best-effort. Anyone without the private data
// repo still has a working `draftroom leans`, and a missing optional check
// must never become a failed command.
func reportUnmatched(sets []draft.LeanSet, cfg, dataDir string) {
	root, err := draft.ResolveDataRoot(dataDir)
	if err != nil {
		fmt.Printf("\nskipped the name check: %v\n", err)
		return
	}
	rows, err := draft.LoadSourceCSV(root.Normalized(projectionSource))
	if err != nil {
		fmt.Printf("\nskipped the name check: %v\n", err)
		return
	}
	pool := make([]string, 0, len(rows))
	for _, r := range rows {
		pool = append(pool, r.Player)
	}
	// Same matcher the board uses, so the report never flags a read the
	// board will happily apply. A missing alias file is not an error.
	aliases, _ := draft.LoadAliases(filepath.Join(cfg, aliasesFile))
	matcher := draft.NewPoolMatcher(pool, aliases)

	for _, set := range sets {
		bad := set.Leans.Unmatched(pool, matcher)
		if len(bad) == 0 {
			continue
		}
		fmt.Printf("\n%s: %d of %d name no player in %s\n",
			set.Name, len(bad), len(set.Leans), projectionSource)
		for _, u := range bad {
			fix := "no close match — check the spelling"
			if u.Suggestion != "" {
				fix = "did you mean " + u.Suggestion + "?"
			}
			fmt.Printf("  %-24s %s\n", u.Lean.Player, fix)
		}
	}
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

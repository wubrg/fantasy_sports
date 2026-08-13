package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"leaguehome/internal/draft"
	"leaguehome/internal/sleeper"
)

// sourceFile pairs a normalized CSV with the schema it must satisfy.
var sourceFiles = []struct {
	file   string
	schema draft.SourceSchema
}{
	{"ciely-2026.csv", draft.CielyColumns},
	{"subvertadown-2026.csv", draft.SubvertadownColumns},
}

// playerNames fetches Sleeper's dictionary for name resolution alone.
//
// Deliberately not playerInfo, which also joins last season's stats for the
// keeper-eligibility rule and therefore needs a season, which needs the
// ledger, which needs five seasons of league history. Resolving a name
// wants none of that — only names, positions and teams — so this asks for
// the one thing it uses and the command needs no league at all.
func playerNames(c *sleeper.Client) (map[string]draft.PlayerInfo, error) {
	players, err := c.Players()
	if err != nil {
		return nil, fmt.Errorf("loading player dictionary: %w", err)
	}
	out := make(map[string]draft.PlayerInfo, len(players))
	for id, p := range players {
		name := p.FullName
		if name == "" {
			// Team defenses carry no full_name.
			name = strings.TrimSpace(p.FirstName + " " + p.LastName)
		}
		if name == "" {
			name = id
		}
		out[id] = draft.PlayerInfo{Name: name, Position: p.Position, Team: p.Team}
	}
	return out, nil
}

// runSources reports what each source contributes and which of its rows
// reach no Sleeper player.
//
// Worth its own command because the board can only ever say how many rows
// failed. A count tells you something is wrong without telling you which
// player is missing from the pool, and a player missing from the pool is
// invisible until somebody nominates him.
func runSources(configDir, dataDir string, unmatchedOnly bool) error {
	cfg, err := draft.ResolveConfigDir(configDir)
	if err != nil {
		return err
	}
	root, err := draft.ResolveDataRoot(dataDir)
	if err != nil {
		return err
	}
	aliases, err := draft.LoadAliases(filepath.Join(cfg, aliasesFile))
	if err != nil {
		return err
	}

	info, err := playerNames(sleeper.New())
	if err != nil {
		return err
	}
	idx := draft.BuildPlayerIndexWithAliases(info, aliases)

	total := 0
	for _, src := range sourceFiles {
		rows, err := draft.LoadSourceCSV(root.Normalized(src.file), src.schema)
		if err != nil {
			return err
		}
		bad := idx.Resolve(rows)
		total += len(bad)

		if !unmatchedOnly {
			fmt.Printf("%-14s %d rows, %d resolved, %d unmatched\n",
				src.schema.Name, len(rows), len(rows)-len(bad), len(bad))
		}
		if len(bad) == 0 {
			continue
		}
		fmt.Printf("\n%s: %d of %d rows reach no Sleeper player\n\n", src.schema.Name, len(bad), len(rows))
		for _, u := range bad {
			reportUnresolved(u, info, src.schema.Name)
		}
	}

	if total == 0 {
		fmt.Printf("\nevery row resolved.\n")
		return nil
	}
	fmt.Printf("\n%d rows unresolved. A row that resolves to nothing is a player the board\n"+
		"never had, so he cannot be leaned on, priced, or nominated against.\n", total)
	return nil
}

// reportUnresolved prints one failed row and, where it can, the aliases.csv
// line that would fix it.
func reportUnresolved(u draft.Unmatched, info map[string]draft.PlayerInfo, source string) {
	where := strings.TrimSpace(u.Row.Position + " " + u.Row.Team)
	fmt.Printf("  %s  %s\n    %s\n", u.Row.Player, where, u.Reason)

	// A defense is matched by team abbreviation, before aliases are ever
	// consulted, so an alias for one is inert — you would paste the line,
	// rerun, and see the identical suggestion forever. The fix is the
	// abbreviation, so say that instead.
	if isDefense(u.Row.Position) {
		if id, p, kind := draft.ClosestPlayer(u.Row.Player, "DEF", "", info); kind != draft.MatchNone {
			fmt.Printf("    Sleeper has %s as %s\n", p.Name, id)
		}
		fmt.Printf("    defenses match on team abbreviation, not by alias — correct the\n" +
			"    team column in the extractor output\n\n")
		return
	}

	id, p, kind := draft.ClosestPlayer(u.Row.Player, u.Row.Position, u.Row.Team, info)
	switch kind {
	case draft.MatchSpelling:
		// The names are a couple of edits apart at the same position, which
		// is what a typo looks like. Safe to hand over ready to paste.
		fmt.Printf("    looks like a misspelling of %s (%s, %s) id=%s\n", p.Name, p.Position, p.Team, id)
		fmt.Printf("    aliases.csv: %s,%s,%s name for %s\n\n", u.Row.Player, id, source, p.Name)
	case draft.MatchSurname:
		// Only the surname, position and team agree. That is how a nickname
		// is found and equally how two different men are confused — Brian
		// and Bijan Robinson are both Atlanta running backs. Naming the
		// candidate is useful; formatting it as a fix to paste unread is
		// not, because a wrong alias binds every read on that player to
		// somebody else and says nothing.
		fmt.Printf("    same surname, position and team: %s (%s, %s) id=%s\n", p.Name, p.Position, p.Team, id)
		fmt.Printf("    if that is the same player, add:  %s,%s,%s name for %s\n",
			u.Row.Player, id, source, p.Name)
		fmt.Printf("    if it is not, he is missing from Sleeper and there is nothing to alias\n\n")
	default:
		fmt.Printf("    no close match in Sleeper — check the spelling, or he may not be rostered\n\n")
	}
}

// isDefense reports whether a row is a team defense, which sources label
// either way.
func isDefense(position string) bool {
	return strings.EqualFold(position, "DST") || strings.EqualFold(position, "DEF")
}

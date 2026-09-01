package draft

import (
	"fmt"
	"os"
	"strings"
)

// ProjectionRole is the part a projection source plays on the board.
type ProjectionRole int

const (
	// RolePrimary is the projection the whole board is solved against: its
	// points order the pool, price the dollar values, and feed the trait
	// classifier. Exactly one source holds it; swapping which one is a change
	// of Role in the registry below, not a change of code.
	RolePrimary ProjectionRole = iota
	// RoleSecondOpinion is a projection re-solved against the same pool as a
	// comparison, kept beside the primary rather than blended into it. Where
	// the two disagree is exactly what a single number would hide.
	RoleSecondOpinion
)

// ProjectionSource declares one projection sheet the board can load.
//
// Adding a sheet is an entry here rather than a bespoke block in loadStatic:
// which file, the columns it must carry, whether its absence is survivable,
// and the part it plays. It mirrors the lean-set Generators registry — the
// same "one place to add a source" shape for the projections half of the board.
type ProjectionSource struct {
	// Name identifies the source internally.
	Name string
	// Label is how warnings name the source, e.g. "Ciely".
	Label string
	// File is the normalized CSV's stem, resolved through the data root.
	File string
	// Schema is the columns the file must carry to mean anything.
	Schema SourceSchema
	// Required fails the load when the source is missing or malformed. An
	// optional source degrades to a warning and an absent column instead, so
	// a snapshot taken before the source existed still renders.
	Required bool
	// AbsentNote completes "<Label> source absent — <AbsentNote>" for an
	// optional source. Empty for required sources, which cannot go absent
	// without failing the load.
	AbsentNote string
	// Role is the part the source plays; see ProjectionRole.
	Role ProjectionRole
	// Include, when set, keeps only the rows that belong on the board.
	// FantasyPros ships consensus rows plus the sharp-subset rows that are
	// the other half of its divergence signal, and only consensus belongs on
	// the board. Nil keeps every resolved, non-DST row.
	Include func(SourceRow) bool
	// RequirePoints drops rows carrying no projection at all. FantasyPros
	// ranks several hundred more players than it projects; a zero would enter
	// the solve as a player who scores nothing and come out at the dollar
	// floor, which reads as a real opinion. Dropped, he reads as "—".
	RequirePoints bool
}

// ProjectionSources is the set of projection sheets the board loads.
//
// Only sheets that get solved into comparable dollar values live here. The
// Subvertadown source is deliberately not one of them: it publishes VBD
// baselines and a market AAV, a different shape a shared registry would only
// distort (see the note on the schema block in sources.go).
// The board solves against FantasyPros' expert consensus rather than any one
// analyst. Two things decided it. Consensus is the thing the room is actually
// bidding against, so an edge measured from it is an edge over the table
// rather than a disagreement between two analysts. And FantasyPros publishes a
// high and a low for every player it projects, where Ciely publishes a median
// alone — the primary is the only source whose band the board can show, so a
// primary without one leaves every price a bare number.
//
// Ciely keeps his vote as a second opinion, now beside the two sharp subsets.
// Three of them re-solved against the same pool is a wider read on where the
// consensus is soft than one analyst could give.
var ProjectionSources = []ProjectionSource{
	{
		Name: "fantasypros", Label: "FantasyPros", File: "fantasypros-2026.csv",
		Schema: FantasyProsColumns, Required: true, Role: RolePrimary,
		Include: func(r SourceRow) bool { return strings.EqualFold(r.Baseline, "consensus") },
		// FantasyPros ranks roughly twice as deep as it projects. Without this
		// the unprojected half enters the solve at zero; see the note in
		// loadProjections.
		RequirePoints: true,
	},
	{
		Name: "ciely", Label: "Ciely", File: "ciely-2026.csv",
		Schema: CielyColumns, Required: false,
		AbsentNote: "Ciely column off", Role: RoleSecondOpinion,
	},
	// The sharp subsets are the same file under a different Include: the top-10
	// and top-20 experts by past accuracy. Where they part from consensus is
	// the divergence the board already flags, and re-solving them gives that
	// disagreement a dollar value instead of only a rank delta.
	{
		Name: "fantasypros-top10", Label: "FantasyPros top-10", File: "fantasypros-2026.csv",
		Schema: FantasyProsColumns, Required: false,
		AbsentNote: "top-10 sharp column off", Role: RoleSecondOpinion,
		Include:       func(r SourceRow) bool { return strings.EqualFold(r.Baseline, "top10") },
		RequirePoints: true,
	},
	{
		Name: "fantasypros-top20", Label: "FantasyPros top-20", File: "fantasypros-2026.csv",
		Schema: FantasyProsColumns, Required: false,
		AbsentNote: "top-20 sharp column off", Role: RoleSecondOpinion,
		Include:       func(r SourceRow) bool { return strings.EqualFold(r.Baseline, "top20") },
		RequirePoints: true,
	},
}

// SecondOpinion is one second-opinion source's projection set, ready to be
// re-solved against the live pool, plus the rank and sharp-expert sidecars
// that survive the solve unchanged.
type SecondOpinion struct {
	Name        string
	Projections []Projection
	// Rank is the source's positional consensus rank per player ID.
	Rank map[string]int
	// Sharp is the sharp-expert move per player ID, present only where the
	// source carries one — zero, and so absent, on every other source.
	Sharp map[string]int
}

// ProjectionData is the resolved output of the projection registry.
type ProjectionData struct {
	// PrimaryRows are the primary source's resolved rows, kept raw because
	// the trait classifier reads their published components.
	PrimaryRows []SourceRow
	// Projections and Points are the primary source restated for the solve
	// and for scarcity ordering, DST excluded to match the priced pool.
	Projections []Projection
	Points      map[string]float64
	// PrimaryWarnings are the primary source's unmatched-row problems.
	PrimaryWarnings []string
	// SecondOpinions are the re-solvable comparison projections.
	SecondOpinions []SecondOpinion
	// SecondWarnings are the second-opinion sources' absence and unmatched
	// problems, kept apart from PrimaryWarnings so the caller can report them
	// in the order the board always has (primary, then Subvertadown, then
	// second opinions).
	SecondWarnings []string
}

// LoadProjections loads and resolves every registered projection source.
//
// normalized resolves a source's file stem to a path; idx ties each row to a
// Sleeper player. A required source that will not load is fatal; an optional
// one degrades to a warning, matching how the board has always treated a
// missing second projection.
func LoadProjections(normalized func(string) string, idx *PlayerIndex) (ProjectionData, error) {
	return loadProjections(ProjectionSources, normalized, idx)
}

// loadProjections is LoadProjections over an explicit source set, so a test
// can drive it without the real registry's filenames.
func loadProjections(sources []ProjectionSource, normalized func(string) string, idx *PlayerIndex) (ProjectionData, error) {
	pd := ProjectionData{Points: map[string]float64{}}
	for _, src := range sources {
		rows, warn, err := loadProjectionRows(normalized(src.File), src)
		if err != nil {
			return ProjectionData{}, err
		}
		if warn != "" {
			pd.appendWarning(src.Role, warn)
		}
		if rows == nil {
			continue // absent optional source
		}
		if bad := idx.Resolve(rows); len(bad) > 0 {
			pd.appendWarning(src.Role, fmt.Sprintf("%d %s rows unmatched", len(bad), src.Label))
		}
		switch src.Role {
		case RolePrimary:
			pd.PrimaryRows = rows
			for _, r := range rows {
				if r.PlayerID == "" || strings.EqualFold(r.Position, "DST") {
					continue
				}
				if src.Include != nil && !src.Include(r) {
					continue
				}
				// A source that ranks further than it projects must not seed the
				// solve with the difference. FantasyPros lists several hundred
				// more players than it projects; each would enter at zero points,
				// leave at the dollar floor, and read as a considered opinion
				// that he is worth a dollar rather than as an absence. Ciely
				// never exposed this — he projects everyone he lists — so the
				// check lived only on the second-opinion branch until a source
				// that needed it became primary.
				if src.RequirePoints && r.Points <= 0 {
					continue
				}
				pd.Points[r.PlayerID] = r.Points
				// The band comes with him. Carrying it only on the second-opinion
				// branch was harmless while the primary published no range, but a
				// primary that does would have had it dropped here — and the band
				// is what PriceBand turns into the spread the board shows instead
				// of a bare number.
				pd.Projections = append(pd.Projections, Projection{
					PlayerID: r.PlayerID, Name: r.Player, Position: r.Position,
					Points: r.Points, PointsLow: r.PointsLow, PointsHigh: r.PointsHigh,
				})
			}
		case RoleSecondOpinion:
			so := SecondOpinion{Name: src.Name, Rank: map[string]int{}, Sharp: map[string]int{}}
			for _, r := range rows {
				if r.PlayerID == "" || strings.EqualFold(r.Position, "DST") {
					continue
				}
				if src.Include != nil && !src.Include(r) {
					continue
				}
				// The rank survives even where the projection does not: a
				// player FantasyPros ranks but does not project still has an
				// ECR, and that is a real read worth keeping.
				so.Rank[r.PlayerID] = r.PosRank
				if d := r.SharpDelta(); d != 0 {
					so.Sharp[r.PlayerID] = d
				}
				if src.RequirePoints && r.Points <= 0 {
					continue
				}
				so.Projections = append(so.Projections, Projection{
					PlayerID: r.PlayerID, Name: r.Player, Position: r.Position,
					Points: r.Points, PointsLow: r.PointsLow, PointsHigh: r.PointsHigh,
				})
			}
			// A source can be present as a file and still say nothing. The two
			// sharp subsets share one file with consensus and are selected by a
			// baseline column, so an export taken without them leaves this loop
			// having matched no rows at all. Emitting that as a second opinion
			// would put an empty column on the board, which reads as "the sharps
			// have no view on anyone" rather than "the sharps were not exported".
			if len(so.Projections) == 0 && len(so.Rank) == 0 {
				pd.SecondWarnings = append(pd.SecondWarnings,
					fmt.Sprintf("%s source absent — %s", src.Label, src.AbsentNote))
				continue
			}
			pd.SecondOpinions = append(pd.SecondOpinions, so)
		}
	}
	return pd, nil
}

func (pd *ProjectionData) appendWarning(role ProjectionRole, w string) {
	if role == RolePrimary {
		pd.PrimaryWarnings = append(pd.PrimaryWarnings, w)
		return
	}
	pd.SecondWarnings = append(pd.SecondWarnings, w)
}

// loadProjectionRows loads one source's rows, turning an optional source's
// absence or unreadability into a warning rather than an error. Returns nil
// rows with no error when an optional source is simply absent.
func loadProjectionRows(path string, src ProjectionSource) ([]SourceRow, string, error) {
	if !src.Required {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Sprintf("%s source absent — %s", src.Label, src.AbsentNote), nil
		}
	}
	rows, err := LoadSourceCSV(path, src.Schema)
	if err != nil {
		if src.Required {
			return nil, "", err
		}
		return nil, fmt.Sprintf("%s source unreadable: %v", src.Label, err), nil
	}
	return rows, "", nil
}

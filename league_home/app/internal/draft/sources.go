package draft

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Basis records the league assumptions a source's numbers were produced
// under. Values are meaningless without it: a $40 auction value from a
// 10-team full-PPR sheet is not the same $40 as one from a 12-team
// half-PPR sheet, and blending them without rescaling silently corrupts
// the board.
type Basis struct {
	// Reception is points per reception (0.5 for this league).
	Reception float64
	// Interception is points per interception thrown.
	Interception float64
	Teams        int
	Budget       int
	// Starters maps position to starting slots, flex excluded.
	Starters map[string]int
	Flex     int
}

// HitOrMiss returns the league's own basis, the target every source is
// rescaled to.
func HitOrMiss() Basis {
	return Basis{
		Reception: 0.5, Interception: -1, Teams: 12, Budget: 200,
		Starters: map[string]int{"QB": 1, "RB": 2, "WR": 3, "TE": 1, "DEF": 1},
		Flex:     1,
	}
}

// Pool is the total auction money in the room.
func (b Basis) Pool() int { return b.Teams * b.Budget }

// Matches reports whether another basis needs no rescaling to be comparable.
func (b Basis) Matches(other Basis) bool {
	return b.Reception == other.Reception && b.Teams == other.Teams &&
		b.Budget == other.Budget && b.Flex == other.Flex &&
		starterKey(b.Starters) == starterKey(other.Starters)
}

func starterKey(s map[string]int) string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s%d", k, s[k])
	}
	return sb.String()
}

// SourceRow is one player's numbers from one source, after normalization.
type SourceRow struct {
	Source   string
	Player   string
	Position string
	Team     string
	PosRank  int
	// Baseline distinguishes valuations a source publishes more than one
	// of — Subvertadown ships "beer", "beerplus", and "vols" for the same
	// player, and the spread between them is itself a signal.
	Baseline string
	// Points is the projection restated in league scoring where the source
	// published enough components to do so, otherwise as published.
	Points float64
	// PointsLow and PointsHigh are the low and high of the source's own
	// projection, restated the same way. Zero where the source publishes a
	// single number, which is every source but FantasyPros.
	PointsLow  float64
	PointsHigh float64
	// ECRvsADP is how far expert consensus rank sits from where the market
	// actually drafts him, positive where the experts rate him above his draft
	// slot. Zero also means "not published".
	//
	// Parsed and deliberately unused. It looks like a discount signal that owes
	// nothing to injury, and it is not one here: measured against adjusted edge
	// across the 2026 pre-draft board it came out at r = +0.01, with the
	// experts-above-market bucket sitting a dollar on the wrong side. ADP is a
	// snake-draft measure and this league is an auction, so how late a player
	// goes says little about what he costs — AAV is the market price, and the
	// board already reads it. Kept so the column round-trips and so the next
	// person to notice it can read this instead of measuring it again.
	ECRvsADP int
	// AuctionValue is the source's dollar value, rescaled to the league's
	// pool.
	AuctionValue float64
	// AAV is the average auction value from real human drafts, where the
	// source publishes one. This is market data, not a model output, and
	// is never rescaled with the model values.
	AAV float64
	// ScarcityPct is Subvertadown's PS%: the fraction of positive value
	// remaining at the position after this player is drafted. It is not a
	// point total and must never be rendered as a loss.
	ScarcityPct float64
	// ECRUp and ECRDown are independent. A player some experts think will
	// beat consensus and others think will miss it carries both, which
	// marks him contested rather than cancelling out.
	ECRUp, ECRDown bool
	// RankVsTop10 and RankVsTop20 are how far the most-accurate-expert
	// subsets move a player against the full consensus: consensus rank minus
	// the sharp rank, so a positive value means the sharps rank him higher
	// (a better, lower number) than the field does. Present only on FantasyPros
	// consensus rows, and only for players the subset also ranked; zero on
	// every other source, which reads as "no sharp signal".
	//
	// Distinct from ECRUp/ECRDown on purpose: those are Subvertadown's
	// industry-deviation flags, this is the FantasyPros sharp-expert
	// divergence. Never conflate the two.
	RankVsTop10, RankVsTop20 int
	// PlayerID is the resolved Sleeper ID, empty until Resolve runs.
	PlayerID string
	// Components is the projection broken into the pieces it was built
	// from, where the source publishes them. Present for Ciely, absent for
	// sources that ship a point total alone.
	//
	// Worth carrying because the pieces say what kind of player he is in a
	// way the total cannot: two backs projected for the same points are
	// different propositions when one gets there on receptions and the
	// other on touchdowns.
	Components Components
}

// Components is a projection's constituent parts.
type Components struct {
	// HasParts reports whether the source published enough to decompose.
	HasParts bool

	PassYards, PassTD, Interceptions float64
	RushYards, RushTD                float64
	Targets, Receptions              float64
	RecvYards, RecvTD                float64
}

// TouchdownPoints is what the projection earns from scoring plays, in this
// league's scoring.
func (c Components) TouchdownPoints() float64 {
	return c.PassTD*4 + c.RushTD*6 + c.RecvTD*6
}

// ReceptionPoints is what it earns from catches alone, at half a point each.
func (c Components) ReceptionPoints() float64 { return c.Receptions * 0.5 }

// YardagePoints is what it earns from yards.
func (c Components) YardagePoints() float64 {
	return c.PassYards/25 + c.RushYards/10 + c.RecvYards/10
}

// Total is the projection rebuilt from its parts. It reconstructs Ciely's
// published league points exactly, which is the check that the scoring
// applied here matches the league's.
func (c Components) Total() float64 {
	return c.TouchdownPoints() + c.ReceptionPoints() + c.YardagePoints() - c.Interceptions
}

// ECRState summarizes the two ECR flags into the four cases that matter.
type ECRState string

const (
	// ECRUnknown is the zero value: no source covered this player at all.
	// Deliberately distinct from consensus — "nobody looked" and "everyone
	// agrees" are different claims, and conflating them would dress up
	// missing data as confidence.
	ECRUnknown ECRState = ""
	// ECRConsensus means a source covered him and no expert flagged a
	// significant deviation.
	ECRConsensus ECRState = "consensus"
	// ECRUpside means experts see the player beating consensus.
	ECRUpside ECRState = "upside"
	// ECRDownside means experts see the player missing consensus.
	ECRDownside ECRState = "downside"
	// ECRContested means experts disagree in both directions — the
	// price is volatile and deserves a hard ceiling rather than a
	// different midpoint.
	ECRContested ECRState = "contested"
)

// SharpDelta is the sharp-expert divergence to act on: the larger-magnitude
// of the top-10 and top-20 moves, keeping its sign. Top-10 and top-20 usually
// agree in direction, so the larger move is the stronger reading; when only
// one subset ranked the player the other is zero and does not win. Negative
// means the sharps rank him better than the full field.
func (r SourceRow) SharpDelta() int {
	if abs(r.RankVsTop10) >= abs(r.RankVsTop20) {
		return r.RankVsTop10
	}
	return r.RankVsTop20
}

// ECR reports which of the four deviation states the row is in.
func (r SourceRow) ECR() ECRState {
	switch {
	case r.ECRUp && r.ECRDown:
		return ECRContested
	case r.ECRUp:
		return ECRUpside
	case r.ECRDown:
		return ECRDownside
	}
	return ECRConsensus
}

// SourceSchema names the columns a source must carry.
//
// Per source rather than one shared list, because the two normalized files
// hold different things: Ciely publishes a projection and a dollar value,
// Subvertadown a baseline label and a market AAV. A list broad enough for
// both would demand columns neither has.
type SourceSchema struct {
	// Name is the source, for the error message.
	Name string
	// Required is one entry per column that must exist, each listing the
	// spellings that count. The spellings mirror what pick() accepts, so
	// this check can never reject a header the parser would have read.
	Required [][]string
}

// CielyColumns and SubvertadownColumns are what each extractor's output has
// to carry for the board to mean anything.
//
// Only the columns whose absence would corrupt a number, not every column
// the files happen to have. Losing ps_pct or an ECR flag costs a signal and
// is survivable; losing the value column produces a board priced entirely
// at zero that still renders and still looks like a board.
var (
	CielyColumns = SourceSchema{
		Name: "ciely",
		Required: [][]string{
			{"player", "name", "player name"},
			{"position", "pos"},
			{"league_points", "points", "fps", "proj"},
			{"auction_value", "auc$", "value", "auction"},
		},
	}
	SubvertadownColumns = SourceSchema{
		Name: "subvertadown",
		Required: [][]string{
			{"player", "name", "player name"},
			{"position", "pos"},
			{"baseline"},
			{"aav"},
			{"value", "auction_value", "auc$", "auction"},
		},
	}
	// FantasyProsColumns is what the FantasyPros ECR extract must carry.
	//
	// It ships no auction value — it is a ranking source recomputed into a
	// projection — so the column that would corrupt a number if missing is
	// league_points, not a dollar value. The rank, tier, and expert-spread
	// columns ride along for the board but their absence only costs a signal,
	// so they are not required here (see the note on CielyColumns).
	FantasyProsColumns = SourceSchema{
		Name: "fantasypros",
		Required: [][]string{
			{"player", "name", "player name"},
			{"position", "pos"},
			{"league_points", "points", "fps", "proj"},
		},
	}
)

// check reports the first required column the header does not satisfy.
func (s SourceSchema) check(header []string, cols map[string]int) error {
	for _, accepted := range s.Required {
		found := false
		for _, name := range accepted {
			if _, ok := cols[name]; ok {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is missing a %s column (have %v)",
				s.Name, strings.Join(accepted, "/"), header)
		}
	}
	return nil
}

// LoadSourceCSV reads a normalized source CSV and checks it carries what the
// schema requires. Column order is read from the header rather than assumed,
// because these files are produced by several different extractors and
// hand-edited between drafts.
func LoadSourceCSV(path string, schema SourceSchema) ([]SourceRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("draft: opening source %s: %w", path, err)
	}
	defer f.Close()
	rows, err := ParseSourceCSVAs(f, schema)
	if err != nil {
		return nil, fmt.Errorf("draft: %s: %w", path, err)
	}
	return rows, nil
}

// ParseSourceCSV reads rows with no schema check, for callers that only
// want whatever the file happens to hold.
func ParseSourceCSV(r io.Reader) ([]SourceRow, error) {
	return ParseSourceCSVAs(r, SourceSchema{})
}

// ParseSourceCSVAs reads normalized source rows, rejecting a file that does
// not carry the columns its schema requires.
//
// Loudly, because the alternative is what this replaced: every column but
// the player name fell through to zero when it was missing, so a renamed
// vendor column became a column of zeros. Nothing downstream can tell a
// real zero from an absent one, and the board renders either way.
func ParseSourceCSVAs(r io.Reader, schema SourceSchema) ([]SourceRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading csv: %w", err)
	}
	if len(records) == 0 {
		// No header at all. With a schema to satisfy there is nothing to
		// check it against, and an empty file is exactly the shape a broken
		// extractor leaves behind.
		if len(schema.Required) > 0 {
			return nil, fmt.Errorf("%s is empty", schema.Name)
		}
		return nil, nil
	}

	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	// Different extractors label the same thing differently.
	pick := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := cols[n]; ok && i < len(row) {
				if v := strings.TrimSpace(row[i]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	if _, ok := cols["player"]; !ok {
		if _, ok := cols["name"]; !ok {
			return nil, fmt.Errorf("no player/name column (have %v)", records[0])
		}
	}
	if err := schema.check(records[0], cols); err != nil {
		return nil, err
	}

	num := func(s string) float64 {
		s = strings.TrimPrefix(strings.TrimSpace(s), "$")
		s = strings.ReplaceAll(s, ",", "")
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}

	var out []SourceRow
	for _, rec := range records[1:] {
		name := pick(rec, "player", "name", "player name")
		if name == "" {
			continue
		}
		rank, _ := strconv.Atoi(pick(rec, "pos_rank", "rank", "rk"))
		truthy := func(s string) bool {
			switch strings.ToLower(s) {
			case "1", "true", "yes", "y":
				return true
			}
			return false
		}
		comp := Components{
			PassYards:     num(pick(rec, "pass_yards")),
			PassTD:        num(pick(rec, "pass_td")),
			Interceptions: num(pick(rec, "interceptions")),
			RushYards:     num(pick(rec, "rush_yards")),
			RushTD:        num(pick(rec, "rush_td")),
			Targets:       num(pick(rec, "targets")),
			Receptions:    num(pick(rec, "receptions")),
			RecvYards:     num(pick(rec, "recv_yards")),
			RecvTD:        num(pick(rec, "recv_td")),
		}
		// A source that publishes none of these ships a total alone, and
		// zeroed parts would read as a player who does nothing.
		comp.HasParts = comp.PassYards+comp.RushYards+comp.RecvYards+comp.Receptions > 0

		out = append(out, SourceRow{
			Components:   comp,
			Source:       pick(rec, "source"),
			Player:       name,
			Position:     strings.ToUpper(pick(rec, "position", "pos")),
			Team:         strings.ToUpper(pick(rec, "team", "tm")),
			PosRank:      rank,
			Baseline:     strings.ToLower(pick(rec, "baseline")),
			Points:       num(pick(rec, "league_points", "points", "fps", "proj")),
			PointsLow:    num(pick(rec, "points_low")),
			ECRvsADP:     signedInt(pick(rec, "ecr_vs_adp")),
			PointsHigh:   num(pick(rec, "points_high")),
			AuctionValue: num(pick(rec, "auction_value", "auc$", "value", "auction")),
			AAV:          num(pick(rec, "aav")),
			ScarcityPct:  num(pick(rec, "ps_pct", "ps")),
			ECRUp:        truthy(pick(rec, "ecr_up")),
			ECRDown:      truthy(pick(rec, "ecr_down")),
			RankVsTop10:  signedInt(pick(rec, "rank_vs_top10")),
			RankVsTop20:  signedInt(pick(rec, "rank_vs_top20")),
		})
	}
	return out, nil
}

// Rescale converts a source's auction values from its own basis to the
// league's, by pool ratio. Returns the multiplier applied so reports can
// show it — a silent rescale is worse than none.
func Rescale(rows []SourceRow, from, to Basis) float64 {
	if from.Pool() == 0 || from.Pool() == to.Pool() {
		return 1
	}
	factor := float64(to.Pool()) / float64(from.Pool())
	for i := range rows {
		rows[i].AuctionValue *= factor
	}
	return factor
}

// signedInt parses an optionally-signed integer, returning 0 for an empty or
// unparseable field. Used for the sharp-divergence columns, which are blank
// on rows the subset did not rank and can carry a negative move.
func signedInt(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// NormalizeName is the exported form of normalizeName, for callers that
// need to key their own maps the same way.
func NormalizeName(s string) string { return normalizeName(s) }

// normalizeName reduces a display name to Sleeper's search_full_name form:
// lowercase, letters and digits only. This collapses "A.J. Brown" and "AJ
// Brown", and strips the punctuation in "Ja'Marr" and "Smith-Njigba".
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nameSuffixes are dropped when a direct match fails, since sources
// disagree about whether to include them.
//
// A bare "v" is deliberately absent — real
// surnames end in v (Popov, Asiasi) and stripping it would create false
// matches, which are worse than the rare missed "V" suffix an alias can
// cover instead.
var nameSuffixes = []string{"iii", "iv", "ii", "jr", "sr"}

// stemName normalizes a display name with a trailing generational suffix
// dropped, treating it as a word rather than as trailing letters.
//
// The difference is not academic. Normalizing first glues the suffix to the
// surname, so "Kyle Monangai II" becomes kylemonangaiii — which ends in
// "iii" and stripped to "kylemonanga", a player who does not exist. Rasheen
// Ali and Mike Gesicki have the same problem, and every one of them was
// unresolvable by his own full name.
//
// Matching a whole token cannot make that mistake: "Monangai" is not a
// suffix, and "II" is, whatever the surname before it happens to end with.
func stemName(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) > 1 {
		last := strings.ToLower(strings.TrimSuffix(fields[len(fields)-1], "."))
		for _, suffix := range nameSuffixes {
			if last == suffix {
				return normalizeName(strings.Join(fields[:len(fields)-1], " "))
			}
		}
	}
	return normalizeName(raw)
}

// stripSuffix is the string-ending form, kept for the one caller that has
// only a normalized key and no name left to split. Prefer stemName.
func stripSuffix(normalized string) string {
	for _, suffix := range nameSuffixes {
		if strings.HasSuffix(normalized, suffix) && len(normalized) > len(suffix)+3 {
			return strings.TrimSuffix(normalized, suffix)
		}
	}
	return normalized
}

// Aliases maps a normalized source name to a Sleeper player ID, for the
// names no amount of normalization will reconcile — nicknames, mostly.
// Sources say "Kenneth Gainwell" and "Hollywood Brown" where Sleeper says
// "Kenny Gainwell" and "Marquise Brown".
type Aliases map[string]string

// LoadAliases reads name aliases from a CSV with the header
// source_name,player_id,note. A missing file is not an error.
func LoadAliases(path string) (Aliases, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Aliases{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("draft: opening aliases %s: %w", path, err)
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("draft: reading aliases %s: %w", path, err)
	}

	out := Aliases{}
	if len(records) == 0 {
		return out, nil
	}
	cols := map[string]int{}
	for i, h := range records[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	nameCol, ok1 := cols["source_name"]
	idCol, ok2 := cols["player_id"]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("draft: aliases %s needs source_name and player_id columns", path)
	}
	for n, rec := range records[1:] {
		if len(rec) <= nameCol || len(rec) <= idCol {
			continue
		}
		name, id := strings.TrimSpace(rec[nameCol]), strings.TrimSpace(rec[idCol])
		if name == "" || id == "" {
			return nil, fmt.Errorf("draft: aliases %s line %d: both columns are required", path, n+2)
		}
		out[normalizeName(name)] = id
	}
	return out, nil
}

// PlayerIndex resolves source names to Sleeper player IDs.
type PlayerIndex struct {
	// aliases short-circuit name lookup for known nicknames.
	aliases Aliases
	// byName maps a normalized name to candidate player IDs. A name can
	// be ambiguous — there are two Kenneth Walkers — so position and team
	// break ties.
	byName map[string][]indexed
	// byTeam maps a team abbreviation to its DEF player ID, since team
	// defenses carry no usable name in Sleeper's dictionary.
	byTeam map[string]string
}

type indexed struct {
	id, position, team string
}

// BuildPlayerIndex indexes Sleeper's player dictionary for name lookup.
func BuildPlayerIndex(players map[string]PlayerInfo) *PlayerIndex {
	return BuildPlayerIndexWithAliases(players, nil)
}

// BuildPlayerIndexWithAliases is BuildPlayerIndex plus a nickname table.
func BuildPlayerIndexWithAliases(players map[string]PlayerInfo, aliases Aliases) *PlayerIndex {
	idx := &PlayerIndex{aliases: aliases, byName: map[string][]indexed{}, byTeam: map[string]string{}}
	for id, p := range players {
		if strings.EqualFold(p.Position, "DEF") {
			// Sleeper keys team defenses by team abbreviation and gives
			// them no full_name, so they can only be found by team.
			if p.Team != "" {
				idx.byTeam[strings.ToUpper(p.Team)] = id
			}
			continue
		}
		n := normalizeName(p.Name)
		if n == "" {
			continue
		}
		e := indexed{id: id, position: strings.ToUpper(p.Position), team: strings.ToUpper(p.Team)}
		idx.byName[n] = append(idx.byName[n], e)
		if s := stemName(p.Name); s != n {
			idx.byName[s] = append(idx.byName[s], e)
		}
	}
	return idx
}

// Unmatched is a source row that could not be tied to a Sleeper player.
type Unmatched struct {
	Row    SourceRow
	Reason string
}

// Resolve fills in PlayerID on each row, returning the rows that could not
// be matched.
//
// Unmatched rows are returned rather than dropped: a name this misses is a
// player silently missing from the draft board, which is the exact failure
// nobody notices until the player is nominated.
func (idx *PlayerIndex) Resolve(rows []SourceRow) []Unmatched {
	var unmatched []Unmatched
	for i := range rows {
		row := &rows[i]
		if strings.EqualFold(row.Position, "DST") || strings.EqualFold(row.Position, "DEF") {
			if id, ok := idx.byTeam[teamOfDefense(*row)]; ok {
				row.PlayerID = id
				continue
			}
			unmatched = append(unmatched, Unmatched{*row, "no team defense for that abbreviation"})
			continue
		}

		n := normalizeName(row.Player)
		if id, ok := idx.aliases[n]; ok {
			row.PlayerID = id
			continue
		}
		cands := idx.byName[n]
		if len(cands) == 0 {
			cands = idx.byName[stemName(row.Player)]
		}
		switch len(cands) {
		case 0:
			unmatched = append(unmatched, Unmatched{*row, "no player with that name"})
		case 1:
			row.PlayerID = cands[0].id
		default:
			if id, ok := disambiguate(cands, *row); ok {
				row.PlayerID = id
			} else {
				unmatched = append(unmatched, Unmatched{*row,
					fmt.Sprintf("%d players share that name and position/team did not resolve it", len(cands))})
			}
		}
	}
	return unmatched
}

// teamOfDefense finds the team abbreviation for a defense row, which
// sources label inconsistently (the team column, or the name itself).
func teamOfDefense(row SourceRow) string {
	if row.Team != "" {
		return strings.ToUpper(row.Team)
	}
	fields := strings.Fields(row.Player)
	if len(fields) > 0 {
		return strings.ToUpper(fields[len(fields)-1])
	}
	return ""
}

// disambiguate picks among same-named players using position, then team.
func disambiguate(cands []indexed, row SourceRow) (string, bool) {
	var byPos []indexed
	for _, c := range cands {
		if c.position == row.Position {
			byPos = append(byPos, c)
		}
	}
	if len(byPos) == 1 {
		return byPos[0].id, true
	}
	if len(byPos) == 0 {
		byPos = cands
	}
	if row.Team != "" {
		var byTeam []indexed
		for _, c := range byPos {
			if c.team == row.Team {
				byTeam = append(byTeam, c)
			}
		}
		if len(byTeam) == 1 {
			return byTeam[0].id, true
		}
	}
	return "", false
}

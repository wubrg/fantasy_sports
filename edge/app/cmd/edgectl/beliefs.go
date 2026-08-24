package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"edge/internal/betlog"
	"edge/internal/board"
	"edge/internal/scenario"
)

// inputPack is the facts a forecaster was shown. Written by beliefpack.py.
type inputPack struct {
	Season      int                            `json:"season"`
	Week        int                            `json:"week"`
	Definitions map[string]scenario.Definition `json:"definitions"`
	BaseRates   map[string]float64             `json:"base_rates"`
	Games       []packGame                     `json:"games"`
}

type packGame struct {
	GameID     string              `json:"game_id"`
	Away       string              `json:"away"`
	Home       string              `json:"home"`
	Kickoff    time.Time           `json:"kickoff"`
	TotalLine  *float64            `json:"total_line"`
	SpreadLine *float64            `json:"spread_line"`
	Teams      map[string]packTeam `json:"teams"`
}

type packTeam struct {
	PriorForm *struct {
		SuccessRatePrior float64 `json:"success_rate_prior"`
		OffensePrior     float64 `json:"offense_prior"`
		PriorGames       int     `json:"prior_games"`
	} `json:"prior_form"`
}

// outcomePack is what happened. Written by beliefpack.py outcomes.
type outcomePack struct {
	Season      int                            `json:"season"`
	Week        int                            `json:"week"`
	Definitions map[string]scenario.Definition `json:"definitions"`
	Rows        []outcomeRow                   `json:"rows"`
}

type outcomeRow struct {
	GameID   string `json:"game_id"`
	Team     string `json:"team"`
	Scenario string `json:"scenario"`
	Status   string `json:"status"` // settled | pending | unavailable
	Occurred bool   `json:"occurred"`
	Reason   string `json:"reason"`
}

// forecast is what the prompt emits.
type forecastFile struct {
	Season       int        `json:"season"`
	Week         int        `json:"week"`
	InputPack    string     `json:"input_pack"`
	InputPackSHA string     `json:"input_pack_sha256"`
	Model        string     `json:"model"`
	Prompt       string     `json:"prompt"`
	GeneratedAt  time.Time  `json:"generated_at"`
	Predictions  []forecast `json:"predictions"`
	Flagged      []string   `json:"flagged"`
}

type forecast struct {
	GameID     string   `json:"game_id"`
	Team       string   `json:"team"`
	Scenario   string   `json:"scenario"`
	Belief     float64  `json:"belief"`
	Confidence float64  `json:"confidence"`
	Abstained  bool     `json:"abstained"`
	Claims     []string `json:"claims"`
	Rejected   bool     `json:"rejected"`
	Reason     string   `json:"rejected_reason"`
}

func beliefsCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: edgectl beliefs ingest|settle|list|score")
	}
	switch args[0] {
	case "ingest":
		return beliefsIngest(args[1:])
	case "settle":
		return beliefsSettle(args[1:])
	case "list":
		return beliefsList(args[1:])
	case "score":
		return beliefsScore(args[1:])
	}
	return fmt.Errorf("edgectl beliefs: unknown subcommand %q (want ingest, settle, list or score)", args[0])
}

func readJSON(path string, v any, strict bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("beliefs: opening %s: %w", path, err)
	}
	defer f.Close()
	d := json.NewDecoder(f)
	if strict {
		// A forecaster that starts volunteering a price or a player name has
		// drifted off contract. Ignoring the field would hide the drift, which
		// is the failure this whole exercise is built around.
		d.DisallowUnknownFields()
	}
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("beliefs: reading %s: %w", path, err)
	}
	return nil
}

func fileSHA(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("beliefs: hashing %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// checkDefinitions refuses a pack whose thresholds disagree with the grid.
//
// Same fail-closed discipline as CheckDefinition: a scenario redefined between
// the pack and the binary is not a smaller measurement, it is a different one,
// and it must be an error rather than a silent substitution.
func checkDefinitions(packed map[string]scenario.Definition) error {
	c, err := scenario.LoadConditionals()
	if err != nil {
		return err
	}
	for name, want := range packed {
		got, ok := c.Definitions[name]
		if !ok {
			return fmt.Errorf(
				"beliefs: the pack defines %q but this grid does not know it", name)
		}
		if got.Basis != want.Basis || got.Op != want.Op || got.Threshold != want.Threshold {
			return fmt.Errorf(
				"beliefs: %q is %s in the pack but %s in this grid — the pack was built "+
					"against a different definition, and scoring it here would measure "+
					"something else", name, want, got)
		}
	}
	return nil
}

func beliefsIngest(args []string) error {
	fs := flag.NewFlagSet("beliefs ingest", flag.ExitOnError)
	file := fs.String("file", "", "the forecaster's predictions JSON")
	packPath := fs.String("pack", "", "the input pack it was shown")
	logPath := fs.String("log", "beliefs/log.jsonl", "the belief log to append to")
	dropLate := fs.Bool("drop-late", false,
		"drop predictions made after kickoff instead of refusing the whole file")
	dry := fs.Bool("n", false, "print the plan and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" || *packPath == "" {
		return fmt.Errorf("beliefs ingest: -file and -pack are both required")
	}

	var pk inputPack
	if err := readJSON(*packPath, &pk, false); err != nil {
		return err
	}
	if err := checkDefinitions(pk.Definitions); err != nil {
		return err
	}
	var ff forecastFile
	if err := readJSON(*file, &ff, true); err != nil {
		return err
	}

	// The pack hash binds the forecast to the exact bytes it saw. Paths are
	// mutable; content is not.
	sum, err := fileSHA(*packPath)
	if err != nil {
		return err
	}
	if ff.InputPackSHA != sum {
		return fmt.Errorf(
			"beliefs: the forecast claims pack sha %s but %s hashes to %s — it was written "+
				"against different facts than the ones on disk",
			short(ff.InputPackSHA), *packPath, short(sum))
	}
	if ff.Season != pk.Season || ff.Week != pk.Week {
		return fmt.Errorf("beliefs: the forecast is for %d week %d, the pack for %d week %d",
			ff.Season, ff.Week, pk.Season, pk.Week)
	}

	byGame := map[string]packGame{}
	for _, g := range pk.Games {
		byGame[g.GameID] = g
	}
	c, err := scenario.LoadConditionals()
	if err != nil {
		return err
	}
	bel, err := scenario.LoadBelief()
	if err != nil {
		return err
	}

	now := time.Now()
	var ready []betlog.Prediction
	var late []string
	seen := map[string]bool{}

	for _, p := range ff.Predictions {
		g, ok := byGame[p.GameID]
		if !ok {
			return fmt.Errorf("beliefs: %s is not a game in this pack", p.GameID)
		}
		def, ok := pk.Definitions[p.Scenario]
		if !ok {
			return fmt.Errorf("beliefs: %q is not a scenario in this pack", p.Scenario)
		}
		team := board.CanonicalTeam(p.Team)
		unit := "team"
		if def.Basis == "total" {
			unit = "game"
		}
		if unit == "game" && team != "" {
			return fmt.Errorf(
				"beliefs: %s is a property of the game, but %s carries team %q",
				p.Scenario, p.GameID, p.Team)
		}
		if unit == "team" {
			if team != board.CanonicalTeam(g.Away) && team != board.CanonicalTeam(g.Home) {
				return fmt.Errorf("beliefs: %q is not a side of %s (%s at %s)",
					p.Team, p.GameID, g.Away, g.Home)
			}
		}
		key := p.GameID + "/" + team + "/" + p.Scenario
		if seen[key] {
			return fmt.Errorf(
				"beliefs: %s appears twice in this file — a second opinion on the same claim "+
					"is exactly the hindsight this log exists to prevent", key)
		}
		seen[key] = true

		pred := betlog.Prediction{
			Season: pk.Season, Week: pk.Week,
			GameID: p.GameID, Team: team, Scenario: p.Scenario,
			Belief: p.Belief, Confidence: p.Confidence,
			Source: "prompt", Model: ff.Model, Prompt: ff.Prompt,
			InputPack: *packPath, InputPackSHA: sum,
			Kickoff: g.Kickoff, GeneratedAt: ff.GeneratedAt,
			Rejected: p.Rejected, RejectedReason: p.Reason, Claims: p.Claims,
		}
		freezeReferences(&pred, pk, g, c, bel, p.Abstained)

		if err := pred.Validate(); err != nil {
			return err
		}
		if !ff.GeneratedAt.Before(g.Kickoff) || !now.Before(g.Kickoff) {
			late = append(late, key)
			continue
		}
		ready = append(ready, pred)
	}

	if len(late) > 0 && !*dropLate {
		return fmt.Errorf(
			"beliefs: %d of %d predictions were made or ingested after kickoff (%s). "+
				"A systematically late file is a broken process, not a data point — pass "+
				"-drop-late if you mean to keep the rest, and the dropped count is recorded",
			len(late), len(ff.Predictions), strings.Join(firstN(late, 3), ", "))
	}

	fmt.Printf("BELIEFS  %d week %d  from %s\n", pk.Season, pk.Week, ff.Model)
	fmt.Printf("  pack     %s  (sha %s)\n", *packPath, short(sum))
	fmt.Printf("  ready    %d\n", len(ready))
	if len(late) > 0 {
		fmt.Printf("  DROPPED  %d made after kickoff\n", len(late))
	}
	if *dry {
		fmt.Printf("\n-n: nothing written\n")
		return nil
	}
	for _, p := range ready {
		if _, err := betlog.Record(*logPath, p, now); err != nil {
			return err
		}
	}
	fmt.Printf("  wrote    %s\n", *logPath)
	return nil
}

// freezeReferences copies onto the record the numbers this forecast will be
// judged against.
//
// Frozen rather than recomputed at scoring time, because `make fit` rewrites
// belief.json: a reference derived later would let a mid-season refit change
// the opponent retroactively, with nothing to show it had happened.
func freezeReferences(p *betlog.Prediction, pk inputPack, g packGame,
	c *scenario.Conditionals, bel *scenario.Belief, abstained bool) {
	if br, ok := pk.BaseRates[p.Scenario]; ok {
		p.SBaseRate = &br
	}
	def := c.Definitions[p.Scenario]

	// The market's own view, converted here and only here. spread_line is the
	// HOME team's expected margin; FromSpread wants the team's own handicap
	// with a favourite negative, so the sign flips once per side.
	switch def.Basis {
	case "total":
		if g.TotalLine != nil {
			if s, err := scenario.FromTotal(p.Scenario, *g.TotalLine, def.Threshold, 0,
				def.Op == "<"); err == nil {
				v := s.Prob
				p.SMarket = &v
			}
		}
	case "margin":
		if g.SpreadLine != nil {
			expected := *g.SpreadLine // home team's expected margin
			if p.Team != board.CanonicalTeam(g.Home) {
				expected = -expected
			}
			if s, err := scenario.FromSpread(p.Scenario, -expected, def.Threshold, 0,
				def.Op == "<"); err == nil {
				v := s.Prob
				p.SMarket = &v
			}
		}
	}

	// The incumbent, where prior form exists. Nil before week 4 rather than a
	// number, because "not measured" is not "measured at zero".
	if t, ok := g.Teams[p.Team]; ok && t.PriorForm != nil {
		var prior float64
		switch def.Basis {
		case "offense_proe":
			prior = t.PriorForm.OffensePrior
		case "success_rate":
			prior = t.PriorForm.SuccessRatePrior
		default:
			prior = 0
		}
		if def.Basis == "offense_proe" || def.Basis == "success_rate" {
			p.PriorForm = &prior
			if band, _, err := bel.Lookup(p.Scenario, prior); err == nil {
				v := band.P
				p.SIncumbent = &v
			}
		}
	}
	if abstained {
		// Recorded in the claims rather than as a field, so the log's shape does
		// not depend on a scoring concept.
		p.Claims = append(p.Claims, "abstained: no read")
	}
}

func beliefsSettle(args []string) error {
	fs := flag.NewFlagSet("beliefs settle", flag.ExitOnError)
	outcomes := fs.String("outcomes", "", "the outcome pack for this week")
	logPath := fs.String("log", "beliefs/log.jsonl", "the belief log")
	dry := fs.Bool("n", false, "print the plan and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outcomes == "" {
		return fmt.Errorf("beliefs settle: -outcomes is required")
	}
	var op outcomePack
	if err := readJSON(*outcomes, &op, false); err != nil {
		return err
	}
	if err := checkDefinitions(op.Definitions); err != nil {
		return err
	}
	preds, err := betlog.LoadPredictions(*logPath)
	if err != nil {
		return err
	}

	type key struct{ game, team, scen string }
	result := map[key]outcomeRow{}
	for _, r := range op.Rows {
		result[key{r.GameID, board.CanonicalTeam(r.Team), r.Scenario}] = r
	}

	var settle, void []betlog.SettledPrediction
	var reasons []string
	var pending, already int
	for _, sp := range preds {
		p := sp.Prediction
		if p.Season != op.Season || p.Week != op.Week {
			continue
		}
		// Idempotence. Without this an accidental second run appends a duplicate
		// settlement, and the log then refuses to load for good.
		if sp.Result != betlog.Open {
			already++
			continue
		}
		r, ok := result[key{p.GameID, p.Team, p.Scenario}]
		if !ok {
			return fmt.Errorf(
				"beliefs: no outcome row for %s — the pack and the log disagree about what "+
					"was scheduled, which must be resolved rather than skipped", p.Key())
		}
		switch r.Status {
		case "settled":
			settle = append(settle, sp)
			reasons = append(reasons, "")
		case "unavailable":
			void = append(void, sp)
			reasons = append(reasons, r.Reason)
		default:
			pending++
		}
	}

	fmt.Printf("SETTLE  %d week %d\n", op.Season, op.Week)
	fmt.Printf("  settle   %d\n  void     %d\n  pending  %d\n  already  %d\n",
		len(settle), len(void), pending, already)
	if *dry {
		fmt.Printf("\n-n: nothing written\n")
		return nil
	}
	for _, sp := range settle {
		r := result[key{sp.Prediction.GameID, sp.Prediction.Team, sp.Prediction.Scenario}]
		if err := betlog.SettleBelief(*logPath, sp.ID, r.Occurred, ""); err != nil {
			return err
		}
	}
	for i, sp := range void {
		if err := betlog.VoidBelief(*logPath, sp.ID, pick(reasons[len(settle)+i], "unmeasurable")); err != nil {
			return err
		}
	}
	return nil
}

func beliefsList(args []string) error {
	fs := flag.NewFlagSet("beliefs list", flag.ExitOnError)
	logPath := fs.String("log", "beliefs/log.jsonl", "the belief log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	preds, err := betlog.LoadPredictions(*logPath)
	if err != nil {
		return err
	}
	if len(preds) == 0 {
		fmt.Printf("no beliefs recorded in %s\n", *logPath)
		return nil
	}
	// The id is truncated because nothing here needs it typed: settlement joins
	// on (game, team, scenario) from the outcome pack. The full value is in the
	// log, which is the record that matters.
	fmt.Printf("%-22s %-18s %-5s %7s %7s %9s  %s\n",
		"game", "scenario", "team", "belief", "ref", "result", "id")
	for _, sp := range preds {
		p := sp.Prediction
		ref := "—"
		if v, ok := reference(p, refAuto); ok {
			ref = fmt.Sprintf("%.3f", v)
		}
		fmt.Printf("%-22s %-18s %-5s %7.3f %7s %9s  %s\n",
			p.GameID, p.Scenario, pick(p.Team, "—"), p.Belief, ref, sp.Result, short(sp.ID))
	}
	return nil
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string{}, s[:n]...), "…")
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "…"
}

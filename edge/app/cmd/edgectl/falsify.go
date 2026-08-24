package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Falsification: checking a forecaster's stated reasons against the facts it
// was given.
//
// The specification asks for claims in the form "<type>: <subject> — <assertion>"
// and says a claim contradicting the cache voids its prediction. Without this,
// `rejected` would only ever be set by the forecaster itself — a prompt marking
// its own homework — and the survivors-versus-all comparison in `beliefs score`
// could never fire.
//
// # What is checked, and what is not
//
// Only `form`, `market` and `schedule`, and only against the PACK. Those are
// the claims where the forecaster was handed the answer, so restating one
// wrongly is not a difference of opinion — it is a fact it had in front of it
// and got wrong.
//
// `usage` and `injury` are checkable in principle, against stats_player_week and
// injuries_*.csv, and are NOT checked here: neither file exists for a season
// that has not started, and a checker that silently passes everything because
// its data is missing is worse than no checker. They are counted as unchecked
// and reported as such.
//
// `personnel` and `narrative` cannot be checked at all — there is no
// depth-chart or coaching table in this repository. They are counted so that a
// prediction resting entirely on them can be flagged.
//
// # The rule is deliberately conservative
//
// A numeric claim is falsified only when it states a number that matches NOTHING
// in the relevant facts, allowing for rounding. "KC's offence has been
// efficient" is never falsified, because it asserts nothing checkable. The cost
// of a false accusation here is high — it would void a prediction and bias the
// survivor set — so the check errs toward letting things through.

var (
	claimRE = regexp.MustCompile(`^\s*([a-z_]+)\s*:\s*([^—–-]+?)\s*[—–]\s*(.+?)\s*$`)
	numRE   = regexp.MustCompile(`[-+]?\d*\.?\d+`)
)

type claim struct {
	kind      string
	subject   string
	assertion string
	raw       string
}

// checkable types are the ones the pack can adjudicate today.
var checkable = map[string]bool{"form": true, "market": true, "schedule": true}

// deferred types are checkable in principle but have no data yet.
var deferred = map[string]bool{"usage": true, "injury": true}

func parseClaim(s string) (claim, bool) {
	m := claimRE.FindStringSubmatch(s)
	if m == nil {
		return claim{raw: s}, false
	}
	return claim{
		kind:      strings.ToLower(m[1]),
		subject:   strings.TrimSpace(m[2]),
		assertion: m[3],
		raw:       s,
	}, true
}

// numbersIn pulls every number out of an assertion.
func numbersIn(s string) []float64 {
	var out []float64
	for _, t := range numRE.FindAllString(s, -1) {
		if v, err := strconv.ParseFloat(t, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// matchesAny reports whether a stated number is a correct rounding of any fact.
//
// Rounding, not equality: a forecaster restating .451 for 0.45123 is right, and
// treating that as a contradiction would make the checker useless. The tolerance
// is derived from the precision the forecaster chose, so a claim of "0.4" is
// held to one decimal and "0.4512" to four.
func matchesAny(stated float64, text string, facts []float64) bool {
	tol := roundingTolerance(stated, text)
	for _, f := range facts {
		if math.Abs(stated-f) <= tol {
			return true
		}
	}
	return false
}

func roundingTolerance(v float64, text string) float64 {
	dp := 0
	for _, t := range numRE.FindAllString(text, -1) {
		if f, err := strconv.ParseFloat(t, 64); err == nil && f == v {
			if i := strings.IndexByte(t, '.'); i >= 0 {
				dp = len(t) - i - 1
			}
			break
		}
	}
	// Half a unit in the last stated place, with a floor so an integer claim is
	// not held to exactness it never implied.
	return math.Max(0.5*math.Pow(10, -float64(dp)), 1e-9)
}

// falsifyResult is what the checker found about one prediction.
type falsifyResult struct {
	Reason       string // non-empty means falsified
	Checked      int    // claims the pack could adjudicate
	Deferred     int    // checkable in principle, no data yet
	Unverifiable int    // personnel and narrative
	Untyped      int    // did not parse
}

// OnlyUnverifiable reports a prediction resting on nothing that can be audited.
func (f falsifyResult) OnlyUnverifiable(total int) bool {
	return total > 0 && f.Checked == 0 && f.Deferred == 0
}

// falsifyPrediction checks one forecast's claims against the pack it was shown.
func falsifyPrediction(p forecast, g packGame, home, away string) falsifyResult {
	var res falsifyResult
	for _, raw := range p.Claims {
		c, ok := parseClaim(raw)
		if !ok {
			res.Untyped++
			continue
		}
		switch {
		case deferred[c.kind]:
			res.Deferred++
			continue
		case !checkable[c.kind]:
			res.Unverifiable++
			continue
		}
		res.Checked++
		if why := adjudicate(c, g, home, away); why != "" && res.Reason == "" {
			res.Reason = why
		}
	}
	return res
}

// adjudicate returns why a claim is false, or "" if it is not.
func adjudicate(c claim, g packGame, home, away string) string {
	subj := strings.ToUpper(strings.TrimSpace(c.subject))

	switch c.kind {
	case "schedule":
		low := strings.ToLower(c.assertion)
		// Only the unambiguous half: which side is at home. Everything else in a
		// schedule claim is prose.
		if strings.Contains(low, "at home") || strings.Contains(low, "home") {
			if subj == strings.ToUpper(away) {
				return fmt.Sprintf("claim %q says %s is at home; the pack has %s at %s",
					c.raw, subj, away, home)
			}
		}
		if strings.Contains(low, "on the road") || strings.Contains(low, "away") {
			if subj == strings.ToUpper(home) {
				return fmt.Sprintf("claim %q says %s is away; the pack has %s hosting",
					c.raw, subj, home)
			}
		}
		return ""

	case "market":
		facts := []float64{}
		if g.TotalLine != nil {
			facts = append(facts, *g.TotalLine)
		}
		if g.SpreadLine != nil {
			// Both signs: the forecaster may state it from either side, and
			// which convention it used is not something to punish.
			facts = append(facts, *g.SpreadLine, -*g.SpreadLine)
		}
		return checkNumbers(c, facts, "the posted total or spread")

	case "form":
		t, ok := g.Teams[subj]
		if !ok || t.PriorForm == nil {
			// No form in the pack means the forecaster was not given any, so a
			// form claim is unsupported rather than contradicted. Reported, not
			// treated as a falsehood.
			return ""
		}
		facts := []float64{
			t.PriorForm.SuccessRatePrior,
			t.PriorForm.OffensePrior,
			float64(t.PriorForm.PriorGames),
		}
		return checkNumbers(c, facts, "this team's prior form in the pack")
	}
	return ""
}

// checkNumbers falsifies a claim whose stated numbers match nothing supplied.
func checkNumbers(c claim, facts []float64, what string) string {
	nums := numbersIn(c.assertion)
	if len(nums) == 0 || len(facts) == 0 {
		return "" // nothing asserted, or nothing to check against
	}
	for _, n := range nums {
		if matchesAny(n, c.assertion, facts) {
			return ""
		}
	}
	return fmt.Sprintf("claim %q states %s, which matches nothing in %s (%s)",
		c.raw, formatNums(nums), what, formatNums(facts))
}

func formatNums(v []float64) string {
	parts := make([]string, 0, len(v))
	for _, f := range v {
		parts = append(parts, strconv.FormatFloat(f, 'g', -1, 64))
	}
	return strings.Join(parts, ", ")
}

// falsifyTally aggregates what the checker found across a week.
//
// The rate is a measurement in its own right, and it is a COVERAGE measurement
// rather than a quality one: a forecaster that is sharp when honest but invents
// a third of its evidence yields few usable candidates a week, and the sample
// this whole exercise depends on is already thin.
type falsifyTally struct {
	Predictions  int
	Falsified    int
	Checked      int
	Deferred     int
	Unverifiable int
	Untyped      int
}

func (t *falsifyTally) add(r falsifyResult, claims int) {
	t.Predictions++
	if r.Reason != "" {
		t.Falsified++
	}
	t.Checked += r.Checked
	t.Deferred += r.Deferred
	t.Unverifiable += r.Unverifiable
	t.Untyped += r.Untyped
}

func (t falsifyTally) report(total, onlyNarrative int) {
	claims := t.Checked + t.Deferred + t.Unverifiable + t.Untyped
	if claims == 0 {
		fmt.Printf("  claims   none stated — nothing to check, and nothing to audit later\n")
		return
	}
	fmt.Printf("  claims   %d checked, %d unverifiable, %d untyped, %d deferred\n",
		t.Checked, t.Unverifiable, t.Untyped, t.Deferred)
	if t.Falsified > 0 {
		fmt.Printf("  FALSIFIED %d of %d predictions contradict the pack they were shown\n",
			t.Falsified, total)
		fmt.Printf("           recorded and settled anyway, so survivors can be scored\n")
		fmt.Printf("           against the whole set on identical outcomes\n")
	}
	if onlyNarrative > 0 {
		fmt.Printf("  NOTE     %d rest entirely on unverifiable claims — not wrong, but\n",
			onlyNarrative)
		fmt.Printf("           they cannot be audited either way\n")
	}
	if t.Deferred > 0 {
		fmt.Printf("  NOTE     usage and injury claims are NOT checked: those files do not\n")
		fmt.Printf("           exist for a season that has not started. A checker that passes\n")
		fmt.Printf("           everything because its data is missing is worse than none\n")
	}
}

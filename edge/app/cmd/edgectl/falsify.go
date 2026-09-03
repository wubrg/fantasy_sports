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
// injuries_*.csv, and are NOT checked here: edgectl reads only the pack, and the
// pack does not carry those facts. Activating them means the pack carrying injury
// report_status and weekly usage -- the same shape as form -- not edgectl reaching
// into the cache. Until then they are counted and reported unchecked, because a
// checker blind to its two declared risk types must say so, not imply coverage.
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
	// A claim is "<type>: <subject> <sep> <assertion>". The separator is an em
	// or en dash, a spaced ASCII hyphen or double hyphen, or a colon -- an LLM
	// writes any of them, and requiring the dash alone left every ASCII-hyphen
	// claim untyped and unchecked. Intra-word hyphens (St-Brown) are safe because
	// the hyphen separator must be spaced.
	claimRE = regexp.MustCompile(`^\s*([a-z_]+)\s*:\s*(.+?)(?:\s*[—–]\s*|\s+--?\s+|\s*:\s*)(.+?)\s*$`)
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

// deferred types are checkable in principle but not against anything edgectl
// holds. It reads only the pack (facts come embedded or pack-supplied, never
// from the raw cache), and the pack carries form, market and schedule but not
// injuries or player usage. So these are deferred not because a season has not
// started but because the FACTS are not in the pack; activating them means the
// pack carrying injury report_status and weekly usage, at which point the check
// is the same shape as form. Until then they are counted and reported unchecked
// rather than passed silently -- a checker blind to its declared risk types must
// say so, not imply coverage it does not have.
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
		why, checked := adjudicate(c, g, home, away)
		// Checked counts claims the pack could ACTUALLY adjudicate, not claims of
		// a checkable type. A form claim about a team with no prior form, or one
		// naming no quantity the pack holds, was not checked -- counting it would
		// overstate coverage and, worse, imply an audit that did not happen.
		if checked {
			res.Checked++
		} else {
			res.Unverifiable++
		}
		if why != "" && res.Reason == "" {
			res.Reason = why
		}
	}
	return res
}

// namedFact ties a quantity's keywords to the pack values it may take.
type namedFact struct {
	re    *regexp.Regexp
	name  string
	value []float64 // acceptable values (spread carries both signs)
}

// adjudicate returns why a claim is false and whether it could be checked at all.
//
// A claim is falsified ONLY when it names a quantity the pack holds and states a
// value that quantity does not take. A number attached to nothing the pack
// knows -- "averaged 27 points", "4 giveaways" -- is unverifiable, not false:
// the old rule falsified any number matching nothing in a three-number bag,
// which rejected true, concrete claims and biased the survivor set toward vague
// ones. checked=false means "no quantity here the pack could rule on".
func adjudicate(c claim, g packGame, home, away string) (why string, checked bool) {
	subj := strings.ToUpper(strings.TrimSpace(c.subject))

	switch c.kind {
	case "schedule":
		return adjudicateSchedule(c, subj, home, away)

	case "market":
		var named []namedFact
		if g.TotalLine != nil {
			named = append(named, namedFact{reTotal, "posted total", []float64{*g.TotalLine}})
		}
		if g.SpreadLine != nil {
			// Both signs: the forecaster may state it from either side, and which
			// convention it used is not something to punish.
			named = append(named, namedFact{reSpread, "posted spread",
				[]float64{*g.SpreadLine, -*g.SpreadLine}})
		}
		return contradiction(c, named)

	case "form":
		t, ok := g.Teams[subj]
		if !ok || t.PriorForm == nil {
			// No form in the pack means the forecaster was not given any, so a
			// form claim is unsupported rather than contradicted, and not checked.
			return "", false
		}
		named := []namedFact{
			{reSuccess, "prior success rate", []float64{t.PriorForm.SuccessRatePrior}},
			{reProe, "prior offensive PROE", []float64{t.PriorForm.OffensePrior}},
			{reGames, "prior games", []float64{float64(t.PriorForm.PriorGames)}},
		}
		return contradiction(c, named)
	}
	return "", false
}

var (
	reTotal   = regexp.MustCompile(`\btotal\b|\bo/?u\b|\bover/under\b`)
	reSpread  = regexp.MustCompile(`\bspread\b|\bline\b|\bfavou?red\b|\bfavou?rite\b|\bunderdog\b|\bdog\b|\blaying\b|\bgetting\b`)
	reSuccess = regexp.MustCompile(`success[ -]?rate|\bsuccess\b`)
	reProe    = regexp.MustCompile(`\bproe\b|pass[ -]?rate[ -]?over[ -]?expected|\bpass[ -]?oe\b`)
	reGames   = regexp.MustCompile(`\bgames?\b`)
	reHome    = regexp.MustCompile(`\bhome\b|\bhosting\b|\bhosts?\b`)
	reAway    = regexp.MustCompile(`\baway\b|\broad\b`)
)

// contradiction falsifies a claim only where a NAMED quantity's stated value
// disagrees with the pack. Each named quantity is checked against the number
// nearest its keyword, so "success rate .45 over 3 games" checks .45 against the
// success rate and 3 against the games count -- and every stated number that
// belongs to a checked quantity must agree, so one true number no longer
// immunises an invented one beside it.
func contradiction(c claim, named []namedFact) (why string, checked bool) {
	low := strings.ToLower(c.assertion)
	for _, nf := range named {
		loc := nf.re.FindStringIndex(low)
		if loc == nil {
			continue
		}
		n, ok := numberNear(c.assertion, loc[0])
		if !ok {
			continue // the quantity is named but carries no number -- unverifiable
		}
		checked = true
		if !matchesAny(n, c.assertion, nf.value) {
			return fmt.Sprintf("claim %q puts %s at %s; the pack has %s",
				c.raw, nf.name, trimNum(n), formatNums(nf.value)), true
		}
	}
	return "", checked
}

// numberNear returns the number closest to a keyword position in the assertion.
func numberNear(assertion string, keyword int) (float64, bool) {
	locs := numRE.FindAllStringIndex(assertion, -1)
	if len(locs) == 0 {
		return 0, false
	}
	best, bestDist := 0, math.MaxInt
	for i, l := range locs {
		d := l[0] - keyword
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist, best = d, i
		}
	}
	v, err := strconv.ParseFloat(assertion[locs[best][0]:locs[best][1]], 64)
	return v, err == nil
}

func trimNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// adjudicateSchedule rules only on which side is home, the one unambiguous fact.
// Away phrases usually contain the word "home" ("away from home", "not at home"),
// so an away reading is taken first and a home reading only in its absence.
func adjudicateSchedule(c claim, subj, home, away string) (string, bool) {
	low := strings.ToLower(c.assertion)
	saysAway := reAway.MatchString(low) || strings.Contains(low, "not at home") ||
		strings.Contains(low, "from home")
	saysHome := !saysAway && reHome.MatchString(low)
	switch {
	case saysHome && subj == strings.ToUpper(away):
		return fmt.Sprintf("claim %q says %s is at home; the pack has %s at %s",
			c.raw, subj, away, home), true
	case saysAway && subj == strings.ToUpper(home):
		return fmt.Sprintf("claim %q says %s is away; the pack has %s hosting",
			c.raw, subj, home), true
	case saysHome || saysAway:
		return "", true // it made a checkable home/away claim, and it was right
	}
	return "", false
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
		fmt.Printf("  NOTE     %d usage/injury claims are NOT checked: the pack does not carry\n", t.Deferred)
		fmt.Printf("           those facts, so edgectl has nothing to rule on. Test A is blind\n")
		fmt.Printf("           to exactly the two types the spec names as the historical risk\n")
	}
}

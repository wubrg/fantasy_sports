// Package oddspull turns a sportsbook's own odds JSON (or a browser HAR
// wrapping it) into priced-ready outcomes. It is the Go port of the oddspull.py
// CLI, so the board server can read a capture without shelling out to Python.
//
// The same two rules as the CLI: it reads a price, it never invents one. An
// object without a valid American price is not an outcome; a body without any
// is not odds. Nothing here returns a zero for a price it could not read.
package oddspull

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"edge/internal/wager"
)

// Outcome is one priceable line: a selection at a price, with the market and
// event it belongs to. MarketID lets the caller pair the two sides of a
// two-sided market (moneyline/spread/total) to de-vig it.
type Outcome struct {
	Event     string
	Market    string
	Selection string
	Line      *float64
	Price     wager.American
	Category  string
	MarketID  string
}

// Parse reads a JSON odds body or a .har wrapping one or more of them and
// returns every outcome it can price. Zero outcomes is an error, not an empty
// slice: a capture with no odds is the wrong capture, and saying so beats
// showing an empty board.
func Parse(data []byte) ([]Outcome, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("not JSON (an HTML block page or the wrong response?): %w", err)
	}

	var out []Outcome
	if bodies := harBodies(root); bodies != nil {
		for _, b := range bodies {
			extractAny(b, &out)
		}
	} else {
		extractAny(root, &out)
	}

	out = dedup(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no betting outcomes found — the capture must contain american odds " +
			"(re-check that the saved response has \"oddsAmerican\"/\"american\")")
	}
	return out, nil
}

// harBodies returns the decoded JSON response bodies of a HAR, or nil if this
// is not a HAR. A HAR is full of images and scripts; only the JSON ones parse,
// and the rest are skipped rather than treated as failures.
func harBodies(root any) []any {
	m, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	log, ok := m["log"].(map[string]any)
	if !ok {
		return nil
	}
	entries, ok := log["entries"].([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, e := range entries {
		ent, ok := e.(map[string]any)
		if !ok {
			continue
		}
		resp, _ := ent["response"].(map[string]any)
		content, _ := resp["content"].(map[string]any)
		text, _ := content["text"].(string)
		if text == "" {
			continue
		}
		if enc, _ := content["encoding"].(string); enc == "base64" {
			dec, err := base64.StdEncoding.DecodeString(text)
			if err != nil {
				continue
			}
			text = string(dec)
		}
		var body any
		if json.Unmarshal([]byte(text), &body) != nil {
			continue // not a JSON response — skip
		}
		out = append(out, body)
	}
	return out
}

// extractAny dispatches on shape: DraftKings' current normalised sportscontent
// API (separate events/markets/selections arrays joined by id) vs the older
// nested shape any generic walker can handle.
func extractAny(body any, out *[]Outcome) {
	if m, ok := body.(map[string]any); ok {
		_, hasSel := m["selections"].([]any)
		_, hasMkt := m["markets"].([]any)
		if hasSel && hasMkt {
			extractDKSportscontent(m, out)
			return
		}
	}
	extractGeneric(body, "", out)
}

// extractDKSportscontent joins the three arrays. selection.marketId -> market
// {name,eventId}; market.eventId -> event.name. Price is displayOdds.american;
// line is points; a player prop names its player in participants.
func extractDKSportscontent(m map[string]any, out *[]Outcome) {
	events := map[string]string{}
	for _, e := range asList(m["events"]) {
		em, _ := e.(map[string]any)
		if id, ok := em["id"].(string); ok {
			events[id] = str(em["name"])
		}
	}
	markets := map[string]map[string]any{}
	for _, mk := range asList(m["markets"]) {
		mm, _ := mk.(map[string]any)
		if id, ok := mm["id"].(string); ok {
			markets[id] = mm
		}
	}
	for _, s := range asList(m["selections"]) {
		sel, ok := s.(map[string]any)
		if !ok {
			continue
		}
		price, ok := americanFromSelection(sel)
		if !ok {
			continue
		}
		marketID := str(sel["marketId"])
		mk := markets[marketID]
		mname := str(mk["name"])
		if mname == "" {
			if mt, ok := mk["marketType"].(map[string]any); ok {
				mname = str(mt["name"])
			}
		}
		event := events[str(mk["eventId"])]

		var line *float64
		if p, ok := toFloat(sel["points"]); ok {
			line = &p
		}
		label := str(sel["label"])
		if player := playerName(sel); player != "" && !strings.Contains(label, player) {
			label = strings.TrimSpace(player + " " + label)
		}
		*out = append(*out, Outcome{
			Event: event, Market: mname, Selection: label,
			Line: line, Price: price, Category: categorise(mname), MarketID: marketID,
		})
	}
}

// extractGeneric walks any nested body for objects that carry an American
// price, tracking the nearest market label so each price keeps its market.
func extractGeneric(obj any, marketLabel string, out *[]Outcome) {
	switch v := obj.(type) {
	case map[string]any:
		if price, ok := firstAmerican(v, priceKeys); ok {
			if label, ok := firstString(v, labelKeys); ok {
				var line *float64
				if f, ok := firstFloat(v, lineKeys); ok {
					line = &f
				}
				*out = append(*out, Outcome{
					Market: marketLabel, Selection: label, Line: line,
					Price: price, Category: categorise(marketLabel),
				})
				return // an outcome has no meaningful children
			}
		}
		next := marketLabel
		if lbl, ok := firstString(v, []string{"label", "name", "marketName", "betOfferType"}); ok {
			next = lbl
		}
		for _, child := range v {
			extractGeneric(child, next, out)
		}
	case []any:
		for _, child := range v {
			extractGeneric(child, marketLabel, out)
		}
	}
}

var (
	priceKeys = []string{"oddsAmerican", "americanOdds", "american", "odds", "price"}
	labelKeys = []string{"label", "name", "outcomeLabel", "participant", "selectionName"}
	lineKeys  = []string{"line", "handicap", "points", "number"}
	amreRe    = regexp.MustCompile(`^[+-]?\d+$`)
)

func americanFromSelection(sel map[string]any) (wager.American, bool) {
	if do, ok := sel["displayOdds"].(map[string]any); ok {
		if a, ok := toAmerican(do["american"]); ok {
			return a, true
		}
	}
	return firstAmerican(sel, priceKeys)
}

func playerName(sel map[string]any) string {
	for _, p := range asList(sel["participants"]) {
		pm, _ := p.(map[string]any)
		if str(pm["type"]) == "Player" {
			return str(pm["name"])
		}
	}
	return ""
}

// toAmerican accepts +150, -110, 150, "150"; rejects anything with |n| < 100
// (a decimal/percentage or junk that is not an American price).
func toAmerican(v any) (wager.American, bool) {
	switch n := v.(type) {
	case float64:
		i := int(n)
		if abs(i) >= 100 {
			return wager.American(i), true
		}
	case string:
		s := strings.ReplaceAll(strings.TrimSpace(n), "−", "-")
		if !amreRe.MatchString(s) {
			return 0, false
		}
		i, err := strconv.Atoi(s)
		if err == nil && abs(i) >= 100 {
			return wager.American(i), true
		}
	}
	return 0, false
}

func categorise(market string) string {
	m := strings.ToLower(market)
	switch {
	case strings.Contains(m, "pass"):
		return "Passing"
	case strings.Contains(m, "rush"):
		return "Rushing"
	case strings.Contains(m, "receiv"), strings.Contains(m, "reception"):
		return "Receiving"
	case strings.Contains(m, "touchdown"), strings.Contains(m, "td scorer"),
		strings.Contains(m, "anytime"), strings.Contains(m, "first td"):
		return "Touchdown"
	case strings.Contains(m, "moneyline"), strings.Contains(m, "spread"),
		strings.Contains(m, "total"), strings.Contains(m, "point"):
		return "Game"
	default:
		return "Other"
	}
}

// ---- small helpers -------------------------------------------------------

func dedup(in []Outcome) []Outcome {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, o := range in {
		line := ""
		if o.Line != nil {
			line = strconv.FormatFloat(*o.Line, 'f', -1, 64)
		}
		key := o.Event + "|" + o.Market + "|" + o.Selection + "|" + line + "|" + strconv.Itoa(int(o.Price))
		if !seen[key] {
			seen[key] = true
			out = append(out, o)
		}
	}
	return out
}

func asList(v any) []any { l, _ := v.([]any); return l }

func str(v any) string { s, _ := v.(string); return s }

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func firstAmerican(m map[string]any, keys []string) (wager.American, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if a, ok := toAmerican(v); ok {
				return a, true
			}
		}
	}
	return 0, false
}

func firstString(m map[string]any, keys []string) (string, bool) {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s, true
		}
	}
	return "", false
}

func firstFloat(m map[string]any, keys []string) (float64, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := toFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

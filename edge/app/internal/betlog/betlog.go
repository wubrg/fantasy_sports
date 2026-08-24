// Package betlog records what you believed before a wager settled, and scores
// it afterwards.
//
// Nothing in the source frameworks does this, and it is the only thing that
// will ever tell you whether the narrative adjustments are worth anything. A
// scenario decomposition turns a bet into an explicit claim -- "this is +EV if
// a shootout is at least 32% likely" -- and a claim that is never checked is
// indistinguishable from a hunch.
//
// # Append-only, deliberately
//
// The file is a JSONL event stream, not a mutable table. Settling a bet appends
// a settlement event rather than rewriting the original record, so a prediction
// cannot be edited after the outcome is known. That is the entire integrity
// property a calibration log needs: hindsight is the failure mode it exists to
// prevent.
package betlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"edge/internal/wager"
)

// Kind discriminates the event types in the stream.
type Kind string

const (
	KindBet Kind = "bet"
	// KindBelief is a claim about a game script with no money on it. It shares
	// this stream because the integrity rules it needs -- append-only, and
	// nothing settled twice -- are the same ones, and a second implementation
	// of them would drift.
	KindBelief Kind = "belief"
	KindSettle Kind = "settle"
)

// Result is how a wager finished.
type Result string

const (
	Open   Result = "open"
	Won    Result = "won"
	Lost   Result = "lost"
	Pushed Result = "push"
	Void   Result = "void"
)

// ParseBankroll maps a recorded bankroll string to the typed value.
//
// Matching used to be a substring test for "bonus", which quietly gave cash
// accounting to anything else -- including "snr", which the CLI accepts as a
// bonus-bet alias. A losing bonus bet costs nothing and a losing cash bet costs
// the stake, so the two differ by the whole stake on every loss.
//
// Only the canonical forms written by wager.Bankroll.String() are accepted.
// Anything else is an error, not a default.
func ParseBankroll(s string) (wager.Bankroll, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "real money":
		return wager.RealMoney, nil
	case "bonus bet":
		return wager.BonusBet, nil
	}
	return wager.RealMoney, fmt.Errorf(
		"betlog: %q is not a recognised bankroll (want %q or %q)",
		s, wager.RealMoney.String(), wager.BonusBet.String())
}

// settleable reports whether a result is one a settlement may record. Load
// rejects anything else rather than silently filing it as excluded.
func (r Result) settleable() bool {
	return r == Won || r == Lost || r == Pushed || r == Void
}

// Counts reports whether a result should be scored. Pushes and voids are
// excluded from calibration: no prediction was tested.
func (r Result) Counts() bool { return r == Won || r == Lost }

// Bet is the prediction, recorded before the outcome is known.
type Bet struct {
	Selection string         `json:"selection"`
	Price     wager.American `json:"price"`
	Book      wager.Book     `json:"book,omitempty"`
	Bankroll  string         `json:"bankroll"`
	Stake     float64        `json:"stake"`

	Scenario       string `json:"scenario,omitempty"`
	ScenarioSource string `json:"scenario_source,omitempty"`

	// ConditionalSource records where q and r came from -- an operator's read
	// or the fitted pooled grid. Kept separate from ScenarioSource because the
	// two are independently right or wrong: you can be good at judging game
	// scripts and bad at judging conditional hit rates, or the reverse, and
	// only separate records can tell you which.
	ConditionalSource string `json:"conditional_source,omitempty"`

	SMarket    float64 `json:"s_market"`
	SYours     float64 `json:"s_yours"`
	Q          float64 `json:"q"`
	R          float64 `json:"r"`
	SStar      float64 `json:"s_star"`
	EdgeSource string  `json:"edge_source,omitempty"`

	// Predicted is the blended P(hit) implied by SYours, Q and R. It is the
	// number calibration is scored against.
	Predicted float64 `json:"predicted"`

	Narrative string `json:"narrative,omitempty"`
}

// Entry is one line of the log.
type Entry struct {
	Kind Kind      `json:"kind"`
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Bet  *Bet      `json:"bet,omitempty"`
	// Prediction is set on a KindBelief entry, never alongside Bet.
	Prediction *Prediction `json:"prediction,omitempty"`
	Result     Result      `json:"result,omitempty"`
	Note       string      `json:"note,omitempty"`
}

// Settled is a bet joined to its outcome.
type Settled struct {
	ID       string
	Placed   time.Time
	Bet      Bet
	Result   Result
	SettleAt time.Time
}

// seq disambiguates identifiers minted within the same nanosecond tick.
var seq atomic.Uint64

// NewID derives an identifier from the placement time and selection.
//
// The nanosecond component and a process counter are both present because
// second granularity is not enough: placing several wagers on one slate takes
// well under a second, and colliding ids would make settlements ambiguous.
// Load rejects duplicates rather than tolerating them, so a weak generator
// surfaces as a corrupt log rather than a silently mis-settled bet.
func NewID(t time.Time, selection string) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == ' ', r == '-', r == '_':
			return '-'
		default:
			return -1
		}
	}, selection)
	if len(slug) > 32 {
		slug = slug[:32]
	}
	return fmt.Sprintf("%s-%s-%06x%04x",
		t.UTC().Format("20060102T150405"), slug,
		t.Nanosecond()&0xffffff, seq.Add(1)&0xffff)
}

// Append writes one entry to the end of the log, creating it if needed.
func Append(path string, e Entry) error {
	if e.Kind == "" {
		return fmt.Errorf("betlog: entry has no kind")
	}
	if e.ID == "" {
		return fmt.Errorf("betlog: entry has no id")
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("betlog: encoding entry: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("betlog: opening %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("betlog: writing to %s: %w", path, err)
	}
	return nil
}

// PlaceBet appends a prediction.
func PlaceBet(path string, b Bet) (string, error) {
	if b.Selection == "" {
		return "", fmt.Errorf("betlog: a bet needs a selection")
	}
	if b.Predicted < 0 || b.Predicted > 1 {
		return "", fmt.Errorf("betlog: predicted probability %v out of range [0,1]", b.Predicted)
	}
	if _, err := b.Price.Breakeven(); err != nil {
		return "", fmt.Errorf("betlog: %w", err)
	}
	if _, err := ParseBankroll(b.Bankroll); err != nil {
		return "", err
	}
	// A non-positive stake used to be accepted, then counted in calibration
	// while vanishing from ROI -- Score incremented Scored before skipping it.
	if b.Stake <= 0 || math.IsNaN(b.Stake) || math.IsInf(b.Stake, 0) {
		return "", fmt.Errorf("betlog: stake %v must be a positive real number", b.Stake)
	}
	if b.Book != "" && !b.Book.Known() {
		return "", fmt.Errorf(
			"betlog: %q is not a known sportsbook; bonus-bet rules differ by book "+
				"and an unrecognised one would silently get the permissive default", b.Book)
	}
	now := time.Now()
	id := NewID(now, b.Selection)
	if err := Append(path, Entry{Kind: KindBet, ID: id, Time: now, Bet: &b}); err != nil {
		// Returning an id alongside an error would let a caller record an id
		// that is not in the file.
		return "", err
	}
	return id, nil
}

// Settle appends an outcome for a previously recorded bet.
func Settle(path, id string, r Result, note string) error {
	if !r.Counts() && r != Pushed && r != Void {
		return fmt.Errorf("betlog: %q is not a settleable result", r)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: r, Note: note})
}

// record is one folded entry of the stream, before it is projected into a
// typed view. It is deliberately kind-agnostic: the integrity rules below --
// unique ids, a settlement must find its subject, and nothing may be settled
// twice -- are properties of the STREAM, not of what a record happens to mean.
// There is exactly one implementation of them, and both projections share it.
type record struct {
	ID       string
	Kind     Kind
	Time     time.Time
	Bet      *Bet
	Pred     *Prediction
	Result   Result
	SettleAt time.Time
	Note     string
}

// scan folds the event stream and enforces its integrity, without knowing what
// any record means.
//
// Unrecognised kinds are skipped and counted rather than treated as corruption.
// A log written by a newer binary should still be readable by an older one --
// the alternative is that adding a third event type is another migration -- but
// silence would hide real corruption, so the count is returned for the caller
// to report.
func scan(path string) (recs []record, order []string, skipped map[Kind]int, err error) {
	f, e := os.Open(path)
	if e != nil {
		if os.IsNotExist(e) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("betlog: opening %s: %w", path, e)
	}
	defer f.Close()

	byID := map[string]*record{}
	skipped = map[Kind]int{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var en Entry
		if err := json.Unmarshal([]byte(text), &en); err != nil {
			return nil, nil, nil, fmt.Errorf("betlog: %s line %d: %w", path, line, err)
		}
		switch en.Kind {
		case KindBet, KindBelief:
			if en.Kind == KindBet && en.Bet == nil {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: bet entry with no bet", path, line)
			}
			if en.Kind == KindBelief && en.Prediction == nil {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: belief entry with no prediction", path, line)
			}
			if _, dup := byID[en.ID]; dup {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: duplicate id %q", path, line, en.ID)
			}
			byID[en.ID] = &record{ID: en.ID, Kind: en.Kind, Time: en.Time,
				Bet: en.Bet, Pred: en.Prediction, Result: Open}
			order = append(order, en.ID)
		case KindSettle:
			r, ok := byID[en.ID]
			if !ok {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: settlement for unknown id %q",
					path, line, en.ID)
			}
			// Append-only bytes are not the same as append-only meaning. Taking
			// the last write here would let a second settlement silently rewrite
			// the outcome -- an audit swung one bet's ROI from -100% to +90.9%
			// that way, with three valid lines on disk and no error. Hindsight is
			// exactly what this log exists to prevent, so a double settlement is
			// a corrupt log.
			if r.Result != Open {
				return nil, nil, nil, fmt.Errorf(
					"betlog: %s line %d: %q was already settled as %q and cannot be "+
						"re-settled as %q — correcting a mis-settled record means voiding it "+
						"and recording a new one, not editing history",
					path, line, en.ID, r.Result, en.Result)
			}
			if !en.Result.settleable() {
				return nil, nil, nil, fmt.Errorf("betlog: %s line %d: %q is not a valid result",
					path, line, en.Result)
			}
			r.Result = en.Result
			r.SettleAt = en.Time
			r.Note = en.Note
		default:
			skipped[en.Kind]++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("betlog: reading %s: %w", path, err)
	}

	recs = make([]record, 0, len(order))
	for _, id := range order {
		recs = append(recs, *byID[id])
	}
	return recs, order, skipped, nil
}

// warnSkipped reports unrecognised event kinds once, rather than dropping them
// in silence.
func warnSkipped(path string, skipped map[Kind]int) {
	for k, n := range skipped {
		fmt.Fprintf(os.Stderr,
			"betlog: %s: skipped %d entries of unknown kind %q — written by a newer "+
				"edgectl than this one\n", path, n, k)
	}
}

// Load reads the stream and folds settlements onto their bets.
//
// A settlement with no matching bet is an error rather than a silent skip: it
// means the log has been edited or an id was mistyped, and quietly dropping it
// would hide the problem from the calibration it exists to feed.
//
// Beliefs are refused rather than ignored. A belief carries no price and no
// stake, so folding one in here would put a stake-less row into ROI -- the
// exact bug PlaceBet's validation exists to prevent, arriving by another door.
func Load(path string) ([]Settled, error) {
	recs, _, skipped, err := scan(path)
	if err != nil {
		return nil, err
	}
	warnSkipped(path, skipped)

	out := make([]Settled, 0, len(recs))
	for _, r := range recs {
		if r.Kind == KindBelief {
			return nil, fmt.Errorf(
				"betlog: %s holds belief predictions, which have no price or stake and "+
					"cannot be scored as wagers — read it with `edgectl beliefs score`", path)
		}
		out = append(out, Settled{ID: r.ID, Placed: r.Time, Bet: *r.Bet,
			Result: r.Result, SettleAt: r.SettleAt})
	}
	return out, nil
}

// Calibration compares what was predicted against what happened.
type Calibration struct {
	Scored    int     // bets with a win/loss outcome
	Open      int     // not yet settled
	Excluded  int     // pushes and voids
	Wins      int     //
	Predicted float64 // sum of predicted probabilities
	Realised  float64 // Wins / Scored

	// Expected is the mean predicted probability. Comparing it to Realised is
	// the headline calibration check: predicting 34% and hitting 25% over
	// enough bets means the belief inputs are optimistic.
	Expected float64

	// StakedROI is realised profit per unit staked, using each bet's own
	// bankroll rules.
	StakedROI float64
	Staked    float64
	Profit    float64
}

// Score computes calibration over a set of bets, optionally filtered.
func Score(bets []Settled, filter func(Settled) bool) (Calibration, error) {
	var c Calibration
	for _, b := range bets {
		if filter != nil && !filter(b) {
			continue
		}
		switch {
		case b.Result == Open:
			c.Open++
			continue
		case !b.Result.Counts():
			c.Excluded++
			continue
		}

		c.Scored++
		c.Predicted += b.Bet.Predicted
		if b.Result == Won {
			c.Wins++
		}

		stake := b.Bet.Stake
		if stake <= 0 || math.IsNaN(stake) || math.IsInf(stake, 0) {
			return Calibration{}, fmt.Errorf(
				"betlog: bet %s has stake %v; it would count toward calibration while "+
					"contributing nothing to ROI", b.ID, stake)
		}
		c.Staked += stake
		profit, err := b.Bet.Price.ProfitMultiple()
		if err != nil {
			return Calibration{}, fmt.Errorf("betlog: bet %s: %w", b.ID, err)
		}
		bankroll, err := ParseBankroll(b.Bet.Bankroll)
		if err != nil {
			return Calibration{}, fmt.Errorf("betlog: bet %s: %w", b.ID, err)
		}
		bonus := bankroll == wager.BonusBet
		switch {
		case b.Result == Won:
			c.Profit += stake * profit
		case bonus:
			// A losing bonus bet costs nothing in cash.
		default:
			c.Profit -= stake
		}
	}

	if c.Scored > 0 {
		c.Realised = float64(c.Wins) / float64(c.Scored)
		c.Expected = c.Predicted / float64(c.Scored)
	}
	if c.Staked > 0 {
		c.StakedROI = c.Profit / c.Staked
	}
	return c, nil
}

// BySource scores bets grouped by where their scenario probability came from.
//
// This is why Source is recorded at all: after a season it answers whether your
// stated reads or the line-derived ones were better calibrated.
func BySource(bets []Settled) (map[string]Calibration, error) {
	return groupBy(bets, func(b Settled) string { return b.Bet.ScenarioSource })
}

// ByConditionalSource scores bets grouped by where q and r came from.
//
// Separate from BySource because the two are independently right or wrong. A
// season could show your game-script reads beating the line while your
// conditional hit rates lose to the fitted grid, or the reverse, and only
// separate tallies can distinguish those.
func ByConditionalSource(bets []Settled) (map[string]Calibration, error) {
	return groupBy(bets, func(b Settled) string { return b.Bet.ConditionalSource })
}

func groupBy(bets []Settled, key func(Settled) string) (map[string]Calibration, error) {
	norm := func(b Settled) string {
		if k := key(b); k != "" {
			return k
		}
		return "unspecified"
	}
	seen := map[string]bool{}
	for _, b := range bets {
		seen[norm(b)] = true
	}
	out := map[string]Calibration{}
	for s := range seen {
		want := s
		c, err := Score(bets, func(b Settled) bool { return norm(b) == want })
		if err != nil {
			return nil, err
		}
		out[s] = c
	}
	return out, nil
}

// SortedSources returns the keys of a BySource result in stable order.
func SortedSources(m map[string]Calibration) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"edge/internal/wager"
)

// Kind discriminates the two event types in the stream.
type Kind string

const (
	KindBet    Kind = "bet"
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

	Scenario       string  `json:"scenario,omitempty"`
	ScenarioSource string  `json:"scenario_source,omitempty"`
	SMarket        float64 `json:"s_market"`
	SYours         float64 `json:"s_yours"`
	Q              float64 `json:"q"`
	R              float64 `json:"r"`
	SStar          float64 `json:"s_star"`
	EdgeSource     string  `json:"edge_source,omitempty"`

	// Predicted is the blended P(hit) implied by SYours, Q and R. It is the
	// number calibration is scored against.
	Predicted float64 `json:"predicted"`

	Narrative string `json:"narrative,omitempty"`
}

// Entry is one line of the log.
type Entry struct {
	Kind   Kind      `json:"kind"`
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Bet    *Bet      `json:"bet,omitempty"`
	Result Result    `json:"result,omitempty"`
	Note   string    `json:"note,omitempty"`
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
	now := time.Now()
	id := NewID(now, b.Selection)
	return id, Append(path, Entry{Kind: KindBet, ID: id, Time: now, Bet: &b})
}

// Settle appends an outcome for a previously recorded bet.
func Settle(path, id string, r Result, note string) error {
	if !r.Counts() && r != Pushed && r != Void {
		return fmt.Errorf("betlog: %q is not a settleable result", r)
	}
	return Append(path, Entry{Kind: KindSettle, ID: id, Time: time.Now(), Result: r, Note: note})
}

// Load reads the stream and folds settlements onto their bets.
//
// A settlement with no matching bet is an error rather than a silent skip: it
// means the log has been edited or an id was mistyped, and quietly dropping it
// would hide the problem from the calibration it exists to feed.
func Load(path string) ([]Settled, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("betlog: opening %s: %w", path, err)
	}
	defer f.Close()

	bets := map[string]*Settled{}
	var order []string

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("betlog: %s line %d: %w", path, line, err)
		}
		switch e.Kind {
		case KindBet:
			if e.Bet == nil {
				return nil, fmt.Errorf("betlog: %s line %d: bet entry with no bet", path, line)
			}
			if _, dup := bets[e.ID]; dup {
				return nil, fmt.Errorf("betlog: %s line %d: duplicate bet id %q", path, line, e.ID)
			}
			bets[e.ID] = &Settled{ID: e.ID, Placed: e.Time, Bet: *e.Bet, Result: Open}
			order = append(order, e.ID)
		case KindSettle:
			b, ok := bets[e.ID]
			if !ok {
				return nil, fmt.Errorf("betlog: %s line %d: settlement for unknown bet %q",
					path, line, e.ID)
			}
			b.Result = e.Result
			b.SettleAt = e.Time
		default:
			return nil, fmt.Errorf("betlog: %s line %d: unknown kind %q", path, line, e.Kind)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("betlog: reading %s: %w", path, err)
	}

	out := make([]Settled, 0, len(order))
	for _, id := range order {
		out = append(out, *bets[id])
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
		if stake <= 0 {
			continue
		}
		c.Staked += stake
		profit, err := b.Bet.Price.ProfitMultiple()
		if err != nil {
			return Calibration{}, fmt.Errorf("betlog: bet %s: %w", b.ID, err)
		}
		bonus := strings.Contains(strings.ToLower(b.Bet.Bankroll), "bonus")
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
	sources := map[string]bool{}
	for _, b := range bets {
		s := b.Bet.ScenarioSource
		if s == "" {
			s = "unspecified"
		}
		sources[s] = true
	}
	out := map[string]Calibration{}
	for s := range sources {
		want := s
		c, err := Score(bets, func(b Settled) bool {
			got := b.Bet.ScenarioSource
			if got == "" {
				got = "unspecified"
			}
			return got == want
		})
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

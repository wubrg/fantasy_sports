// Package ledger is an append-only event log of what the bankroll holds, and
// when each piece of it dies.
//
// # Why this exists
//
// A seven-book promo campaign lost money in exactly one way: deadlines. Bonus
// tokens with 24-hour placement clocks, site credit with a 7-day expiry, and
// four profit boosts that were never used. Not one loss came from a bad price.
// A balance sheet cannot see any of that, because a balance is a scalar and an
// expiry is a schedule. So the primary question this package answers is not
// "how much do I have" but "what dies soonest" -- see Expiring, which is the
// function the rest of the package exists to serve.
//
// # Why assets are not numbers
//
// The same campaign carried four profit boosts recorded as "$19.03 of value".
// They were worth nothing, because a Fanatics profit boost will not attach to a
// bonus bet -- it needs a real-money stake, and there was no real money left to
// stake. No amount of arithmetic on 19.03 could have surfaced that. So a boost
// here is not an amount at all: it is a BoostSpec, with a percentage, a max stake,
// a minimum price and a RequiresCashStake flag, held as a lot with no
// Amount. Balance reports "3 boosts" separately from "$120 bonus", because
// adding those two numbers is the mistake.
//
// # Why the asset name is an open string
//
// Asset is a string, not an enum over cash/bonus/boost. Site credit -- FanCash,
// which converts 1:1 into a bonus bet but is not one until converted -- is
// deliberately not modelled yet, and when it is added it must not require
// rewriting a single recorded line. Replay therefore never switches on the asset
// name; it switches on the *shape* of a lot (see Lot.Unit), which the data
// carries with it. An asset string this package has never heard of parses,
// replays and reports correctly, and cannot disturb the balances of the ones it
// does know.
//
// # Why balances are derived and never stored
//
// The file is JSONL, one event per line, never rewritten -- the same shape as
// internal/betlog, and for a related reason. Storing a balance answers "what do
// I have now". Replaying a log answers "what did I hold on 2026-08-20", which is
// the question a campaign gets audited with after the fact, and the only one
// that can be checked. It also means an impossible state is *detectable*:
// spending more than was held, or expiring something already spent, is an error
// on replay rather than a number that silently went wrong months ago.
package ledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"edge/internal/wager"
)

// eps absorbs float noise in money arithmetic. Two paths that should agree to
// the cent must not disagree by 1e-17 and be called a corrupt log.
const eps = 1e-9

// Kind discriminates the event types in the stream.
type Kind string

const (
	KindDeposit  Kind = "deposit"  // real money arrives from outside
	KindWithdraw Kind = "withdraw" // real money leaves to outside
	KindGrant    Kind = "grant"    // the book hands you a promo asset
	KindConvert  Kind = "convert"  // one asset becomes another
	KindPlace    Kind = "place"    // an asset is committed to a wager
	KindSettle   Kind = "settle"   // a wager resolves
	KindExpire   Kind = "expire"   // an asset died unused
)

// The asset names modelled today. These are conveniences for callers,
// deliberately *not* a closed set: nothing in replay requires an asset to be one
// of these. See the package comment.
const (
	Cash  = "cash"
	Bonus = "bonus"
	Boost = "boost"
)

// Result is how a wager finished. Mirrors betlog.Result on purpose: the two logs
// describe the same wagers from different angles, and a wager that is "push" in
// one and "lost" in the other is a reconciliation bug worth being able to see.
type Result string

const (
	Won    Result = "won"
	Lost   Result = "lost"
	Pushed Result = "push"
	Void   Result = "void"
)

func (r Result) valid() bool {
	return r == Won || r == Lost || r == Pushed || r == Void
}

// BoostSpec is a profit boost: a multiplier on the profit of a qualifying
// wager.
//
// It has no amount, and that omission is the whole point. A boost's value is
// contingent on a wager existing that satisfies every field below, and the
// campaign that motivated this package proved that a boost can satisfy none of
// them and still look like money on a spreadsheet.
type BoostSpec struct {
	// Percent is the profit uplift, 0.50 for a 50% boost. It multiplies profit,
	// never stake, so it is worth nothing on a losing wager -- and nothing at
	// all on a bonus bet at books that disallow the pairing.
	Percent float64 `json:"percent"`

	// MaxStake caps the stake the boost applies to. A 50% boost with a $10 max
	// stake at +100 is worth at most $5, however large the bankroll.
	MaxStake float64 `json:"max_stake,omitempty"`

	// MinOdds is the price floor: a boost quoted as "-500 or longer" cannot be
	// used on a -600 favourite. Typed as wager.American so a caller can hand it
	// straight to that package's helpers rather than re-deriving the comparison
	// and getting the sign wrong.
	MinOdds wager.American `json:"min_odds,omitempty"`

	// RequiresCashStake is the field that made four of these worthless. When
	// true the boost will not attach to a bonus bet; it needs real money at
	// risk. Replay enforces it at settlement -- see state.settle.
	RequiresCashStake bool `json:"requires_cash_stake,omitempty"`
}

// Lot is one distinct holding: a specific $25 bonus token with its own clock,
// not a share of a pooled "bonus" balance.
//
// Identity matters because expiry is per-token. Pooling $25 + $25 into "$50 of
// bonus" destroys the fact that one of them dies tonight, which is the only fact
// worth having.
type Lot struct {
	ID     string  `json:"id,omitempty"`
	Book   string  `json:"book"`
	Asset  string  `json:"asset"`
	Amount float64 `json:"amount,omitempty"`

	// Expires is the deadline. nil means the asset does not expire -- cash,
	// normally. A zero-valued time would have been indistinguishable from "the
	// operator did not record one", so this is a pointer.
	Expires *time.Time `json:"expires,omitempty"`

	// Boost is set only for lots that are boosts. Its presence is what makes the
	// lot a unit lot; see Unit.
	Boost *BoostSpec `json:"boost,omitempty"`
}

// Unit reports whether this lot is spent whole rather than drawn down.
//
// A $50 bonus bet can (at some books) be split; a profit boost cannot be half
// used. Rather than switch on the asset name -- which would close the open
// string the package comment argues for -- the shape of the lot decides: a lot
// with no Amount is a unit lot. A future asset type inherits the right behaviour
// by carrying the right shape, with no change here.
func (l Lot) Unit() bool { return l.Amount == 0 }

func (l Lot) validate() error {
	if strings.TrimSpace(l.Book) == "" {
		return fmt.Errorf("lot %q has no book; assets are held per book, and a bonus bet at Fanatics is not spendable at DraftKings", l.ID)
	}
	if strings.TrimSpace(l.Asset) == "" {
		return fmt.Errorf("lot %q has no asset type", l.ID)
	}
	if l.Amount < 0 || math.IsNaN(l.Amount) || math.IsInf(l.Amount, 0) {
		return fmt.Errorf("lot %q has amount %v; want a non-negative real number", l.ID, l.Amount)
	}
	if l.Amount == 0 && l.Boost == nil {
		// A zero-amount lot with no boost is almost always a forgotten -amount
		// flag. Accepting it would silently create a unit lot of "cash", which
		// spends whole and cannot be drawn down: a very confusing balance to
		// debug three weeks later.
		return fmt.Errorf("lot %q has no amount and no boost; an asset must either carry a value or be a unit asset such as a boost", l.ID)
	}
	if l.Amount != 0 && l.Boost != nil {
		return fmt.Errorf("lot %q is a boost and also carries amount %v; a boost is not an amount, and pretending it is one is the mistake this type exists to prevent", l.ID, l.Amount)
	}
	if l.Boost != nil {
		if l.Boost.Percent <= 0 || math.IsNaN(l.Boost.Percent) {
			return fmt.Errorf("lot %q: boost percent %v must be positive (0.50 for a 50%% boost)", l.ID, l.Boost.Percent)
		}
		if l.Boost.MaxStake < 0 {
			return fmt.Errorf("lot %q: boost max stake %v must not be negative", l.ID, l.Boost.MaxStake)
		}
	}
	return nil
}

// Event is one line of the log.
//
// The struct is wide because replay must never have to guess. Every kind names
// either the lot it creates, the lot it draws from, or both -- a convert does
// both, which is exactly why a conversion cannot be expressed as a bare
// (asset, amount) pair.
type Event struct {
	Kind Kind      `json:"kind"`
	ID   string    `json:"id"`
	Time time.Time `json:"time"`

	// Creates is the lot this event brings into existence: the funds of a
	// deposit, the token of a grant, the *destination* of a convert, or the
	// proceeds of a settlement. If its ID is empty it defaults to the event ID,
	// so a hand-written line need not invent two identifiers.
	Creates *Lot `json:"creates,omitempty"`

	// Lot is the id of an existing lot this event draws from: withdraw,
	// convert's source, place, expire.
	Lot string `json:"lot,omitempty"`

	// Amount is how much of Lot is drawn. It must be zero for a unit lot (always
	// spent whole) and for expire (which always takes whatever remains -- a
	// partial expiry is not a thing that happens).
	Amount float64 `json:"amount,omitempty"`

	// Wager ties place and settle together. It is deliberately free-form so it
	// can hold a betlog id, which is how the two logs get reconciled.
	//
	// Several places may share one Wager id, and that is the normal case for a
	// boosted bet: one place spends the cash stake, another spends the boost.
	// Modelling a placement as a single event would have made "which asset did
	// this actually cost" unanswerable.
	Wager string `json:"wager,omitempty"`

	// Result and Returns describe a settlement. Returns is what the book
	// actually handed back, and it is recorded rather than derived on purpose: a
	// push returns the stake for cash, returns the token at DraftKings, and
	// forfeits it outright at FanDuel (see wager.Book.BonusLostOnPush). A
	// formula here would be wrong at some book on some day, silently.
	Result  Result `json:"result,omitempty"`
	Returns *Lot   `json:"returns,omitempty"`

	Note string `json:"note,omitempty"`
}

// seq disambiguates ids minted inside one nanosecond tick, as in betlog: several
// grants land together when a promo drops, and colliding ids would make the log
// unreplayable.
var seq atomic.Uint64

// NewID mints an event id from a timestamp and a label.
func NewID(t time.Time, label string) string {
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
	}, label)
	if len(slug) > 24 {
		slug = slug[:24]
	}
	return fmt.Sprintf("%s-%s-%06x%04x",
		t.UTC().Format("20060102T150405"), slug,
		t.Nanosecond()&0xffffff, seq.Add(1)&0xffff)
}

// Validate checks an event in isolation. It cannot catch "spent more than you
// had" -- that needs the whole history, and lives in replay.
func (e Event) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("ledger: event has no id")
	}
	if e.Time.IsZero() {
		return fmt.Errorf("ledger: event %s has no time; this package is entirely about deadlines, so an undated event is not recordable", e.ID)
	}
	if e.Amount < 0 || math.IsNaN(e.Amount) || math.IsInf(e.Amount, 0) {
		return fmt.Errorf("ledger: event %s has amount %v; want a non-negative real number", e.ID, e.Amount)
	}

	creates := func() error {
		if e.Creates == nil {
			return fmt.Errorf("ledger: %s event %s creates nothing", e.Kind, e.ID)
		}
		return e.Creates.validate()
	}
	draws := func() error {
		if e.Lot == "" {
			return fmt.Errorf("ledger: %s event %s does not say which lot it draws from", e.Kind, e.ID)
		}
		return nil
	}

	switch e.Kind {
	case KindDeposit, KindGrant:
		if e.Lot != "" {
			return fmt.Errorf("ledger: %s event %s must not draw from an existing lot; value arriving from outside creates one", e.Kind, e.ID)
		}
		return creates()
	case KindWithdraw:
		if err := draws(); err != nil {
			return err
		}
		if e.Creates != nil {
			return fmt.Errorf("ledger: withdraw event %s must not create a lot", e.ID)
		}
		return nil
	case KindConvert:
		if err := draws(); err != nil {
			return err
		}
		return creates()
	case KindPlace:
		if err := draws(); err != nil {
			return err
		}
		if e.Wager == "" {
			return fmt.Errorf("ledger: place event %s has no wager id; without one the stake can never be settled", e.ID)
		}
		if e.Creates != nil {
			return fmt.Errorf("ledger: place event %s must not create a lot", e.ID)
		}
		return nil
	case KindSettle:
		if e.Wager == "" {
			return fmt.Errorf("ledger: settle event %s has no wager id", e.ID)
		}
		if !e.Result.valid() {
			return fmt.Errorf("ledger: settle event %s has result %q; want won, lost, push or void", e.ID, e.Result)
		}
		if e.Lot != "" || e.Amount != 0 {
			return fmt.Errorf("ledger: settle event %s draws from a lot; a settlement consumes nothing (the stake was already consumed at placement) -- use returns", e.ID)
		}
		if e.Returns != nil {
			return e.Returns.validate()
		}
		return nil
	case KindExpire:
		if err := draws(); err != nil {
			return err
		}
		if e.Amount != 0 {
			return fmt.Errorf("ledger: expire event %s carries amount %v; an expiry takes whatever is left, it is not a partial spend", e.ID, e.Amount)
		}
		if e.Creates != nil {
			return fmt.Errorf("ledger: expire event %s must not create a lot", e.ID)
		}
		return nil
	case "":
		return fmt.Errorf("ledger: event %s has no kind", e.ID)
	default:
		return fmt.Errorf("ledger: event %s has unknown kind %q (want deposit, withdraw, grant, convert, place, settle or expire)", e.ID, e.Kind)
	}
}

// Append writes one event as a JSONL line.
//
// It validates first: a structurally broken line in an append-only file cannot
// be taken back, and would make every later read of the file fail.
func Append(w io.Writer, e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("ledger: encoding event %s: %w", e.ID, err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("ledger: writing event %s: %w", e.ID, err)
	}
	return nil
}

// Parse reads a JSONL stream into events, in file order.
//
// It does not replay. Parsing and replaying are separate so that a log which
// records an impossible state can still be *read* -- otherwise the only tool
// able to diagnose a broken log would be the one refusing to open it.
func Parse(r io.Reader) ([]Event, error) {
	var out []Event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("ledger: line %d: %w", line, err)
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("ledger: line %d: %w", line, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ledger: reading log: %w", err)
	}
	return out, nil
}

// Load reads a log file. A missing file is an empty log, not an error: the first
// `edgectl ledger add` should not have to be preceded by a touch.
func Load(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ledger: opening %s: %w", path, err)
	}
	defer f.Close()
	events, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%w (in %s)", err, path)
	}
	return events, nil
}

// AppendFile appends one event to a log file, creating it if needed.
func AppendFile(path string, e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ledger: opening %s: %w", path, err)
	}
	defer f.Close()
	if err := Append(f, e); err != nil {
		return err
	}
	return f.Close()
}

// Commitment is value currently at risk in an unsettled wager.
//
// It is tracked apart from the lots because it is neither held nor spent: the
// money is gone from the balance but may come back, and reporting it as either
// would be a lie in one direction or the other.
type Commitment struct {
	Wager  string
	Lot    string
	Book   string
	Asset  string
	Amount float64
	Unit   bool
	Boost  *BoostSpec
	At     time.Time
}

// Position is what was held at a moment in time.
type Position struct {
	AsOf      time.Time
	Lots      []Lot        // open lots, Amount reduced to what remains
	Committed []Commitment // staked and not yet settled
}

// Balance aggregates one (book, asset) pair.
//
// Amount and Units are two fields rather than one because they are not
// commensurable. "$120 of bonus and 3 boosts" is the honest statement; a single
// number would have to price the boosts, and pricing them is exactly the
// arithmetic that turned four unusable boosts into "$76 of value".
type Balance struct {
	Book   string
	Asset  string
	Amount float64
	Units  int
}

// Balances aggregates the open lots, sorted by book then asset.
func (p Position) Balances() []Balance {
	idx := map[string]*Balance{}
	var keys []string
	for _, l := range p.Lots {
		k := l.Book + "\x00" + l.Asset
		b, ok := idx[k]
		if !ok {
			b = &Balance{Book: l.Book, Asset: l.Asset}
			idx[k] = b
			keys = append(keys, k)
		}
		if l.Unit() {
			b.Units++
		} else {
			b.Amount += l.Amount
		}
	}
	sort.Strings(keys)
	out := make([]Balance, 0, len(keys))
	for _, k := range keys {
		out = append(out, *idx[k])
	}
	return out
}

// Total is the summed amount of one (book, asset). Unit lots contribute nothing,
// by design -- see Balance.
func (p Position) Total(book, asset string) float64 {
	var t float64
	for _, l := range p.Lots {
		if l.Book == book && l.Asset == asset && !l.Unit() {
			t += l.Amount
		}
	}
	return t
}

// Units counts the unit lots of one (book, asset): how many boosts are held.
func (p Position) Units(book, asset string) int {
	n := 0
	for _, l := range p.Lots {
		if l.Book == book && l.Asset == asset && l.Unit() {
			n++
		}
	}
	return n
}

// Expiry is an open lot with a deadline, and how long is left.
type Expiry struct {
	Lot Lot
	At  time.Time

	// In is At minus now, and is negative for a lot whose deadline has already
	// passed with no expire event recorded. That case is reported, not hidden:
	// it means the asset was probably already lost and the log has not caught
	// up, and dropping it silently would let the ledger disagree with the book.
	In time.Duration
}

// Expired reports whether this deadline has already passed.
func (e Expiry) Expired() bool { return e.In < 0 }

type held struct {
	lot   Lot
	order int
}

type state struct {
	lots map[string]*held
	next int

	// closed remembers *why* a lot is gone. Without it, expiring a spent lot and
	// expiring a typo would produce the same "no such lot" error, and only one
	// of those is the bug worth naming.
	closed map[string]string
	seen   map[string]bool

	commits map[string][]*Commitment
	settled map[string]time.Time
}

func newState() *state {
	return &state{
		lots:    map[string]*held{},
		closed:  map[string]string{},
		seen:    map[string]bool{},
		commits: map[string][]*Commitment{},
		settled: map[string]time.Time{},
	}
}

func (s *state) create(l Lot, fallbackID string) error {
	if l.ID == "" {
		l.ID = fallbackID
	}
	if _, live := s.lots[l.ID]; live {
		return fmt.Errorf("lot %q already exists; two lots sharing an id would make every later reference ambiguous", l.ID)
	}
	if why, gone := s.closed[l.ID]; gone {
		return fmt.Errorf("lot %q was already %s and cannot be re-created under the same id", l.ID, why)
	}
	s.lots[l.ID] = &held{lot: l, order: s.next}
	s.next++
	return nil
}

// draw removes value from a lot. It returns a copy of the lot describing what
// left it, so the caller knows the book, asset and boost spec of what was spent
// without having to look the lot up again after it has been closed.
//
// whole=true means "take the remainder", which is what an expiry does.
func (s *state) draw(id string, amount float64, verb string, whole bool) (Lot, float64, error) {
	h, ok := s.lots[id]
	if !ok {
		if why, gone := s.closed[id]; gone {
			return Lot{}, 0, fmt.Errorf("cannot %s lot %q: it was already %s", verb, id, why)
		}
		return Lot{}, 0, fmt.Errorf("cannot %s lot %q: no such lot has been granted, deposited, or converted into (check the id, or record the grant first)", verb, id)
	}
	if h.lot.Unit() {
		if amount != 0 {
			return Lot{}, 0, fmt.Errorf("cannot %s %.2f of lot %q: it is a unit asset (%s) and is spent whole or not at all", verb, amount, id, h.lot.Asset)
		}
		out := h.lot
		delete(s.lots, id)
		s.closed[id] = verb + "d"
		return out, 0, nil
	}
	if whole {
		amount = h.lot.Amount
	}
	if amount <= 0 {
		return Lot{}, 0, fmt.Errorf("cannot %s %v of lot %q: a divisible asset needs an amount", verb, amount, id)
	}
	if amount > h.lot.Amount+eps {
		return Lot{}, 0, fmt.Errorf("cannot %s %.2f from lot %q: only %.2f remains (catching this is the whole reason balances are replayed rather than stored)", verb, amount, id, h.lot.Amount)
	}
	out := h.lot
	out.Amount = amount
	h.lot.Amount -= amount
	if h.lot.Amount <= eps {
		delete(s.lots, id)
		s.closed[id] = verb + "d"
	}
	return out, amount, nil
}

func (s *state) apply(e Event) error {
	if s.seen[e.ID] {
		return fmt.Errorf("duplicate event id %q; an append-only log with repeated ids cannot be replayed twice to the same answer", e.ID)
	}
	s.seen[e.ID] = true

	switch e.Kind {
	case KindDeposit, KindGrant:
		return s.create(*e.Creates, e.ID)

	case KindWithdraw:
		_, _, err := s.draw(e.Lot, e.Amount, "withdraw", false)
		return err

	case KindExpire:
		// whole=true: an expiry takes the remainder. A $50 token half spent and
		// then expired costs what was left, not the original face value.
		_, _, err := s.draw(e.Lot, 0, "expire", true)
		return err

	case KindConvert:
		src, amount, err := s.draw(e.Lot, e.Amount, "convert", false)
		if err != nil {
			return err
		}
		dst := *e.Creates
		// Conservation. A convert moves value between asset types; it must not
		// mint or burn any. FanCash -> bonus bet is 1:1, and so is every other
		// conversion a book offers. If one ever is not, it is a grant plus an
		// expiry, recorded as two honest events -- not a lossy convert that
		// quietly rounds the bankroll down where nobody will look for it.
		//
		// Unit lots are exempt in the direction where the check is meaningless:
		// a boost has no amount to conserve.
		if !src.Unit() && !dst.Unit() && math.Abs(dst.Amount-amount) > 1e-6 {
			return fmt.Errorf("takes %.2f of %s but creates %.2f of %s: a conversion must not create or destroy value",
				amount, src.Asset, dst.Amount, dst.Asset)
		}
		return s.create(dst, e.ID)

	case KindPlace:
		if at, done := s.settled[e.Wager]; done {
			return fmt.Errorf("cannot place onto wager %q: it already settled at %s", e.Wager, at.Format(time.RFC3339))
		}
		src, amount, err := s.draw(e.Lot, e.Amount, "place", false)
		if err != nil {
			return err
		}
		s.commits[e.Wager] = append(s.commits[e.Wager], &Commitment{
			Wager: e.Wager, Lot: e.Lot, Book: src.Book, Asset: src.Asset,
			Amount: amount, Unit: src.Unit(), Boost: src.Boost, At: e.Time,
		})
		return nil

	case KindSettle:
		return s.settle(e)
	}
	return fmt.Errorf("unknown kind %q", e.Kind)
}

func (s *state) settle(e Event) error {
	commits, ok := s.commits[e.Wager]
	if !ok {
		return fmt.Errorf("no placement was recorded for wager %q (check the wager id, or record the place first)", e.Wager)
	}
	if at, done := s.settled[e.Wager]; done {
		// Same reasoning as betlog.Load: append-only bytes are not append-only
		// meaning. A second settlement would rewrite an outcome after the fact,
		// with three valid-looking lines on disk and no error.
		return fmt.Errorf("wager %q already settled at %s and cannot be settled again; correct a mistake by voiding and re-recording, not by editing history",
			e.Wager, at.Format(time.RFC3339))
	}

	// The expensive lesson, enforced. A boost that requires a real-money stake
	// cannot attach to a wager funded entirely with promo credit. Four of these
	// were carried for a week as "$19.03 of value" apiece, and every one was
	// unusable for exactly this reason.
	//
	// The check runs at settlement rather than at placement because funding
	// arrives over several place events (the cash stake, then the boost) whose
	// order is not fixed. A settlement is the single point at which a wager's
	// funding is known to be complete, which also makes the check safe under the
	// asOf truncation in Balances: settle is one event, so a prefix either
	// contains it or does not.
	for _, c := range commits {
		if c.Boost == nil || !c.Boost.RequiresCashStake {
			continue
		}
		hasCash := false
		for _, o := range commits {
			if o.Asset == Cash && o.Amount > 0 {
				hasCash = true
			}
		}
		if !hasCash {
			return fmt.Errorf("wager %q used boost lot %q, which requires a real-money stake, but nothing on the wager was cash: a profit boost will not attach to a bonus bet",
				e.Wager, c.Lot)
		}
	}

	s.settled[e.Wager] = e.Time
	if e.Returns != nil {
		if err := s.create(*e.Returns, e.ID); err != nil {
			return err
		}
	}
	return nil
}

// replay folds the events into a state, stopping after asOf.
//
// Events are replayed in time order, not file order. The distinction matters: an
// operator recording yesterday's grant this morning is normal and must not break
// the log, but "what did I hold on the 20th" is only well defined if the prefix
// is chosen by event time. sort.SliceStable with file order as the tiebreak
// keeps the answer deterministic for events sharing a timestamp, which is the
// common case when a promo drops five tokens at once.
func replay(events []Event, asOf time.Time) (*state, error) {
	ordered := make([]Event, len(events))
	copy(ordered, events)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Time.Before(ordered[j].Time) })

	s := newState()
	for _, e := range ordered {
		if err := e.Validate(); err != nil {
			return nil, err
		}
		if !asOf.IsZero() && e.Time.After(asOf) {
			continue
		}
		if err := s.apply(e); err != nil {
			return nil, fmt.Errorf("ledger: event %s (%s at %s): %w",
				e.ID, e.Kind, e.Time.Format(time.RFC3339), err)
		}
	}
	return s, nil
}

func (s *state) position(asOf time.Time) Position {
	p := Position{AsOf: asOf}
	hs := make([]*held, 0, len(s.lots))
	for _, h := range s.lots {
		hs = append(hs, h)
	}
	// Insertion order, which replay already made deterministic. Sorting by
	// book/asset here instead would lose the chronology that makes a position
	// readable, and Balances re-sorts anyway for the aggregate view.
	sort.Slice(hs, func(i, j int) bool { return hs[i].order < hs[j].order })
	for _, h := range hs {
		p.Lots = append(p.Lots, h.lot)
	}

	var wagers []string
	for w := range s.commits {
		if _, done := s.settled[w]; !done {
			wagers = append(wagers, w)
		}
	}
	sort.Strings(wagers)
	for _, w := range wagers {
		for _, c := range s.commits[w] {
			p.Committed = append(p.Committed, *c)
		}
	}
	return p
}

// Balances derives what was held at asOf by replaying the log.
//
// asOf is not decoration. "What did I hold on 2026-08-20" is the question a
// campaign gets audited with, and a stored balance can never answer it. A zero
// asOf means the whole log.
func Balances(events []Event, asOf time.Time) (Position, error) {
	s, err := replay(events, asOf)
	if err != nil {
		return Position{}, err
	}
	return s.position(asOf), nil
}

// Expiring lists the open lots that die within the window, soonest first.
//
// This is the function the package exists for. Every meaningful loss in the
// campaign that motivated it was a deadline, not a bad price, and no balance
// sheet anywhere shows a deadline.
//
// Lots already past their deadline with no expire event recorded are included,
// with a negative In. They are the ones probably already lost, and they belong
// at the top of the list precisely because the log does not yet know about them.
func Expiring(events []Event, within time.Duration, now time.Time) ([]Expiry, error) {
	pos, err := Balances(events, now)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(within)
	var out []Expiry
	for _, l := range pos.Lots {
		if l.Expires == nil {
			continue
		}
		if l.Expires.After(cutoff) {
			continue
		}
		out = append(out, Expiry{Lot: l, At: *l.Expires, In: l.Expires.Sub(now)})
	}
	// Deadline first, then lot id. The id tiebreak is what makes the order
	// total: five tokens from one promo drop share a timestamp to the second,
	// and without it their order could differ between runs on the same file.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Lot.ID < out[j].Lot.ID
	})
	return out, nil
}

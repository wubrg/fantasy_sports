package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"leaguehome/internal/draft"
)

// scratchpad is a hypothetical roster: players you are thinking about
// buying, at what you think they will cost.
//
// Held entirely apart from the live draft state, which matters more than it
// looks. server.manual records claims about what actually happened and
// drives real budget arithmetic on draft night; a what-if that leaked into
// it would corrupt the board mid-draft, and the corruption would be
// invisible, because a scratch pick and a real sale both look like a player
// leaving the board. Separate map, separate endpoints, and responses that
// say which mode produced them.
type scratchpad struct {
	mu sync.Mutex
	// picks maps a player ID to the price being imagined for him.
	picks map[string]int
	// order preserves insertion order so the panel reads as it was built.
	order []string
}

func newScratchpad() *scratchpad {
	return &scratchpad{picks: map[string]int{}}
}

// add records an imagined purchase, or re-prices one already held.
func (s *scratchpad) add(playerID string, price int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, held := s.picks[playerID]; !held {
		s.order = append(s.order, playerID)
	}
	s.picks[playerID] = price
}

func (s *scratchpad) remove(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.picks, playerID)
	for i, id := range s.order {
		if id == playerID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *scratchpad) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.picks, s.order = map[string]int{}, nil
}

// contents returns the picks in the order they were added.
func (s *scratchpad) contents() ([]string, map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := append([]string(nil), s.order...)
	picks := make(map[string]int, len(s.picks))
	for id, price := range s.picks {
		picks[id] = price
	}
	return order, picks
}

// ScratchSpot is one imagined purchase.
type ScratchSpot struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Slot     string `json:"slot"`
	Price    int    `json:"price"`
	// Kept marks a keeper: already yours, not something you are trying on.
	Kept bool `json:"kept"`
	// Won marks a player you bought at auction. Also already yours, and so
	// also not a try — but the panel prints what you paid for him where a
	// keeper only says that he is kept.
	Won    bool    `json:"won"`
	Points float64 `json:"points"`
	Cost   int     `json:"cost"`
}

// ScratchView is the hypothetical roster and how it reads.
type ScratchView struct {
	Starters []ScratchSpot       `json:"starters"`
	Bench    []ScratchSpot       `json:"bench"`
	Metrics  draft.RosterMetrics `json:"metrics"`
	// BudgetLeft and SlotsLeft are what would remain after these buys.
	BudgetLeft int `json:"budgetLeft"`
	SlotsLeft  int `json:"slotsLeft"`
	// BenchSlots is how many bench spots the roster has, filled or not.
	//
	// Sent because an empty bench spot is not a player and so cannot be
	// inferred from Bench — without a count the panel can only draw the
	// bench it already has, which reads as a finished roster while two
	// spots are still open. Derived from the shape rather than fixed, so a
	// league that changes its lineup does not need this changed with it.
	BenchSlots int `json:"benchSlots"`
	// MaxBid is the most that could still go on one more player.
	MaxBid int `json:"maxBid"`
	// Unfilled names the starting slots this roster cannot yet cover.
	Unfilled []string `json:"unfilled"`
	// Traits is the lineup's composition: how many starters are each kind
	// of player. The question the roster shapes were built to answer, asked
	// of the roster you are actually assembling rather than of a
	// hypothetical one a greedy search had to go and find.
	Traits map[string]int `json:"traits"`
	// Dropped names players who left the real board while sitting here.
	Dropped []string `json:"dropped"`
	// Empty means there is nothing to *clear* — no players being tried on.
	// Deliberately not "the panel has no rows": keepers and players you have
	// won are rows, and neither is cleared by the button this drives, so a
	// live button on a roster full of them would visibly do nothing.
	Empty bool `json:"empty"`
}

// ownedPicks are the players you have actually won, at what you paid.
//
// Read under the board's lock because the live feed writes them, and returned
// sorted so the panel does not reshuffle itself between polls: taken is a map,
// and ranging one twice gives two different orders.
func (s *server) ownedPicks() []struct {
	ID    string
	Price int
} {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []struct {
		ID    string
		Price int
	}
	// Combined exactly as rebuildLocked combines them, manual overwriting the
	// feed, because hand-entered sales win — you knew before the API did. Any
	// other precedence here would print a price the board's own budget
	// disagrees with.
	combined := make(map[string]gone, len(s.taken)+len(s.manual))
	for id, g := range s.taken {
		combined[id] = g
	}
	for id, g := range s.manual {
		combined[id] = g
	}
	for id, g := range combined {
		if !g.mine {
			continue
		}
		out = append(out, struct {
			ID    string
			Price int
		}{id, g.price})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Price != out[j].Price {
			return out[i].Price > out[j].Price
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// scratchView scores the scratchpad against a board.
//
// Uses the same Score and ScoringBaselines the board does, so the roster
// you assemble here is measured exactly as the live one is.
func (s *server) scratchView(snap draft.Snapshot) ScratchView {
	order, picks := s.scratch.contents()
	view := ScratchView{Empty: len(order) == 0}

	onBoard := make(map[string]draft.PlayerSignals, len(snap.Players))
	for _, p := range snap.Players {
		onBoard[p.PlayerID] = p
	}

	r := &draft.Roster{}
	// Keepers first. The panel's whole job is showing what the finished
	// roster feels like, and yours already contains the players you are
	// keeping — leaving them out understates POPR, hides the slots they
	// fill, and disagrees with the shapes report about the same roster.
	for _, h := range s.static.heldRoster(s.static.ownerID) {
		h.Held = true
		r.Players = append(r.Players, h)
	}
	// Then the players you have actually won, for the same reason and with
	// the same standing. A keeper and a player bought an hour ago are both
	// simply yours; the panel that shows what your roster feels like is wrong
	// about every number on it if it knows only about the first kind.
	//
	// Held rather than a third state, because Held is the question the
	// arithmetic below asks — "is this already mine, or something I am trying
	// on" — and the budget is already net of these picks. Only Won separates
	// the two for the panel, which prints a price for one and "kept" for the
	// other.
	won := map[string]bool{}
	for _, o := range s.ownedPicks() {
		spot := s.static.wonSpot(o.ID, o.Price)
		spot.Held = true
		r.Players = append(r.Players, spot)
		won[o.ID] = true
	}
	for _, id := range order {
		if won[id] {
			// Penciled in, then actually won. He is on the roster above at
			// what he really cost, so counting the guess as well would put
			// him on it twice.
			continue
		}
		p, still := onBoard[id]
		if !still {
			// He has been drafted for real since being added — by someone
			// else, since the won set is handled above. Keeping him would
			// have the panel plan around a player who is gone, which is
			// worse than losing the note.
			view.Dropped = append(view.Dropped, s.static.nameOf(id))
			continue
		}
		r.Add(p, picks[id])
	}

	view.Metrics = draft.Score(r, s.scoringBaselines(), s.static.rosterShape())
	view.Unfilled = view.Metrics.Unfilled
	// After Score: it assigns the lineup, and composition counts starters.
	view.Traits = map[string]int{}
	for t, n := range draft.TraitCounts(*r) {
		view.Traits[string(t)] = n
	}

	// Which of the two kinds of owned row this is. Held says a player is
	// already yours; won says you bought him rather than kept him, and it is
	// what stops a player you paid $83 for being labelled a keeper.
	owned := func(spot draft.RosterSpot) ScratchSpot {
		out := toScratchSpot(spot)
		out.Won = won[spot.Player.PlayerID]
		out.Kept = spot.Held && !out.Won
		return out
	}
	for _, spot := range r.Starters() {
		view.Starters = append(view.Starters, owned(spot))
	}
	for _, spot := range r.Bench() {
		view.Bench = append(view.Bench, owned(spot))
	}

	view.BenchSlots = benchSlots(s.static.rosterShape())

	// Budget and slots are already net of the keepers, so only the players
	// actually bought here count against them.
	bought := view.Metrics.Spend - heldSpend(r)
	view.BudgetLeft = snap.Me.Budget - bought
	view.SlotsLeft = snap.Me.OpenSlots - (len(r.Players) - heldCount(r))
	if view.SlotsLeft > 0 {
		if bid := view.BudgetLeft - (view.SlotsLeft - 1); bid > 0 {
			view.MaxBid = bid
		}
	}
	return view
}

// benchSlots is the roster less its starting lineup.
//
// Counted off the shape — every starting position plus the flex — rather
// than written down, because the two numbers have to agree: a bench sized by
// hand would quietly stop matching the lineup the same shape draws above it,
// and the panel would show a roster of the wrong length.
func benchSlots(shape draft.PoolState) int {
	starting := shape.FlexSlots
	for _, n := range shape.Starters {
		starting += n
	}
	if n := rosterSize - starting; n > 0 {
		return n
	}
	return 0
}

func heldSpend(r *draft.Roster) int {
	total := 0
	for _, p := range r.Players {
		if p.Held {
			total += p.Price
		}
	}
	return total
}

func heldCount(r *draft.Roster) int {
	n := 0
	for _, p := range r.Players {
		if p.Held {
			n++
		}
	}
	return n
}

func toScratchSpot(s draft.RosterSpot) ScratchSpot {
	return ScratchSpot{
		PlayerID: s.Player.PlayerID,
		Name:     s.Player.Name,
		Position: s.Player.Position,
		Slot:     s.Slot,
		Price:    s.Price,
		Kept:     s.Held,
		Points:   s.Player.PrimaryPoints,
		Cost:     s.Player.Cost,
	}
}

// scoringBaselines are the pinned VOLS baselines every roster is measured
// against. Owned by staticData so the scratch roster and the scarcity
// counts measure against one set.
func (s *server) scoringBaselines() map[string]float64 {
	return s.static.baselines
}

// scratchResponse pairs the untouched live board with the scratch view, so
// the page never has to work out which numbers came from where.
type scratchResponse struct {
	Board   draft.Snapshot `json:"board"`
	Scratch ScratchView    `json:"scratch"`
}

// handleScratch adds, re-prices, removes, or clears a hypothetical pick.
func (s *server) handleScratch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Action string `json:"action"`
		Player string `json:"player"`
		Price  int    `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch body.Action {
	case "clear":
		s.scratch.clear()
	case "add", "remove":
		id := s.static.playerIDByName(body.Player)
		if id == "" {
			http.Error(w, fmt.Sprintf("no player named %q on the board", body.Player), http.StatusBadRequest)
			return
		}
		if body.Action == "remove" {
			s.scratch.remove(id)
			break
		}
		price := body.Price
		if price < 1 {
			// Default to what the board says he will go for.
			price = 1
			for _, p := range s.snapshot().Players {
				if p.PlayerID == id {
					price = draft.BoardPrice(p)
					break
				}
			}
		}
		s.scratch.add(id, price)
	default:
		http.Error(w, "action must be add, remove or clear", http.StatusBadRequest)
		return
	}

	snap := s.snapshot()
	writeJSON(w, scratchResponse{Board: snap, Scratch: s.scratchView(snap)})
}

// handleScratchView returns the current scratch roster without changing it.
func (s *server) handleScratchView(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	writeJSON(w, scratchResponse{Board: snap, Scratch: s.scratchView(snap)})
}

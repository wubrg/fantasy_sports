package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	Kept   bool    `json:"kept"`
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
	// MaxBid is the most that could still go on one more player.
	MaxBid int `json:"maxBid"`
	// Unfilled names the starting slots this roster cannot yet cover.
	Unfilled []string `json:"unfilled"`
	// Dropped names players who left the real board while sitting here.
	Dropped []string `json:"dropped"`
	Empty   bool     `json:"empty"`
}

// scratchView scores the scratchpad against a board.
//
// Uses the same Score and ScoringBaselines the archetypes use, so a roster
// built by hand and one produced by a shape are directly comparable — which
// is the whole point of having both.
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
	for _, id := range order {
		p, still := onBoard[id]
		if !still {
			// He has been drafted for real since being added. Keeping him
			// would have the panel plan around a player who is gone, which
			// is worse than losing the note.
			view.Dropped = append(view.Dropped, s.static.nameOf(id))
			continue
		}
		r.Add(p, picks[id])
	}

	view.Metrics = draft.Score(r, s.scoringBaselines(), s.static.shape)
	view.Unfilled = view.Metrics.Unfilled

	for _, spot := range r.Starters() {
		view.Starters = append(view.Starters, toScratchSpot(spot))
	}
	for _, spot := range r.Bench() {
		view.Bench = append(view.Bench, toScratchSpot(spot))
	}

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
		Points:   s.Player.CielyPoints,
		Cost:     s.Player.Cost,
	}
}

// scoringBaselines are the pinned VOLS baselines every roster is measured
// against. Owned by staticData so the scratch roster, the archetypes and
// the scarcity counts all measure against one set.
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

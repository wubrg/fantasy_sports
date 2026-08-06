package draft

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// WriteBoard renders the draft board: what each player should cost, what
// you will pay, and the handful of flags worth glancing at while bidding.
//
// Kept deliberately narrow. The temptation is to show every signal we have;
// during live bidding that is the same as showing none. Everything else
// lives in the gap and scarcity reports, which are for prep.
func WriteBoard(w io.Writer, players []PlayerSignals, me MyState, must MustHaveCost, top Pivot, hasPivot bool, limit int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "$%d left\t%d slots\tmax bid $%d\n", me.Budget, me.OpenSlots, me.MaxBid())
	if need := formatNeeds(me.StartersNeeded); need != "" {
		fmt.Fprintf(tw, "still starting: %s\n", need)
	}
	if hasPivot {
		fmt.Fprintf(tw, "\n>> %s: %s\n", top.Name, top.Reason)
	}
	if err := writeMustHaves(tw, must); err != nil {
		return err
	}
	fmt.Fprintln(tw)

	fmt.Fprintln(tw, "PLAYER\tPOS\tCOST\tVALUE\tEDGE\tMY MAX\tPS%\tFLAGS")
	shown := 0
	for _, p := range players {
		if limit > 0 && shown >= limit {
			break
		}
		shown++

		myMax := fmt.Sprintf("$%d", p.MyMaxBid)
		if p.MyMaxBid == 0 {
			myMax = "—"
		} else if p.BidRule == RuleMustHave {
			// Mark the number as a deliberate overpay, not a valuation.
			myMax += "*"
		}
		// A bid you cannot afford is worse than no number at all.
		if p.MyMaxBid > me.MaxBid() {
			myMax = fmt.Sprintf("$%d!", me.MaxBid())
		}
		edge := fmt.Sprintf("%+d", p.Edge())
		if p.Cost == 0 {
			edge = "—"
		}
		ps := "—"
		if p.ScarcityPct > 0 {
			ps = fmt.Sprintf("%.0f%%", p.ScarcityPct)
		}
		cost := "—"
		if p.Cost > 0 {
			cost = fmt.Sprintf("$%d", p.Cost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t$%d\t%s\t%s\t%s\t%s\n",
			p.Name, p.Position, cost, p.Value, edge, myMax, ps, strings.Join(flagsFor(p), " "))
	}
	return tw.Flush()
}

// writeMustHaves states what the declared must-haves commit you to.
//
// Wanting specific players is legitimate; not knowing what it costs is not.
// This turns "get your guys" into a budget line rather than a feeling.
func writeMustHaves(w io.Writer, m MustHaveCost) error {
	if len(m.Players) == 0 {
		return nil
	}
	var parts []string
	for _, pl := range m.Players {
		parts = append(parts, fmt.Sprintf("%s $%d", pl.Player, pl.Cap))
	}
	warn := ""
	if m.Overcommitted() {
		warn = "  <- too thin to field a roster"
	}
	_, err := fmt.Fprintf(w, "\nmust-haves: %s = $%d, leaving $%d for %d slots (~$%.0f each)%s\n",
		strings.Join(parts, ", "), m.Committed, m.Remaining, m.SlotsLeft, m.PerSlot(), warn)
	return err
}

// flagsFor is the short glyph set shown beside a player. Only things that
// change a decision earn a place here.
func flagsFor(p PlayerSignals) []string {
	var out []string
	if m := p.Lean.Marker(); m != "" {
		out = append(out, m)
	}
	// Naming who disagrees rather than just that someone does: "vs menton"
	// tells you which read to go and check, which a bare mark does not.
	for _, o := range p.Lean.Disagreement() {
		out = append(out, "vs "+o.Source)
	}
	switch p.ECR {
	case ECRContested:
		out = append(out, "split")
	case ECRUpside:
		out = append(out, "ecr+")
	case ECRDownside:
		out = append(out, "ecr-")
	}
	if p.Availability != "" {
		out = append(out, strings.ToLower(p.Availability))
	}
	// Only call out a spread big enough to change the price materially.
	if s := p.BaselineSpread(); s >= 10 {
		out = append(out, fmt.Sprintf("swing$%.0f", s))
	}
	return out
}

func formatNeeds(needs map[string]int) string {
	var parts []string
	for _, pos := range []string{"QB", "RB", "WR", "TE", "DEF"} {
		if n := needs[pos]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, pos))
		}
	}
	return strings.Join(parts, ", ")
}

// WriteEdges renders the value-versus-cost view with the positional
// component removed.
//
// Raw edge is shown beside the adjusted figure on purpose. A large raw edge
// with a small adjusted one is the source disagreeing about the whole
// position, which is a claim about the model rather than about the player,
// and hiding the difference would let a baseline artifact read as a
// bargain.
func WriteEdges(w io.Writer, players []PlayerSignals, bias map[string]float64, limit int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PLAYER\tPOS\tCOST\tVALUE\tEDGE\tvs POS\tFLAGS")
	for i, p := range players {
		if limit > 0 && i >= limit {
			break
		}
		fmt.Fprintf(tw, "%s\t%s\t$%d\t$%d\t%+d\t%+.0f\t%s\n",
			p.Name, p.Position, p.Cost, p.Value, p.Edge(),
			AdjustedEdge(p, bias), strings.Join(flagsFor(p), " "))
	}
	return tw.Flush()
}

// WritePositionalBias reports how much of every edge at a position is the
// source disagreeing about the position itself.
func WritePositionalBias(w io.Writer, bias map[string]float64) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "POS\tMEDIAN EDGE\tREADING")
	for _, pos := range []string{"QB", "RB", "WR", "TE"} {
		v, ok := bias[pos]
		if !ok {
			continue
		}
		note := "value and cost broadly agree here"
		switch {
		case v >= 8:
			note = "value source prices this position far above the market — discount these edges"
		case v >= 3:
			note = "value source runs high here"
		case v <= -8:
			note = "market pays far above the value source — treat bargains here with suspicion"
		case v <= -3:
			note = "market runs high here"
		}
		fmt.Fprintf(tw, "%s\t%+.0f\t%s\n", pos, v, note)
	}
	return tw.Flush()
}

// WriteGaps renders where our board and an outside reference disagree.
// This is prep reading, not draft-night reading.
func WriteGaps(w io.Writer, gaps []Gap, limit int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "PLAYER\tPOS\tOUR COST\t%s\tDIFF\n", strings.ToUpper(sourceLabel(gaps)))
	for i, g := range gaps {
		if limit > 0 && i >= limit {
			break
		}
		fmt.Fprintf(tw, "%s\t%s\t$%d\t$%.0f\t%+.0f\n", g.Player, g.Position, g.Cost, g.Compare, g.Delta)
	}
	return tw.Flush()
}

func sourceLabel(gaps []Gap) string {
	if len(gaps) == 0 {
		return "reference"
	}
	return gaps[0].Source
}

// WriteScarcity renders how thin each position has become.
func WriteScarcity(w io.Writer, s map[string]PositionScarcity) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "POS\tSTARTABLE\tSTARTERS STILL NEEDED\tCOVER\tPS% AT TOP\tNEXT CLIFF")
	for _, pos := range []string{"QB", "RB", "WR", "TE"} {
		v, ok := s[pos]
		if !ok {
			continue
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%.0f%%\t%.0f pts\n",
			pos, v.Startable, v.StartersLeft, formatCover(v.Cover), v.TopScarcityPct, v.Cliff)
	}
	return tw.Flush()
}

// formatCover marks the line where the room runs out of startable players,
// since that is the only threshold in the column that changes a decision.
func formatCover(cover float64) string {
	if cover == 0 {
		return "—"
	}
	short := ""
	if cover < 1 {
		short = " !"
	}
	return fmt.Sprintf("%.2fx%s", cover, short)
}

// WriteShapes renders the archetype comparison.
//
// The shapes are illustrations, not recommendations. The fill is greedy and
// the thresholds are first guesses, so the report says so rather than
// letting a POPR ranking imply one shape is correct.
func WriteShapes(w io.Writer, shapes []Shape, budget int, held []RosterSpot) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "ROSTER SHAPES — $%d\n", budget)
	fmt.Fprintln(tw, strings.Repeat("=", 78))
	fmt.Fprintf(tw, "What each strategy buys at current prices. POPR is the starting lineup's\n")
	fmt.Fprintf(tw, "points above positional replacement, which cancels the projection source's\n")
	fmt.Fprintf(tw, "positional skew. These are illustrations of a shape, not the best possible\n")
	fmt.Fprintf(tw, "roster of it — the fill is greedy. Thresholds are calibrated against this\n")
	fmt.Fprintf(tw, "league's own 2023-2025 drafts; nothing in them separates these shapes on\n")
	fmt.Fprintf(tw, "results, so read them as what your money can buy rather than as a ranking.\n")
	if len(held) > 0 {
		var parts []string
		for _, h := range held {
			parts = append(parts, fmt.Sprintf("%s %s $%d", h.Player.Name, h.Player.Position, h.Price))
		}
		fmt.Fprintf(tw, "\nEvery shape is built around your keepers — %s — because they are part\n",
			strings.Join(parts, ", "))
		fmt.Fprintf(tw, "of the finished roster and count against the shape like any other buy.\n")
	}
	fmt.Fprintln(tw)

	fmt.Fprintln(tw, "SHAPE\tPOPR\tSPEND\tQB\tRB\tWR\tTE\tMY GUYS\tNOTES")
	for _, s := range shapes {
		note := ""
		if s.BlockedBy != "" {
			note = fmt.Sprintf("ruled out by keeping %s", s.BlockedBy)
		} else if !s.Achieved && !s.Possible {
			note = "the board cannot supply this shape"
		} else if !s.Achieved {
			note = "possible, but the greedy fill did not reach it"
		} else if s.Metrics.Injured > 0 {
			note = fmt.Sprintf("%d carrying an injury designation", s.Metrics.Injured)
		}
		sp := s.Metrics.SpendPosition
		fmt.Fprintf(tw, "%s\t%.0f\t$%d\t$%d\t$%d\t$%d\t$%d\t%d\t%s\n",
			s.Archetype.Name, s.Metrics.POPR, s.Metrics.Spend,
			sp["QB"], sp["RB"], sp["WR"], sp["TE"], s.Metrics.MyGuys, note)
	}

	for _, s := range shapes {
		fmt.Fprintf(tw, "\n%s — %s\n", s.Archetype.Name, s.Archetype.Why)
		if s.Archetype.Seen != "" {
			fmt.Fprintf(tw, "  built %s\n", s.Archetype.Seen)
		}
		fmt.Fprintln(tw, "  SLOT\tPLAYER\tPOS\tPRICE\tPTS")
		for _, spot := range s.Roster.Starters() {
			name := spot.Player.Name
			if spot.Held {
				// Which players you are committed to before the auction is
				// the difference between a shape you can pursue and one you
				// are already inside.
				name += " (kept)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t$%d\t%.0f\n",
				spot.Slot, name, spot.Player.Position, spot.Price, spot.Player.CielyPoints)
		}
		bench := s.Roster.Bench()
		if len(bench) > 0 {
			var names []string
			cost := 0
			for _, b := range bench {
				names = append(names, fmt.Sprintf("%s $%d", b.Player.Name, b.Price))
				cost += b.Price
			}
			fmt.Fprintf(tw, "  BN\t%s\t\t$%d\t%.0f\n", strings.Join(names, ", "), cost, s.Metrics.BenchPoints)
		}
	}
	return tw.Flush()
}

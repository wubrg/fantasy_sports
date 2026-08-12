package draft

import (
	"fmt"
	"io"
	"math"
	"sort"
	"text/tabwriter"
)

// TeamSeason is one manager's completed draft and how the season went.
type TeamSeason struct {
	Season  string
	OwnerID string
	Team    string
	// Picks is every drafted player with what was actually charged,
	// keepers included — a keeper is money committed like any other.
	Picks []DraftedPlayer
	// Points is the season's total, and Rank the finish within the season
	// by points. Wins are noisy in a twelve-team league; points are the
	// closest thing to a measure of the roster itself.
	Points float64
	Rank   int
}

// DraftedPlayer is one purchase.
type DraftedPlayer struct {
	Name     string
	Position string
	Price    int
	Keeper   bool
	// Points is what he went on to score that season, in Sleeper's
	// half-PPR scoring. Zero when unknown, which is how a player who never
	// played reads — the same as one who played and scored nothing, and
	// for the purpose of "was he worth the money" that is the right answer.
	Points float64
}

// Spend totals what was paid at a position, or everywhere when pos is empty.
func (t TeamSeason) Spend(pos string) int {
	total := 0
	for _, p := range t.Picks {
		if pos == "" || p.Position == pos {
			total += p.Price
		}
	}
	return total
}

// PricesAt returns the prices paid at a position, most expensive first.
func (t TeamSeason) PricesAt(pos string) []int {
	var out []int
	for _, p := range t.Picks {
		if pos == "" || p.Position == pos {
			out = append(out, p.Price)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// CountOver is how many players at a position cost more than price.
func (t TeamSeason) CountOver(pos string, price int) int {
	n := 0
	for _, p := range t.Picks {
		if (pos == "" || p.Position == pos) && p.Price > price {
			n++
		}
	}
	return n
}

// minTopFiveLine is the top-five price below which a position has no ladder
// worth correlating.
//
// Read at the fifth rank rather than the first because that is the line the
// report is about. Judging by the dearest player was fitted to one glance at
// one season and nearly broke: 2025's priciest defense went for exactly $10
// against a $10 guard, so the whole position was excluded only by the
// unrelated ten-player minimum, and one more drafted defense would have put
// a rho of +0.02 into the headline.
//
// The fifth-rank line separates cleanly instead. Defenses sit at $1 or $2
// there in every season on record; the cheapest real position is 2022
// quarterback at $8.
const minTopFiveLine = 5

// PriceRankFit is how closely what a position cost tracked how it finished,
// for one position in one season.
type PriceRankFit struct {
	Season   string
	Position string
	N        int
	// Rho is the rank correlation between price and finish.
	Rho float64
	// TopFive counts how many of the five most expensive went on to finish
	// in the position's top five. The correlation says the relationship
	// exists; this says how often it paid.
	TopFive int
}

// FitPriceRanks measures whether paying more bought a better finish.
//
// The question underneath the price lines. Telling somebody a bid is
// "top-five money" is only worth saying if top-five money has meant
// anything, and the honest answer is that it means something loose: across
// this league's drafts the correlation sits near +0.5, while the five
// dearest at a position finish in its top five well under half the time.
//
// Keepers are included. A keeper is money committed at a price, which is
// exactly what is being tested, and dropping them would throw away the
// cheapest players on every roster and flatter the curve.
func FitPriceRanks(seasons []TeamSeason) []PriceRankFit {
	type key struct{ season, pos string }
	grouped := map[key][]DraftedPlayer{}
	for _, ts := range seasons {
		for _, p := range ts.Picks {
			if p.Price <= 0 || p.Position == "" {
				continue
			}
			grouped[key{ts.Season, p.Position}] = append(grouped[key{ts.Season, p.Position}], p)
		}
	}

	var out []PriceRankFit
	for k, players := range grouped {
		if len(players) < 10 {
			// Too few to rank against each other; a four-man position
			// produces a correlation that is an artifact of its size.
			continue
		}
		sort.SliceStable(players, func(i, j int) bool { return players[i].Price > players[j].Price })

		// A position nobody bids on cannot answer this question. Defenses go
		// for a dollar or two here, so their "price rank" is mostly the
		// order the picks happened in, and correlating that against a
		// finish produces a confident number about nothing.
		if players[4].Price < minTopFiveLine {
			continue
		}

		// Raw values, not ranks computed here. Spearman does its own
		// ranking with ties averaged, and handing it ranks of my own would
		// throw that away — every player who did not play would get a
		// distinct made-up finish instead of sharing last place with the
		// others, which is a lot of invented ordering in a league where a
		// dozen picks never score.
		prices := make([]float64, len(players))
		points := make([]float64, len(players))
		for i, p := range players {
			prices[i], points[i] = float64(p.Price), p.Points
		}

		fit := PriceRankFit{Season: k.season, Position: k.pos, N: len(players), Rho: Spearman(prices, points)}

		// Whether the five dearest were among the five best. Ties in points
		// are broken by the sort, which can only matter at the boundary and
		// only between players who scored identically.
		byFinish := append([]DraftedPlayer(nil), players...)
		sort.SliceStable(byFinish, func(i, j int) bool { return byFinish[i].Points > byFinish[j].Points })
		best := map[string]bool{}
		for i := 0; i < 5 && i < len(byFinish); i++ {
			best[byFinish[i].Name] = true
		}
		for i := 0; i < 5 && i < len(players); i++ {
			if best[players[i].Name] {
				fit.TopFive++
			}
		}
		out = append(out, fit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Position < out[j].Position
	})
	return out
}

// Spearman is the rank correlation between two equal-length series.
//
// Rank rather than linear because the question is whether spending more on
// a position tends to finish higher, not whether the relationship is a
// straight line — and because one blow-up season would drag a linear fit
// around by itself.
//
// Computed as the correlation of the ranks rather than by the 1-6Σd² short
// form, which is only valid when nothing ties. Prices tie constantly — a
// column where six teams all spent $1 at a position is normal — and the
// short form reports a correlation of 0.5 for a series that is entirely
// constant, which is a number about the arithmetic rather than the league.
func Spearman(xs, ys []float64) float64 {
	if len(xs) != len(ys) || len(xs) < 3 {
		return 0
	}
	return pearson(ranks(xs), ranks(ys))
}

// pearson is the product-moment correlation, returning zero when either
// series has no variance to correlate.
func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx, my = mx/n, my/n

	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0
	}
	return sxy / math.Sqrt(sxx*syy)
}

// ranks returns 1-based ranks, averaging ties so a column of equal values
// cannot manufacture a correlation.
func ranks(xs []float64) []float64 {
	idx := make([]int, len(xs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return xs[idx[a]] < xs[idx[b]] })

	out := make([]float64, len(xs))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && xs[idx[j+1]] == xs[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			out[idx[k]] = avg
		}
		i = j + 1
	}
	return out
}

// significantRho is roughly where a Spearman correlation reaches p<0.05 at
// n=36. Stated so the report can say which correlations clear it rather
// than leaving the reader to judge a bare number.
const significantRho = 0.33

// WriteCalibration reports whether how a manager spent predicted how the
// season went.
//
// It used to fit named roster shapes against history as well. Those are gone:
// three seasons said none of them separated on results, and the machinery
// that filled them was answering questions a greedy search cannot answer.
// What survives is the question that never needed the shapes — whether the
// distribution of a budget has any relationship to points at all.
func WriteCalibration(w io.Writer, seasons []TeamSeason) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	bySeason := map[string]int{}
	for _, ts := range seasons {
		bySeason[ts.Season]++
	}
	years := make([]string, 0, len(bySeason))
	for y := range bySeason {
		years = append(years, y)
	}
	sort.Strings(years)
	fmt.Fprintf(tw, "CALIBRATION — %d rosters across %v\n\n", len(seasons), years)

	var pts []float64
	for _, ts := range seasons {
		pts = append(pts, ts.Points)
	}
	fmt.Fprintf(tw, "median finish %.1f, median points %.0f\n",
		median(rankSeries(seasons)), median(pts))

	writePriceRankFit(tw, seasons)

	fmt.Fprintf(tw, "\nDOES SPENDING SHAPE PREDICT POINTS?\t(n=%d, |rho| > %.2f is p<0.05)\n",
		len(seasons), significantRho)
	for _, m := range shapeMetrics() {
		var xs []float64
		for _, ts := range seasons {
			xs = append(xs, m.of(ts))
		}
		rho := Spearman(xs, pts)
		verdict := "no signal"
		if rho > significantRho || rho < -significantRho {
			verdict = "SIGNIFICANT"
		}
		fmt.Fprintf(tw, "%s\trho %+.2f\t%s\n", m.name, rho, verdict)
	}
	return tw.Flush()
}

func rankSeries(seasons []TeamSeason) []float64 {
	var out []float64
	for _, ts := range seasons {
		out = append(out, float64(ts.Rank))
	}
	return out
}

type shapeMetric struct {
	name string
	of   func(TeamSeason) float64
}

// shapeMetrics describe how a budget was divided, as continuous quantities.
//
// Continuous on purpose. Cutting the same information into named buckets was
// what the roster shapes did, and a bucket of nine rosters can look striking
// by accident where a rank correlation over all of them cannot hide behind a
// small denominator.
func shapeMetrics() []shapeMetric {
	nth := func(pos string, n int) func(TeamSeason) float64 {
		return func(t TeamSeason) float64 {
			p := t.PricesAt(pos)
			if len(p) <= n {
				return 0
			}
			return float64(p[n])
		}
	}
	topShare := func(n int) func(TeamSeason) float64 {
		return func(t TeamSeason) float64 {
			total := t.Spend("")
			if total == 0 {
				return 0
			}
			sum := 0
			for i, price := range t.PricesAt("") {
				if i >= n {
					break
				}
				sum += price
			}
			return float64(sum) / float64(total)
		}
	}
	return []shapeMetric{
		{"top-2 concentration", topShare(2)},
		{"top-3 concentration", topShare(3)},
		{"RB total spend", func(t TeamSeason) float64 { return float64(t.Spend("RB")) }},
		{"WR total spend", func(t TeamSeason) float64 { return float64(t.Spend("WR")) }},
		{"best RB price", nth("RB", 0)},
		{"second RB price", nth("RB", 1)},
		{"best WR price", nth("WR", 0)},
	}
}

// writePriceRankFit reports whether price rank tracked finish rank, which is
// what the board's price lines lean on.
func writePriceRankFit(tw *tabwriter.Writer, seasons []TeamSeason) {
	fits := FitPriceRanks(seasons)
	if len(fits) == 0 {
		return
	}
	fmt.Fprintf(tw, "\nDID PRICE RANK PREDICT FINISH RANK?\t(1.0 = perfectly, 0 = not at all)\n")

	var rhos []float64
	hits, of := 0, 0
	for _, f := range fits {
		fmt.Fprintf(tw, "%s %s\tn=%d\trho %+.2f\ttop-5 money finished top-5: %d of 5\n",
			f.Season, f.Position, f.N, f.Rho, f.TopFive)
		rhos = append(rhos, f.Rho)
		hits += f.TopFive
		of += 5
	}
	fmt.Fprintf(tw, "mean\t\trho %+.2f\t%d of %d\n", mean(rhos), hits, of)
	fmt.Fprintf(tw, "\nReal, and loose. The board may say a bid is top-five money; it may not\n")
	fmt.Fprintf(tw, "say the player will finish there.\n")
}

// mean of a slice, zero when empty.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var total float64
	for _, x := range xs {
		total += x
	}
	return total / float64(len(xs))
}

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

// shapeMetrics are the continuous forms of what the archetypes carve into
// buckets. Worth reporting alongside: a bucket of six rosters can look
// striking by accident, while a rank correlation over all of them cannot
// hide behind a small denominator.
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

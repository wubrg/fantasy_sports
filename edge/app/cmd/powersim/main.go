// powersim regenerates the belief-probe power table using the SHIPPED calib
// package — the same PairedBrierGain and BootstrapCI the verdict runs — so the
// table cannot drift from the code that decides the endpoint. The prior table
// was unreproducible from anything in the repo; this is its replacement.
//
// It fixes the three defects the 2026-09-04 review found in the old table:
//
//  1. α. The old table claimed "95%, one-sided" but read BootstrapCI's lower
//     bound at alpha=0.05, which is a one-sided 2.5% test. Here alpha=0.10 is
//     passed, so the lower bound is a genuine one-sided 5% bound, and the
//     verdict is expected to use the same.
//
//  2. Clustering. The old table treated a week as ~60 effective predictions
//     (game-level DEFF ~1.1). The real dependence is a TEAM across weeks: an
//     efficient_offense row for the same team is correlated week to week (ICC
//     ~0.11), and an LLM's read of a team persists too. This injects a team
//     random effect and clusters the bootstrap on team-season, so the interval
//     sees the true design effect (~1.7-2.0) rather than the game-level one.
//
//  3. Population. The decision scenarios after the S6 fix are shootout
//     (game-level, 16/week) and efficient_offense (per team, 32/week) — 48 raw
//     rows a week, not 112. And a forecaster that abstains freely, as the
//     contract asks, commits on only a fraction of them; the table is computed
//     at a stated commit rate and the effective n is printed beside it.
//
// The generative model, stated so the table is reproducible: the reference is
// the base rate b; on a committed row the forecaster knows the truth, which sits
// edge away from b in a random direction (so its expected paired-Brier gain per
// row is edge^2); y is a Bernoulli draw of that truth. efficient_offense rows
// carry a per-team random effect so rows sharing a team co-vary. Abstentions are
// excluded exactly as Positions() excludes them in scoring.
//
// "Hardest binds" (C-B) is NOT modelled: E1 now requires beating EVERY opponent,
// so the real power is AT MOST what this single-reference table shows. Read the
// numbers as a ceiling.
//
//	go run ./cmd/powersim            # the table
//	go run ./cmd/powersim -commit 0.4 -trials 2000
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"

	"edge/internal/calib"
)

func main() {
	base := flag.Float64("base", 0.35, "reference base rate")
	commit := flag.Float64("commit", 0.40, "fraction of rows the forecaster takes a position on")
	teamICC := flag.Float64("icc", 0.11, "within-team-season correlation of efficient_offense outcomes")
	trials := flag.Int("trials", 500, "simulation trials per cell")
	iters := flag.Int("iters", 400, "bootstrap iterations (the verdict uses 800; 400 is enough for a power estimate)")
	alpha := flag.Float64("alpha", 0.10, "two-sided alpha; the lower bound is one-sided alpha/2 = 5%")
	flag.Parse()

	weeks := []int{1, 3, 6, 8, 12, 18}
	edges := []float64{0.05, 0.10, 0.15, 0.20}

	fmt.Printf("belief-probe power, shipped calib, one-sided %.0f%%\n", *alpha/2*100)
	fmt.Printf("base %.2f, commit rate %.2f, team-season ICC %.2f, %d trials, %d bootstrap iters\n\n",
		*base, *commit, *teamICC, *trials, *iters)
	fmt.Printf("decision scenarios: shootout (16/wk, ~independent) + efficient_offense (32/wk, team-clustered)\n")
	fmt.Printf("raw rows/week = 48; committed/week ≈ %.0f\n\n", 48*(*commit))

	fmt.Printf("%5s %8s %9s", "weeks", "commit-n", "eff-n")
	for _, e := range edges {
		fmt.Printf("  edge+%.2f", e)
	}
	fmt.Println()

	for _, w := range weeks {
		// One representative dataset to report the committed and effective n.
		rep := simWeek(w, *base, *commit, *teamICC, 0.10, rand.New(rand.NewSource(1)))
		effN := effectiveN(rep)
		fmt.Printf("%5d %8d %9.0f", w, len(calib.Positions(rep)), effN)
		for _, e := range edges {
			p := power(w, *base, *commit, *teamICC, e, *trials, *iters, *alpha)
			fmt.Printf("  %7.0f%%", p*100)
		}
		fmt.Println()
	}
}

// power is the share of trials whose one-sided lower bound clears zero.
func power(weeks int, base, commit, icc, edge float64, trials, iters int, alpha float64) float64 {
	rng := rand.New(rand.NewSource(int64(weeks)*1000 + int64(edge*1000)))
	pass := 0
	for t := 0; t < trials; t++ {
		pts := simWeek(weeks, base, commit, icc, edge, rng)
		lo, _ := calib.BootstrapCI(pts, calib.PairedBrierGain, iters, rng.Int63(), alpha)
		if !math.IsNaN(lo) && lo > 0 {
			pass++
		}
	}
	return float64(pass) / float64(trials)
}

// simWeek builds `weeks` of decision-scenario rows. Abstentions are included as
// points so the population matches what Score/Positions see; only committed rows
// carry an edge.
func simWeek(weeks int, base, commit, icc, edge float64, rng *rand.Rand) []calib.Point {
	var pts []calib.Point
	// A season has 32 teams; efficient_offense clusters on the team across weeks.
	teamEffectSD := math.Sqrt(icc * base * (1 - base))
	teamEffect := map[int]float64{}
	teamOf := func(id int) float64 {
		if _, ok := teamEffect[id]; !ok {
			teamEffect[id] = rng.NormFloat64() * teamEffectSD
		}
		return teamEffect[id]
	}

	// The forecaster's read of a team PERSISTS across the season: the sign of its
	// edge on efficient_offense is fixed per team-season, so its gains co-vary
	// within a team (correlated same-direction hits or misses). This is the
	// clustering the review said exceeds the outcome's own — the gain, not just
	// the outcome, is what the bootstrap must see resampled by team.
	teamDir := map[int]float64{}
	dirOf := func(id int) float64 {
		if _, ok := teamDir[id]; !ok {
			if rng.Float64() < 0.5 {
				teamDir[id] = -1
			} else {
				teamDir[id] = 1
			}
		}
		return teamDir[id]
	}

	for wk := 1; wk <= weeks; wk++ {
		// shootout: 16 games a week, one row each, direction independent per game.
		for g := 0; g < 16; g++ {
			d := 1.0
			if rng.Float64() < 0.5 {
				d = -1
			}
			pts = append(pts, row(base, edge, commit, 0, d, fmt.Sprintf("g-%d-%d", wk, g), rng))
		}
		// efficient_offense: 32 team-rows a week, clustered by team-season with a
		// persistent per-team edge direction.
		for team := 0; team < 32; team++ {
			pts = append(pts, row(base, edge, commit, teamOf(team), dirOf(team), fmt.Sprintf("eo-%d", team), rng))
		}
	}
	return pts
}

// row makes one point: an abstention (excluded from scoring) or a committed
// forecast whose belief sits `edge` from base in a random direction.
//
// The team effect perturbs only the OUTCOME probability, never the forecaster's
// belief or the reference — it is irreducible clustered noise that neither side
// knows, which is what a team's week-to-week persistence is. This keeps the mean
// paired gain at edge^2 while making same-team gains co-vary, so the cluster
// bootstrap sees the real design effect. (An earlier version folded the shift
// into the belief, which handed the informed forecaster credit for knowing each
// team's level and tripled the apparent edge.)
func row(base, edge, commit, teamShift, dir float64, cluster string, rng *rand.Rand) calib.Point {
	p := calib.Point{Ref: base, HasRef: true, Cluster: cluster}
	q := clip(base + teamShift) // outcome probability, forecaster does not see the shift
	if rng.Float64() >= commit {
		p.P = base
		p.Abstained = true
		p.Y = rng.Float64() < q
		return p
	}
	p.P = clip(base + dir*edge) // informed on the signal, blind to the team noise
	p.Y = rng.Float64() < clip(q+dir*edge)
	return p
}

func clip(x float64) float64 {
	if x < 0.01 {
		return 0.01
	}
	if x > 0.99 {
		return 0.99
	}
	return x
}

// effectiveN backs out the design effect the bootstrap actually sees: the ratio
// of the naive iid variance of the gain to the clustered variance, times n.
func effectiveN(pts []calib.Point) float64 {
	pos := calib.Positions(pts)
	n := len(withReference(pos))
	if n == 0 {
		return 0
	}
	// Cluster-robust vs iid variance of the per-row gain gives the design effect.
	deff := designEffect(pos)
	if deff <= 0 {
		return float64(n)
	}
	return float64(n) / deff
}

func withReference(pts []calib.Point) []calib.Point {
	var out []calib.Point
	for _, p := range pts {
		if p.HasRef {
			out = append(out, p)
		}
	}
	return out
}

// designEffect estimates DEFF = 1 + (m̄−1)·ICC of the per-row gain, via the ratio
// of the between-cluster variance of cluster means to the pooled row variance.
func designEffect(pts []calib.Point) float64 {
	gains := map[string][]float64{}
	var all []float64
	for _, p := range pts {
		if !p.HasRef {
			continue
		}
		dp := p.P - y(p)
		dr := p.Ref - y(p)
		g := dr*dr - dp*dp
		gains[p.Cluster] = append(gains[p.Cluster], g)
		all = append(all, g)
	}
	if len(all) < 2 {
		return 1
	}
	grand := mean(all)
	rowVar := variance(all, grand)
	if rowVar == 0 {
		return 1
	}
	// One-way ANOVA between/within to get ICC, then DEFF at the mean cluster size.
	var ssB, nClusters, sumM float64
	for _, gs := range gains {
		m := mean(gs)
		ssB += float64(len(gs)) * (m - grand) * (m - grand)
		nClusters++
		sumM += float64(len(gs))
	}
	mbar := sumM / nClusters
	msB := ssB / math.Max(nClusters-1, 1)
	// ICC ≈ (msB − rowVar) / (msB + (mbar−1)·rowVar), floored at 0.
	icc := (msB - rowVar) / (msB + (mbar-1)*rowVar)
	if icc < 0 {
		icc = 0
	}
	return 1 + (mbar-1)*icc
}

func y(p calib.Point) float64 {
	if p.Y {
		return 1
	}
	return 0
}
func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
func variance(xs []float64, m float64) float64 {
	var s float64
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return s / float64(len(xs))
}

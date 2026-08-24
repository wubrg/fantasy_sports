package main

import (
	"errors"
	"flag"
	"fmt"

	"edge/internal/scenario"
)

func beliefCmd(args []string) error {
	fs := flag.NewFlagSet("belief", flag.ExitOnError)
	name := fs.String("name", "", "scenario to price the probability of")
	prior := fs.Float64("prior", 0, "the team's prior-form value for this scenario's quantity")
	supplied := map[string]bool{}
	if err := fs.Parse(args); err != nil {
		return err
	}
	fs.Visit(func(f *flag.Flag) { supplied[f.Name] = true })

	b, err := scenario.LoadBelief()
	if err != nil {
		return err
	}
	if *name == "" {
		fmt.Printf("BELIEF MODELS  (%s, %d-%d)\n\n", b.GeneratedAt[:10], b.Seasons[0], b.Seasons[1])
		fmt.Printf("  %s\n\n", b.Note)
		for _, n := range b.Names() {
			m := b.Scenarios[n]
			fmt.Printf("  %-20s %s > %g\n", n, m.Field, m.Threshold)
			fmt.Printf("  %-20s base rate %.3f, spread across bands %.3f, monotone %v\n",
				"", m.BaseRate, m.Spread, m.Monotone)
		}
		fmt.Printf("\n  edgectl belief -name <scenario> -prior <value>\n")
		return nil
	}
	if !supplied["prior"] {
		return fmt.Errorf("-prior is required: the team's own prior-form value, which is " +
			"what the model reads. Without it there is nothing to look up")
	}

	band, m, err := b.Lookup(*name, *prior)
	if err != nil {
		return err
	}
	fmt.Printf("BELIEF  %s  (%s > %g)\n", *name, m.Field, m.Threshold)
	fmt.Printf("  prior form %g falls in band %s\n", *prior, band.Range())
	fmt.Printf("\n  s = %.3f   P(this scenario occurs)\n", band.P)
	fmt.Printf("  fitted on %d team-weeks through %d\n", band.N, m.Split)
	if band.HeldP != nil {
		fmt.Printf("  held out: %.3f on %d team-weeks (%+.1fpp)\n",
			*band.HeldP, band.HeldN, (*band.HeldP-band.P)*100)
	}
	fmt.Printf("\n  base rate for any team is %.3f, so this is %+.1fpp of information\n",
		m.BaseRate, (band.P-m.BaseRate)*100)
	// The ordering survives out of sample and the level does not, so the drift
	// is stated here rather than left in the fit log.
	fmt.Printf("  CAVEAT the band ORDER holds out of sample; the LEVEL drifts by up to\n")
	fmt.Printf("         %.1fpp, so treat s as a rank rather than a precise probability.\n",
		m.WorstBandShift*100)
	fmt.Printf("\n  edgectl scenario -name %s -smarket %.3f ...\n", *name, band.P)
	return nil
}

// beliefFor is used by `scenario` to derive s where the operator would
// otherwise have had to invent it.
func beliefFor(name string, prior float64) (float64, error) {
	b, err := scenario.LoadBelief()
	if err != nil {
		return 0, err
	}
	band, _, err := b.Lookup(name, prior)
	if err != nil {
		if errors.Is(err, scenario.ErrNoBeliefModel) {
			return 0, err
		}
		return 0, err
	}
	return band.P, nil
}

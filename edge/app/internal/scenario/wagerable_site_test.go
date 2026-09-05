package scenario

import "testing"

// TestBestWagerableSite pins the site the belief probe's E2 wagers against.
//
// It must exist for the scenarios a belief can be spent on, price the scenario
// ABOVE its absence (q > r, or the wager makes no sense), and refuse the
// scenario that validates nowhere. The exact q and r move with a refit; the
// shape does not.
func TestBestWagerableSite(t *testing.T) {
	c, err := LoadConditionals()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"shootout", "efficient_offense"} {
		q, r, site, ok := c.BestWagerableSite(name)
		if !ok {
			t.Errorf("%s has no wagerable site; E2 can never fire for it", name)
			continue
		}
		if q <= r {
			t.Errorf("%s: q=%.3f is not above r=%.3f — the scenario must raise the prop", name, q, r)
		}
		if q-r < 0.05 {
			t.Errorf("%s: separation %.3f is too small to be the BEST site", name, q-r)
		}
		if site == "" {
			t.Errorf("%s: a frozen site must name where it came from", name)
		}
		if q < 0 || q > 1 || r < 0 || r > 1 {
			t.Errorf("%s: q=%.3f r=%.3f are not probabilities", name, q, r)
		}
	}

	// blowout_loss has no site where BOTH halves validate and ratio 1.0 is on
	// support, so it must have no wagerable site -- scoring it for accuracy is
	// free, but there is nothing to bet.
	if _, _, _, ok := c.BestWagerableSite("blowout_loss"); ok {
		t.Error("blowout_loss returned a wagerable site; no cell pair validates")
	}
}

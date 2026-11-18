package main

import (
	"math"
	"testing"
)

func TestPriceSGPScenarioMixture(t *testing.T) {
	legs, err := parseSGPLegs("0.8:0.5:-200:a,0.8:0.5:-200:b,0.8:0.5:-200:c,0.8:0.5:-200:d,0.8:0.5:-200:e")
	if err != nil {
		t.Fatal(err)
	}
	res, err := priceSGPScenario(0.4, legs)
	if err != nil {
		t.Fatal(err)
	}
	wantIn := math.Pow(0.8, 5)  // 0.32768
	wantOut := math.Pow(0.5, 5) // 0.03125
	want := 0.4*wantIn + 0.6*wantOut
	if math.Abs(res.pIn-wantIn) > 1e-12 || math.Abs(res.pOut-wantOut) > 1e-12 || math.Abs(res.pAll-want) > 1e-12 {
		t.Fatalf("got in=%v out=%v all=%v, want %v %v %v", res.pIn, res.pOut, res.pAll, wantIn, wantOut, want)
	}
	// five legs at -200 (decimal 1.5): 1.5^5 = 7.59375
	if math.Abs(res.naiveDec-math.Pow(1.5, 5)) > 1e-9 {
		t.Fatalf("naive decimal %v", res.naiveDec)
	}
	// fair price is +(1/P - 1)*100, positive here (P ~ 0.1498)
	if int(res.fair) < 560 || int(res.fair) > 570 {
		t.Fatalf("fair %+d, want about +567", int(res.fair))
	}
}

func TestPriceSGPScenarioReducesToIndependentAtCertainty(t *testing.T) {
	legs, _ := parseSGPLegs("0.7:0.4:-150,0.6:0.3:+110,0.9:0.6:-400")
	res, _ := priceSGPScenario(0.999999, legs)
	if math.Abs(res.pAll-0.7*0.6*0.9) > 1e-5 {
		t.Fatalf("s~1 should give prod(q), got %v", res.pAll)
	}
}

func TestParseSGPLegsRejects(t *testing.T) {
	for _, bad := range []string{"", "0.5:0.4", "1.2:0.4:-110", "0.5:0:-110", "0.5:0.4:abc", "0.5:0.4:+50"} {
		if _, err := parseSGPLegs(bad); err == nil {
			t.Errorf("parseSGPLegs(%q) accepted", bad)
		}
	}
	legs, err := parseSGPLegs("0.6:0.4:+120:Mixon rush 50+")
	if err != nil || legs[0].label != "Mixon rush 50+" || int(legs[0].price) != 120 {
		t.Fatalf("label/price parse: %+v %v", legs, err)
	}
}

package main

import (
	"testing"
)

func TestParseCorrelation(t *testing.T) {
	specs, err := parseCorrelation("mia@lv:0.3;was@phi:0.5,0.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d: %+v", len(specs), specs)
	}
	if specs[0].game != "mia@lv" || len(specs[0].rhos) != 1 || specs[0].rhos[0] != 0.3 {
		t.Errorf("spec 0 = %+v, want game mia@lv, rhos [0.3]", specs[0])
	}
	if specs[1].game != "was@phi" || len(specs[1].rhos) != 2 ||
		specs[1].rhos[0] != 0.5 || specs[1].rhos[1] != 0.2 {
		t.Errorf("spec 1 = %+v, want game was@phi, rhos [0.5, 0.2]", specs[1])
	}
}

func TestParseCorrelationEmptyStringIsNoSpecsNotAnError(t *testing.T) {
	specs, err := parseCorrelation("")
	if err != nil {
		t.Fatal(err)
	}
	if specs != nil {
		t.Errorf("empty -correlation should produce no specs, got %+v", specs)
	}
}

func TestParseCorrelationRejectsAMalformedEntry(t *testing.T) {
	if _, err := parseCorrelation("noColonHere"); err == nil {
		t.Error("an entry with no game:rhos separator should be rejected")
	}
	if _, err := parseCorrelation("mia@lv:not-a-number"); err == nil {
		t.Error("a non-numeric rho should be rejected")
	}
	if _, err := parseCorrelation(":0.5"); err == nil {
		t.Error("an empty game tag should be rejected")
	}
}

func TestParseCorrelationRejectsARhoOutsideRange(t *testing.T) {
	if _, err := parseCorrelation("mia@lv:1.5"); err == nil {
		t.Error("a rho above 1 should be rejected at parse time, not deferred to FrechetBlend")
	}
}

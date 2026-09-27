package state

// ParseStep is String's inverse (added with the cast-timing window features:
// the seat's view-shaped Board adapter parses the projected step string back
// to the exact engine step). The round-trip is the property that keeps the
// two adapters naming the same window.

import "testing"

func TestParseStepRoundTripsEveryStep(t *testing.T) {
	for s := Step(0); s.Valid(); s++ {
		got, ok := ParseStep(s.String())
		if !ok || got != s {
			t.Fatalf("ParseStep(%q) = (%v, %v), want (%v, true)", s.String(), got, ok, s)
		}
	}
}

func TestParseStepRejectsUnknownName(t *testing.T) {
	if s, ok := ParseStep("unknown"); ok {
		t.Fatalf("ParseStep(%q) = (%v, true), want ok=false", "unknown", s)
	}
	if s, ok := ParseStep(""); ok {
		t.Fatalf("ParseStep(\"\") = (%v, true), want ok=false", s)
	}
	if s, ok := ParseStep("Main1"); ok {
		t.Fatalf("ParseStep(%q) = (%v, true), want ok=false — the names are lowercase", "Main1", s)
	}
}

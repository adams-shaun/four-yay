package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestQuietSubtypeKeepsAHarmlessEtbCounterCreature pins the printed-P/T gate
// on entersWithCounters. A 0/0 etbCounter creature (Academy Elite) dies on the
// battlefield without entering through a cast, so it is not an inert setup
// fixture; a 1/1 etbCounter creature (Arctic Merfolk, whose counter keyword is
// conditional) survives setup and must stay a legal quiet fixture. Before the
// gate, any etbCounter keyword excluded a face, which dropped Arctic Merfolk
// and moved ECL Sygg's Command's frozen Level-A verdict to a different fixture.
func TestQuietSubtypeKeepsAHarmlessEtbCounterCreature(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// The fallback name heuristic, so a sibling test's installed manifest set
	// cannot decide the picker's answer for the synthetic registry below.
	SetXMageKnown(nil)
	t.Cleanup(func() { SetXMageKnown(nil) })

	academy, ok := reg.Lookup("Academy Elite")
	if !ok || len(academy.Faces) == 0 {
		t.Fatal("Academy Elite not in corpus: the fixture's negative case is missing")
	}
	arctic, ok := reg.Lookup("Arctic Merfolk")
	if !ok || len(arctic.Faces) == 0 {
		t.Fatal("Arctic Merfolk not in corpus: the fixture's positive case is missing")
	}

	// Precondition: the two faces really differ in the way the predicate reads
	// them -- printed P/T -- so a same-answer result would be a real failure,
	// not a vacuous setup.
	if got := arctic.Faces[0].PT; got != "1/1" {
		t.Fatalf("Arctic Merfolk PT = %q, want 1/1 (the gate's positive case)", got)
	}
	if got := academy.Faces[0].PT; got != "0/0" {
		t.Fatalf("Academy Elite PT = %q, want 0/0 (the gate's negative case)", got)
	}

	if !faceHasEtbCounter(arctic.Faces[0]) {
		t.Fatal("Arctic Merfolk carries no etbCounter keyword: test is vacuous")
	}
	if !faceHasEtbCounter(academy.Faces[0]) {
		t.Fatal("Academy Elite carries no etbCounter keyword: test is vacuous")
	}

	if entersWithCounters(arctic.Faces[0]) {
		t.Errorf("entersWithCounters(Arctic Merfolk 1/1) = true, want false: a 1/1 survives setup")
	}
	if !entersWithCounters(academy.Faces[0]) {
		t.Errorf("entersWithCounters(Academy Elite 0/0) = false, want true: a 0/0 dies on the battlefield")
	}

	// A controlled registry, so the picker's corpus order cannot make the
	// assertion pass or fail by accident: both faces are Merfolk, and the
	// harmful 0/0 is listed first, so only the printed-P/T gate can let the
	// picker reach the second.
	synth := func(name, pt, kw string) *cards.Card {
		return &cards.Card{Faces: []*cards.Face{{
			Name:     name,
			PT:       pt,
			Types:    []string{"Creature", "Merfolk"},
			Keywords: []string{kw},
		}}}
	}
	ctrl := cards.NewRegistry()
	ctrl.Add(synth("Doomed Merfolk", "0/0", "etbCounter:P1P1:1"))
	ctrl.Add(synth("Hardy Merfolk", "1/1", "etbCounter:P1P1:1"))
	got, ok := registryQuietSubtype(ctrl, "Merfolk")
	if !ok || got != "Hardy Merfolk" {
		t.Errorf("registryQuietSubtype(Merfolk) = %q,%v, want Hardy Merfolk,true", got, ok)
	}
}

// faceHasEtbCounter mirrors the keyword test inside entersWithCounters so the
// regression test can assert the setup is non-vacuous (the face really carries
// the keyword the predicate looks at).
func faceHasEtbCounter(f *cards.Face) bool {
	for _, kw := range f.Keywords {
		if strings.HasPrefix(strings.ToLower(kw), "etbcounter") {
			return true
		}
	}
	return false
}

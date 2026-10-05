package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

func TestTypesCollapsesAllCreatureSubtypesToDriverMarker(t *testing.T) {
	typesWithAllSubtypes := append([]string{"Creature", "Legendary"}, effects.CreatureTypeWordList()...)
	got := types(typesWithAllSubtypes)
	if got != "allcreaturetypes creature legendary" {
		t.Fatalf("types() = %q, want driver's all-creature-types marker and non-subtype types", got)
	}
	if strings.Contains(got, "advisor") {
		t.Fatalf("expanded subtype list was not collapsed: %q", got)
	}
}

func TestTypesDoesNotCollapseIncompleteLargeSubtypeList(t *testing.T) {
	all := effects.CreatureTypeWordList()
	if len(all) < 102 {
		t.Fatalf("precondition: expected a large subtype vocabulary, got %d", len(all))
	}
	incomplete := append([]string{"Creature"}, all[:len(all)-1]...)
	incomplete = append(incomplete, "Imaginary Subtype")
	got := types(incomplete)
	if strings.Contains(got, "allcreaturetypes") {
		t.Fatalf("incomplete large type list collapsed: %q", got)
	}
	if !strings.Contains(got, "imaginarysubtype") || !strings.Contains(got, "advisor") {
		t.Fatalf("incomplete subtypes were discarded: %q", got)
	}
	complete := types(append([]string{"Creature"}, all...))
	if got == complete {
		t.Fatalf("distinct complete and incomplete type lists canonicalized equally: %q", got)
	}
}

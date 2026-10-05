package oraclediff

import (
	"strings"
	"testing"
)

func TestTypesCollapsesAllCreatureSubtypesToDriverMarker(t *testing.T) {
	subtypes := []string{"Creature"}
	for i := 0; i < 300; i++ {
		subtypes = append(subtypes, "Subtype "+string(rune('A'+i%26)))
	}
	got := types(subtypes)
	if got != "allcreaturetypes creature" {
		t.Fatalf("types() = %q, want driver's single all-creature-types marker", got)
	}
	if strings.Contains(got, "subtype") {
		t.Fatalf("expanded subtype list was not collapsed: %q", got)
	}
}

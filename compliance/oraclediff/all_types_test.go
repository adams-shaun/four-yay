package oraclediff

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

func TestTypesUsesExplicitAllCreatureTypesFlag(t *testing.T) {
	withAllSubtypes := append([]string{"Creature", "Legendary"}, effects.CreatureTypeWordList()...)
	got := types(withAllSubtypes, true)
	if got != "allcreaturetypes creature legendary" {
		t.Fatalf("types() = %q, want explicit all-creature-types marker and non-subtype types", got)
	}
	if strings.Contains(got, "advisor") {
		t.Fatalf("expanded subtype list was not collapsed: %q", got)
	}
	// The explicit flag, not the apparent completeness of the current
	// vocabulary, controls canonicalization.
	withoutFlag := types(withAllSubtypes, false)
	if withoutFlag == got || !strings.Contains(withoutFlag, "advisor") {
		t.Fatalf("complete ordinary subtype list was inferred as all types: %q", withoutFlag)
	}
}

func TestTypesTrueFlagCanonicalizesDifferentEnumerations(t *testing.T) {
	gorge := []string{"Creature", "Bear", "Elf", "Legendary"}
	xmage := []string{"Creature", "Advisor", "Sliver", "Legendary"}
	gotGorge, gotXMage := types(gorge, true), types(xmage, true)
	if gotGorge != gotXMage {
		t.Fatalf("explicit all-types snapshots differ: %q vs %q", gotGorge, gotXMage)
	}
	if types([]string{"Creature", "Bear"}, false) == types([]string{"Creature", "Elf"}, false) {
		t.Fatal("false/missing flag erased ordinary subtype distinction")
	}
	if got := types([]string{"Creature", "Artifact", "Legendary", "Bear"}, true); got != "allcreaturetypes artifact creature legendary" {
		t.Fatalf("true flag discarded unrelated types: %q", got)
	}
}

func TestOracleSnapshotAllCreatureTypesJSONField(t *testing.T) {
	var snapshot rules.OracleSnapshot
	if err := json.Unmarshal([]byte(`{"permanents":[{"name":"Grizzly Bears","types":["Creature","Bear"],"all_creature_types":true}]}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Permanents) != 1 || !snapshot.Permanents[0].AllCreatureTypes {
		t.Fatalf("explicit XMage flag was lost decoding: %+v", snapshot.Permanents)
	}
	if got := permKeys(snapshot.Permanents); len(got) != 1 || !strings.Contains(got[0], "[allcreaturetypes creature]") {
		t.Fatalf("decoded flag was not used by permanent canonicalization: %v", got)
	}
}

func TestTypesDoesNotCollapseIncompleteLargeSubtypeList(t *testing.T) {
	all := effects.CreatureTypeWordList()
	if len(all) < 102 {
		t.Fatalf("precondition: expected a large subtype vocabulary, got %d", len(all))
	}
	incomplete := append([]string{"Creature"}, all[:len(all)-1]...)
	incomplete = append(incomplete, "Imaginary Subtype")
	got := types(incomplete, false)
	if strings.Contains(got, "allcreaturetypes") {
		t.Fatalf("incomplete large type list collapsed: %q", got)
	}
	if !strings.Contains(got, "imaginarysubtype") || !strings.Contains(got, "advisor") {
		t.Fatalf("incomplete subtypes were discarded: %q", got)
	}
	complete := types(append([]string{"Creature"}, all...), false)
	if got == complete {
		t.Fatalf("distinct complete and incomplete type lists canonicalized equally: %q", got)
	}
}

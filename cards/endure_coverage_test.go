package cards

import (
	"reflect"
	"sort"
	"testing"
)

// TestEndureCorpusCensus pins every corpus card whose compiled primitives
// include api:Endure, so a newly-added Endure carrier fails the build instead
// of silently escaping the mechanic's coverage. The list is measured over the
// pinned FORGE_REF corpus; a corpus bump that adds a carrier must extend it
// deliberately (the same both-directions ratchet the rules coverage table
// uses).
func TestEndureCorpusCensus(t *testing.T) {
	r := compiledCorpus(t)
	var got []string
	for _, card := range r.Cards {
		for _, primitive := range card.Primitives() {
			if primitive == "api:Endure" {
				got = append(got, card.Faces[0].Name)
				break
			}
		}
	}
	sort.Strings(got)
	want := []string{
		"Amber-Plate Ainok",
		"Anafenza, Unyielding Lineage",
		"Descendant of Storms",
		"Dusyut Earthcarver",
		"Fortress Kin-Guard",
		"Hamza, Might of the Yathan",
		"Inspirited Vanguard",
		"Kin-Tree Nurturer",
		"Krumar Initiate",
		"Sandskitter Outrider",
		"Sinkhole Surveyor",
		"Warden of the Grove",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("api:Endure corpus census = %v, want %v", got, want)
	}
}

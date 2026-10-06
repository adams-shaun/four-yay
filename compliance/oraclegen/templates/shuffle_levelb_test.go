package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestGeneratedFblthpLevelBTriggerSkipsLibraryOrder(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup("Fblthp, Impossibly Lost")
	if !ok {
		t.Fatal("Fblthp is absent from the corpus")
	}
	var req *levelb.Requirement
	for _, candidate := range levelb.Requirements(card) {
		if candidate.Key == "trigger#0.0" {
			r := candidate
			req = &r
			break
		}
	}
	if req == nil {
		t.Fatal("Fblthp trigger#0.0 requirement not classified")
	}
	item, skip := GenerateB(reg, "Fblthp, Impossibly Lost", *req)
	if skip != nil {
		t.Fatalf("GenerateB(Fblthp trigger#0.0): %s", skip.Reason)
	}
	if item.ID != "Fblthp, Impossibly Lost/trigger#0.0/v1" {
		t.Fatalf("generated wrong requirement: %q", item.ID)
	}
	if !strings.Contains(strings.ToLower(card.Faces[0].Oracle), "shuffle") {
		t.Fatal("precondition: Fblthp face no longer has its library-shuffle text")
	}
	found := false
	for _, option := range item.Compare {
		if option == oraclegen.CompareNoLibraryOrder {
			found = true
		}
	}
	if !found {
		t.Fatalf("actual level-B trigger item missing shuffle comparison mark: %v", item.Compare)
	}
}

// The mark is level-B only: a level-A item for the same face keeps an empty
// Compare, so its scenario sha and frozen verdict do not move.
func TestLevelAFblthpItemCarriesNoLibraryOrderMark(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup("Fblthp, Impossibly Lost")
	if !ok || !oraclegen.CanShuffleLibrary(card.Faces[0]) {
		t.Fatal("precondition: Fblthp is absent or its face is not shuffle-marked")
	}
	it, skip := Generate(reg, "Fblthp, Impossibly Lost")
	if skip != nil {
		t.Fatalf("Fblthp: %s", skip.Reason)
	}
	if len(it.Compare) != 0 {
		t.Fatalf("level-A Fblthp item %s carries compare options %v", it.ID, it.Compare)
	}
}

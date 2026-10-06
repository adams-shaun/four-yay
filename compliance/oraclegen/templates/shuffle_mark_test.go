package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestGeneratedFblthpShuffleItemOptsOutOfLibraryOrder(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Fblthp, Impossibly Lost")
	if skip != nil {
		t.Fatalf("Fblthp: %s", skip.Reason)
	}
	if len(it.Compare) != 1 || it.Compare[0] != oraclegen.CompareNoLibraryOrder {
		t.Fatalf("generated Fblthp item missing shuffle comparison mark: %v", it.Compare)
	}
}

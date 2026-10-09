package gate

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

// TestCheckDeclaredMatchesCheckPerSet pins the bulk path to the per-set
// path: CheckDeclared's only job over Check is loading the shared tables
// once, so for any one set its problems must be element-identical to
// Check's. The set is deliberately a small declared one (BIG, 30 cards) so
// the test stays inside the per-test budget. Without the declared level the
// comparison would not exercise the claim's own level, so the set is looked
// up in declared.json and its absence is a precondition failure.
func TestCheckDeclaredMatchesCheckPerSet(t *testing.T) {
	root := "../.."
	declared, err := compliance.LoadDeclared(root + "/compliance/declared.json")
	if err != nil {
		t.Fatal(err)
	}
	const set = "BIG"
	level, ok := declared[set]
	if !ok {
		t.Fatalf("precondition: %s is not declared (compliance/declared.json); pick a small declared set", set)
	}
	reg, err := cards.SharedCorpus(root + "/.cards")
	if err != nil {
		t.Fatalf("need the corpus (make fetch-cards compile-cards): %v", err)
	}
	single, err := Check(reg, root, set, level)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	bulk, err := CheckDeclared(reg, root, map[string]string{set: level})
	if err != nil {
		t.Fatalf("CheckDeclared: %v", err)
	}
	if len(bulk) != 1 || bulk[0].Set != set || bulk[0].Level != level {
		t.Fatalf("CheckDeclared returned %+v, want one entry for %s at %s", bulk, set, level)
	}
	if !reflect.DeepEqual(single, bulk[0].Problems) {
		t.Errorf("bulk path diverged from the per-set path on %s at %s:\n per set: %+v\n bulk:    %+v", set, level, single, bulk[0].Problems)
	}
}

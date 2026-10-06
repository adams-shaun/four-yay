package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestShuffleOptOutIgnoresOnlyLibraryOrder(t *testing.T) {
	if err := ValidateCompare([]string{CompareNoLibraryOrder}); err != nil {
		t.Fatalf("shuffle comparison option rejected: %v", err)
	}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("step 1 (resolve)")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("step 1 (resolve)")}}
	g.Snapshots[0].Players[0].LibraryTop = []string{"Wastes", "Fblthp, Impossibly Lost", "Wastes"}
	x.Snapshots[0].Players[0].LibraryTop = []string{"Wastes", "Wastes", "Wastes"}
	if g.Snapshots[0].Players[0].LibraryTop[1] == x.Snapshots[0].Players[0].LibraryTop[1] {
		t.Fatal("precondition: library orders must differ")
	}
	if v := CompareOpts(g, nil, x, nil); v.Status != Diverge || v.Field != "p0.library_top" {
		t.Fatalf("unmarked item must retain order comparison, got %+v", v)
	}
	if v := CompareOpts(g, nil, x, []string{CompareNoLibraryOrder}); v.Status != Agree {
		t.Fatalf("marked shuffled item should agree when only order differs: %+v", v)
	}
	x.Snapshots[0].Players[0].LibraryCount--
	if v := CompareOpts(g, nil, x, []string{CompareNoLibraryOrder}); v.Status != Diverge || v.Field != "p0.library_count" {
		t.Fatalf("library count must remain compared: %+v", v)
	}
}

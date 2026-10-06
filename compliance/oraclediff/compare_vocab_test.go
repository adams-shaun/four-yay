package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The library-order opt-in is NAMED on the generator side
// (oraclegen.CompareNoLibraryOrder, set on an Item by NewItem/GenerateB) and
// VALIDATED on the comparator side (oraclediff.CompareNoLibraryOrder, read by
// ValidateCompare and fields). They are separate constants in separate
// packages, so the wire value can drift silently: oraclegen would set an
// option the comparator no longer recognises, ValidateCompare would not run on
// a real pass, and every marked item would compare a random library order
// again. Pin the two together.
func TestLibraryOrderOptionMatchesGenerator(t *testing.T) {
	if CompareNoLibraryOrder != oraclegen.CompareNoLibraryOrder {
		t.Fatalf("comparator option %q != generator option %q", CompareNoLibraryOrder, oraclegen.CompareNoLibraryOrder)
	}
}

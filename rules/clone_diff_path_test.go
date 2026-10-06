package rules

import (
	"reflect"
	"testing"
)

// TestCloneFirstDiffRendersTheEagerPath pins the lazily rendered path of
// cloneFirstDiff to the form the eager string concatenation produced: a
// field, an index, a pointer dereference and a map key, nested.
func TestCloneFirstDiffRendersTheEagerPath(t *testing.T) {
	t.Parallel()
	type leaf struct{ M map[string]int }
	type root struct {
		Skip int
		L    []*leaf
	}
	a := root{L: []*leaf{{M: map[string]int{"k": 1}}, {M: map[string]int{"k": 1}}}}
	b := root{L: []*leaf{{M: map[string]int{"k": 1}}, {M: map[string]int{"k": 2}}}}
	got := cloneFirstDiff(reflect.ValueOf(a), reflect.ValueOf(b), "G", map[[2]uintptr]bool{})
	if want := "(*G.L[1]).M[k]: 1 != 2"; got != want {
		t.Fatalf("diff = %q, want %q", got, want)
	}
	if got := cloneFirstDiff(reflect.ValueOf(a), reflect.ValueOf(a), "G", map[[2]uintptr]bool{}); got != "" {
		t.Fatalf("equal values diff = %q", got)
	}
}

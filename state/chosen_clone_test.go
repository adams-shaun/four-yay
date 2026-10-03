package state

import "testing"

// A cleared choice (a non-nil empty Chosen) and no choice at all (nil) are
// different states -- the ChosenCard condition gate reads the first as a
// known zero and leaves the second unresolved -- so CloneDeep must keep them
// apart, or a resolution re-executed from a clone gates differently from the
// live engine.
func TestCloneDeepKeepsClearedChosen(t *testing.T) {
	cleared := Object{ID: 7, Chosen: []Target{}}
	if c := cleared.CloneDeep(); c.Chosen == nil {
		t.Fatal("CloneDeep turned a cleared (non-nil empty) Chosen into nil")
	}
	none := Object{ID: 8}
	if c := none.CloneDeep(); c.Chosen != nil {
		t.Fatalf("CloneDeep invented a Chosen binding: %v", c.Chosen)
	}
	one := Object{ID: 9, Chosen: []Target{{Obj: 4}}}
	if c := one.CloneDeep(); len(c.Chosen) != 1 || c.Chosen[0].Obj != 4 {
		t.Fatalf("CloneDeep Chosen = %v, want [4]", c.Chosen)
	}
}

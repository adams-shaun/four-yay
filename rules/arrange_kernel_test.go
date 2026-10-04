package rules

import "testing"

// TestArrangePosesDecisionAndSuspends is the Ruling J0/J2 leaf, restored for
// the resolution kernel: resolving a RearrangeTopOfLibrary | NumCards$ 3
// poses a KArrange decision to the library owner with 3 options and
// Min == Max == 3 (a full permutation over the top N), with the spell still
// on the stack while it is posed.
func TestArrangePosesDecisionAndSuspends(t *testing.T) {
	t.Parallel()
	e, _, id := arrangeFixture(t, 100)
	d := arrangeDecision(t, e, id)
	if d.Min != 3 || d.Max != 3 {
		t.Fatalf("Min/Max = %d/%d, want 3/3 (a full reorder is a permutation over the top N)", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want 3", len(d.Options))
	}
	if d.Player != 0 {
		t.Fatalf("decision player = %d, want 0 (the library owner)", d.Player)
	}
	if len(e.G.Stack) == 0 || e.G.Stack[len(e.G.Stack)-1] != id {
		t.Fatalf("stack = %v, want the resolving spell %d still on it while the ask is posed", e.G.Stack, id)
	}
}

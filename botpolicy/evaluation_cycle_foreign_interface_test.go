package botpolicy

import "testing"

// A call through a foreign interface has no same-package callee object, but
// the selection still supplies an interface receiver and a method signature.
// The concrete implementation must remain reachable in the recursion graph.
func TestPolicyCallGraphCatchesForeignInterfaceDispatch(t *testing.T) {
	const src = `package probe
import "sort"
type T struct{}
func (T) Len() int { return 0 }
func (T) Less(i, j int) bool { score(); return i < j }
func (T) Swap(i, j int) {}
func score() { var s sort.Interface = T{}; s.Less(1, 2) }
`
	g := buildProbeGraph(t, src)
	if !g.edges["score"]["T.Less"] {
		t.Fatalf("probe precondition failed: score does not call the concrete implementation; edges=%v", g.edges)
	}
	if !g.edges["T.Less"]["score"] {
		t.Fatalf("probe precondition failed: implementation does not recurse to score; edges=%v", g.edges)
	}
	for _, scc := range g.sccs {
		foundScore, foundImpl := false, false
		for _, node := range scc {
			foundScore = foundScore || node == "score"
			foundImpl = foundImpl || node == "T.Less"
		}
		if foundScore && foundImpl {
			return
		}
	}
	t.Fatalf("foreign interface recursion not detected: sccs=%s self=%v edges=%v", formatSCCs(g.sccs), g.selfLoops, g.edges)
}

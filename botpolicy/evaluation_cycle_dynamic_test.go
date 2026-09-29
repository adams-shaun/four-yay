package botpolicy

import "testing"

// TestPolicyCallGraphCatchesDynamicEvaluatorRecursion pins the fail-closed arm
// for calls the syntactic and function-value walks cannot resolve to a single
// declaration: an indexed map/slice element, interface dispatch, and a
// factory-returned function. Each cyclic probe must yield an SCC (or self
// edge) containing score. There is deliberately no acyclic control here: the
// arm is a SOUND over-approximation, so every caller of a dynamic dispatch
// also gains an edge to itself when its own signature is assignable (that is
// exactly what lets the map-element probe detect a self-recursive target). A
// caller that is provably not a target still gets the self edge, so an
// "acyclic dynamic dispatch" control would assert something the design does
// not promise; the acyclic controls live on the function-value and indirect
// probes above, where the value flow is actually traceable.
//
// The interface shapes are keyed on the RESOLVED declaration object
// (an interface method), not the receiver expression, so a promoted/embedded
// interface method and a bound interface method value are caught too. That is
// what the three embedded/bound probes below name explicitly.
func TestPolicyCallGraphCatchesDynamicEvaluatorRecursion(t *testing.T) {
	probes := map[string]string{
		"map element": `package probe
func score() { m := map[string]func(){"score": score}; m["score"]() }
`,
		"slice element": `package probe
func score() { fns := []func(){score}; fns[0]() }
`,
		"interface dispatch": `package probe
type I interface{ run() }
type T struct{}
func (T) run() { score() }
func score() { var i I = T{}; i.run() }
`,
		"factory returned function": `package probe
func makeF() func() { return score }
func score() { f := makeF(); f() }
`,
		"embedded interface dispatch (value receiver expression)": `package probe
type I interface{ run() }
type T struct{}
func (T) run() { score() }
type S struct{ I }
func score() { var s S = S{I: T{}}; s.run() }
`,
		"embedded interface dispatch (pointer receiver expression)": `package probe
type I interface{ run() }
type T struct{}
func (T) run() { score() }
type S struct{ I }
func score() { var s *S = &S{I: T{}}; s.run() }
`,
		"bound interface method value called through a slot": `package probe
type I interface{ run() }
type T struct{}
func (T) run() { score() }
func score() { var i I = T{}; f := i.run; f() }
`,
	}
	for name, src := range probes {
		t.Run(name, func(t *testing.T) {
			g := buildProbeGraph(t, src)
			if len(g.edges) == 0 {
				t.Fatalf("probe precondition failed: no call edges: %v", g.edges)
			}
			for _, self := range g.selfLoops {
				if self == "score" {
					return
				}
			}
			for _, scc := range g.sccs {
				for _, node := range scc {
					if node == "score" {
						return
					}
				}
			}
			t.Fatalf("recursive dynamic invocation not detected: sccs=%s self=%v edges=%v", formatSCCs(g.sccs), g.selfLoops, g.edges)
		})
	}
}

package botpolicy

import "testing"

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

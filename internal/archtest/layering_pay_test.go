package archtest

import (
	"sort"
	"strings"
	"testing"
)

// layering_pay_test.go pins rules/pay, the L5 payment-planning package of the
// rules-engine lasagna (spec 2026-10-03 §3, W5 step E7): the planner's
// vocabulary (alternatives, consequences, rank, witness) and its bounded
// rank-aware search. It reaches the engine's mana solver only through the
// pay.Env hook package rules installs. Its rows live in their own file so
// parallel W5 steps do not edit the same lines.

// payImports is the closed set of module packages rules/pay may import
// directly: the L0/L1 vocabulary (cards, state, decision) and the L2 cost
// vocabulary. rules, effects, the sibling L5 packages, the resolution kernel
// and the bot layer would invert the layering.
var payImports = map[string]bool{
	module + "/cards":      true,
	module + "/state":      true,
	module + "/decision":   true,
	module + "/events":     true,
	module + "/rules/cost": true,
}

// TestPayImportsStayBelowRules pins rules/pay's direct module imports to
// payImports and forbids the transitive edges the layering rules out. It
// skips until the package exists.
func TestPayImportsStayBelowRules(t *testing.T) {
	const sub = module + "/rules/pay"
	pkgs := packages(t)
	p, ok := pkgs[sub]
	if !ok {
		t.Skip("rules/pay is not built yet")
	}
	var bad []string
	for imp := range p.imports {
		if strings.HasPrefix(imp, module+"/") && !payImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the payment layer may import only %v "+
			"(internal/archtest/layering_pay_test.go payImports)", sub, imp, sortedKeys(payImports))
	}
	for _, to := range []string{
		module + "/rules",
		module + "/effects",
		module + "/rules/combat",
		module + "/rules/trigmatch",
		module + "/rules/chars",
		module + "/rules/resolve",
		module + "/botpolicy",
	} {
		if p.deps[to] {
			t.Errorf("%s depends on %s (transitively); the dependency order forbids it", sub, to)
		}
	}
}

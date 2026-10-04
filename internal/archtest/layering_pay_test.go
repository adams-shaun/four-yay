package archtest

import (
	"sort"
	"strings"
	"testing"
)

// layering_pay_test.go pins rules/pay, the L5 payment-planning package of the
// rules-engine lasagna (spec 2026-10-03 §3, W5 step E7): the planner's
// vocabulary (alternatives, consequences, rank, witness) and its bounded
// rank-aware search, the pool solver and payment core, and the payment
// vocabulary (cost modifiers, window sources, planner classification and
// stats). It reaches the engine only through the pay.Engine interface package
// rules implements (rules/pay_engine.go). Its rows live in their own file so
// parallel W5 steps do not edit the same lines.

// payImports is the closed set of module packages rules/pay may import
// directly: the L0/L1 vocabulary (cards, state, decision), the L2 cost
// vocabulary and effects/params, the leaf compiled-parameter records (the
// mana production, targeting, Defined and DealDamage records the ring reads
// on nearly every path; paramsImports pins it below effects). rules, effects, the sibling L5 packages, the resolution kernel
// and the bot layer would invert the layering.
var payImports = map[string]bool{
	module + "/cards":          true,
	module + "/state":          true,
	module + "/decision":       true,
	module + "/events":         true,
	module + "/rules/cost":     true,
	module + "/effects/params": true,
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

// payForbiddenStd is the standard library rules/pay must not import: the
// payment layer is part of the deterministic replay core (no wall clock, no
// ambient randomness) and holds no shared mutable state of its own.
var payForbiddenStd = []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "sync", "unsafe", "os"}

// TestPayStaysDeterministic pins rules/pay off the standard-library packages
// that would let a payment depend on anything but the game it is handed.
func TestPayStaysDeterministic(t *testing.T) {
	const sub = module + "/rules/pay"
	p, ok := packages(t)[sub]
	if !ok {
		t.Skip("rules/pay is not built yet")
	}
	for _, imp := range payForbiddenStd {
		if p.imports[imp] {
			t.Errorf("%s imports %s; the payment layer is replay-deterministic and stateless "+
				"(internal/archtest/layering_pay_test.go payForbiddenStd)", sub, imp)
		}
	}
}

// paramsImports is the closed set of module packages effects/params may
// import directly: the L0/L1 vocabulary only. effects/params is the leaf
// home of the compiled ability-parameter records both the resolution
// (effects aliases every name) and the payment layer (rules/pay) read (W5
// step E7); anything above L1 would put the resolution's Host/Ctx machinery,
// or the engine, back under the payment layer.
var paramsImports = map[string]bool{
	module + "/cards":    true,
	module + "/state":    true,
	module + "/decision": true,
}

// TestParamsIsALeaf pins effects/params' direct module imports to
// paramsImports.
func TestParamsIsALeaf(t *testing.T) {
	const sub = module + "/effects/params"
	p, ok := packages(t)[sub]
	if !ok {
		t.Fatal("effects/params is not built")
	}
	var bad []string
	for imp := range p.imports {
		if strings.HasPrefix(imp, module+"/") && !paramsImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the compiled-parameter leaf may import only %v "+
			"(internal/archtest/layering_pay_test.go paramsImports)", sub, imp, sortedKeys(paramsImports))
	}
	for _, imp := range payForbiddenStd {
		if imp != "unsafe" && p.imports[imp] {
			t.Errorf("%s imports %s; the compiled-parameter leaf is replay-deterministic", sub, imp)
		}
	}
}

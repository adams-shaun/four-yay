package rules

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The Scam.EXE coverage guard (ticket E5). The primitive census once counted
// the deck "fully supported" while its Golgari Thug loop line placed the
// card on the wrong zone: an empty census does not prove a combo behaves.
// So the deck earns its "fully supported" verdict only when every
// loop-combos.json line is either executed and passing (this file runs the
// named prototypes as subtests of the ratchet, so the ratchet cannot go
// green while a line is broken) or explicitly listed with a reason in
// scamUnmeasuredLines. Both an unmeasured line without a reason and a stale
// reason for a deleted line fail the guard.
var scamUnmeasuredLines = map[string]string{
	"current-list-bounded-engines": "additional research candidates; not simulated or certified as executable prototypes",
}

// loopCombo is the slice of rules/testdata/loop-combos.json the guard reads.
type loopCombo struct {
	ID   string `json:"id"`
	Test string `json:"test"`
}

// loopGuardResult records how one catalog line was classified: a measured
// line carries the prototype's pass state, an unmeasured line carries its
// listed reason.
type loopGuardResult struct {
	passed bool
	reason string
}

// loadLoopCombos reads the real research catalog.
func loadLoopCombos(t *testing.T) []loopCombo {
	t.Helper()
	raw, err := os.ReadFile("testdata/loop-combos.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Combos []loopCombo `json:"combos"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Combos) == 0 {
		t.Fatal("loop-combos.json has no combos")
	}
	return catalog.Combos
}

// prototypeNameOf extracts the executable prototype a line names:
// "TestLoopPrototypeDualcaster/Molten_Duplication" ->
// "TestLoopPrototypeDualcaster"; a suffix in parentheses (the measured N
// note) is ignored.
func prototypeNameOf(line string) string {
	return strings.SplitN(strings.Fields(line)[0], "/", 2)[0]
}

// catalogNamesPrototype reports whether any line resolves to the named
// prototype (test-only helper for the demonstration below).
func catalogNamesPrototype(combos []loopCombo, name string) bool {
	for _, c := range combos {
		if !strings.HasPrefix(c.Test, "Not simulated:") && prototypeNameOf(c.Test) == name {
			return true
		}
	}
	return false
}

// evaluateLoopGuard walks the catalog against the executable checks and
// reports whether the Scam.EXE deck may count as fully supported. Every
// line naming a prototype runs that prototype as a subtest (one run shared
// by the lines that name it, its result propagated to each) and must pass;
// every line marked unmeasured must carry a reason in scamUnmeasuredLines.
func evaluateLoopGuard(t *testing.T, combos []loopCombo, checks map[string]func(*testing.T)) bool {
	t.Helper()
	measured := make(map[string]loopGuardResult, len(checks))
	proto := make(map[string]loopGuardResult, len(checks))
	supported := true
	for _, c := range combos {
		if strings.HasPrefix(c.Test, "Not simulated:") {
			reason, listed := scamUnmeasuredLines[c.ID]
			if !listed || reason == "" {
				t.Errorf("loop %s is unmeasured and has no reason in scamUnmeasuredLines", c.ID)
				supported = false
				continue
			}
			measured[c.ID] = loopGuardResult{reason: reason}
			continue
		}
		name := prototypeNameOf(c.Test)
		check, ok := checks[name]
		if !ok {
			t.Errorf("loop %s names prototype %q with no executable check", c.ID, c.Test)
			supported = false
			continue
		}
		res, ran := proto[name]
		if !ran {
			res.passed = t.Run("loop prototype/"+name, check)
			proto[name] = res
		}
		measured[c.ID] = res
		if !res.passed {
			supported = false
		}
	}
	for id := range scamUnmeasuredLines {
		stale := true
		for _, c := range combos {
			if c.ID == id {
				stale = false
				break
			}
		}
		if stale {
			t.Errorf("stale unmeasured-line reason for %s: the combo no longer exists in loop-combos.json", id)
			supported = false
		}
	}
	return supported
}

// scamLoopChecks maps the prototype names the catalog references to the
// real prototype tests in loops_prototype_test.go. A renamed or removed
// prototype is caught by evaluateLoopGuard's missing-check error, not
// silently dropped.
func scamLoopChecks() map[string]func(*testing.T) {
	return map[string]func(*testing.T){
		"TestLoopPrototypeMiner":      TestLoopPrototypeMiner,
		"TestLoopPrototypeSephiroth":  TestLoopPrototypeSephiroth,
		"TestLoopPrototypeSoultrader": TestLoopPrototypeSoultrader,
		"TestLoopPrototypeThug":       TestLoopPrototypeThug,
		"TestLoopPrototypeDualcaster": TestLoopPrototypeDualcaster,
		"TestLoopPrototypeMonk":       TestLoopPrototypeMonk,
		"TestLoopPrototypeNightmare":  TestLoopPrototypeNightmare,
		"TestLoopPrototypeBreach":     TestLoopPrototypeBreach,
	}
}

// runScamLoopCoverageGuard is the hook the deck ratchet calls: it walks the
// real catalog with the real prototypes.
func runScamLoopCoverageGuard(t *testing.T) {
	t.Helper()
	evaluateLoopGuard(t, loadLoopCombos(t), scamLoopChecks())
}

// TestLoopGuardFailingPrototypeKeepsScamOutOfFullSupport demonstrates the
// guard with test-only stub prototypes standing in for the real ones, so a
// failing line is observable without rerunning the engine lines: on the
// real catalog, every stub passing counts the deck fully supported, and one
// stub failing (the historical Thug false positive, simulated) keeps it out
// despite the empty census. Preconditions assert the stubs actually cover
// what the real catalog references, so a catalog that drifts fails loudly.
func TestLoopGuardFailingPrototypeKeepsScamOutOfFullSupport(t *testing.T) {
	combos := loadLoopCombos(t)
	names := make([]string, 0, len(scamLoopChecks()))
	for name := range scamLoopChecks() {
		names = append(names, name)
		if !catalogNamesPrototype(combos, name) {
			t.Errorf("precondition: real catalog no longer references prototype %s", name)
		}
	}
	if !catalogNamesPrototype(combos, "TestLoopPrototypeThug") {
		t.Fatal("precondition: the catalog does not name the Thug prototype the guard demo needs")
	}
	stub := func(name string, pass bool) func(*testing.T) {
		return func(t *testing.T) {
			if !pass {
				t.Fatalf("stub prototype %s failed (test-only fixture)", name)
			}
		}
	}
	t.Run("all prototypes pass -> deck counts as fully supported", func(t *testing.T) {
		passing := make(map[string]func(*testing.T), len(names))
		for _, name := range names {
			passing[name] = stub(name, true)
		}
		if !evaluateLoopGuard(t, combos, passing) {
			t.Fatal("guard rejected a fully passing prototype set")
		}
	})
	t.Run("a failing prototype keeps the deck out of full support", func(t *testing.T) {
		withFailure := make(map[string]func(*testing.T), len(names))
		for _, name := range names {
			pass := name != "TestLoopPrototypeThug"
			withFailure[name] = stub(name, pass)
		}
		if evaluateLoopGuard(t, combos, withFailure) {
			t.Fatal("guard counted the Scam.EXE deck fully supported while the Thug prototype failed -- the historical false positive is back")
		}
	})
	t.Run("an unmeasured line without a reason keeps the deck out of full support", func(t *testing.T) {
		checks := make(map[string]func(*testing.T), len(names))
		for _, name := range names {
			checks[name] = stub(name, true)
		}
		combos := append([]loopCombo(nil), combos...)
		combos = append(combos, loopCombo{ID: "test-only-unmeasured", Test: "Not simulated: test-only fixture"})
		if evaluateLoopGuard(t, combos, checks) {
			t.Fatal("guard counted an unmeasured line without a listed reason as supported")
		}
	})
}

package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
//
// The verdict is a pure function (loopGuardVerdict) so the negative cases
// can be demonstrated with injected outcomes, without a deliberately failing
// subtest: a failed subtest fails its parent, which would make the
// demonstration itself red and unusable as a regression gate.
var scamUnmeasuredLines = map[string]string{
	"current-list-bounded-engines": "additional research candidates; not simulated or certified as executable prototypes",
}

// loopCombo is the slice of rules/testdata/loop-combos.json the guard reads.
type loopCombo struct {
	ID   string `json:"id"`
	Test string `json:"test"`
}

// loopOutcome is how one prototype's guard run ended. A blocked prototype is
// distinct from a failed one only so the message can say which; both keep
// the deck out of full support. "blocked" is the loopBlocked path: the
// research script could not complete the line, so it never asserted the
// behaviour the census claims is supported.
type loopOutcome string

const (
	loopOutcomePassed  loopOutcome = "passed"
	loopOutcomeFailed  loopOutcome = "failed"
	loopOutcomeBlocked loopOutcome = "blocked"
)

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

// loopGuardVerdict walks the catalog against the executable checks and the
// recorded prototype outcomes and returns one message per reason the deck
// may NOT count as fully supported. It is pure: problems are returned, not
// reported through *testing.T, so a caller can demonstrate the negative
// cases without failing its own test. An empty result means fully supported.
//
// Every line naming a prototype must resolve to an executable check and that
// check's outcome must be loopOutcomePassed; every line marked unmeasured
// must carry a reason in scamUnmeasuredLines; and every listed reason must
// still name a line in the catalog.
func loopGuardVerdict(combos []loopCombo, checks map[string]func(*testing.T), outcomes map[string]loopOutcome) []string {
	var problems []string
	seen := make(map[string]bool, len(combos))
	for _, c := range combos {
		seen[c.ID] = true
		if strings.HasPrefix(c.Test, "Not simulated:") {
			reason, listed := scamUnmeasuredLines[c.ID]
			if !listed || reason == "" {
				problems = append(problems, fmt.Sprintf("loop %s is unmeasured and has no reason in scamUnmeasuredLines", c.ID))
			}
			continue
		}
		name := prototypeNameOf(c.Test)
		if _, ok := checks[name]; !ok {
			problems = append(problems, fmt.Sprintf("loop %s names prototype %q with no executable check", c.ID, c.Test))
			continue
		}
		outcome, ran := outcomes[name]
		if !ran {
			problems = append(problems, fmt.Sprintf("loop %s names prototype %q that the guard did not run", c.ID, name))
			continue
		}
		if outcome != loopOutcomePassed {
			problems = append(problems, fmt.Sprintf("loop %s prototype %s is %s, not passing", c.ID, name, outcome))
		}
	}
	listed := make([]string, 0, len(scamUnmeasuredLines))
	for id := range scamUnmeasuredLines {
		listed = append(listed, id)
	}
	sort.Strings(listed)
	for _, id := range listed {
		if !seen[id] {
			problems = append(problems, fmt.Sprintf("stale unmeasured-line reason for %s: the combo no longer exists in loop-combos.json", id))
		}
	}
	return problems
}

// executeLoopPrototypes runs every catalog-named prototype once, in strict
// mode, and records its outcome. Running in strict mode is what makes a
// blocked prototype fail rather than log: loopBlocked observes
// loopGuardStrict, so a prototype that reaches it fails its subtest and the
// deck can no longer be counted as fully supported. The run is shared by all
// the lines that name the prototype.
func executeLoopPrototypes(t *testing.T, combos []loopCombo, checks map[string]func(*testing.T)) map[string]loopOutcome {
	t.Helper()
	nameSet := make(map[string]bool)
	for _, c := range combos {
		if strings.HasPrefix(c.Test, "Not simulated:") {
			continue
		}
		nameSet[prototypeNameOf(c.Test)] = true
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		if _, ok := checks[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	prevStrict := loopGuardStrict
	loopGuardStrict = true
	defer func() { loopGuardStrict = prevStrict }()
	outcomes := make(map[string]loopOutcome, len(names))
	for _, name := range names {
		check := checks[name]
		loopGuardBlocked = false
		pass := t.Run("loop prototype/"+name, check)
		switch {
		case loopGuardBlocked:
			outcomes[name] = loopOutcomeBlocked
		case pass:
			outcomes[name] = loopOutcomePassed
		default:
			outcomes[name] = loopOutcomeFailed
		}
	}
	return outcomes
}

// evaluateLoopGuard is the guard as the ratchet uses it: run the prototypes,
// take the pure verdict, report each problem through t, and return whether
// the deck may count as fully supported.
func evaluateLoopGuard(t *testing.T, combos []loopCombo, checks map[string]func(*testing.T)) bool {
	t.Helper()
	problems := loopGuardVerdict(combos, checks, executeLoopPrototypes(t, combos, checks))
	for _, p := range problems {
		t.Error(p)
	}
	return len(problems) == 0
}

// scamLoopChecks maps the prototype names the catalog references to the
// real prototype tests in loops_prototype_test.go. A renamed or removed
// prototype is caught by loopGuardVerdict's missing-check error, not
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
// guard's two-directions contract with injected outcomes, so every negative
// case is observable without failing this test (a real failing subtest would
// fail its parent). Preconditions assert the stubs cover what the real
// catalog references, so a catalog that drifts fails loudly.
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
	checks := make(map[string]func(*testing.T), len(names))
	for _, name := range names {
		checks[name] = func(*testing.T) {}
	}
	allOutcomes := func(overrides map[string]loopOutcome) map[string]loopOutcome {
		out := make(map[string]loopOutcome, len(names))
		for _, name := range names {
			out[name] = loopOutcomePassed
		}
		for name, o := range overrides {
			out[name] = o
		}
		return out
	}
	mentions := func(problems []string, want string) bool {
		for _, p := range problems {
			if strings.Contains(p, want) {
				return true
			}
		}
		return false
	}

	t.Run("all prototypes pass -> deck counts as fully supported", func(t *testing.T) {
		if problems := loopGuardVerdict(combos, checks, allOutcomes(nil)); len(problems) != 0 {
			t.Fatalf("guard rejected a fully passing prototype set: %v", problems)
		}
	})
	t.Run("a failing prototype keeps the deck out of full support", func(t *testing.T) {
		problems := loopGuardVerdict(combos, checks, allOutcomes(map[string]loopOutcome{"TestLoopPrototypeThug": loopOutcomeFailed}))
		if len(problems) == 0 || !mentions(problems, "TestLoopPrototypeThug") {
			t.Fatalf("guard counted the Scam.EXE deck fully supported while the Thug prototype failed -- the historical false positive is back: %v", problems)
		}
	})
	t.Run("a blocked prototype keeps the deck out of full support", func(t *testing.T) {
		// A blocked prototype is one that reached loopBlocked: the research
		// script could not complete the line, so it never asserted the
		// behaviour the census claims is supported. Even with the census
		// empty, a blocked line must keep the deck out of full support.
		problems := loopGuardVerdict(combos, checks, allOutcomes(map[string]loopOutcome{"TestLoopPrototypeThug": loopOutcomeBlocked}))
		if len(problems) == 0 || !mentions(problems, "blocked") {
			t.Fatalf("guard counted a blocked prototype as supported -- a blocked line must keep the deck out of full support: %v", problems)
		}
	})
	t.Run("a prototype the guard did not run keeps the deck out of full support", func(t *testing.T) {
		out := allOutcomes(nil)
		delete(out, "TestLoopPrototypeThug")
		problems := loopGuardVerdict(combos, checks, out)
		if len(problems) == 0 || !mentions(problems, "did not run") {
			t.Fatalf("guard counted an unrun prototype as supported: %v", problems)
		}
	})
	t.Run("an unmeasured line without a reason keeps the deck out of full support", func(t *testing.T) {
		combos := append([]loopCombo(nil), combos...)
		combos = append(combos, loopCombo{ID: "test-only-unmeasured", Test: "Not simulated: test-only fixture"})
		problems := loopGuardVerdict(combos, checks, allOutcomes(nil))
		if len(problems) == 0 || !mentions(problems, "test-only-unmeasured") {
			t.Fatalf("guard counted an unmeasured line without a listed reason as supported: %v", problems)
		}
	})
	t.Run("an unmeasured line with a listed reason keeps the deck supported", func(t *testing.T) {
		// The other direction of the contract: a line explicitly reasoned
		// about is allowed to keep the deck in full support.
		combos := append([]loopCombo(nil), combos...)
		id := "test-only-unmeasured-with-reason"
		scamUnmeasuredLines[id] = "test-only fixture reason"
		t.Cleanup(func() { delete(scamUnmeasuredLines, id) })
		combos = append(combos, loopCombo{ID: id, Test: "Not simulated: test-only fixture"})
		if problems := loopGuardVerdict(combos, checks, allOutcomes(nil)); len(problems) != 0 {
			t.Fatalf("guard rejected a reasoned unmeasured line: %v", problems)
		}
	})
	t.Run("the strict mode the guard relies on is what makes a blocked line fail", func(t *testing.T) {
		// executeLoopPrototypes sets loopGuardStrict; loopBlocked must take
		// its failure branch under it, or a blocked prototype would return
		// success and the guard would count it as passing.
		prev := loopGuardStrict
		loopGuardStrict = true
		defer func() { loopGuardStrict = prev }()
		if !loopBlockedFails() {
			t.Fatal("loopBlockedFails is false while loopGuardStrict is set: a blocked prototype would be counted as passing")
		}
	})
	t.Run("executeLoopPrototypes runs the real prototypes under strict mode", func(t *testing.T) {
		// The chain from a blocked prototype to a rejected deck depends on
		// executeLoopPrototypes raising loopGuardStrict for the run. A stub
		// that observes it proves the wiring without a real failing subtest.
		saw := false
		stub := func(t *testing.T) {
			if !loopBlockedFails() {
				t.Error("prototype ran without loopGuardStrict: a blocked line would be counted as passing")
			}
			saw = true
		}
		checks := map[string]func(*testing.T){"TestLoopPrototypeThug": stub}
		combos := []loopCombo{{ID: "test-only", Test: "TestLoopPrototypeThug"}}
		outcomes := executeLoopPrototypes(t, combos, checks)
		if !saw {
			t.Fatal("precondition: executeLoopPrototypes did not run the stub")
		}
		if outcomes["TestLoopPrototypeThug"] != loopOutcomePassed {
			t.Fatalf("stub outcome = %q, want passed", outcomes["TestLoopPrototypeThug"])
		}
		if loopGuardStrict {
			t.Fatal("executeLoopPrototypes left loopGuardStrict set after it returned")
		}
	})
}

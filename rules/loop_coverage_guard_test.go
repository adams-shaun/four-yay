package rules

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Prototype tests make the primitive census meaningful for the Scam.EXE loop
// lines: a registered primitive alone cannot certify the line's behavior.
// The one research-only candidate without a prototype is explicitly tracked
// here rather than silently treated as measured.
var scamLoopUnmeasured = map[string]string{
	"current-list-bounded-engines": "Additional research candidates; not simulated or certified as executable prototypes.",
}

type loopPrototypeResult struct {
	passed bool
	reason string
}

func loopCoverageSupported(censusEmpty bool, combos []string, results map[string]loopPrototypeResult) bool {
	if !censusEmpty {
		return false
	}
	for _, id := range combos {
		result, ok := results[id]
		if !ok || (!result.passed && result.reason == "") {
			return false
		}
	}
	return true
}

// Run every named loop prototype directly under the deck ratchet. This means
// the ratchet cannot pass on a clean primitive census if a prototype fails;
// Go's normal test runner also runs these tests independently by name.
func runScamLoopCoverageGuard(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("testdata/loop-combos.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Combos []struct {
			ID   string `json:"id"`
			Test string `json:"test"`
		} `json:"combos"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func(*testing.T){
		"TestLoopPrototypeMiner":      TestLoopPrototypeMiner,
		"TestLoopPrototypeSephiroth":  TestLoopPrototypeSephiroth,
		"TestLoopPrototypeSoultrader": TestLoopPrototypeSoultrader,
		"TestLoopPrototypeThug":       TestLoopPrototypeThug,
		"TestLoopPrototypeDualcaster": TestLoopPrototypeDualcaster,
		"TestLoopPrototypeMonk":       TestLoopPrototypeMonk,
		"TestLoopPrototypeNightmare":  TestLoopPrototypeNightmare,
		"TestLoopPrototypeBreach":     TestLoopPrototypeBreach,
	}
	results := make(map[string]loopPrototypeResult, len(catalog.Combos))
	var comboIDs []string
	seenTests := map[string]bool{}
	for _, combo := range catalog.Combos {
		comboIDs = append(comboIDs, combo.ID)
		if strings.HasPrefix(combo.Test, "Not simulated:") {
			reason, listed := scamLoopUnmeasured[combo.ID]
			if !listed || reason == "" {
				t.Errorf("loop %s is unmeasured and has no reason in scamLoopUnmeasured", combo.ID)
				results[combo.ID] = loopPrototypeResult{}
			} else {
				results[combo.ID] = loopPrototypeResult{reason: reason}
			}
			continue
		}
		name := strings.Fields(combo.Test)[0]
		name = strings.SplitN(name, "/", 2)[0]
		check, ok := checks[name]
		if !ok {
			t.Errorf("loop %s names prototype %q without an executable check", combo.ID, combo.Test)
			results[combo.ID] = loopPrototypeResult{}
			continue
		}
		if seenTests[name] {
			results[combo.ID] = loopPrototypeResult{passed: true}
			continue
		}
		seenTests[name] = true
		passed := t.Run("loop prototype/"+combo.ID, check)
		results[combo.ID] = loopPrototypeResult{passed: passed}
	}
	for id := range scamLoopUnmeasured {
		found := false
		for _, comboID := range comboIDs {
			if comboID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("stale unmeasured loop reason for %s", id)
		}
	}
	if !loopCoverageSupported(true, comboIDs, results) {
		t.Error("Scam.EXE loop lines are not fully supported despite an empty primitive census")
	}
}

func TestLoopPrototypeCoverageGuardRejectsFailedPrototypeWithEmptyCensus(t *testing.T) {
	if loopCoverageSupported(true, []string{"rakdos-thug-ashnod"}, map[string]loopPrototypeResult{
		"rakdos-thug-ashnod": {passed: false},
	}) {
		t.Fatal("failed loop prototype was counted fully supported with an empty census")
	}
}

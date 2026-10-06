package oraclegen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

const scenarioReplayPath = "../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java"

// TestDriverActivateCasePinsXMageAbility couples the level-B activate
// scenarios to the Java driver: the step switch must have an activate case
// that reads the item's xmage_ability and uses XMage's activate API.
func TestDriverActivateCasePinsXMageAbility(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	for _, required := range []string{
		`case "activate":`,
		`"xmage_ability"`,
		"activateAbility(TURN, phase, p, text)",
		"activateManaAbility(TURN, phase, p, text)",
		"step(st, op, i);",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java lacks the activate contract %q", required)
		}
	}
}

// TestDriverStepSwitchCoversLevelBActivateOps builds the activate items for
// the L5 test cards and checks the driver handles every op they use, and that
// xmage_ability is non-empty exactly on the activate steps.
func TestDriverStepSwitchCoversLevelBActivateOps(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	i := strings.Index(java, "private void step(JsonObject st, String op, int stepIdx)")
	if i < 0 {
		t.Fatal("step switch not found in ScenarioReplay.java")
	}
	stepSrc := java[i:]
	cased := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z_]+)":`).FindAllStringSubmatch(stepSrc, -1) {
		cased[m[1]] = true
	}

	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the generator needs the corpus: %v", err)
	}
	cases := []struct{ card, key string }{
		{"Druid of the Cowl", "activate#0.0"},
		{"Axgard Cavalry", "activate#0.0"},
		{"Basilisk Collar", "activate#0.0"},
	}
	sawActivate := false
	for _, tc := range cases {
		c, ok := reg.Lookup(tc.card)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", tc.card)
		}
		var req *levelb.Requirement
		for _, r := range levelb.Requirements(c) {
			if r.Key == tc.key {
				r := r
				req = &r
			}
		}
		if req == nil {
			t.Fatalf("precondition: %s has no requirement %s", tc.card, tc.key)
		}
		it, skip := templates.GenerateB(reg, tc.card, *req)
		if skip != nil {
			t.Fatalf("%s %s: %s", tc.card, tc.key, skip.Reason)
		}
		if len(it.XAbility) != len(it.Scenario.Steps) {
			t.Fatalf("%s: xmage_ability has %d entries for %d steps", tc.card, len(it.XAbility), len(it.Scenario.Steps))
		}
		for n, st := range it.Scenario.Steps {
			if !cased[st.Op] {
				t.Errorf("%s step %d: op %q has no case in the driver's step switch", tc.card, n, st.Op)
			}
			if st.Op == "activate" {
				sawActivate = true
			}
			if (it.XAbility[n] != "") != (st.Op == "activate") {
				t.Errorf("%s step %d (%s): xmage_ability = %q, want non-empty exactly on activate", tc.card, n, st.Op, it.XAbility[n])
			}
		}
	}
	if !sawActivate {
		t.Fatal("no activate step generated; the contract test is vacuous")
	}
}

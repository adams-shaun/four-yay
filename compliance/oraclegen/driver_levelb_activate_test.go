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
	// Precondition: the step signature carries the step index the activate
	// case reads its xmage_ability entry by.
	if !strings.Contains(java, "step(st, op, i);") {
		t.Fatalf("ScenarioReplay.java no longer passes the step index: missing %q", "step(st, op, i);")
	}
	body, ok := caseBody(java, "activate")
	if !ok {
		t.Fatalf("ScenarioReplay.java has no `case \"activate\":` in the step switch")
	}
	// The file must read the item's xmage_ability, and the activate case
	// itself must resolve its step's entry and call both API forms, not
	// merely mention them somewhere in the file.
	if !strings.Contains(java, `"xmage_ability"`) {
		t.Errorf("ScenarioReplay.java does not read the item's xmage_ability")
	}
	for _, required := range []string{
		"xabilityAt(stepIdx)",
		"activateAbility(TURN, phase, p, text)",
		"activateManaAbility(TURN, phase, p, text)",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("the activate case does not %q", required)
		}
	}
}

// caseBody returns the source of a step-switch `case "name":` from its
// label through the matching closing brace. It scans brace depth so the
// case's own blocks (an if, a nested lambda) are included and the next case
// is not. ok is false when the label is absent.
func caseBody(java, name string) (string, bool) {
	label := `case "` + name + `":`
	i := strings.Index(java, label)
	if i < 0 {
		return "", false
	}
	rest := java[i:]
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return rest, true
	}
	depth := 0
	for j := open; j < len(rest); j++ {
		switch rest[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:j+1], true
			}
		}
	}
	return rest, true
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

	reg, err := cards.SharedCorpus(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatalf("the generator needs the corpus: %v", err)
	}
	// Every card the L5 activate tests exercise, so the driver contract
	// covers each shape: a true mana ability, a {T} ability with a target,
	// a keyword-expanded Equip, a plain mana cost with a self-sacrifice, a
	// planeswalker's +/-N loyalty, and a loyalty-cost MANA-API ability that
	// is not a mana ability (Chandra, CR 605.1b).
	cases := []struct{ card, key string }{
		{"Druid of the Cowl", "activate#0.0"},
		{"Axgard Cavalry", "activate#0.0"},
		{"Basilisk Collar", "activate#0.0"},
		{"Ajani, Caller of the Pride", "activate#0.0"},
		{"Ajani, Caller of the Pride", "activate#0.1"},
		{"Cathar Commando", "activate#0.0"},
		{"Chandra, Flameshaper", "activate#0.0"},
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
				// XMage at XMAGE_REF renders this as "{1}, Sacrifice {this}":
				// SacrificeSourceCost.getText() is "sacrifice {this}", but
				// CostsImpl.getText() upper-cases each cost's first letter,
				// and TestPlayer matches ability.toString() with a
				// case-sensitive startsWith. Mage.Tests spells it the same way
				// (GrinningTotemTest, AngelOfJubilationTest: "Sacrifice {this}").
				if tc.card == "Cathar Commando" && it.XAbility[n] != "{1}, Sacrifice {this}" {
					t.Errorf("Cathar Commando self-sacrifice prefix = %q, want XMage's \"{1}, Sacrifice {this}\"", it.XAbility[n])
				}
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

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

func TestDriverCombatUsesCurrentTurnAndStepSeat(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	attack, ok := caseBody(java, "attack")
	if !ok {
		t.Fatal("ScenarioReplay.java has no attack case")
	}
	block, ok := caseBody(java, "block")
	if !ok {
		t.Fatal("ScenarioReplay.java has no block case")
	}
	if strings.Contains(attack, "attack(TURN,") || !strings.Contains(attack, "attack(turn, p,") {
		t.Errorf("attack must use the current turn and step seat, got:\n%s", attack)
	}
	if strings.Contains(block, "block(TURN,") || !strings.Contains(block, "block(turn, p,") {
		t.Errorf("block must use the current turn and step seat, got:\n%s", block)
	}
	if !strings.Contains(attack, "if (seatIdx != activeSeat)") ||
		!strings.Contains(attack, "turn++;") || !strings.Contains(attack, "activeSeat = seatIdx;") {
		t.Error("an attack by a non-active seat must advance the turn and active seat first")
	}
	passTo, ok := caseBody(java, "pass_to")
	if !ok || !strings.Contains(passTo, "attackAdvancedTurn && nextActiveSeat == activeSeat") {
		t.Error("pass_to must preserve the turn already advanced by an off-turn attack")
	}
	if !strings.Contains(java, "runCode(cp, turn, phase, playerA") {
		t.Error("step checkpoints must use the current turn")
	}
}

// TestDriverStepSwitchCoversLevelBCombatOps couples the actual scenarios
// emitted by GenerateB to the XMage driver's op and checkpoint mappings.
func TestDriverStepSwitchCoversLevelBCombatOps(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	stepAt := strings.Index(java, "private void step(JsonObject st, String op, int stepIdx)")
	if stepAt < 0 {
		t.Fatal("step switch not found in ScenarioReplay.java")
	}
	stepSrc := java[stepAt:]
	cases := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z_]+)":`).FindAllStringSubmatch(stepSrc, -1) {
		cases[m[1]] = true
	}
	passTo, ok := caseBody(java, "pass_to")
	if !ok {
		t.Fatal("ScenarioReplay.java has no pass_to case")
	}
	for step, condition := range map[string]string{
		"main2": `stepName.equals("main2")`,
	} {
		if !strings.Contains(passTo, condition) {
			t.Errorf("combat checkpoint step %q is not handled by pass_to", step)
		}
	}
	if !strings.Contains(passTo, `active.equals("p1")`) {
		t.Error(`combat active seat "p1" is not handled by pass_to`)
	}

	reg, err := cards.SharedCorpus(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatalf("combat item generation needs the corpus: %v", err)
	}
	// These are the L13 combat fixtures: a keyword attacker (attack row) and
	// a defender (block row). GenerateB is authoritative for their emitted ops.
	fixtures := []struct{ card, key string }{
		{"Vampire Nighthawk", "combat#0.attack"},
		{"Wall of Omens", "combat#0.block"},
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		c, exists := reg.Lookup(fixture.card)
		if !exists {
			t.Fatalf("precondition: combat fixture %q is absent from corpus", fixture.card)
		}
		var req *levelb.Requirement
		for _, r := range levelb.Requirements(c) {
			if r.Key == fixture.key {
				r := r
				req = &r
				break
			}
		}
		if req == nil {
			t.Fatalf("precondition: %s has no %s requirement", fixture.card, fixture.key)
		}
		item, skip := templates.GenerateB(reg, fixture.card, *req)
		if skip != nil {
			t.Fatalf("GenerateB(%s, %s) skipped: %s", fixture.card, fixture.key, skip.Reason)
		}
		for _, st := range item.Scenario.Steps {
			if !cases[st.Op] {
				t.Errorf("%s %s emits unsupported driver op %q", fixture.card, fixture.key, st.Op)
			}
			seen[st.Op] = true
			if st.Op == "pass_to" && st.Step != "main2" {
				t.Errorf("%s %s emits unexpected pass_to step %q", fixture.card, fixture.key, st.Step)
			}
			if st.Op == "pass_to" && fixture.key == "combat#0.block" && st.Active != "p1" {
				t.Errorf("%s block item active = %q, want p1", fixture.card, st.Active)
			}
		}
	}
	for _, op := range []string{"attack", "block", "pass_to"} {
		if !seen[op] {
			t.Errorf("combat GenerateB fixtures did not exercise %q", op)
		}
	}
}

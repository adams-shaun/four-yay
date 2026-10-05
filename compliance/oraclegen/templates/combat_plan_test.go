package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// These cases guard the structural combat-plan shapes found during review:
// controller-constrained blockers and spells with multiple combat targets.
func TestControllerConstrainedBlockerIsExplicitlyUnsupported(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	_, skip := Generate(reg, "Aang's Defense")
	if skip == nil || !strings.Contains(skip.Reason, "no fixture gorge can cast") {
		t.Fatalf("YouCtrl blocking shape must be explicitly unsupported, got skip=%+v", skip)
	}
}

func TestOpponentControlledCombatAlternativeUsesBlocker(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Spirit Flare")
	if skip != nil {
		t.Fatalf("no generated scenario: %s", skip.Reason)
	}
	block := stepFor(it.Scenario, "block")
	if block == nil || len(block.Blocks) == 0 || block.Blocks[0][0][:3] != "p1:" {
		t.Fatalf("opponent-controlled blocking alternative was not arranged: %+v", it.Scenario.Steps)
	}
	if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
		t.Fatalf("gorge cannot play opponent-controlled combat alternative: %+v", it.Scenario.Steps)
	}
}

func TestMultipleAttackingTargetsAreAllDeclared(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Command of Unsummoning")
	if skip != nil {
		t.Fatalf("no generated scenario: %s", skip.Reason)
	}
	attack := stepFor(it.Scenario, "attack")
	cast := stepFor(it.Scenario, "cast")
	if attack == nil || cast == nil || len(cast.Targets) < 2 {
		t.Fatalf("scenario did not generate multiple attacking targets: %+v", it.Scenario.Steps)
	}
	declared := map[string]bool{}
	for _, ref := range attack.Attackers {
		if !seatPlacement(it.Scenario, ref) {
			t.Fatalf("attacker %q is not on the battlefield", ref)
		}
		declared[ref] = true
	}
	for _, target := range cast.Targets {
		if !declared[target] {
			t.Errorf("target %q is not declared attacking (attackers %v)", target, attack.Attackers)
		}
	}
	if len(declared) < 2 {
		t.Fatalf("only %d attacking participant(s): %v", len(declared), attack.Attackers)
	}
	if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
		t.Fatalf("gorge cannot play multi-target combat scenario: %+v", it.Scenario.Steps)
	}
}

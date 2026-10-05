package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// These are the std3 combat-trick cards: their only target slot demands a
// creature that is attacking/blocking, so the generated scenario must carry
// a creature into combat before the cast. Before the fix the fixture put a
// creature on the battlefield but never declared it attacking, so gorge
// refused the target and every one of these cards was a
// "no generated scenario (no fixture gorge can cast ...)" skip.
var attackingTargetCards = []string{
	"Protective Response",  // ECL
	"Focus Fire",           // EOE
	"Elspeth's Smite",      // FDN
	"Cosmium Blast",        // LCI
	"Sudden Strike",        // SPM
	"Osseous Exhale",       // TDM
	"Razor Rings",          // TLA
	"Sonar Strike",         // BLB
	"Dreadmaw's Ire",       // LCI
	"Not on My Watch",      // MKM
	"Kellan's Lightblades", // WOE
}

// These demand a tapped creature; the fixture must start the target tapped and
// must not declare combat. Before the fix the target was placed untapped, so
// the card was a skip.
var tappedTargetCards = []string{
	"Deadly Riposte",    // FDN
	"Eriette's Lullaby", // OTJ
	"Push // Pull",      // MKM
}

// stepFor returns the first step with op, or nil.
func stepFor(sc oraclegen.Scenario, op string) *oraclegen.Step {
	for i := range sc.Steps {
		if sc.Steps[i].Op == op {
			return &sc.Steps[i]
		}
	}
	return nil
}

// seatPlacement reports whether ref ("pN:Name") names a card in that seat's
// named setup zone lists. It is the precondition each combat assertion
// depends on: the test must prove the creature really is on the battlefield,
// not merely that a ref string was written.
func seatPlacement(sc oraclegen.Scenario, ref string) bool {
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	for _, seatKey := range []string{"p0", "p1"} {
		s := sc.Setup[seatKey]
		named := make([]string, 0, len(s.Battlefield)+len(s.Graveyard)+len(s.Hand))
		named = append(named, s.Battlefield...)
		named = append(named, s.Graveyard...)
		named = append(named, s.Hand...)
		for _, card := range named {
			if seatKey+":"+card == ref {
				return true
			}
		}
	}
	return false
}

// TestAttackingTargetScenarioIsCastable proves the attacking-trick shape: the
// generated scenario declares an attacker, the cast targets that attacker, and
// gorge can play the whole thing.
func TestAttackingTargetScenarioIsCastable(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, name := range attackingTargetCards {
		t.Run(name, func(t *testing.T) {
			it, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("no generated scenario: %s", skip.Reason)
			}
			attack := stepFor(it.Scenario, "attack")
			// Precondition the cast assertion depends on: a real attack step
			// declaring a creature, not an empty op.
			if attack == nil || len(attack.Attackers) != 1 {
				t.Fatalf("scenario has no single-attacker attack step: %+v", it.Scenario.Steps)
			}
			attacker := attack.Attackers[0]
			if !seatPlacement(it.Scenario, attacker) {
				t.Fatalf("attacker %q is not on a named battlefield (vacuous setup)", attacker)
			}
			cast := stepFor(it.Scenario, "cast")
			if cast == nil || len(cast.Targets) == 0 {
				t.Fatalf("cast step has no target: %+v", it.Scenario.Steps)
			}
			if cast.Targets[0] != attacker {
				t.Fatalf("cast target %q is not the declared attacker %q", cast.Targets[0], attacker)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("gorge cannot play the attacking-target scenario: %+v", it.Scenario.Steps)
			}
		})
	}
}

// TestTappedTargetScenarioIsCastable proves the tapped-trick shape: the target
// starts tapped and the scenario needs no combat. It also pins that the tap
// list carries the target exactly once -- before the per-candidate tap the
// coarse handler duplicated it.
func TestTappedTargetScenarioIsCastable(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, name := range tappedTargetCards {
		t.Run(name, func(t *testing.T) {
			it, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("no generated scenario: %s", skip.Reason)
			}
			cast := stepFor(it.Scenario, "cast")
			if cast == nil || len(cast.Targets) == 0 {
				t.Fatalf("cast step has no target: %+v", it.Scenario.Steps)
			}
			target := cast.Targets[0]
			if stepFor(it.Scenario, "attack") != nil {
				t.Fatalf("a tapped target needs no combat, got %+v", it.Scenario.Steps)
			}
			// Precondition: the target is on the battlefield (a tap list entry
			// for a non-battlefield card would be meaningless) and is listed
			// as tapped by name exactly once.
			if !seatPlacement(it.Scenario, target) {
				t.Fatalf("target %q is not on a named battlefield", target)
			}
			name := target[strings.IndexByte(target, ':')+1:] // "pN:Name"
			tapped := 0
			for _, s := range it.Scenario.Setup {
				for _, n := range s.Tapped {
					if n == name {
						tapped++
					}
				}
			}
			if tapped != 1 {
				t.Fatalf("target %q is tapped %d times in setup, want exactly 1", target, tapped)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("gorge cannot play the tapped-target scenario: %+v", it.Scenario.Steps)
			}
		})
	}
}

// TestBlockingTargetScenarioIsCastable proves the blocking shape (Vanquish:
// "target blocking creature"): p0 attacks with a spare creature, p1 blocks
// with the target, and the cast targets the blocker. The block follows the
// attack directly: no pass_to checkpoint exists, because XMage cannot observe
// the pre-block declare-blockers state (its engine selects blockers before
// any priority), so a scenario that stopped there could never agree.
func TestBlockingTargetScenarioIsCastable(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Vanquish")
	if skip != nil {
		t.Fatalf("no generated scenario: %s", skip.Reason)
	}
	attack := stepFor(it.Scenario, "attack")
	block := stepFor(it.Scenario, "block")
	if attack == nil || len(attack.Attackers) != 1 {
		t.Fatalf("no single-attacker attack step: %+v", it.Scenario.Steps)
	}
	if stepFor(it.Scenario, "pass_to") != nil {
		t.Fatalf("a blocking scenario must not emit a pre-block pass_to checkpoint: %+v", it.Scenario.Steps)
	}
	if block == nil || len(block.Blocks) != 1 {
		t.Fatalf("no block step: %+v", it.Scenario.Steps)
	}
	attacker := attack.Attackers[0]
	blocker := block.Blocks[0][0]
	if block.Blocks[0][1] != attacker {
		t.Fatalf("block pairs %q against %q, want the declared attacker %q", blocker, block.Blocks[0][1], attacker)
	}
	if !seatPlacement(it.Scenario, blocker) {
		t.Fatalf("blocker %q is not on a named battlefield (vacuous setup)", blocker)
	}
	cast := stepFor(it.Scenario, "cast")
	if cast == nil || len(cast.Targets) == 0 || cast.Targets[0] != blocker {
		t.Fatalf("cast does not target the blocker %q: %+v", blocker, it.Scenario.Steps)
	}
	if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
		t.Fatalf("gorge cannot play the blocking-target scenario: %+v", it.Scenario.Steps)
	}
}

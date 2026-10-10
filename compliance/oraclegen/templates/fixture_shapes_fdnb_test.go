package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateLifeTotalGate: Ayli's exile ability is gated on "at least 10
// life more than your starting life total" (CheckSVar$ X | SVarCompare$ GEY,
// X = Count$YourLifeTotal, Y = Count$YourStartingLife/Plus.10). The item sets
// p0's setup life to exactly 30, and the same scenario at the format's
// starting life does not play through: the gate, not the fixture, decides.
func TestActivateLifeTotalGate(t *testing.T) {
	reg := loadGenRegistry(t)
	it, _ := activateRequirement(t, reg, "Ayli, Eternal Pilgrim", "activate#0.1")
	p0 := it.Scenario.Setup["p0"]
	if p0.Life == nil || *p0.Life != 30 {
		t.Fatalf("p0 setup life = %v, want 30", p0.Life)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	control := it.Scenario
	control.Setup = map[string]oraclegen.Seat{"p0": p0, "p1": it.Scenario.Setup["p1"]}
	low := int32(29)
	p0.Life = &low
	control.Setup["p0"] = p0
	if _, ok := oraclegen.PlaysThrough(reg, control); ok {
		t.Fatalf("control: Ayli's gated ability resolved at 29 life")
	}
}

// TestLifeTotalGateValues pins the comparands lifeTotalGate reads.
func TestLifeTotalGateValues(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name string
		want int32
	}{
		{"Ayli, Eternal Pilgrim", 30},      // GEY, Y = Count$YourStartingLife/Plus.10
		{"Speaker of the Heavens", 27},     // GEY, Y = Count$YourStartingLife/Plus.7
		{"Bilbo, Birthday Celebrant", 111}, // GE111
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s missing", tc.name)
		}
		f := c.Faces[0]
		got := int32(0)
		for _, sa := range f.Abilities {
			check := sa.Params["CheckSVar"]
			if check == "" {
				continue
			}
			if life, ok := lifeTotalGate(f, f.SVars[check], sa.Params["SVarCompare"]); ok {
				got = life
			}
		}
		if got != tc.want {
			t.Errorf("%s: life gate = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestActivateNonbasicLandTarget: Demolition Field's "target nonbasic land an
// opponent controls" gets a nonbasic land on p1's battlefield (Forest, the
// generic land candidate, is basic and never legal).
func TestActivateNonbasicLandTarget(t *testing.T) {
	reg := loadGenRegistry(t)
	it, _ := activateRequirement(t, reg, "Demolition Field", "activate#0.1")
	var targets []string
	for _, st := range it.Scenario.Steps {
		if st.Op == "activate" {
			targets = st.Targets
		}
	}
	if len(targets) != 1 || targets[0] != "p1:Crystal Vein" {
		t.Fatalf("activate targets = %v, want [p1:Crystal Vein]", targets)
	}
	if !inZone(it.Scenario.Setup["p1"].Battlefield, "Crystal Vein") {
		t.Fatalf("p1 battlefield %v lacks the nonbasic land", it.Scenario.Setup["p1"].Battlefield)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
}

// TestActivateAttackingTargetCombatPrelude: Ingenious Leonin's "another target
// attacking creature you control" has the fixture's attacker declared
// attacking before the activation, which then happens in combat.
func TestActivateAttackingTargetCombatPrelude(t *testing.T) {
	reg := loadGenRegistry(t)
	it, _ := activateRequirement(t, reg, "Ingenious Leonin", "activate#0.0")
	steps := it.Scenario.Steps
	attack, activate := -1, -1
	for i, st := range steps {
		switch st.Op {
		case "attack":
			attack = i
		case "activate":
			activate = i
		}
	}
	if attack < 0 || activate < 0 || attack > activate {
		t.Fatalf("steps = %+v, want an attack before the activation", steps)
	}
	if len(steps[activate].Targets) != 1 || len(steps[attack].Attackers) != 1 || steps[activate].Targets[0] != steps[attack].Attackers[0] {
		t.Fatalf("activation targets %v, attackers %v: want the attacker targeted", steps[activate].Targets, steps[attack].Attackers)
	}
	if steps[activate].Targets[0] == "p0:Ingenious Leonin" {
		t.Fatalf("the target must be another creature, got the Leonin itself")
	}
	if len(it.XAbility) != len(steps) || it.XAbility[activate] == "" || it.XAbility[attack] != "" {
		t.Fatalf("XAbility = %q, want the prefix on the activate step only", it.XAbility)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
}

// TestDiesOtherDamagedByCombat: Predator Ooze's "whenever a creature dealt
// damage by this creature this turn dies" is caused by combat: the Ooze
// attacks, p1's 1/1 blocks and dies to its damage. The trigger is on the
// stack at end of combat, and without the block it never fires.
func TestDiesOtherDamagedByCombat(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Predator Ooze", "trigger#0.0", "trigger.dies-other")
	if !inZone(it.Scenario.Setup["p1"].Battlefield, damagedByBlocker) {
		t.Fatalf("p1 battlefield %v lacks the blocker", it.Scenario.Setup["p1"].Battlefield)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	atEndCombat := func(sc oraclegen.Scenario, block bool) bool {
		var steps []oraclegen.Step
		for _, st := range sc.Steps {
			if st.Op == "block" && !block {
				continue
			}
			if st.Op == "pass_to" {
				steps = append(steps, oraclegen.Step{Op: "pass_to", Step: "end-combat"})
				break
			}
			steps = append(steps, st)
		}
		sc.Steps = steps
		_, res, ok := oraclegen.Settle(reg, sc)
		return ok && abilityOnStack(res.Snapshots, []string{"predator ooze"}, "0")
	}
	if !atEndCombat(it.Scenario, true) {
		t.Fatalf("the dies trigger is not on the stack at end of combat")
	}
	if atEndCombat(it.Scenario, false) {
		t.Fatalf("control: the dies trigger fired with no creature damaged by the Ooze")
	}
}

// TestStateSelfCountersTrigger: Mazemind Tome's "When there are four or more
// page counters on it" starts with three and its own scry activation adds
// the fourth as a cost. Starting one counter lower, the same activation does
// not fire it.
func TestStateSelfCountersTrigger(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Mazemind Tome", "trigger#0.0", stateSelfCountersSub)
	p0 := it.Scenario.Setup["p0"]
	if got := p0.Counters["Mazemind Tome"]["PAGE"]; got != 3 {
		t.Fatalf("setup PAGE counters = %d, want 3", got)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	if !stackedAfterCause(t, reg, it.Scenario, "Mazemind Tome", "0") {
		t.Fatalf("the state trigger never reaches the stack")
	}
	control := it.Scenario
	control.Setup = map[string]oraclegen.Seat{"p0": oraclegen.WithCounters(p0, "Mazemind Tome", "PAGE", -1), "p1": it.Scenario.Setup["p1"]}
	if stackedAfterCause(t, reg, control, "Mazemind Tome", "0") {
		t.Fatalf("control: the state trigger fired with three page counters after the activation")
	}
}

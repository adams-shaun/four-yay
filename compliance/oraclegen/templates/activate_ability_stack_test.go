package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Ability-on-stack target fixtures (ticket agent-20261009T085207Z-c99a362b,
// activate_ability_stack.go): the level-B activate rows whose target is an
// ACTIVATED or TRIGGERED ability PENDING on the stack. One "ability prelude"
// serves them all: a probe permanent's simple ability is activated (or, for
// a triggered-only TargetType$, a card is moved onto the battlefield so its
// ETB trigger fires) and left UNRESOLVED on the stack, and the ability under
// test targets it through the runner's pN:ability:<source> ref.

// abilityStackProbeCard is the prelude's probe the scenario places or moves,
// read back from the generated setup (the plan's own probe choice).
func abilityStackProbeCard(t *testing.T, it oraclegen.Item, levelBIdx int) (probe string, probeStep int) {
	t.Helper()
	for i := 0; i < levelBIdx; i++ {
		st := it.Scenario.Steps[i]
		if st.Op == "activate" && st.Card != it.Card {
			return strings.TrimPrefix(st.Card, "p0:"), i
		}
		if st.Op == "move" {
			return strings.TrimPrefix(st.Card, "p0:"), i
		}
	}
	t.Fatalf("%s: no probe prelude step before the level-B activate step: %+v", it.Card, it.Scenario.Steps)
	return "", 0
}

// assertAbilityStackItem checks the shared shape of a generated
// ability-on-stack activate item and replays it clean on gorge.
// TestActivateAbilityStackActivatedProbe covers the four ACTIVATED-probe
// rows: Gogo (FIN) and Peter Parker's Camera (SPM) with the plain `Card`
// filter, Echo and Scientist Supreme of A.I.M. (MSH) with a card-type filter
// judged against the pending ability's source (Card.Creature / Artifact).
func TestActivateAbilityStackActivatedProbe(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Gogo, Master of Mimicry", "Peter Parker's Camera", "Echo, Perceptive Prodigy", "Scientist Supreme of A.I.M."} {
		t.Run(name, func(t *testing.T) {
			it, _ := activateRequirement(t, reg, name, "activate#0.0")
			// Precondition: the ability under test really targets an
			// ability on the stack.
			c, _ := reg.Lookup(name)
			sa := c.Faces[0].Abilities[0]
			if !oraclegen.AbilityTargetsStack(sa.Params) {
				t.Fatalf("precondition: %s ability 0 does not target the stack", name)
			}
			activateIdx := activateStepIndex(it.Scenario.Steps)
			if activateIdx == 0 {
				t.Fatalf("%s: no probe prelude before the activate step", name)
			}
			probe, probeStep := abilityStackProbeCard(t, it, activateIdx)
			// The probe's activation rides its own activate step and is
			// never resolved before the ability under test runs: pending is
			// the whole point.
			for i := probeStep + 1; i < activateIdx; i++ {
				if it.Scenario.Steps[i].Op == "resolve" {
					t.Fatalf("%s: a resolve between the probe and the ability under test unpends it (step %d)", name, i)
				}
			}
			if st := it.Scenario.Steps[probeStep]; st.AbilityIndex == nil {
				t.Fatalf("%s: the probe step names no ability index", name)
			}
			// The ability under test targets the pending probe ability.
			wantRef := "p0:ability:" + probe
			if len(it.Scenario.Steps[activateIdx].Targets) != 1 ||
				it.Scenario.Steps[activateIdx].Targets[0] != wantRef {
				t.Fatalf("%s: activate targets = %v, want [%s]",
					name, it.Scenario.Steps[activateIdx].Targets, wantRef)
			}
			// Precondition: the probe sits on p0's battlefield at setup.
			found := false
			for _, bf := range it.Scenario.Setup["p0"].Battlefield {
				if bf == probe {
					found = true
				}
			}
			if !found {
				t.Fatalf("precondition: probe %s is not on p0's battlefield: %v", probe, it.Scenario.Setup["p0"].Battlefield)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s does not play through gorge: ok=%v fails=%v\n%s", name, ok, res.Fails, strings.Join(res.Transcript, "\n"))
			}
			// The level-B ask OFFERED and PICKED the pending ability.
			picked := false
			for _, d := range res.Decisions {
				if d.Step != activateIdx || d.Kind != "target" {
					continue
				}
				if len(d.PickRefs) != 1 || d.PickRefs[0] != wantRef {
					t.Fatalf("%s: target ask picked %v, want [%s]", name, d.PickRefs, wantRef)
				}
				picked = true
			}
			if !picked {
				t.Fatalf("%s: no target decision at the activate step; decisions %+v", name, res.Decisions)
			}
			// The copy resolved: the probe deals 1 damage twice (copy +
			// original), so p1 lost exactly 2 life.
			final := res.Snapshots[len(res.Snapshots)-1]
			if life := final.Players[1].Life; life != 18 {
				t.Fatalf("%s: p1 life = %d, want 18 (the probe resolved twice)", name, life)
			}
			if len(final.Stack) != 0 {
				t.Fatalf("%s: final stack = %+v, want empty", name, final.Stack)
			}
		})
	}
}

// TestActivateAbilityStackTriggeredProbe covers Kirol (ECL), the
// triggered-only row: the prelude MOVES Elvish Visionary onto the
// battlefield so its ETB trigger is the pending object.
func TestActivateAbilityStackTriggeredProbe(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Kirol, Attentive First-Year"
	it, _ := activateRequirement(t, reg, name, "activate#0.0")
	c, _ := reg.Lookup(name)
	sa := c.Faces[0].Abilities[0]
	// Precondition: the ability targets triggered abilities only, which is
	// why the prelude must leave a TRIGGER pending.
	if sa.ParamStr(cards.PKTargetType) != "Triggered.YouCtrl" {
		t.Fatalf("precondition: Kirol TargetType$ = %q, want Triggered.YouCtrl", sa.ParamStr(cards.PKTargetType))
	}
	activateIdx := activateStepIndex(it.Scenario.Steps)
	probe, probeStep := abilityStackProbeCard(t, it, activateIdx)
	if probe != "Elvish Visionary" || it.Scenario.Steps[probeStep].Op != "move" {
		t.Fatalf("%s: probe prelude = (%s at step %d, op %s), want the Visionary move",
			name, probe, probeStep, it.Scenario.Steps[probeStep].Op)
	}
	for i := probeStep + 1; i < activateIdx; i++ {
		if it.Scenario.Steps[i].Op == "resolve" {
			t.Fatalf("%s: a resolve between the move and the activate unpends the trigger (step %d)", name, i)
		}
	}
	if len(it.Scenario.Steps[activateIdx].Targets) != 1 ||
		it.Scenario.Steps[activateIdx].Targets[0] != "p0:ability:Elvish Visionary" {
		t.Fatalf("%s: activate targets = %v, want [p0:ability:Elvish Visionary]",
			name, it.Scenario.Steps[activateIdx].Targets)
	}
	// Precondition: the Visionary waits in p0's hand; the move fires the
	// trigger (setup placements fire no ETB triggers).
	found := false
	for _, h := range it.Scenario.Setup["p0"].Hand {
		if h == "Elvish Visionary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: the Visionary is not in p0's hand: %v", it.Scenario.Setup["p0"].Hand)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("%s does not play through gorge: ok=%v fails=%v\n%s", name, ok, res.Fails, strings.Join(res.Transcript, "\n"))
	}
	picked := false
	for _, d := range res.Decisions {
		if d.Step != activateIdx || d.Kind != "target" {
			continue
		}
		if len(d.PickRefs) != 1 || d.PickRefs[0] != "p0:ability:Elvish Visionary" {
			t.Fatalf("%s: target ask picked %v, want [p0:ability:Elvish Visionary]", name, d.PickRefs)
		}
		picked = true
	}
	if !picked {
		t.Fatalf("%s: no target decision at the activate step; decisions %+v", name, res.Decisions)
	}
	// The copy resolved: the trigger and its copy each drew a card, on top
	// of the empty post-move hand.
	final := res.Snapshots[len(res.Snapshots)-1]
	if got := len(final.Players[0].Hand); got != 2 {
		t.Fatalf("%s: p0's hand has %d cards, want 2 (two draws)", name, got)
	}
	if len(final.Stack) != 0 {
		t.Fatalf("%s: final stack = %+v, want empty", name, final.Stack)
	}
}

// Ability-stack target fixtures for the level-B activate template (ticket
// agent-20261009T085207Z-c99a362b, level B activate rows whose target is an
// ACTIVATED or TRIGGERED ability PENDING on the stack: Gogo, Kirol, Peter
// Parker's Camera, Echo, Scientist Supreme of A.I.M.). The engine already
// offers such objects (rules/target_legal.go stackKindAdmits); the gap was
// the fixture: no candidate left an ability pending. The serving shape is
// one "ability prelude": a PROBE permanent's simple ability is activated
// (or, for a triggered-only TargetType$, a card is moved onto the
// battlefield so its ETB trigger fires) and left UNRESOLVED on the stack --
// the runner's activate and move ops both stop at the next priority
// decision without resolving -- and the ability under test targets that
// pending object through the runner's `pN:ability:<source name>` ref
// (rules/oracle_run.go resolve's ability branch).
//
// The ValidTgts$ card-type filter on an ability target is judged against the
// ability's SOURCE object (rules/target_legal.go specSrc = o.Source), so the
// probe is picked by source type and a wrong-source probe is simply never
// offered -- no extra filtering here.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The probe permanents, one per source shape an ability-stack target names.
// Both activated probes deal 1 damage to an opponent, so the probe's own
// target ask is answered with the player ref p1 and neither carries a
// rider ability the copy would compound:
//
//	Prodigal Pyromancer (creature source): {T}: deals 1 damage to any target.
//	Razortip Whip (artifact source): {1}{T}: deals 1 damage to target
//	opponent or planeswalker.
//
// Elvish Visionary is the triggered probe: setup placements do not fire ETB
// triggers, so its "When enters, draw a card" is pushed by a mid-game move
// op instead (rules/oracle_run.go move arm + priorityRound).
const (
	abilityProbeCreature = "Prodigal Pyromancer"
	abilityProbeArtifact = "Razortip Whip"
	abilityProbeTrigger  = "Elvish Visionary"
)

// abilityStackPlan is the ability-target fixture plan for one ability: the
// probe to leave pending, its activation step (or the trigger's move step)
// and the pN:ability: ref every stack slot targets.
type abilityStackPlan struct {
	probe       string   // battlefield probe whose activated ability pends ("")
	trigger     string   // hand card moved onto the battlefield so its ETB trigger pends ("")
	mana        string   // the probe ability's mana cost ("" for a {T}-only one)
	probeIndex  int      // the probe ability's IR index into Face().Abilities
	probeTarget string   // the probe ability's own scripted target ("" when targetless)
	targets     []string // one p0:ability:<source> ref per stack slot, in slot order
}

// abilityStackPlanOf plans the ability prelude when the ability's target
// slots are EXACTLY ONE ability-kind stack slot (TargetType$
// Activated/Triggered/SpellAbility): the five level-B rows this serves. A
// spell-kind stack target (Instant/Sorcery -- the spell precast's shape) and
// any mixed or multi-slot chain keep the ordinary path, which still skips;
// widening this is the sibling spell-stack tickets' work, not ours.
func abilityStackPlanOf(reg *cards.Registry, sa *cards.SA, slots []oraclegen.Slot) *abilityStackPlan {
	if sa == nil || len(slots) != 1 {
		return nil
	}
	links := 0
	var params map[string]string
	for s := sa; s != nil; s = s.Sub {
		if strings.TrimSpace(s.Params["ValidTgts"]) == "" {
			continue
		}
		links++
		params = s.Params
	}
	if links != 1 || !oraclegen.SlotIsStack(slots[0].Filter) {
		return nil
	}
	kinded, triggeredOnly := false, true
	for _, part := range strings.Split(params["TargetType"], ",") {
		switch strings.SplitN(strings.TrimSpace(part), ".", 2)[0] {
		case "Activated", "SpellAbility":
			kinded, triggeredOnly = true, false
		case "Triggered":
			kinded = true
		}
	}
	if !kinded {
		return nil
	}
	plan := &abilityStackPlan{}
	source := abilityProbeTrigger
	if !triggeredOnly {
		// A "from a creature source" / plain Card filter is served by the
		// creature probe; "from an artifact source" by the artifact one.
		source = abilityProbeCreature
		if strings.Contains(strings.ToLower(strings.TrimSuffix(slots[0].Filter, "@Stack")), "artifact") {
			source = abilityProbeArtifact
		}
		plan.probe = source
		idx, mana, target := abilityProbeSpec(reg, source)
		if idx < 0 {
			return nil
		}
		plan.probeIndex, plan.mana, plan.probeTarget = idx, mana, target
	} else {
		plan.trigger = source
	}
	plan.targets = []string{"p0:ability:" + source}
	return plan
}

// abilityProbeSpec reads one activated probe's IR ability index (its first
// non-mana AB) and its cost/own-target shape.
func abilityProbeSpec(reg *cards.Registry, probe string) (idx int, mana, target string) {
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return -1, "", ""
	}
	for i, a := range c.Faces[0].Abilities {
		if a.Kind == "AB" && !cards.IsManaAbilityAPI(a.API) {
			// Both probes cost {T} (Whip adds {1}) and target the opponent,
			// so a mana-free pool top-up of one C and the player ref p1
			// serve the whip and a bare tap serves the pyromancer.
			return i, map[bool]string{true: "", false: "C"}[probe == abilityProbeCreature], "p1"
		}
	}
	return -1, "", ""
}

// setup adds the probe to the seat's setup: a battlefield probe is placed
// before turn 1 (not summoning sick), the triggered probe waits in hand for
// the move step that fires its ETB trigger.
func (p *abilityStackPlan) setup(s *oraclegen.Seat) {
	if p.probe != "" {
		s.Battlefield = appendFixtureUnique(s.Battlefield, p.probe)
	}
	if p.trigger != "" {
		s.Hand = appendFixtureUnique(s.Hand, p.trigger)
	}
}

// steps is the prelude that leaves the probe ability PENDING on the stack:
// the activate op stops at the next priority decision without resolving
// (rules/oracle_run.go untilPriority), and the move op's own
// priorityRound+untilPriority pushes the ETB trigger and stops at the same
// priority -- no resolve step, no pass.
func (p *abilityStackPlan) steps() []oraclegen.Step {
	if p.probe != "" {
		return []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + p.probe,
			Mana: p.mana, AbilityIndex: &p.probeIndex, Targets: probeTargetsOf(p.probeTarget)}}
	}
	return []oraclegen.Step{{Op: "move", Seat: 0, Card: "p0:" + p.trigger, To: "battlefield"}}
}

func probeTargetsOf(target string) []string {
	if target == "" {
		return nil
	}
	return []string{target}
}

// activateWithAbilityStack serves the plan: the base fixture (no
// non-stack slots to place), the ability prelude before the ability under
// test, whose stack slot targets the pending probe ability ref.
func activateWithAbilityStack(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, prefix, mana, cost, zone string, slots []oraclegen.Slot, restrictions []conditionPrelude, plan *abilityStackPlan) (oraclegen.Item, bool) {
	preludes := withTokenCostPrelude(reg, cost, append([]conditionPrelude{{}}, restrictions...))
	for _, fx := range oraclegen.Fixtures(reg, nil) {
		for _, pre := range preludes {
			it, ok := activateWithFixture(reg, f, name, req, idx, prefix, mana, cost, zone, fx, pre, slots, plan)
			if ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// Level-B trigger recipes for the combat/keyword remainder: a Vehicle's own
// block, an opponent's attack count, an Aura/Equipment becoming attached, an
// untap step, excess noncombat damage, an opponent's loyalty activation and
// an opponent's own-turn cast. Each cause is built from ops the runner
// already has (attach, activate, attack, block, pass_to, cast); the fire
// probe (gorge's own trigger matcher) still decides whether the row fires.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// auraProbe is the Aura an attach cause casts onto a bearer: {W}, enchant
// creature, no choice on resolution.
const auraProbe = "Holy Strength"

// excessDamageProbe is the 1-toughness creature p1 controls that takes one
// point of excess damage from Shock (2 damage).
const excessDamageProbe = "Elvish Mystic"

// abilityTriggeredProbe is the attacker whose own attack trigger is the
// causing ability an AbilityTriggered watcher reads.
const abilityTriggeredProbe = "Borderland Marauder"

// caseSolvedProbe is the Case whose "To solve" condition already holds at
// setup (nothing is suspected, and setup drops the Case's enter trigger), so
// its bare solve sequence is the cause a CaseSolved watcher reads.
const caseSolvedProbe = "Case of the Stashed Skeleton"

// attachExtraProbe is the second creature beside an attachment's bearer, so
// a "becomes attached" trigger whose effect targets another creature you
// control (Blade of Shared Souls) has a legal target at queue time.
const attachExtraProbe = "Llanowar Elves"

// remainderTriggerRecipe builds the causes for the remainder sub-families.
// ok is false for every other sub-family, which baseTriggerRecipe then treats
// as it always has.
func remainderTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	add := func(c triggerCause, yes bool) {
		if yes {
			causes = append(causes, c)
		}
	}
	switch sub {
	case "trigger.attached":
		add(attachedCause(reg, f, name, t))
	case "trigger.untap-all":
		// "Whenever you untap one or more permanents during your untap
		// step": tap a probe with the tap spell (setup's Tapped list is
		// undone by the genesis untap step), then pass to p0's next untap
		// step, where the probe untaps. The trigger queues during the untap
		// step and is on the stack at the upkeep checkpoint.
		c, yes := castCause(reg, name, tapSpellProbe, "p0:"+bearsProbe)
		if yes {
			c.battlefield = append(c.battlefield, bearsProbe)
			c.steps = append(c.steps, oraclegen.Step{Op: "pass_to", Step: "upkeep", Active: "p0"})
		}
		add(c, yes)
	case "trigger.excess-damage":
		// Shock at a 1-toughness creature p1 controls: one point of excess
		// noncombat damage, the smallest cause the ExcessDamageAll matcher
		// admits.
		if _, exists := reg.Lookup(excessDamageProbe); !exists {
			return nil, "excess-damage probe not in corpus", true
		}
		c, yes := castCause(reg, name, shockProbe, "p1:"+excessDamageProbe)
		if yes {
			c.opponentBattlefield = []string{excessDamageProbe}
		}
		add(c, yes)
	case "trigger.attacks-opponent":
		// "Whenever one or more of your opponents are attacked": p0's own
		// attack on p1. triggerAttacker picks the attacker the line's own
		// ValidAttackers$ filter accepts.
		attackers, extra := triggerAttacker(reg, f, name, t)
		add(triggerCause{
			battlefield: extra,
			steps:       []oraclegen.Step{p0Attack(attackers...)},
		}, true)
	case "trigger.blocks-vehicle":
		c, yes := vehicleBlockCause(reg, f, name, t)
		add(c, yes)
	case "trigger.ability-activated-opponent":
		for _, probe := range loyaltyProbes {
			add(opponentLoyaltyCause(reg, name, probe))
		}
	case "trigger.ability-triggered":
		// Borderland Marauder's own "whenever this attacks" line is the
		// causing ability: attacking with it emits the engine's
		// AbilityTriggered marker (ValidMode Attacks, own ability), which
		// the row's watcher reads.
		if _, exists := reg.Lookup(abilityTriggeredProbe); !exists {
			return nil, "ability-triggered probe not in corpus", true
		}
		add(triggerCause{
			battlefield: []string{abilityTriggeredProbe},
			steps:       []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + abilityTriggeredProbe}}},
		}, true)
	case "trigger.case-solved":
		// A probe Case on p0's battlefield is solved by its own end-step
		// "To solve" trigger (CR 719.3a): pass to the end step, resolve the
		// solve trigger with two passes, and the row trigger is on the stack
		// from the Solved grant. Case of the Stashed Skeleton's solve
		// condition holds at setup (nothing is suspected, and setup drops
		// the Case's enter trigger).
		if _, exists := reg.Lookup(caseSolvedProbe); !exists {
			return nil, "case-solved probe not in corpus", true
		}
		add(triggerCause{
			battlefield: []string{caseSolvedProbe},
			steps: []oraclegen.Step{
				{Op: "pass_to", Step: "end"},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
			},
		}, true)
	default:
		return nil, "", false
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus or no matching ability", true
	}
	return causes, "", true
}

// attachedCause builds the "becomes attached" cause. The card itself (an
// Equipment/Reconfigure) is placed unattached and the attach op makes it
// become attached to a probe bearer; an Aura source (the card itself, or any
// Aura attaching to the source) is cast onto its bearer, because an
// unattached Aura is binned by the state-based actions before the first step.
func attachedCause(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (triggerCause, bool) {
	src := strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKValidSource)))
	if src == "card.self" {
		if _, ok := reg.Lookup(bearsProbe); !ok {
			return triggerCause{}, false
		}
		// A second plain creature beside the bearer, so a trigger whose
		// effect targets "another creature you control" (Blade of Shared
		// Souls) has a legal target at queue time.
		return triggerCause{
			battlefield: []string{bearsProbe, attachExtraProbe},
			steps: []oraclegen.Step{{
				Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + bearsProbe,
			}},
		}, true
	}
	// The bearer is the source itself when the line watches an Aura arrive
	// on CARDNAME, otherwise a probe creature p0 controls.
	bearer := bearsProbe
	if namesSelfFold(t.ParamStr(cards.PKValidTarget)) {
		bearer = name
	}
	c, ok := castCause(reg, name, auraProbe, "p0:"+bearer)
	if !ok {
		return triggerCause{}, false
	}
	if bearer == bearsProbe {
		c.battlefield = append(c.battlefield, bearsProbe)
	}
	return c, true
}

// vehicleBlockCause crews (or saddles) the source Vehicle during p1's attack,
// then declares it as the blocker. The crew activation is the same one the
// attack cause uses (attackActivationCause): the activation-cost fixtures
// supply the tapped creatures and the XMage answers. The activation happens
// after p1's declare-attackers decision, so the until-end-of-turn animation
// is still live when blockers are declared.
func vehicleBlockCause(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (triggerCause, bool) {
	act, ok := attackActivationCause(reg, f, name, t)
	if !ok || len(act.steps) == 0 {
		return triggerCause{}, false
	}
	activate := act.steps[0]
	return triggerCause{
		battlefield:         act.battlefield,
		opponentBattlefield: []string{bearsProbe},
		steps: []oraclegen.Step{
			{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + bearsProbe}},
			{Op: "pass", Seat: 1},
			activate,
			{Op: "resolve"},
			{Op: "block", Seat: 0, Blocks: [][2]string{{"p0:" + name, "p1:" + bearsProbe}}},
		},
		xability:     act.xability,
		activateCost: act.activateCost,
	}, true
}

// opponentLoyaltyCause activates the first plus ability of a probe
// planeswalker p1 controls, during p1's own main phase (a loyalty ability is
// sorcery-speed). The activate step names the ability by IR index and carries
// XMage's rule-text prefix, exactly as the p0 loyalty cause does.
func opponentLoyaltyCause(reg *cards.Registry, name, probe string) (triggerCause, bool) {
	if probe == name {
		return triggerCause{}, false
	}
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := c.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || !strings.HasPrefix(sa.ParamStr(cards.PKCost), "AddCounter<") || sa.ParamStr(cards.PKValidTgts) != "" {
			continue
		}
		prefix, okp := prefixes[i]
		if !okp {
			return triggerCause{}, false
		}
		idx := i
		return triggerCause{
			opponentBattlefield: []string{probe},
			steps: []oraclegen.Step{
				{Op: "pass_to", Step: "main1", Active: "p1"},
				{Op: "activate", Seat: 1, Card: "p1:" + probe, AbilityIndex: &idx},
			},
			xability: []string{prefix},
		}, true
	}
	return triggerCause{}, false
}

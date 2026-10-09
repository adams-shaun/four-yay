// Level-B static template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 7). It
// serves statics observed through stack/state differences, combat damage,
// attack/block legality and priority options. A legality candidate is served
// only when its control proves the action is available without the static and
// its observation proves the restriction is active.
//
// Both observations ride an existing checkpoint: the stack snapshot for
// DisableTriggers, the players' life for combat damage. DisableTriggers is a
// "nothing happens" claim, and a field that does not change after setup is
// never frozen, so its item also carries a Step.Expect -- with it a later
// regression fails the frozen `fails` field (compliance/oraclediff/freeze.go).
package templates

import (
	"encoding/json"
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// StaticObserved serves the level-B static sub-families that observe an
// effect via an existing checkpoint. Its own version: bumping it stales only
// this family's level-B rows.
var StaticObserved = Template{ID: "static", Version: 1}

// staticSubs are the level-B static sub-families this template serves.
func staticSubs(sub string) bool {
	switch sub {
	case "static.disable-triggers", "static.combat-damage-toughness", "static.can-attack-defender", "static.can-attack-defender-svar", "static.cant-block-by", "static.cant-be-cast-threshold", "static.cant-be-cast-combat", "static.cant-be-activated-combat", "static.cant-block-self", "static.cant-block-by-self", "static.min-blockers",
		"static.cant-be-cast-opponent-turn", "static.cant-be-cast-first-turns", "static.cant-be-cast-limit", "static.cant-be-activated-opponent-turn", "static.cant-be-activated-all", "static.cant-be-activated-enchanted", "static.panharmonicon", "static.optional-cost",
		"static.cant-attack-enchanted", "static.cant-block-enchanted", "static.cant-block-by-blocker-filter", "static.cant-gain-life", "static.mana-convert-creature-spells", "static.cant-be-activated-named",
		"static.tap-power-value", "static.cast-with-flash", "static.untap-other-player", "static.cant-draw",
		"static.cant-attack-gated", "static.cant-block-gated", "static.cant-block-by-gated", "static.can-attack-defender-gated", "static.max-blockers", "static.must-attack-self":
		return true
	}
	return false
}

// disableTriggerProbe is a creature with a self-ETB trigger (draw a card) and
// no targets, so both the observation and its control replay without an
// answer. Spec hypothesis H4: it exists in XMage's card database; the host
// replay confirms it.
const disableTriggerProbe = "Elvish Visionary"

// toughnessAttacker is a creature whose power is less than its toughness
// (2/4), so CombatDamageToughness changes the damage it deals from 2 to 4.
// It has no defender, so it does not also need a CanAttackDefender static.
const toughnessAttacker = "Giant Spider"

// staticRequirement builds the scenario serving one static requirement.
func staticRequirement(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + why}
	}
	switch req.Sub {
	case "static.disable-triggers":
		return disableTriggersItem(reg, f, name, req)
	case "static.combat-damage-toughness":
		return combatDamageToughnessItem(reg, f, name, req)
	case "static.can-attack-defender", "static.can-attack-defender-svar":
		return canAttackDefenderItem(reg, f, name, req)
	case "static.cant-block-by":
		return cantBlockByItem(reg, f, name, req)
	case "static.cant-block-self":
		return cantBlockSelfItem(reg, f, name, req)
	case "static.cant-block-by-self":
		return unblockableItem(reg, f, name, req)
	case "static.min-blockers":
		return minBlockersItem(reg, f, name, req)
	case "static.cant-attack-enchanted":
		return cantAttackEnchantedItem(reg, f, name, req)
	case "static.cant-block-enchanted":
		return cantBlockEnchantedItem(reg, f, name, req)
	case "static.cant-block-by-blocker-filter":
		return cantBlockByBlockerFilterItem(reg, f, name, req)
	case "static.cant-attack-gated":
		return gatedCantAttackItem(reg, f, name, req)
	case "static.cant-block-gated":
		return gatedCantBlockItem(reg, f, name, req)
	case "static.cant-block-by-gated":
		return gatedUnblockableItem(reg, f, name, req)
	case "static.can-attack-defender-gated":
		return gatedCanAttackDefenderItem(reg, f, name, req)
	case "static.max-blockers":
		return maxBlockersItem(reg, f, name, req)
	case "static.must-attack-self":
		return mustAttackItem(reg, f, name, req)
	case "static.cant-gain-life":
		return cantGainLifeItem(reg, f, name, req)
	case "static.mana-convert-creature-spells":
		return manaConvertCreatureItem(reg, f, name, req)
	case "static.cant-be-activated-named":
		return cantBeActivatedNamedItem(reg, f, name, req)
	case "static.cant-be-cast-threshold":
		return staticCastOffer(reg, f, name, req, false)
	case "static.cant-be-cast-combat":
		return staticCastOffer(reg, f, name, req, true)
	case "static.cant-be-activated-combat":
		return cantBeActivatedItem(reg, f, name, req)
	case "static.cant-be-cast-opponent-turn":
		return cantBeCastOpponentTurnItem(reg, f, name, req)
	case "static.cant-be-cast-first-turns":
		return cantBeCastFirstTurnsItem(reg, f, name, req)
	case "static.cant-be-cast-limit":
		return cantBeCastLimitItem(reg, f, name, req)
	case "static.cant-be-activated-opponent-turn":
		return cantBeActivatedOpponentTurnItem(reg, f, name, req)
	case "static.cant-be-activated-all":
		return cantBeActivatedAllItem(reg, f, name, req)
	case "static.cant-be-activated-enchanted":
		return cantBeActivatedEnchantedItem(reg, f, name, req)
	case "static.panharmonicon":
		return panharmoniconItem(reg, f, name, req)
	case "static.optional-cost":
		return optionalCostItem(reg, f, name, req)
	case "static.tap-power-value":
		return tapPowerValueItem(reg, f, name, req)
	case "static.cast-with-flash":
		return castWithFlashItem(reg, f, name, req)
	case "static.untap-other-player":
		return untapOtherPlayerItem(reg, f, name, req)
	case "static.cant-draw":
		return cantDrawItem(reg, f, name, req)
	}
	return skip("no observation for " + req.Sub)
}

// staticScenario is the scenario skeleton every observation shares: the card
// under test on p0's battlefield, the probe cards in p0's hand and on the
// battlefield, and p1's baseline permanent.
func staticScenario(f *cards.Face, name string, battlefield []string, hand []string, steps []oraclegen.Step) oraclegen.Scenario {
	// No named library: both engines pad each seat's library to 40 with the
	// Wastes filler, which survives any turn a combat-decision checkpoint
	// traverses. A named library of Plains made the setup library_top differ
	// by construction: XMage stacks the filler ON TOP of named library cards
	// (ScenarioReplay.build), gorge shuffles them in with the filler.
	p0 := oraclegen.Seat{Hand: append([]string(nil), hand...)}
	for _, b := range battlefield {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, b)
	}
	p1 := oraclegen.Seat{}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        append([]oraclegen.Step(nil), steps...),
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}

// runStatic runs a scenario in gorge directly (not oraclegen.Settle, which
// appends resolve steps and would settle the very trigger the observation
// watches).
func runStatic(reg *cards.Registry, sc oraclegen.Scenario) (rules.OracleResult, bool) {
	b, err := json.Marshal(sc)
	if err != nil {
		return rules.OracleResult{}, false
	}
	res, err := rules.RunOracleScenarioJSON(reg, b)
	return res, err == nil
}

// triggerOnStack reports whether card ref is an ability stack entry in snap.
func triggerOnStack(snap rules.OracleSnapshot, ref string) bool {
	for _, e := range snap.Stack {
		if e.Kind == "ability" && e.Source == ref {
			return true
		}
	}
	return false
}

// disableTriggersItem serves DisableTriggers (Karn, Argent Defender): with
// the card on the battlefield, the probe's enters-the-battlefield trigger
// does not go on the stack; without it, it does. The control proves the probe
// has a suppressible trigger, so the item cannot pass vacuously.
func disableTriggersItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static DisableTriggers " + why}
	}
	probe, ok := reg.Lookup(disableTriggerProbe)
	if !ok || len(probe.Faces) == 0 {
		return skip("probe not in corpus")
	}
	pool, why := oraclegen.PoolFor(probe.Faces[0].ManaCost)
	if why != "" {
		return skip("probe mana: " + why)
	}
	cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + disableTriggerProbe, Mana: pool}
	// The probe resolves on the second pass; the trigger it would cause sits
	// on the stack at that checkpoint.
	pass := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}

	// Control: the probe alone. Its trigger must reach the stack.
	control, ok := runStatic(reg, staticScenario(f, name, nil, []string{disableTriggerProbe}, append([]oraclegen.Step{cast}, pass...)))
	if !ok || len(control.Snapshots) == 0 || !triggerOnStack(control.Snapshots[len(control.Snapshots)-1], "p0:"+disableTriggerProbe) {
		return skip("probe's trigger does not fire without the card")
	}

	// Observation: the card on the battlefield suppresses the trigger. The
	// want=false expectation holds the "nothing happens" claim.
	want := false
	steps := append([]oraclegen.Step{cast}, pass...)
	steps[len(steps)-1].Expect = []oraclegen.Expect{{TriggerOnStack: "p0:" + disableTriggerProbe, Want: &want}}
	sc := staticScenario(f, name, []string{name}, []string{disableTriggerProbe}, steps)
	p0 := sc.Setup["p0"]
	setupBackFace(&p0, name, req)
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("the expectation does not hold")
	}
	if triggerOnStack(res.Snapshots[len(res.Snapshots)-1], "p0:"+disableTriggerProbe) {
		return skip("trigger still on the stack with the card")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"614.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// combatDamageToughnessItem serves CombatDamageToughness (Ghalta, the
// Immovable): a creature you control with toughness greater than its power
// assigns combat damage equal to its toughness. The control (the attacker
// alone) shows the damage its power would assign; the observation (the card
// on the battlefield) shows the toughness instead.
func combatDamageToughnessItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static CombatDamageToughness " + why}
	}
	attacker, ok := reg.Lookup(toughnessAttacker)
	if !ok || len(attacker.Faces) == 0 || !powerLToughness(attacker.Faces[0]) {
		return skip("attacker has no power < toughness")
	}
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + toughnessAttacker}},
		{Op: "pass_to", Seat: 0, Step: "main2"},
	}
	// Control: the attacker alone assigns its power.
	control, ok := runStatic(reg, staticScenario(f, name, []string{toughnessAttacker}, nil, steps))
	if !ok {
		return skip("control scenario does not replay")
	}
	base, ok := damageToDefender(control)
	if !ok {
		return skip("control has no combat-damage checkpoint")
	}
	// Observation: the card on the battlefield raises the damage to toughness.
	sc := staticScenario(f, name, []string{name, toughnessAttacker}, nil, steps)
	p0 := sc.Setup["p0"]
	setupBackFace(&p0, name, req)
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("observation scenario does not replay")
	}
	got, ok := damageToDefender(res)
	if !ok {
		return skip("observation has no combat-damage checkpoint")
	}
	if got <= base {
		return skip("the card does not raise the assigned damage")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"510.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// damageToDefender returns the damage p1 took in the last checkpoint that
// followed the attack, read as the drop in p1's life.
func damageToDefender(res rules.OracleResult) (int32, bool) {
	if len(res.Snapshots) < 2 {
		return 0, false
	}
	life := func(s rules.OracleSnapshot) (int32, bool) {
		for _, p := range s.Players {
			if p.Seat == 1 {
				return p.Life, true
			}
		}
		return 0, false
	}
	full, _ := life(res.Snapshots[0])
	last, ok := life(res.Snapshots[len(res.Snapshots)-1])
	if !ok {
		return 0, false
	}
	return full - last, true
}

// powerLToughness reports whether f's power is less than its toughness, the
// Forge filter the CombatDamageToughness static matches on.
func powerLToughness(f *cards.Face) bool {
	p, q, ok := parsePT(f.PT)
	return ok && p < q
}

// parsePT parses a Forge P/T string ("2/4"); an empty or non-numeric part
// (a '*' or a variable) reports !ok.
func parsePT(pt string) (int, int, bool) {
	slash := -1
	for i := 0; i < len(pt); i++ {
		if pt[i] == '/' {
			slash = i
			break
		}
	}
	if slash < 0 {
		return 0, 0, false
	}
	a, errA := strconv.Atoi(pt[:slash])
	b, errB := strconv.Atoi(pt[slash+1:])
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return a, b, true
}

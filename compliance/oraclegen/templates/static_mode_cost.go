// Level-B observations for the damage- and cost-reading static modes (ticket
// levelb-remaining-static-modes): damage a prevention source cannot stop
// (unconditional and combat-only), the attack/block taxes an untapped or
// attacking Archangel of Tithes prices onto the opponent's combat pairs, and
// the two ManaConvert shapes the creature-spell one does not cover. The
// controls replay the same steps with the card absent; where the claim is a
// refusal (the cast or the pair is unavailable), the control's failing replay
// is the proof.
package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// preventVictim is the creature the prevention covers; preventProbe the
// prevention source; preventDamageProbe the burn spell.
const (
	preventVictim      = "Giant Spider"
	preventProbe       = "Hold at Bay"
	preventDamageProbe = "Shock"
	preventDefender    = "Grizzly Bears"
)

// preventSteps builds the unconditional observation: the prevention covers
// the victim, then the burn lands on it.
func preventSteps(victim string) []oraclegen.Step {
	return []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + preventProbe, Mana: "WW", Targets: []string{"p0:" + victim}},
		{Op: "resolve"},
		{Op: "cast", Seat: 0, Card: "p0:" + preventDamageProbe, Mana: "R", Targets: []string{"p0:" + victim}},
		{Op: "resolve"},
	}
}

// cantPreventDamageItem serves CantPreventDamage in both shapes. The
// unconditional one burns the protected creature: the control without the card
// shows the prevention absorbing the Shock, the observation shows it landing.
// The IsCombat$ True shape protects p1 from p0's unblocked attack instead:
// combat damage to a player is prevented without the card and lands with it.
func cantPreventDamageItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantPreventDamage"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	st, _ := staticSlotOf(f, req)
	combat := strings.EqualFold(st.ParamStr(cards.PKIsCombat), "True")
	var steps []oraclegen.Step
	setup := []string{name, preventVictim}
	if !combat {
		steps = preventSteps(preventVictim)
	} else {
		setup = []string{name, preventDefender}
		steps = []oraclegen.Step{
			{Op: "cast", Seat: 0, Card: "p0:" + preventProbe, Mana: "WW", Targets: []string{"p1"}},
			{Op: "resolve"},
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + preventDefender}},
			{Op: "pass_to", Seat: 0, Step: "main2"},
		}
	}
	// Control: the card absent; the victim takes no damage (or p1 loses no
	// life, for the combat shape).
	control := staticScenario(f, name, setup[1:2], []string{preventProbe, preventDamageProbe}, steps)
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	if !combat {
		cp, ok := permByNameIn(cres, preventVictim)
		if !ok || cp.Damage != 0 {
			return skip(fmt.Sprintf("control does not show the damage prevented (damage %d)", cp.Damage))
		}
	} else if lifeOf(cres, 1) != 20 {
		return skip(fmt.Sprintf("control does not show the combat damage prevented (p1 life %d)", lifeOf(cres, 1)))
	}
	// Observation: the card live; the damage lands.
	sc := staticScenario(f, name, setup, []string{preventProbe, preventDamageProbe}, steps)
	sc = withBackFace(sc, name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if !combat {
		op, ok := permByNameIn(res, preventVictim)
		if !ok || op.Damage != 2 {
			return skip(fmt.Sprintf("the damage did not land with the card on the battlefield (damage %d)", op.Damage))
		}
	} else if lifeOf(res, 1) != 18 {
		return skip(fmt.Sprintf("the combat damage did not land with the card on the battlefield (p1 life %d)", lifeOf(res, 1)))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"615.6"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// unlessAttacker is the attacker both CantAttackUnless runs check at p1's
// declare-attackers decision; p1 holds no mana, so the {1} tax the card
// prices onto the pair withholds the option.
const unlessAttacker = "Grizzly Bears"

// cantAttackUnlessTaxItem serves CantAttackUnless (Archangel of Tithes's
// untapped static, the card on p1's side): with no mana floated, p0's
// declare-attackers decision withholds the Memnite (the card's {1} tax on
// every creature attacking p1 exceeds p0's empty pool); the control without
// the card offers it.
func cantAttackUnlessTaxItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttackUnless"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	mk := func(want bool, withCard bool) oraclegen.Scenario {
		steps := []oraclegen.Step{
			{Op: "pass_to", Seat: 0, Decision: "attackers",
				Expect: []oraclegen.Expect{{CanAttack: &oraclegen.CanAttack{Attacker: "p0:" + unlessAttacker}, Want: boolPtr(want)}}},
		}
		if withCard {
			// With the tax live the attackers decision is not posed until p0
			// passes the priority the step opens with, so the pair of passes
			// reaches it.
			steps = append([]oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "declare-attackers"}}, steps...)
		}
		sc := staticScenario(f, name, nil, nil, steps)
		if withCard {
			p1 := sc.Setup["p1"]
			p1.Battlefield = append(p1.Battlefield, name)
			sc.Setup["p1"] = p1
		}
		p0 := sc.Setup["p0"]
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, unlessAttacker)
		sc.Setup["p0"] = p0
		return sc
	}
	sc := mk(false, true)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if !onBattlefield(res, "p1:"+name) {
		return skip("the card is not on the battlefield at the checkpoint")
	}
	cres, ok := runStatic(reg, mk(true, false))
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not offer the attack (ok=%v fails=%v)", ok, cres.Fails))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"508.1g"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// unlessBlocker is the blocker both CantBlockUnless runs check at p1's
// declare-blockers decision.
const unlessBlocker = "Grizzly Bears"

// cantBlockUnlessTaxItem keeps Archangel of Tithes's attacking static a named
// gap: the blockers decision is not posed when the {1} tax leaves no
// affordable block, and every affordable block pays the same {1}, so no
// checkpoint the scenario grammar reaches can show the tax refusing a pair.
func cantBlockUnlessTaxItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockUnless"
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode +
		" needs a paid-block checkpoint: the blockers decision is not posed when the {1} tax leaves no affordable block"}
}

// manaConvertCaseProbe is the Case spell the conversion pays for from
// off-colour mana.
const manaConvertCaseProbe = "Case of the Gorgon's Kiss"

// manaConvertCaseItem serves ManaConvert over Case spells (Case File
// Auditor): p0 casts the Case probe from red mana with the card on the
// battlefield; the control without the card refuses the cast (the replay
// fails at it).
func manaConvertCaseItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "ManaConvert"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	if _, ok := reg.Lookup(manaConvertCaseProbe); !ok {
		return skip("probe not in corpus")
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + manaConvertCaseProbe, Mana: "R"},
		{Op: "resolve"},
	}
	// Control: the card absent; red cannot pay the black pip.
	control := staticScenario(f, name, nil, nil, steps)
	if cres, ok := runStatic(reg, control); ok && len(cres.Fails) == 0 &&
		onBattlefield(cres, "p0:"+manaConvertCaseProbe) {
		return skip("control casts the probe from off-colour mana without the card")
	}
	// Observation: the card live; the cast resolves and the Case enters.
	sc := withBackFace(staticScenario(f, name, []string{name}, []string{manaConvertCaseProbe}, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the probe is not cast from off-colour mana with the card (ok=%v fails=%v)", ok, res.Fails))
	}
	if !onBattlefield(res, "p0:"+manaConvertCaseProbe) {
		return skip("the probe did not resolve onto the battlefield")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"609.4b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// manaConvertAbilityProbe is the creature whose ability the conversion pays
// for from red mana; its rider grants lifelink to its own target, the
// compared trace the activation leaves.
const (
	manaConvertAbilityProbe = "Alabaster Mage"
	manaConvertAbilityKW    = "Lifelink"
)

// manaConvertAbilityItem serves ManaConvert over activated abilities
// (Agatha's Soul Cauldron): the probe's ability activates from a red pool with
// the card on the battlefield and its lifelink grant reaches the target; the
// control without the card refuses the payment (the replay fails at it).
func manaConvertAbilityItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "ManaConvert"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	probe, ok := reg.Lookup(manaConvertAbilityProbe)
	if !ok || len(probe.Faces) == 0 || len(probe.Faces[0].Abilities) != 1 {
		return skip("probe does not carry exactly one activated ability")
	}
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + manaConvertAbilityProbe, Mana: "RR", AbilityIndex: intPtr(0),
			Targets: []string{"p0:" + manaConvertAbilityProbe}},
		{Op: "resolve"},
	}
	granted := func(res rules.OracleResult) bool {
		p, ok := permByNameIn(res, manaConvertAbilityProbe)
		if !ok {
			return false
		}
		for _, k := range p.Keywords {
			if strings.EqualFold(k, manaConvertAbilityKW) {
				return true
			}
		}
		return false
	}
	// Control: the card absent; red cannot pay the activation cost.
	control := staticScenario(f, name, []string{manaConvertAbilityProbe}, nil, steps)
	if cres, ok := runStatic(reg, control); ok && len(cres.Fails) == 0 && granted(cres) {
		return skip("control activated the ability from off-colour mana without the card")
	}
	// Observation: the card live; the activation resolves and the grant shows.
	sc := withBackFace(staticScenario(f, name, []string{name, manaConvertAbilityProbe}, nil, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the ability is not activated from off-colour mana with the card (ok=%v fails=%v)", ok, res.Fails))
	}
	if !granted(res) {
		return skip("the activation's grant did not reach the target")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"609.4b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

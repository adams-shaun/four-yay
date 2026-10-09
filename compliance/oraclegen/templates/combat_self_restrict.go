package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// Self-restriction combat observations (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 7): a
// creature whose own "CARDNAME can't attack/block unless ..." static makes
// gorge refuse the action has no scripted declaration to compare, so the item
// stops AT the declare decision and asserts, through Step.Expect, that the
// restricted creature is absent from the offered options while a vanilla
// probe on the same board is present -- the same shape the Defender and
// CantBlock observations (combat_legality.go) use. The probe is the control
// that cannot be vacuous: the decision exists and offers the probe, and only
// the card under test is missing.
//
// The static, not the outcome, is the guard: a card that died at setup or was
// tapped by its own trigger is also "not offered", so the observation is only
// emitted for a card whose script carries a self-scoped CantAttack/CantBlock
// static, and the card is required to sit untapped on the battlefield at the
// checkpoint.

// selfScope reports whether a static's ValidCard$/ValidCreature$ scope names
// the card itself, with optional qualifiers after it (Card.Self+powerLT6).
func selfScope(st *cards.Static) bool {
	for _, key := range []cards.ParamKey{cards.PKValidCard, cards.PKValidCreature} {
		scope := strings.ToLower(strings.TrimSpace(st.ParamStr(key)))
		if scope == "card.self" || scope == "creature.self" ||
			strings.HasPrefix(scope, "card.self+") || strings.HasPrefix(scope, "creature.self+") {
			return true
		}
	}
	return false
}

// selfCombatRestrictionStatic returns the face's static whose comma-separated
// Mode list contains want exactly (so CantAttackUnless never matches
// CantAttack) and whose scope names the card itself. Any gate parameters
// (CheckSVar$, IsPresent$, ...) belong to that static's own condition.
func selfCombatRestrictionStatic(f *cards.Face, want string) *cards.Static {
	for i := range f.Statics {
		st := &f.Statics[i]
		if !selfScope(st) {
			continue
		}
		for _, mode := range strings.Split(st.Mode, ",") {
			if strings.EqualFold(strings.TrimSpace(mode), want) {
				return st
			}
		}
	}
	return nil
}

// selfRestrictionAttackItem serves a self-CantAttack creature's combat.attack
// with the attacker-not-offered observation.
func selfRestrictionAttackItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttack"
	if !f.IsCreature() || name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probe")
	}
	probe := cardAt(0, smallAttackerProbe)
	steps := []oraclegen.Step{{
		Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
		Expect: []oraclegen.Expect{canAttackExpect(probe, true), canAttackExpect(cardAt(0, name), false)},
	}}
	sc := withBackFace(staticScenario(f, name, []string{name, smallAttackerProbe}, nil, steps), name, req)
	return selfRestrictionFinish(reg, f, name, mode, req, sc, []string{"508.1a", "508.1d"})
}

// selfRestrictionBlockItem serves a self-CantBlock creature's combat.block
// with the blocker-not-offered observation.
func selfRestrictionBlockItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlock"
	if !f.IsCreature() || name == blockerProbe || name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	attacker := cardAt(1, smallAttackerProbe)
	sc := legalityBlockScenario(f, name, req, []oraclegen.Expect{
		canBlockExpect(cardAt(0, blockerProbe), attacker, true),
		canBlockExpect(cardAt(0, name), attacker, false),
	})
	return selfRestrictionFinish(reg, f, name, mode, req, sc, []string{"509.1b"})
}

// selfRestrictionFinish replays the observation, requires the card to sit
// untapped on the battlefield at the checkpoint (a tapped card is suppressed
// for the tap's own reason, not the static's), and builds the item.
func selfRestrictionFinish(reg *cards.Registry, f *cards.Face, name, mode string, req levelb.Requirement, sc oraclegen.Scenario, cr []string) (oraclegen.Item, *oraclegen.Skip) {
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "observation does not hold")
	}
	if !onBattlefieldUntapped(res, "p0:"+name) {
		return staticSkip(name, mode, "card is not an untapped battlefield permanent at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, cr, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// onBattlefieldUntapped is onBattlefield (combat_legality.go) with the tapped
// half added.
func onBattlefieldUntapped(res rules.OracleResult, ref string) bool {
	if len(res.Snapshots) == 0 {
		return false
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Ref == ref {
			return !p.Tapped
		}
	}
	return false
}

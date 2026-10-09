package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// Level-B combat-legality observations (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 7). A
// restriction that removes an attacker or a block from what the declare
// decision offers has no scripted declaration to compare, so the item stops AT
// the declare-attackers / declare-blockers decision and asserts, through
// Step.Expect, that the restricted creature is absent from the offered options
// while an unrestricted probe on the same board is present. The probe is the
// control that cannot be vacuous: the decision exists and offers options, and
// only the card under test is missing.
//
// The assertion rides gorge's frozen `fails` field. XMage's driver reads
// neither Step.Expect nor an `offered` snapshot field (tools/xmageoracle
// ScenarioReplay.java has no such code), and gorge's snapshot carries `offered`
// only at a priority decision (rules/oracle_snapshot.go), so an item at a
// declare decision sets no Compare: an `offered` compare there would compare
// two empty strings.

// evasionKeywords make a creature unblockable by the vanilla probe blocker for
// a reason other than the static under test; a card carrying one cannot prove
// that static from a "not offered" observation.
var evasionKeywords = []string{"Flying", "Fear", "Intimidate", "Shadow", "Horsemanship", "Skulk", "Menace", "Defender", "Landwalk"}

// cardAt is the scenario ref of a battlefield fixture on seat s.
func cardAt(s int, name string) string { return "p" + strconv.Itoa(s) + ":" + name }

// canAttackExpect is the "is this creature among the offered attackers"
// assertion.
func canAttackExpect(ref string, want bool) oraclegen.Expect {
	return oraclegen.Expect{CanAttack: &oraclegen.CanAttack{Attacker: ref}, Want: &want}
}

// canBlockExpect is the "may this blocker block this attacker" assertion.
func canBlockExpect(blocker, attacker string, want bool) oraclegen.Expect {
	return oraclegen.Expect{CanBlock: &oraclegen.CanBlock{Blocker: blocker, Attacker: attacker}, Want: &want}
}

func hasEvasion(f *cards.Face) bool {
	for _, k := range evasionKeywords {
		if f.HasKeyword(k) {
			return true
		}
	}
	return false
}

// combatNotOffered serves a combat.attack / combat.block requirement whose
// ordinary attack or block scenario cannot replay because gorge refuses the
// card the action. served is false when no structural cause is known (the
// caller keeps its own skip): a creature that merely died at setup is also
// "not offered", so the observation is only emitted for a card whose script
// carries the restriction that explains it.
func combatNotOffered(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (it oraclegen.Item, skip *oraclegen.Skip, served bool) {
	switch req.Sub {
	case "combat.attack":
		if f.HasKeyword("Defender") {
			it, skip = defenderNotOfferedItem(reg, f, name, req)
			return it, skip, true
		}
		if selfCombatRestrictionStatic(f, "CantAttack") != nil {
			it, skip = selfRestrictionAttackItem(reg, f, name, req)
			return it, skip, true
		}
	case "combat.block":
		for i := range f.Statics {
			if strings.EqualFold(f.Statics[i].Mode, "CantBlock") && f.IsCreature() && levelb.SelfLegalityStatic(&f.Statics[i], cards.PKValidCard) {
				it, skip = cantBlockSelfItem(reg, f, name, req)
				return it, skip, true
			}
		}
		if f.IsCreature() && selfCombatRestrictionStatic(f, "CantBlock") != nil {
			it, skip = selfRestrictionBlockItem(reg, f, name, req)
			return it, skip, true
		}
	}
	return oraclegen.Item{}, nil, false
}

// onBattlefield reports whether the last snapshot of res has ref on the
// battlefield: an observation about a creature that is not on the board is
// vacuous.
func onBattlefield(res rules.OracleResult, ref string) bool {
	if len(res.Snapshots) == 0 {
		return false
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Ref == ref {
			return true
		}
	}
	return false
}

// withBackFace puts the card under test in its back face when the requirement
// belongs to one, as combatScenario does.
func withBackFace(sc oraclegen.Scenario, name string, req levelb.Requirement) oraclegen.Scenario {
	p0 := sc.Setup["p0"]
	setupBackFace(&p0, name, req)
	sc.Setup["p0"] = p0
	return sc
}

// finishLegalityItem replays sc, requires every assertion to hold and the card
// under test to be on the battlefield, and builds the item.
func finishLegalityItem(reg *cards.Registry, f *cards.Face, name, mode string, req levelb.Requirement, sc oraclegen.Scenario, cr []string) (oraclegen.Item, *oraclegen.Skip) {
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("observation does not hold (ok=%v fails=%v)", ok, res.Fails))
	}
	if !onBattlefield(res, "p0:"+name) {
		return staticSkip(name, mode, "card is not on the battlefield at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, cr, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// defenderNotOfferedItem serves a Defender creature's combat.attack: at the
// declare-attackers decision the card is not offered while a vanilla probe
// is.
func defenderNotOfferedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "Defender"
	if !f.IsCreature() || name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probe")
	}
	probe := cardAt(0, smallAttackerProbe)
	steps := []oraclegen.Step{{
		Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
		Expect: []oraclegen.Expect{canAttackExpect(probe, true), canAttackExpect(cardAt(0, name), false)},
	}}
	sc := withBackFace(staticScenario(f, name, []string{name, smallAttackerProbe}, nil, steps), name, req)
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"702.3", "508.1a"})
}

// legalityBlockScenario is p1 attacking p0 with the vanilla probe, stopped at
// p0's declare-blockers decision with expect asserted there. p0 fields the
// card under test and the probe blocker.
func legalityBlockScenario(f *cards.Face, name string, req levelb.Requirement, expect []oraclegen.Expect) oraclegen.Scenario {
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{cardAt(1, smallAttackerProbe)}},
		{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: expect},
	}
	sc := withBackFace(staticScenario(f, name, []string{name, blockerProbe}, nil, steps), name, req)
	p1 := sc.Setup["p1"]
	p1.Battlefield = []string{smallAttackerProbe}
	sc.Setup["p1"] = p1
	return sc
}

// cantBlockSelfItem serves "CARDNAME can't block": at p0's declare-blockers
// decision the card may not block the attacker while the vanilla probe may.
func cantBlockSelfItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlock"
	if !f.IsCreature() || name == blockerProbe || name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	attacker := cardAt(1, smallAttackerProbe)
	sc := legalityBlockScenario(f, name, req, []oraclegen.Expect{
		canBlockExpect(cardAt(0, blockerProbe), attacker, true),
		canBlockExpect(cardAt(0, name), attacker, false),
	})
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b"})
}

// selfAttackerBlockScenario is p0 attacking p1 with the card under test and
// the vanilla large probe, stopped at p1's declare-blockers decision. p1 fields
// blockers copies of the probe blocker.
func selfAttackerBlockScenario(f *cards.Face, name string, req levelb.Requirement, blockers int, expect []oraclegen.Expect) oraclegen.Scenario {
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{cardAt(0, name), cardAt(0, largeAttackerProbe)}},
		{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: expect},
	}
	sc := withBackFace(staticScenario(f, name, []string{name, largeAttackerProbe}, nil, steps), name, req)
	p1 := sc.Setup["p1"]
	p1.Battlefield = oraclegen.Repeat(blockerProbe, blockers)
	sc.Setup["p1"] = p1
	return sc
}

// unblockableItem serves "CARDNAME can't be blocked" (CantBlockBy with the
// card as the attacker and no blocker filter): no probe blocker may block it,
// though the same blocker may block the vanilla probe attacker beside it.
func unblockableItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	if !f.IsCreature() || name == largeAttackerProbe || name == blockerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	if hasEvasion(f) {
		return staticSkip(name, mode, "another evasion would make the card unblockable without the static")
	}
	blocker, spider := cardAt(1, blockerProbe), cardAt(0, largeAttackerProbe)
	sc := selfAttackerBlockScenario(f, name, req, 1, []oraclegen.Expect{
		canBlockExpect(blocker, spider, true),
		canBlockExpect(blocker, cardAt(0, name), false),
	})
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b"})
}

// minBlockersItem serves "can't be blocked except by N or more creatures"
// (MinMaxBlocker Min$ N, the card as the attacker). Gorge does not offer the
// attacker to a defender with fewer than N legal blockers at all, so with N-1
// probe blockers the block is absent, and the control with N blockers offers
// it.
func minBlockersItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "MinMaxBlocker"
	if !f.IsCreature() || name == largeAttackerProbe || name == blockerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	if hasEvasion(f) {
		return staticSkip(name, mode, "another evasion would make the card unblockable without the static")
	}
	slot, err := strconv.Atoi(req.Slot)
	if err != nil || slot < 0 || slot >= len(f.Statics) {
		return staticSkip(name, mode, "requirement slot names no static")
	}
	n := levelb.MinBlockers(&f.Statics[slot])
	if n == 0 {
		return staticSkip(name, mode, "Min$ is not a plain bound")
	}
	blocker, spider, self := cardAt(1, blockerProbe), cardAt(0, largeAttackerProbe), cardAt(0, name)
	control := selfAttackerBlockScenario(f, name, req, n, []oraclegen.Expect{canBlockExpect(blocker, spider, true), canBlockExpect(blocker, self, true)})
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("control with %d blockers does not offer the block: %v", n, res.Fails))
	}
	sc := selfAttackerBlockScenario(f, name, req, n-1, []oraclegen.Expect{canBlockExpect(blocker, spider, true), canBlockExpect(blocker, self, false)})
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b", "702.111b"})
}

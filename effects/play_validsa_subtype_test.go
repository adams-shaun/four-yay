package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestValidSAOKHeroSubtype(t *testing.T) {
	hero := mkCard(t, "Name:Hero Fixture\nTypes:Creature Human Hero\nPT:2/2\nOracle:x\n").Faces[0]
	nonHero := mkCard(t, "Name:Nonhero Fixture\nTypes:Creature Human Soldier\nPT:2/2\nOracle:x\n").Faces[0]
	if !hero.IsCreature() || !CreatureTypeWords("Hero") {
		t.Fatal("precondition: fixture must be a creature with the registered Hero subtype")
	}
	if nonHero.IsCreature() != hero.IsCreature() || CreatureTypeWords("Soldier") != CreatureTypeWords("Hero") {
		t.Fatal("precondition: compared faces must both be creatures and differ in Hero subtype")
	}
	if !validSAOK(hero, "Spell.Hero+YouOwn", nil) {
		t.Fatal("Spell.Hero+YouOwn rejected a Hero creature")
	}
	if validSAOK(nonHero, "Spell.Hero+YouOwn", nil) {
		t.Fatal("Spell.Hero+YouOwn admitted a creature without the Hero subtype")
	}
	if validSAOK(hero, "Spell.NotARealCreatureType", nil) {
		t.Fatal("unknown ValidSA subtype did not fail closed")
	}
}

func TestPlayOffersHeroSubtypeFromHand(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	hero := h.g.AddObject(mkCard(t, "Name:Hero Fixture\nTypes:Creature Human Hero\nPT:2/2\nOracle:x\n"), 0)
	nonHero := h.g.AddObject(mkCard(t, "Name:Nonhero Fixture\nTypes:Creature Human Soldier\nPT:2/2\nOracle:x\n"), 0)
	hand := []state.ObjID{hero.ID, nonHero.ID}
	h.g.SetZone(state.ZHand, 0, hand)
	hero.Zone, nonHero.Zone = state.ZHand, state.ZHand
	if hero.Zone != state.ZHand || nonHero.Zone != state.ZHand {
		t.Fatal("precondition: both candidate cards must be in the controller's hand")
	}
	if hero.Face().Types[2] == nonHero.Face().Types[2] {
		t.Fatalf("precondition: fixtures unexpectedly share subtype %q", hero.Face().Types[2])
	}
	src := h.g.AddObject(mkCard(t, "Name:Play Source\nTypes:Sorcery\nOracle:x\n"), 0)
	src.Zone = state.ZBattlefield

	Resolve(h, &Ctx{Source: src.ID, Controller: 0}, sa(t,
		"SP$ Play | Valid$ Card | ValidSA$ Spell.Hero+YouOwn | ValidZone$ Hand | WithoutManaCost$ True | Amount$ 1 | Controller$ You | Optional$ True"))
	if h.asked == nil {
		t.Fatal("Play did not pose the expected choice")
	}
	if h.asked.ResumeKind != "play" {
		t.Fatalf("ResumeKind = %q, want play", h.asked.ResumeKind)
	}
	if len(h.asked.Options) != 1 || h.asked.Options[0].Obj != hero.ID {
		t.Fatalf("Play options = %+v, want only Hero object %d (non-Hero %d excluded)", h.asked.Options, hero.ID, nonHero.ID)
	}
}

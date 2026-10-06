package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Fixtures spelled after the LCI Craft carriers (written inline: Forge card
// scripts never enter the tree). Each front face carries the front-face
// behaviour a CR 702.167a craft must NOT run, because the card returns to the
// battlefield transformed (never front face up first).
const (
	craftNetSource = "Name:Craft Net\nManaCost:2 U\nTypes:Artifact\nK:etbCounter:NET:3\n" +
		"K:Craft:1 U ExileCtrlOrGrave<1/Artifact.Other>\nAlternateMode:DoubleFaced\nOracle:x\n\n" +
		"ALTERNATE\nName:Craft Quipu\nManaCost:no cost\nColors:blue\nTypes:Artifact\nOracle:x\n"
	craftBladeSource = "Name:Craft Blade\nManaCost:1 B\nTypes:Artifact\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigSac | TriggerDescription$ When CARDNAME enters, each opponent sacrifices a creature.\n" +
		"SVar:TrigSac:DB$ Sacrifice | Defined$ Opponent | SacValid$ Creature\n" +
		"K:Craft:4 B ExileCtrlOrGrave<1/Creature.Other>\nAlternateMode:DoubleFaced\nOracle:x\n\n" +
		"ALTERNATE\nName:Craft Sepulcher\nManaCost:no cost\nColors:black\nTypes:Artifact\nOracle:x\n"
	craftLatticeSource = "Name:Craft Lattice\nManaCost:1 R\nTypes:Artifact\n" +
		"K:Craft:4 R XMin1 ExileCtrlOrGrave<X/Dinosaur.Other>\nSVar:X:Count$xPaid\nAlternateMode:DoubleFaced\nOracle:x\n\n" +
		"ALTERNATE\nName:Craft Raptor\nManaCost:no cost\nColors:red\nTypes:Artifact Creature Dinosaur\nPT:*/4\n" +
		"S:Mode$ Continuous | CharacteristicDefining$ True | SetPower$ X | Description$ power is the total power of the exiled cards used to craft it.\n" +
		"SVar:X:ExiledWith$CardPower\nOracle:x\n"
	craftStandardSource = "Name:Craft Standard\nManaCost:3\nTypes:Artifact\n" +
		"K:Craft:5 XMin1 ExileCtrlOrGrave<X/Permanent.Other/permanent>\nSVar:X:Count$xPaid\nAlternateMode:DoubleFaced\nOracle:x\n\n" +
		"ALTERNATE\nName:Craft Effigy\nManaCost:no cost\nTypes:Artifact Creature\nPT:2/2\nOracle:x\n"
	craftDreadmaw = "Name:Craft Dreadmaw\nManaCost:4 G G\nTypes:Creature Dinosaur\nPT:6/6\nOracle:x\n"
	craftSolRing  = "Name:Craft Sol Ring\nManaCost:1\nTypes:Artifact\nOracle:x\n"
)

func craftOnBattlefield(t *testing.T, e *Engine, source state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(source); o.Zone != state.ZBattlefield || o.FaceIdx != 0 {
		t.Fatalf("precondition: Craft source must start on the battlefield front face, got zone %v face %d", o.Zone, o.FaceIdx)
	}
}

// TestCraftEntersTransformed: CR 702.167a returns the card transformed, so the
// front face's enters replacement (Braided Net's net counters) and enters
// trigger (Tithing Blade's sacrifice) never run on the craft's return.
func TestCraftEntersTransformed(t *testing.T) {
	t.Run("etb counters", func(t *testing.T) {
		// Control: the same front face entering normally DOES get its counters,
		// so the zero below is the craft's doing, not a fixture that never counts.
		ctl, _, ctlSrc := newFixtureDeck(t, 9301, craftNetSource)
		craftOnBattlefield(t, ctl, ctlSrc)
		if got := ctl.G.Obj(ctlSrc).Counter("NET"); got != 3 {
			t.Fatalf("control: front-face entry NET counters = %d, want 3", got)
		}

		e, _, source := newFixtureDeck(t, 9301, craftNetSource)
		craftOnBattlefield(t, e, source)
		e.G.Obj(source).Counters = nil
		putBattlefield(t, e, 0, craftSolRing)
		addMana(t, e, 0, "UU")
		driveCraftActivation(t, e, source, -1, nil)
		o := e.G.Obj(source)
		if o.Zone != state.ZBattlefield || o.FaceIdx != 1 {
			t.Fatalf("crafted card = zone %v face %d, want battlefield back face", o.Zone, o.FaceIdx)
		}
		if got := o.Counter("NET"); got != 0 {
			t.Fatalf("crafted Braided Quipu has %d NET counters, want 0 (it never entered front face up)", got)
		}
	})
	t.Run("etb trigger", func(t *testing.T) {
		oppCreature := "Name:Craft Victim\nManaCost:0\nTypes:Creature\nPT:1/1\nOracle:x\n"
		// Control: a normal front-face entry makes the opponent sacrifice.
		ctl, _, ctlSrc := newFixtureDeck(t, 9302, craftBladeSource)
		victim := putBattlefield(t, ctl, 1, oppCreature)
		craftOnBattlefield(t, ctl, ctlSrc)
		toMain1(t, ctl)
		ctl.priorityRound()
		passUntilStackEmpty(t, ctl, 20)
		if z := ctl.G.Obj(victim).Zone; z != state.ZGraveyard {
			t.Fatalf("control: front-face entry left the opponent's creature in %v, want sacrificed", z)
		}

		e, _, source := newFixtureDeck(t, 9302, craftBladeSource)
		craftOnBattlefield(t, e, source)
		// Drain the setup entry's own trigger (no opposing creature yet), so
		// only the craft's return can sacrifice the victim placed next.
		toMain1(t, e)
		e.priorityRound()
		passUntilStackEmpty(t, e, 20)
		victim = putBattlefield(t, e, 1, oppCreature)
		material := putGraveyard(t, e, 0, craftDreadmaw)
		addMana(t, e, 0, "BBBBB")
		driveCraftActivation(t, e, source, -1, []state.ObjID{material})
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(source); o.Zone != state.ZBattlefield || o.FaceIdx != 1 {
			t.Fatalf("crafted card = zone %v face %d, want battlefield back face", o.Zone, o.FaceIdx)
		}
		if z := e.G.Obj(victim).Zone; z != state.ZBattlefield {
			t.Fatalf("opponent's creature is in %v after the craft, want still on the battlefield", z)
		}
	})
}

// TestCraftMaterialsAreExiledWith: the materials are exiled with the crafted
// card (CR 702.167a), so Saheeli's Lattice's Raptor reads their total power.
func TestCraftMaterialsAreExiledWith(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9303, craftLatticeSource)
	craftOnBattlefield(t, e, source)
	dreadmaw := putGraveyard(t, e, 0, craftDreadmaw)
	if p := e.G.Obj(dreadmaw).Face().Power(); p != 6 {
		t.Fatalf("precondition: material power = %d, want 6", p)
	}
	addMana(t, e, 0, "RRRRR")
	driveCraftActivation(t, e, source, 1, []state.ObjID{dreadmaw})
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(dreadmaw).Zone; z != state.ZExile {
		t.Fatalf("material zone = %v, want exile", z)
	}
	if w := e.G.Obj(dreadmaw).ExiledWith; w != source {
		t.Fatalf("material ExiledWith = %d, want the crafted card %d", w, source)
	}
	if got := e.Derived(source).Power; got != 6 {
		t.Fatalf("Mastercraft Raptor power = %d, want 6", got)
	}
}

// TestCraftGraveyardPermanentMaterial: a `Permanent.Other` material spec
// (Sunbird Standard) accepts a permanent CARD in the graveyard, not only a
// permanent on the battlefield.
func TestCraftGraveyardPermanentMaterial(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9304, craftStandardSource)
	craftOnBattlefield(t, e, source)
	dreadmaw := putGraveyard(t, e, 0, craftDreadmaw)
	if z := e.G.Obj(dreadmaw).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: material in %v, want graveyard", z)
	}
	addMana(t, e, 0, "CCCCC")
	if _, ok := findAbilityOption(e, source, 0); !ok {
		t.Fatalf("Craft not offered with only a graveyard permanent card; options = %+v", e.Pending().Options)
	}
	driveCraftActivation(t, e, source, 1, []state.ObjID{dreadmaw})
	if o := e.G.Obj(source); o.Zone != state.ZBattlefield || o.FaceIdx != 1 {
		t.Fatalf("crafted card = zone %v face %d, want battlefield back face", o.Zone, o.FaceIdx)
	}
	if z := e.G.Obj(dreadmaw).Zone; z != state.ZExile {
		t.Fatalf("material zone = %v, want exile", z)
	}
}

// TestCraftGraveyardPermanentMaterialEnigma covers the sibling material spec:
// The Enigma Jewel's `Permanent.Other+nonLand+hasAbility Activated` must also
// accept graveyard permanent CARDS -- the same missing-zone bug as Sunbird's
// bare `Permanent.Other`, but through the suffix path. permanentCardBase
// rewrites a leading `Permanent` base with any suffix chain, so this is what
// proves the rewrite is not special-cased to the bare spelling.
func TestCraftGraveyardPermanentMaterialEnigma(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9305, craftDFCSource(enigmaKeyword))
	craftOnBattlefield(t, e, source)
	if got := e.G.Obj(source).Face().Abilities; len(got) == 0 {
		t.Fatal("precondition: the Enigma Jewel front face must carry its Craft ability")
	}
	var mats []state.ObjID
	for _, name := range []string{"Enigma Grave A", "Enigma Grave B", "Enigma Grave C", "Enigma Grave D"} {
		mats = append(mats, putGraveyard(t, e, 0, craftTestMaterial(name, "Creature")))
	}
	for _, id := range mats {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("precondition: material %d in %v, want graveyard", id, z)
		}
	}
	// {8}{U} plus self-exile: float ten blue.
	addMana(t, e, 0, "UUUUUUUUUU")
	if _, ok := findAbilityOption(e, source, 0); !ok {
		t.Fatalf("Craft not offered with only graveyard nonland permanent cards; options = %+v", e.Pending().Options)
	}
	driveCraftActivation(t, e, source, 4, mats)
	if o := e.G.Obj(source); o.Zone != state.ZBattlefield || o.FaceIdx != 1 {
		t.Fatalf("crafted card = zone %v face %d, want battlefield back face", o.Zone, o.FaceIdx)
	}
	for _, id := range mats {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("material %d zone = %v, want exile", id, z)
		}
	}
}

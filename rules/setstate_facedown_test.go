package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestCyberConversionTurnsCreatureFaceDown(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := facedownEngine(t, reg, "Cyber Conversion", "Llanowar Elves")
	spell := searchMoveByName(t, e, "Cyber Conversion", state.ZHand)
	creature := searchMoveByName(t, e, "Llanowar Elves", state.ZBattlefield)
	before := e.G.Obj(creature)
	if before.Zone != state.ZBattlefield || before.FaceDown || before.Face().Power() == 2 || before.Face().Toughness() == 2 {
		t.Fatalf("precondition: target must be a face-up battlefield creature with non-2/2 printed P/T: %+v", before)
	}
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("precondition: Cyber Conversion zone = %s, want hand", e.G.Obj(spell).Zone)
	}

	addMana(t, e, 0, "UU")
	castFromPriority(t, e, spell)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Cyber Conversion target decision = %+v", d)
	}
	answerKTarget(t, e, creature)
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(creature)
	if o.Zone != state.ZBattlefield || o.ID != creature || !o.FaceDown {
		t.Fatalf("target identity/zone/face-down = id %d zone %s faceDown %v", o.ID, o.Zone, o.FaceDown)
	}
	if o.FaceDownSetType != "Artifact & Creature & Cyberman" || !o.FaceDownHasPT || o.FaceDownPower != 2 || o.FaceDownToughness != 2 {
		t.Fatalf("folded face-down set = %q %d/%d hasPT=%v", o.FaceDownSetType, o.FaceDownPower, o.FaceDownToughness, o.FaceDownHasPT)
	}
	derived := e.Derived(creature)
	if len(derived.Types) != 3 || derived.Types[0] != "Artifact" || derived.Types[1] != "Creature" || derived.Types[2] != "Cyberman" || derived.Power != 2 || derived.Toughness != 2 {
		t.Fatalf("derived face-down characteristics = %v %d/%d", derived.Types, derived.Power, derived.Toughness)
	}
	own := view.Project(e.G, e, 0, nil)
	opponent := view.Project(e.G, e, 1, nil)
	assertProjectedFaceDownIdentity(t, own, creature, "Llanowar Elves", true)
	assertProjectedFaceDownIdentity(t, opponent, creature, "", false)
	foundEvent := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnFaceDown && ev.Obj == creature {
			foundEvent = true
			setType, power, toughness, hasPT, ok := events.FaceDownEntryFields(ev.Counter)
			if !ok || setType != "Artifact & Creature & Cyberman" || !hasPT || power != 2 || toughness != 2 {
				t.Fatalf("TurnFaceDown event payload = %q parsed %q %d/%d hasPT=%v", ev.Counter, setType, power, toughness, hasPT)
			}
		}
	}
	if !foundEvent {
		t.Fatal("Cyber Conversion emitted no TurnFaceDown event for target")
	}
	replayCheck(t, e, cfg)
}

func assertProjectedFaceDownIdentity(t *testing.T, projected view.View, id state.ObjID, name string, controller bool) {
	t.Helper()
	for _, player := range projected.Players {
		for _, card := range player.Battlefield {
			if card.ID == id {
				if !card.FaceDown || card.Name != name {
					t.Fatalf("projected face-down card = %+v, want name %q", card, name)
				}
				return
			}
		}
	}
	t.Fatalf("projected battlefield missing object %d (controller=%v)", id, controller)
}

func TestSetStateTurnFaceUpRevealsFaceDownPermanent(t *testing.T) {
	e, cfg, id := newFixtureDeck(t, 881, "Name:Face Fixture\nTypes:Creature Bear\nPT:3/4\nOracle:x\n")
	from := e.G.Obj(id).Zone
	if from != state.ZHand && from != state.ZLibrary {
		t.Fatalf("precondition: fixture source zone = %s, want hand or library", from)
	}
	// Seed the face-down fixture through the existing entry event so this
	// companion pins TurnFaceUp independently of the TurnFaceDown change.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield,
		Counter: events.FaceDownEntryCounterFor("Artifact & Creature", 2, 2, true)})
	o := e.G.Obj(id)
	if !o.FaceDown || o.FaceDownSetType != "Artifact & Creature" || o.FaceIdx != 0 {
		t.Fatalf("precondition: fixture did not become face down on current face: %+v", o)
	}
	if int32(o.Face().Power()) == o.FaceDownPower || int32(o.Face().Toughness()) == o.FaceDownToughness {
		t.Fatalf("precondition: printed characteristics must differ from folded face-down values: printed=%d/%d facedown=%d/%d", o.Face().Power(), o.Face().Toughness(), o.FaceDownPower, o.FaceDownToughness)
	}
	abilityCard := card(t, "Name:SetState Turn Face Up\nTypes:Sorcery\nA:DB$ SetState | Defined$ Targeted | Mode$ TurnFaceUp\nOracle:x\n")
	if len(abilityCard.Faces[0].Abilities) != 1 {
		t.Fatalf("precondition: expected one compiled inline ability, got %+v", abilityCard.Faces[0].Abilities)
	}
	turnFaceUp := abilityCard.Faces[0].Abilities[0]
	if turnFaceUp == nil || turnFaceUp.API != "SetState" || turnFaceUp.Params["Mode"] != "TurnFaceUp" {
		t.Fatalf("precondition: inline SetState TurnFaceUp did not compile: %+v", turnFaceUp)
	}
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, turnFaceUp)
	o = e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.FaceDown || o.FaceDownSetType != "" || o.FaceDownHasPT || o.FaceDownPower != 0 || o.FaceDownToughness != 0 || o.FaceIdx != 0 {
		t.Fatalf("turn-up did not reveal unchanged face and clear set: %+v", o)
	}
	if got := e.Derived(id); got.Power != 3 || got.Toughness != 4 {
		t.Fatalf("revealed characteristics = %d/%d, want 3/4", got.Power, got.Toughness)
	}
	replayCheck(t, e, cfg)
}

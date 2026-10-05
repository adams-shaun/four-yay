package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCraftSupportRegistrationKeepsExoticMarkerUnsupported(t *testing.T) {
	supported := effects.Supported()
	if !supported["kw:Craft"] {
		t.Fatal("kw:Craft is not registered as supported")
	}
	if supported["api:Craft.OtherShape"] {
		t.Fatal("api:Craft.OtherShape must remain unsupported so exotic carriers fail closed")
	}
}

const craftArtifactSource = "Name:Crafter\nManaCost:0\nTypes:Artifact\nK:Craft:0 ExileCtrlOrGrave<1/Artifact.Other>\nAlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\nName:Crafter Awakened\nManaCost:no cost\nTypes:Artifact Creature\nPT:3/3\nOracle:x\n"

func TestCraftAsksAcrossBothZonesAndBotAnswerValidates(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9102, craftArtifactSource)
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	bf := putBattlefield(t, e, 0, "Name:Battlefield Material\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	gy := putGraveyard(t, e, 0, "Name:Graveyard Material\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	if e.G.Obj(bf).Zone != state.ZBattlefield || e.G.Obj(gy).Zone != state.ZGraveyard {
		t.Fatal("precondition: one artifact material must occupy each candidate zone")
	}
	addMana(t, e, 0, "CC") // material and self-exile cost units
	e.Advance()
	option := abilityOption(t, e, source, 0)
	submitChoices(t, e, option.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Craft material decision = %+v, want both battlefield and graveyard candidates", d)
	}
	botAnswer := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}})
	if err := d.Validate(botAnswer); err != nil {
		t.Fatalf("botpolicy.Clamp answer rejected by decision validator: %v (%+v)", err, botAnswer)
	}
	chosenMaterial := state.ObjID(0)
	for _, option := range d.Options {
		if option.Index == botAnswer.Choices[0] {
			chosenMaterial = option.Obj
		}
	}
	submitChoices(t, e, botAnswer.Choices[0])
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(source); got == nil || got.Zone != state.ZBattlefield || got.FaceIdx != 1 {
		t.Fatalf("Craft return: source = %+v, want battlefield on transformed face", got)
	}
	remembered := false
	for _, target := range e.G.Obj(source).Remembered {
		remembered = remembered || (!target.IsPlayer && target.Obj == chosenMaterial)
	}
	if !remembered {
		t.Fatalf("transformed source Remembered = %+v, want paid material %d", e.G.Obj(source).Remembered, chosenMaterial)
	}
}

func TestCraftWithNoMaterialIsNotOffered(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9103, craftArtifactSource)
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	addMana(t, e, 0, "C")
	if len(e.G.Zone(state.ZBattlefield, 0)) == 0 || e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: Craft source must be on the battlefield")
	}
	if len(e.G.Obj(source).Face().Abilities) == 0 || e.G.Obj(source).Face().Abilities[0].API != "ChangeZone" {
		t.Fatal("precondition: Craft expander did not install its activated ability")
	}
	if hasActivateOption(e, source) {
		t.Fatal("Craft activation offered with no eligible artifact in battlefield or graveyard")
	}
}

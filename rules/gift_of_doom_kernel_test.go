package rules

// Kernel-era restorations of the gift_of_doom_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestGiftOfDoomFaceDownSurvivesAttachmentSBAAndTurnsFaceUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Gift of Doom", "Grizzly Bears", "Grizzly Bears")
	// Gift of Doom is an Aura: its printed cast is only offered once a legal
	// Enchant:Creature bearer exists, and morphDownCast asserts the PAIR (the
	// face-down option is an addition to the printed one). Seat the first
	// bear before the cast; it is also the sacrifice-cost creature.
	b1 := putCorpusPermanent(t, e, "Grizzly Bears")
	id := morphDownCast(t, e, "Gift of Doom", "morphed", "CCCCCB", 3)
	// Precondition: the printed face really carries the Aura subtype and the
	// Morph turn-up cost, while the face-down battlefield entry's CURRENT
	// characteristics are exactly [Creature] — the two reads the SBA must
	// keep apart.
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.FaceDown || o.Face() == nil {
		t.Fatalf("precondition: face-down battlefield entry missing: %+v", o)
	}
	if !hasTypeWord(o.Face().Types, "Aura") {
		t.Fatalf("precondition: printed types %v carry no Aura subtype", o.Face().Types)
	}
	if _, ok := o.Face().KeywordParam("Morph"); !ok {
		t.Fatalf("precondition: no Morph keyword on the printed face")
	}
	if der := e.Derived(id); len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("precondition: face-down derived types = %v, want exactly [Creature]", der.Types)
	}

	// The fix: an attachment SBA checkpoint must leave the face-down
	// permanent alone — no MoveZone, no graveyard.
	mark := len(e.L.Events)
	e.checkStateBased()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("the face-down Gift of Doom did not survive the SBA checkpoint: %+v", o)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.Obj == id {
			t.Fatalf("an SBA checkpoint moved the face-down Gift of Doom: %+v", ev)
		}
	}

	// Seat a second bear for the attach bearer (the first bear pays the
	// printed Sac<1/Creature.Other> turn-up cost). Both must really be on
	// the battlefield before the action is offered.
	b2 := putCorpusPermanent(t, e, "Grizzly Bears")
	for _, bid := range []state.ObjID{b1, b2} {
		if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear %d not on the battlefield", bid)
		}
	}

	mark = len(e.L.Events)
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	// The turn-up cost is real: with two Creature.Other candidates the flow
	// poses the sacrifice ask.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("sacrifice-cost ask = %+v", d)
	}
	sIdx := -1
	for _, opt := range d.Options {
		if opt.Obj == b1 {
			sIdx = opt.Index
		}
	}
	if sIdx < 0 {
		t.Fatalf("the sacrifice ask does not name bear %d: %+v", b1, d.Options)
	}
	submitChoices(t, e, sIdx)

	// The printed turn-up replacement (R:Event$ TurnFaceUp ReplaceWith$
	// DBAttach, Optional$ True) asks which creature to attach the now
	// face-up Aura to. Exactly one legal bearer survives the sacrifice: b2
	// (the source itself is never offered).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "card" {
		t.Fatalf("attach-choice ask = %+v", d)
	}
	aIdx := -1
	for _, opt := range d.Options {
		if opt.Obj == b2 {
			aIdx = opt.Index
		}
	}
	if aIdx < 0 {
		t.Fatalf("the attach ask does not name bear %d: %+v", b2, d.Options)
	}
	submitChoices(t, e, aIdx)
	passUntilStackEmpty(t, e, 20)

	// Exactly one TurnFaceUp, no stack use (the special action contract).
	assertTurnUpEventOnce(t, e, id, mark)
	o = e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceDown {
		t.Fatalf("Gift of Doom after the turn-up = %+v, want face up on the battlefield", o)
	}
	if o.AttachedTo != b2 {
		t.Fatalf("the face-up Gift of Doom attached to %d, want bear %d", o.AttachedTo, b2)
	}
	// The sacrificed cost creature is gone; the bearer carries the Aura's
	// granted keywords.
	if o := e.G.Obj(b1); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the sacrifice-cost bear %d is in %v, want the graveyard", b1, o)
	}
	if !e.HasKeyword(b2, "Deathtouch") || !e.HasKeyword(b2, "Indestructible") {
		t.Fatalf("bearer %d lacks the enchanted deathtouch/indestructible: %v", b2, e.Derived(b2).Keywords)
	}
	replayCheck(t, e, cfg)
}

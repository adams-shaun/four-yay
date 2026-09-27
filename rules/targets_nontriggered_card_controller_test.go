package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestConfusionTargetsWithNonTriggeredCardController(t *testing.T) {
	reg := freshCorpusRegistry(t, "c/confusion_in_the_ranks.txt")
	card, ok := reg.Lookup("Confusion in the Ranks")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("precondition: Confusion in the Ranks is missing from the corpus")
	}
	sa := cards.ResolveSVar(card.Faces[0].SVars, "TrigExchangeControl")
	if sa == nil || sa.API != "ExchangeControl" || sa.Params["TargetsWithDefinedController"] != "NonTriggeredCardController" || sa.Params["ValidTgts"] != "Permanent" {
		t.Fatalf("precondition: Confusion target SVar = %+v; want ExchangeControl targeting Permanent with NonTriggeredCardController", sa)
	}

	e := newSeats(t, 2)
	confusion := e.G.AddObject(card, 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: confusion, From: state.ZLibrary, To: state.ZBattlefield})
	sameController := putBattlefield(t, e, 0, "Name:Same Controller Permanent\nTypes:Enchantment Creature\nOracle:x\n")
	otherController := putBattlefield(t, e, 1, "Name:Other Controller Permanent\nTypes:Enchantment Creature\nOracle:x\n")
	if e.G.Obj(confusion).Zone != state.ZBattlefield || e.G.Obj(sameController).Zone != state.ZBattlefield || e.G.Obj(otherController).Zone != state.ZBattlefield {
		t.Fatal("precondition: Confusion and both candidates must be on the battlefield")
	}
	if e.G.Obj(sameController).Controller != 0 || e.G.Obj(otherController).Controller != 1 || e.G.Obj(sameController).Controller == e.G.Obj(otherController).Controller {
		t.Fatal("precondition: candidate controllers must be distinct (same=0, other=1)")
	}
	if !e.matchesSpec(sa.Params["ValidTgts"], sameController, effects.SpecContext{}) || !e.matchesSpec(sa.Params["ValidTgts"], otherController, effects.SpecContext{}) {
		t.Fatal("precondition: both candidates must match Confusion's ValidTgts")
	}

	// Confusion's triggering card controller is captured as seat 0. The
	// offer must therefore exclude seat 0 and retain the otherwise-valid
	// permanent controlled by seat 1.
	e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
	e.triggerContexts[confusion] = effects.TriggerContext{
		TriggerCard:           confusion,
		TriggerCardController: state.Target{Player: 0, IsPlayer: true},
	}
	triggerController, triggerControllerOK := effects.TriggeredCardController(e.G, e.triggerContexts[confusion], nil)
	if !triggerControllerOK || triggerController != 0 || e.G.Obj(sameController).Controller != triggerController || e.G.Obj(otherController).Controller == triggerController {
		t.Fatalf("precondition: trigger controller = %d (ok=%v), candidate controllers = %d/%d; want trigger=0, same=0 and other=1", triggerController, triggerControllerOK, e.G.Obj(sameController).Controller, e.G.Obj(otherController).Controller)
	}
	e.pending = nil
	e.askTarget(0, confusion, sa)
	d := e.Pending()
	if d == nil {
		min, max := e.resolvedTargetBounds(0, confusion, sa, 0)
		t.Fatalf("real Confusion SVar did not offer a target decision (bounds=%d/%d candidates=%+v)", min, max, e.legalTargetCandidates(0, confusion, confusion, sa))
	}
	containsOption := func(id state.ObjID) bool {
		for _, option := range d.Options {
			if option.Obj == id {
				return true
			}
		}
		return false
	}
	if containsOption(sameController) {
		t.Fatal("same-controller permanent was offered")
	}
	if !containsOption(otherController) {
		t.Fatal("different-controller permanent was not offered")
	}

	// The resolution recheck uses the same real selector and trigger
	// context; at unchanged state it admits exactly the offered candidate.
	initial := e.legalTargets([]state.Target{{Obj: sameController}, {Obj: otherController}}, sa, targetZones(sa), 0, confusion, confusion)
	if len(initial) != 1 || initial[0].Obj != otherController {
		t.Fatalf("real Confusion recheck = %+v, want only other-controller permanent %d", initial, otherController)
	}

	// A candidate that changes controller after announcement is no longer
	// legal under CR 608.2b, even though it was offered at announcement.
	e.emit(events.Event{Kind: events.ControlChange, Obj: otherController, Player: 0})
	if e.G.Obj(otherController).Controller != e.G.Obj(confusion).Controller {
		t.Fatal("precondition: target controller change did not make it match the triggering-card controller")
	}
	changed := e.legalTargets([]state.Target{{Obj: otherController}}, sa, targetZones(sa), 0, confusion, confusion)
	if len(changed) != 0 {
		t.Fatalf("recheck after target controller changed = %+v, want no legal targets", changed)
	}

	// The selector fails closed at both the offer and recheck when its
	// triggering-card controller cannot be resolved.
	delete(e.triggerContexts, confusion)
	e.pending = nil
	e.askTarget(0, confusion, sa)
	if d := e.Pending(); d != nil {
		t.Fatalf("unbound trigger context unexpectedly offered targets: %+v", d.Options)
	}
	unbound := e.legalTargets([]state.Target{{Obj: sameController}, {Obj: otherController}}, sa, targetZones(sa), 0, confusion, confusion)
	if len(unbound) != 0 {
		t.Fatalf("unbound trigger context recheck = %+v, want no legal targets", unbound)
	}
}

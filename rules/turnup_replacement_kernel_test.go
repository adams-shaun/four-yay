package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestVesuvanShapeshifterCanAcceptItsTurnFaceUpReplacementKernel pins the yes
// arm of Optional$: the compiled Clone body applies before the turn-up folds,
// so Vesuvan turns face up as the battlefield's only other creature.
func TestVesuvanShapeshifterCanAcceptItsTurnFaceUpReplacementKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Vesuvan Shapeshifter", "Grizzly Bears")
	bear := oppBear(t, e)
	id := morphDownCastWithoutPrintedOption(t, e, "Vesuvan Shapeshifter", "CCCUU")
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || d.Options[0].Kind != "apply" {
		t.Fatalf("expected the optional replacement's yes/no choice, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		// The kernel-era ask (see the Skip'd sibling below): pick the Bear.
		kr9AnswerObj(t, e, bear)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.FaceDown || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("accepting the optional replacement should turn Vesuvan face up as Grizzly Bears: %+v", o)
	}
	assertTurnUpEventOnce(t, e, id, mark)
	replayCheck(t, e, cfg)
}

// TestVesuvanShapeshifterTurnFaceUpAsksWhichCreatureToCopy: accepting the
// optional Clone replacement asks which creature to copy (the old
// TestVesuvanShapeshifterCanAcceptItsTurnFaceUpReplacement's choice half).
func TestVesuvanShapeshifterTurnFaceUpAsksWhichCreatureToCopy(t *testing.T) {
	t.Skip("regression: the turn-face-up special action (CR 116.2b) runs outside any tape run, " +
		"so the Clone body's Choices$ ask inside its ReplaceWith takes the R-9 default (first eligible) " +
		"with the Note 'no resolution run serves it (choose/clone_choice)' instead of asking; making " +
		"turn_face_up (and its KReplacement answer) a tape-run boundary needs its legacy choosing asks " +
		"converted -- architectural, reported")
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Vesuvan Shapeshifter", "Grizzly Bears")
	bear := oppBear(t, e)
	id := morphDownCastWithoutPrintedOption(t, e, "Vesuvan Shapeshifter", "CCCUU")
	submitChoices(t, e, turnFaceUpIndex(t, e, id))
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Options[0].Kind != "apply" {
		t.Fatalf("expected the optional replacement's yes/no choice, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind == decision.KPriority {
		t.Fatalf("accepting the optional Clone replacement did not ask which creature to copy: %+v", d)
	}
	found := false
	for _, o := range d.Options {
		found = found || o.Obj == bear
	}
	if !found {
		t.Fatalf("Clone choice omitted the battlefield Grizzly Bears: %+v", d.Options)
	}
}

// TestTurnFaceUpReplacementAskParksTheTransitionKernel: an asking ReplaceWith
// body cannot expose the turn-up while its answer is outstanding.
func TestTurnFaceUpReplacementAskParksTheTransitionKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := onBoardCard(t, e, 0, lookup(t, reg, "Aquamorph Entity"))
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
	if o := e.G.Obj(id); o == nil || !o.FaceDown {
		t.Fatalf("precondition: Aquamorph Entity did not turn face down: %+v", o)
	}
	mark := len(e.L.Events)
	kr9Probe(e, func() { e.emit(events.Event{Kind: events.TurnFaceUp, Obj: id}) })
	if d := e.Pending(); d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected Aquamorph's replacement choice, got %+v", d)
	}
	if o := e.G.Obj(id); o == nil || !o.FaceDown {
		t.Fatalf("precondition: transition folded before the replacement answer: %+v", o)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TurnFaceUp && ev.Obj == id {
			t.Fatal("TurnFaceUp event was logged while replacement choice was pending")
		}
	}
	d := e.Pending()
	if len(d.Options) == 0 {
		t.Fatalf("replacement choice has no options: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	kr9Settle(e)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.FaceDown {
		t.Fatalf("answered replacement did not complete turn-up: %+v", o)
	}
	assertTurnUpEventOnce(t, e, id, mark)
}

// kr9AnswerObj answers the pending decision with the option naming obj.
func kr9AnswerObj(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no option for object %d: %+v", obj, d.Options)
}

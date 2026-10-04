package rules

import (
	"strings"
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
		// The copy choice (the sibling below pins it): pick the Bear.
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

// TestAquamorphEntityTurnFaceUpAsksItsMode: the turn-face-up special action
// (CR 116.2b) is a tape-run boundary, so Aquamorph Entity's "as this is
// turned face up" GenericChoice asks which P/T it takes instead of taking the
// first mode by default, and the answer is replayable.
func TestAquamorphEntityTurnFaceUpAsksItsMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label      string
		pow, tough int32
	}{{"5/1", 5, 1}, {"1/5", 1, 5}} {
		t.Run(tc.label, func(t *testing.T) {
			reg := searchTestRegistry(t)
			e, cfg := manifestEngine(t, reg, "Aquamorph Entity")
			id := morphDownCastWithoutPrintedOption(t, e, "Aquamorph Entity", "CCCCCU")
			submitChoices(t, e, turnFaceUpIndex(t, e, id))
			d := e.Pending()
			if d == nil || d.Kind != decision.KModes {
				t.Fatalf("turning Aquamorph Entity face up did not ask its mode: %+v", d)
			}
			pick := -1
			for _, o := range d.Options {
				if strings.Contains(o.Label, tc.label) {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("no %s mode offered: %+v", tc.label, d.Options)
			}
			submitChoices(t, e, pick)
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.FaceDown {
				t.Fatalf("Aquamorph Entity did not turn face up on the battlefield: %+v", o)
			}
			if p, th := e.Power(id), e.Toughness(id); p != tc.pow || th != tc.tough {
				t.Fatalf("Aquamorph Entity is %d/%d, want %d/%d", p, th, tc.pow, tc.tough)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestFaceDownAquamorphEntityEntersWithoutItsModeChoice: a face-down
// permanent has no abilities (CR 708.2), so Aquamorph Entity cast face down
// enters as a 2/2 without being offered its printed "as this enters" mode
// choice.
func TestFaceDownAquamorphEntityEntersWithoutItsModeChoice(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Aquamorph Entity")
	id := searchMoveByName(t, e, "Aquamorph Entity", state.ZHand)
	addMana(t, e, 0, "CCC")
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "morphed" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatal("no face-down cast option for Aquamorph Entity")
	}
	submitChoices(t, e, idx)
	for i := 0; i < 10 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("the face-down cast posed %+v; a face-down permanent has no abilities", d)
		}
		passPriorityOnce(t, e)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("the face-down entry posed %+v; a face-down permanent has no abilities", d)
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("Aquamorph Entity did not enter face down: %+v", o)
	}
	if p, th := e.Power(id), e.Toughness(id); p != 2 || th != 2 {
		t.Fatalf("face-down Aquamorph Entity is %d/%d, want 2/2", p, th)
	}
	replayCheck(t, e, cfg)
}

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestStackSpellTargetOptionUsesSpellKind uses Counterspell's real corpus
// TargetType$ Spell path. A spell already on the stack must be exposed as a
// spell target, not as a battlefield permanent.
func TestStackSpellTargetOptionUsesSpellKind(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Counterspell", "Lightning Bolt")
	counter := searchMoveByName(t, e, "Counterspell", state.ZHand)
	bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)

	// Preconditions: both real corpus cards are distinct cards in hand, and the
	// target card is a face-bearing spell rather than a faceless ability object.
	if counter == 0 || bolt == 0 || counter == bolt {
		t.Fatalf("fixture cards are not distinct: Counterspell=%d Lightning Bolt=%d", counter, bolt)
	}
	counterObj, boltObj := e.G.Obj(counter), e.G.Obj(bolt)
	if counterObj == nil || boltObj == nil || counterObj.Zone != state.ZHand || boltObj.Zone != state.ZHand {
		t.Fatalf("fixture zones: Counterspell=%+v Lightning Bolt=%+v", counterObj, boltObj)
	}
	if boltObj.Face() == nil || boltObj.Ability != nil {
		t.Fatalf("Lightning Bolt is not a face-bearing spell object: %+v", boltObj)
	}

	// Put the real Lightning Bolt on the stack, then ask the real Counterspell
	// to target it. The event is setup, not a state mutation outside events.Apply.
	e.pending = nil
	e.emit(events.Event{Kind: events.PutOnStack, Obj: bolt, Player: 0, From: state.ZHand, To: state.ZStack})
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZStack || len(e.G.Stack) != 1 {
		t.Fatalf("stack precondition: bolt=%+v stack=%v", e.G.Obj(bolt), e.G.Stack)
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 2})
	e.askPriority(0)

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority to cast Counterspell, got %+v", d)
	}
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == counter {
			cast = o.Index
			break
		}
	}
	if cast < 0 {
		t.Fatalf("real Counterspell was not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{cast}}); err != nil {
		t.Fatalf("cast Counterspell: %v", err)
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Counterspell target decision, got %+v", d)
	}
	var target *decision.Option
	for i := range d.Options {
		if d.Options[i].Obj == bolt {
			target = &d.Options[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("Counterspell did not offer the stack Bolt: %+v", d.Options)
	}
	if got := e.stackObjKind(e.G.Obj(bolt)); got != stackSpell {
		t.Fatalf("stack classifier = %v, want spell", got)
	}
	if target.Kind != "spell" {
		t.Fatalf("stack spell option kind = %q, want %q (old battlefield kind was %q)", target.Kind, "spell", "permanent")
	}
}

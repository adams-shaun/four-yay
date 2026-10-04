package rules

// Kernel-era restorations of the tests W3 removed from rakdos_params_reflected_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestChromeMoxReflectsTheImprintedCardColourKernel(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Chrome Mox"))
	mox := e.G.Zone(state.ZHand, 0)[0]
	// Seed the imprint candidate BEFORE the trigger resolves (the mover ask
	// reads the hand at resolution time, so the card must be there first).
	cleric := e.G.AddObject(card(t, "Name:Imprinted Cleric\nManaCost:1 W\nTypes:Creature Cleric\nPT:1/2\nOracle:x\n"), 0)
	// A real move (not SetZone) so the object's Zone field matches the slice
	// the hidden-hand mover ask reads.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cleric.ID, From: state.ZLibrary, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: mox, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	// The optional ask is posed at RESOLUTION (CR 603.5, the trigger sits on
	// the stack until then), so resolve the top and expect the yes/no ask.
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	// The imprint trigger is optional (OptionalDecider$ You): a yes/no ask.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("no optional imprint ask: %+v", d)
	}
	yes := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yes = o.Index
		}
	}
	if yes < 0 {
		t.Fatalf("imprint ask without a yes option: %+v", d.Options)
	}
	submitChoices(t, e, yes)
	// The mover ask (Min 0 -- "you may exile"): pick the Cleric when posed.
	if d = e.Pending(); d != nil && d.Kind == decision.KChoose {
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(cleric.ID); o == nil || o.Zone != state.ZExile || o.ExiledWith != mox {
		t.Fatalf("imprinted card zone=%v exiledWith=%v, want exile/%d", o, o.ExiledWith, mox)
	}
	addMana(t, e, 0, "")
	activateAbilityOf(t, e, mox, "ManaReflected")
	if got := e.G.Players[0].Pool[state.MW]; got != 1 {
		t.Fatalf("pool W=%d, want 1 (the exiled Cleric's colour)", got)
	}
	for _, m := range []int{state.MU, state.MB, state.MR, state.MG, state.MC} {
		if e.G.Players[0].Pool[m] != 0 {
			t.Fatalf("pool slot %d = %d, want 0", m, e.G.Players[0].Pool[m])
		}
	}
	if !e.G.Obj(mox).Tapped {
		t.Fatal("the {T} cost did not tap Chrome Mox")
	}
}

package effects

// ChooseCard's `Choices$ Card.targetedBy` (Forge's Card.targetedBy): the
// chooser picks among the resolving ability's OWN answered targets. It is a
// membership test, not a filter predicate -- the filter tier strips the token
// (conditions.go's stripTargetedByToken) because it has no object to test
// against, so before this fix cardChoices matched nothing and the choose was
// never posed (Trial of Agony's "that player chooses one of those creatures"
// drew no 5 damage). The pool must be exactly the targeted objects, excluding
// an untargeted decoy that otherwise matches the base.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChooseCardTargetedByPoolIsTheAnsweredTargets is the real-corpus
// regression for Trial of Agony's DBChoose.
func TestChooseCardTargetedByPoolIsTheAnsweredTargets(t *testing.T) {
	_, sa := corpusSA(t, "Trial of Agony", "DBChoose")
	if sa.API != "ChooseCard" {
		t.Fatalf("Trial of Agony DBChoose fixture changed: %+v", sa)
	}
	if got := sa.ParamStr(cards.PKChoices); got != "Creature.targetedBy" {
		t.Fatalf("fixture is not the targetedBy shape: Choices$ = %q", got)
	}
	// Precondition: the ask names its chooser through TargetedController, so
	// the seat asked is the targeted creatures' controller, not the caster.
	if got := sa.ParamStr(cards.PKDefined); got != "TargetedController" {
		t.Fatalf("fixture is not the opponent-chooses shape: Defined$ = %q", got)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Trial of Agony\nTypes:Sorcery\nOracle:x\n"), 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZStack})

	// Seat 1 (the opponent) controls the two targeted creatures plus an
	// untargeted decoy that also satisfies `Creature`.
	targetA := controlCreature(t, h, 1, "Serra Angel", "4/4")
	targetB := controlCreature(t, h, 1, "Grizzly Bears", "2/2")
	decoy := controlCreature(t, h, 1, "Savannah Lions", "2/1")

	// Precondition: all three are battlefield creatures under seat 1, so the
	// only thing that can exclude the decoy is the targetedBy membership.
	for _, o := range []*state.Object{targetA, targetB, decoy} {
		if o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("precondition: %s zone=%v controller=%d", o.Face().Name, o.Zone, o.Controller)
		}
	}

	ctx := &Ctx{Controller: 0, Source: src.ID,
		Targets: []state.Target{{Obj: targetA.ID}, {Obj: targetB.ID}}}

	got := cardChoices(h, ctx, sa, 1)
	if len(got) != 2 || got[0].Obj != targetA.ID || got[1].Obj != targetB.ID {
		t.Fatalf("targetedBy pool = %v, want exactly the two answered targets [%d %d] (decoy %d must be excluded)",
			choiceIDs(got), targetA.ID, targetB.ID, decoy.ID)
	}

	// A spell that resolved with no answered targets offers nothing.
	if empty := cardChoices(h, &Ctx{Controller: 0, Source: src.ID}, sa, 1); len(empty) != 0 {
		t.Fatalf("an unanswered targetedBy choice offered %v, want none", choiceIDs(empty))
	}
}

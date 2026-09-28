package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMoraugCountsEachAffectedCreaturesAttacks is the AffectedX count-anchor
// regression for Count$CardNumAttacksThisTurn. Moraug, Fury of Akoum grants
// each creature you control +X/+0 where X is "the number of times IT has
// attacked this turn" (Oracle). The static is `AddPower$ AffectedX` with
// `AffectedX:Count$CardNumAttacksThisTurn`, so the count must read each
// RECIPIENT's Object.AttacksThisTurn -- not Moraug's own attack tally and not
// an aggregate over the team. This test gives two creatures different attack
// counts (0 and 2) and asserts the three-way power split that only the
// per-recipient read produces:
//
//	Moraug (never attacked):            6/6  (its own +0)
//	Grizzly Bears (0 attacks):          2/2  (+0)
//	Llanowar Elves (2 attacks):         1 + 2 = 3 power (1/1 +2/+0)
//
// If the count anchored on Moraug's source (0) every creature would keep its
// base power; if it aggregated the team (2) both pumped creatures would get
// +2 -- neither matches the asserted values.
func TestMoraugCountsEachAffectedCreaturesAttacks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Moraug, Fury of Akoum"),
		mustCorpusCard(t, reg, "Grizzly Bears"),
		mustCorpusCard(t, reg, "Llanowar Elves"),
	}, nil)
	moraug := moveByName(t, e, 0, "Moraug, Fury of Akoum", state.ZBattlefield)
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	elves := moveByName(t, e, 0, "Llanowar Elves", state.ZBattlefield)
	for _, o := range []state.ObjID{moraug, bears, elves} {
		if got := e.G.Obj(o).Zone; got != state.ZBattlefield {
			t.Fatalf("precondition: object %d zone = %s, want battlefield", o, got)
		}
	}
	// Precondition: all three start with no attacks this turn, and Moraug's
	// static is the only pump in play.
	if got := e.G.Obj(moraug).AttacksThisTurn; got != 0 {
		t.Fatalf("precondition: Moraug attacks = %d, want 0", got)
	}
	if got := e.G.Obj(bears).AttacksThisTurn; got != 0 {
		t.Fatalf("precondition: Grizzly Bears attacks = %d, want 0", got)
	}
	if got := e.G.Obj(elves).AttacksThisTurn; got != 0 {
		t.Fatalf("precondition: Llanowar Elves attacks = %d, want 0", got)
	}
	// Declare only Llanowar Elves as an attacker, TWICE (an extra combat
	// re-enters declare-attackers without a TurnChange, so the tally is not
	// reset -- the same shape TestScourgeOfTheThroneFirstAttackOnly uses).
	// Grizzly Bears deliberately stays home, giving the two recipients
	// DIFFERENT counts so a swapped/aggregated anchor cannot pass.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{elves}})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{elves}})
	if got := e.G.Obj(elves).AttacksThisTurn; got != 2 {
		t.Fatalf("precondition: Llanowar Elves attacks = %d, want 2 after two declarations", got)
	}
	if got := e.G.Obj(bears).AttacksThisTurn; got != 0 {
		t.Fatalf("precondition: Grizzly Bears attacks = %d, want 0 (never declared)", got)
	}
	if got := e.G.Obj(moraug).AttacksThisTurn; got != 0 {
		t.Fatalf("precondition: Moraug attacks = %d, want 0 (never declared)", got)
	}
	if got, want := e.Power(moraug), int32(6); got != want {
		t.Fatalf("Moraug power = %d, want %d (its own +0: it never attacked)", got, want)
	}
	if got, want := e.Power(bears), int32(2); got != want {
		t.Fatalf("Grizzly Bears power = %d, want %d (+0: it never attacked)", got, want)
	}
	if got, want := e.Power(elves), int32(3); got != want {
		t.Fatalf("Llanowar Elves power = %d, want %d (1/1 base +2/+0 for two attacks)", got, want)
	}
	if got, want := e.Toughness(elves), int32(1); got != want {
		t.Fatalf("Llanowar Elves toughness = %d, want %d (Moraug adds only +X/+0)", got, want)
	}
}

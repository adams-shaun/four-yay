package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// exiled_cards_count_test.go pins Corpseweft's `SVar:Y:ExiledCards$Amount/Twice`
// count source (effects/count.go refTargets' `ExiledCards` referent). Forge's
// `ExiledCards` amount is the cast-cost PAID list -- the cards the resolving
// cast or activation actually exiled as a cost -- not Object.ExiledCards (a
// ChangeZone zone association) and not Ctx.Remembered (the memory/captured
// trigger objects `TokenRemembered$ ExiledCards` reads). The corpus body is a
// bare head with the shared `/Twice` arithmetic, so this test also pins the
// multiplier the token's X/X comes from.

// TestExiledCardsPaidCount pins the paid-list read and its /Twice op, then the
// zero contrast: an empty paid list is a legitimate modelled zero even when
// unrelated remembered and ExiledWith-associated cards are present.
func TestExiledCardsPaidCount(t *testing.T) {
	h, c := fixtureHost(t)
	src := h.g.Obj(c.Source)
	paidA := h.g.AddObject(mkCard(t, "Name:ExiledPaidA\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	paidB := h.g.AddObject(mkCard(t, "Name:ExiledPaidB\nTypes:Creature\nPT:3/3\nOracle:x\n"), 0).ID
	// Unrelated sets, present in every assertion below so a read of them
	// instead of Ctx.Exiled would be caught: a remembered object (the
	// TokenRemembered$ ExiledCards source) and an object the source exiled
	// through ChangeZone (Object.ExiledCards / ExiledWith).
	memory := h.g.AddObject(mkCard(t, "Name:UnrelatedMemory\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	associated := h.g.AddObject(mkCard(t, "Name:UnrelatedAssociated\nTypes:Creature\nPT:4/4\nOracle:x\n"), 0).ID
	c.Remembered = []state.Target{{Obj: memory}}
	h.g.Obj(associated).ExiledWith = c.Source
	src.ExiledCards = []state.ObjID{associated}
	// Precondition: the paid cards and the unrelated sets are disjoint, and
	// the paid set's size differs from each unrelated set's, so no substitution
	// can produce the numbers asserted.
	if memory == paidA || memory == paidB || associated == paidA || associated == paidB {
		t.Fatal("precondition: paid cards must not be the remembered/associated objects")
	}
	if int32(len(c.Remembered)) == int32(len([]state.ObjID{paidA, paidB})) {
		t.Fatal("precondition: remembered size must differ from the paid size")
	}
	if int32(len(src.ExiledCards)) == int32(len([]state.ObjID{paidA, paidB})) {
		t.Fatal("precondition: associated size must differ from the paid size")
	}

	// Two paid cards: the /Twice body reads 4, the bare amount 2 -- the values
	// Corpseweft's token P/T is set from.
	c.Exiled = []state.ObjID{paidA, paidB}
	if got, ok := EvalCountOK(h, c, "ExiledCards$Amount/Twice"); !ok || got != 4 {
		t.Fatalf("ExiledCards$Amount/Twice with %d paid cards = %d (ok=%v), want 4", len(c.Exiled), got, ok)
	}
	if got, ok := EvalCountOK(h, c, "ExiledCards$Amount"); !ok || got != 2 {
		t.Fatalf("ExiledCards$Amount with %d paid cards = %d (ok=%v), want 2", len(c.Exiled), got, ok)
	}

	// Zero contrast: no paid list, but the remembered and associated cards are
	// still there -- the modelled head must read a legitimate zero (ok=true),
	// never borrow those sets. An unresolvable verdict here would mean the
	// referent was not claimed at all.
	c.Exiled = nil
	if got, ok := EvalCountOK(h, c, "ExiledCards$Amount/Twice"); !ok || got != 0 {
		t.Fatalf("ExiledCards$Amount/Twice without a paid list = %d (ok=%v), want a modelled 0", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "ExiledCards$Amount"); !ok || got != 0 {
		t.Fatalf("ExiledCards$Amount without a paid list = %d (ok=%v), want a modelled 0", got, ok)
	}
}

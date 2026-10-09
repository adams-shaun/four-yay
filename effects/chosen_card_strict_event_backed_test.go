package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// chosen_card_strict_event_backed_test.go pins the trigger-matcher fallback
// in the ChosenCard/ChosenCardStrict/nonChosenCard predicate: a Ctx whose
// ChosenValid is NOT bound (a SpecContext built outside the resolution --
// trigBoard.MatchesObject's, among others) reads the SOURCE OBJECT's
// event-backed chosen list (state.Object.Chosen, the Choose "chosen" fold)
// before failing closed. Without the fallback a trigger-side
// ValidCard$ Card.ChosenCardStrict carrier (Zenos yae Galvus' "when the
// chosen creature leaves the battlefield, transform Zenos yae Galvus")
// never fired. TestSourceScopedSelfExileEndsOnlyEffectRegistrations and
// TestColourSourcePredicatesMatch are the style this follows.

func TestChosenCardStrictReadsEventBackedChoiceWhenCtxUnbound(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Zenos\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	cand := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	other := h.g.AddObject(mkCard(t, "Name:Elf\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{src.ID, cand.ID, other.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}
	// Precondition: the three object ids really are distinct, so the
	// membership walk cannot pass on an id collision.
	if src.ID == cand.ID || cand.ID == other.ID || src.ID == other.ID {
		t.Fatalf("precondition: object ids collide: %d %d %d", src.ID, cand.ID, other.ID)
	}
	// The Choose "chosen" fold (events/apply_cast.go foldChoose) recorded
	// the answer onto the source permanent.
	h.g.Obj(src.ID).Chosen = []state.Target{{Obj: cand.ID}}

	// No Ctx.Chosen bound: the fallback must bind the event-backed choice.
	unbound := &Ctx{Controller: 0, Source: src.ID}
	if !choiceMatches(h, h.g, unbound, "Card.ChosenCardStrict", h.g.Obj(cand.ID)) {
		t.Fatal("the event-backed chosen candidate did not match Card.ChosenCardStrict with no Ctx.Chosen bound")
	}
	if choiceMatches(h, h.g, unbound, "Card.ChosenCardStrict", h.g.Obj(other.ID)) {
		t.Fatal("a candidate outside the event-backed chosen set matched Card.ChosenCardStrict")
	}
	// nonChosenCard under the same unbound Ctx: the negated read over the
	// event-backed set.
	if choiceMatches(h, h.g, unbound, "Card.nonChosenCard", h.g.Obj(cand.ID)) {
		t.Fatal("the chosen candidate matched nonChosenCard over the event-backed set")
	}
	if !choiceMatches(h, h.g, unbound, "Card.nonChosenCard", h.g.Obj(other.ID)) {
		t.Fatal("a candidate outside the event-backed chosen set did not match nonChosenCard")
	}
	// The in-flight path is unchanged: a bound ChosenValid still decides.
	bound := &Ctx{Controller: 0, Source: src.ID, Chosen: []state.Target{{Obj: other.ID}}, ChosenValid: true}
	if !choiceMatches(h, h.g, bound, "Card.ChosenCardStrict", h.g.Obj(other.ID)) {
		t.Fatal("the in-flight chosen candidate did not match Card.ChosenCardStrict with ChosenValid bound")
	}
	if choiceMatches(h, h.g, bound, "Card.ChosenCardStrict", h.g.Obj(cand.ID)) {
		t.Fatal("an event-backed candidate matched a ChosenValid Ctx whose in-flight set names another card")
	}
}

func TestChosenCardStrictStillFailsClosedWithoutAnyChoice(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Zenos\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	cand := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{src.ID, cand.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}
	// Precondition: the source carries NO event-backed chosen list and the
	// Ctx binds none either -- the truly choice-less case that must stay
	// fail closed.
	if got := h.g.Obj(src.ID).Chosen; len(got) != 0 {
		t.Fatalf("precondition: source Chosen = %+v, want empty", got)
	}
	unbound := &Ctx{Controller: 0, Source: src.ID}
	if choiceMatches(h, h.g, unbound, "Card.ChosenCardStrict", h.g.Obj(cand.ID)) {
		t.Fatal("a choice-less source matched Card.ChosenCardStrict; the fail-closed guard was lost")
	}
	if choiceMatches(h, h.g, unbound, "Card.nonChosenCard", h.g.Obj(cand.ID)) {
		t.Fatal("a choice-less source matched nonChosenCard beneath the unbound Ctx; the negated form must stay unchanged for choice-less sources")
	}
}

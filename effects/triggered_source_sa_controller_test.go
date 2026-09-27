package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggeredSourceSAControllerDefinedAndFilter pins the
// TriggeredSourceSAController spelling on both consumers: the shared Defined
// resolver (Leyline of Combustion's DealDamage, Ashenmoor Liege's LoseLife)
// and the two-token ControlledBy/OwnedBy referent grammar (Black Bolt's
// "destroy target nonland permanent that player controls" offer).
//
// The referent names the controller of the CAUSING spell or ability's source,
// never the triggered permanent, never the trigger's own controller, never a
// target's controller -- so every fixture below keeps those three seats
// distinct and asserts the precondition that they are.
func TestTriggeredSourceSAControllerDefinedAndFilter(t *testing.T) {
	h := newHost(t, 2)
	mk := func(controller state.PlayerID) state.ObjID {
		return h.g.AddObject(mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n"), controller).ID
	}
	src := mk(0)    // the resolving ability's source: seat 0
	cause := mk(1)  // the causing spell/ability's source: seat 1
	victim := mk(0) // the triggering event's target: also seat 0
	c := &Ctx{Source: src, Controller: 0,
		TriggerContext: TriggerContext{TriggerSource: cause, TriggerTarget: state.Target{Obj: victim}}}

	// Precondition: the seat the referent must name is distinct from both the
	// trigger's own controller and the event target's controller.
	if got := h.g.Obj(cause).Controller; got == c.Controller || got == h.g.Obj(victim).Controller {
		t.Fatalf("precondition: cause controller %d is not distinct (trigger controller %d, target controller %d)",
			got, c.Controller, h.g.Obj(victim).Controller)
	}

	// Defined: the seat of the causing source's controller.
	for _, form := range []string{"TriggeredSourceSAController", "TriggeredSourceController"} {
		got := Defined(h, c, definedSA(form))
		if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
			t.Errorf("%s = %v, want the causing seat 1", form, got)
		}
	}

	// Role absent: Defined retains the TriggeredSourceController fallback
	// convention (Remembered[0]'s controller); with nothing remembered it is
	// an empty, well-formed answer -- never another seat.
	cNoRole := &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: cause}}}
	got := Defined(h, cNoRole, definedSA("TriggeredSourceSAController"))
	if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
		t.Errorf("role-absent fallback = %v, want Remembered[0]'s controller seat 1", got)
	}
	cEmpty := &Ctx{Source: src, Controller: 0}
	if got := Defined(h, cEmpty, definedSA("TriggeredSourceSAController")); len(got) != 0 {
		t.Errorf("no role and nothing remembered = %v, want empty", got)
	}

	// A captured source object that is gone resolves to nothing -- a deleted
	// referent must not fall back to the trigger's own controller.
	cGone := &Ctx{Source: src, Controller: 0,
		TriggerContext: TriggerContext{TriggerSource: 999999}}
	if got := Defined(h, cGone, definedSA("TriggeredSourceSAController")); len(got) != 0 {
		t.Errorf("gone source = %v, want empty (fail closed)", got)
	}

	// Filter grammar: the referent resolves to a SEAT, so ControlledBy
	// compares the candidate's controller and OwnedBy the candidate's owner
	// against that seat. The board object the trigger captured (theirBig) is
	// OWNED by seat 0 and CONTROLLED by seat 1, and myBear is flipped the
	// other way, so ownership and control can never be confused with each
	// other or with the source's own owner.
	g, ids := board(t)
	causeFilter := ids["theirBig"]
	g.Obj(causeFilter).Owner = 0 // owned by seat 0, controlled by seat 1
	bear := ids["myBear"]
	g.Obj(bear).Owner = 1 // owned by seat 1, controlled by seat 0
	sc := SpecContext{You: 0, Source: ids["myFlier"],
		TriggerContext: TriggerContext{TriggerSource: causeFilter}}
	if !MatchesSpecCtx(g, "Creature.ControlledBy TriggeredSourceSAController", causeFilter, sc) {
		t.Error("a creature controlled by the causing seat did not match ControlledBy")
	}
	if MatchesSpecCtx(g, "Creature.ControlledBy TriggeredSourceSAController", bear, sc) {
		t.Error("a creature owned by the causing seat but controlled elsewhere matched ControlledBy")
	}
	if !MatchesSpecCtx(g, "Creature.OwnedBy TriggeredSourceSAController", bear, sc) {
		t.Error("a creature owned by the causing seat did not match OwnedBy")
	}
	if MatchesSpecCtx(g, "Creature.OwnedBy TriggeredSourceSAController", causeFilter, sc) {
		t.Error("a creature controlled by the causing seat but owned elsewhere matched OwnedBy")
	}
	if unk := UnknownPredicates("Permanent.ControlledBy TriggeredSourceSAController"); len(unk) != 0 {
		t.Errorf("referent reported unknown %v", unk)
	}
	// Bound context, negated form: the negation admits exactly the
	// non-matching candidates.
	if MatchesSpecCtx(g, "Creature.!ControlledBy TriggeredSourceSAController", causeFilter, sc) {
		t.Error("!ControlledBy matched a creature the positive form also matched")
	}
	if !MatchesSpecCtx(g, "Creature.!ControlledBy TriggeredSourceSAController", bear, sc) {
		t.Error("!ControlledBy rejected a creature the positive form rejected")
	}

	// Unbound contexts fail closed in BOTH directions: no role, or a role
	// whose object is gone, must never admit the predicate or its negation.
	for name, tc := range map[string]TriggerContext{
		"no role":     {},
		"gone source": {TriggerSource: 999999},
	} {
		scDead := SpecContext{You: 0, Source: ids["myFlier"], TriggerContext: tc}
		for _, spec := range []string{
			"Creature.ControlledBy TriggeredSourceSAController",
			"Creature.!ControlledBy TriggeredSourceSAController",
		} {
			for _, id := range []state.ObjID{causeFilter, bear} {
				if MatchesSpecCtx(g, spec, id, scDead) {
					t.Errorf("%s: %s matched object %d", name, spec, id)
				}
			}
		}
	}
}

// definedSA builds a one-selector Defined$ SA for the tests above.
func definedSA(defined string) *cards.SA {
	return &cards.SA{Params: map[string]string{"Defined": defined}}
}

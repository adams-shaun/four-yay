package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// grantAbility builds a main-phase priority decision offering exactly one
// "ability" option (source Obj src) that carries the engine-supplied
// decision.Grant no-op flag g, in front of the unavoidable "pass" option —
// the shape legalActions emits when the deciding seat's only worth-thinking-
// about action is one activated ability and the engine has flagged that
// ability's effect. Min/Max 1 is the engine's real priority shape.
func grantAbility(src state.ObjID, lab string, g *decision.Grant) *decision.Decision {
	return &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: src, Label: lab, Grant: g},
			{Index: 1, Kind: "pass"},
		}}
}

// TestA1RedundantKeywordGrantAlreadyNotChosen pins A1's keyword-grant no-op
// (ability.go grantNoOp): an activation whose whole effect is a pure,
// idempotent keyword grant -- and whose granting permanent ALREADY has that
// keyword -- gains nothing the second time, so it is never activated and the
// policy passes instead. This is the resolved-board half of the check: the
// Goblin Balloon Brigade already flew this turn, so "{R}: gains flying" is a
// nil action. The gate deletes grantNoOp's Already read (treats the grant as
// never redundant) and this test fails: the no-op grant is then worth taking
// and chosen over pass.
func TestA1RedundantKeywordGrantAlreadyNotChosen(t *testing.T) {
	// Goblin Balloon Brigade (7) is the seat's own 1/1 already carrying
	// Flying from an earlier resolution this turn.
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{7: {Power: 1, Toughness: 1, Keywords: []string{"Flying"}, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{7: {}}),
	}
	d := grantAbility(7, "Goblin Balloon Brigade: CARDNAME gains flying until end of turn.",
		&decision.Grant{Keywords: []string{"Flying"}, Already: true})
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("already-granted keyword = %s (option %d), want pass — a grant the source already has gains nothing", got, in.Choices[0])
	}
}

// TestA1RedundantKeywordGrantDuplicatePendingNotChosen pins A1's STACK half
// (ability.go grantNoOp): even when the granting permanent does NOT yet have
// the keyword, an identical activation from the same source already on the
// stack unresolved means a second copy adds nothing — the one that resolves
// will grant it. This is the half that actually failed in the reported
// game: all three "{R}: gains flying" activations were pending
// simultaneously, so "does it already have flying" was false each time. The
// gate deletes grantNoOp's Duplicate read and this test fails: the duplicate
// grant is then worth taking and chosen over pass.
func TestA1RedundantKeywordGrantDuplicatePendingNotChosen(t *testing.T) {
	// The brigade is a 1/1 with NO flying yet, but an identical activation
	// from it is already pending on the stack (the reported failure shape).
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{7: {Power: 1, Toughness: 1, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{7: {}}),
	}
	d := grantAbility(7, "Goblin Balloon Brigade: CARDNAME gains flying until end of turn.",
		&decision.Grant{Keywords: []string{"Flying"}, Duplicate: true})
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("duplicate- pending grant = %s (option %d), want pass — a second identical grant adds nothing once one is on the stack", got, in.Choices[0])
	}
}

// TestA1KeywordGrantNotRedundantChosen is grantNoOp's negative half: the
// rule must not disable keyword-granting abilities altogether. A pure
// keyword grant the source does NOT yet have, with no identical one pending,
// really changes the board (the brigade gains flying), so it is worth taking
// and IS chosen. The gate makes grantNoOp return true for every non-nil
// Grant (treats every grant as a no-op) and this test fails: the policy
// passes instead of giving the brigade flying.
func TestA1KeywordGrantNotRedundantChosen(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{7: {Power: 1, Toughness: 1, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{7: {}}),
	}
	d := grantAbility(7, "Goblin Balloon Brigade: CARDNAME gains flying until end of turn.",
		&decision.Grant{Keywords: []string{"Flying"}})
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "ability" {
		t.Fatalf("non-redundant grant = %s, want the ability chosen (the brigade really gains flying)", got)
	}
}

// TestA1AdditiveKeywordGrantStacks is the idempotent/additive distinction
// grantNoOp must not blur: an activation that carries an ADDITIVE component
// (a pump that also changes power) stacks and must stay freely repeatable,
// whatever the engine flags. A nil Grant (additive, or not a keyword grant)
// is never a no-op, so the policy activates it even when the source already
// holds the keyword. The gate extends grantNoOp to treat a nil Grant as a
// no-op and this test fails: the additive pump is suppressed.
func TestA1AdditiveKeywordGrantStacks(t *testing.T) {
	// A +1/+1-and-flying pump on a creature that already flies and
	// already has the +1/+1 resolved: the STAT still stacks, so it is not a
	// no-op even though the keyword half is redundant. The engine gives such
	// an activation a nil Grant (it is not a pure keyword grant).
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{7: {Power: 1, Toughness: 1, Keywords: []string{"Flying"}, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{7: {}}),
	}
	d := grantAbility(7, "Berserk: CARDNAME gains flying and gets +1/+1 until end of turn.", nil)
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "ability" {
		t.Fatalf("additive pump = %s (option %d), want the ability chosen — a nil Grant means the effect stacks and is never a no-op", got, in.Choices[0])
	}
}

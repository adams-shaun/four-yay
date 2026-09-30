package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The Spawner> control-referent chain (spawnercontrol): Forge's
// adjustTriggerContext re-anchor, as the ControlledBy/OwnedBy argument.
// The Motherlode, Excavator's destroy ask
// (Land.nonBasic+ControlledBy Spawner>TriggeredDefendingPlayer) and its
// seven sibling corpus cards are the carriers; before the arm the chain
// classified unknown, the predicate failed closed, and the destroy ask
// offered an empty candidate set -- the paid {E}{E}{E}{E} did nothing.

func TestSpawnerChainControlReferent(t *testing.T) {
	g, ids := board(t)
	bound := SpecContext{TriggerContext: TriggerContext{
		TriggerTarget:   state.Target{Obj: ids["theirBig"]},
		DefendingPlayer: state.Target{IsPlayer: true, Player: 1},
	}}
	// A bound TriggerTarget (seat 1's Giant) makes the chain name seat 1:
	// only seat 1's creature matches, under both ops. Neither the live game
	// nor its objects are mutated here.
	for _, op := range []string{"ControlledBy", "OwnedBy"} {
		spec := "Creature." + op + " Spawner>TriggeredTarget"
		if unknown := UnknownPredicates(spec); len(unknown) != 0 {
			t.Fatalf("%s reported unknown %v", spec, unknown)
		}
		if !MatchesSpecCtx(g, spec, ids["theirBig"], bound) {
			t.Errorf("%s did not match the triggering target's controller", spec)
		}
		if MatchesSpecCtx(g, spec, ids["myBear"], bound) {
			t.Errorf("%s matched seat 0's bear", spec)
		}
	}
	// The defending-player inner ref names seat 1 by role: The Motherlode's
	// exact corpus shape.
	defending := "Creature.ControlledBy Spawner>TriggeredDefendingPlayer"
	if unknown := UnknownPredicates(defending); len(unknown) != 0 {
		t.Fatalf("%s reported unknown %v", defending, unknown)
	}
	if !MatchesSpecCtx(g, defending, ids["theirBig"], bound) {
		t.Error("defending-player chain did not name seat 1")
	}
	if MatchesSpecCtx(g, defending, ids["myBear"], bound) {
		t.Error("defending-player chain matched seat 0")
	}
	// Unbound context: the recognised chain fails closed, positive AND
	// negated -- an absent binding must never match by negation.
	unbound := SpecContext{You: 0}
	for _, spec := range []string{
		"Creature.ControlledBy Spawner>TriggeredTarget",
		"Creature.!ControlledBy Spawner>TriggeredTarget",
		"Creature.!ControlledBy Spawner>TriggeredDefendingPlayer",
	} {
		for _, id := range []state.ObjID{ids["myBear"], ids["theirBig"]} {
			if MatchesSpecCtx(g, spec, id, unbound) {
				t.Errorf("unbound context matched %s object %d", spec, id)
			}
		}
	}
	// A chain over an inner ref the grammar does not know stays unknown,
	// reported as the whole two-token predicate.
	if got := UnknownPredicates("Creature.ControlledBy Spawner>TriggeredTargetController"); len(got) == 0 || got[0] != "ControlledBy Spawner>TriggeredTargetController" {
		t.Errorf("unknown inner ref reported %v, want the whole token", got)
	}
}

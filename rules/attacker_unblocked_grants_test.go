package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFrenzySliverGrantedAttackerUnblockedPumps verifies the AddTrigger$ path
// of Mode$ AttackerUnblocked with the real Frenzy Sliver. Its Continuous
// static grants the trigger to every Sliver, including itself; the dedicated
// round-complete hook must queue that grant through GrantTriggerPush, then the
// resolved body must pump the captured unblocked attacker.
func TestFrenzySliverGrantedAttackerUnblockedPumps(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	frenzy := onBoardCard(t, e, 0, unblockedCorpusCard(t, "f/frenzy_sliver.txt"))
	e.G.Obj(frenzy).SummonSick = false
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	// Preconditions: the grant recipient is a live 1/1 Sliver on the
	// battlefield, so the observed +1/+0 cannot be a setup artefact.
	if o := e.G.Obj(frenzy); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Frenzy Sliver battlefield precondition: %+v", o)
	}
	if e.Power(frenzy) != 1 || e.Toughness(frenzy) != 1 {
		t.Fatalf("Frenzy Sliver precondition = %d/%d, want 1/1", e.Power(frenzy), e.Toughness(frenzy))
	}

	e.askAttackers()
	submitAttackersOnly(t, e, frenzy)
	drainCombatPriority(t, e)
	if !e.G.Obj(frenzy).IsAttacking {
		t.Fatal("Frenzy Sliver attacker precondition: it did not attack")
	}
	declareUnblocked(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("Frenzy Sliver's granted AttackerUnblocked trigger stack=%d, want 1", len(e.G.Stack))
	}
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 1 {
		t.Fatalf("GrantTriggerPush events = %d, want 1", n)
	}
	if tc := e.triggerContexts[e.G.Stack[0]]; tc.TriggerCard != frenzy || tc.TriggerSource != frenzy {
		t.Fatalf("granted trigger captured card/source = %d/%d, want Frenzy Sliver %d", tc.TriggerCard, tc.TriggerSource, frenzy)
	}

	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		eng.resolveTop()
		if eng.Power(frenzy) != 2 || eng.Toughness(frenzy) != 1 {
			t.Fatalf("Frenzy Sliver after granted trigger = %d/%d, want 2/1", eng.Power(frenzy), eng.Toughness(frenzy))
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across Frenzy Sliver's GrantTriggerPush resolution")
	}
}

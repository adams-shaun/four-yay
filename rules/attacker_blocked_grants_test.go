package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the AddTrigger$ half of the become-blocked walk (the
// "AttackerBlocked misses AddTrigger$-granted instances" row). Neither
// Mode$ AttackerBlocked nor Mode$ AttackerBlockedByCreature has an entry in
// trigMatchers -- both are dedicated hooks -- so the ordinary granted-trigger
// matcher rejects every DeclareBlockers event for them and a granted instance
// used to never fire. Both real corpus carriers named below drive the fix:
// Retaliation's Affected$ Creature.YouCtrl grant of an AttackerBlockedByCreature
// trigger, and Stormsurge Kraken's self grant of an AttackerBlocked trigger.

// TestGrantedAttackerBlockedByCreaturePumps pins the pair-mode grant: the
// real Retaliation grants every creature you control "Whenever this creature
// becomes blocked by a creature, this creature gets +1/+1 until end of turn."
// A granted creature that becomes blocked must pump.
func TestGrantedAttackerBlockedByCreaturePumps(t *testing.T) {
	t.Parallel()
	retaliation := mshCorpusCardPath(t, "Retaliation", "r/retaliation.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, retaliation) // the grant's source; never attacks here
	bear := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	// Preconditions: the grant recipient is a live 2/2 with NO printed
	// become-blocked trigger, so the observed +1/+1 cannot be a printed line
	// and must come from the grant. The two P/T values under assertion
	// (2/2 before, 3/3 after) genuinely differ.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Retaliation grant recipient battlefield precondition: %+v", o)
	}
	if e.Power(bear) != 2 || e.Toughness(bear) != 2 {
		t.Fatalf("grant recipient precondition = %d/%d, want 2/2", e.Power(bear), e.Toughness(bear))
	}
	for _, tr := range e.G.Obj(bear).Face().Triggers {
		if tr.Mode == "AttackerBlocked" || tr.Mode == "AttackerBlockedByCreature" {
			t.Fatalf("grant recipient prints %s; the scenario is not the granted path", tr.Mode)
		}
	}

	e.askAttackers()
	submitAttackersOnly(t, e, bear)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, blk)

	// The granted trigger must have been queued through GrantTriggerPush (the
	// replayable-grant path), not a TriggerPush keyed to a face index.
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 1 {
		t.Fatalf("Retaliation granted AttackerBlockedByCreature GrantTriggerPush events = %d, want 1", n)
	}

	// The cloned engine must reproduce the same event stream: the body is an
	// SVar-named grant resolved from the grantor's table during Apply.
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		eng.resolveTop()
		if eng.Power(bear) != 3 || eng.Toughness(bear) != 3 {
			t.Fatalf("granted block pump = %d/%d, want 3/3", eng.Power(bear), eng.Toughness(bear))
		}
	}
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("grant recipient zone = %s, want battlefield", got)
	}
}

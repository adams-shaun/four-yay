package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGrantedAttackerBlockedDrawsKernel pins the per-attacker-mode grant:
// Stormsurge Kraken's Lieutenant static grants itself "Whenever CARDNAME
// becomes blocked, you may draw two cards" while you control your commander.
// The granted AttackerBlocked instance fires once for the blocked Kraken, its
// may-draw asks yes/no, and yes draws two.
func TestGrantedAttackerBlockedDrawsKernel(t *testing.T) {
	t.Parallel()
	kraken := mshCorpusCardPath(t, "Stormsurge Kraken", "s/stormsurge_kraken.txt")
	e := combatEngine(t)
	krakenID := onBoardCard(t, e, 0, kraken)
	e.G.Obj(krakenID).SummonSick = false
	cmd := onBoardReady(t, e, 0, "Name:Test Commander\nManaCost:2 G\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	e.G.Players[0].Commanders = []state.ObjID{cmd}
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	if e.Power(krakenID) != 7 || e.Toughness(krakenID) != 7 {
		t.Fatalf("Kraken precondition = %d/%d, want 7/7", e.Power(krakenID), e.Toughness(krakenID))
	}
	for _, tr := range e.G.Obj(krakenID).Face().Triggers {
		if tr.Mode == "AttackerBlocked" || tr.Mode == "AttackerBlockedByCreature" {
			t.Fatalf("Kraken prints %s; the scenario is not the granted path", tr.Mode)
		}
	}
	beforeHand := len(e.G.Zone(state.ZHand, 0))

	e.askAttackers()
	submitAttackersOnly(t, e, krakenID)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, blk)
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 1 {
		t.Fatalf("granted AttackerBlocked GrantTriggerPush events = %d, want 1", n)
	}
	e.pending = nil
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.Options[0].Kind != "yes" {
		t.Fatalf("expected the granted trigger's may-draw yes/no ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	if afterHand := len(e.G.Zone(state.ZHand, 0)); afterHand != beforeHand+2 {
		t.Fatalf("hand size after the granted draw = %d, want %d", afterHand, beforeHand+2)
	}
}

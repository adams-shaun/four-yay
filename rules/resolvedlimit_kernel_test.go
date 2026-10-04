package rules

// Kernel-era restorations of resolvedlimit_test.go's accepted-resolution
// leaves (ResolvedLimit$, "Do this only once each turn."): the first accepted
// resolution spends the per-turn limit, a mandatory line without the param
// does not, and the count survives Engine.Clone. The helpers (enterCreature,
// drainQueuedTrigger, putInHand) are resolvedlimit_test.go's; direct
// resolveTop calls run as kernel probes (probe_test.go).

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestResolvedLimitBaronStruckerOnlyOncePerTurn is the reported face: the
// first Villain entering queues and resolves Baron's optional connive (answered
// YES), a SECOND Villain in the same turn must queue no second trigger, and a
// third Villain after the turn changes queues again.
func TestKr8ResolvedLimitBaronStruckerOnlyOncePerTurn(t *testing.T) {
	t.Parallel()
	e, baron := baronStruckerFixture(t)

	first := enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	if n := drainQueuedTrigger(t, e, baron); n != 1 {
		t.Fatalf("first accepted Villain produced %d TriggerPush events, want 1", n)
	}
	_ = first

	// Second Villain, same turn: the resolution limit is spent, so no trigger
	// may queue. Assert on queueing, not on the connive outcome (api:Connive
	// is unimplemented and emits an unimplemented-API Note).
	enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second Villain in the same turn queued %d triggers, want 0", len(e.pendingTriggers))
	}
	if n := pushCount(e, baron); n != 1 {
		t.Fatalf("after the second Villain, Baron has %d TriggerPush events, want 1", n)
	}

	// Next turn: the per-turn count resets, so a third Villain queues again.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	if n := drainQueuedTrigger(t, e, baron); n != 2 {
		t.Fatalf("third Villain next turn produced %d total TriggerPush events, want 2", n)
	}
}

// TestResolvedLimitMixedLineOtherTriggerDoesNotConsume pins the round-2
// defect's exact shape on the real mixed-line carrier Cosmic Crucible: line 1
// is a MANDATORY Main1 Phase trigger carrying NO ResolvedLimit$ ("add four
// mana"), line 2 is the optional SpellCast copy trigger that does. Resolving
// line 1 must not consume line 2's limit -- under the round-2 code the copy
// trigger was dead EVERY turn, because the Main1 trigger resolved at the start
// of every turn. After an ACCEPTED line-2 resolution the limit binds normally.
func TestKr8ResolvedLimitMixedLineOtherTriggerDoesNotConsume(t *testing.T) {
	t.Parallel()
	crucible := mshCorpusCardPath(t, "Cosmic Crucible", "c/cosmic_crucible.txt")
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, crucible)

	// Line 1 (mandatory, no ResolvedLimit$): Main1 begins, the mana trigger
	// queues and resolves. Its Combo Any allocation is answered by
	// drainTriggerAsks.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("Cosmic Crucible's Main1 trigger did not queue")
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.resolveTop()
	drainTriggerAsks(t, e, 10)
	if n := pushCount(e, src); n != 1 {
		t.Fatalf("Main1 trigger produced %d TriggerPush events, want 1", n)
	}

	// Line 2 (SpellCast, ResolvedLimit$ 1): cast a noncreature spell. The
	// mandatory line-1 resolution must NOT have spent line 2's limit.
	spell := putInHand(t, e, 0, card(t, "Name:Test Charm\nManaCost:1 U\nTypes:Instant\nOracle:x\n"))
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell, Player: 0, From: state.ZHand, To: state.ZStack})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("the non-RL Main1 resolution consumed the copy trigger's ResolvedLimit; it must still queue")
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.resolveTop()
	// Accept the copy. The probe-driven resolution completes with its
	// priority event; the spell and its copy stay on the stack, which is
	// all the limit needs (no priority round is driven here).
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the copy trigger's optional ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	copied := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy && ev.Obj == spell {
			copied = true
		}
	}
	if !copied {
		t.Fatal("the accepted copy trigger made no copy")
	}
	if n := pushCount(e, src); n != 2 {
		t.Fatalf("accepted copy trigger produced %d TriggerPush events, want 2", n)
	}

	// The accepted line-2 resolution consumed ITS OWN limit: a second cast in
	// the same turn queues nothing (line 1 resolving again is a different
	// turn boundary away, but line 2 is spent for this turn).
	spell2 := putInHand(t, e, 0, card(t, "Name:Test Charm Two\nManaCost:1 U\nTypes:Instant\nOracle:x\n"))
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell2, Player: 0, From: state.ZHand, To: state.ZStack})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second cast after an accepted copy resolution queued %d triggers, want 0", len(e.pendingTriggers))
	}
	if n := pushCount(e, src); n != 2 {
		t.Fatalf("after the second cast, Crucible has %d TriggerPush events, want 2", n)
	}
}

// TestResolvedLimitTidusCheerOncePerTurn is the brief's own "Done means"
// corpus-card leaf: Tidus, Yuna's Guardian's Cheer trigger
// (`T:Mode$ DamageDoneOnce | CombatDamage$ True | ValidSource$
// Creature.YouCtrl+HasCounters | ValidTarget$ Player | ResolvedLimit$ 1 |
// OptionalDecider$ You` -- "you may draw a card and proliferate. Do this
// only once each turn.") is a DamageDoneOnce carrier, the mode whose own
// once-per-batch latch (damageBatchKey) is per damaged object per batch and
// therefore did NOT stop a second SEPARATE combat damage event in the same
// turn from firing the cheer again. Two separate combat damage events in one
// turn must yield exactly one cheer resolution; the turn change re-arms it.
func TestKr8ResolvedLimitTidusCheerOncePerTurn(t *testing.T) {
	t.Parallel()
	tidus := mshCorpusCardPath(t, "Tidus, Yuna's Guardian", "t/tidus_yunas_guardian.txt")
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, tidus)
	// The cheer's ValidSource$ is Creature.YouCtrl+HasCounters, so the bearer
	// needs a counter: place it and mark it with one eventlessly (the same
	// eventless placement onBoardCard uses for the zone move itself).
	bearer := onBoardCard(t, e, 0, card(t, "Name:Counter Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	e.G.Obj(bearer).AddCounter("P1P1", 1)
	// Precondition the whole leaf depends on: the bearer really does carry a
	// counter and really is a creature, so the trigger's ValidSource$ can
	// match it (a mismatched setup would make the test pass vacuously).
	if n := e.G.Obj(bearer).Counter("P1P1"); n != 1 {
		t.Fatalf("precondition: bearer has %d P1P1 counters, want 1", n)
	}
	if !e.G.Obj(bearer).Face().IsCreature() {
		t.Fatal("precondition: bearer is not a creature")
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition: %d triggers queued before any damage", len(e.pendingTriggers))
	}

	base := countDraws(e, 0)
	// First combat damage event of the turn: the bearer hits player 1. The
	// e.damaging/combatDamaging state is exactly what dealCombatDamage emits
	// each assignment under (the TestScreamingNemesisFiresPerDamageEvent
	// shape).
	combatHit := func() {
		e.damaging = bearer
		e.combatDamaging = true
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
		e.combatDamaging = false
		e.damaging = 0
	}
	combatHit()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("first combat hit queued %d triggers, want 1", len(e.pendingTriggers))
	}
	drainQueuedTrigger(t, e, src)
	if n := pushCount(e, src); n != 1 {
		t.Fatalf("first cheer produced %d TriggerPush events, want 1", n)
	}
	if n := countDraws(e, 0) - base; n != 1 {
		t.Fatalf("first accepted cheer drew %d cards, want 1", n)
	}

	// Second SEPARATE combat damage event, same turn. DamageDoneOnce's own
	// latch is per batch, so before the ResolvedLimit$ gate this queued a
	// second cheer; now the per-turn resolution cap must suppress it.
	combatHit()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second combat hit in the same turn queued %d triggers, want 0 (ResolvedLimit$ spent)", len(e.pendingTriggers))
	}
	if n := pushCount(e, src); n != 1 {
		t.Fatalf("after the second hit, Tidus has %d TriggerPush events, want 1", n)
	}

	// New turn: the per-turn count self-resets, so a third hit cheers again.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	combatHit()
	if n := drainQueuedTrigger(t, e, src); n != 2 {
		t.Fatalf("third hit on the next turn produced %d total TriggerPush events, want 2", n)
	}
	if n := countDraws(e, 0) - base; n != 2 {
		t.Fatalf("two accepted cheers drew %d cards total, want 2", n)
	}
}

// TestResolvedLimitReplaysExactly clones the engine after the first resolution
// and proves the clone's continued play is byte-identical -- the leaf that
// catches a missing Engine.Clone copy of triggerTurnResolved (the clone would
// re-fire where the original is silent).
func TestKr8ResolvedLimitReplaysExactly(t *testing.T) {
	t.Parallel()
	e, baron := baronStruckerFixture(t)
	enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	drainQueuedTrigger(t, e, baron)

	// Pre-place the later Villains BEFORE cloning: adding an object advances
	// the engine's own NextID, so an AddObject on each branch would genuinely
	// diverge the two games for a reason unrelated to the ResolvedLimit map.
	second := putInHand(t, e, 0, card(t, resolvedLimitVillain))
	third := putInHand(t, e, 0, card(t, resolvedLimitVillain))

	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		eng.emit(events.Event{Kind: events.MoveZone, Obj: second, From: state.ZHand, To: state.ZBattlefield})
		if len(eng.pendingTriggers) != 0 {
			t.Fatalf("second Villain queued %d triggers, want 0", len(eng.pendingTriggers))
		}
		eng.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
		eng.emit(events.Event{Kind: events.MoveZone, Obj: third, From: state.ZHand, To: state.ZBattlefield})
		if len(eng.pendingTriggers) == 0 {
			t.Fatal("third Villain next turn did not queue")
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the ResolvedLimit turn")
	}
	// Game.Clone's `c.Stack = append([]ObjID(nil), g.Stack...)` collapses an
	// empty-but-non-nil Stack (the shape a resolution's MoveZone leaves behind)
	// to nil, a pre-existing representation quirk unrelated to this map. Both
	// engines played the identical event stream (Asserted above), so normalise
	// the shape and compare the rest field for field.
	if len(e.G.Stack) == 0 {
		e.G.Stack = nil
	}
	if len(clone.G.Stack) == 0 {
		clone.G.Stack = nil
	}
	if !reflect.DeepEqual(e.G, clone.G) {
		t.Fatal("clone game state diverged across the ResolvedLimit turn")
	}
}

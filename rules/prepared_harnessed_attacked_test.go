package rules

// The IsPrepared, harnessed and attackedThisCombat filter predicates: the
// three words effects.UnknownPredicates used to report, which made every
// condition that read them fail closed (Paradox Shaper / Stingerquill
// Voxmancer / Woodwork Prodigy's upkeep gate, The Mind Stone / The Soul
// Stone's infinity trigger, Tolsimir's Wolf trigger). Each test drives the
// REAL corpus card, and each asserts the state the predicate reads moved
// through the event fold, never by writing the field.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// phaseTriggersQueued emits a step change and returns how many triggers it
// queued; the pending list is cleared first so earlier steps do not count.
func phaseTriggersQueued(e *Engine, step state.Step) int {
	e.pending = nil
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.StepChange, Step: step})
	return len(e.pendingTriggers)
}

func TestPredicatesAreNoLongerUnknown(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"Card.!IsPrepared", "Card.IsPrepared", "Card.Self+harnessed", "Card.Self+attackedThisCombat",
	} {
		if un := effects.UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want none", spec, un)
		}
	}
}

// TestPreparedUpkeepGateIsTrueOnlyWhileUnprepared pins CheckSVar$ Count$ValidSelf
// Card.!IsPrepared on the real Paradox Shaper (and its two siblings): the
// upkeep trigger queues while the creature is unprepared, resolves into the
// prepared designation, and does not queue again while it stays prepared.
func TestPreparedUpkeepGateIsTrueOnlyWhileUnprepared(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Paradox Shaper", "Stingerquill Voxmancer", "Woodwork Prodigy"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from the corpus", name)
		}
		e := combatEngine(t)
		id := onBoardCard(t, e, 0, c)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || !o.HasPrepareSpell() {
			t.Fatalf("%s precondition: on battlefield with a prepare spell (zone %v)", name, o.Zone)
		}
		if o.Prepared {
			t.Fatalf("%s precondition: starts unprepared", name)
		}
		if !effects.MatchesObjectCtx(e.G, "Card.!IsPrepared", o, effects.SpecContext{}) ||
			effects.MatchesObjectCtx(e.G, "Card.IsPrepared", o, effects.SpecContext{}) {
			t.Fatalf("%s: unprepared object must match Card.!IsPrepared only", name)
		}
		if n := phaseTriggersQueued(e, state.StepUpkeep); n != 1 {
			t.Fatalf("%s: upkeep queued %d triggers while unprepared, want 1", name, n)
		}
		e.putTriggersOnStack()
		e.resolveTop()
		if !e.G.Obj(id).Prepared {
			t.Fatalf("%s: the upkeep trigger did not prepare it", name)
		}
		if effects.MatchesObjectCtx(e.G, "Card.!IsPrepared", e.G.Obj(id), effects.SpecContext{}) ||
			!effects.MatchesObjectCtx(e.G, "Card.IsPrepared", e.G.Obj(id), effects.SpecContext{}) {
			t.Fatalf("%s: prepared object must match Card.IsPrepared only", name)
		}
		if n := phaseTriggersQueued(e, state.StepUpkeep); n != 0 {
			t.Fatalf("%s: upkeep queued %d triggers while already prepared, want 0", name, n)
		}
		// An unprepare effect (Activate$ False) makes the gate true again.
		e.emit(events.Event{Kind: events.AlterAttribute, Obj: id, Text: "Prepared", Amount: -1})
		if n := phaseTriggersQueued(e, state.StepUpkeep); n != 1 {
			t.Fatalf("%s: upkeep queued %d triggers after unprepare, want 1", name, n)
		}
	}
}

// TestHarnessedGatesTheInfinityTrigger drives the real The Mind Stone and The
// Soul Stone: the infinity trigger (`IsPresent$ Card.Self+harnessed`) is
// absent until the real Harness ability resolves, then queues.
func TestHarnessedGatesTheInfinityTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		step state.Step
	}{
		{"The Mind Stone", state.StepEnd},
		{"The Soul Stone", state.StepUpkeep},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s missing from the corpus", tc.name)
		}
		e := combatEngine(t)
		id := onBoardCard(t, e, 0, c)
		if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Harnessed {
			t.Fatalf("%s precondition: on battlefield, not harnessed", tc.name)
		}
		harness := -1
		for i, sa := range c.Faces[0].Abilities {
			if sa.API == "AlterAttribute" {
				harness = i
			}
		}
		if harness < 0 {
			t.Fatalf("%s has no AlterAttribute ability", tc.name)
		}
		if n := phaseTriggersQueued(e, tc.step); n != 0 {
			t.Fatalf("%s: infinity trigger queued %d while not harnessed, want 0", tc.name, n)
		}
		e.emit(events.Event{Kind: events.AbilityPush, Obj: id, Player: 0, Amount: int32(harness)})
		e.resolveTop()
		if !e.G.Obj(id).Harnessed {
			t.Fatalf("%s: resolving the Harness ability did not harness it (notes: %v)", tc.name, notesOf(e))
		}
		if !effects.MatchesObjectCtx(e.G, "Card.Self+harnessed", e.G.Obj(id), effects.SpecContext{Source: id}) {
			t.Fatalf("%s: harnessed object does not match Card.Self+harnessed", tc.name)
		}
		if n := phaseTriggersQueued(e, tc.step); n != 1 {
			t.Fatalf("%s: infinity trigger queued %d once harnessed, want 1", tc.name, n)
		}
		// The designation ends when the permanent leaves the battlefield.
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
		if e.G.Obj(id).Harnessed {
			t.Fatalf("%s: still harnessed after leaving the battlefield", tc.name)
		}
	}
}

func notesOf(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestAttackedThisCombatGatesTolsimirsWolfTrigger drives the real Tolsimir,
// Midnight's Light: a Wolf attacking WITH Tolsimir queues the force-block
// trigger, a Wolf attacking alone does not, and the stamp lapses with the
// combat.
func TestAttackedThisCombatGatesTolsimirsWolfTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Tolsimir, Midnight's Light")
	if !ok {
		t.Fatal("Tolsimir missing from the corpus")
	}
	setup := func() (*Engine, state.ObjID, state.ObjID) {
		e := combatEngine(t)
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
		tol := onBoardCard(t, e, 0, c)
		wolf := onBoardReady(t, e, 0, "Name:Test Wolf\nManaCost:2 G\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")
		onBoardReady(t, e, 1, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if e.G.Obj(tol).Zone != state.ZBattlefield || e.G.Obj(wolf).Zone != state.ZBattlefield {
			t.Fatal("precondition: Tolsimir and the Wolf are on the battlefield")
		}
		return e, tol, wolf
	}
	match := func(e *Engine, tol state.ObjID) bool {
		return effects.MatchesObjectCtx(e.G, "Card.Self+attackedThisCombat", e.G.Obj(tol), effects.SpecContext{Source: tol})
	}

	e, tol, wolf := setup()
	if match(e, tol) {
		t.Fatal("Tolsimir matches attackedThisCombat before attacking")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{tol, wolf}})
	if !match(e, tol) {
		t.Fatal("Tolsimir does not match attackedThisCombat after attacking")
	}
	if n := len(e.pendingTriggers); n != 1 {
		t.Fatalf("Wolf attacking with Tolsimir queued %d triggers, want 1", n)
	}
	// "This combat": a later combat phase this turn, or the next turn, no
	// longer matches. Advance through the event fold that owns the combat clock.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	if match(e, tol) {
		t.Fatal("attackedThisCombat survived into a later combat phase")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if match(e, tol) {
		t.Fatal("attackedThisCombat survived the turn boundary")
	}

	e, tol, wolf = setup()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{wolf}})
	if match(e, tol) {
		t.Fatal("Tolsimir matches attackedThisCombat though only the Wolf attacked")
	}
	if n := len(e.pendingTriggers); n != 0 {
		t.Fatalf("Wolf attacking alone queued %d triggers, want 0", n)
	}
}

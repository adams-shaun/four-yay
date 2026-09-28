package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Coveted Jewel's downside trigger, Mode$ AttackerUnblockedOnce, pinned end to
// end on the real corpus card (task trigunblk1). The trigger is a dedicated
// hook (checkAttackerUnblockedOnceTriggers) run at the declare-blockers ROUND
// COMPLETE instant; these fixtures drive the real combat engine to get there.

// jewelAttackFixture builds combatEngine with Coveted Jewel under seat 1 (the
// defender), a ready attacker for seat 0, and a spare seat-1 creature so the
// declare-blockers step actually poses a KBlockers decision an empty intent
// can answer. The attacker is declared; the caller decides block/no-block.
func jewelAttackFixture(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	jewel := mshCorpusCardPath(t, "Coveted Jewel", "c/coveted_jewel.txt")
	e := combatEngine(t)
	jewelID := onBoardCard(t, e, 1, jewel)
	// The Jewel enters tapped so the trigger's Untap$ True has observable
	// work to do; an untapped start would make the untap assertion vacuous.
	e.emit(events.Event{Kind: events.Tap, Obj: jewelID})
	// A blocker candidate so seat 1 is offered a KBlockers decision; an
	// EMPTY intent is a real declaration (handleBlockers' own comment).
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	attacker := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.askAttackers()
	return e, jewelID, attacker
}

// declareUnblocked submits the empty blockers intent for the pending
// KBlockers decision, leaving every attacker unblocked.
func declareUnblocked(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("submit empty blockers: %v", err)
	}
}

// TestCovetedJewelUnblockedAttackHandsItOver: an unblocked attack against the
// Jewel's controller fires the trigger; seat 0's hand grows by exactly three,
// the Jewel is under seat 0's control and untapped.
func TestCovetedJewelUnblockedAttackHandsItOver(t *testing.T) {
	t.Parallel()
	e, jewelID, attacker := jewelAttackFixture(t)
	submitAttackersOnly(t, e, attacker)
	drainCombatPriority(t, e)

	handBefore := len(e.G.Zone(state.ZHand, 0))
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		declareUnblocked(t, eng)
		if len(eng.G.Stack) != 1 {
			t.Fatalf("stack depth after the declare-blockers round = %d, want 1 (the trigger)", len(eng.G.Stack))
		}
		// Non-vacuity guard: the Jewel is still tapped with the trigger on the
		// stack, so the untap assertion below can only pass because the
		// trigger's Untap$ True fired.
		if !eng.G.Obj(jewelID).Tapped {
			t.Fatal("Coveted Jewel was already untapped before the trigger resolved")
		}
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the AttackerUnblockedOnce resolution")
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - handBefore; got != 3 {
		t.Fatalf("attacking player drew %d cards, want 3", got)
	}
	jo := e.G.Obj(jewelID)
	if jo.Controller != 0 {
		t.Fatalf("Coveted Jewel controller = %d, want 0 (the attacking player)", jo.Controller)
	}
	if jo.Tapped {
		t.Fatal("Coveted Jewel is still tapped; Untap$ True should have untapped it")
	}
}

// TestCovetedJewelBlockedAttackDoesNothing: a declared blocker leaves no
// unblocked attacker, so the hook queues nothing -- no stack entry, no draw,
// and the Jewel stays with seat 1.
func TestCovetedJewelBlockedAttackDoesNothing(t *testing.T) {
	t.Parallel()
	e, jewelID, attacker := jewelAttackFixture(t)
	submitAttackersOnly(t, e, attacker)
	drainCombatPriority(t, e)

	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	var blocker state.ObjID
	for _, o := range d.Options {
		if o.Obj != 0 && o.Obj != attacker {
			blocker = o.Obj
			break
		}
	}
	if blocker == 0 {
		t.Fatalf("no blocker option: %+v", d.Options)
	}
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		submitBlockersOnly(t, eng, blocker)
		if len(eng.G.Stack) != 0 {
			t.Fatalf("a blocked attack left %d stack entries, want 0", len(eng.G.Stack))
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the blocked declaration")
	}
	if got := e.G.Obj(jewelID).Controller; got != 1 {
		t.Fatalf("Coveted Jewel controller = %d, want 1 (blocked attack fires nothing)", got)
	}
}

// TestCovetedJewelOneTriggerPerCombatNotPerAttacker: two unblocked attackers
// are ONE trigger instance -- 3 cards drawn, not 6 -- and one resolution.
func TestCovetedJewelOneTriggerPerCombatNotPerAttacker(t *testing.T) {
	t.Parallel()
	jewel := mshCorpusCardPath(t, "Coveted Jewel", "c/coveted_jewel.txt")
	e := combatEngine(t)
	jewelID := onBoardCard(t, e, 1, jewel)
	e.emit(events.Event{Kind: events.Tap, Obj: jewelID})
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	a1 := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a2 := onBoardReady(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.askAttackers()
	submitAttackersOnly(t, e, a1, a2)
	drainCombatPriority(t, e)

	handBefore := len(e.G.Zone(state.ZHand, 0))
	declareUnblocked(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("two unblocked attackers queued %d stack entries, want 1 trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)) - handBefore; got != 3 {
		t.Fatalf("attacking player drew %d cards, want 3 (one trigger, not one per attacker)", got)
	}
	if got := e.G.Obj(jewelID).Controller; got != 0 {
		t.Fatalf("Coveted Jewel controller = %d, want 0", got)
	}
}

// TestCovetedJewelOncePerCombatNotAgainThisCombat: after the trigger resolves,
// re-running the round-complete hook in the SAME combat queues nothing; a new
// (Turn, CombatsThisTurn) stamp re-arms it.
func TestCovetedJewelOncePerCombatNotAgainThisCombat(t *testing.T) {
	t.Parallel()
	e, jewelID, attacker := jewelAttackFixture(t)
	submitAttackersOnly(t, e, attacker)
	drainCombatPriority(t, e)
	declareUnblocked(t, e)
	e.resolveTop()

	// The latch is stamped for this combat. A direct second hook call (the
	// same call the completion branch makes) must queue nothing.
	e.pendingTriggers = nil
	e.checkAttackerUnblockedOnceTriggers()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second hook call in the same combat queued %d triggers, want 0", len(e.pendingTriggers))
	}

	// A new combat in the same turn re-arms: the stamp's Combat component
	// changes. Restore the Jewel to seat 1 first (the resolution handed it to
	// the attacker, which would fail ValidDefenders$ You), then simulate the
	// event fold's increment (BeginCombat) and re-run.
	e.emit(events.Event{Kind: events.ControlChange, Obj: jewelID, Player: 1})
	e.G.CombatsThisTurn++
	e.pendingTriggers = nil
	e.checkAttackerUnblockedOnceTriggers()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("hook after a new combat queued %d triggers, want 1", len(e.pendingTriggers))
	}
}

// TestCovetedJewelValidDefendersGate: at three seats the attacker is an
// opponent of the Jewel's controller (ValidAttackingPlayer$ passes) but
// attacks a DIFFERENT defender, so ValidDefenders$ You fails and nothing
// queues.
func TestCovetedJewelValidDefendersGate(t *testing.T) {
	t.Parallel()
	jewel := mshCorpusCardPath(t, "Coveted Jewel", "c/coveted_jewel.txt")
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	onBoardCard(t, e, 1, jewel)
	// The hook reads b.Attacking, not the decision option, so steering the
	// attacker's declared defender directly isolates the ValidDefenders$ gate.
	attacker := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(attacker).IsAttacking = true
	e.G.Obj(attacker).Attacking = 2

	e.checkAttackerUnblockedOnceTriggers()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("attack against a different defender queued %d triggers, want 0", len(e.pendingTriggers))
	}

	// Retarget the same attack at seat 1 (the Jewel's controller): both gates
	// now hold and exactly one trigger queues.
	e.G.Obj(attacker).Attacking = 1
	e.checkAttackerUnblockedOnceTriggers()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("attack against the Jewel's controller queued %d triggers, want 1", len(e.pendingTriggers))
	}
}

// TestCovetedJewelValidAttackingPlayerGate isolates ValidAttackingPlayer$: a
// synthetic trigger with a broad ValidDefenders$ (any defending player) under
// seat 1, where seat 1's OWN creature attacks seat 0. The defender gate is
// satisfied but the attacker's controller is the trigger's controller, not an
// opponent, so nothing queues.
func TestCovetedJewelValidAttackingPlayerGate(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	onBoard(t, e, 1, "Name:Watcher\nManaCost:2\nTypes:Artifact\n"+
		"T:Mode$ AttackerUnblockedOnce | ValidAttackingPlayer$ Player.Opponent | ValidDefenders$ Player | TriggerZones$ Battlefield | Execute$ X | TriggerDescription$ x.\n"+
		"SVar:X:DB$ Draw | Defined$ TriggeredAttackingPlayer | NumCards$ 3\n"+
		"Oracle:x\n")
	// Seat 1 attacks seat 0 with its own creature.
	attacker := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Obj(attacker).IsAttacking = true
	e.G.Obj(attacker).Attacking = 0

	e.checkAttackerUnblockedOnceTriggers()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("own-side attacker queued %d triggers, want 0 (ValidAttackingPlayer$ must fail)", len(e.pendingTriggers))
	}
}

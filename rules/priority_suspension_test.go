package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSuspendedResolutionLogsPriorityOnlyAtCompletion pins the priority-log
// contract this task restores: CR 117.5, nobody receives priority in the
// middle of a resolution. When a resolution suspends on a mid-resolution ask
// (a modal spell's KModes, an unless-pay, a discard), the engine is parked on
// that question and NOTHING has priority — so the log must carry no Priority
// event between the mid-resolution DecisionAsk and the answer. Before the
// fix (rules/legal.go) the pass-branch's pass-count reset emit ran
// unconditionally and logged Priority{active} right after that DecisionAsk,
// while the resolution was still suspended: a log lie, and the source of the
// host boundsOf mis-derivation.
//
// The companion property: once the resolution completes, the pass count
// resets and the "back to active" marker lands AFTER the object leaves the
// stack (the completion grant in rules/resolution.go, plus the normal
// grantPriority tail), never during the suspension. The engine's established
// shape for a completed resolution is the reset marker immediately followed by
// the round grant, so the leaf asserts the reset-with-zero holds after the
// object's completion move rather than counting Priority events (the two-event
// convention is the same for an unsuspended resolution, pinned elsewhere).
func TestSuspendedResolutionLogsPriorityOnlyAtCompletion(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\nA:SP$ Charm | Choices$ DoGain,DoLose\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 91, charm)
	addMana(t, e, 0, "R")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a suspended KModes decision, got %+v", d)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("the charm must suspend with the spell still on the stack, zone %s", o.Zone)
	}

	// Find the mid-resolution DecisionAsk's seq and the seq of the answer that
	// follows it in the log (the next DecisionMade).
	askIdx := -1
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind == events.DecisionAsk && e.L.Events[i].Text == "modes" {
			askIdx = i
			break
		}
	}
	if askIdx < 0 {
		t.Fatal("no mid-resolution 'modes' DecisionAsk in the log")
	}
	askSeq := e.L.Events[askIdx].Seq

	// Answer the modes question and run the resolution out.
	submitChoices(t, e, 1)
	passUntilStackEmpty(t, e, 20)

	// Find the answer's DecisionMade seq (the first DecisionMade after the
	// ask) and assert no Priority sits between the ask and the answer.
	ansIdx := -1
	for i := askIdx + 1; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.DecisionMade {
			ansIdx = i
			break
		}
	}
	if ansIdx < 0 {
		t.Fatal("no DecisionMade follows the mid-resolution ask")
	}
	// THE discriminating assertion: no Priority between the ask and its
	// answer. The old pass-branch emit put exactly one here.
	for i := askIdx + 1; i < ansIdx; i++ {
		if e.L.Events[i].Kind == events.Priority {
			t.Fatalf("Priority logged between the mid-resolution DecisionAsk (seq %d) and its answer (seq %d): seq %d player %d",
				askSeq, e.L.Events[ansIdx].Seq, e.L.Events[i].Seq, e.L.Events[i].Player)
		}
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("the charm resolved to %s, want Graveyard", z)
	}

	// The completion grant must land AFTER the object leaves the stack, with
	// the pass-count reset (Amount 0) that marks the round ending. Find the
	// charm's MoveZone off the stack, then the first Priority strictly after
	// it.
	moveIdx := -1
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack {
			moveIdx = i
			break
		}
	}
	if moveIdx < 0 {
		t.Fatal("the charm never left the stack after resolving")
	}
	found := false
	for i := moveIdx + 1; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.Priority {
			if e.L.Events[i].Amount != 0 {
				t.Fatalf("priority grant after completion carries Amount %d, want 0 (pass count not reset)",
					e.L.Events[i].Amount)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no Priority (pass-count reset) logged after the suspended resolution completed")
	}

	replayCheck(t, e, cfg)
}

// TestMidCastManaWindowLogsNoPriorityBetweenAskAndAnswer pins the CAST-TIME
// half of the same CR 117.5 / CR 117.3c contract that
// TestSuspendedResolutionLogsPriorityOnlyAtCompletion pins for a
// mid-RESOLUTION ask: nobody receives priority in the middle of an
// announcement. A cast that parks on a mid-cast ask leaves the engine waiting
// on an unanswered question (CR 601.2g's mana window, where the caster
// activates mana abilities before paying 601.2h) -- so the log must carry no
// Priority event between that DecisionAsk and its answer.
//
// The documented site is rules/stack.go's handleTarget cast branch: after
// payCast it emits the CR 117.3c "caster keeps priority" marker, and payCast
// can park on the 601.2g window. Before the guard (the `else` instead of
// `else if e.pending == nil`) that marker was emitted while the cast was
// still parked, logging a Priority immediately after the window's DecisionAsk
// and before its answer -- an `ask -> priority` adjacency inside one burst,
// exactly the class of log lie that breaks host/snapshot.go's boundsOf
// invariant (derived 1371, recorded 1372 on main). This leaf is what guards
// that line: a future regression reverting it to an unconditional emit makes
// the "no Priority between ask and answer" assertion below fail.
//
// Reaching the window through the public cast offer is impossible by design:
// the offer gate (castable) requires the pool alone to pay, while the window
// poses only when the pool alone does NOT pay. The window is therefore
// reached (as the CR 601 conformance lane already does) by beginning the cast
// directly, bypassing the offer gate -- the same white-box entry the 601.2g
// leaf uses. A targeted spell (so handleTarget runs on the cast path), an
// empty pool, and an untapped Mountain on the battlefield is the shape that
// parks payCast on the window after the target is chosen.
func TestMidCastManaWindowLogsNoPriorityBetweenAskAndAnswer(t *testing.T) {
	t.Parallel()
	// A targeted instant whose single red pip the empty pool cannot pay, but
	// an untapped Mountain can -- so the 601.2g window must pose after the
	// target choice; the mid-cast ask is a KChoose, not a mid-resolution KModes.
	spell := "Name:Bolt\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 77, spell)

	// Put an untapped Mountain under seat 0 (the caster) as the mana source
	// the window can activate; the pool stays empty.
	var land state.ObjID
	for _, c := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(c); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			land = c
			break
		}
	}
	if land == 0 {
		t.Fatal("fixture: no Mountain to put under seat 0")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZLibrary, To: state.ZBattlefield})
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("fixture: seat 0 pool = %v, want empty so the window must pose", e.G.Players[0].Pool)
	}

	// Begin the cast directly (bypassing the pool-only offer gate, which can
	// never offer a cast the pool alone cannot pay) and drive to the target
	// choice (CR 601.2c) -- after which handleTarget runs payCast for the
	// mid-cast 601.2g window.
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the cast-time target decision, got %+v", d)
	}
	// Choose the opponent (seat 1) as the target, which completes the target
	// choice and enters payCast.
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("opponent not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Now parked on the mid-cast 601.2g window: a KChoose over the caster's
	// untapped mana sources, with the spell still on the stack (pushCast ran
	// before the target choice).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the mid-cast 601.2g mana-window KChoose, got %+v", d)
	}
	if z := e.G.Obj(id).Zone; z != state.ZStack {
		t.Fatalf("the spell must be mid-cast, still on the stack, zone %s", z)
	}
	windowSeq := d.Seq
	if idx := int(windowSeq); e.L.Events[idx].Kind != events.DecisionAsk {
		t.Fatalf("event at the pending decision's seq %d is %v, want DecisionAsk",
			windowSeq, e.L.Events[idx].Kind)
	}

	// Answer the window by activating the Mountain, which pays the cost and
	// completes the cast. Then find the window's answer (its DecisionMade) and
	// assert no Priority sits between the DecisionAsk and the answer -- the
	// discriminating assertion. The old unconditional emit put exactly one
	// here, immediately after the ask, before the answer.
	submitChoices(t, e, 0)
	ansIdx := -1
	for i := int(windowSeq) + 1; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.DecisionMade {
			ansIdx = i
			break
		}
	}
	if ansIdx < 0 {
		t.Fatal("no DecisionMade follows the mid-cast window ask")
	}
	askSeq := e.L.Events[windowSeq].Seq
	for i := int(windowSeq) + 1; i < ansIdx; i++ {
		if e.L.Events[i].Kind == events.Priority {
			t.Fatalf("Priority logged between the mid-cast DecisionAsk (seq %d) and its answer (seq %d): seq %d player %d",
				askSeq, e.L.Events[ansIdx].Seq, e.L.Events[i].Seq, e.L.Events[i].Player)
		}
	}

	// The cast completed: paid (pool drained) and on the stack. Let it resolve
	// and replay the whole log, so the leaf also covers the fidelity contract.
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

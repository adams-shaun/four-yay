package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// K:MayFlashSac (CR 702.8) is pinned end to end: the "you may cast this as
// though it had flash" permission (granted for free, with no additional cost
// -- unlike the adjacent K:MayFlashCost) and the keyword's own consequence,
// "if you cast it any time a sorcery couldn't have been cast, the controller
// of the permanent it becomes sacrifices it at the beginning of the next
// cleanup step". The core behaviour is pinned on a synthetic enchantment
// (below) and the filing card itself (Necromancy) at the bottom.

// mayflashsacEnchantSrc is the minimal carrier: an enchantment with the
// keyword, no ETB, no cost complications, so the tests measure only the
// permission and the cleanup rider.
const mayflashsacEnchantSrc = "Name:Flashsac Enchant\nManaCost:1 G\nTypes:Enchantment\nK:MayFlashSac\nOracle:x\n"

// mayflashsacTargetSrc is a creature card seeded to a graveyard as a legal
// target for Necromancy's ETB reanimation.
const mayflashsacTargetSrc = "Name:Flashsac Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// sawMayFlashSacFlag reports whether the log carries a pay-time CastInfo for
// id whose flags include state.FlagMayFlashSac.
func sawMayFlashSacFlag(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id && events.FlagsFrom(ev.Counter)&state.FlagMayFlashSac != 0 {
			return true
		}
	}
	return false
}

// sawMayFlashSacCleanupRegister reports whether the log carries a
// DelayedRegister for id at exactly the cleanup step with the keyword's
// builtin SVar -- the registration the ETB hook emits.
func sawMayFlashSacCleanupRegister(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Obj == id &&
			ev.Step == state.StepCleanup && ev.Counter == "__kwMayFlashSacrifice" {
			return true
		}
	}
	return false
}

// passUntilTarget answers "pass" priority decisions (for whichever seat
// holds them) until a KTarget decision appears, and returns it. A resolution
// that asks its target only once the spell is resolving needs the caster and
// every responder to pass first.
func passUntilTarget(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while waiting for a target ask")
		}
		if d.Kind == decision.KTarget {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("decision %+v while waiting for a target ask", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("no target ask within %d passes", limit)
	return nil
}

// resolveStackFor answers any target ask with preferred (when it is among the
// offered candidates) and passes every priority decision until the stack is
// empty. It bounds the Necromancy cast's resolution without assuming the
// reanimation asks a target (a lone graveyard candidate may be taken
// silently).
func resolveStackFor(t *testing.T, e *Engine, preferred state.ObjID, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTarget:
			idx := indexOfObjOption(d, preferred)
			if idx < 0 {
				idx = d.Options[0].Index
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %+v while resolving", d)
		}
	}
}

// driveToNextTurnMain1 passes every decision (including a cleanup discard)
// until the next turn's main phase begins. Used after a cast resolution.
func driveToNextTurnMain1(t *testing.T, e *Engine) {
	t.Helper()
	driveToStep(t, e, e.G.Turn+1, e.G.NextAlive(e.G.Active), state.StepMain1)
}

// graveyardMoveStep reports the step the MoveZone that carried id to the
// graveyard happened in, read off the log's own StepChange records (a log
// records which step every event belongs to; a post-hoc snapshot cannot
// answer this once the game has moved on). ok is false when no such move
// exists. This is what pins the CR 514.3 timing: the sacrifice must be
// emitted inside a cleanup step, never at the next turn's upkeep -- the old
// gap the prior round shipped, where the queued registration waited until
// the next turn's first priority round and resolved there (a StepUpkeep
// answer here).
func graveyardMoveStep(e *Engine, id state.ObjID) (state.Step, bool) {
	step, moveStep := state.Step(0), state.Step(0)
	found := false
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.StepChange:
			step = ev.Step
		case events.MoveZone:
			if ev.Obj == id && ev.To == state.ZGraveyard {
				moveStep, found = step, true
			}
		}
	}
	return moveStep, found
}

// TestMayFlashSacSorceryCastIsNotSacrificed is the negative half: cast at
// NORMAL sorcery timing (main phase, empty stack), the ordinary cast is
// offered, no FlagMayFlashSac is stamped, no cleanup registration is made,
// and the permanent survives its own cleanup step.
func TestMayFlashSacSorceryCastIsNotSacrificed(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 501, mayflashsacEnchantSrc, mayflashsacTargetSrc)
	addMana(t, e, 0, "GC") // {1}{G}
	// Precondition: the cast is offered at plain sorcery timing (Mode "") --
	// without this an off-timing fixture would make the assertions vacuous.
	opt := plainCastOption(t, e, id)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: enchantment zone %s, want battlefield after the sorcery-speed cast", o.Zone)
	}
	if sawMayFlashSacFlag(e, id) {
		t.Fatal("a sorcery-timed cast must not stamp FlagMayFlashSac")
	}
	if sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("a sorcery-timed cast must register no cleanup sacrifice")
	}
	// Drive through this turn's cleanup into the next turn: the permanent
	// must still be on the battlefield (nothing sacrifices it).
	driveToStep(t, e, e.G.Turn+1, 1, state.StepMain1)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("enchantment zone %s after cleanup, want battlefield (sorcery-timed cast is not sacrificed)", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestMayFlashSacInstantWindowCastSacrificedAtCleanup is the filing defect's
// fix leaf: in a priority window that is NOT seat 0's main phase (so a
// sorcery could not have been cast), the card is offered through its
// MayFlashSac permission at no extra cost, the off-sorcery provenance is
// stamped, the permanent's entry registers the cleanup-step delayed
// sacrifice, and the permanent is gone by the time the next turn begins.
func TestMayFlashSacInstantWindowCastSacrificedAtCleanup(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 502, mayflashsacEnchantSrc, mayflashsacTargetSrc)
	// Stop in begin combat: seat 0 still has priority there, but it is not a
	// main phase, so spellTimingOK withholds the card unless the MayFlashSac
	// permission grants the flash window.
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)

	// Precondition: the ordinary sorcery-speed offer is absent off-main --
	// otherwise the permission would be untested (this is exactly the shape
	// that failed before the fix).
	if plainCastOptionExists(e.Pending().Options, id) {
		t.Fatal("precondition: plain cast offered off-main; the permission is untested")
	}
	// Fund {1}{G} after the drive (mana empties across step boundaries).
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.pending = nil
	e.askPriority(0)

	opt := plainCastOption(t, e, id)
	if opt.Mode != "" {
		t.Fatalf("MayFlashSac cast mode %q, want the ordinary cast (the permission carries no mode and no extra cost)", opt.Mode)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("enchantment zone %s, want battlefield after the off-sorcery cast", o.Zone)
	}
	if !sawMayFlashSacFlag(e, id) {
		t.Fatal("off-sorcery MayFlashSac cast did not stamp FlagMayFlashSac")
	}
	if !sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("off-sorcery MayFlashSac cast did not register the cleanup sacrifice")
	}

	// It must survive the rest of the turn (not sacrificed at end of turn)
	// and be gone once the next turn is underway.
	driveToStep(t, e, e.G.Turn, 0, state.StepEnd)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("enchantment zone %s at the end step, want battlefield (the rider fires at cleanup, not end of turn)", o.Zone)
	}
	driveToStep(t, e, e.G.Turn+1, 1, state.StepMain1)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("enchantment zone %s after the cleanup, want graveyard (sacrificed at the next cleanup step)", o.Zone)
	}
	// The CR 514.3 timing pin: the sacrifice MoveZone was emitted inside a
	// cleanup step. The old gap resolved the registration at the next turn's
	// upkeep, which kept the permanent alive through its cleanup step.
	if s, ok := graveyardMoveStep(e, id); !ok || s != state.StepCleanup {
		t.Fatalf("sacrifice emitted at step %v (found=%v), want StepCleanup (CR 514.3 resolves the registration inside the cleanup step)", s, ok)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("MayFlashSac registration not consumed: %d", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// TestMayFlashSacIsRegistered pins the coverage census: kw:MayFlashSac is
// registered as a supported primitive, so the report's ratchet defends
// every carrier. Without the RegisterNonAPI call the coverage walk
// (cards/primitive.go's Primitives, which lists kw:MayFlashSac off the
// K: line) would still count the carriers unsupported.
func TestMayFlashSacIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:MayFlashSac"] {
		t.Fatal("effects.Supported() does not report kw:MayFlashSac")
	}
	// The permission read and the coverage walk must agree about which cards
	// carry the keyword.
	src := "Name:Has It\nManaCost:1 G\nTypes:Enchantment\nK:MayFlashSac\nOracle:x\n"
	if !mayFlashSacFace(card(t, src).Faces[0]) {
		t.Fatal("mayFlashSacFace missed a printed K:MayFlashSac")
	}
	none := card(t, "Name:Has Not\nManaCost:1 G\nTypes:Enchantment\nOracle:x\n")
	if mayFlashSacFace(none.Faces[0]) {
		t.Fatal("mayFlashSacFace matched a card with no keyword")
	}
	if mayFlashSacFace(nil) {
		t.Fatal("mayFlashSacFace(nil) must be false")
	}
}

// TestNecromancyOffSorceryCastSacrificesItselfAtCleanup is the filing card's
// own corpus leaf: the real Necromancy script carries K:MayFlashSac and its
// reanimation body. Cast off-sorcery it resolves (reanimating the graveyard
// creature and becoming an Aura), then the keyword's rider sacrifices it at
// the next cleanup step.
func TestNecromancyOffSorceryCastSacrificesItselfAtCleanup(t *testing.T) {
	t.Parallel()
	necro := corpusCardText(t, "n/necromancy.txt")
	if !mayFlashSacFace(card(t, necro).Faces[0]) {
		t.Fatal("setup: the corpus Necromancy script does not carry K:MayFlashSac")
	}
	e, cfg, id := newFixtureDeck(t, 503, necro, mayflashsacTargetSrc)
	bear := moveSeeded(t, e, 0, mayflashsacTargetSrc, state.ZGraveyard)
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)

	// Fund {2}{B} after the drive.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 2})
	e.pending = nil
	e.askPriority(0)

	if !plainCastOptionExists(e.Pending().Options, id) {
		t.Fatalf("Necromancy not offered through K:MayFlashSac off-main: %+v", e.Pending().Options)
	}
	submitChoices(t, e, plainCastOption(t, e, id).Index)

	// Resolve the spell and its ETB reanimation, answering the reanimation's
	// graveyard-creature target with the seeded bear if it asks.
	resolveStackFor(t, e, bear, 60)
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("Necromancy zone %s, want battlefield after resolving", o.Zone)
	}
	// Precondition for the LTB assertions below: the reanimate chain really
	// ran -- the bear is under our control on the battlefield and the Aura is
	// attached to it, so the delayed TrigSacrifice registration exists.
	if b := e.G.Obj(bear); b.Zone != state.ZBattlefield || b.Controller != 0 {
		t.Fatalf("bear zone %s controller %d, want battlefield under seat 0 (the reanimation never ran)", b.Zone, b.Controller)
	}
	if o := e.G.Obj(id); o.AttachedTo != bear {
		t.Fatalf("Necromancy AttachedTo %d, want %d (the Aurify attach never ran)", o.AttachedTo, bear)
	}
	if !sawMayFlashSacFlag(e, id) {
		t.Fatal("off-sorcery Necromancy cast did not stamp FlagMayFlashSac")
	}
	if !sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("off-sorcery Necromancy cast did not register the cleanup sacrifice")
	}

	driveToStepAnsweringOrder(t, e, e.G.Turn+1, 1, state.StepMain1)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("Necromancy zone %s after cleanup, want graveyard (the rider sacrifices the permanent it became)", o.Zone)
	}
	if s, ok := graveyardMoveStep(e, id); !ok || s != state.StepCleanup {
		t.Fatalf("Necromancy sacrifice emitted at step %v (found=%v), want StepCleanup (CR 514.3)", s, ok)
	}
	replayCheck(t, e, cfg)
}

// driveToStepAnsweringOrder is driveToStep for a board whose leave-battlefield
// moment poses a trigger_order ask. Necromancy's cleanup sacrifice fires the
// registered DBDelay delayed trigger ("that creature's controller sacrifices
// it") in the same window as the card's own printed Static$-True DBCleanup
// trigger, and the controller is asked to order the simultaneous pair (CR
// 603.3) -- an ask driveToStep, which only answers priority passes and
// cleanup discards, cannot answer. The order submitted is the queue's own
// order (an identity permutation), which is a legal answer; the drain's
// existing semantics settle the rest.
func driveToStepAnsweringOrder(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while driving to turn %d seat %d step %s", turn, active, step)
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			idxs := make([]int, 0, d.Min)
			for j := 0; j < d.Min && j < len(d.Options); j++ {
				idxs = append(idxs, d.Options[j].Index)
			}
			if len(idxs) != d.Min {
				t.Fatalf("trigger_order with %d options, want %d: %+v", len(d.Options), d.Min, d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idxs}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		default:
			t.Fatalf("non-priority decision %+v encountered while driving to turn %d seat %d step %s",
				d, turn, active, step)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s within the budget", turn, active, step)
}

// TestNecromancySorceryCastIsNotSacrificed is the corpus negative half the
// prior round was missing: the REAL Necromancy script cast at plain
// sorcery speed (main phase, empty stack) resolves its reanimation, stamps
// no FlagMayFlashSac, registers no cleanup sacrifice, and survives its own
// cleanup step into the next turn.
func TestNecromancySorceryCastIsNotSacrificed(t *testing.T) {
	t.Parallel()
	necro := corpusCardText(t, "n/necromancy.txt")
	if !mayFlashSacFace(card(t, necro).Faces[0]) {
		t.Fatal("setup: the corpus Necromancy script does not carry K:MayFlashSac")
	}
	e, cfg, id := newFixtureDeck(t, 505, necro, mayflashsacTargetSrc)
	bear := moveSeeded(t, e, 0, mayflashsacTargetSrc, state.ZGraveyard)
	addMana(t, e, 0, "BCC") // {2}{B}

	// Precondition: the ordinary sorcery-speed cast IS offered in the main
	// phase (Mode ""), so the negative assertions below are not vacuous.
	opt := plainCastOption(t, e, id)
	if opt.Mode != "" {
		t.Fatalf("sorcery-speed cast mode %q, want the ordinary cast", opt.Mode)
	}
	submitChoices(t, e, opt.Index)
	resolveStackFor(t, e, bear, 60)
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("Necromancy zone %s, want battlefield after the sorcery-speed cast", o.Zone)
	}
	if sawMayFlashSacFlag(e, id) {
		t.Fatal("a sorcery-timed Necromancy cast must not stamp FlagMayFlashSac")
	}
	if sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("a sorcery-timed Necromancy cast must register no cleanup sacrifice")
	}
	driveToNextTurnMain1(t, e)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("Necromancy zone %s after the next turn began, want battlefield (a sorcery-timed cast is never sacrificed)", o.Zone)
	}
	if _, moved := graveyardMoveStep(e, id); moved {
		t.Fatal("Necromancy reached the graveyard without an off-sorcery cast")
	}
	replayCheck(t, e, cfg)
}

// TestMayFlashSacLeaveAndReturnIsNotSacrificed is the CR 400.7 incarnation
// pin (round-2 MAJOR): the delayed sacrifice is registered against the EXACT
// permanent that entered. One that leaves the battlefield and returns before
// that cleanup is a new incarnation; the stale promise must expire without
// acting on the returned permanent -- which stays on the battlefield.
func TestMayFlashSacLeaveAndReturnIsNotSacrificed(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 504, mayflashsacEnchantSrc, mayflashsacTargetSrc)
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.pending = nil
	e.askPriority(0)

	opt := plainCastOption(t, e, id)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)

	// Precondition: the off-sorcery cast registered the cleanup sacrifice
	// against this incarnation.
	if !sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("setup: the off-sorcery cast registered no cleanup sacrifice")
	}
	inc0 := e.G.Obj(id).Incarnation
	if sawMayFlashSacFlag(e, id) != true {
		t.Fatal("setup: the off-sorcery cast stamped no FlagMayFlashSac")
	}

	// Leave and return: each battlefield-boundary crossing advances the
	// incarnation, so the returned object is a different incarnation of the
	// same stable ObjID.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand, Player: 0})
	e.pending = nil
	e.Advance()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield, Player: 0})
	e.pending = nil
	e.Advance()
	if e.G.Obj(id).Incarnation == inc0 {
		t.Fatal("setup: the bounce did not advance the incarnation; the test would not exercise the tracking")
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("setup: returned-incarnation zone %s, want battlefield", o.Zone)
	}

	driveToNextTurnMain1(t, e)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("returned incarnation zone %s after the cleanup, want battlefield (the stale promise must expire, CR 400.7)", o.Zone)
	}
	if _, moved := graveyardMoveStep(e, id); moved {
		t.Fatal("the stale promise sacrificed the returned incarnation")
	}
	// The one-shot registration was still consumed -- it expired once, it
	// neither acts on the returned object nor waits for a later cleanup.
	if len(e.G.Delayed) != 0 {
		t.Fatalf("stale MayFlashSac registration not consumed at its fire: %d", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// --- CR 514.3b repeat (review round 3 MAJOR) --------------------------------
//
// The cleanup step is not "run once and leave". CR 514.3b: when a triggered
// ability is put on the stack during cleanup, the cleanup procedure REPEATS
// -- its 514.1 discard and 514.2 "until end of turn" actions run again --
// before the turn can end. The two tests below are the reviewer's two break
// attempts against the first pass at the repeat: an instant cast in the
// cleanup priority window whose until-end-of-turn effect must expire in the
// repeated cleanup, and a spell that draws the active player over the hand
// limit, which must face the repeated 514.1 discard. Both are driven through
// the real priority loop (handlePriority's empty-stack pass), not a direct
// repeatCleanup call, so the routing itself is under test.

// driveToStepCleanupWindow drives to a pending priority decision held inside
// the cleanup step with a non-empty stack -- the window the MayFlashSac
// delayed sacrifice opens. It stops the instant that decision is pending so a
// test can act in it (cast an instant), which driveToStep cannot do: it would
// answer the priority and keep going.
func driveToStepCleanupWindow(t *testing.T, e *Engine, turn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == state.StepCleanup &&
			len(e.G.Stack) > 0 && e.Pending() != nil && e.Pending().Kind == decision.KPriority {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before the cleanup window (turn %d seat %d step %s)",
				e.G.Turn, e.G.Active, e.G.Step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while driving to the cleanup window", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{passPriorityOption(t, d)}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("never reached a cleanup priority window")
}

// passPriorityOption returns the pass option index of the pending priority
// decision, failing the test when there is none.
func passPriorityOption(t *testing.T, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d)
	return -1
}

// TestMayFlashSacCleanupWindowPumpExpiresInRepeatedCleanup is the reviewer's
// until-end-of-turn break attempt. The MayFlashSac delayed sacrifice opens
// priority during the cleanup step; seat 0 casts a pump instant in that
// window, so the pump's until-end-of-turn effect exists while the cleanup
// step is still being processed. CR 514.3b's repeat must run the 514.2 body
// again once the stack empties, expiring the pump before the next turn --
// without it, handlePriority's pass advanced straight to the next turn and
// the +3/+3 survived it.
func TestMayFlashSacCleanupWindowPumpExpiresInRepeatedCleanup(t *testing.T) {
	t.Parallel()
	const pumpSrc = "Name:Cleanup Pump\nManaCost:0\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ +3 | NumTou$ +3\nOracle:x\n"

	// Control, on its own engine: the fixture pump really applies its
	// until-end-of-turn +3/+3 at ordinary timing (power 2 -> 5). Without this
	// the cleanup assertion below could pass because the pump does nothing at
	// all, which is the vacuous failure mode the review directive names.
	{
		ce, _, _ := newFixtureDeck(t, 7001, mayflashsacEnchantSrc, mayflashsacTargetSrc, pumpSrc)
		cb := moveSeeded(t, ce, 0, mayflashsacTargetSrc, state.ZBattlefield)
		cp := moveSeeded(t, ce, 0, pumpSrc, state.ZHand)
		ce.askPriority(0)
		submitChoices(t, ce, plainCastOption(t, ce, cp).Index)
		if td := ce.Pending(); td != nil && td.Kind == decision.KTarget {
			submitChoices(t, ce, indexOfObjOption(td, cb))
		}
		passUntilStackEmpty(t, ce, 30)
		if got := ce.Power(cb); got != 5 {
			t.Fatalf("control: the fixture pump did not apply at ordinary timing (power %d, want 5); the cleanup assertion would be vacuous", got)
		}
	}

	e, cfg, id := newFixtureDeck(t, 506, mayflashsacEnchantSrc, mayflashsacTargetSrc, pumpSrc)
	bear := moveSeeded(t, e, 0, mayflashsacTargetSrc, state.ZBattlefield)
	pump := moveSeeded(t, e, 0, pumpSrc, state.ZHand)

	// Precondition: the pump is a real hand card and the bear a real
	// battlefield creature at its printed power.
	if e.G.Obj(pump).Zone != state.ZHand || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("setup: pump zone %s bear zone %s, want hand/battlefield", e.G.Obj(pump).Zone, e.G.Obj(bear).Zone)
	}
	if got := e.Power(bear); got != 2 {
		t.Fatalf("setup: bear power %d, want printed 2", got)
	}

	// Cast the keyword card off-sorcery so it registers the cleanup rider.
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	submitChoices(t, e, plainCastOption(t, e, id).Index)
	passUntilStackEmpty(t, e, 30)
	if !sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("setup: the off-sorcery cast registered no cleanup sacrifice")
	}

	// Reach the cleanup priority window the sacrifice opens.
	driveToStepCleanupWindow(t, e, e.G.Turn, 0)
	if len(e.G.Stack) == 0 {
		t.Fatal("setup: no cleanup trigger on the stack; the window is not the delayed sacrifice")
	}

	// Cast the pump in the cleanup window. Precondition: it is offered and
	// the target ask offers the bear, so the pump will act on it.
	e.pending = nil
	e.askPriority(0)
	submitChoices(t, e, plainCastOption(t, e, pump).Index)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("pump target decision: %+v", td)
	}
	tidx := indexOfObjOption(td, bear)
	if tidx < 0 {
		t.Fatalf("pump target ask does not offer the bear %d: %+v", bear, td.Options)
	}
	submitChoices(t, e, tidx)

	// The pump resolves (into the graveyard) as the cleanup window drains.
	// Its effect applies and then, in the SAME drain, the CR 514.3b repeat
	// runs the 514.2 body and expires it, so the observable state here is
	// already post-expiry -- that is the fix. Drive to the next turn.
	for i := 0; i < 80 && !e.G.Over && e.G.Obj(pump).Zone != state.ZGraveyard; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatalf("no decision while the cleanup-window pump resolves")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %+v while the cleanup-window pump resolves", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{passPriorityOption(t, d)}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if e.G.Obj(pump).Zone != state.ZGraveyard {
		t.Fatal("precondition failed: the cleanup-window pump never resolved")
	}

	driveToNextTurnMain1(t, e)

	// The repeated cleanup expired the pump before the next turn began. If
	// the pass skipped the repeat and advanced the turn, the until-end-of-turn
	// +3/+3 would still be live here (power 5; the control above proves the
	// pump supplies exactly that when it is not expired).
	if got := e.Power(bear); got != 2 {
		t.Fatalf("bear power at the next turn = %d, want 2: a cleanup-window until-end-of-turn effect must expire in the CR 514.3b repeated cleanup, not survive into the next turn", got)
	}
	replayCheck(t, e, cfg)
}

// TestMayFlashSacCleanupWindowDrawTriggersRepeatedDiscard is the reviewer's
// second break attempt: a cleanup-window spell draws the active player over
// the hand limit, so the repeated cleanup's 514.1 action must ask an
// oversized hand to discard. Without the repeat, the turn advanced with the
// hand still over the limit and nothing was asked.
func TestMayFlashSacCleanupWindowDrawTriggersRepeatedDiscard(t *testing.T) {
	t.Parallel()
	const drawSrc = "Name:Cleanup Probe\nManaCost:0\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 2\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 507, mayflashsacEnchantSrc, drawSrc)
	probe := moveSeeded(t, e, 0, drawSrc, state.ZHand)

	// Precondition: the active player starts the cleanup window at the hand
	// limit (7), so casting the free probe and drawing 2 leaves them exactly
	// one over and there is a real discard owed in the repeat.
	if probe == 0 || e.G.Obj(probe).Zone != state.ZHand {
		t.Fatalf("setup: probe not in hand (zone %v)", e.G.Obj(probe).Zone)
	}

	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	submitChoices(t, e, plainCastOption(t, e, id).Index)
	passUntilStackEmpty(t, e, 30)
	if !sawMayFlashSacCleanupRegister(e, id) {
		t.Fatal("setup: the off-sorcery cast registered no cleanup sacrifice")
	}

	driveToStepCleanupWindow(t, e, e.G.Turn, 0)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	if handBefore != 7 {
		t.Fatalf("setup: hand at the cleanup window = %d, want 7 (the limit)", handBefore)
	}

	// Cast the cantrip in the cleanup window.
	e.pending = nil
	e.askPriority(0)
	submitChoices(t, e, plainCastOption(t, e, probe).Index)

	// Drive through the resolution and the repeat; the repeated cleanup must
	// open a KChoose discard of the one over-limit card before the next turn
	// can begin. (The draw happens as the spell resolves, so it is asserted
	// here, at the point the discard is posed, not before.)
	var discard *decision.Decision
	for i := 0; i < 200 && discard == nil; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatalf("no decision while waiting for the repeated cleanup discard")
		}
		if d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "discard" {
			discard = d
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %+v while waiting for the repeated cleanup discard", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{passPriorityOption(t, d)}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if discard == nil {
		t.Fatal("the CR 514.3b repeated cleanup never asked the over-limit hand to discard")
	}
	// The probe's draw really happened: the hand is one card over the limit.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand at the repeated cleanup discard = %d, want %d (7 minus the cast, plus the probe's 2)", got, handBefore+1)
	}
	if discard.Min != 1 || discard.Max != 1 {
		t.Fatalf("repeated-cleanup discard Min==Max==%d/%d, want 1/1 (one over the limit)", discard.Min, discard.Max)
	}
	if e.G.Step != state.StepCleanup {
		t.Fatalf("discard asked in step %s, want cleanup (CR 514.1 is a cleanup action)", e.G.Step)
	}
	// Answer it and finish the turn; the hand must be back at the limit.
	handNow := e.G.Zone(state.ZHand, 0)
	submitDiscard(t, e, handNow[len(handNow)-1])
	driveToNextTurnMain1(t, e)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("hand after the repeated cleanup = %d, want 7", got)
	}
	replayCheck(t, e, cfg)
}

// TestCleanupEmptyStackPassRepeatsCleanup targets the exact CR 514.3b path
// the reviewer's trace names: a priority round held INSIDE the cleanup step
// with an EMPTY stack, where handlePriority's pass case used to call
// advanceStep and begin the next turn outright. That state is reachable --
// finishCleanupStep's own doc records it: when the trigger drain asks a
// decision during cleanup, the answer resumes through resumeTriggerDrain,
// whose tail is grantPriority, "never a priorityRound re-entry". The player
// then holds priority in cleanup with whatever the drain left on the stack.
//
// The test builds exactly that state: an until-end-of-turn pump is live, the
// step is cleanup, the stack is empty, and priority is granted. Two passes
// reach handlePriority's empty-stack arm, which must run the cleanup
// procedure again (expiring the pump) before the turn can end. With
// advanceStep there instead, the pump survives into the next turn -- the
// assertion below is what fails.
func TestCleanupEmptyStackPassRepeatsCleanup(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", Controller: 0, AddPower: 3, AddToughness: 3, UntilEOT: true})

	// Precondition: the until-end-of-turn effect is live and the step is
	// cleanup with an empty stack, so the assertion below is about expiry in
	// the repeated cleanup and not about an effect that never applied.
	if got := e.Power(id); got != 5 {
		t.Fatalf("setup: power %d, want 5 (2 printed + 3 until-end-of-turn)", got)
	}
	e.G.Step = state.StepCleanup
	e.G.Stack = nil
	e.G.Priority = 0
	// Reach handlePriority's empty-stack arm in one pass: the pass count is
	// already at AliveCount-1, so the next pass takes `passes >= AliveCount`.
	// (With a lower count handlePriority only emits the next Priority event and
	// Submit's Advance re-enters priorityRound, which repeats the cleanup by a
	// different route -- that route is covered by the two end-to-end tests
	// above; this one isolates the empty-stack arm itself.)
	e.G.Passes = int32(e.G.AliveCount()) - 1
	e.grantPriority()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("setup: expected a cleanup priority decision, got %+v", d)
	}
	if e.G.Step != state.StepCleanup || len(e.G.Stack) != 0 {
		t.Fatalf("setup: step=%s stack=%d, want cleanup with an empty stack", e.G.Step, len(e.G.Stack))
	}
	// One pass now crosses the threshold and lands on the empty-stack arm.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{passPriorityOption(t, d)}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}

	// The cleanup procedure repeated: the 514.2 body expired the pump. Had
	// handlePriority advanced the step instead, the effect would still read 5
	// here.
	if got := e.Power(id); got != 2 {
		t.Fatalf("power after the empty-stack cleanup pass = %d, want 2: the cleanup step must REPEAT (CR 514.3b) rather than advance, so its 514.2 until-end-of-turn action runs again", got)
	}
}

// --- CR 707.10: a COPY is never cast (review round 4 MAJOR) -----------------

// countMayFlashSacRegisters counts the keyword's cleanup registrations in the
// log, by object. One off-sorcery cast must produce exactly one, naming the
// permanent the CAST spell became and nothing else.
func countMayFlashSacRegisters(e *Engine) map[state.ObjID]int {
	n := map[state.ObjID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Step == state.StepCleanup &&
			ev.Counter == "__kwMayFlashSacrifice" {
			n[ev.Obj]++
		}
	}
	return n
}

// TestMayFlashSacCopiedSpellIsNotSacrificed pins the cast-provenance half of
// the keyword's rider: "if you CAST it any time a sorcery couldn't have been
// cast". A copy of the spell is PUT on the stack, never cast (CR 707.10), so
// the token it becomes (CR 707.10g) carries no obligation, while the spell
// that really was cast off-sorcery still sacrifices itself at cleanup.
//
// events.StackCopy inherits the original's CastFlags on purpose (a copy of a
// fused or kicked spell resolves as one), and Move clears IsCopy as it turns
// the resolved copy into a token -- so by the time rules/altcast.go's entry
// hook runs there is nothing left to tell a never-cast token from the real
// cast except the flags themselves. state.CastProvenanceFlags is stripped at
// the mint for exactly that reason.
func TestMayFlashSacCopiedSpellIsNotSacrificed(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 506, mayflashsacEnchantSrc, mayflashsacTargetSrc)
	e.askPriority(0)
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.pending = nil
	e.askPriority(0)

	submitChoices(t, e, plainCastOption(t, e, id).Index)

	// Precondition: the cast spell is on the stack carrying the off-sorcery
	// provenance, so the copy below really does inherit a set flag.
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("setup: cast spell zone %s, want stack", o.Zone)
	}
	if e.G.Obj(id).CastFlags&state.FlagMayFlashSac == 0 {
		t.Fatal("setup: the off-sorcery cast stamped no FlagMayFlashSac on the stack object")
	}

	// Copy the permanent spell on the stack (the effects/copy.go emission).
	before := state.ObjID(len(e.G.Objs))
	e.emit(events.Event{Kind: events.StackCopy, Obj: id, Player: 0})
	copyID := state.ObjID(len(e.G.Objs))
	if copyID != before+1 {
		t.Fatalf("setup: StackCopy minted %d objects, want 1", copyID-before)
	}
	if o := e.G.Obj(copyID); o == nil || !o.IsCopy || o.Zone != state.ZStack {
		t.Fatalf("setup: minted copy %+v, want a stack copy", o)
	}
	// The measured defect: the copy inherited the cast provenance.
	if e.G.Obj(copyID).CastFlags&state.FlagMayFlashSac != 0 {
		t.Fatal("a stack copy inherited FlagMayFlashSac; a copy is never cast (CR 707.10)")
	}

	passUntilStackEmpty(t, e, 60)

	// Both are on the battlefield: the cast card, and the copy as a token.
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("cast permanent zone %s, want battlefield", o.Zone)
	}
	tok := e.G.Obj(copyID)
	if tok.Zone != state.ZBattlefield || !tok.IsToken {
		t.Fatalf("copy zone %s isToken=%v, want a battlefield token (CR 707.10g)", tok.Zone, tok.IsToken)
	}

	// Exactly one cleanup registration, and it names the cast permanent.
	regs := countMayFlashSacRegisters(e)
	if regs[copyID] != 0 {
		t.Fatalf("the copy got %d cleanup-sacrifice registrations, want 0: a copy is never cast", regs[copyID])
	}
	if regs[id] != 1 {
		t.Fatalf("the cast permanent got %d cleanup-sacrifice registrations, want 1", regs[id])
	}

	driveToStep(t, e, e.G.Turn+1, 1, state.StepMain1)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("cast permanent zone %s after the cleanup, want graveyard (it was cast off-sorcery)", o.Zone)
	}
	if o := e.G.Obj(copyID); o.Zone != state.ZBattlefield {
		t.Fatalf("copy token zone %s after the cleanup, want battlefield (only the CAST permanent is sacrificed)", o.Zone)
	}
	replayCheck(t, e, cfg)
}

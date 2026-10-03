package rules

// disabletriggers2: Nowhere to Run's second line --
// `S:Mode$ DisableTriggers | Secondary$ True | ValidTrigger$ Triggered.Ward |
// ValidCard$ Creature.OppCtrl+inZoneBattlefield` -- suppresses the Ward
// trigger of a ValidCard$-matched creature. Ward is not a printed T: line: it
// is synthesized by rules/trigger_granted.go's checkGrantedWardTriggers from
// the derived keyword list, and marked Params["Ward"] == "True". The
// ValidTrigger$ gate therefore matches that marker, and fails closed for any
// qualifier this build does not model.
//
// The two directions are what make the test unable to pass by over-suppressing
// (the trap a naive "add ValidTrigger to the known params" fix falls into):
// the static's ValidCard$ scopes it to the static controller's OPPONENTS, so
// the controller's own warded creature must still ward.
//
// Ward is GRANTED here (a layer-6 AddKeywords effect), never printed: a
// printed K:Ward: line is suppressed from the granted-walk by design (its
// derived string equals the printed one, so checkGrantedWardTriggers skips
// it), and the synthesized trigger is precisely the granted/cloaked shape
// ValidTrigger$ Triggered.Ward exists for.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// nowhereToRunStaticFixture is Nowhere to Run's suppression line verbatim.
const nowhereToRunStaticFixture = "Name:Nowhere Gate\nTypes:Enchantment\n" +
	"S:Mode$ DisableTriggers | Secondary$ True | ValidTrigger$ Triggered.Ward | ValidCard$ Creature.OppCtrl+inZoneBattlefield\n" +
	"Oracle:x\n"

// wardCreatureFixture prints no Ward; the grant is added by wardQueueGame.
const wardCreatureFixture = "Name:Warded Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// wardQueueGame parks the static (when present) under seat 0 and one warded
// creature per requested seat on the battlefield, granting each Ward:2 with a
// layer-6 continuous effect so checkGrantedWardTriggers synthesizes the
// trigger. Returns the creature ids in seat order.
func wardQueueGame(t *testing.T, withStatic bool, creatureSeats ...state.PlayerID) (*Engine, []state.ObjID) {
	t.Helper()
	e := newSeats(t, 2)
	if withStatic {
		gate := parkObj(t, e, card(t, nowhereToRunStaticFixture), 0, state.ZBattlefield)
		if got := e.activeStatics("DisableTriggers"); len(got) != 1 {
			t.Fatalf("precondition: DisableTriggers static from %d is not active: %v", gate, got)
		}
	}
	var ids []state.ObjID
	for _, seat := range creatureSeats {
		id := parkObj(t, e, card(t, wardCreatureFixture), seat, state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: warded creature %d is not on the battlefield: %+v", id, o)
		}
		e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LAbilities,
			Affects: "Creature", Controller: seat, AddKeywords: []string{"Ward:2"}, UntilEOT: true})
		has := false
		for _, k := range e.Derived(id).Keywords {
			if cards.KeywordHead(k) == "Ward" {
				has = true
				break
			}
		}
		if !has {
			t.Fatalf("precondition: creature %d carries no Ward head: %v", id, e.Derived(id).Keywords)
		}
		ids = append(ids, id)
	}
	return e, ids
}

// wardTriggerCount emits a TargetsChosen at warded from a real stack object
// controlled by targeter (CR 702.21a: a Ward trigger never fires when the
// warded permanent's controller IS the targeting spell's controller, so the
// targeter must be the warded creature's opponent), and returns how many
// triggers queued (pending or on the stack).
func wardTriggerCount(t *testing.T, e *Engine, warded state.ObjID, targeter state.PlayerID) int {
	t.Helper()
	cause := e.G.Zone(state.ZLibrary, targeter)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: targeter, From: state.ZLibrary, To: state.ZStack})
	before := len(e.pendingTriggers) + len(e.G.Stack)
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	return len(e.pendingTriggers) + len(e.G.Stack) - before
}

// TestDisableTriggersNowhereToRunSuppressesOpponentWard pins the leaf: with
// Nowhere to Run's static out, an opponent's warded creature does not queue
// its Ward trigger, the static controller's own warded creature still does
// (ValidCard$ scopes it), and with the static removed the opponent's Ward
// fires again -- so the zero is the static, not a harness that never fires.
// Each direction runs on a FRESH engine so one direction's stack cannot leak
// into the next count.
func TestDisableTriggersNowhereToRunSuppressesOpponentWard(t *testing.T) {
	// A: the static's controller targets an OPPONENT's warded creature (the
	// card's actual shape) -- suppressed.
	aE, aIDs := wardQueueGame(t, true, 1)
	if n := wardTriggerCount(t, aE, aIDs[0], 0); n != 0 {
		t.Fatalf("opponent's Ward queued %d trigger(s) under Nowhere to Run, want 0", n)
	}

	// B: the same static, but the warded creature is the static controller's
	// OWN -- ValidCard$ OppCtrl excludes it, so its Ward still queues. This is
	// the anti-blanket-suppression assertion.
	bE, bIDs := wardQueueGame(t, true, 0)
	if n := wardTriggerCount(t, bE, bIDs[0], 1); n != 1 {
		t.Fatalf("controller's own Ward queued %d trigger(s), want 1 (ValidCard$ OppCtrl excludes it)", n)
	}

	// C: control -- no static, the opponent's Ward fires, proving A's zero
	// was the static and not a harness that never fires.
	cE, cIDs := wardQueueGame(t, false, 1)
	if got := cE.activeStatics("DisableTriggers"); len(got) != 0 {
		t.Fatalf("precondition: control board has a DisableTriggers static: %v", got)
	}
	if n := wardTriggerCount(t, cE, cIDs[0], 0); n != 1 {
		t.Fatalf("without the static the opponent's Ward queued %d trigger(s), want 1", n)
	}
}

// TestDisableTriggersNowhereToRunUnreadValidTriggerFailsOpen pins the
// fail-closed contract for a ValidTrigger$ value this build does not model:
// the whole static fails open and reports the unread value loudly, never a
// blanket suppression. It uses an enter-the-battlefield trigger (the
// dependency's own fail-open shape) so the live walk's once-per-game Note is
// exercised on the path that carries it, and separately proves a Ward trigger
// under the same static still queues.
func TestDisableTriggersNowhereToRunUnreadValidTriggerFailsOpen(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Unknown Kind Gate\nTypes:Enchantment\n"+
		"S:Mode$ DisableTriggers | Secondary$ True | ValidTrigger$ Triggered.FutureKeyword | ValidCard$ Creature.OppCtrl+inZoneBattlefield\nOracle:x\n")
	etb := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this enters, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	etbCreature := card(t, "Name:ETB Creature\nTypes:Creature\n"+etb+"Oracle:x\n")

	if gate := parkObj(t, e, staticCard, 0, state.ZBattlefield); len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: static from %d is not active", gate)
	}
	id := parkObj(t, e, etbCreature, 1, state.ZHand)
	if got := disableTriggersUnread(e.activeStatics("DisableTriggers")[0]); len(got) != 1 || got[0] != "ValidTrigger=Triggered.FutureKeyword" {
		t.Fatalf("precondition: unread = %v, want [ValidTrigger=Triggered.FutureKeyword]", got)
	}
	if n := countQueued(e, id, state.ZHand, state.ZBattlefield); n != 1 {
		t.Fatalf("unmodelled ValidTrigger$ suppressed the ETB trigger (queued %d, want 1)", n)
	}
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled DisableTriggers parameters: ValidTrigger=Triggered.FutureKeyword") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("%d unmodelled DisableTriggers notes, want exactly 1; log=%+v", notes, e.L.Events)
	}
}

// TestDisableTriggersNowhereToRunUnreadValueLeavesWardFiring pins the
// fail-open on the Ward path itself: an unmodelled ValidTrigger$ value makes
// the static inert, so the opponent's Ward still queues.
func TestDisableTriggersNowhereToRunUnreadValueLeavesWardFiring(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Unknown Kind Gate\nTypes:Enchantment\n"+
		"S:Mode$ DisableTriggers | Secondary$ True | ValidTrigger$ Triggered.FutureKeyword | ValidCard$ Creature.OppCtrl+inZoneBattlefield\nOracle:x\n")
	parkObj(t, e, staticCard, 0, state.ZBattlefield)
	id := parkObj(t, e, card(t, wardCreatureFixture), 1, state.ZBattlefield)
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LAbilities,
		Affects: "Creature", Controller: 1, AddKeywords: []string{"Ward:2"}, UntilEOT: true})
	if got := disableTriggersUnread(e.activeStatics("DisableTriggers")[0]); len(got) != 1 {
		t.Fatalf("precondition: unread = %v, want one unread-value entry", got)
	}
	if n := wardTriggerCount(t, e, id, 0); n != 1 {
		t.Fatalf("unmodelled ValidTrigger$ suppressed the Ward trigger (queued %d, want 1)", n)
	}
}

// TestDisableTriggersNowhereToRunRealCorpusCard drives the real Nowhere to Run
// script through the same path, so the fix is tied to the card, not only the
// fixture. Vein Ripper's Ward is a real granted-synthesis shape (its derived
// string differs from the printed one), so the ordinary combat-engine harness
// can drive it.
func TestDisableTriggersNowhereToRunRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	nowhere := mustCorpusCard(t, reg, "Nowhere to Run")
	veinRipper := mustCorpusCard(t, reg, "Vein Ripper")
	e := combatEngine(t)
	if gate := parkObj(t, e, nowhere, 0, state.ZBattlefield); len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: Nowhere to Run's static from %d is not active", gate)
	}
	warded := onBoardCard(t, e, 1, veinRipper)
	has := false
	for _, k := range e.Derived(warded).Keywords {
		if cards.KeywordHead(k) == "Ward" {
			has = true
			break
		}
	}
	if !has {
		t.Fatalf("precondition: %d carries no Ward head: %v", warded, e.Derived(warded).Keywords)
	}
	if n := wardTriggerCount(t, e, warded, 0); n != 0 {
		t.Fatalf("Nowhere to Run did not suppress the opponent's Ward (queued %d, want 0)", n)
	}
}

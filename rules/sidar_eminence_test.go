package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the command-zone object traversal defect behind Sidar
// Jabari of Zhalfir's Eminence (fb-20260927T160557Z-b958ef31): the ordinary
// printed-trigger walk used to stop at ZStack, and ZCommand is appended
// AFTER ZStack (state/ids.go), so a trigger declaring TriggerZones$ Command
// was never scanned from the zone it functions in. The regression uses the
// real corpus card (Forge scripts are GPL and stay gitignored); the Knight
// fixture is authored inline.

const eminenceKnightSrc = "Name:Test Knight\nManaCost:1 W\nTypes:Creature Human Knight\nPT:2/2\nOracle:x\n"

// eminenceBoard builds a fresh two-seat engine with Sidar in seat 0's COMMAND
// zone and a non-summoning-sick Knight on seat 0's battlefield, plus a second
// battlefield creature of the given source string. It asserts the placement
// preconditions the trigger rules read (object in the zone, type, controller)
// so a vacuous setup fails loudly rather than passing silently.
func eminenceBoard(t *testing.T, sidar *cards.Card, otherSrc string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	e.Advance()
	toMain1(t, e)

	so := e.G.AddObject(sidar, 0)
	so.Zone = state.ZCommand
	e.G.SetZone(state.ZCommand, 0, append(e.G.Zone(state.ZCommand, 0), so.ID))
	e.staticEpoch, e.activeEpoch = -1, -1

	knight := onBoard(t, e, 0, eminenceKnightSrc)
	e.G.Obj(knight).SummonSick = false

	other := onBoard(t, e, 0, otherSrc)
	e.G.Obj(other).SummonSick = false

	// Preconditions: Sidar is in the command zone (the zone the trigger's
	// TriggerZones$/PresentZone$ read) and is not on the battlefield.
	if got := e.G.Obj(so.ID).Zone; got != state.ZCommand {
		t.Fatalf("precondition: Sidar zone = %s, want command", got)
	}
	if zoneHas(e.G.Zone(state.ZBattlefield, 0), so.ID) {
		t.Fatal("precondition: Sidar must not also be on the battlefield")
	}
	// The Knight is a battlefield creature controlled by seat 0 with the
	// Knight creature type the trigger's ValidAttackers$ requires.
	if got := e.G.Obj(knight).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: knight zone = %s, want battlefield", got)
	}
	if c := e.G.Obj(knight).Controller; c != 0 {
		t.Fatalf("precondition: knight controller = %d, want seat 0", c)
	}
	if !slices.Contains(e.Derived(knight).Types, "Knight") {
		t.Fatalf("precondition: knight derived types = %v, want Knight", e.Derived(knight).Types)
	}
	return e, so.ID, knight
}

// declareAttackersReal drives a real CR 508.1 declaration of the given
// attackers through the engine's own KAttackers decision.
func declareAttackersReal(t *testing.T, e *Engine, attackers ...state.ObjID) {
	t.Helper()
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, attackers...)
}

// TestSidarJabariEminenceTriggersFromCommandZone is the regression for the
// command-zone traversal fix. With Sidar in seat 0's command zone and a
// controlled Knight actually declared attacking, the Eminence ability must
// queue exactly once and, on resolution, draw a card then discard a card.
func TestSidarJabariEminenceTriggersFromCommandZone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sidar := mustCorpusCard(t, reg, "Sidar Jabari of Zhalfir")

	e, sidarID, knight := eminenceBoard(t, sidar, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	handBefore := len(e.G.Zone(state.ZHand, 0))

	declareAttackersReal(t, e, knight)

	// Precondition: the declaration actually happened -- the Knight is
	// attacking. (Sidar is not, and must not be; it is in the command zone.)
	if !e.G.Obj(knight).IsAttacking {
		t.Fatal("precondition: the declared Knight is not marked attacking")
	}
	if e.G.Obj(sidarID).IsAttacking {
		t.Fatal("precondition: Sidar must not be an attacker")
	}

	// The trigger must have queued and drained onto the stack, exactly once.
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Eminence did not queue exactly once: stack = %v, want one trigger", e.G.Stack)
	}
	trig := e.G.Obj(e.G.Stack[0])
	if trig == nil || trig.Ability == nil {
		t.Fatalf("stack top is not a trigger ability: %+v", trig)
	}
	if trig.Source != sidarID {
		t.Fatalf("stack top source = %d, want Sidar %d", trig.Source, sidarID)
	}

	// Resolve: draw a card, then discard a card (Sidar's TrigLoot: Draw 1,
	// then SubAbility DBDiscard: Discard 1 | Mode$ TgtChoose, which poses a
	// mid-resolution discard ask).
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("Eminence draw: hand = %d, want %d (one drawn)", got, handBefore+1)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard" || len(d.Options) == 0 {
		t.Fatalf("Eminence did not pose its discard after the draw: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("Eminence discard: hand = %d, want %d (drew one, discarded one)", got, handBefore)
	}
	// The discarded card is the new graveyard top for seat 0.
	if len(e.G.Zone(state.ZGraveyard, 0)) == 0 {
		t.Fatal("no card in the graveyard after the Eminence discard")
	}
}

// TestSidarEminenceControlConditions screens the two conditions the Eminence
// trigger must keep reading: a non-Knight attacker queues nothing, and a
// Knight attacker queues nothing when Sidar is not in the command zone. Both
// use the same walk the positive case exercises, so a fix that ignored either
// condition would fail here.
func TestSidarEminenceControlConditions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sidar := mustCorpusCard(t, reg, "Sidar Jabari of Zhalfir")

	t.Run("non-knight attacker", func(t *testing.T) {
		e, sidarID, _ := eminenceBoard(t, sidar, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if got := e.G.Obj(sidarID).Zone; got != state.ZCommand {
			t.Fatalf("precondition: Sidar zone = %s, want command", got)
		}
		bear := e.G.Zone(state.ZBattlefield, 0)[1] // the non-Knight creature
		if slices.Contains(e.Derived(bear).Types, "Knight") {
			t.Fatalf("precondition: control attacker is a Knight: %v", e.Derived(bear).Types)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear}})
		e.putTriggersOnStack()
		if len(e.pendingTriggers) != 0 || len(e.G.Stack) != 0 {
			t.Fatalf("non-Knight attacker fired Eminence: pending=%d stack=%v", len(e.pendingTriggers), e.G.Stack)
		}
	})

	t.Run("sidar outside the command zone", func(t *testing.T) {
		e, sidarID, knight := eminenceBoard(t, sidar, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		// Move Sidar out of the command zone (to its owner's hand) -- a
		// setup-only move, the same shape the placement used.
		e.G.SetZone(state.ZCommand, 0, nil)
		o := e.G.Obj(sidarID)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), sidarID))
		if got := e.G.Obj(sidarID).Zone; got != state.ZHand {
			t.Fatalf("precondition: Sidar zone = %s, want hand", got)
		}
		if len(e.G.Zone(state.ZCommand, 0)) != 0 {
			t.Fatal("precondition: command zone is not empty")
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{knight}})
		e.putTriggersOnStack()
		if len(e.pendingTriggers) != 0 || len(e.G.Stack) != 0 {
			t.Fatalf("Eminence fired with Sidar outside the command zone: pending=%d stack=%v", len(e.pendingTriggers), e.G.Stack)
		}
	})
}

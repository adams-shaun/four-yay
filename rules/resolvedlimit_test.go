package rules

// Task resolvedlimit1: ResolvedLimit$ ("Do this only once each turn.") is a
// trigger-mode variant parameter Forge's TriggeredAbility counts as
// resolvedThisTurn -- the RESOLUTION-counted cousin of ActivationLimit$ (which
// counts trigger OCCURRENCES). Before this fix nothing in the Go tree read it,
// so every ResolvedLimit$ carrier fired on every eligible event all turn long
// instead of once (Baron Strucker, HYDRA Overlord's two Villains conniving
// twice). The gate is scoped to EVERY trigger mode (ChangesZone and SpellCast,
// the two largest groups, are deliberately NOT in actionTriggerModes), is
// keyed per SOURCE object so a card's paired lines share one limit, and is
// incremented only where the effect actually runs -- a declined OptionalDecider$
// instance never consumes it.
//
// Fixtures are inline per the licensing rule (never a .cards/ .txt); the real
// corpus carriers' T:/SVar: lines are copied verbatim off .cards/cardsfolder.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resolvedLimitVillain is a bare Villain, the type Baron Strucker's trigger
// watches for.
const resolvedLimitVillain = "Name:Test Villain\nManaCost:1 B\nTypes:Creature Human Villain\nPT:1/1\nOracle:x\n"

// putInHand places a freshly-added object into seat p's hand zone eventlessly,
// so a following MoveZone emit is a real hand->battlefield entry the trigger
// walk sees (the action_statics_test.go handEngine shape, without a deck).
func putInHand(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// enterCreature moves a card from seat p's hand to the battlefield, firing the
// ChangesZone entry triggers, and returns its id.
func enterCreature(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	id := putInHand(t, e, p, c)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	return id
}

// baronStruckerFixture places the real corpus Baron Strucker, HYDRA Overlord on
// seat 0's battlefield and returns the engine plus Baron's id.
func baronStruckerFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	card := mshCorpusCardPath(t, "Baron Strucker, HYDRA Overlord", "b/baron_strucker_hydra_overlord.txt")
	e := combatEngine(t)
	return e, onBoardCard(t, e, 0, card)
}

// answerOptional submits the pending KTriggerOptional decision with the given
// option index (0 = yes, 1 = no) and drains the resolution's remaining
// decisions, so the stack empties.
func answerOptional(t *testing.T, e *Engine, option int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected a trigger_optional decision, got %+v", d)
	}
	if option < 0 || option >= len(d.Options) {
		t.Fatalf("option %d out of range for %+v", option, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[option].Index}}); err != nil {
		t.Fatalf("submit optional %d: %v", option, err)
	}
	// The yes arm re-enters the resolution (Connive is an unimplemented API
	// Note, so nothing else asks); the no arm finishes it. Either way a
	// Priority event returns priority. Drain any residual decisions (a
	// trigger-order ask would have been answered before we got here).
	for i := 0; i < 10 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while emptying the stack (depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					goto next
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit %v: %v", d.Kind, err)
		}
	next:
	}
}

// drainQueuedTrigger queues and resolves the single pending trigger, answering
// its optional ask YES (option 0), and returns the number of TriggerPush
// events for source.
func drainQueuedTrigger(t *testing.T, e *Engine, source state.ObjID) int {
	t.Helper()
	if len(e.pendingTriggers) == 0 {
		t.Fatalf("no trigger queued for source %d", source)
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.resolveTop()
	answerOptional(t, e, 0)
	return pushCount(e, source)
}

// TestResolvedLimitDeclinedOptionalDoesNotConsume pins the report's explicit
// semantics: a DECLINED optional instance is not a resolution, so a second
// Villain in the same turn must still be offered. This is the leaf that fails
// if the increment is wrongly placed on the queue path.
func TestResolvedLimitDeclinedOptionalDoesNotConsume(t *testing.T) {
	t.Parallel()
	e, baron := baronStruckerFixture(t)

	enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	if len(e.pendingTriggers) == 0 {
		t.Fatal("first Villain did not queue the trigger")
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.resolveTop()
	answerOptional(t, e, 1) // NO: declined
	if n := pushCount(e, baron); n != 1 {
		t.Fatalf("declined first instance produced %d TriggerPush events, want 1 (it did go on the stack)", n)
	}

	// Second Villain: the decline must NOT have consumed the limit.
	enterCreature(t, e, 0, card(t, resolvedLimitVillain))
	if len(e.pendingTriggers) == 0 {
		t.Fatal("declined first instance consumed the ResolvedLimit; a second Villain must still queue")
	}
	if n := drainQueuedTrigger(t, e, baron); n != 2 {
		t.Fatalf("accepted second instance produced %d total TriggerPush events, want 2", n)
	}
}

// TestResolvedLimitMandatoryCarrierCounts pins the mandatory (no
// OptionalDecider$) increment site on the real corpus carrier Irreverent
// Gremlin: its "may" is a Cost$ on the Execute AB, not an OptionalDecider$, so
// its resolution reaches resolveTop's mandatory tail and must consume the
// limit there. The second eligibility in the same turn queues nothing.
func TestResolvedLimitMandatoryCarrierCounts(t *testing.T) {
	t.Parallel()
	gremlin := mshCorpusCardPath(t, "Irreverent Gremlin", "i/irreverent_gremlin.txt")
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, gremlin)

	// A power-1 creature enters: the trigger queues and resolves (its Draw's
	// Discard cost is answered by drainTriggerAsks' default first option).
	enterCreature(t, e, 0, card(t, "Name:Test Small\nManaCost:1\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"))
	if len(e.pendingTriggers) == 0 {
		t.Fatal("first small creature did not queue the mandatory trigger")
	}
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.resolveTop()
	drainTriggerAsks(t, e, 30)
	if n := pushCount(e, src); n != 1 {
		t.Fatalf("first small creature produced %d TriggerPush events, want 1", n)
	}

	// Second small creature, same turn: silent.
	enterCreature(t, e, 0, card(t, "Name:Test Small Two\nManaCost:1\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"))
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second small creature queued %d triggers, want 0", len(e.pendingTriggers))
	}
	if n := pushCount(e, src); n != 1 {
		t.Fatalf("after the second small creature, Gremlin has %d TriggerPush events, want 1", n)
	}
}

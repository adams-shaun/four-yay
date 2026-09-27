// The Replacements$ grant on a resolving DB$ Animate body
// (effects/leavebattlefield.go's registerAnimateReplacements): a named
// R:-shaped SVar on the ANIMATING face's table is installed as an
// Effect-created replacement riding the animated object, for the animation's
// own lifetime. Spirit-Sister's Call is the flagship real-corpus carrier: its
// end-step ability returns a chosen permanent card from the graveyard and
// animates it with `Replacements$ ReplaceLeaves`, whose body promises "If
// this permanent would leave the battlefield, exile it instead of putting it
// anywhere else." The other participants are real corpus cards moved through
// the ordinary helpers; no corpus .txt is inlined.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// spiritSistersCallEngine seeds Spirit-Sister's Call on seat 0's battlefield,
// one Doomed Traveler card in seat 0's graveyard (the end-step ability's
// chosen target; its real dies trigger -- "When Doomed Traveler dies, create
// a 1/1 white Spirit token" -- is the counter the flagship test asserts never
// fires) and a Grizzly Bears on seat 0's battlefield (a permanent sharing a
// card type with the chosen card, so the optional sacrifice can be paid). It
// then drives to seat 0's end step and through the ability's choices: choose
// the graveyard Traveler, accept the sacrifice, and let the chained
// ChangeZone + Animate resolve. Returns the engine, its replay Config, the
// call's id and the returned Traveler's id. The preconditions the caller's
// assertions rely on (the Traveler really returned under seat 0) are asserted
// HERE, so a silently failed return fails loudly and names itself.
func spiritSistersCallEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	call := lookup(t, reg, "Spirit-Sister's Call")
	traveler := lookup(t, reg, "Doomed Traveler")
	bears := lookup(t, reg, "Grizzly Bears")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{call, traveler, bears}, []*cards.Card{})
	callID := moveByName(t, e, 0, "Spirit-Sister's Call", state.ZBattlefield)
	// The Traveler starts in the graveyard: the ability's chosen target.
	target := moveByName(t, e, 0, "Doomed Traveler", state.ZGraveyard)
	// The Bear stays on the battlefield as the sacrifice fodder.
	fodder := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	// Drive to seat 0's end step, then answer the ability's asks: the
	// ChooseCard (target the graveyard Traveler), the optional sacrifice
	// (accept it) and the ordinary trigger-order/priority rounds.
	ateotDriveToStep(t, e, e.G.Turn, 0, state.StepEnd)
	answeredTarget, answeredSac := false, false
	for i := 0; i < 200 && !e.G.Over; i++ {
		// Once both asks are answered and the trigger's resolution has
		// drained (stack empty), stop: the ability has resolved and further
		// decisions belong to later steps.
		if answeredTarget && answeredSac && len(e.G.Stack) == 0 {
			break
		}
		d := e.Pending()
		if d == nil {
			if !e.putTriggersOnStack() {
				break
			}
			continue
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			answerTriggerOrders(t, e)
		case decision.KTriggerOptional:
			// Accept the optional trigger.
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("trigger_optional with no yes option: %+v", d)
			}
			submitChoices(t, e, idx)
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
			submitChoices(t, e, idx)
		case decision.KChoose:
			if !answeredTarget {
				idx := -1
				for _, o := range d.Options {
					if o.Obj == target {
						idx = o.Index
					}
				}
				if idx < 0 {
					t.Fatalf("graveyard Traveler %d not offered as the chosen card: %+v", target, d.Options)
				}
				submitChoices(t, e, idx)
				answeredTarget = true
			} else if !answeredSac {
				// The optional sacrifice: choose the battlefield Bear.
				idx := -1
				for _, o := range d.Options {
					if o.Obj == fodder {
						idx = o.Index
					}
				}
				if idx < 0 {
					t.Fatalf("battlefield Bear %d not offered as the sacrifice: %+v", fodder, d.Options)
				}
				submitChoices(t, e, idx)
				answeredSac = true
			} else {
				t.Fatalf("unexpected third choose decision: %+v", d)
			}
		case decision.KTarget:
			idx := -1
			for _, o := range d.Options {
				if o.Obj == target {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("graveyard Traveler %d not offered as a target: %+v", target, d.Options)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision %q while resolving Spirit-Sister's Call: %+v", d.Kind, d)
		}
	}
	if !answeredTarget {
		t.Fatal("the end-step ability never asked which graveyard card to choose")
	}
	if !answeredSac {
		t.Fatal("the end-step ability never asked whether to sacrifice a permanent")
	}

	// PRECONDITIONS for every caller assertion below: the chosen Traveler
	// really returned to the battlefield under seat 0, and the fodder was
	// really sacrificed away.
	o := e.G.Obj(target)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned Traveler zone = %+v, want battlefield (precondition: the end-step ability returned it)", o)
	}
	if o.Controller != 0 {
		t.Fatalf("returned Traveler controller = %d, want seat 0", o.Controller)
	}
	if fo := e.G.Obj(fodder); fo != nil && fo.Zone == state.ZBattlefield {
		t.Fatalf("sacrifice fodder %d is still on the battlefield, want it sacrificed (precondition)", fodder)
	}
	return e, cfg, callID, target
}

// TestSpiritSistersCallGrantedReplacementExilesPermanent is the ticket's
// flagship regression: the permanent Spirit-Sister's Call returns gains the
// named Replacements$ replacement, so when it would leave the battlefield it
// is exiled instead -- it must NOT reach the graveyard, its real dies trigger
// must NOT fire, and the replacement must be consumed by the departure so it
// cannot re-arm on a later re-entry.
func TestSpiritSistersCallGrantedReplacementExilesPermanent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, _, target := spiritSistersCallEngine(t, reg)

	// PRECONDITION: the animated Traveler is on the battlefield, and the dies
	// trigger the departure below must not fire has not fired yet (its
	// triggered ability is a real, supported corpus trigger -- the counter is
	// live, not vacuous).
	before := e.G.Obj(target)
	if before == nil || before.Zone != state.ZBattlefield {
		t.Fatalf("returned Traveler not on the battlefield before the departure (precondition): %+v", before)
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.TokenCreate && ev.Text == "w_1_1_spirit_flying"
	}); n != 0 {
		t.Fatalf("the Traveler's dies trigger fired before its departure (%d Spirit TokenCreate events): precondition broken", n)
	}

	// The Traveler is destroyed: the granted replacement rewrites the
	// battlefield->graveyard move as a battlefield->exile move.
	e.emit(events.Event{Kind: events.MoveZone, Obj: target,
		From: state.ZBattlefield, To: state.ZGraveyard})
	o := e.G.Obj(target)
	if o == nil {
		t.Fatal("the returned Traveler vanished entirely")
	}
	if o.Zone != state.ZExile {
		t.Fatalf("the returned Traveler's departure left it in %s, want exile (pre-fix behaviour: it dies to the graveyard)", o.Zone)
	}
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if id == target {
			t.Fatal("the returned Traveler reached the graveyard despite the granted Replacements$ Exile promise")
		}
	}
	// No logged battlefield-departure MoveZone may ever have delivered the
	// Traveler to a graveyard: the replaced move is discarded, not merely
	// overwritten. (The Traveler's setup move from the library to the
	// graveyard is a different From zone and is deliberately not matched.)
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == target &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			t.Fatalf("a MoveZone event delivered the Traveler from the battlefield to the graveyard: %+v", ev)
		}
	}
	// THE DIES-TRIGGER COUNTER: because the Traveler was exiled rather than
	// put into a graveyard, "when Doomed Traveler dies" never fires -- no
	// Spirit token is created for its controller.
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "w_1_1_spirit_flying" {
			t.Fatalf("the exiled Traveler's dies trigger fired and created a Spirit token: %+v", ev)
		}
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if b := e.G.Obj(id); b != nil && b.Face() != nil && b.Face().Name == "Spirit" {
			t.Fatalf("the exiled Traveler's dies trigger left a Spirit token %d on the battlefield", id)
		}
	}

	// The promise is consumed by the departure (the ExileOnMoved sweep):
	// return the card to the battlefield and confirm a second departure goes
	// to the graveyard normally -- the animation is gone (CR 400.7).
	e.emit(events.Event{Kind: events.MoveZone, Obj: target,
		From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(target); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned Traveler zone = %+v, want battlefield (precondition for the re-entry arm)", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: target,
		From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(target); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the Traveler's second departure ended in %+v, want the graveyard: the consumed replacement re-armed on re-entry (CR 400.7)", o)
	}
	// The dies trigger DOES fire on the un-animated second death, proving the
	// counter above was live rather than dead code. Drive the queued trigger
	// (the raw emit only logs the move; triggered abilities enter the stack
	// through putTriggersOnStack).
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 30)
	}
	created := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "w_1_1_spirit_flying" {
			created = true
		}
	}
	if !created {
		t.Fatal("the Traveler's dies trigger never fired even on the un-animated graveyard death: the dies-trigger counter is vacuous")
	}
	replayCheck(t, e, cfg)
}
